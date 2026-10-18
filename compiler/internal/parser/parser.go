package parser

import (
	"fmt"
	"kaula/internal/ast"
	"kaula/internal/errors"
	"kaula/internal/lexer"
	"kaula/internal/stdlib"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// verboseEnabled 控制加载日志输出（ninja 风格默认安静）
var verboseEnabled = false

// SetVerboseEnabled 设置详细输出（--verbose 开启）
func SetVerboseEnabled(on bool) { verboseEnabled = on }

// ParseTaskType 表示解析任务类型
type ParseTaskType int

const (
	TASK_PARSE_PROGRAM ParseTaskType = iota
	TASK_PARSE_STATEMENT
	TASK_PARSE_EXPRESSION
	TASK_PARSE_BINARY_EXPR
	TASK_PARSE_PRIMARY_EXPR
	TASK_PARSE_CALL_EXPR
	TASK_PARSE_MEMBER_ACCESS
	TASK_PARSE_IF_STATEMENT
	TASK_PARSE_WHILE_STATEMENT
	TASK_PARSE_FOR_STATEMENT
	TASK_PARSE_SWITCH_STATEMENT
	TASK_PARSE_FUNCTION_STATEMENT
	TASK_PARSE_CLASS_STATEMENT
	TASK_PARSE_BLOCK
)

// ParseTask 表示解析任务
type ParseTask struct {
	TaskType   ParseTaskType
	Precedence int
	Result     interface{}
}

// Parser 表示语法分析器
type Parser struct {
	lexer          *lexer.Lexer
	curTok         lexer.Token
	peekTok        lexer.Token
	lastTokLine    int // 最近一次被消费的 token 所在行（用于语句边界判断）
	errorCollector *errors.ErrorCollector
	logger         *log.Logger
	loggingEnabled bool
	file           string
	taskStack      []ParseTask
	skipMainCheck  bool // 跳过 main 函数检查（用于导入目标文件）
	pendingAttributes []*ast.Attribute // 已解析但尚未应用的属性（#[attr] pub 场景）
}

// NewParser 创建一个新的语法分析器
func NewParser(lexer *lexer.Lexer) *Parser {
	p := &Parser{
		lexer:          lexer,
		errorCollector: errors.NewErrorCollector(),
		logger:         log.New(os.Stdout, "[Parser] ", log.LstdFlags),
		loggingEnabled: true,
		taskStack:      make([]ParseTask, 0, 64),
	}
	p.nextToken()
	p.nextToken()
	return p
}

// EnableLogging 启用日志记录
func (p *Parser) EnableLogging(enabled bool) {
	p.loggingEnabled = enabled
}

// SetSkipMainCheck 设置是否跳过 main 函数检查（用于导入的库文件）
func (p *Parser) SetSkipMainCheck(skip bool) {
	p.skipMainCheck = skip
}

// log 记录日志
func (p *Parser) log(format string, v ...interface{}) {
	if p.loggingEnabled {
		p.logger.Printf(format, v...)
	}
}

// nextToken 前进到下一个 token
func (p *Parser) nextToken() {
	p.lastTokLine = p.curTok.Line
	p.curTok = p.peekTok
	p.peekTok = p.lexer.Next()
}

// parseProgram 迭代解析整个程序
func (p *Parser) parseProgram() *ast.Program {
	p.log("开始解析程序")
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	program := &ast.Program{
		Statements: make([]ast.Statement, 0, 256), // 预分配容量，避免频繁扩容
		Pos:        pos,
		Source:     p.lexer.GetSource(),
	}

	maxStatements := 10000 // 限制最大语句数量
	statementCount := 0
	for p.curTok.Type != lexer.TOKEN_EOF {
		if p.loggingEnabled {
			p.log("当前 token: %s, 开始解析语句", lexer.TokenTypeToString(p.curTok.Type))
		}

		// 跳过空语句（分号、换行符等）
		if p.curTok.Type == lexer.TOKEN_SEMICOLON {
			p.nextToken()
			continue
		}

		stmt := p.parseStatementIterative()
		if stmt != nil {
			if p.loggingEnabled {
				p.log("解析完成语句：%s", stmt.String())
			}
			program.Statements = append(program.Statements, stmt)
			statementCount++
			if statementCount > maxStatements {
				// 超过最大语句数，跳出循环避免内存爆炸
				break
			}
		} else {
			// 如果无法解析，跳过当前 token 避免死循环
			p.nextToken()
		}
	}
	p.log("程序解析完成，共 %d 条语句", len(program.Statements))
	return program
}

// isExpressionAttribute 检查属性名是否是表达式级属性
// 表达式级属性作为语句出现时，不应被解析为声明
func isExpressionAttribute(name string) bool {
	switch name {
	case "asm", "volatile_load", "volatile_store",
		"atomic_load", "atomic_store", "atomic_cas", "atomic_faa",
		"fence":
		return true
	}
	return false
}

// getAttributeName 从 TOKEN_ATTRIBUTE 的 Value 中提取第一个属性名
func (p *Parser) getAttributeName() string {
	val := p.curTok.Value
	val = strings.TrimPrefix(val, "#[")
	val = strings.TrimSuffix(val, "]")
	for i, ch := range val {
		if ch == ',' || ch == '(' {
			return strings.TrimSpace(val[:i])
		}
	}
	return strings.TrimSpace(val)
}

// parseStatementIterative 迭代解析语句
func (p *Parser) parseStatementIterative() (stmt ast.Statement) {
	defer func() {
		// 消毒：子解析器返回的类型化 nil（如 (*VariableDeclaration)(nil)）会在
		// 接口转换后逃过 stmt != nil 判断，这里统一归一为真正的 nil
		if stmt != nil {
			if v := reflect.ValueOf(stmt); v.Kind() == reflect.Ptr && v.IsNil() {
				stmt = nil
			}
		}
	}()

	// 检查是否有属性注解，如果有则解析后分发给对应的声明
	if p.curTok.Type == lexer.TOKEN_ATTRIBUTE {
		// 预读属性后面的 token 类型来决定分发
		switch p.peekTok.Type {
		case lexer.TOKEN_PUB:
			// #[attr] pub fn/struct/... — 先解析属性暂存，交给 pub 解析器（函数解析时生效）
			p.pendingAttributes = p.parseAttributes()
			return p.parsePubStatementIterative()
		case lexer.TOKEN_FUNC:
			return p.parseFunctionStatementIterative()
		case lexer.TOKEN_STRUCT:
			return p.parseStructStatementIterative()
		case lexer.TOKEN_ENUM:
			return p.parseEnumStatementIterative()
		case lexer.TOKEN_TYPE:
			return p.parseTypeStatementIterative()
		case lexer.TOKEN_AUTO:
			return p.parseAutoDeclarationIterative()
		case lexer.TOKEN_TREE:
			return p.parseTreeStatementIterative()
		case lexer.TOKEN_TYPE_INT, lexer.TOKEN_TYPE_FLOAT, lexer.TOKEN_TYPE_DOUBLE,
			lexer.TOKEN_TYPE_BOOL, lexer.TOKEN_TYPE_CHAR, lexer.TOKEN_TYPE_STRING,
			lexer.TOKEN_TYPE_VOID, lexer.TOKEN_IDENT, lexer.TOKEN_MULTIPLY:
			// 可能是变量声明（#[volatile] i32 x）或表达式级属性后跟标识符（#[volatile_store(...)] vga_x = ...）
			// 通过属性名区分：表达式级属性按表达式语句处理
			if isExpressionAttribute(p.getAttributeName()) {
				return p.parseExpressionStatementIterative()
			}
			return p.parseVariableDeclarationIterative()
		}
		// 属性后面不是已知声明关键字，作为表达式级属性语句处理
		// 例如：#[asm("...")], #[volatile_store(addr, val)], #[fence()]
		return p.parseExpressionStatementIterative()
	}

	switch p.curTok.Type {
	case lexer.TOKEN_SPEND:
		return p.parseSpendCallStatementIterative()
	case lexer.TOKEN_SPEND_CALL:
		return p.parseSpendCallStatementIterative()
	case lexer.TOKEN_CALL:
		return p.parseCallStatementIterative()
	case lexer.TOKEN_PREFIX:
		return p.parsePrefixStatementIterative()
	case lexer.TOKEN_TREE:
		return p.parseTreeStatementIterative()
	case lexer.TOKEN_OBJECT:
		// object { ... } / object() 是动态对象字面量表达式；
		// object x = ... 是动态对象类型变量声明；
		// object Type Name { ... } 是旧的对象声明语句
		if p.peekTok.Type == lexer.TOKEN_LBRACE || p.peekTok.Type == lexer.TOKEN_LPAREN {
			return p.parseExpressionStatementIterative()
		}
		if p.peekTok.Type == lexer.TOKEN_IDENT {
			if stmt := p.parseVariableDeclarationIterative(); stmt != nil {
				return stmt
			}
		}
		return p.parseObjectStatementIterative()
	case lexer.TOKEN_FUNC:
		// fn 后面跟 ( 是 lambda 表达式，跟标识符是函数声明
		if p.peekTok.Type == lexer.TOKEN_LPAREN {
			return p.parseExpressionStatementIterative()
		}
		return p.parseFunctionStatementIterative()
	case lexer.TOKEN_CLASS:
		return p.parseClassStatementIterative()
	case lexer.TOKEN_LITERAL_INTERFACE:
		return p.parseInterfaceStatementIterative()
	case lexer.TOKEN_STRUCT:
		return p.parseStructStatementIterative()
	case lexer.TOKEN_ENUM:
		return p.parseEnumStatementIterative()
	case lexer.TOKEN_TYPE:
		return p.parseTypeStatementIterative()
	case lexer.TOKEN_AUTO:
		return p.parseAutoDeclarationIterative()
	case lexer.TOKEN_YIELD:
		return p.parseYieldStatementIterative()
	case lexer.TOKEN_RELEASE:
		return p.parseReleaseStatementIterative()
	case lexer.TOKEN_EXTRACT:
		return p.parseExtractStatementIterative()
	case lexer.TOKEN_IF:
		return p.parseIfStatementIterative()
	case lexer.TOKEN_WHILE:
		return p.parseWhileStatementIterative()
	case lexer.TOKEN_FOR:
		// range-based for: for x in <iterable> { body }
		// <iterable> 可以是 range(N) / range(start, end[, step]) 或数组/切片表达式
		if p.peekTok.Type != lexer.TOKEN_IDENT {
			p.error("expected 'for <var> in <iterable> { ... }' (range-based for); got token " + lexer.TokenTypeToString(p.peekTok.Type))
			return nil
		}
		varName := p.peekTok.Value
		// consume 'for' and the loop variable identifier
		p.nextToken() // curTok = IDENT
		p.nextToken() // consume identifier; curTok should be 'in'
		if p.curTok.Type != lexer.TOKEN_IN {
			p.error("expected 'in' after loop variable '" + varName + "'; use syntax: for " + varName + " in range(...) { ... }")
			return nil
		}
		return p.parseForInStatement(varName)
	case lexer.TOKEN_RETURN:
		return p.parseReturnStatementIterative()
	case lexer.TOKEN_BREAK:
		return p.parseBreakStatementIterative()
	case lexer.TOKEN_CONTINUE:
		return p.parseContinueStatementIterative()
	case lexer.TOKEN_IMPORT:
		return p.parseImportStatementIterative()
	case lexer.TOKEN_EXPORT:
		return p.parseExportStatementIterative()
	case lexer.TOKEN_PACKAGE:
		return p.parsePackageStatementIterative()
	case lexer.TOKEN_PUB:
		return p.parsePubStatementIterative()
	case lexer.TOKEN_EXTERN:
		return p.parseExternStatementIterative()
	case lexer.TOKEN_STATIC:
		return p.parseStaticDeclarationIterative()
	case lexer.TOKEN_CONST:
		return p.parseConstDeclarationIterative()
	case lexer.TOKEN_NONLOCAL:
		return p.parseNonLocalStatementIterative()
	case lexer.TOKEN_PRINTLN:
		return p.parseExpressionStatementIterative()
	case lexer.TOKEN_TYPE_INT, lexer.TOKEN_TYPE_FLOAT, lexer.TOKEN_TYPE_DOUBLE, lexer.TOKEN_TYPE_BOOL, lexer.TOKEN_TYPE_CHAR, lexer.TOKEN_TYPE_STRING, lexer.TOKEN_TYPE_VOID:
		// 类型关键字开头，尝试解析变量声明
		return p.parseVariableDeclarationIterative()
	case lexer.TOKEN_MULTIPLY:
		// * 开头可能是 C 风格指针声明: *Type name，也可能是解引用表达式: *p = x
		peekIsType := p.isTypeToken(p.peekTok.Type) || p.peekTok.Type == lexer.TOKEN_IDENT
		if peekIsType {
			// 先尝试按 *Type name 指针声明解析；失败则回退到表达式语句。
			// parseVariableDeclarationIterative 在判定不是声明时会恢复 token 流，
			// 因此回退时 * 仍位于 curTok，可正常进入解引用表达式分支。
			if decl := p.parseVariableDeclarationIterative(); decl != nil {
				return decl
			}
		}
		// 否则（或指针声明解析失败）作为表达式语句处理，例如 *p = x
		return p.parseExpressionStatementIterative()
	case lexer.TOKEN_LBRACKET:
		// [ 开头可能是固定大小数组声明: [N]type name 或 []type name
		if stmt := p.parseVariableDeclarationIterative(); stmt != nil {
			return stmt
		}
		// 否则作为数组字面量表达式处理
		return p.parseExpressionStatementIterative()
	case lexer.TOKEN_IDENT:
		// 检查是否是变量声明：标识符后面跟另一个标识符（类型名）
		// 例如：i64 x, int y, MyType obj
		if p.peekTok.Type == lexer.TOKEN_IDENT || p.peekTok.Type == lexer.TOKEN_QUESTION || p.peekTok.Type == lexer.TOKEN_MULTIPLY || p.peekTok.Type == lexer.TOKEN_LT {
			// 可能是变量声明，但也可能是其他语句
			// 先尝试变量声明
			if stmt := p.parseVariableDeclarationIterative(); stmt != nil {
				return stmt
			}
		}
		if p.peekTok.Type == lexer.TOKEN_LBRACE {
			return p.parsePrefixCallStatementIterative()
		}
		// 其他情况，解析为表达式语句（赋值、函数调用等）
		return p.parseExpressionStatementIterative()
	case lexer.TOKEN_AT:
		return p.parsePrefixCallStatementIterative()
	case lexer.TOKEN_CONSTRUCTOR:
		return nil
	case lexer.TOKEN_SEMICOLON:
		return nil
	case lexer.TOKEN_RBRACE:
		return nil
	default:
		// 尝试解析为表达式语句（如赋值、函数调用等）
		return p.parseExpressionStatementIterative()
	}
}

// parseVariableDeclarationIterative 迭代解析变量声明
// 语法：类型 变量名 [= 表达式]
// 例如：int x = 10  或者  int x
func (p *Parser) parseVariableDeclarationIterative() *ast.VariableDeclaration {
	stmt := &ast.VariableDeclaration{
		Pos: ast.Position{
			Line:   p.curTok.Line,
			Column: p.curTok.Column,
			File:   p.file,
		},
	}

	// 解析属性注解（如 #[volatile], #[section("...")], #[aligned(N)] 等）
	attrs := p.parseAttributes()
	if attrs != nil {
		stmt.Attributes = attrs
	}

	// 保存 token 状态：若最终判定不是变量声明（返回 nil），必须恢复 token 流，
	// 让调用方以表达式语句等其它方式重新解析，避免吞掉后续语句的 token
	// （例如 "u8 buf[64]" 后紧跟 "foo(a, b)" 时，[64]foo( 会被此函数预读后失败）
	// 注意：必须同时恢复 lexer 状态，否则 token 窗口虽然还原，lexer 内部位置
	// 仍停留在预读处，下一次 nextToken() 会跳过中间 token
	savedLexState := p.lexer.SaveState()
	savedCurTok := p.curTok
	savedPeekTok := p.peekTok

	// 首先检查是否有类型关键字
	var typeName string

	// 检查是否是指针前缀 (*Type name)
	isPointerPrefix := false
	if p.curTok.Type == lexer.TOKEN_MULTIPLY {
		isPointerPrefix = true
		p.nextToken()
	}

	// 检查是否是基本类型关键字（int, float, string 等）
	if p.isTypeToken(p.curTok.Type) {
		// void(T...)R 签名记法：必须交由 parseTypeString 完整解析，否则 () 会被误判
		// 用 non-greedy 版本：不吃尾部返回类型（避免把变量名误当返回类型）
		if p.curTok.Type == lexer.TOKEN_TYPE_VOID && p.peekTok.Type == lexer.TOKEN_LPAREN {
			typeName = p.parseTypeStringForDecl()
		} else {
			typeName = lexer.TokenTypeToString(p.curTok.Type)
			typeName = strings.TrimPrefix(typeName, "TYPE_")
			// 转换为小写（如 "INT" -> "int"）
			typeName = strings.ToLower(typeName)
			p.nextToken()
		}
	} else if p.curTok.Type == lexer.TOKEN_OBJECT {
		// object 作为动态对象类型：object x = object { ... }
		typeName = "object"
		p.nextToken()
	} else if p.curTok.Type == lexer.TOKEN_IDENT {
		// 可能是自定义类型（如类名、结构体名等）
		typeName = p.curTok.Value
		p.nextToken()

		// 检查是否是泛型类型（如 Box<int>）
		if p.curTok.Type == lexer.TOKEN_LT {
			p.nextToken()
			typeArgs := []string{}
			for p.curTok.Type == lexer.TOKEN_IDENT || p.isTypeToken(p.curTok.Type) {
				typeArgs = append(typeArgs, p.curTok.Value)
				p.nextToken()
				if p.curTok.Type == lexer.TOKEN_COMMA {
					p.nextToken()
				} else if p.curTok.Type == lexer.TOKEN_GT {
					break
				}
			}
			if p.curTok.Type == lexer.TOKEN_GT {
				p.nextToken()
				// 构建泛型类型名称
				typeName = typeName + "<"
				for i, arg := range typeArgs {
					if i > 0 {
						typeName += ","
					}
					typeName += arg
				}
				typeName += ">"
			}
		}
	} else if p.curTok.Type == lexer.TOKEN_LBRACKET {
		// 数组类型 [N]type 或 []type（固定大小数组 / 动态数组）
		p.nextToken() // consume '['
		arraySize := ""
		if p.curTok.Type == lexer.TOKEN_LITERAL_INT || p.curTok.Type == lexer.TOKEN_IDENT {
			arraySize = p.curTok.Value
			p.nextToken()
		}
		if p.curTok.Type == lexer.TOKEN_RBRACKET {
			p.nextToken()
		}
		elemType := ""
		isElemType := false
		switch p.curTok.Type {
		case lexer.TOKEN_TYPE_INT, lexer.TOKEN_TYPE_FLOAT, lexer.TOKEN_TYPE_DOUBLE,
			lexer.TOKEN_TYPE_BOOL, lexer.TOKEN_TYPE_CHAR, lexer.TOKEN_TYPE_STRING,
			lexer.TOKEN_TYPE_VOID:
			elemType = p.curTok.Value
			isElemType = true
		case lexer.TOKEN_IDENT:
			elemType = p.curTok.Value
			isElemType = true
		}
		if !isElemType {
			p.lexer.RestoreState(savedLexState)
			p.curTok = savedCurTok
			p.peekTok = savedPeekTok
			return nil
		}
		p.nextToken()
		typeName = "[" + arraySize + "]" + elemType
	} else {
		// 不是类型，恢复 token 并返回 nil
		p.lexer.RestoreState(savedLexState)
		p.curTok = savedCurTok
		p.peekTok = savedPeekTok
		return nil
	}

	// 现在必须有变量名
	// 检查是否是可空类型（?）在变量名前面 - 语法: Type? name
	if p.curTok.Type == lexer.TOKEN_QUESTION {
		stmt.Nullable = true
		p.nextToken()
	}

	// 如果之前有 * 前缀 (*Type name)，添加 * 到类型名
	if isPointerPrefix {
		typeName = typeName + "*"
	}

	// 检查是否是指针类型（*）在变量名前面 - 语法: Type* name
	if p.curTok.Type == lexer.TOKEN_MULTIPLY {
		// 指针类型，在类型名后添加 *
		typeName = typeName + "*"
		p.nextToken()
	}

	if p.curTok.Type != lexer.TOKEN_IDENT {
		// 不是变量名，恢复 token 流并返回 nil（不要报告错误，因为可能是其他语句）
		p.lexer.RestoreState(savedLexState)
		p.curTok = savedCurTok
		p.peekTok = savedPeekTok
		return nil
	}

	stmt.Type = typeName
	stmt.Name = p.curTok.Value
	p.nextToken()

	// 检查变量名后是否还有指针修饰符 - 语法: Type name*
	if p.curTok.Type == lexer.TOKEN_MULTIPLY {
		stmt.Type = stmt.Type + "*"
		p.nextToken()
	}

	// 检查变量名后是否还有数组后缀 - 语法: Type name[N]
	// （与 [N]Type name 前缀形式等价，demo 大量使用后缀形式）
	// 注意：若类型已用 [N] 前缀形式（typeName 以 [ 开头），不能再有后缀，
	// 否则会吞掉下一行的前缀数组声明（如 [10]Entity ents 后跟 [3400]f64 zb）。
	if p.curTok.Type == lexer.TOKEN_LBRACKET && !strings.HasPrefix(stmt.Type, "[") {
		p.nextToken() // consume '['
		arraySize := ""
		if p.curTok.Type == lexer.TOKEN_LITERAL_INT || p.curTok.Type == lexer.TOKEN_IDENT {
			arraySize = p.curTok.Value
			p.nextToken()
		}
		if p.curTok.Type == lexer.TOKEN_RBRACKET {
			p.nextToken()
			stmt.Type = "[" + arraySize + "]" + stmt.Type
		} else {
			p.error("expected ']' after array size")
		}
	}

	// 检查是否有赋值
	if p.curTok.Type == lexer.TOKEN_ASSIGN {
		p.nextToken()
		stmt.Value = p.parseExpressionIterative()
	}

	return stmt
}

// parseExternStatementIterative 解析 extern 外部符号/函数声明
// 变量语法: extern name: type
// 函数语法: extern fn name(params) -> return_type
// 示例:
//   extern bss_start: u8
//   extern fn boot_main() -> void
func (p *Parser) parseExternStatementIterative() *ast.ExternStatement {
	stmt := &ast.ExternStatement{
		Pos: ast.Position{Line: p.curTok.Line, Column: p.curTok.Column},
	}

	p.nextToken() // 跳过 extern

	// 检查是否是函数声明: extern fn name(...)
	if p.curTok.Type == lexer.TOKEN_FUNC {
		return p.parseExternFunctionIterative(stmt)
	}

	// 变量声明: extern name: type
	if p.curTok.Type != lexer.TOKEN_IDENT {
		p.error("extern 后应该跟符号名或 fn")
		return nil
	}
	stmt.Name = p.curTok.Value
	p.nextToken()

	// extern 符号名后必须跟 : type
	if p.curTok.Type != lexer.TOKEN_COLON {
		p.error("extern 声明需要 : type 语法，如 extern name: type")
		return nil
	}
	p.nextToken() // 跳过 :

	// 解析类型
	typeName := p.parseTypeString()
	if typeName == "" {
		p.error("extern 声明需要有效的类型")
		return nil
	}
	stmt.Type = typeName

	return stmt
}

// parseExternFunctionIterative 解析 extern fn 函数声明
// 语法: extern fn name(param1: type1, param2: type2) -> return_type
func (p *Parser) parseExternFunctionIterative(stmt *ast.ExternStatement) *ast.ExternStatement {
	stmt.IsFunction = true
	p.nextToken() // 跳过 fn

	if p.curTok.Type != lexer.TOKEN_IDENT {
		p.error("extern fn 后应该跟函数名")
		return nil
	}
	stmt.Name = p.curTok.Value
	p.nextToken()

	// 解析参数列表
	if p.curTok.Type != lexer.TOKEN_LPAREN {
		p.error("extern fn 声明需要参数列表 (")
		return nil
	}
	p.nextToken() // 跳过 (

	for p.curTok.Type != lexer.TOKEN_RPAREN && p.curTok.Type != lexer.TOKEN_EOF {
		// 解析参数: name: type
		if p.curTok.Type != lexer.TOKEN_IDENT {
			p.error("extern fn 参数需要名称")
			return nil
		}
		paramName := p.curTok.Value
		p.nextToken()

		if p.curTok.Type != lexer.TOKEN_COLON {
			p.error("extern fn 参数需要 : type 语法")
			return nil
		}
		p.nextToken() // 跳过 :

		paramType := p.parseTypeStringForDecl()
		if paramType == "" {
			p.error("extern fn 参数需要有效的类型")
			return nil
		}

		stmt.Params = append(stmt.Params, paramName)
		stmt.ParamTypes = append(stmt.ParamTypes, paramType)

		if p.curTok.Type == lexer.TOKEN_COMMA {
			p.nextToken()
		}
	}

	if p.curTok.Type != lexer.TOKEN_RPAREN {
		p.error("extern fn 参数列表需要 )")
		return nil
	}
	p.nextToken() // 跳过 )

	// 解析返回类型: -> type
	if p.curTok.Type == lexer.TOKEN_ARROW {
		p.nextToken() // 跳过 ->
		returnType := p.parseTypeStringForDecl()
		if returnType == "" {
			p.error("extern fn 返回类型无效")
			return nil
		}
		stmt.ReturnType = returnType
		return stmt
	}
	if p.curTok.Type == lexer.TOKEN_MINUS && p.peekTok.Type == lexer.TOKEN_GT {
		p.nextToken() // 跳过 -
		p.nextToken() // 跳过 >
		returnType := p.parseTypeStringForDecl()
		if returnType == "" {
			p.error("extern fn 返回类型无效")
			return nil
		}
		stmt.ReturnType = returnType
		stmt.Type = returnType
	} else if p.curTok.Type == lexer.TOKEN_ARROW {
		p.nextToken()
		returnType := p.parseTypeStringForDecl()
		if returnType == "" {
			p.error("extern fn 返回类型无效")
			return nil
		}
		stmt.ReturnType = returnType
		stmt.Type = returnType
	} else {
		// 无返回类型默认 void
		stmt.ReturnType = "void"
		stmt.Type = "void"
	}

	return stmt
}

// parseStaticDeclarationIterative 解析 static 静态变量声明
// 语法: static name: type = value  或  static type name = value
// 函数内的静态变量，生命周期为整个程序，只初始化一次
// 全局作用域等价于普通变量（C 的 static 链接语义不同，这里只做存储期语义）
func (p *Parser) parseStaticDeclarationIterative() *ast.VariableDeclaration {
	p.nextToken() // 跳过 static
	stmt := p.parseVariableDeclarationIterative()
	if stmt != nil {
		stmt.IsStatic = true
	}
	return stmt
}

// parseConstDeclarationIterative 解析 const 编译期常量声明
// 语法: const type name = value  或  const name = value
// 编译期常量，不可修改
func (p *Parser) parseConstDeclarationIterative() *ast.VariableDeclaration {
	p.nextToken() // 跳过 const

	// const name = value 风格（无类型，从值推导）
	if p.curTok.Type == lexer.TOKEN_IDENT && p.peekTok.Type == lexer.TOKEN_ASSIGN {
		stmt := &ast.VariableDeclaration{
			IsConst: true,
			Pos: ast.Position{
				Line:   p.curTok.Line,
				Column: p.curTok.Column,
				File:   p.file,
			},
		}
		stmt.Name = p.curTok.Value
		p.nextToken() // 跳过 name
		if p.curTok.Type == lexer.TOKEN_ASSIGN {
			p.nextToken()
			stmt.Value = p.parseExpressionIterative()
		}
		return stmt
	}

	stmt := p.parseVariableDeclarationIterative()
	if stmt != nil {
		stmt.IsConst = true
		if stmt.Value == nil {
			p.error("const 声明必须有初始值")
			return nil
		}
	}
	return stmt
}

// parseAutoDeclarationIterative 解析 auto 声明
// 语法: auto name = expression
// 支持属性: #[volatile] auto ptr = ...
func (p *Parser) parseAutoDeclarationIterative() *ast.VariableDeclaration {
	stmt := &ast.VariableDeclaration{
		IsAuto: true,
		Pos: ast.Position{
			Line:   p.curTok.Line,
			Column: p.curTok.Column,
			File:   p.file,
		},
	}

	// 解析属性注解（如 #[volatile], #[section("...")] 等）
	attrs := p.parseAttributes()
	if attrs != nil {
		stmt.Attributes = attrs
	}

	p.nextToken()

	if p.curTok.Type != lexer.TOKEN_IDENT {
		p.error("auto 后应该跟变量名")
		return nil
	}

	stmt.Name = p.curTok.Value
	p.nextToken()

	if p.curTok.Type != lexer.TOKEN_ASSIGN {
		p.error("auto 声明必须有初始值用于类型推导 (auto name = value)")
		return nil
	}

	p.nextToken()
	stmt.Value = p.parseExpressionIterative()

	return stmt
}

// inferLiteralType 根据字面量推断 Kaula 类型（顶层 var 用）
// 与 sema.inferExpressionType 的整数字面量规则保持一致：按数值大小选 u8/u16/u32/u64
func inferLiteralType(value ast.Expression) string {
	switch v := value.(type) {
	case *ast.IntegerLiteral:
		if v == nil {
			return ""
		}
		if v.Value <= 255 {
			return "u8"
		}
		if v.Value <= 65535 {
			return "u16"
		}
		if v.Value <= 4294967295 {
			return "u32"
		}
		return "u64"
	case *ast.FloatLiteral:
		return "f64"
	case *ast.StringLiteral:
		return "string"
	case *ast.CharLiteral:
		return "char"
	case *ast.BooleanLiteral:
		return "bool"
	}
	return ""
}

// isTypeToken 检查是否是类型关键字
func (p *Parser) isTypeToken(tokenType lexer.TokenType) bool {
	return tokenType >= lexer.TOKEN_TYPE_INT && tokenType <= lexer.TOKEN_TYPE_VOID
}

// isIdentOrTypeToken 检查是否是标识符或类型关键字
func (p *Parser) isIdentOrTypeToken(tokenType lexer.TokenType) bool {
	return tokenType == lexer.TOKEN_IDENT ||
		(tokenType >= lexer.TOKEN_TYPE_INT && tokenType <= lexer.TOKEN_TYPE_VOID)
}

// parseCallStatementIterative 迭代解析 call 语句
func (p *Parser) parseCallStatementIterative() *ast.CallStatement {
	p.log("开始解析 call 语句")
	stmt := &ast.CallStatement{}
	p.nextToken()
	if p.curTok.Type == lexer.TOKEN_LPAREN {
		p.nextToken()
		stmt.Target = p.parseExpressionIterative()
		p.log("解析 call 目标")
		if p.curTok.Type == lexer.TOKEN_RPAREN {
			p.nextToken()
		}
	}
	if p.curTok.Type == lexer.TOKEN_COLON {
		p.nextToken()
		callBody := []ast.Statement{}
		p.log("开始解析 call 语句体")
		bodyStmt := p.parseStatementIterative()
		if bodyStmt != nil {
			callBody = append(callBody, bodyStmt)
			p.log("call 语句体添加语句")
		}
		if p.curTok.Type == lexer.TOKEN_LBRACE {
			p.nextToken()
			for p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
				bodyStmt := p.parseStatementIterative()
				if bodyStmt != nil {
					callBody = append(callBody, bodyStmt)
					p.log("call 语句体添加语句")
				}
				if p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
					p.nextToken()
				}
			}
			if p.curTok.Type == lexer.TOKEN_RBRACE {
				p.nextToken()
			}
		}
		stmt.Body = callBody
		p.log("call 语句体解析完成，共 %d 条语句", len(callBody))
	}
	p.log("call 语句解析完成")
	return stmt
}

// parseSpendCallStatementIterative 迭代解析 spend/call 语句
// 新语法：
// spend(obj1){
//     call(1){
//         return 1
//     }
//     call(2){
//         return 2
//     }
// }
func (p *Parser) parseSpendCallStatementIterative() *ast.SpendStatement {
	p.log("开始解析 spend 语句")
	stmt := &ast.SpendStatement{
		Calls: []*ast.CallClause{},
	}

	p.nextToken() // 跳过 spend/spend_call 关键字

	// 解析 spend 目标
	if p.curTok.Type == lexer.TOKEN_LPAREN {
		p.nextToken()
		stmt.Target = p.parseExpressionIterative()
		p.log("解析 spend 目标表达式")
		if p.curTok.Type == lexer.TOKEN_RPAREN {
			p.nextToken()
		}
	}

	// 解析花括号内的 call 子句
	if p.curTok.Type == lexer.TOKEN_LBRACE {
		p.nextToken()
		p.log("开始解析 call 子句")

		for p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
			// 检查是否是 call 关键字
			if p.curTok.Type == lexer.TOKEN_CALL {
				callClause := p.parseCallClause()
				if callClause != nil {
					stmt.Calls = append(stmt.Calls, callClause)
					p.log("添加 call 子句，索引=%s", callClause.Index)
				}
			} else {
				// 跳过其他 token
				p.nextToken()
			}
		}

		if p.curTok.Type == lexer.TOKEN_RBRACE {
			p.nextToken()
		}
	}

	p.log("spend 语句解析完成，共 %d 个 call 子句", len(stmt.Calls))
	return stmt
}

// parseCallClause 解析 call 子句
// 语法：call(index){ body }
func (p *Parser) parseCallClause() *ast.CallClause {
	if p.curTok.Type != lexer.TOKEN_CALL {
		return nil
	}

	p.nextToken() // 跳过 CALL token

	clause := &ast.CallClause{
		Body: []ast.Statement{},
	}

	// 解析索引
	if p.curTok.Type == lexer.TOKEN_LPAREN {
		p.nextToken()
		if p.curTok.Type == lexer.TOKEN_DEFAULT {
			// call(default) 兜底子句：消费全部剩余元素
			clause.IsDefault = true
			p.nextToken()
		} else {
			clause.Index = p.parseExpressionIterative()
		}
		if p.curTok.Type == lexer.TOKEN_RPAREN {
			p.nextToken()
		}
	}

	// 解析处理逻辑
	if p.curTok.Type == lexer.TOKEN_LBRACE {
		p.nextToken()

		for p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
			bodyStmt := p.parseStatementIterative()
			if bodyStmt != nil {
				clause.Body = append(clause.Body, bodyStmt)
			}
		}

		if p.curTok.Type == lexer.TOKEN_RBRACE {
			p.nextToken()
		}
	}

	return clause
}

// parsePrefixStatementIterative 迭代解析 prefix 语句
func (p *Parser) parsePrefixStatementIterative() *ast.PrefixStatement {
	// 检查是否是 prefix 模块调用（如 prefix.enter()）
	if p.peekNextTokenType() == lexer.TOKEN_DOT {
		// 这是 prefix 模块调用，不是 prefix 语句，返回 nil
		return nil
	}

	stmt := &ast.PrefixStatement{
		Body: []ast.Statement{},
	}
	p.nextToken()
	if p.curTok.Type == lexer.TOKEN_IDENT {
		stmt.Name = p.curTok.Value
		p.nextToken()
	} else if p.curTok.Type == lexer.TOKEN_STRING {
		stmt.Name = p.curTok.Value
		p.nextToken()
	}
	if p.curTok.Type == lexer.TOKEN_LBRACE {
		p.nextToken()
		for p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
			bodyStmt := p.parseStatementIterative()
			if bodyStmt != nil {
				stmt.Body = append(stmt.Body, bodyStmt)
			} else {
				p.nextToken()
			}
		}
		if p.curTok.Type == lexer.TOKEN_RBRACE {
			p.nextToken()
		}
	}
	return stmt
}

// parseTreeStatementIterative 迭代解析 tree 语句
func (p *Parser) parseTreeStatementIterative() *ast.TreeStatement {
	stmt := &ast.TreeStatement{
		Body: []ast.Statement{},
	}

	// 检查是否有注解
	attrs := p.parseAttributes()
	if attrs != nil {
		// tree 注解只用第一个属性名
		if len(attrs) > 0 {
			stmt.Annotation = ast.ParseTreeAnnotation(attrs[0].Name)
		}
		p.log("解析 tree 注解：annotation=%v", stmt.Annotation)
	}

	p.nextToken()

	// 解析树名称（可选）
	if p.curTok.Type == lexer.TOKEN_LPAREN {
		p.nextToken()
		stmt.Root = p.parseExpressionIterative()
		if p.curTok.Type == lexer.TOKEN_RPAREN {
			p.nextToken()
		}
	}

	// 解析 tree body（如果有花括号）
	if p.curTok.Type == lexer.TOKEN_LBRACE {
		p.nextToken()
		for p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
			bodyStmt := p.parseStatementIterative()
			if bodyStmt != nil {
				stmt.Body = append(stmt.Body, bodyStmt)
			}
			if p.curTok.Type != lexer.TOKEN_RBRACE {
				p.nextToken()
			}
		}
		if p.curTok.Type == lexer.TOKEN_RBRACE {
			p.nextToken()
		}
	}

	return stmt
}

// parseObjectStatementIterative 迭代解析 object 语句
func (p *Parser) parseObjectStatementIterative() *ast.ObjectStatement {
	stmt := &ast.ObjectStatement{
		Fields: []ast.Expression{},
	}
	p.nextToken()
	if p.curTok.Type == lexer.TOKEN_IDENT {
		stmt.Type = p.curTok.Value
		p.nextToken()
	}
	if p.curTok.Type == lexer.TOKEN_IDENT {
		stmt.Name = p.curTok.Value
		p.nextToken()
	}
	if p.curTok.Type == lexer.TOKEN_SELF {
		p.nextToken()
		if p.curTok.Type == lexer.TOKEN_LPAREN {
			p.nextToken()
			for p.curTok.Type != lexer.TOKEN_RPAREN {
				field := p.parseExpressionIterative()
				stmt.Fields = append(stmt.Fields, field)
				if p.curTok.Type == lexer.TOKEN_COMMA {
					p.nextToken()
				}
			}
			if p.curTok.Type == lexer.TOKEN_RPAREN {
				p.nextToken()
			}
		}
		if p.curTok.Type == lexer.TOKEN_LBRACE {
			p.nextToken()
			for p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
				if p.curTok.Type == lexer.TOKEN_IDENT {
					p.nextToken()
					if p.curTok.Type == lexer.TOKEN_COLON {
						p.nextToken()
						fieldValue := p.parseExpressionIterative()
						stmt.Fields = append(stmt.Fields, fieldValue)
						if p.curTok.Type == lexer.TOKEN_COMMA {
							p.nextToken()
						}
					}
				} else {
					p.nextToken()
				}
			}
			if p.curTok.Type == lexer.TOKEN_RBRACE {
				p.nextToken()
			}
		}
	}
	if p.curTok.Type == lexer.TOKEN_ASSIGN {
		p.nextToken()
		stmt.Value = p.parseExpressionIterative()
	}
	if p.curTok.Type == lexer.TOKEN_DOUBLE_COLON {
		p.nextToken()
		if p.curTok.Type == lexer.TOKEN_LBRACKET {
			p.nextToken()
			for p.curTok.Type != lexer.TOKEN_RBRACKET && p.curTok.Type != lexer.TOKEN_EOF {
				stmt.Fields = append(stmt.Fields, p.parseExpressionIterative())
				if p.curTok.Type == lexer.TOKEN_COMMA {
					p.nextToken()
				} else if p.curTok.Type != lexer.TOKEN_RBRACKET {
					p.nextToken()
				}
			}
			if p.curTok.Type == lexer.TOKEN_RBRACKET {
				p.nextToken()
			}
		}
	}
	return stmt
}

// parseObjectLiteralExpressionIterative 解析动态对象字面量
// 语法：object { name: value, ... } 或 object()
// 字段值可以是任意表达式（字面量、嵌套对象、lambda 方法等）
func (p *Parser) parseObjectLiteralExpressionIterative() ast.Expression {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	stmt := &ast.ObjectLiteral{
		Fields: []ast.ObjectLiteralField{},
		Pos:    pos,
	}
	p.nextToken() // 跳过 object

	if p.curTok.Type == lexer.TOKEN_LPAREN {
		// object() 空动态对象
		p.nextToken()
		if p.curTok.Type == lexer.TOKEN_RPAREN {
			p.nextToken()
		}
		return stmt
	}

	if p.curTok.Type != lexer.TOKEN_LBRACE {
		p.error("expected '{' or '(' after 'object'")
		return stmt
	}
	p.nextToken() // 跳过 {

	for p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
		if p.curTok.Type == lexer.TOKEN_IDENT {
			fieldName := p.curTok.Value
			fieldPos := ast.Position{
				Line:   p.curTok.Line,
				Column: p.curTok.Column,
				File:   p.file,
			}
			p.nextToken()
			if p.curTok.Type == lexer.TOKEN_COLON {
				p.nextToken()
				fieldValue := p.parseExpressionIterative()
				stmt.Fields = append(stmt.Fields, ast.ObjectLiteralField{
					Name:  fieldName,
					Value: fieldValue,
					Pos:   fieldPos,
				})
			} else {
				p.error(fmt.Sprintf("expected ':' after field name '%s' in object literal", fieldName))
			}
		} else if p.curTok.Type == lexer.TOKEN_COMMA {
			p.nextToken()
		} else {
			p.nextToken()
		}
	}
	if p.curTok.Type == lexer.TOKEN_RBRACE {
		p.nextToken()
	}
	return stmt
}

// parseAttributes 解析 #[attr1, attr2(arg)] 格式的属性列表
// 返回解析出的属性切片，如果当前 token 不是 TOKEN_ATTRIBUTE 则返回 nil
func (p *Parser) parseAttributes() []*ast.Attribute {
	if p.curTok.Type != lexer.TOKEN_ATTRIBUTE {
		// 之前已解析但尚未应用的属性（#[attr] pub fn 场景）
		if p.pendingAttributes != nil {
			attrs := p.pendingAttributes
			p.pendingAttributes = nil
			return attrs
		}
		return nil
	}

	annotationValue := p.curTok.Value
	annotationContent := strings.TrimPrefix(annotationValue, "#[")
	annotationContent = strings.TrimSuffix(annotationContent, "]")

	attrs := ast.ParseAttributeList(annotationContent)

	p.log("解析属性：%s -> %d 个属性", annotationContent, len(attrs))

	p.nextToken()
	return attrs
}

// applyFunctionAttributes 将属性列表应用到函数声明
// 同时设置兼容旧字段（NoKMM/Inline/SOREnabled/Annotation）
func applyFunctionAttributes(stmt *ast.FunctionStatement, attrs []*ast.Attribute) {
	stmt.Attributes = attrs
	for _, attr := range attrs {
		switch attr.Name {
		case "no_kmm":
			stmt.NoKMM = true
		case "inline":
			stmt.Inline = true
		case "sor":
			stmt.SOREnabled = true
		case "naked":
			// 新属性，通过 Attributes 列表访问
		case "section":
			// 新属性，通过 Attributes 列表访问
		case "weak":
			// 新属性，通过 Attributes 列表访问
		case "deprecated":
			// 新属性，通过 Attributes 列表访问
		case "asm":
			stmt.IsAsm = true
		case "prefix":
			stmt.Annotation = ast.TreeAnnotationPrefix
		case "tree":
			if stmt.Annotation == ast.TreeAnnotationPrefix {
				stmt.Annotation = ast.TreeAnnotationPrefixTree
			} else {
				stmt.Annotation = ast.TreeAnnotationTree
			}
		case "root":
			stmt.Annotation = ast.TreeAnnotationRoot
		default:
			parsed := ast.ParseTreeAnnotation(attr.Name)
			if parsed != ast.TreeAnnotationNone {
				stmt.Annotation = parsed
			}
		}
	}
}

// parseFunctionStatementIterative 迭代解析函数语句
func (p *Parser) parseFunctionStatementIterative() *ast.FunctionStatement {
	p.log("开始解析函数语句")
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	stmt := &ast.FunctionStatement{
		Params: []string{},
		Body:   []ast.Statement{},
		Pos:    pos,
		NoKMM:  false,
		Inline: false,
	}

	// 解析函数注解（如果存在）
	attrs := p.parseAttributes()
	if attrs != nil {
		applyFunctionAttributes(stmt, attrs)
	}

	p.nextToken()
	if p.curTok.Type == lexer.TOKEN_IDENT {
		stmt.Name = p.curTok.Value
		p.nextToken()
	}
	// 解析泛型参数（如果存在）
	if p.curTok.Type == lexer.TOKEN_LT {
		p.nextToken()
		for p.curTok.Type == lexer.TOKEN_IDENT {
			stmt.TypeParams = append(stmt.TypeParams, &ast.TypeParameter{Name: p.curTok.Value})
			p.nextToken()
			if p.curTok.Type == lexer.TOKEN_COMMA {
				p.nextToken()
			} else if p.curTok.Type == lexer.TOKEN_GT {
				break
			}
		}
		if p.curTok.Type == lexer.TOKEN_GT {
			p.nextToken()
		}
	}
	if p.curTok.Type == lexer.TOKEN_LPAREN {
		p.nextToken()
		p.log("开始解析函数参数")
		for p.curTok.Type != lexer.TOKEN_RPAREN && p.curTok.Type != lexer.TOKEN_EOF {
			prevTok := p.curTok

			if p.curTok.Type == lexer.TOKEN_IDENT && p.peekTok.Type == lexer.TOKEN_COLON {
				// name: Type 形式（与 extern fn 参数一致）
				paramName := p.curTok.Value
				p.nextToken() // 跳过 name
				p.nextToken() // 跳过 :
				paramType := p.parseTypeStringForDecl()
				stmt.Params = append(stmt.Params, paramName)
				stmt.ParamTypes = append(stmt.ParamTypes, paramType)
				p.log("解析参数：%s (类型：%s)", paramName, paramType)
				if p.curTok.Type == lexer.TOKEN_COMMA {
					p.nextToken()
				}
				continue
			}

			if p.curTok.Type == lexer.TOKEN_IDENT || p.isTypeToken(p.curTok.Type) || p.curTok.Type == lexer.TOKEN_MULTIPLY {
				typeOrName := ""
				typeName := ""

				// 处理指针前缀 (*Type)
				if p.curTok.Type == lexer.TOKEN_MULTIPLY {
					p.nextToken()
					if p.curTok.Type == lexer.TOKEN_IDENT || p.isTypeToken(p.curTok.Type) {
						typeName = p.curTok.Value + "*"
						p.nextToken()
					}
				} else {
					typeOrName = p.curTok.Value
					p.nextToken()

					// 泛型类型参数（如 Option<int> opt）：参数上下文中的 < 只会是泛型类型
					if p.curTok.Type == lexer.TOKEN_LT {
						typeName = typeOrName + p.parseTypeStringForDecl()
						typeOrName = ""
						fmt.Printf("[PARAM-GENERIC] %s\n", typeName)
					} else if p.curTok.Type == lexer.TOKEN_MULTIPLY {
						// 检查后面是否跟着 * (Type*)
						typeName = typeOrName + "*"
						typeOrName = ""
						p.nextToken()
					}
				}

				if p.curTok.Type == lexer.TOKEN_IDENT {
					// 有类型：typeName 或 typeOrName 是类型，p.curTok.Value 是名称
					paramType := typeName
					if paramType == "" {
						paramType = typeOrName
					}
					stmt.Params = append(stmt.Params, p.curTok.Value)
					stmt.ParamTypes = append(stmt.ParamTypes, paramType)
					p.log("解析参数：%s (类型：%s)", p.curTok.Value, paramType)
					p.nextToken()
				} else if typeOrName != "" {
					// typeOrName 就是名称（无类型）
					stmt.Params = append(stmt.Params, typeOrName)
					stmt.ParamTypes = append(stmt.ParamTypes, "")
					p.log("解析参数：%s (无类型)", typeOrName)
				}
			}

			// 如果解析失败，跳过当前 token 避免死循环
			if p.curTok.Type == prevTok.Type && p.curTok.Value == prevTok.Value {
				p.log("跳过无法解析的参数 token: %s=%q", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
				p.nextToken()
			}

			if p.curTok.Type == lexer.TOKEN_COMMA {
				p.nextToken()
			}
		}
		p.log("函数参数解析完成，共 %d 个参数", len(stmt.Params))
		if p.curTok.Type == lexer.TOKEN_RPAREN {
			p.nextToken()
		}
	}
	// 解析返回类型（如果存在）(支持 -> type 形式)
	if p.curTok.Type == lexer.TOKEN_ARROW {
		p.nextToken()
	}
	if p.isTypeToken(p.curTok.Type) || p.curTok.Type == lexer.TOKEN_IDENT || p.curTok.Type == lexer.TOKEN_MULTIPLY {
		isPointer := false
		// 处理指针前缀 (*Type)
		if p.curTok.Type == lexer.TOKEN_MULTIPLY {
			isPointer = true
			p.nextToken()
		}
		if p.isTypeToken(p.curTok.Type) {
			stmt.ReturnType = strings.ToLower(lexer.TokenTypeToString(p.curTok.Type))
			stmt.ReturnType = strings.TrimPrefix(stmt.ReturnType, "type_")
		} else {
			stmt.ReturnType = p.curTok.Value
		}
		p.nextToken()
		// 处理指针后缀 (Type*)
		if p.curTok.Type == lexer.TOKEN_MULTIPLY {
			isPointer = true
			p.nextToken()
		}
		if isPointer {
			stmt.ReturnType = stmt.ReturnType + "*"
		}
	}
	if p.curTok.Type == lexer.TOKEN_COLON {
		p.error("unexpected ':' - Kaula uses braces {} for function bodies, not colons")
		p.nextToken()
	}
	// Kaula 只支持花括号语法：fn main() { ... }
	if p.curTok.Type != lexer.TOKEN_LBRACE {
		p.error("expected '{' to begin function body")
		return stmt
	}

	if stmt.IsAsm {
		source := p.lexer.GetSource()
		pos := p.lexer.GetPosition()
		for pos > 0 && source[pos] != '{' {
			pos--
		}
		if pos < len(source) && source[pos] == '{' {
			pos++
			startPos := pos
			braceCount := 1
			for pos < len(source) && braceCount > 0 {
				if source[pos] == '{' {
					braceCount++
				} else if source[pos] == '}' {
					braceCount--
				}
				pos++
			}
			if braceCount == 0 && pos > startPos {
				stmt.AsmBody = source[startPos : pos-1]
			}
		}
		p.lexer.SetPosition(pos)
		p.curTok = p.lexer.Next()
		p.peekTok = p.lexer.Next()
		return stmt
	} else {
		p.nextToken()
		maxStatements := 10000
		statementCount := 0
		for p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
			bodyStmt := p.parseStatementIterative()
			if bodyStmt != nil {
				stmt.Body = append(stmt.Body, bodyStmt)
				statementCount++
				if statementCount > maxStatements {
					break
				}
			} else {
				p.nextToken()
			}
		}
	}

	if p.curTok.Type == lexer.TOKEN_RBRACE {
		p.nextToken()
	} else {
		// 遇到 EOF 或其他 token，报错
		if p.curTok.Type == lexer.TOKEN_EOF {
			p.error("unexpected end of file - expected '}' to end function body")
		} else {
			p.error("expected '}' to end function body")
		}
	}
	return stmt
}

// parseIfStatementIterative 迭代解析 if 语句
func (p *Parser) parseIfStatementIterative() *ast.IfStatement {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	stmt := &ast.IfStatement{
		Body: []ast.Statement{},
		Else: []ast.Statement{},
		Pos:  pos,
	}
	p.nextToken() // consume 'if'

	// 解析 if 条件表达式
	// 直接交给表达式解析器：开头的 '(' 由分组表达式处理，
	// 支持 (a) == (b) 这种首项是括号表达式的比较链
	stmt.Condition = p.parseExpressionIterative()

	if p.curTok.Type == lexer.TOKEN_LBRACE {
		p.nextToken()
		for p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
			bodyStmt := p.parseStatementIterative()
			if bodyStmt != nil {
				stmt.Body = append(stmt.Body, bodyStmt)
			} else {
				p.nextToken()
			}
		}
		if p.curTok.Type == lexer.TOKEN_RBRACE {
			p.nextToken()
		}
	}
	if p.curTok.Type == lexer.TOKEN_ELSE {
		p.nextToken()
		// 检查是否是 else if
		if p.curTok.Type == lexer.TOKEN_IF {
			// 递归解析 else if 作为另一个 if 语句
			elseIfStmt := p.parseIfStatementIterative()
			stmt.Else = append(stmt.Else, elseIfStmt)
		} else if p.curTok.Type == lexer.TOKEN_LBRACE {
			p.nextToken()
			for p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
				bodyStmt := p.parseStatementIterative()
				if bodyStmt != nil {
					stmt.Else = append(stmt.Else, bodyStmt)
				}
				if bodyStmt == nil && p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
					p.nextToken()
				}
			}
			if p.curTok.Type == lexer.TOKEN_RBRACE {
				p.nextToken()
			}
		}
	}
	return stmt
}

// parseWhileStatementIterative 迭代解析 while 语句
func (p *Parser) parseWhileStatementIterative() *ast.WhileStatement {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	stmt := &ast.WhileStatement{
		Body: []ast.Statement{},
		Pos:  pos,
	}
	p.nextToken()
	// 解析 while 条件表达式：同 if，开头的 '(' 由分组表达式处理
	stmt.Condition = p.parseExpressionIterative()
	if p.curTok.Type == lexer.TOKEN_LBRACE {
		p.nextToken()
		for p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
			bodyStmt := p.parseStatementIterative()
			if bodyStmt != nil {
				stmt.Body = append(stmt.Body, bodyStmt)
			} else {
				p.nextToken()
			}
		}
		if p.curTok.Type == lexer.TOKEN_RBRACE {
			p.nextToken()
		}
	}
	return stmt
}

// parseForInStatement 解析 for x in expr { body } 安全迭代语句
// 调用时 curTok = TOKEN_IN, peekTok = 表达式开始
func (p *Parser) parseForInStatement(varName string) *ast.ForInStatement {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	p.nextToken() // consume TOKEN_IN

	iterExpr := p.parseExpressionIterative()

	stmt := &ast.ForInStatement{
		Variable: &ast.Identifier{Name: varName, Pos: pos},
		Iterable: iterExpr,
		Body:     []ast.Statement{},
		Pos:      pos,
	}

	if p.curTok.Type == lexer.TOKEN_LBRACE {
		p.nextToken()
		for p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
			bodyStmt := p.parseStatementIterative()
			if bodyStmt != nil {
				stmt.Body = append(stmt.Body, bodyStmt)
			}
			if bodyStmt == nil && p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
				p.nextToken()
			}
		}
		if p.curTok.Type == lexer.TOKEN_RBRACE {
			p.nextToken()
		}
	}
	return stmt
}

// parseReturnStatementIterative 迭代解析 return 语句
func (p *Parser) parseReturnStatementIterative() *ast.ReturnStatement {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	stmt := &ast.ReturnStatement{
		Pos: pos,
	}
	p.nextToken()
	stmt.Value = p.parseExpressionIterative()
	return stmt
}

// parseBreakStatementIterative 迭代解析 break 语句
func (p *Parser) parseBreakStatementIterative() *ast.BreakStatement {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	stmt := &ast.BreakStatement{
		Pos: pos,
	}
	p.nextToken()
	return stmt
}

// parseContinueStatementIterative 迭代解析 continue 语句
func (p *Parser) parseContinueStatementIterative() *ast.ContinueStatement {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	stmt := &ast.ContinueStatement{
		Pos: pos,
	}
	p.nextToken()
	return stmt
}

// parseImportStatementIterative 迭代解析 import 语句
// 支持两种形式：
//   1. import std.io           模块名导入
//   2. import "lib" / "file"   路径导入（相对路径以当前文件目录为基准，Python 风格）
func (p *Parser) parseImportStatementIterative() *ast.ImportStatement {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	stmt := &ast.ImportStatement{
		Pos: pos,
	}
	p.nextToken()
	if p.curTok.Type == lexer.TOKEN_STRING {
		// 路径导入：import "path"
		stmt.Path = p.curTok.Value
		p.nextToken()
		return stmt
	}
	if p.curTok.Type == lexer.TOKEN_IDENT || p.isTypeToken(p.curTok.Type) {
		stmt.Module = p.curTok.Value
		p.nextToken()
		for p.curTok.Type == lexer.TOKEN_DOT {
			p.nextToken()
			if p.curTok.Type == lexer.TOKEN_IDENT || p.isTypeToken(p.curTok.Type) {
				stmt.Module += "." + p.curTok.Value
				p.nextToken()
			} else {
				break
			}
		}
	}
	return stmt
}

// parsePackageStatementIterative 解析 package 声明
func (p *Parser) parsePackageStatementIterative() *ast.PackageStatement {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	stmt := &ast.PackageStatement{Pos: pos}
	p.nextToken() // 消耗 'package'
	if p.curTok.Type == lexer.TOKEN_IDENT {
		stmt.Name = p.curTok.Value
		p.nextToken()
		for p.curTok.Type == lexer.TOKEN_DOT {
			p.nextToken()
			if p.curTok.Type == lexer.TOKEN_IDENT {
				stmt.Name += "." + p.curTok.Value
				p.nextToken()
			} else {
				break
			}
		}
	}
	return stmt
}

// parsePubStatementIterative 解析 pub 修饰符 + 声明
// pub fn name() { ... }       → FunctionStatement{IsPublic: true}
// pub struct Name { ... }     → StructStatement (标记 public)
// pub var name = value        → VariableDeclaration (标记 public)
func (p *Parser) parsePubStatementIterative() ast.Statement {
	p.nextToken() // 消耗 'pub'

	switch p.curTok.Type {
	case lexer.TOKEN_FUNC:
		fn := p.parseFunctionStatementIterative()
		if fn != nil {
			fn.IsPublic = true
		}
		return fn
	case lexer.TOKEN_STRUCT:
		stmt := p.parseStructStatementIterative()
		// StructStatement 没有直接的 IsPublic 字段，但我们可以标记
		// 对于跨文件，struct 定义本身已经可见，pub 只是语义标记
		return stmt
	case lexer.TOKEN_CLASS:
		stmt := p.parseClassStatementIterative()
		return stmt
	case lexer.TOKEN_TYPE_INT, lexer.TOKEN_TYPE_FLOAT, lexer.TOKEN_TYPE_DOUBLE,
		lexer.TOKEN_TYPE_BOOL, lexer.TOKEN_TYPE_CHAR, lexer.TOKEN_TYPE_STRING,
		lexer.TOKEN_TYPE_VOID, lexer.TOKEN_IDENT, lexer.TOKEN_AUTO:
		stmt := p.parseVariableDeclarationIterative()
		if stmt != nil {
			stmt.IsPublic = true
		}
		return stmt
	default:
		// pub 后面不是已知声明类型，报错
		p.log("pub 后面必须是 fn/struct/class/变量声明，得到: %s", lexer.TokenTypeToString(p.curTok.Type))
		p.nextToken()
		return nil
	}
}

// parseExportStatementIterative 迭代解析 export 语句
// 支持三种形式：
//   1. export fn/var/const/class/struct/object <声明>  修饰符形式（含 pub 语义 + C 级导出）
//   2. export <name>                                 裸名形式：标记既有符号为导出（可转发导入的符号）
func (p *Parser) parseExportStatementIterative() ast.Statement {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}

	// 消耗 export 关键字
	p.nextToken()

	// 修饰符形式：export fn / export class / export struct / export object / export var|const
	switch p.curTok.Type {
	case lexer.TOKEN_FUNC:
		fn := p.parseFunctionStatementIterative()
		if fn != nil {
			fn.IsPublic = true
			fn.IsExported = true
		}
		return fn
	case lexer.TOKEN_CLASS:
		return p.parseClassStatementIterative()
	case lexer.TOKEN_STRUCT:
		return p.parseStructStatementIterative()
	case lexer.TOKEN_OBJECT:
		return p.parseObjectStatementIterative()
	case lexer.TOKEN_CONST:
		stmt := p.parseConstDeclarationIterative()
		if stmt != nil {
			stmt.IsPublic = true
			stmt.IsExported = true
		}
		return stmt
	case lexer.TOKEN_TYPE_INT, lexer.TOKEN_TYPE_FLOAT, lexer.TOKEN_TYPE_DOUBLE,
		lexer.TOKEN_TYPE_BOOL, lexer.TOKEN_TYPE_CHAR, lexer.TOKEN_TYPE_STRING,
		lexer.TOKEN_TYPE_VOID, lexer.TOKEN_AUTO:
		stmt := p.parseVariableDeclarationIterative()
		if stmt != nil {
			stmt.IsPublic = true
			stmt.IsExported = true
		}
		return stmt
	}

	// 词法关键字 var 不是 TOKEN_IDENT 专属语法，单独按 auto 风格解析：
	// export var name = value（类型推导）
	if p.curTok.Type == lexer.TOKEN_IDENT && p.curTok.Value == "var" &&
		p.peekTok.Type == lexer.TOKEN_IDENT {
		p.nextToken() // 消耗 var
		decl := &ast.VariableDeclaration{IsAuto: true, Pos: pos}
		decl.Name = p.curTok.Value
		p.nextToken()
		if p.curTok.Type != lexer.TOKEN_ASSIGN {
			p.error("var 声明必须有初始值用于类型推导 (var name = value)")
		} else {
			p.nextToken()
			decl.Value = p.parseExpressionIterative()
			// 顶层 var 无法依赖 sema 的类型推导（顶层 auto 有缺陷），在这里直接推导
			if t := inferLiteralType(decl.Value); t != "" {
				decl.Type = t
			} else {
				p.error(fmt.Sprintf("var 变量 '%s' 类型推导失败，请使用显式类型：export type %s = value", decl.Name, decl.Name))
			}
		}
		decl.IsPublic = true
		decl.IsExported = true
		return decl
	}

	// 裸名 / 类型开头声明：export name 或 export Type name
	if p.curTok.Type == lexer.TOKEN_IDENT {
		// 向后看一个 token 判断是变量声明（Type name）还是裸名导出
		isDecl := p.peekTok.Type == lexer.TOKEN_IDENT ||
			p.peekTok.Type == lexer.TOKEN_MULTIPLY ||
			p.peekTok.Type == lexer.TOKEN_TYPE_INT || p.peekTok.Type == lexer.TOKEN_TYPE_FLOAT ||
			p.peekTok.Type == lexer.TOKEN_TYPE_DOUBLE || p.peekTok.Type == lexer.TOKEN_TYPE_BOOL ||
			p.peekTok.Type == lexer.TOKEN_TYPE_CHAR || p.peekTok.Type == lexer.TOKEN_TYPE_STRING ||
			p.peekTok.Type == lexer.TOKEN_TYPE_VOID || p.peekTok.Type == lexer.TOKEN_AUTO
		if isDecl {
			stmt := p.parseVariableDeclarationIterative()
			if stmt != nil {
				stmt.IsPublic = true
				stmt.IsExported = true
			}
			return stmt
		}
	}

	// 裸名导出：export <name>（符号可来自本文件声明，或 import 导入的其他模块/库）
	stmt := &ast.ExportStatement{
		Type: "function",
		Pos:  pos,
	}
	if p.curTok.Type == lexer.TOKEN_IDENT {
		stmt.Name = p.curTok.Value
		p.nextToken()
	} else {
		p.error("export 语句后应该跟标识符")
	}
	return stmt
}

// parseNonLocalStatementIterative 迭代解析 nonlocal 语句
func (p *Parser) parseNonLocalStatementIterative() *ast.NonLocalStatement {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	stmt := &ast.NonLocalStatement{
		Pos: pos,
	}
	p.nextToken()
	if p.isTypeToken(p.curTok.Type) || p.curTok.Type == lexer.TOKEN_IDENT {
		stmt.Type = p.curTok.Value
		p.nextToken()
	}
	if p.isTypeToken(p.curTok.Type) || p.curTok.Type == lexer.TOKEN_IDENT {
		stmt.Name = p.curTok.Value
		p.nextToken()
	}
	if p.curTok.Type == lexer.TOKEN_ASSIGN {
		p.nextToken()
		stmt.Value = p.parseExpressionIterative()
	}
	return stmt
}

// parseClassStatementIterative 迭代解析类定义
func (p *Parser) parseClassStatementIterative() *ast.ClassStatement {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	stmt := &ast.ClassStatement{
		Fields:       make([]*ast.FieldDeclaration, 0, 16),
		Methods:      make([]*ast.MethodStatement, 0, 16),
		Constructors: make([]*ast.ConstructorStatement, 0, 4),
		Implements:   make([]string, 0, 4),
		Pos:          pos,
	}
	p.nextToken()
	if p.curTok.Type == lexer.TOKEN_IDENT {
		stmt.Name = p.curTok.Value
		p.nextToken()
	}
	// 解析泛型参数（如果存在）
	if p.curTok.Type == lexer.TOKEN_LT {
		p.nextToken()
		for p.curTok.Type == lexer.TOKEN_IDENT {
			stmt.TypeParams = append(stmt.TypeParams, &ast.TypeParameter{Name: p.curTok.Value})
			p.nextToken()
			if p.curTok.Type == lexer.TOKEN_COMMA {
				p.nextToken()
			} else if p.curTok.Type == lexer.TOKEN_GT {
				break
			}
		}
		if p.curTok.Type == lexer.TOKEN_GT {
			p.nextToken()
		}
		stmt.Generic = true // 标记为泛型类
	}
	if p.curTok.Type == lexer.TOKEN_IMPLEMENTS {
		p.nextToken()
		for p.curTok.Type == lexer.TOKEN_IDENT {
			stmt.Implements = append(stmt.Implements, p.curTok.Value)
			p.nextToken()
			if p.curTok.Type == lexer.TOKEN_COMMA {
				p.nextToken()
			}
		}
	}
	if p.curTok.Type == lexer.TOKEN_LBRACE {
		p.nextToken()
		for p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
			p.log("当前 token: %s, 开始解析类成员", lexer.TokenTypeToString(p.curTok.Type))

			// 检查是否是类型关键字开头（方法或字段类型）
			isTypeKeyword := false
			switch p.curTok.Type {
			case lexer.TOKEN_TYPE_INT, lexer.TOKEN_TYPE_FLOAT, lexer.TOKEN_TYPE_DOUBLE,
				lexer.TOKEN_TYPE_BOOL, lexer.TOKEN_TYPE_CHAR, lexer.TOKEN_TYPE_STRING,
				lexer.TOKEN_TYPE_VOID:
				isTypeKeyword = true
			}

			if p.curTok.Type == lexer.TOKEN_IDENT || isTypeKeyword {
				savedCurTok := p.curTok
				savedPeekTok := p.peekTok

				// 先尝试解析为字段声明（name: type;）
				if p.curTok.Type == lexer.TOKEN_IDENT {
					if field := p.parseFieldDeclarationIterative(); field != nil {
						p.log("解析完成字段声明：%s", field.String())
						stmt.Fields = append(stmt.Fields, field)
						continue
					}
				}

				// 恢复 token 位置，尝试解析为方法
				p.curTok = savedCurTok
				p.peekTok = savedPeekTok

				if method := p.parseMethodStatementIterative(); method != nil {
					p.log("解析完成方法声明：%s", method.String())
					stmt.Methods = append(stmt.Methods, method)
					continue
				}

				// 恢复 token 位置，尝试解析为构造函数
				p.curTok = savedCurTok
				p.peekTok = savedPeekTok

				if p.curTok.Type == lexer.TOKEN_IDENT && p.curTok.Value == stmt.Name {
					p.log("开始解析构造函数")
					constructor := p.parseConstructorStatementIterative()
					if constructor != nil {
						p.log("解析完成构造函数")
						stmt.Constructors = append(stmt.Constructors, constructor)
						continue
					}
				}

				// 如果都不匹配，跳过 token
				p.log("跳过 token: %s", lexer.TokenTypeToString(p.curTok.Type))
				p.nextToken()
			} else if p.curTok.Type == lexer.TOKEN_FUNC {
				// fn 关键字开头的方法: fn Name(params) [ReturnType] { ... }
				savedCurTok := p.curTok
				savedPeekTok := p.peekTok
				p.nextToken() // 跳过 fn

				// 下一个 token 必须是方法名 IDENT
				if p.curTok.Type != lexer.TOKEN_IDENT {
					p.curTok = savedCurTok
					p.peekTok = savedPeekTok
					p.log("跳过 token: %s (fn 后不是方法名)", lexer.TokenTypeToString(p.curTok.Type))
					p.nextToken()
					continue
				}

				method := &ast.MethodStatement{
					Params: []*ast.Param{},
					Body:   []ast.Statement{},
					Pos: ast.Position{
						Line:   p.curTok.Line,
						Column: p.curTok.Column,
						File:   p.file,
					},
				}
				method.Name = p.curTok.Value
				p.log("解析 fn 方法名：%s", method.Name)
				p.nextToken()

				// 期望左括号
				if p.curTok.Type != lexer.TOKEN_LPAREN {
					p.curTok = savedCurTok
					p.peekTok = savedPeekTok
					p.log("跳过 token: %s (方法名后不是 ()", lexer.TokenTypeToString(p.curTok.Type))
					p.nextToken()
					continue
				}
				p.nextToken() // 跳过 (

				// 解析参数（支持指针类型: Type* name / *Type name）
				for p.curTok.Type != lexer.TOKEN_RPAREN && p.curTok.Type != lexer.TOKEN_EOF {
					isParamTypeKeyword := false
					switch p.curTok.Type {
					case lexer.TOKEN_TYPE_INT, lexer.TOKEN_TYPE_FLOAT, lexer.TOKEN_TYPE_DOUBLE,
						lexer.TOKEN_TYPE_BOOL, lexer.TOKEN_TYPE_CHAR, lexer.TOKEN_TYPE_STRING,
						lexer.TOKEN_TYPE_VOID:
						isParamTypeKeyword = true
					}

					param := &ast.Param{}
					// *Type name 形式
					if p.curTok.Type == lexer.TOKEN_MULTIPLY {
						param.Type = "*" + p.peekTok.Value
						p.log("跳过指针参数类型：%s", param.Type)
						p.nextToken()
						p.nextToken()
						if p.curTok.Type != lexer.TOKEN_IDENT {
							p.error(fmt.Sprintf("expected parameter name, got %s", lexer.TokenTypeToString(p.curTok.Type)))
							break
						}
						param.Name = p.curTok.Value
						p.nextToken()
						method.Params = append(method.Params, param)
						if p.curTok.Type == lexer.TOKEN_COMMA {
							p.nextToken()
						}
						continue
					}

					if p.curTok.Type != lexer.TOKEN_IDENT && !isParamTypeKeyword {
						break
					}
					param.Type = p.curTok.Value
					p.nextToken()
					// Type* name 形式（指针后缀）
					if p.curTok.Type == lexer.TOKEN_MULTIPLY {
						param.Type += "*"
						p.log("指针后缀：%s", param.Type)
						p.nextToken()
					}
					if p.curTok.Type == lexer.TOKEN_QUESTION {
						param.Nullable = true
						p.nextToken()
					}
					if p.curTok.Type != lexer.TOKEN_IDENT {
						p.error(fmt.Sprintf("expected parameter name, got %s", lexer.TokenTypeToString(p.curTok.Type)))
						break
					}
					param.Name = p.curTok.Value
					p.nextToken()
					method.Params = append(method.Params, param)
					if p.curTok.Type == lexer.TOKEN_COMMA {
						p.nextToken()
					}
				}
				if p.curTok.Type == lexer.TOKEN_RPAREN {
					p.nextToken() // 跳过 )
				}

				// 解析可选返回类型：可以是 IDENT 或类型关键字（int, void, f64 等）
				isRetTypeKeyword := false
				switch p.curTok.Type {
				case lexer.TOKEN_TYPE_INT, lexer.TOKEN_TYPE_FLOAT, lexer.TOKEN_TYPE_DOUBLE,
					lexer.TOKEN_TYPE_BOOL, lexer.TOKEN_TYPE_CHAR, lexer.TOKEN_TYPE_STRING,
					lexer.TOKEN_TYPE_VOID:
					isRetTypeKeyword = true
				}
				returnPointer := false
				if p.curTok.Type == lexer.TOKEN_MULTIPLY {
					// 不常见，但兼容处理
					returnPointer = true
					p.nextToken()
					isRetTypeKeyword = true
				}
				if p.curTok.Type == lexer.TOKEN_IDENT || isRetTypeKeyword {
					retType := p.curTok.Value
					p.nextToken()
					if p.curTok.Type == lexer.TOKEN_MULTIPLY {
						retPointerSuffix := true
						_ = retPointerSuffix
						retType = retType + "*"
						p.nextToken()
					}
					if returnPointer {
						retType = "*" + retType
					}
					method.ReturnType = retType
					p.log("解析 fn 方法返回类型：%s", method.ReturnType)
				}

				// 解析方法体 { ... }
				if p.curTok.Type == lexer.TOKEN_LBRACE {
					p.nextToken() // 跳过 {
					for p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
						bodyStmt := p.parseStatementIterative()
						if bodyStmt != nil {
							method.Body = append(method.Body, bodyStmt)
						}
						// 如果 parseStatementIterative 没有推进到下一个有效位置，手动前进
						// 避免 RBRACE 前死循环
						if p.curTok.Type != lexer.TOKEN_RBRACE &&
							p.curTok.Type != lexer.TOKEN_EOF {
							// 检查是否还在同一个 token 上没动（防止死循环）
							// 简单策略：若下一个解析仍是该 token，则跳过它（这里仅通过查看 peek 是否 == curTok 判断不了）
							// 我们只在 parseStatementIterative 返回 nil 时手动推进
							if bodyStmt == nil {
								p.nextToken()
							}
						}
					}
					if p.curTok.Type == lexer.TOKEN_RBRACE {
						p.nextToken() // 跳过 }
					}
					stmt.Methods = append(stmt.Methods, method)
					p.log("解析完成 fn 方法声明：%s", method.String())
				} else if p.curTok.Type == lexer.TOKEN_SEMICOLON {
					// 接口方法（仅有声明，无体）
					p.nextToken()
					stmt.Methods = append(stmt.Methods, method)
					p.log("解析完成 fn 接口方法声明（无体）：%s", method.String())
				} else {
					// 不是 fn 方法声明：恢复
					p.curTok = savedCurTok
					p.peekTok = savedPeekTok
					p.log("跳过 token: %s (fn 声明后无 { 或 ;)", lexer.TokenTypeToString(p.curTok.Type))
					p.nextToken()
				}
			} else if p.curTok.Type == lexer.TOKEN_CONSTRUCTOR {
				// constructor 关键字开头的构造函数
				savedCurTok := p.curTok
				savedPeekTok := p.peekTok
				p.nextToken() // 跳过 constructor

				// 期望构造函数名（类名），也支持直接 constructor(...)（文档风格）
				if (p.curTok.Type == lexer.TOKEN_IDENT && p.curTok.Value == stmt.Name) ||
					p.curTok.Type == lexer.TOKEN_LPAREN {
					p.log("开始解析构造函数")
					constructor := p.parseConstructorStatementIterative()
					if constructor != nil {
						p.log("解析完成构造函数")
						stmt.Constructors = append(stmt.Constructors, constructor)
					} else {
						p.curTok = savedCurTok
						p.peekTok = savedPeekTok
						p.log("跳过 token: %s", lexer.TokenTypeToString(p.curTok.Type))
						p.nextToken()
					}
				} else {
					p.curTok = savedCurTok
					p.peekTok = savedPeekTok
					p.log("跳过 token: %s", lexer.TokenTypeToString(p.curTok.Type))
					p.nextToken()
				}
			} else if p.curTok.Type == lexer.TOKEN_SEMICOLON {
				p.log("跳过分号")
				p.nextToken()
			} else {
				p.log("跳过 token: %s", lexer.TokenTypeToString(p.curTok.Type))
				p.nextToken()
			}
		}
		p.log("解析完成类体")
	}
	p.log("类解析完成：%s, 字段数：%d, 方法数：%d, 构造函数数：%d", stmt.Name, len(stmt.Fields), len(stmt.Methods), len(stmt.Constructors))
	return stmt
}

// parseInterfaceStatementIterative 迭代解析接口定义
func (p *Parser) parseInterfaceStatementIterative() *ast.InterfaceStatement {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	stmt := &ast.InterfaceStatement{
		Methods: []*ast.MethodStatement{},
		Pos:     pos,
	}
	p.nextToken()
	if p.curTok.Type == lexer.TOKEN_IDENT {
		stmt.Name = p.curTok.Value
		p.nextToken()
	}
	if p.curTok.Type == lexer.TOKEN_LBRACE {
		p.nextToken()
		for p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
			// 检查是否是类型关键字开头（接口方法语法：returnType methodName(); 或 fn methodName() returnType;）
			isTypeKeyword := false
			switch p.curTok.Type {
			case lexer.TOKEN_TYPE_INT, lexer.TOKEN_TYPE_FLOAT, lexer.TOKEN_TYPE_DOUBLE,
				lexer.TOKEN_TYPE_BOOL, lexer.TOKEN_TYPE_CHAR, lexer.TOKEN_TYPE_STRING,
				lexer.TOKEN_TYPE_VOID:
				isTypeKeyword = true
			}

			if p.curTok.Type == lexer.TOKEN_FUNC {
				// 接口方法使用 fn methodName(params) [retType]; 语法
				savedCurTok := p.curTok
				savedPeekTok := p.peekTok
				p.nextToken() // 跳过 fn

				if p.curTok.Type != lexer.TOKEN_IDENT {
					// 不是方法名：恢复并跳过
					p.curTok = savedCurTok
					p.peekTok = savedPeekTok
					p.nextToken()
					continue
				}

				method := &ast.MethodStatement{
					Params: []*ast.Param{},
					Body:   []ast.Statement{},
					Pos: ast.Position{
						Line:   p.curTok.Line,
						Column: p.curTok.Column,
						File:   p.file,
					},
				}
				method.Name = p.curTok.Value
				p.log("接口 fn 方法名：%s", method.Name)
				p.nextToken()

				if p.curTok.Type != lexer.TOKEN_LPAREN {
					p.curTok = savedCurTok
					p.peekTok = savedPeekTok
					p.nextToken()
					continue
				}
				p.nextToken() // 跳过 (

				for p.curTok.Type != lexer.TOKEN_RPAREN && p.curTok.Type != lexer.TOKEN_EOF {
					isParamTypeKeyword := false
					switch p.curTok.Type {
					case lexer.TOKEN_TYPE_INT, lexer.TOKEN_TYPE_FLOAT, lexer.TOKEN_TYPE_DOUBLE,
						lexer.TOKEN_TYPE_BOOL, lexer.TOKEN_TYPE_CHAR, lexer.TOKEN_TYPE_STRING,
						lexer.TOKEN_TYPE_VOID:
						isParamTypeKeyword = true
					}
					if p.curTok.Type != lexer.TOKEN_IDENT && !isParamTypeKeyword {
						break
					}
					param := &ast.Param{}
					param.Type = p.curTok.Value
					p.nextToken()
					if p.curTok.Type == lexer.TOKEN_QUESTION {
						param.Nullable = true
						p.nextToken()
					}
					if p.curTok.Type != lexer.TOKEN_IDENT {
						break
					}
					param.Name = p.curTok.Value
					p.nextToken()
					method.Params = append(method.Params, param)
					if p.curTok.Type == lexer.TOKEN_COMMA {
						p.nextToken()
					}
				}
				if p.curTok.Type == lexer.TOKEN_RPAREN {
					p.nextToken() // 跳过 )
				}

				// 解析可选返回类型
				isRetTypeKeyword := false
				switch p.curTok.Type {
				case lexer.TOKEN_TYPE_INT, lexer.TOKEN_TYPE_FLOAT, lexer.TOKEN_TYPE_DOUBLE,
					lexer.TOKEN_TYPE_BOOL, lexer.TOKEN_TYPE_CHAR, lexer.TOKEN_TYPE_STRING,
					lexer.TOKEN_TYPE_VOID:
					isRetTypeKeyword = true
				}
				if p.curTok.Type == lexer.TOKEN_IDENT || isRetTypeKeyword {
					method.ReturnType = p.curTok.Value
					p.nextToken()
					if p.curTok.Type == lexer.TOKEN_MULTIPLY {
						method.ReturnType = method.ReturnType + "*"
						p.nextToken()
					}
				}
				// 接口方法：需要分号结尾
				if p.curTok.Type == lexer.TOKEN_SEMICOLON {
					p.nextToken()
					stmt.Methods = append(stmt.Methods, method)
					p.log("解析完成接口 fn 方法声明：%s", method.String())
				} else if p.curTok.Type == lexer.TOKEN_RBRACE {
					// 无分号，但到了接口结尾：也算合法（分号可选）
					stmt.Methods = append(stmt.Methods, method)
					p.log("解析完成接口 fn 方法声明（无分号）：%s", method.String())
				} else {
					// 失败：恢复
					p.curTok = savedCurTok
					p.peekTok = savedPeekTok
					p.nextToken()
				}
			} else if p.curTok.Type == lexer.TOKEN_IDENT || isTypeKeyword {
				savedCurTok := p.curTok
				savedPeekTok := p.peekTok

				if method := p.parseMethodStatementIterative(); method != nil {
					stmt.Methods = append(stmt.Methods, method)
					continue
				}

				// 如果解析失败，恢复 token 位置并跳过一个 token 避免死循环
				p.curTok = savedCurTok
				p.peekTok = savedPeekTok
				p.nextToken()
			} else if p.curTok.Type == lexer.TOKEN_SEMICOLON {
				p.nextToken()
			} else {
				p.nextToken()
			}
		}
		if p.curTok.Type == lexer.TOKEN_RBRACE {
			p.nextToken()
		}
	}
	return stmt
}

// parseStructStatementIterative 迭代解析结构体定义
func (p *Parser) parseStructStatementIterative() *ast.StructStatement {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	stmt := &ast.StructStatement{
		Fields: make([]*ast.FieldDeclaration, 0, 16),
		Pos:    pos,
	}

	// 解析属性注解（如 #[packed], #[aligned(16)] 等）
	attrs := p.parseAttributes()
	if attrs != nil {
		stmt.Attributes = attrs
		p.log("结构体属性：%s, 属性数：%d", stmt.Name, len(attrs))
	}

	p.nextToken()
	if p.curTok.Type == lexer.TOKEN_IDENT {
		stmt.Name = p.curTok.Value
		p.nextToken()
	}
	// 解析泛型参数（如果存在）
	if p.curTok.Type == lexer.TOKEN_LT {
		p.nextToken()
		for p.curTok.Type == lexer.TOKEN_IDENT {
			stmt.TypeParams = append(stmt.TypeParams, &ast.TypeParameter{Name: p.curTok.Value})
			p.nextToken()
			if p.curTok.Type == lexer.TOKEN_COMMA {
				p.nextToken()
			} else if p.curTok.Type == lexer.TOKEN_GT {
				break
			}
		}
		if p.curTok.Type == lexer.TOKEN_GT {
			p.nextToken()
		}
		stmt.Generic = true // 标记为泛型结构体
	}
	if p.curTok.Type == lexer.TOKEN_LBRACE {
		p.nextToken()
		for p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
			if field := p.parseFieldDeclarationIterative(); field != nil {
				stmt.Fields = append(stmt.Fields, field)
			} else if p.curTok.Type == lexer.TOKEN_SEMICOLON {
				p.nextToken()
			} else {
				// 跳过无法识别的 token，继续解析
				p.nextToken()
			}
		}
	}
	if p.curTok.Type == lexer.TOKEN_RBRACE {
		p.nextToken()
	}
	p.log("结构体解析完成：%s, 字段数：%d", stmt.Name, len(stmt.Fields))
	return stmt
}

// parseTypeStatementIterative 迭代解析 type 语句
// 语法: type Name = UnderlyingType
// 或:    type Name[T] = UnderlyingType (泛型)
// 或:    type Name func(params) returnType (函数类型)
func (p *Parser) parseTypeStatementIterative() *ast.TypeAliasStatement {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	stmt := &ast.TypeAliasStatement{
		Pos: pos,
	}

	p.nextToken()
	if p.curTok.Type == lexer.TOKEN_IDENT {
		stmt.Name = p.curTok.Value
		p.nextToken()
	} else {
		p.error(fmt.Sprintf("expected type name after 'type', got %s", lexer.TokenTypeToString(p.curTok.Type)))
		return nil
	}

	// 解析泛型参数（如果存在）
	if p.curTok.Type == lexer.TOKEN_LT {
		p.nextToken()
		for p.curTok.Type == lexer.TOKEN_IDENT {
			stmt.TypeParams = append(stmt.TypeParams, &ast.TypeParameter{Name: p.curTok.Value})
			p.nextToken()
			if p.curTok.Type == lexer.TOKEN_COMMA {
				p.nextToken()
			} else if p.curTok.Type == lexer.TOKEN_GT {
				break
			}
		}
		if p.curTok.Type == lexer.TOKEN_GT {
			p.nextToken()
		}
		stmt.Generic = true
	}

	// 解析 '=' 或直接解析函数类型
	if p.curTok.Type == lexer.TOKEN_ASSIGN {
		p.nextToken()
		// 解析底层类型
		stmt.UnderlyingType = p.parseTypeString()
	} else if p.curTok.Type == lexer.TOKEN_IDENT && p.curTok.Value == "func" {
		// 函数类型: type Name func(params) returnType
		stmt.IsFuncType = true
		p.nextToken() // 跳过 'func'

		// 解析参数列表
		if p.curTok.Type == lexer.TOKEN_LPAREN {
			p.nextToken()
			for p.curTok.Type != lexer.TOKEN_RPAREN && p.curTok.Type != lexer.TOKEN_EOF {
				param := p.parseTypeFuncParam()
				if param != nil {
					stmt.FuncParams = append(stmt.FuncParams, param)
				}
				if p.curTok.Type == lexer.TOKEN_COMMA {
					p.nextToken()
				}
			}
			if p.curTok.Type == lexer.TOKEN_RPAREN {
				p.nextToken()
			}
		}

		// 解析返回类型
		stmt.FuncReturnType = p.parseTypeString()
	} else {
		p.error(fmt.Sprintf("expected '=' or 'func' after type name, got %s", lexer.TokenTypeToString(p.curTok.Type)))
		return nil
	}

	if stmt.IsFuncType {
		p.log("函数类型别名解析完成：%s func(...) %s", stmt.Name, stmt.FuncReturnType)
	} else {
		p.log("类型别名解析完成：%s = %s (Generic=%v)", stmt.Name, stmt.UnderlyingType, stmt.Generic)
	}
	return stmt
}

// parseTypeFuncParam 解析函数类型参数
func (p *Parser) parseTypeFuncParam() *ast.TypeFuncParam {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	param := &ast.TypeFuncParam{
		Pos: pos,
	}

	// 解析类型
	param.Type = p.parseTypeString()

	// 检查是否可空
	if p.curTok.Type == lexer.TOKEN_QUESTION {
		param.Nullable = true
		p.nextToken()
	}

	return param
}

// parseTypeString 解析类型字符串（支持指针、数组等复合类型）
// greedyReturn=true：void(T...)R 记法在 ) 后贪婪吃返回类型（用于独立类型位置：type 别名、函数返回类型、泛型实参）
func (p *Parser) parseTypeString() string {
	return p.parseTypeStringImpl(true)
}

// parseTypeStringForDecl 用于"类型后跟名字"的上下文（变量声明、字段、函数参数）。
// void(T...)R 记法不吃尾部返回类型——因为返回类型与变量名均为 IDENT，无法区分。
// 需要带返回类型的函数指针在这些上下文请用 type 别名声明。
func (p *Parser) parseTypeStringForDecl() string {
	return p.parseTypeStringImpl(false)
}

func (p *Parser) parseTypeStringImpl(greedyReturn bool) string {
	var typeStr strings.Builder

	for {
		switch p.curTok.Type {
		case lexer.TOKEN_CONST:
			typeStr.WriteString("const ")
			p.nextToken()
		case lexer.TOKEN_IDENT, lexer.TOKEN_TYPE_INT, lexer.TOKEN_TYPE_FLOAT, lexer.TOKEN_TYPE_DOUBLE,
			lexer.TOKEN_TYPE_BOOL, lexer.TOKEN_TYPE_CHAR, lexer.TOKEN_TYPE_STRING, lexer.TOKEN_TYPE_VOID:
			// void(T...)R 签名记法：
			//   void()        - 完全不透明指针 (void*)
			//   void(T)       - 幻影类型化数据指针 (void*, 类型系统记 T)
			//   void(T1,T2)R  - 带签名函数指针 (R (*)(T1,T2))，仅 greedyReturn=true 时吃 R
			//   void(T1,T2)   - 无返回值函数指针 (void (*)(T1,T2))
			if p.curTok.Type == lexer.TOKEN_TYPE_VOID && p.peekTok.Type == lexer.TOKEN_LPAREN {
				typeStr.WriteString("void(")
				p.nextToken() // void -> (
				p.nextToken() // ( -> 首个参数或 )
				first := true
				for p.curTok.Type != lexer.TOKEN_RPAREN && p.curTok.Type != lexer.TOKEN_EOF {
					if !first {
						typeStr.WriteString(",")
						if p.curTok.Type == lexer.TOKEN_COMMA {
							p.nextToken()
						}
					}
					if p.curTok.Type == lexer.TOKEN_RPAREN {
						break
					}
					// 参数类型递归用 greedy=true（参数列表内类型独立）
					typeStr.WriteString(p.parseTypeString())
					first = false
				}
				if p.curTok.Type == lexer.TOKEN_RPAREN {
					typeStr.WriteString(")")
					p.nextToken()
				}
				// 返回类型：仅 greedyReturn=true 且 ) 后跟类型起始 token 时吃 → 函数指针；否则数据指针
				if greedyReturn && p.isIdentOrTypeToken(p.curTok.Type) {
					typeStr.WriteString(p.parseTypeString())
				}
				return typeStr.String()
			}
			typeStr.WriteString(p.curTok.Value)
			p.nextToken()
		case lexer.TOKEN_MULTIPLY:
			typeStr.WriteString("*")
			p.nextToken()
		case lexer.TOKEN_LBRACKET:
			p.nextToken()
			if p.curTok.Type == lexer.TOKEN_LITERAL_INT {
				typeStr.WriteString("[")
				typeStr.WriteString(p.curTok.Value)
				p.nextToken()
				if p.curTok.Type == lexer.TOKEN_RBRACKET {
					typeStr.WriteString("]")
					p.nextToken()
				}
			} else {
				typeStr.WriteString("[]")
				if p.curTok.Type == lexer.TOKEN_RBRACKET {
					p.nextToken()
				}
			}
		case lexer.TOKEN_LT:
			typeStr.WriteString("<")
			p.nextToken()
			first := true
			for p.curTok.Type != lexer.TOKEN_GT && p.curTok.Type != lexer.TOKEN_EOF {
				if !first {
					typeStr.WriteString(", ")
				}
				typeStr.WriteString(p.parseTypeString())
				first = false
				if p.curTok.Type == lexer.TOKEN_COMMA {
					p.nextToken()
				}
			}
			if p.curTok.Type == lexer.TOKEN_GT {
				typeStr.WriteString(">")
				p.nextToken()
			}
			// 泛型闭合后若跟随名称/逗号/右括号等终止符，说明类型已解析完
			switch p.curTok.Type {
			case lexer.TOKEN_IDENT, lexer.TOKEN_COMMA, lexer.TOKEN_RPAREN,
				lexer.TOKEN_SEMICOLON, lexer.TOKEN_EOF, lexer.TOKEN_LBRACE:
				return typeStr.String()
			}
		default:
			return typeStr.String()
		}
	}
}

// parseFieldDeclarationIterative 迭代解析字段声明
func (p *Parser) parseFieldDeclarationIterative() *ast.FieldDeclaration {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}

	if p.curTok.Type != lexer.TOKEN_IDENT {
		return nil
	}

	savedCurTok := p.curTok
	savedPeekTok := p.peekTok

	// 解析字段名
	fieldName := p.curTok.Value
	p.nextToken()

	// 检查下一个 token 是否是冒号（name: type 语法）
	// 冒号必须存在: 无冒号时不是字段声明(可能是方法/构造/其他), 回退
	if p.curTok.Type == lexer.TOKEN_COLON {
		p.nextToken()
	} else {
		p.curTok = savedCurTok
		p.peekTok = savedPeekTok
		return nil
	}

	// 解析类型（支持 [N]byte 数组语法和普通类型）
	typeName := ""
	typePrefix := ""

	// const 前缀（与 parseTypeString 一致）: const char* name
	if p.curTok.Type == lexer.TOKEN_CONST {
		typePrefix = "const "
		p.nextToken()
	}

	// 检查是否是数组类型 [N]type
	if p.curTok.Type == lexer.TOKEN_LBRACKET {
		p.nextToken()
		// 解析数组大小
		arraySize := ""
		if p.curTok.Type == lexer.TOKEN_LITERAL_INT || p.curTok.Type == lexer.TOKEN_IDENT {
			arraySize = p.curTok.Value
			p.nextToken()
		}
		if p.curTok.Type == lexer.TOKEN_RBRACKET {
			p.nextToken()
		}
		// 解析元素类型（byte 是 IDENT，不是关键字）
		elemType := ""
		isElemType := false
		switch p.curTok.Type {
		case lexer.TOKEN_TYPE_INT, lexer.TOKEN_TYPE_FLOAT, lexer.TOKEN_TYPE_DOUBLE,
			lexer.TOKEN_TYPE_BOOL, lexer.TOKEN_TYPE_CHAR, lexer.TOKEN_TYPE_STRING,
			lexer.TOKEN_TYPE_VOID:
			elemType = p.curTok.Value
			isElemType = true
		case lexer.TOKEN_IDENT:
			elemType = p.curTok.Value
			isElemType = true
		}
		if !isElemType {
			p.curTok = savedCurTok
			p.peekTok = savedPeekTok
			return nil
		}
		p.nextToken()
		typeName = typePrefix + "[" + arraySize + "]" + elemType
	} else {
		// 普通类型
		isTypeKeyword := false
		switch p.curTok.Type {
		case lexer.TOKEN_TYPE_INT, lexer.TOKEN_TYPE_FLOAT, lexer.TOKEN_TYPE_DOUBLE,
			lexer.TOKEN_TYPE_BOOL, lexer.TOKEN_TYPE_CHAR, lexer.TOKEN_TYPE_STRING,
			lexer.TOKEN_TYPE_VOID, lexer.TOKEN_IDENT:
			isTypeKeyword = true
		}

		if !isTypeKeyword {
			p.curTok = savedCurTok
			p.peekTok = savedPeekTok
			return nil
		}

		typeName = typePrefix + p.curTok.Value
		p.nextToken()

		// 处理指针后缀（如 ListNode* → "ListNode*"）
		if p.curTok.Type == lexer.TOKEN_MULTIPLY {
			typeName = typeName + "*"
			p.nextToken()
		}
	}

	// 字段结束后可以是分号、逗号、} 或下一个字段名
	// 不强制要求分隔符

	// 检查位域语法: 类型后 : N 位宽
	bitWidth := 0
	if p.curTok.Type == lexer.TOKEN_COLON {
		p.nextToken() // 跳过 :
		if p.curTok.Type == lexer.TOKEN_LITERAL_INT {
			widthVal, err := strconv.Atoi(p.curTok.Value)
			if err == nil && widthVal > 0 {
				bitWidth = widthVal
			}
			p.nextToken()
		} else {
			p.error("bitfield width must be a positive integer")
		}
	}

	// 字段结束后可以是分号、逗号、} 或下一个字段名
	// 不强制要求分隔符
	if p.curTok.Type == lexer.TOKEN_SEMICOLON || p.curTok.Type == lexer.TOKEN_COMMA {
		p.nextToken()
	}

	field := &ast.FieldDeclaration{
		Name:     fieldName,
		Type:     typeName,
		Nullable: false,
		BitWidth: bitWidth,
		Pos:      pos,
	}
	return field
}

// parseMethodStatementIterative 迭代解析方法定义
func (p *Parser) parseMethodStatementIterative() *ast.MethodStatement {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}

	savedCurTok := p.curTok
	savedPeekTok := p.peekTok

	method := &ast.MethodStatement{
		Params: []*ast.Param{},
		Body:   []ast.Statement{},
		Pos:    pos,
	}
	p.log("开始解析方法，当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)

	// 检查是否是类型关键字（string, int, void 等）作为返回类型
	isTypeKeyword := false
	switch p.curTok.Type {
	case lexer.TOKEN_TYPE_INT, lexer.TOKEN_TYPE_FLOAT, lexer.TOKEN_TYPE_DOUBLE,
		lexer.TOKEN_TYPE_BOOL, lexer.TOKEN_TYPE_CHAR, lexer.TOKEN_TYPE_STRING,
		lexer.TOKEN_TYPE_VOID:
		isTypeKeyword = true
	}

	if p.curTok.Type != lexer.TOKEN_IDENT && !isTypeKeyword && p.curTok.Type != lexer.TOKEN_MULTIPLY {
		p.log("不是方法声明，返回 nil")
		p.curTok = savedCurTok
		p.peekTok = savedPeekTok
		return nil
	}

	// 文档风格: fn name(params) ReturnType (方法名前置, 返回类型后置)
	isPointer := false
	if p.curTok.Type == lexer.TOKEN_IDENT && p.peekTok.Type == lexer.TOKEN_LPAREN {
		method.Name = p.curTok.Value
		p.log("解析方法名（前置风格）：%s", method.Name)
		p.nextToken() // 到 (
		p.nextToken() // 跳过 (
		p.log("当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
		goto parseMethodParams
	}

	// 处理指针前缀 (*Type)
	if p.curTok.Type == lexer.TOKEN_MULTIPLY {
		isPointer = true
		p.nextToken()
	}
	method.ReturnType = p.curTok.Value
	p.log("解析返回类型：%s", p.curTok.Value)
	p.nextToken()
	// 处理指针后缀 (Type*)
	if p.curTok.Type == lexer.TOKEN_MULTIPLY {
		isPointer = true
		p.nextToken()
	}
	if isPointer {
		method.ReturnType = method.ReturnType + "*"
	}
	p.log("当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
	if p.curTok.Type == lexer.TOKEN_QUESTION {
		p.log("跳过 QUESTION token")
		p.nextToken()
		p.log("当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
	}
	if p.curTok.Type != lexer.TOKEN_IDENT {
		p.log("不是方法声明，返回 nil")
		p.curTok = savedCurTok
		p.peekTok = savedPeekTok
		return nil
	}
	method.Name = p.curTok.Value
	p.log("解析方法名：%s", p.curTok.Value)
	p.log("跳过方法名前，curTok: %s, 值：%s, peekTok: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value, lexer.TokenTypeToString(p.peekTok.Type), p.peekTok.Value)
	p.nextToken()
	p.log("跳过方法名后，当前 token: %s, 值：%s, peekTok: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value, lexer.TokenTypeToString(p.peekTok.Type), p.peekTok.Value)
	if p.curTok.Type != lexer.TOKEN_LPAREN {
		if p.curTok.Type == lexer.TOKEN_RPAREN {
			p.log("当前 token 是 RPAREN，跳过它")
			p.nextToken()
			p.log("跳过 RPAREN 后，当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
			if p.curTok.Type == lexer.TOKEN_LBRACE {
				p.log("发现左大括号，这是一个没有参数的方法")
				goto parseMethodBody
			}
		} else if p.peekTok.Type == lexer.TOKEN_LPAREN {
			p.log("当前 token 不是 LPAREN，但 peekTok 是，前进一个 token")
			p.nextToken()
		} else {
			p.log("不是方法声明，返回 nil")
			p.curTok = savedCurTok
			p.peekTok = savedPeekTok
			return nil
		}
	}
	p.log("跳过 LPAREN token")
	p.nextToken()
	p.log("当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
parseMethodParams:
	if p.curTok.Type != lexer.TOKEN_RPAREN {
		for p.curTok.Type != lexer.TOKEN_RPAREN {
			p.log("解析参数，当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)

			// 检查是否是类型关键字
			isParamTypeKeyword := false
			switch p.curTok.Type {
			case lexer.TOKEN_TYPE_INT, lexer.TOKEN_TYPE_FLOAT, lexer.TOKEN_TYPE_DOUBLE,
				lexer.TOKEN_TYPE_BOOL, lexer.TOKEN_TYPE_CHAR, lexer.TOKEN_TYPE_STRING,
				lexer.TOKEN_TYPE_VOID:
				isParamTypeKeyword = true
			}

			// 支持指针类型参数: Type* name / *Type name / char* name
			param := &ast.Param{}
			if p.curTok.Type == lexer.TOKEN_MULTIPLY {
				// *Type name 形式
				param.Type = "*" + p.peekTok.Value
				p.log("跳过指针参数类型：%s", param.Type)
				p.nextToken()
				p.nextToken()
				if p.curTok.Type != lexer.TOKEN_IDENT {
					p.error(fmt.Sprintf("expected parameter name, got %s", lexer.TokenTypeToString(p.curTok.Type)))
					p.curTok = savedCurTok
					p.peekTok = savedPeekTok
					return nil
				}
				param.Name = p.curTok.Value
				p.nextToken()
				method.Params = append(method.Params, param)
				if p.curTok.Type == lexer.TOKEN_COMMA {
					p.nextToken()
				}
				continue
			}

			if p.curTok.Type != lexer.TOKEN_IDENT && !isParamTypeKeyword {
				p.log("不是方法声明，返回 nil")
				p.curTok = savedCurTok
				p.peekTok = savedPeekTok
				return nil
			}
			param.Type = p.curTok.Value
			p.log("跳过参数类型：%s", p.curTok.Value)
			p.nextToken()
			p.log("当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
			// Type* name 形式 (指针后缀)
			if p.curTok.Type == lexer.TOKEN_MULTIPLY {
				param.Type += "*"
				p.log("指针后缀：%s", param.Type)
				p.nextToken()
				p.log("当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
			}
			if p.curTok.Type == lexer.TOKEN_QUESTION {
				param.Nullable = true
				p.log("跳过 QUESTION token")
				p.nextToken()
				p.log("当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
			}
			if p.curTok.Type != lexer.TOKEN_IDENT {
				p.error(fmt.Sprintf("expected parameter name, got %s", lexer.TokenTypeToString(p.curTok.Type)))
				p.curTok = savedCurTok
				p.peekTok = savedPeekTok
				return nil
			}
			param.Name = p.curTok.Value
			p.log("跳过参数名：%s", p.curTok.Value)
			p.nextToken()
			p.log("当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
			method.Params = append(method.Params, param)
			if p.curTok.Type == lexer.TOKEN_COMMA {
				p.log("跳过 COMMA token")
				p.nextToken()
				p.log("当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
			}
		}
	}
	if p.curTok.Type == lexer.TOKEN_RPAREN {
		p.log("跳过 RPAREN token")
		p.nextToken()
		p.log("当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
	} else {
		p.error(fmt.Sprintf("expected ), got %s", lexer.TokenTypeToString(p.curTok.Type)))
		p.log("不是方法声明，返回 nil")
		p.curTok = savedCurTok
		p.peekTok = savedPeekTok
		return nil
	}

	// 文档风格: 返回类型在参数后 (fn name(params) ReturnType / fn name(params) -> ReturnType)
	if method.ReturnType == "" {
		if p.curTok.Type == lexer.TOKEN_ARROW {
			p.log("跳过返回类型箭头 ->")
			p.nextToken()
		}
		retIsTypeKeyword := false
		switch p.curTok.Type {
		case lexer.TOKEN_TYPE_INT, lexer.TOKEN_TYPE_FLOAT, lexer.TOKEN_TYPE_DOUBLE,
			lexer.TOKEN_TYPE_BOOL, lexer.TOKEN_TYPE_CHAR, lexer.TOKEN_TYPE_STRING,
			lexer.TOKEN_TYPE_VOID:
			retIsTypeKeyword = true
		}
		if p.curTok.Type == lexer.TOKEN_IDENT || retIsTypeKeyword || p.curTok.Type == lexer.TOKEN_MULTIPLY {
			method.ReturnType = p.curTok.Value
			p.log("解析返回类型（后置）：%s", method.ReturnType)
			p.nextToken()
			if p.curTok.Type == lexer.TOKEN_MULTIPLY {
				method.ReturnType += "*"
				p.nextToken()
			}
		}
	}

parseMethodBody:
	p.log("解析方法体或分号，当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
	if p.curTok.Type == lexer.TOKEN_SEMICOLON {
		p.log("接口方法声明，跳过分号")
		p.nextToken()
	} else if p.curTok.Type == lexer.TOKEN_LBRACE {
		p.log("跳过 LBRACE token")
		p.nextToken()
		p.log("当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
		for p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
			bodyStmt := p.parseStatementIterative()
			if bodyStmt != nil {
				method.Body = append(method.Body, bodyStmt)
			}
			if p.curTok.Type != lexer.TOKEN_RBRACE && bodyStmt == nil {
				p.nextToken()
			}
		}
		if p.curTok.Type == lexer.TOKEN_RBRACE {
			p.log("跳过 RBRACE token")
			p.nextToken()
			p.log("当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
		} else {
			p.error(fmt.Sprintf("expected }, got %s", lexer.TokenTypeToString(p.curTok.Type)))
		}
	} else {
		p.log("不是方法声明，返回 nil")
		p.curTok = savedCurTok
		p.peekTok = savedPeekTok
		return nil
	}

	p.log("方法解析完成：%s", method.Name)
	return method
}

// parseConstructorStatementIterative 迭代解析构造函数
func (p *Parser) parseConstructorStatementIterative() *ast.ConstructorStatement {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	constructor := &ast.ConstructorStatement{
		Params: []*ast.Param{},
		Body:   []ast.Statement{},
		Pos:    pos,
	}
	// 两种形式: constructor ClassName(params) / constructor(params)
	if p.curTok.Type == lexer.TOKEN_IDENT {
		constructorName := p.curTok.Value
		p.log("解析构造函数名：%s", constructorName)
		p.nextToken()
		if p.curTok.Type == lexer.TOKEN_LPAREN {
			p.log("跳过 LPAREN token")
			p.nextToken()
			p.log("当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
		}
	} else if p.curTok.Type == lexer.TOKEN_LPAREN {
		// 文档风格 constructor(...): 无显式构造名, 直接解析参数
		p.log("解析构造函数参数（无构造名形式）")
		p.nextToken()
		p.log("当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
	} else {
		p.log("不是构造函数声明，返回 nil")
		return nil
	}
	{
		for p.curTok.Type != lexer.TOKEN_RPAREN {
			p.log("解析参数，当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)

			isTypeKeyword := false
			switch p.curTok.Type {
			case lexer.TOKEN_TYPE_INT, lexer.TOKEN_TYPE_FLOAT, lexer.TOKEN_TYPE_DOUBLE,
				lexer.TOKEN_TYPE_BOOL, lexer.TOKEN_TYPE_CHAR, lexer.TOKEN_TYPE_STRING,
				lexer.TOKEN_TYPE_VOID:
				isTypeKeyword = true
			}

			if p.curTok.Type != lexer.TOKEN_IDENT && !isTypeKeyword {
				p.error(fmt.Sprintf("expected type name, got %s", lexer.TokenTypeToString(p.curTok.Type)))
				break
			}
			param := &ast.Param{}
			param.Type = p.curTok.Value
			p.log("跳过参数类型：%s", p.curTok.Value)
			p.nextToken()
			p.log("当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
			// Type* name 形式（指针后缀）
			if p.curTok.Type == lexer.TOKEN_MULTIPLY {
				param.Type += "*"
				p.log("构造函数指针后缀：%s", param.Type)
				p.nextToken()
			}
			if p.curTok.Type == lexer.TOKEN_QUESTION {
				param.Nullable = true
				p.log("跳过 QUESTION token")
				p.nextToken()
				p.log("当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
			}
			if p.curTok.Type != lexer.TOKEN_IDENT {
				p.error(fmt.Sprintf("expected parameter name, got %s", lexer.TokenTypeToString(p.curTok.Type)))
				break
			}
			param.Name = p.curTok.Value
			p.log("跳过参数名：%s", p.curTok.Value)
			p.nextToken()
			p.log("当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
			constructor.Params = append(constructor.Params, param)
			if p.curTok.Type == lexer.TOKEN_COMMA {
				p.log("跳过 COMMA token")
				p.nextToken()
				p.log("当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
			}
		}
		if p.curTok.Type == lexer.TOKEN_RPAREN {
			p.log("跳过 RPAREN token")
			p.nextToken()
			p.log("当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
		} else {
			p.error(fmt.Sprintf("expected ), got %s", lexer.TokenTypeToString(p.curTok.Type)))
		}
	}
	if p.curTok.Type == lexer.TOKEN_LBRACE {
		p.log("跳过 LBRACE token")
		p.nextToken()
		p.log("当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
		for p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
			bodyStmt := p.parseStatementIterative()
			if bodyStmt != nil {
				constructor.Body = append(constructor.Body, bodyStmt)
			}
			if p.curTok.Type != lexer.TOKEN_RBRACE && bodyStmt == nil {
				p.nextToken()
			}
		}
		if p.curTok.Type == lexer.TOKEN_RBRACE {
			p.log("跳过 RBRACE token")
			p.nextToken()
			p.log("当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
		}
	} else {
		p.error(fmt.Sprintf("expected {, got %s", lexer.TokenTypeToString(p.curTok.Type)))
		return nil
	}
	p.log("构造函数解析完成")
	return constructor
}

// parseExpressionStatementIterative 迭代解析表达式语句
func (p *Parser) parseExpressionStatementIterative() *ast.ExpressionStatement {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	expr := p.parseExpressionIterative()
	if expr == nil {
		return nil
	}
	if p.curTok.Type == lexer.TOKEN_SEMICOLON {
		p.nextToken()
	}
	return &ast.ExpressionStatement{
		Expression: expr,
		Pos:        pos,
	}
}

// parseExpressionIterative 迭代解析表达式（使用栈替代递归）
func (p *Parser) parseExpressionIterative() ast.Expression {
	return p.parseBinaryExpressionIterative(0)
}

// parseBinaryExpressionIterative 迭代解析二元表达式（使用显式栈）
func (p *Parser) parseBinaryExpressionIterative(precedence int) ast.Expression {
	left := p.parsePrimaryExpressionIterative()

	// 如果解析失败，返回 nil
	if left == nil {
		return nil
	}

	// 使用迭代方式处理相同优先级的运算符
	for {
		opTok := p.curTok
		op := opTok.Type
		opPrecedence := p.precedence(op)

		// 如果没有运算符或优先级不够高，退出循环
		if opPrecedence == 0 || precedence >= opPrecedence {
			break
		}

		// 运算符出现在新行首（即左表达式结束 token 的下一行）视为语句边界，
		// 不继续跨行拼接表达式。例：`*hx = x` 下一行 `*hy = y` 不会被解析成 `x * hy`
		if opTok.Line != p.lastTokLine {
			break
		}

		prevTok := p.curTok
		p.nextToken()

		// 如果 nextToken 后 token 没变，说明无法解析，跳出循环避免死循环
		if p.curTok.Type == prevTok.Type && p.curTok.Value == prevTok.Value {
			break
		}

		// 解析右侧表达式
		var right ast.Expression
		if op == lexer.TOKEN_ASSIGN {
			// 赋值运算符是右结合的，使用 precedence - 1 以允许连续的赋值
			right = p.parseBinaryExpressionIterative(opPrecedence - 1)
		} else {
			// 其他运算符是左结合的，使用相同的优先级
			right = p.parseBinaryExpressionIterative(opPrecedence)
		}

		// 如果右侧解析失败，返回已解析的左侧
		if right == nil {
			return left
		}

		// 构建新的二元表达式
		// 优先用 token 自身的 Value(源码字面,如 "==","+","<=")
		// Kaula 0.1.0-alpha 历史上用 TokenTypeToString 拿类型名("EQ","PLUS"),导致
		// kaulafmt 输出 "path EQ \"/\"" 这种怪东西
		operator := opTok.Value
		if operator == "" {
			operator = lexer.TokenTypeToString(op)
		}
		left = &ast.BinaryExpression{
			Left:     left,
			Operator: operator,
			Right:    right,
			Pos: ast.Position{
				Line:   opTok.Line,
				Column: opTok.Column,
				File:   p.file,
			},
		}
	}

	return left
}

// precedences 运算符优先级表
var precedences = map[lexer.TokenType]int{
	lexer.TOKEN_ASSIGN:    1,
	lexer.TOKEN_OR:        2,
	lexer.TOKEN_AND:       3,
	lexer.TOKEN_EQ:        4,
	lexer.TOKEN_NE:        4,
	lexer.TOKEN_LT:        5,
	lexer.TOKEN_GT:        5,
	lexer.TOKEN_LE:        5,
	lexer.TOKEN_GE:        5,
	lexer.TOKEN_LSHIFT:    5,
	lexer.TOKEN_RSHIFT:    5,
	lexer.TOKEN_XOR:       4,
	lexer.TOKEN_PIPE:      4,
	lexer.TOKEN_AMPERSAND: 5,
	lexer.TOKEN_PLUS:      6,
	lexer.TOKEN_MINUS:     6,
	lexer.TOKEN_MULTIPLY:  7,
	lexer.TOKEN_DIVIDE:    7,
	lexer.TOKEN_MOD:       7,
}

// precedence 获取运算符优先级
func (p *Parser) precedence(tokenType lexer.TokenType) int {
	if prec, ok := precedences[tokenType]; ok {
		return prec
	}
	return 0
}

// parsePrimaryExpressionIterative 迭代解析基本表达式
func (p *Parser) parsePrimaryExpressionIterative() ast.Expression {
	switch p.curTok.Type {
	case lexer.TOKEN_IDENT:
		// 类型构造符语法：cstring("...") / str("...") 等价于 as<cstring>("...")
		if (p.curTok.Value == "cstring" || p.curTok.Value == "str") && p.peekTok.Type == lexer.TOKEN_LPAREN {
			return p.parseTypeConstructorCastIterative()
		}
		return p.parseIdentifierIterative()
	case lexer.TOKEN_LITERAL_INT:
		return p.parseIntegerLiteralIterative()
	case lexer.TOKEN_LITERAL_FLOAT:
		return p.parseFloatLiteralIterative()
	case lexer.TOKEN_LITERAL_CHAR:
		return p.parseCharLiteralIterative()
	case lexer.TOKEN_STRING:
		return p.parseStringLiteralIterative()
	case lexer.TOKEN_LPAREN:
		// 分组表达式 (expr)
		return p.parseGroupedExpressionIterative()
	case lexer.TOKEN_LBRACKET:
		// 数组字面量 [e1, e2, ...]（后缀索引 arr[i] 由 postfix 循环处理）
		return p.parseArrayLiteralIterative()
	case lexer.TOKEN_ATTRIBUTE:
		// 表达式级属性：#[name(args)] 作为特殊操作表达式
		// 这是统一的"表达式级特殊操作"语法，可扩展 asm/volatile/atomic/fence 等
		return p.parseAttributeExpressionIterative()
	case lexer.TOKEN_AMPERSAND:
		// 取地址操作符 &x
		p.nextToken()
		right := p.parsePrimaryExpressionIterative()
		if right == nil {
			p.error("& 后应该跟表达式")
			return nil
		}
		return &ast.UnaryExpression{
			Operator: "&",
			Right:    right,
		}
	case lexer.TOKEN_MINUS:
		// 一元负号 -x
		p.nextToken()
		right := p.parsePrimaryExpressionIterative()
		if right == nil {
			p.error("- 后应该跟表达式")
			return nil
		}
		return &ast.UnaryExpression{
			Operator: "-",
			Right:    right,
		}
	case lexer.TOKEN_MULTIPLY:
		// 一元解引用 *x
		p.nextToken()
		right := p.parsePrimaryExpressionIterative()
		if right == nil {
			p.error("* 后应该跟表达式")
			return nil
		}
		return &ast.UnaryExpression{
			Operator: "*",
			Right:    right,
		}
	case lexer.TOKEN_TILDE:
		// 按位取反 ~x
		p.nextToken()
		right := p.parsePrimaryExpressionIterative()
		if right == nil {
			p.error("~ 后应该跟表达式")
			return nil
		}
		return &ast.UnaryExpression{
			Operator: "~",
			Right:    right,
		}
	case lexer.TOKEN_NOT:
		// 逻辑非 !x
		p.nextToken()
		right := p.parsePrimaryExpressionIterative()
		if right == nil {
			p.error("! 后应该跟表达式")
			return nil
		}
		return &ast.UnaryExpression{
			Operator: "!",
			Right:    right,
		}
	case lexer.TOKEN_PREFIX_REF:
		p.nextToken()
		if p.curTok.Type == lexer.TOKEN_IDENT {
			ident := &ast.Identifier{
				Name:        p.curTok.Value,
				IsPrefixVar: true,
			}
			p.nextToken()
			return ident
		} else if p.curTok.Type == lexer.TOKEN_RBRACE {
			p.error("expected identifier after prefix ref, got RBRACE")
			return nil
		}
		p.error(fmt.Sprintf("expected identifier after prefix ref, got %s", lexer.TokenTypeToString(p.curTok.Type)))
		p.nextToken()
		return nil
	case lexer.TOKEN_PRINTLN:
		ident := &ast.Identifier{
			Name: p.curTok.Value,
		}
		p.nextToken()
		return p.parsePostfixChain(ident)
	case lexer.TOKEN_SELF:
		pos := ast.Position{
			Line:   p.curTok.Line,
			Column: p.curTok.Column,
			File:   p.file,
		}
		ident := &ast.Identifier{
			Name: p.curTok.Value,
			Pos:  pos,
		}
		p.nextToken()
		return p.parsePostfixChain(ident)
	case lexer.TOKEN_NULL:
		ident := &ast.Identifier{
			Name: p.curTok.Value,
		}
		p.nextToken()
		return p.parsePostfixChain(ident)
	case lexer.TOKEN_TRUE:
		pos := ast.Position{
			Line:   p.curTok.Line,
			Column: p.curTok.Column,
			File:   p.file,
		}
		p.nextToken()
		return &ast.BooleanLiteral{
			Value: true,
			Pos:   pos,
		}
	case lexer.TOKEN_FALSE:
		pos := ast.Position{
			Line:   p.curTok.Line,
			Column: p.curTok.Column,
			File:   p.file,
		}
		p.nextToken()
		return &ast.BooleanLiteral{
			Value: false,
			Pos:   pos,
		}
	case lexer.TOKEN_SIZEOF:
		if expr := p.parseSizeOfExpressionIterative(); expr != nil {
			return expr
		}
		return nil
	case lexer.TOKEN_ALIGNOF:
		if expr := p.parseAlignOfExpressionIterative(); expr != nil {
			return expr
		}
		return nil
	case lexer.TOKEN_OFFSETOF:
		if expr := p.parseOffsetOfExpressionIterative(); expr != nil {
			return expr
		}
		return nil
	case lexer.TOKEN_COMPTIME:
		if expr := p.parseComptimeExpressionIterative(); expr != nil {
			return expr
		}
		return nil
	case lexer.TOKEN_TYPE_NAME:
		if expr := p.parseTypeNameExpressionIterative(); expr != nil {
			return expr
		}
		return nil
	case lexer.TOKEN_FIELD_COUNT:
		if expr := p.parseFieldCountExpressionIterative(); expr != nil {
			return expr
		}
		return nil
	case lexer.TOKEN_FIELD_NAME:
		if expr := p.parseFieldNameExpressionIterative(); expr != nil {
			return expr
		}
		return nil
	case lexer.TOKEN_FIELD_TYPE:
		if expr := p.parseFieldTypeExpressionIterative(); expr != nil {
			return expr
		}
		return nil
	case lexer.TOKEN_TYPE_KIND:
		if expr := p.parseTypeKindExpressionIterative(); expr != nil {
			return expr
		}
		return nil
	case lexer.TOKEN_MATCH:
		return p.parseMatchExpressionIterative()
	case lexer.TOKEN_OBJECT:
		// object { name: value, ... } / object() 动态对象字面量
		return p.parseObjectLiteralExpressionIterative()
	case lexer.TOKEN_FUNC:
		// fn 后面跟 ( 是 lambda，跟 标识符是函数声明
		if p.peekTok.Type == lexer.TOKEN_LPAREN {
			return p.parseLambdaExpression()
		}
		// 否则忽略（函数声明不是表达式）
		return nil
	case lexer.TOKEN_LBRACE:
		return p.parseStructLiteralExpression()
	case lexer.TOKEN_AS:
		// as<T>(e) 类型转换表达式（唯一允许的强转形式，禁止裸 (T)e）
		return p.parseAsCastExpressionIterative()
	case lexer.TOKEN_RBRACE, lexer.TOKEN_DOT, lexer.TOKEN_ASSIGN, lexer.TOKEN_LT, lexer.TOKEN_GT, lexer.TOKEN_LSHIFT, lexer.TOKEN_RSHIFT, lexer.TOKEN_RPAREN, lexer.TOKEN_COMMA:
		return nil
	default:
		p.error(fmt.Sprintf("unexpected token: %s", lexer.TokenTypeToString(p.curTok.Type)))
		p.nextToken()
		return nil
	}
}

// parseStructLiteralExpression 解析结构体字面量 { .field = value, ... }
func (p *Parser) parseStructLiteralExpression() ast.Expression {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	stmt := &ast.StructLiteral{
		Fields: []ast.StructLiteralField{},
		Pos:    pos,
	}
	p.nextToken() // 跳过 {
	for p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
		if p.curTok.Type == lexer.TOKEN_DOT {
			p.nextToken()
			if p.curTok.Type == lexer.TOKEN_IDENT {
				field := ast.StructLiteralField{Name: p.curTok.Value}
				p.nextToken()
				if p.curTok.Type == lexer.TOKEN_ASSIGN {
					p.nextToken()
					field.Value = p.parseExpressionIterative()
				}
				stmt.Fields = append(stmt.Fields, field)
			}
		} else if p.curTok.Type == lexer.TOKEN_COMMA {
			p.nextToken()
		} else {
			p.nextToken()
		}
	}
	if p.curTok.Type == lexer.TOKEN_RBRACE {
		p.nextToken()
	}
	return stmt
}

// parsePostfixChain 解析后缀表达式链：成员访问(.field)、索引访问([i])、函数调用(())
// 从给定的初始表达式开始，循环处理后续的运算符直到不再匹配
func (p *Parser) parsePostfixChain(expr ast.Expression) ast.Expression {
	// 循环处理多级成员访问和索引访问（如 std.io.println 或 arr[i].field）
	for {
		if p.curTok.Type == lexer.TOKEN_DOT {
			p.nextToken()
			// 成员名可以是标识符、类型关键字、或带下划线的标识符（如 string_contains_string）
			if p.isIdentOrTypeToken(p.curTok.Type) || p.curTok.Type == lexer.TOKEN_PRINTLN || p.curTok.Type == lexer.TOKEN_LPAREN {
				// 如果点后面直接是 (，如 obj.(expr)，跳过
				if p.curTok.Type == lexer.TOKEN_LPAREN {
					break
				}
				memberName := p.curTok.Value
				memberPos := ast.Position{
					Line:   p.curTok.Line,
					Column: p.curTok.Column,
					File:   p.file,
				}
				p.nextToken()

				// 创建新的成员访问表达式
				expr = &ast.MemberAccessExpression{
					Object: expr,
					Member: memberName,
					Pos:    memberPos,
				}

				// 如果后面还有 LPAREN，说明是函数调用
				if p.curTok.Type == lexer.TOKEN_LPAREN {
					return p.parseCallExpressionIterative(expr)
				}
				// 泛型调用（obj.method<int>(...)），需避免与比较运算符混淆
				if p.curTok.Type == lexer.TOKEN_LT && p.looksLikeGenericArgs() {
					return p.parseCallExpressionIterative(expr)
				}
			} else {
				break
			}
		} else if p.curTok.Type == lexer.TOKEN_LBRACKET {
			// 处理索引访问（如 arr[i]）
			indexPos := ast.Position{
				Line:   p.curTok.Line,
				Column: p.curTok.Column,
				File:   p.file,
			}
			p.nextToken() // consume '['

			indexExpr := &ast.IndexExpression{
				Pos:    indexPos,
				Object: expr,
				Index:  p.parseExpressionIterative(),
			}

			if p.curTok.Type == lexer.TOKEN_RBRACKET {
				p.nextToken()
			} else {
				p.error(fmt.Sprintf("expected ']', got %s", lexer.TokenTypeToString(p.curTok.Type)))
			}

			expr = indexExpr
		} else {
			break
		}
	}

	// 检查是否是函数调用（含泛型调用 identity<int>(42)）
	if p.curTok.Type == lexer.TOKEN_LPAREN {
		return p.parseCallExpressionIterative(expr)
	}

	// 泛型调用：必须是 < 后跟类型名（避免与比较运算符 < 混淆）
	if p.curTok.Type == lexer.TOKEN_LT && p.looksLikeGenericArgs() {
		return p.parseCallExpressionIterative(expr)
	}

	return expr
}

// parseIdentifierIterative 迭代解析标识符（支持多级成员访问和索引访问）
func (p *Parser) parseIdentifierIterative() ast.Expression {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}

	expr := &ast.Identifier{
		Name: p.curTok.Value,
		Pos:  pos,
	}
	p.nextToken()

	return p.parsePostfixChain(expr)
}

// looksLikeGenericArgs 检查当前 < 是否是泛型类型参数列表
// 规则：
//   - < 后跟类型关键字（int/float/bool/char/string/void/double）→ 泛型
//   - < 后跟标识符（i64/u32/用户自定义类型），需进一步确认后跟 > ( 模式
//     以区分比较运算（a < b）
func (p *Parser) looksLikeGenericArgs() bool {
	// 类型关键字：直接判定为泛型参数
	if p.isTypeToken(p.peekTok.Type) {
		return true
	}
	// 标识符：投机扫描 < ... > ( 模式以区分比较运算
	if p.peekTok.Type == lexer.TOKEN_IDENT {
		return p.speculativeMatchGenericArgs()
	}
	return false
}

// speculativeMatchGenericArgs 投机扫描判断 < 后是否为 < typeargs > ( 模式。
// 保存 lexer 状态，向前扫描类型参数标记，遇到 > 后若跟 ( 则判定为泛型调用。
// 无论结果如何，恢复 lexer 状态，不影响后续解析。
func (p *Parser) speculativeMatchGenericArgs() bool {
	// 保存 lexer 状态（pos/line/column 是 Lexer 的全部可变状态）
	savedState := p.lexer.SaveState()
	defer func() {
		p.lexer.RestoreState(savedState)
	}()

	// 向前扫描类型参数：接受 IDENT、类型关键字、逗号、[]*等类型构造符
	// 直到遇到 > 或不匹配的 token
	for {
		tok := p.lexer.Next()
		switch tok.Type {
		case lexer.TOKEN_IDENT:
			// 标识符类型参数（i64, MyType 等），继续
		case lexer.TOKEN_TYPE_INT, lexer.TOKEN_TYPE_FLOAT, lexer.TOKEN_TYPE_DOUBLE,
			lexer.TOKEN_TYPE_BOOL, lexer.TOKEN_TYPE_CHAR, lexer.TOKEN_TYPE_STRING,
			lexer.TOKEN_TYPE_VOID:
			// 类型关键字，继续
		case lexer.TOKEN_LBRACKET, lexer.TOKEN_RBRACKET, lexer.TOKEN_COMMA, lexer.TOKEN_LT:
			// []T, *T, 逗号分隔, 嵌套 < 等，继续
		case lexer.TOKEN_GT:
			// 闭合 > ：检查后续是否为 ( ，是则为泛型调用
			next := p.lexer.Next()
			return next.Type == lexer.TOKEN_LPAREN
		default:
			// 遇到非类型参数 token，判定为比较运算
			return false
		}
	}
}

// parseIntegerLiteralIterative 迭代解析整数字面量
func (p *Parser) parseIntegerLiteralIterative() *ast.IntegerLiteral {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	value, err := strconv.ParseUint(p.curTok.Value, 0, 64)
	if err != nil {
		p.error(fmt.Sprintf("invalid integer literal: %s", p.curTok.Value))
		return &ast.IntegerLiteral{Value: 0, Pos: pos}
	}
	literal := &ast.IntegerLiteral{Value: value, Pos: pos}
	p.nextToken()
	return literal
}

// parseCharLiteralIterative 迭代解析字符字面量
func (p *Parser) parseCharLiteralIterative() *ast.CharLiteral {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	literal := &ast.CharLiteral{Value: p.curTok.Value, Pos: pos}
	p.nextToken()
	return literal
}

// parseFloatLiteralIterative 迭代解析浮点数字面量
func (p *Parser) parseFloatLiteralIterative() *ast.FloatLiteral {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	value, err := strconv.ParseFloat(p.curTok.Value, 64)
	if err != nil {
		p.error(fmt.Sprintf("invalid float literal: %s", p.curTok.Value))
		return &ast.FloatLiteral{Value: 0, Pos: pos}
	}
	literal := &ast.FloatLiteral{Value: value, Pos: pos}
	p.nextToken()
	return literal
}

// parseStringLiteralIterative 迭代解析字符串字面量
func (p *Parser) parseStringLiteralIterative() *ast.StringLiteral {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	literal := &ast.StringLiteral{Value: p.curTok.Value, Pos: pos}
	p.nextToken()
	return literal
}

// parseTypeConstructorCastIterative 解析类型构造符 cstring(expr) / str(expr)。
//
// 语法: cstring(expr)
//
// 语义上等价于 as<cstring>(expr)：把字符串字面量/string 值转换为 C 风格字符串。
// 与 as<T> 一样是显式转换，不引入隐式转换。
func (p *Parser) parseTypeConstructorCastIterative() ast.Expression {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	targetType := p.curTok.Value
	p.nextToken() // 跳过 cstring

	if p.curTok.Type != lexer.TOKEN_LPAREN {
		p.error(fmt.Sprintf("expected '(' after '%s', got %s", targetType, lexer.TokenTypeToString(p.curTok.Type)))
		return nil
	}
	p.nextToken()

	expr := p.parseExpressionIterative()
	if expr == nil {
		p.error(fmt.Sprintf("expected expression in %s(...)", targetType))
		return nil
	}

	if p.curTok.Type != lexer.TOKEN_RPAREN {
		p.error(fmt.Sprintf("expected ')' to close %s(...', got %s", targetType, lexer.TokenTypeToString(p.curTok.Type)))
		return nil
	}
	p.nextToken()

	return &ast.TypeCastExpression{
		TargetType: targetType,
		Expression: expr,
		Pos:        pos,
	}
}

// parseAsCastExpressionIterative 解析 as<T>(e) 类型转换表达式。
//
// 语法: as<T>(expr)
//
// 作为唯一允许的强转形式，取代裸 (T)e 语法。
// 运行时零开销：直接映射为 C 的 ((T)(e))。
// 语义上统一处理：相容类型走安全转换，指针/不同族走位重解释，
// 具体安全检查由 sema 阶段负责（codegen 无差别生成 C 强转）。
func (p *Parser) parseAsCastExpressionIterative() ast.Expression {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}

	// 跳过 as
	p.nextToken()

	// 期望 <类型>
	if p.curTok.Type != lexer.TOKEN_LT {
		p.error(fmt.Sprintf("expected '<' after 'as', got %s", lexer.TokenTypeToString(p.curTok.Type)))
		return nil
	}
	p.nextToken()

	// 解析目标类型：接受类型关键字、标识符、void(T...)R 签名记法、复合类型
	targetType := p.parseTypeStringForDecl()
	if targetType == "" {
		p.error(fmt.Sprintf("expected type after 'as<', got %s", lexer.TokenTypeToString(p.curTok.Type)))
		return nil
	}

	// 期望 >
	if p.curTok.Type != lexer.TOKEN_GT {
		p.error(fmt.Sprintf("expected '>' to close 'as<%s', got %s", targetType, lexer.TokenTypeToString(p.curTok.Type)))
		return nil
	}
	p.nextToken()

	// 期望 (expr)  —— 强制括号包裹，避免优先级歧义
	if p.curTok.Type != lexer.TOKEN_LPAREN {
		p.error(fmt.Sprintf("expected '(' after 'as<%s>', got %s (use syntax: as<%s>(expr))",
			targetType, lexer.TokenTypeToString(p.curTok.Type), targetType))
		return nil
	}
	p.nextToken()

	expr := p.parseExpressionIterative()
	if expr == nil {
		p.error(fmt.Sprintf("expected expression in as<%s>(...)", targetType))
		return nil
	}

	if p.curTok.Type != lexer.TOKEN_RPAREN {
		p.error(fmt.Sprintf("expected ')' to close 'as<%s>(...', got %s", targetType, lexer.TokenTypeToString(p.curTok.Type)))
		return nil
	}
	p.nextToken()

	return &ast.TypeCastExpression{
		TargetType: targetType,
		Expression: expr,
		Pos:        pos,
	}
}

// parseGroupedExpressionIterative 迭代解析分组表达式
func (p *Parser) parseGroupedExpressionIterative() ast.Expression {
	p.nextToken()
	expr := p.parseExpressionIterative()
	if p.curTok.Type == lexer.TOKEN_RPAREN {
		p.nextToken()
	} else {
		p.error(fmt.Sprintf("expected ')', got %s", lexer.TokenTypeToString(p.curTok.Type)))
	}
	return expr
}

// parseAttributeExpressionIterative 解析表达式级属性 #[name(args)]
// 这是 Kaula 特殊操作的统一语法：asm/volatile/atomic/fence 等都通过此机制实现
// 示例:
//
//   let cr3 = #[asm("mov %cr3, %rax")]
//   let val = #[volatile_load(ptr)]
//   #[fence()]
func (p *Parser) parseAttributeExpressionIterative() ast.Expression {
	if p.curTok.Type != lexer.TOKEN_ATTRIBUTE {
		return nil
	}

	pos := ast.Position{Line: p.curTok.Line, Column: p.curTok.Column}

	annotationValue := p.curTok.Value
	annotationContent := strings.TrimPrefix(annotationValue, "#[")
	annotationContent = strings.TrimSuffix(annotationContent, "]")

	attrs := ast.ParseAttributeList(annotationContent)
	p.nextToken()

	if len(attrs) == 0 {
		p.error("empty attribute expression")
		return nil
	}

	// 表达式级属性只取第一个（与声明级不同）
	return &ast.AttributeExpression{
		Attr: attrs[0],
		Pos:  pos,
	}
}

// parseCallExpressionIterative 迭代解析函数调用表达式
func (p *Parser) parseCallExpressionIterative(function ast.Expression) ast.Expression {
	call := &ast.CallExpression{
		Function: function,
		Args:     []ast.Expression{},
	}

	// 解析泛型参数（如果存在），语法: fn<T>(args)
	if p.curTok.Type == lexer.TOKEN_LT {
		p.nextToken()
		for p.curTok.Type == lexer.TOKEN_IDENT || p.isTypeToken(p.curTok.Type) {
			call.TypeArgs = append(call.TypeArgs, p.curTok.Value)
			p.nextToken()
			if p.curTok.Type == lexer.TOKEN_COMMA {
				p.nextToken()
			} else if p.curTok.Type == lexer.TOKEN_GT {
				break
			}
		}
		if p.curTok.Type == lexer.TOKEN_GT {
			p.nextToken()
		}
	}

	// 当前 token 必须是 LPAREN
	if p.curTok.Type != lexer.TOKEN_LPAREN {
		return nil
	}
	p.nextToken()

	for p.curTok.Type != lexer.TOKEN_RPAREN && p.curTok.Type != lexer.TOKEN_EOF {
		prevTok := p.curTok

		// 如果当前 token 是 RPAREN、COMMA 或 RBRACE，说明参数解析应该结束
		if p.curTok.Type == lexer.TOKEN_RPAREN || p.curTok.Type == lexer.TOKEN_COMMA || p.curTok.Type == lexer.TOKEN_RBRACE {
			break
		}

		if p.curTok.Type == lexer.TOKEN_IDENT && p.peekTok.Type == lexer.TOKEN_COLON {
			p.nextToken()
			p.nextToken()
			arg := p.parseExpressionIterative()
			if arg != nil {
				call.Args = append(call.Args, arg)
			}
		} else {
			arg := p.parseExpressionIterative()
			if arg != nil {
				call.Args = append(call.Args, arg)
			}
		}

		// 如果 parseExpressionIterative 没有消费任何 token，跳过当前 token 避免死循环
		if p.curTok.Type == prevTok.Type && p.curTok.Value == prevTok.Value {
			p.nextToken()
		}

		if p.curTok.Type == lexer.TOKEN_COMMA {
			p.nextToken()
		}
	}
	if p.curTok.Type == lexer.TOKEN_RPAREN {
		p.nextToken()
	}
	return call
}

// parseIndexExpressionIterative 迭代解析索引表达式
func (p *Parser) parseIndexExpressionIterative() ast.Expression {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	index := &ast.IndexExpression{
		Pos: pos,
	}
	p.nextToken()
	index.Object = p.parseExpressionIterative()
	if p.curTok.Type == lexer.TOKEN_COLON {
		// 切片语法: object[start:end]
		p.nextToken()
		index.Index = p.parseExpressionIterative()
	} else {
		// 普通索引: object[index]
		index.Index = p.parseExpressionIterative()
	}
	if p.curTok.Type == lexer.TOKEN_RBRACKET {
		p.nextToken()
	} else {
		p.error(fmt.Sprintf("expected ']', got %s", lexer.TokenTypeToString(p.curTok.Type)))
	}

	// 处理索引后的成员访问（如 arr[i].field）
	expr := ast.Expression(index)
	for p.curTok.Type == lexer.TOKEN_DOT {
		p.nextToken()
		if p.curTok.Type == lexer.TOKEN_IDENT || p.curTok.Type == lexer.TOKEN_PRINTLN {
			memberName := p.curTok.Value
			memberPos := ast.Position{
				Line:   p.curTok.Line,
				Column: p.curTok.Column,
				File:   p.file,
			}
			p.nextToken()

			expr = &ast.MemberAccessExpression{
				Object: expr,
				Member: memberName,
				Pos:    memberPos,
			}

			// 如果后面还有 LPAREN，说明是函数调用
			if p.curTok.Type == lexer.TOKEN_LPAREN {
				return p.parseCallExpressionIterative(expr)
			}
		} else {
			break
		}
	}

	return expr
}

// parseArrayLiteralIterative 解析数组字面量
// 语法: [expr1, expr2, ...] 或 []
func (p *Parser) parseArrayLiteralIterative() ast.Expression {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	p.nextToken() // 跳过 '['
	lit := &ast.ArrayLiteral{
		Elements: []ast.Expression{},
		Pos:      pos,
	}
	for p.curTok.Type != lexer.TOKEN_RBRACKET && p.curTok.Type != lexer.TOKEN_EOF {
		elem := p.parseExpressionIterative()
		if elem != nil {
			lit.Elements = append(lit.Elements, elem)
		}
		if p.curTok.Type == lexer.TOKEN_COMMA {
			p.nextToken()
		} else if p.curTok.Type != lexer.TOKEN_RBRACKET {
			p.nextToken()
		}
	}
	if p.curTok.Type == lexer.TOKEN_RBRACKET {
		p.nextToken()
	}
	return lit
}

// parsePrefixCallStatementIterative 迭代解析前缀调用语句
// 语法: @PrefixName(param1=value1, param2=value2) { body }
func (p *Parser) parsePrefixCallStatementIterative() *ast.ExpressionStatement {
	var prefixName string
	params := make(map[string]ast.Expression)

	// 检查是否是 @ 前缀调用
	if p.curTok.Type == lexer.TOKEN_AT {
		p.nextToken() // consume @
		if p.curTok.Type != lexer.TOKEN_IDENT {
			p.error("expected identifier after @")
			return nil
		}
		prefixName = p.curTok.Value
		p.nextToken() // consume identifier

		// 解析参数（如果有）
		if p.curTok.Type == lexer.TOKEN_LPAREN {
			p.nextToken()
			for p.curTok.Type != lexer.TOKEN_RPAREN && p.curTok.Type != lexer.TOKEN_EOF {
				if p.curTok.Type == lexer.TOKEN_IDENT && p.peekTok.Type == lexer.TOKEN_ASSIGN {
					paramName := p.curTok.Value
					p.nextToken() // skip IDENT
					p.nextToken() // skip ASSIGN
					paramValue := p.parseExpressionIterative()
					params[paramName] = paramValue
				}
				if p.curTok.Type == lexer.TOKEN_COMMA {
					p.nextToken()
				}
			}
			if p.curTok.Type == lexer.TOKEN_RPAREN {
				p.nextToken()
			}
		}
	} else if p.curTok.Type == lexer.TOKEN_IDENT && p.peekTok.Type == lexer.TOKEN_LBRACE {
		// 处理没有 @ 的情况
		prefixName = p.curTok.Value
		p.nextToken()
	} else {
		return nil
	}

	// 解析花括号
	if p.curTok.Type == lexer.TOKEN_LBRACE {
		p.nextToken()
		blockBody := []ast.Statement{}
		for p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
			bodyStmt := p.parseStatementIterative()
			if bodyStmt != nil {
				blockBody = append(blockBody, bodyStmt)
			}
			// 如果 parseStatementIterative 没有前进 token（返回 nil 且 token 没变），需要手动前进
			// 避免死循环
			if p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
				// parseStatementIterative 应该已经前进了 token
				// 如果它失败了，我们也需要前进以避免死循环
			}
		}
		if p.curTok.Type == lexer.TOKEN_RBRACE {
			p.nextToken()
		}
		prefixCall := &ast.PrefixCallExpression{
			Name:   prefixName,
			Params: params,
			Body:   blockBody,
		}
		return &ast.ExpressionStatement{
			Expression: prefixCall,
		}
	}

	return nil
}

// parseYieldStatementIterative 解析 yield 语句
// 语法: yield source -> target
func (p *Parser) parseYieldStatementIterative() ast.Statement {
	pos := ast.Position{Line: p.curTok.Line, Column: p.curTok.Column, File: p.file}
	p.nextToken() // 跳过 yield

	// 解析 source 表达式（标识符或索引表达式）
	source := p.parsePrimaryExpressionIterative()
	if source == nil {
		p.error("yield: expected source expression")
		return nil
	}

	// 期望 -> 箭头
	if p.curTok.Type == lexer.TOKEN_ARROW {
		p.nextToken()
	} else if p.curTok.Type == lexer.TOKEN_MINUS && p.peekTok.Type == lexer.TOKEN_GT {
		p.nextToken() // 跳过 -
		p.nextToken() // 跳过 >
	}

	// 解析目标标识符
	target := ""
	if p.curTok.Type == lexer.TOKEN_IDENT {
		target = p.curTok.Value
		p.nextToken()
	}

	if p.curTok.Type == lexer.TOKEN_SEMICOLON {
		p.nextToken()
	}

	return &ast.YieldStatement{
		Source: source,
		Target: target,
		Pos:    pos,
	}
}

// parseReleaseStatementIterative 解析 release 语句
// 语法: release source -> [holder1, holder2, ...]
func (p *Parser) parseReleaseStatementIterative() ast.Statement {
	pos := ast.Position{Line: p.curTok.Line, Column: p.curTok.Column, File: p.file}
	p.nextToken() // 跳过 release

	// 解析 source 表达式
	source := p.parsePrimaryExpressionIterative()
	if source == nil {
		p.error("release: expected source expression")
		return nil
	}

	// 期望 ->
	if p.curTok.Type == lexer.TOKEN_ARROW {
		p.nextToken()
	} else if p.curTok.Type == lexer.TOKEN_MINUS && p.peekTok.Type == lexer.TOKEN_GT {
		p.nextToken()
		p.nextToken()
	}

	// 解析持有者列表 [holder1, holder2, ...]
	var holders []string
	if p.curTok.Type == lexer.TOKEN_LBRACKET {
		p.nextToken() // 跳过 [
		for p.curTok.Type == lexer.TOKEN_IDENT {
			holders = append(holders, p.curTok.Value)
			p.nextToken()
			if p.curTok.Type == lexer.TOKEN_COMMA {
				p.nextToken()
			}
		}
		if p.curTok.Type == lexer.TOKEN_RBRACKET {
			p.nextToken() // 跳过 ]
		}
	}

	if p.curTok.Type == lexer.TOKEN_SEMICOLON {
		p.nextToken()
	}

	return &ast.ReleaseStatement{
		Source:  source,
		Holders: holders,
		Pos:     pos,
	}
}

// parseExtractStatementIterative 解析 extract 语句
// 语法: extract source[index] -> target
func (p *Parser) parseExtractStatementIterative() ast.Statement {
	pos := ast.Position{Line: p.curTok.Line, Column: p.curTok.Column, File: p.file}
	p.nextToken() // 跳过 extract

	// 解析 source 表达式（标识符）
	source := p.parsePrimaryExpressionIterative()
	if source == nil {
		p.error("extract: expected source expression")
		return nil
	}

	// 解析索引 [index]
	var index ast.Expression
	if p.curTok.Type == lexer.TOKEN_LBRACKET {
		p.nextToken() // 跳过 [
		index = p.parsePrimaryExpressionIterative()
		if p.curTok.Type == lexer.TOKEN_RBRACKET {
			p.nextToken() // 跳过 ]
		}
	}

	// 期望 ->
	if p.curTok.Type == lexer.TOKEN_ARROW {
		p.nextToken()
	} else if p.curTok.Type == lexer.TOKEN_MINUS && p.peekTok.Type == lexer.TOKEN_GT {
		p.nextToken()
		p.nextToken()
	}

	// 解析目标标识符
	target := ""
	if p.curTok.Type == lexer.TOKEN_IDENT {
		target = p.curTok.Value
		p.nextToken()
	}

	if p.curTok.Type == lexer.TOKEN_SEMICOLON {
		p.nextToken()
	}

	return &ast.ExtractStatement{
		Source: source,
		Index:  index,
		Target: target,
		Pos:    pos,
	}
}

// error 报告错误
func (p *Parser) error(message string) {
	suggestion := errors.GenerateSuggestion(message)
	context, sourceLine, lineNumStr := errors.ExtractSourceContext(p.lexer.GetSource(), p.curTok.Line, p.curTok.Column)
	err := &errors.Error{
		Type:          errors.ErrorSyntax,
		Message:       message,
		Line:          p.curTok.Line,
		Column:        p.curTok.Column,
		File:          p.file,
		Suggestion:    suggestion,
		SourceContext: context,
		SourceLine:    sourceLine,
		LineNumberStr: lineNumStr,
		Highlight:     errors.BuildHighlight(p.lexer.GetSource(), p.curTok.Line, p.curTok.Column, 0, p.file, errors.ErrorSyntax, message),
	}
	p.errorCollector.AddErrorInstance(err)
}

// SetFile 设置文件名
func (p *Parser) SetFile(file string) {
	p.file = file
	p.lexer.SetFile(file)
}

// SetErrorCollector 设置错误收集器
func (p *Parser) SetErrorCollector(errorCollector *errors.ErrorCollector) {
	p.errorCollector = errorCollector
	p.lexer.SetErrorCollector(errorCollector)
}

// GetErrorCollector 获取错误收集器
func (p *Parser) GetErrorCollector() *errors.ErrorCollector {
	return p.errorCollector
}

// HasErrors 检查是否有错误
func (p *Parser) HasErrors() bool {
	return p.errorCollector.HasErrors() || p.lexer.HasErrors()
}

// ReportErrors 报告错误
func (p *Parser) ReportErrors() {
	p.lexer.ReportErrors()
	p.errorCollector.ReportErrors()
}

// Parse 解析程序
func (p *Parser) Parse() *ast.Program {
	p.log("开始解析程序")
	program := p.parseProgram()
	p.log("parseProgram returned, %d statements", len(program.Statements))
	if p.HasErrors() {
		p.log("解析完成，发现错误")
		// 不立即报告错误，等待所有阶段完成后统一报告
	} else {
		p.log("解析完成，未发现错误")
		p.Validate(program)
		// 验证阶段的错误也不立即报告
	}
	return program
}

// Validate 验证 AST 的数据完整性
func (p *Parser) Validate(program *ast.Program) {
	p.log("开始验证 AST 数据完整性")

	functionNames := make(map[string]bool)
	hasMain := false
	for _, stmt := range program.Statements {
		if fnStmt, ok := stmt.(*ast.FunctionStatement); ok {
			if fnStmt.Name == "" {
				p.error("函数缺少名称")
			} else if functionNames[fnStmt.Name] {
				p.error(fmt.Sprintf("函数名称重复：%s", fnStmt.Name))
			} else {
				functionNames[fnStmt.Name] = true
				if fnStmt.Name == "main" {
					hasMain = true
				}
			}
		}
	}

	if !hasMain && !p.skipMainCheck {
		p.error("找不到 main 函数")
	}

	for _, stmt := range program.Statements {
		if spendStmt, ok := stmt.(*ast.SpendStatement); ok {
			if spendStmt.Target == nil {
				p.error("spend 语句缺少目标表达式")
			}
			if len(spendStmt.Calls) == 0 {
				p.error("spend 语句缺少 call 子句")
			}
			for i, call := range spendStmt.Calls {
				if call.Index == nil {
					p.error(fmt.Sprintf("spend 语句的第 %d 个 call 子句缺少索引", i+1))
				}
			}
		}
	}

	prefixNames := make(map[string]bool)
	for _, stmt := range program.Statements {
		if prefixStmt, ok := stmt.(*ast.PrefixStatement); ok {
			if prefixStmt.Name == "" {
				p.error("prefix 语句缺少名称")
			} else if prefixNames[prefixStmt.Name] {
				p.error(fmt.Sprintf("prefix 名称重复：%s", prefixStmt.Name))
			} else {
				prefixNames[prefixStmt.Name] = true
			}
		}
	}

	for _, stmt := range program.Statements {
		if objStmt, ok := stmt.(*ast.ObjectStatement); ok {
			if objStmt.Type == "" {
				p.error("object 语句缺少类型")
			}
			if objStmt.Name == "" {
				p.error("object 语句缺少名称")
			}
		}
	}

	validModules := map[string]bool{
		// 基础模块
		"std":      true,
		"std.base": true,

		// 标准库模块（带 std. 前缀）
		"std.io":         true,
		"std.string":     true,
		"std.memory":     true,
		"std.container":  true,
		"std.math":       true,
		"std.system":     true,
		"std.prefix":     true,
		"std.concurrent": true,
		"std.error":      true,
		"std.time":       true,
		"std.web":        true,
		"std.json":       true,
		"std.format":     true,
		"std.crypto":     true,
		"std.net":        true,
		"std.logging":    true,
		"std.testing":    true,
		"std.i18n":       true,
		"std.gui":        true,
		"std.random":     true,
		"std.algebra":    true,
		"std.cmath":      true,

		// 兼容旧版（不带 std. 前缀）
		"io":         true,
		"string":     true,
		"memory":     true,
		"container":  true,
		"math":       true,
		"system":     true,
		"prefix":     true,
		"concurrent": true,
		"error":      true,
		"base":       true,

		// 系统模块
		"syscall": true,
	}

	// 加载第三方库配置，将第三方库名称添加到有效模块列表
	// 尝试多个路径（与 kaulac findStdlib 的候选一致: KAULA_HOME > exe 路径 > cwd 相对）
	stdlibPaths := []string{"stdlib.json", "kaula/stdlib.json", "../stdlib.json"}
	if envHome := os.Getenv("KAULA_HOME"); envHome != "" {
		stdlibPaths = append(stdlibPaths, filepath.Join(envHome, "compiler", "stdlib.json"))
	}
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(filepath.Clean(exePath))
		stdlibPaths = append(stdlibPaths,
			filepath.Join(exeDir, "stdlib.json"),
			filepath.Join(exeDir, "compiler", "stdlib.json"),
			filepath.Join(exeDir, "..", "compiler", "stdlib.json"),
		)
	}
	for _, path := range stdlibPaths {
		stdlibConfig, err := stdlib.LoadStdlibConfig(path)
		if err == nil && stdlibConfig != nil {
			// 添加标准库模块
			for moduleName := range stdlibConfig.Modules {
				validModules[moduleName] = true
			}
			// 添加第三方库
			for _, lib := range stdlibConfig.ThirdParty {
				validModules[lib.Name] = true
			}
			if verboseEnabled {
				fmt.Printf("Parser: Loaded %d stdlib modules and %d third-party libraries from %s\n", len(stdlibConfig.Modules), len(stdlibConfig.ThirdParty), path)
			}
			break // 加载成功后退出
		}
	}

	for _, stmt := range program.Statements {
		if importStmt, ok := stmt.(*ast.ImportStatement); ok {
			if importStmt.Path != "" {
				// 路径导入（Python 风格）：import "lib" / import "file"
				p.resolvePathImport(importStmt)
				continue
			}
			if importStmt.Module == "" {
				p.error("import 语句缺少模块名称")
			} else if !validModules[importStmt.Module] {
				// 检查是否是本地 .kl 文件
				// 查找顺序:
				//   1. 点号转路径分隔符: import my.util -> my/util.kl (目录查询)
				//   2. 单文件带点: import my.util -> my.util.kl (兼容旧写法)
				var localCandidates []string
				dir := filepath.Dir(p.file)
				if dir == "" {
					dir = "."
				}
				// 路径分隔形式: my.util -> my/util.kl (仅在含点号时)
				if strings.Contains(importStmt.Module, ".") {
					relPath := strings.ReplaceAll(importStmt.Module, ".", string(os.PathSeparator)) + ".kl"
					localCandidates = append(localCandidates, relPath)
					localCandidates = append(localCandidates, filepath.Join(dir, relPath))
				}
				// 单文件带点形式: my.util.kl
				localCandidates = append(localCandidates, importStmt.Module+".kl")
				localCandidates = append(localCandidates, filepath.Join(dir, importStmt.Module+".kl"))

				found := false
				for _, cand := range localCandidates {
					if _, err := os.Stat(cand); err == nil {
						importStmt.IsLocal = true
						importStmt.LocalPath = cand
						found = true
						break
					}
				}
				if !found {
					p.error(fmt.Sprintf("导入的模块不存在：%s", importStmt.Module))
				}
			}
		}
	}

	if p.HasErrors() {
		p.log("验证完成，发现验证错误")
	} else {
		p.log("验证完成，未发现错误")
	}
}

// resolvePathImport 解析路径导入语句（import "lib" / import "file"）
// 规则（Python 风格）：
//   - 相对路径以当前导入文件所在目录为基准，其次回退到工作目录；
//   - 绝对路径（以 /、\ 或盘符开头）直接使用；
//   - 目标为目录时视作 Kaula 库导入，目录内必须存在 kaula.json（可为空文件），
//     否则报错「缺少 kaula.json，无法作为库导入」；导入库目录下的全部 .kl 文件；
//   - 目标为文件时视作普通文件导入，无 kaula.json 要求；
//   - 目标不存在时尝试补全 .kl 后缀。
func (p *Parser) resolvePathImport(stmt *ast.ImportStatement) {
	target := strings.TrimSpace(stmt.Path)
	if target == "" {
		p.error("import 路径为空")
		return
	}

	// 统一分隔符（兼容 Windows 反斜杠）
	target = strings.ReplaceAll(target, "\\", "/")

	// 相对路径解析：优先以当前导入文件所在目录为基准（Python 风格）
	if !filepath.IsAbs(target) {
		dir := filepath.Dir(p.file)
		if dir == "" {
			dir = "."
		}
		cand := filepath.Join(dir, target)
		stored := cand
		if _, err := os.Stat(cand); err != nil {
			// 当前文件目录下不存在，回退到工作目录
			stored = target
		}
		if abs, err := filepath.Abs(stored); err == nil {
			stored = abs
		}
		target = stored
	}

	info, err := os.Stat(target)
	if err == nil && info.IsDir() {
		// === 库导入：必须包含 kaula.json ===
		if _, err := os.Stat(filepath.Join(target, "kaula.json")); err != nil {
			p.error(fmt.Sprintf("目标文件夹 '%s' 缺少 kaula.json，无法作为库导入", stmt.Path))
			return
		}
		entries, err := os.ReadDir(target)
		if err != nil {
			p.error(fmt.Sprintf("读取库文件夹失败 '%s'：%v", stmt.Path, err))
			return
		}
		var files []string
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			if strings.HasSuffix(strings.ToLower(entry.Name()), ".kl") {
				files = append(files, filepath.Join(target, entry.Name()))
			}
		}
		sort.Strings(files)
		if len(files) == 0 {
			p.error(fmt.Sprintf("库文件夹 '%s' 中没有可导入的 .kl 文件", stmt.Path))
			return
		}
		stmt.IsLocal = true
		stmt.LocalPaths = files
		stmt.LocalPath = files[0]
		return
	}

	if err == nil {
		// === 文件导入：无 kaula.json 要求 ===
		stmt.IsLocal = true
		stmt.LocalPath = target
		stmt.LocalPaths = []string{target}
		return
	}

	// 目标不存在：尝试补全 .kl 后缀
	fileCand := target + ".kl"
	if fileInfo, err := os.Stat(fileCand); err == nil && !fileInfo.IsDir() {
		stmt.IsLocal = true
		stmt.LocalPath = fileCand
		stmt.LocalPaths = []string{fileCand}
		return
	}
	p.error(fmt.Sprintf("导入的文件或库不存在：%s", stmt.Path))
}

func (p *Parser) parseSizeOfExpressionIterative() *ast.SizeOfExpression {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	p.nextToken()
	if p.curTok.Type != lexer.TOKEN_LPAREN {
		p.error("sizeof 后应该跟 (")
		return nil
	}
	p.nextToken()
	targetType := p.parseTypeString()
	if targetType == "" {
		p.error("sizeof 中缺少类型")
		return nil
	}
	if p.curTok.Type != lexer.TOKEN_RPAREN {
		p.error("sizeof 类型后应该跟 )")
		return nil
	}
	p.nextToken()
	return &ast.SizeOfExpression{
		TargetType: targetType,
		Pos:        pos,
	}
}

func (p *Parser) parseAlignOfExpressionIterative() *ast.AlignOfExpression {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	p.nextToken()
	if p.curTok.Type != lexer.TOKEN_LPAREN {
		p.error("alignof 后应该跟 (")
		return nil
	}
	p.nextToken()
	targetType := p.parseTypeString()
	if targetType == "" {
		p.error("alignof 中缺少类型")
		return nil
	}
	if p.curTok.Type != lexer.TOKEN_RPAREN {
		p.error("alignof 类型后应该跟 )")
		return nil
	}
	p.nextToken()
	return &ast.AlignOfExpression{
		TargetType: targetType,
		Pos:        pos,
	}
}

func (p *Parser) parseOffsetOfExpressionIterative() *ast.OffsetOfExpression {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	p.nextToken()
	if p.curTok.Type != lexer.TOKEN_LPAREN {
		p.error("offsetof 后应该跟 (")
		return nil
	}
	p.nextToken()
	targetType := p.parseTypeString()
	if targetType == "" {
		p.error("offsetof 中缺少类型")
		return nil
	}
	if p.curTok.Type != lexer.TOKEN_COMMA {
		p.error("offsetof 中类型后应该跟 , 字段名")
		return nil
	}
	p.nextToken()
	if p.curTok.Type != lexer.TOKEN_IDENT {
		p.error("offsetof 中 , 后应该跟字段名")
		return nil
	}
	fieldName := p.curTok.Value
	p.nextToken()
	if p.curTok.Type != lexer.TOKEN_RPAREN {
		p.error("offsetof 后应该跟 )")
		return nil
	}
	p.nextToken()
	return &ast.OffsetOfExpression{
		TargetType: targetType,
		FieldName:  fieldName,
		Pos:        pos,
	}
}

func (p *Parser) parseComptimeExpressionIterative() *ast.ComptimeExpression {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	p.nextToken()
	inner := p.parseExpressionIterative()
	if inner == nil {
		p.error("comptime 后应该跟表达式")
		return nil
	}
	return &ast.ComptimeExpression{
		Inner: inner,
		Pos:   pos,
	}
}

func (p *Parser) parseTypeNameExpressionIterative() *ast.TypeNameExpression {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	p.nextToken()
	if p.curTok.Type != lexer.TOKEN_LPAREN {
		p.error("type_name 后应该跟 (")
		return nil
	}
	p.nextToken()
	targetType := p.parseTypeString()
	if targetType == "" {
		p.error("type_name 中缺少类型")
		return nil
	}
	if p.curTok.Type != lexer.TOKEN_RPAREN {
		p.error("type_name 类型后应该跟 )")
		return nil
	}
	p.nextToken()
	return &ast.TypeNameExpression{
		TargetType: targetType,
		Pos:        pos,
	}
}

func (p *Parser) parseFieldCountExpressionIterative() *ast.FieldCountExpression {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	p.nextToken()
	if p.curTok.Type != lexer.TOKEN_LPAREN {
		p.error("field_count 后应该跟 (")
		return nil
	}
	p.nextToken()
	targetType := p.parseTypeString()
	if targetType == "" {
		p.error("field_count 中缺少类型")
		return nil
	}
	if p.curTok.Type != lexer.TOKEN_RPAREN {
		p.error("field_count 类型后应该跟 )")
		return nil
	}
	p.nextToken()
	return &ast.FieldCountExpression{
		TargetType: targetType,
		Pos:        pos,
	}
}

func (p *Parser) parseFieldNameExpressionIterative() *ast.FieldNameExpression {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	p.nextToken()
	if p.curTok.Type != lexer.TOKEN_LPAREN {
		p.error("field_name 后应该跟 (")
		return nil
	}
	p.nextToken()
	targetType := p.parseTypeString()
	if targetType == "" {
		p.error("field_name 中缺少类型")
		return nil
	}
	if p.curTok.Type != lexer.TOKEN_COMMA {
		p.error("field_name 中类型后应该跟 , 索引")
		return nil
	}
	p.nextToken()
	index := p.parseExpressionIterative()
	if index == nil {
		p.error("field_name 中缺少索引")
		return nil
	}
	if p.curTok.Type != lexer.TOKEN_RPAREN {
		p.error("field_name 后应该跟 )")
		return nil
	}
	p.nextToken()
	return &ast.FieldNameExpression{
		TargetType: targetType,
		Index:      index,
		Pos:        pos,
	}
}

func (p *Parser) parseFieldTypeExpressionIterative() *ast.FieldTypeExpression {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	p.nextToken()
	if p.curTok.Type != lexer.TOKEN_LPAREN {
		p.error("field_type 后应该跟 (")
		return nil
	}
	p.nextToken()
	targetType := p.parseTypeString()
	if targetType == "" {
		p.error("field_type 中缺少类型")
		return nil
	}
	if p.curTok.Type != lexer.TOKEN_COMMA {
		p.error("field_type 中类型后应该跟 , 索引")
		return nil
	}
	p.nextToken()
	index := p.parseExpressionIterative()
	if index == nil {
		p.error("field_type 中缺少索引")
		return nil
	}
	if p.curTok.Type != lexer.TOKEN_RPAREN {
		p.error("field_type 后应该跟 )")
		return nil
	}
	p.nextToken()
	return &ast.FieldTypeExpression{
		TargetType: targetType,
		Index:      index,
		Pos:        pos,
	}
}

func (p *Parser) parseTypeKindExpressionIterative() *ast.TypeKindExpression {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	p.nextToken()
	if p.curTok.Type != lexer.TOKEN_LPAREN {
		p.error("type_kind 后应该跟 (")
		return nil
	}
	p.nextToken()
	targetType := p.parseTypeString()
	if targetType == "" {
		p.error("type_kind 中缺少类型")
		return nil
	}
	if p.curTok.Type != lexer.TOKEN_RPAREN {
		p.error("type_kind 类型后应该跟 )")
		return nil
	}
	p.nextToken()
	return &ast.TypeKindExpression{
		TargetType: targetType,
		Pos:        pos,
	}
}

// peekNextTokenType 获取下一个 token 的类型
func (p *Parser) peekNextTokenType() lexer.TokenType {
	return p.peekTok.Type
}

// parseLambdaExpression 解析 lambda/闭包表达式
// 语法: fn(参数列表) { 函数体 } 或 fn(参数列表) -> 返回类型 { 函数体 }
func (p *Parser) parseLambdaExpression() *ast.LambdaExpression {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	expr := &ast.LambdaExpression{
		Params:     []string{},
		ParamTypes: []string{},
		Body:       []ast.Statement{},
		Captures:   []string{},
		Pos:        pos,
	}

	p.nextToken() // 跳过 fn

	// 解析参数 (param1: type1, param2: type2)
	if p.curTok.Type == lexer.TOKEN_LPAREN {
		p.nextToken()
		for p.curTok.Type != lexer.TOKEN_RPAREN && p.curTok.Type != lexer.TOKEN_EOF {
			if p.curTok.Type == lexer.TOKEN_IDENT || p.isTypeToken(p.curTok.Type) {
				// 可能是 "Type name" 或 "name" 两种形式
				firstToken := p.curTok.Value
				firstIsType := p.isTypeToken(p.curTok.Type)
				p.nextToken()

				if p.curTok.Type == lexer.TOKEN_IDENT {
					// "Type name" 形式
					paramType := firstToken
					paramName := p.curTok.Value
					expr.Params = append(expr.Params, paramName)
					expr.ParamTypes = append(expr.ParamTypes, paramType)
					p.nextToken()
				} else if firstIsType {
					// 类型关键字后没有标识符，跳过
				} else {
					// "name" 形式（无类型注解）
					expr.Params = append(expr.Params, firstToken)
					expr.ParamTypes = append(expr.ParamTypes, "auto")
				}

				// 可选的冒号类型注解: name: type
				if p.curTok.Type == lexer.TOKEN_COLON {
					p.nextToken()
					if p.curTok.Type == lexer.TOKEN_IDENT || p.isTypeToken(p.curTok.Type) {
						// 更新最后一个参数的类型
						if len(expr.ParamTypes) > 0 && expr.ParamTypes[len(expr.ParamTypes)-1] == "auto" {
							expr.ParamTypes[len(expr.ParamTypes)-1] = p.curTok.Value
						}
						p.nextToken()
					}
				}
			} else {
				p.nextToken()
			}

			if p.curTok.Type == lexer.TOKEN_COMMA {
				p.nextToken()
			}
		}
		if p.curTok.Type == lexer.TOKEN_RPAREN {
			p.nextToken()
		}
	}

	// 可选的返回类型: fn(...) -> type 或 fn(...) type
	if p.curTok.Type == lexer.TOKEN_ARROW {
		// -> type 形式
		p.nextToken()
		if p.curTok.Type == lexer.TOKEN_IDENT || p.isTypeToken(p.curTok.Type) {
			expr.ReturnType = p.curTok.Value
			p.nextToken()
		}
	} else if p.curTok.Type == lexer.TOKEN_MINUS && p.peekTok.Type == lexer.TOKEN_GT {
		// -> type 形式
		p.nextToken() // -
		p.nextToken() // >
		if p.curTok.Type == lexer.TOKEN_IDENT || p.isTypeToken(p.curTok.Type) {
			expr.ReturnType = p.curTok.Value
			p.nextToken()
		}
	} else if p.isTypeToken(p.curTok.Type) && p.peekTok.Type == lexer.TOKEN_LBRACE {
		// bare type 形式: fn(...) int { ... }
		expr.ReturnType = strings.ToLower(lexer.TokenTypeToString(p.curTok.Type))
		expr.ReturnType = strings.TrimPrefix(expr.ReturnType, "type_")
		p.nextToken()
	} else if p.curTok.Type == lexer.TOKEN_IDENT && p.peekTok.Type == lexer.TOKEN_LBRACE {
		// bare custom type 形式: fn(...) MyType { ... }
		expr.ReturnType = p.curTok.Value
		p.nextToken()
	}

	// 解析函数体
	if p.curTok.Type == lexer.TOKEN_LBRACE {
		p.nextToken()
		for p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
			if stmt := p.parseStatementIterative(); stmt != nil {
				expr.Body = append(expr.Body, stmt)
			} else {
				p.nextToken()
			}
		}
		if p.curTok.Type == lexer.TOKEN_RBRACE {
			p.nextToken()
		}
	}

	return expr
}

// parseEnumStatementIterative 迭代解析 enum 语句
// 语法: enum Name { Variant1, Variant2(Type), Variant3(Type1, Type2) }
// 或:   enum Name<T, E> { Variant1(T), Variant2(E) }
func (p *Parser) parseEnumStatementIterative() ast.Statement {
	pos := ast.Position{Line: p.curTok.Line, Column: p.curTok.Column, File: p.file}
	stmt := &ast.EnumStatement{
		Variants: make([]*ast.EnumVariant, 0, 8),
		Pos:      pos,
	}

	// 解析属性注解
	attrs := p.parseAttributes()
	if attrs != nil {
		stmt.Attributes = attrs
	}

	p.nextToken() // 跳过 enum

	// 解析枚举名
	if p.curTok.Type == lexer.TOKEN_IDENT {
		stmt.Name = p.curTok.Value
		p.nextToken()
	}

	// 解析泛型参数（如果存在）
	if p.curTok.Type == lexer.TOKEN_LT {
		p.nextToken()
		for p.curTok.Type == lexer.TOKEN_IDENT {
			stmt.TypeParams = append(stmt.TypeParams, &ast.TypeParameter{Name: p.curTok.Value})
			p.nextToken()
			if p.curTok.Type == lexer.TOKEN_COMMA {
				p.nextToken()
			} else if p.curTok.Type == lexer.TOKEN_GT {
				break
			}
		}
		if p.curTok.Type == lexer.TOKEN_GT {
			p.nextToken()
		}
		stmt.Generic = true
	}

	// 解析 { 变体列表 }
	if p.curTok.Type == lexer.TOKEN_LBRACE {
		p.nextToken()
		for p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
			if p.curTok.Type != lexer.TOKEN_IDENT {
				p.nextToken()
				continue
			}

			variant := &ast.EnumVariant{
				Name: p.curTok.Value,
			}
			p.nextToken()

			// 解析变体参数 Variant(Type1, Type2) 或 Variant(name: Type, name2: Type2)
			if p.curTok.Type == lexer.TOKEN_LPAREN {
				p.nextToken()
				for p.curTok.Type != lexer.TOKEN_RPAREN && p.curTok.Type != lexer.TOKEN_EOF {
					// 检查是否是命名参数形式: name: Type
					if p.curTok.Type == lexer.TOKEN_IDENT && p.peekTok.Type == lexer.TOKEN_COLON {
						fieldName := p.curTok.Value
						p.nextToken() // 跳过标识符
						p.nextToken() // 跳过冒号
						if p.curTok.Type == lexer.TOKEN_IDENT || p.isTypeToken(p.curTok.Type) {
							variant.FieldTypes = append(variant.FieldTypes, p.curTok.Value)
							variant.FieldNames = append(variant.FieldNames, fieldName)
							p.nextToken()
						}
					} else if p.curTok.Type == lexer.TOKEN_IDENT || p.isTypeToken(p.curTok.Type) {
						variant.FieldTypes = append(variant.FieldTypes, p.curTok.Value)
						p.nextToken()
					} else {
						p.nextToken()
					}

					if p.curTok.Type == lexer.TOKEN_COMMA {
						p.nextToken()
					}
				}
				if p.curTok.Type == lexer.TOKEN_RPAREN {
					p.nextToken()
				}
			}

			stmt.Variants = append(stmt.Variants, variant)

			// 跳过逗号分隔符
			if p.curTok.Type == lexer.TOKEN_COMMA {
				p.nextToken()
			}
		}
		if p.curTok.Type == lexer.TOKEN_RBRACE {
			p.nextToken()
		}
	}

	p.log("枚举解析完成：%s, 变体数：%d", stmt.Name, len(stmt.Variants))
	return stmt
}

// parseMatchExpressionIterative 迭代解析 match 表达式
// 语法: match(expr) { Pattern1 => stmt1, Pattern2(x) => stmt2, _ => default }
func (p *Parser) parseMatchExpressionIterative() ast.Expression {
	pos := ast.Position{Line: p.curTok.Line, Column: p.curTok.Column, File: p.file}
	expr := &ast.MatchExpression{
		Pos: pos,
	}

	p.nextToken() // 跳过 match

	// 解析 ( 目标表达式 ) 或无括号目标: match expr { ... }
	if p.curTok.Type == lexer.TOKEN_LPAREN {
		p.nextToken()
		expr.Target = p.parseExpressionIterative()
		if p.curTok.Type == lexer.TOKEN_RPAREN {
			p.nextToken()
		}
	} else if p.curTok.Type != lexer.TOKEN_LBRACE && p.curTok.Type != lexer.TOKEN_EOF {
		// 无括号形式: match x { ... }，直接解析目标表达式
		expr.Target = p.parseExpressionIterative()
	}

	// 解析 { 匹配分支列表 }
	if p.curTok.Type == lexer.TOKEN_LBRACE {
		p.nextToken()
		for p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
			arm := p.parseMatchArm()
			if arm != nil {
				expr.Arms = append(expr.Arms, arm)
			} else {
				// 跳过无法识别的 token
				p.nextToken()
			}
		}
		if p.curTok.Type == lexer.TOKEN_RBRACE {
			p.nextToken()
		}
	}

	return expr
}

// parseMatchArm 解析一个匹配分支
func (p *Parser) parseMatchArm() *ast.MatchArm {
	pos := ast.Position{Line: p.curTok.Line, Column: p.curTok.Column, File: p.file}
	arm := &ast.MatchArm{
		Pos: pos,
	}

	// 解析模式
	arm.Pattern = p.parseMatchPattern()
	if arm.Pattern == nil {
		return nil
	}

	// 期望 =>
	if p.curTok.Type == lexer.TOKEN_ARROW {
		p.nextToken()
	} else {
		p.error(fmt.Sprintf("match arm: expected '=>', got %s", lexer.TokenTypeToString(p.curTok.Type)))
		return nil
	}

	// 解析分支体（语句列表，以逗号或 } 分隔）
	for p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
		// 如果遇到下一个模式的开始（标识符后跟 => 或 _ 后跟 =>），停止解析当前分支体
		if p.isNextArmStart() {
			break
		}
		// { 开头：解析为代码块（如 Some(val) => { ... }），
		// 否则会被误解析成结构体字面量
		if p.curTok.Type == lexer.TOKEN_LBRACE {
			p.nextToken()
			block := &ast.BlockStatement{
				Pos: ast.Position{Line: p.curTok.Line, Column: p.curTok.Column, File: p.file},
			}
			for p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
				if p.isNextArmStart() {
					break
				}
				inner := p.parseStatementIterative()
				if inner != nil {
					block.Statements = append(block.Statements, inner)
				} else {
					p.nextToken()
				}
			}
			if p.curTok.Type == lexer.TOKEN_RBRACE {
				p.nextToken()
			}
			arm.Body = append(arm.Body, block)
			// 块结束后的逗号表示分支结束
			if p.curTok.Type == lexer.TOKEN_COMMA {
				p.nextToken()
				break
			}
			continue
		}
		stmt := p.parseStatementIterative()
		if stmt != nil {
			arm.Body = append(arm.Body, stmt)
		} else {
			break
		}
		// 跳过逗号分隔符
		if p.curTok.Type == lexer.TOKEN_COMMA {
			p.nextToken()
			break // 逗号也表示分支结束
		}
	}

	return arm
}

// isNextArmStart 检查当前位置是否是下一个匹配分支的开始
func (p *Parser) isNextArmStart() bool {
	if p.curTok.Type == lexer.TOKEN_IDENT && p.peekTok.Type == lexer.TOKEN_ARROW {
		return true
	}
	if p.curTok.Type == lexer.TOKEN_LITERAL_INT && p.peekTok.Type == lexer.TOKEN_ARROW {
		return true
	}
	if p.curTok.Type == lexer.TOKEN_STRING && p.peekTok.Type == lexer.TOKEN_ARROW {
		return true
	}
	if p.curTok.Type == lexer.TOKEN_TRUE && p.peekTok.Type == lexer.TOKEN_ARROW {
		return true
	}
	if p.curTok.Type == lexer.TOKEN_FALSE && p.peekTok.Type == lexer.TOKEN_ARROW {
		return true
	}
	return false
}

// parseMatchPattern 解析模式匹配的模式
func (p *Parser) parseMatchPattern() *ast.MatchPattern {
	pos := ast.Position{Line: p.curTok.Line, Column: p.curTok.Column, File: p.file}

	switch p.curTok.Type {
	case lexer.TOKEN_IDENT:
		name := p.curTok.Value
		// 特殊处理 _ 通配符
		if name == "_" {
			p.nextToken()
			return &ast.MatchPattern{
				Kind: ast.PatternWildcard,
				Pos:  pos,
			}
		}
		p.nextToken()

		// 检查是否有 ( 表示带数据的变体模式
		if p.curTok.Type == lexer.TOKEN_LPAREN {
			p.nextToken()
			pattern := &ast.MatchPattern{
				Kind:        ast.PatternVariant,
				VariantName: name,
				Pos:         pos,
			}
			// 解析绑定变量列表
			for p.curTok.Type != lexer.TOKEN_RPAREN && p.curTok.Type != lexer.TOKEN_EOF {
				if p.curTok.Type == lexer.TOKEN_IDENT {
					pattern.Bindings = append(pattern.Bindings, p.curTok.Value)
					p.nextToken()
				} else {
					p.nextToken()
				}
				if p.curTok.Type == lexer.TOKEN_COMMA {
					p.nextToken()
				}
			}
			if p.curTok.Type == lexer.TOKEN_RPAREN {
				p.nextToken()
			}
			return pattern
		}

		// 无参数标识符：可能是变体名或变量绑定
		// 如果首字母大写，视为变体名；否则视为变量绑定
		if len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z' {
			return &ast.MatchPattern{
				Kind:        ast.PatternVariant,
				VariantName: name,
				Pos:         pos,
			}
		}
		return &ast.MatchPattern{
			Kind:     ast.PatternVariable,
			Bindings: []string{name},
			Pos:      pos,
		}

	case lexer.TOKEN_LITERAL_INT:
		value := p.curTok.Value
		p.nextToken()
		intVal, _ := strconv.ParseInt(value, 10, 64)
		return &ast.MatchPattern{
			Kind:     ast.PatternInteger,
			IntValue: intVal,
			Pos:      pos,
		}

	case lexer.TOKEN_STRING:
		value := p.curTok.Value
		p.nextToken()
		return &ast.MatchPattern{
			Kind:     ast.PatternString,
			StrValue: value,
			Pos:      pos,
		}

	case lexer.TOKEN_TRUE:
		p.nextToken()
		return &ast.MatchPattern{
			Kind:        ast.PatternBoolean,
			VariantName: "true",
			Pos:         pos,
		}

	case lexer.TOKEN_FALSE:
		p.nextToken()
		return &ast.MatchPattern{
			Kind:        ast.PatternBoolean,
			VariantName: "false",
			Pos:         pos,
		}

	default:
		p.error(fmt.Sprintf("match pattern: unexpected token %s", lexer.TokenTypeToString(p.curTok.Type)))
		return nil
	}
}
