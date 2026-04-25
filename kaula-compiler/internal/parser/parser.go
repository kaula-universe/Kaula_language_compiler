package parser

import (
	"fmt"
	"kaula-compiler/internal/ast"
	"kaula-compiler/internal/errors"
	"kaula-compiler/internal/lexer"
	"kaula-compiler/internal/stdlib"
	"log"
	"os"
	"strconv"
	"strings"
)

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
	lexer  *lexer.Lexer
	curTok lexer.Token
	peekTok lexer.Token
	errorCollector *errors.ErrorCollector
	logger *log.Logger
	loggingEnabled bool
	file string
	taskStack []ParseTask
}

// NewParser 创建一个新的语法分析器
func NewParser(lexer *lexer.Lexer) *Parser {
	p := &Parser{
		lexer:  lexer,
		errorCollector: errors.NewErrorCollector(),
		logger: log.New(os.Stdout, "[Parser] ", log.LstdFlags),
		loggingEnabled: true,
		taskStack: make([]ParseTask, 0, 64),
	}
	p.nextToken()
	p.nextToken()
	return p
}

// EnableLogging 启用日志记录
func (p *Parser) EnableLogging(enabled bool) {
	p.loggingEnabled = enabled
}

// log 记录日志
func (p *Parser) log(format string, v ...interface{}) {
	if p.loggingEnabled {
		p.logger.Printf(format, v...)
	}
}

// nextToken 前进到下一个 token
func (p *Parser) nextToken() {
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

// parseStatementIterative 迭代解析语句
func (p *Parser) parseStatementIterative() ast.Statement {
	// 检查是否有函数注解
	if p.curTok.Type == lexer.TOKEN_ATTRIBUTE && p.peekTok.Type == lexer.TOKEN_FUNC {
		return p.parseFunctionStatementIterative()
	}
	
	switch p.curTok.Type {
	case lexer.TOKEN_VO:
		return p.parseVOStatementIterative()
	case lexer.TOKEN_SPEND:
		return p.parseSpendCallStatementIterative()
	case lexer.TOKEN_SPEND_CALL:
		return p.parseSpendCallStatementIterative()
	case lexer.TOKEN_CALL:
		return p.parseCallStatementIterative()
	case lexer.TOKEN_TASK:
		return p.parseTaskStatementIterative()
	case lexer.TOKEN_PREFIX:
		return p.parsePrefixStatementIterative()
	case lexer.TOKEN_TREE:
		return p.parseTreeStatementIterative()
	case lexer.TOKEN_OBJECT:
		return p.parseObjectStatementIterative()
	case lexer.TOKEN_FUNC:
		return p.parseFunctionStatementIterative()
	case lexer.TOKEN_CLASS:
		return p.parseClassStatementIterative()
	case lexer.TOKEN_LITERAL_INTERFACE:
		return p.parseInterfaceStatementIterative()
	case lexer.TOKEN_STRUCT:
		return p.parseStructStatementIterative()
	case lexer.TOKEN_IF:
		return p.parseIfStatementIterative()
	case lexer.TOKEN_WHILE:
		return p.parseWhileStatementIterative()
	case lexer.TOKEN_FOR:
		return p.parseForStatementIterative()
	case lexer.TOKEN_SWITCH:
		return p.parseSwitchStatementIterative()
	case lexer.TOKEN_RETURN:
		return p.parseReturnStatementIterative()
	case lexer.TOKEN_IMPORT:
		return p.parseImportStatementIterative()
	case lexer.TOKEN_EXPORT:
		return p.parseExportStatementIterative()
	case lexer.TOKEN_NONLOCAL:
		return p.parseNonLocalStatementIterative()
	case lexer.TOKEN_PRINTLN:
		return p.parseExpressionStatementIterative()
	case lexer.TOKEN_TYPE_INT, lexer.TOKEN_TYPE_FLOAT, lexer.TOKEN_TYPE_DOUBLE, lexer.TOKEN_TYPE_BOOL, lexer.TOKEN_TYPE_CHAR, lexer.TOKEN_TYPE_STRING, lexer.TOKEN_TYPE_VOID:
		// 类型关键字开头，尝试解析变量声明
		return p.parseVariableDeclarationIterative()
	case lexer.TOKEN_IDENT:
		if p.peekTok.Type == lexer.TOKEN_IDENT || p.peekTok.Type == lexer.TOKEN_QUESTION || p.peekTok.Type == lexer.TOKEN_MULTIPLY || p.peekTok.Type == lexer.TOKEN_LT {
			if stmt := p.parseVariableDeclarationIterative(); stmt != nil {
				return stmt
			}
		}
		if p.peekTok.Type == lexer.TOKEN_LBRACE {
			return p.parsePrefixCallStatementIterative()
		}
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
	stmt := &ast.VariableDeclaration{}
	
	// 首先检查是否有类型关键字
	var typeName string
	
	// 检查是否是基本类型关键字（int, float, string 等）
	if p.isTypeToken(p.curTok.Type) {
		typeName = lexer.TokenTypeToString(p.curTok.Type)
		typeName = strings.TrimPrefix(typeName, "TYPE_")
		// 转换为小写（如 "INT" -> "int"）
		typeName = strings.ToLower(typeName)
		p.nextToken()
	} else if p.curTok.Type == lexer.TOKEN_IDENT {
		// 可能是自定义类型（如类名、结构体名等）
		typeName = p.curTok.Value
		p.nextToken()
		
		// 检查是否是泛型类型（如 Box<int>）
		if p.curTok.Type == lexer.TOKEN_LT {
			p.nextToken()
			typeArgs := []string{}
			for p.curTok.Type == lexer.TOKEN_IDENT {
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
	} else {
		// 不是类型，返回 nil
		return nil
	}
	
	// 现在必须有变量名
	// 检查是否是指针类型（*）在变量名前面
	if p.curTok.Type == lexer.TOKEN_MULTIPLY {
		// 指针类型，在类型名后添加 *
		typeName = typeName + "*"
		p.nextToken()
	}
	
	if p.curTok.Type != lexer.TOKEN_IDENT {
		p.error(fmt.Sprintf("变量声明缺少变量名，当前 token: %s", lexer.TokenTypeToString(p.curTok.Type)))
		return nil
	}
	
	stmt.Type = typeName
	stmt.Name = p.curTok.Value
	p.nextToken()
	
	// 检查是否是指针类型（*）或可空类型（?）
	if p.curTok.Type == lexer.TOKEN_QUESTION {
		stmt.Nullable = true
		p.nextToken()
	} else if p.curTok.Type == lexer.TOKEN_MULTIPLY {
		// 指针类型，在类型名后添加 *
		stmt.Type = stmt.Type + "*"
		p.nextToken()
	}
	
	// 检查是否有赋值
	if p.curTok.Type == lexer.TOKEN_ASSIGN {
		p.nextToken()
		stmt.Value = p.parseExpressionIterative()
	}
	
	return stmt
}

// isTypeToken 检查是否是类型关键字
func (p *Parser) isTypeToken(tokenType lexer.TokenType) bool {
	return tokenType >= lexer.TOKEN_TYPE_INT && tokenType <= lexer.TOKEN_TYPE_VOID
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

// parseVOStatementIterative 迭代解析 VO 语句
func (p *Parser) parseVOStatementIterative() ast.Statement {
	// 检查是否是 VO 模块调用（如 vo.create()）
	if p.peekNextTokenType() == lexer.TOKEN_DOT {
		p.nextToken()
		p.nextToken()
		if p.curTok.Type == lexer.TOKEN_IDENT {
			memberIdent := &ast.Identifier{
				Name: p.curTok.Value,
			}
			p.nextToken()
			if p.curTok.Type == lexer.TOKEN_LPAREN {
				return &ast.ExpressionStatement{
					Expression: p.parseCallExpressionIterative(memberIdent),
				}
			}
			if p.curTok.Type == lexer.TOKEN_IDENT {
				bodyStmt := p.parseStatementIterative()
				if bodyStmt != nil {
					return bodyStmt
				}
			}
			return &ast.ExpressionStatement{
				Expression: memberIdent,
			}
		}
	}
	
	p.log("开始解析 VO 语句")
	stmt := &ast.VOStatement{}
	p.nextToken()
	if p.curTok.Type == lexer.TOKEN_LPAREN {
		p.nextToken()
		if p.curTok.Type == lexer.TOKEN_IDENT {
			p.nextToken()
		}
		if p.curTok.Type == lexer.TOKEN_RPAREN {
			p.nextToken()
		}
	}
	if p.curTok.Type == lexer.TOKEN_IDENT {
		p.nextToken()
	}
	if p.curTok.Type == lexer.TOKEN_SELF {
		p.nextToken()
		if p.curTok.Type == lexer.TOKEN_LPAREN {
			p.nextToken()
			for p.curTok.Type != lexer.TOKEN_RPAREN {
				if p.curTok.Type == lexer.TOKEN_IDENT {
					p.nextToken()
					if p.curTok.Type == lexer.TOKEN_ASSIGN {
						p.nextToken()
						p.parseExpressionIterative()
					}
				} else if p.curTok.Type == lexer.TOKEN_COMMA {
					p.nextToken()
				} else {
					p.nextToken()
				}
			}
			if p.curTok.Type == lexer.TOKEN_RPAREN {
				p.nextToken()
			}
		}
	}
	if p.curTok.Type == lexer.TOKEN_LBRACE {
		p.nextToken()
		for p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
			bodyStmt := p.parseStatementIterative()
			if bodyStmt != nil {
				// VOStatement 使用 Value/Code/Access 字段存储表达式
				// 将解析的语句转换为表达式（如果是表达式语句）
				if exprStmt, ok := bodyStmt.(*ast.ExpressionStatement); ok && stmt.Value == nil {
					stmt.Value = exprStmt.Expression
				} else if exprStmt, ok := bodyStmt.(*ast.ExpressionStatement); ok && stmt.Code == nil {
					stmt.Code = exprStmt.Expression
				}
			}
			if p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
				p.nextToken()
			}
		}
		if p.curTok.Type == lexer.TOKEN_RBRACE {
			p.nextToken()
		}
	}
	p.log("VO 语句解析完成")
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
		clause.Index = p.parseExpressionIterative()
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

// parseTaskParam 解析任务参数
// 语法：task(优先级)
func (p *Parser) parseTaskParam() *ast.TaskParam {
	if p.curTok.Type != lexer.TOKEN_TASK {
		return nil
	}

	p.nextToken() // 跳过 TASK token

	param := &ast.TaskParam{
		Priority: nil,
	}

	// 解析优先级
	if p.curTok.Type == lexer.TOKEN_LPAREN {
		p.nextToken()

		// 解析优先级表达式
		param.Priority = p.parseExpressionIterative()

		if p.curTok.Type == lexer.TOKEN_RPAREN {
			p.nextToken()
		}
	}

	return param
}

// parseAsyncParam 解析异步参数
// 语法：async(值)
func (p *Parser) parseAsyncParam() *ast.AsyncParam {
	if p.curTok.Type != lexer.TOKEN_ASYNC {
		return nil
	}

	p.nextToken() // 跳过 ASYNC token

	param := &ast.AsyncParam{
		Value: nil,
	}

	// 解析值
	if p.curTok.Type == lexer.TOKEN_LPAREN {
		p.nextToken()

		// 解析值表达式
		param.Value = p.parseExpressionIterative()

		if p.curTok.Type == lexer.TOKEN_RPAREN {
			p.nextToken()
		}
	}

	return param
}

// parseTaskStatementIterative 迭代解析 task 语句
func (p *Parser) parseTaskStatementIterative() *ast.TaskStatement {
	stmt := &ast.TaskStatement{}
	p.nextToken()
	if p.curTok.Type == lexer.TOKEN_LPAREN {
		p.nextToken()
		if p.curTok.Type == lexer.TOKEN_LITERAL_INT {
			priority, err := strconv.Atoi(p.curTok.Value)
			if err == nil {
				stmt.Priority = priority
			}
			p.nextToken()
		}
		if p.curTok.Type == lexer.TOKEN_COMMA {
			p.nextToken()
			stmt.Func = p.parseExpressionIterative()
			if p.curTok.Type == lexer.TOKEN_COMMA {
				p.nextToken()
				stmt.Arg = p.parseExpressionIterative()
			}
		}
		if p.curTok.Type == lexer.TOKEN_RPAREN {
			p.nextToken()
		}
	}
	return stmt
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
		for p.curTok.Type != lexer.TOKEN_RBRACE {
			bodyStmt := p.parseStatementIterative()
			if bodyStmt != nil {
				stmt.Body = append(stmt.Body, bodyStmt)
			}
			if p.curTok.Type != lexer.TOKEN_RBRACE {
				// 检查当前 token 是否是 IDENT 且下一个 token 是 ASSIGN
				// 如果是，说明这是下一个赋值语句的开始，不要调用 nextToken()
				if p.curTok.Type == lexer.TOKEN_IDENT && p.peekTok.Type == lexer.TOKEN_ASSIGN {
					// 跳过 nextToken()，直接继续循环
					continue
				}
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
	if p.curTok.Type == lexer.TOKEN_ATTRIBUTE {
		annotationValue := p.curTok.Value
		annotationContent := strings.TrimPrefix(annotationValue, "#[")
		annotationContent = strings.TrimSuffix(annotationContent, "]")

		stmt.Annotation = ast.ParseTreeAnnotation(annotationContent)

		p.log("解析 tree 注解：%s -> annotation=%v", annotationContent, stmt.Annotation)
		p.nextToken()
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

// parseFunctionAnnotations 解析函数注解
func (p *Parser) parseFunctionAnnotations(stmt *ast.FunctionStatement) *ast.FunctionStatement {
	if p.curTok.Type == lexer.TOKEN_ATTRIBUTE {
		annotationValue := p.curTok.Value
		annotationContent := strings.TrimPrefix(annotationValue, "#[")
		annotationContent = strings.TrimSuffix(annotationContent, "]")

		annotations := strings.Split(annotationContent, ",")
		for _, ann := range annotations {
			ann = strings.TrimSpace(ann)
			switch ann {
			case "no_kmm":
				stmt.NoKMM = true
			case "inline":
				stmt.Inline = true
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
				parsed := ast.ParseTreeAnnotation(ann)
				if parsed != ast.TreeAnnotationNone {
					stmt.Annotation = parsed
				}
			}
		}

		p.log("解析函数注解：%s -> annotation=%v, no_kmm=%v, inline=%v", annotationContent, stmt.Annotation, stmt.NoKMM, stmt.Inline)

		p.nextToken()
	}

	return stmt
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
	stmt = p.parseFunctionAnnotations(stmt)
	
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

			// 检查是否是 task(优先级) 语法
			if p.curTok.Type == lexer.TOKEN_TASK {
				// 解析 task(优先级) 任务参数
				taskParam := p.parseTaskParam()
				if taskParam != nil {
					stmt.TaskParams = append(stmt.TaskParams, taskParam)
					p.log("解析任务参数：优先级=%s", taskParam.Priority)
				}
				// 如果解析失败且 token 没有前进，手动跳过当前 token 避免死循环
				if p.curTok.Type == prevTok.Type && p.curTok.Value == prevTok.Value {
					p.nextToken()
				}
				continue
			}

			// 检查是否是 async(值) 语法
			if p.curTok.Type == lexer.TOKEN_ASYNC {
				// 解析 async(值) 异步参数
				asyncParam := p.parseAsyncParam()
				if asyncParam != nil {
					stmt.AsyncParams = append(stmt.AsyncParams, asyncParam)
					p.log("解析异步参数：值=%s", asyncParam.Value)
				}
				// 如果解析失败且 token 没有前进，手动跳过当前 token 避免死循环
				if p.curTok.Type == prevTok.Type && p.curTok.Value == prevTok.Value {
					p.nextToken()
				}
				continue
			}

			if p.curTok.Type == lexer.TOKEN_IDENT {
				// 第一个 IDENT 是类型，第二个是参数名
				typeOrName := p.curTok.Value
				p.nextToken()
				// 检查下一个 token 是否是 IDENT（参数名）
				if p.curTok.Type == lexer.TOKEN_IDENT {
					// typeOrName 是类型，curTok 是参数名
					stmt.Params = append(stmt.Params, p.curTok.Value)
					p.log("解析参数：%s (类型：%s)", p.curTok.Value, typeOrName)
					p.nextToken()
				} else {
					// 只有参数名，没有类型（可能是旧语法）
					stmt.Params = append(stmt.Params, typeOrName)
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
		p.log("函数参数解析完成，共 %d 个参数，%d 个任务参数", len(stmt.Params), len(stmt.TaskParams))
		if p.curTok.Type == lexer.TOKEN_RPAREN {
			p.nextToken()
		}
	}
	if p.curTok.Type == lexer.TOKEN_COLON {
		p.nextToken()
	}
	if p.curTok.Type == lexer.TOKEN_LBRACE {
		p.nextToken()
		maxStatements := 10000 // 限制最大语句数量
		statementCount := 0
		for p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
			bodyStmt := p.parseStatementIterative()
			if bodyStmt != nil {
				stmt.Body = append(stmt.Body, bodyStmt)
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
		// 消费 RBRACE
		if p.curTok.Type == lexer.TOKEN_RBRACE {
			p.nextToken()
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
	p.nextToken()
	// 解析 if 条件表达式：需要解析到 LBRACE 之前的所有表达式
	// 尝试解析完整表达式（可能包含括号和后续运算符）
	stmt.Condition = p.parseExpressionIterative()
	
	// 如果解析后遇到 RPAREN，说明有括号，需要跳过
	if p.curTok.Type == lexer.TOKEN_RPAREN {
		p.nextToken()
	}
	if p.curTok.Type == lexer.TOKEN_LBRACE {
		p.nextToken()
		for p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
			bodyStmt := p.parseStatementIterative()
			if bodyStmt != nil {
				stmt.Body = append(stmt.Body, bodyStmt)
			}
			if p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
				// 检查当前 token 是否是语句开头（IDENT、类型关键字、WHILE 等）
				// 如果是，说明这是下一个语句的开始，不要调用 nextToken()
				isStmtStart := (p.curTok.Type == lexer.TOKEN_IDENT && p.peekTok.Type == lexer.TOKEN_ASSIGN) ||
					p.curTok.Type == lexer.TOKEN_TYPE_INT ||
					p.curTok.Type == lexer.TOKEN_TYPE_FLOAT ||
					p.curTok.Type == lexer.TOKEN_TYPE_DOUBLE ||
					p.curTok.Type == lexer.TOKEN_TYPE_BOOL ||
					p.curTok.Type == lexer.TOKEN_WHILE ||
					p.curTok.Type == lexer.TOKEN_FOR ||
					p.curTok.Type == lexer.TOKEN_IF ||
					p.curTok.Type == lexer.TOKEN_RETURN ||
					p.curTok.Type == lexer.TOKEN_PRINTLN
				if isStmtStart {
					// 跳过 nextToken()，直接继续循环
					continue
				}
				p.nextToken()
			}
		}
		if p.curTok.Type == lexer.TOKEN_RBRACE {
			p.nextToken()
		}
	}
	if p.curTok.Type == lexer.TOKEN_ELSE {
		p.nextToken()
		if p.curTok.Type == lexer.TOKEN_LBRACE {
			p.nextToken()
			for p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
				bodyStmt := p.parseStatementIterative()
				if bodyStmt != nil {
					stmt.Else = append(stmt.Else, bodyStmt)
				}
				if p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
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
	// 检查是否有括号
	if p.curTok.Type == lexer.TOKEN_LPAREN {
		p.nextToken()
		stmt.Condition = p.parseExpressionIterative()
		if p.curTok.Type == lexer.TOKEN_RPAREN {
			p.nextToken()
		}
	} else {
		// 没有括号，直接解析条件表达式
		stmt.Condition = p.parseExpressionIterative()
	}
	if p.curTok.Type == lexer.TOKEN_LBRACE {
		p.nextToken()
		for p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
			bodyStmt := p.parseStatementIterative()
			if bodyStmt != nil {
				stmt.Body = append(stmt.Body, bodyStmt)
			}
			if p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
				// 检查当前 token 是否是语句开头（IDENT、类型关键字、WHILE 等）
				// 如果是，说明这是下一个语句的开始，不要调用 nextToken()
				isStmtStart := (p.curTok.Type == lexer.TOKEN_IDENT && p.peekTok.Type == lexer.TOKEN_ASSIGN) ||
					p.curTok.Type == lexer.TOKEN_TYPE_INT ||
					p.curTok.Type == lexer.TOKEN_TYPE_FLOAT ||
					p.curTok.Type == lexer.TOKEN_TYPE_DOUBLE ||
					p.curTok.Type == lexer.TOKEN_TYPE_BOOL ||
					p.curTok.Type == lexer.TOKEN_WHILE ||
					p.curTok.Type == lexer.TOKEN_FOR ||
					p.curTok.Type == lexer.TOKEN_IF ||
					p.curTok.Type == lexer.TOKEN_RETURN ||
					p.curTok.Type == lexer.TOKEN_PRINTLN
				if isStmtStart {
					// 跳过 nextToken()，直接继续循环
					continue
				}
				p.nextToken()
			}
		}
		if p.curTok.Type == lexer.TOKEN_RBRACE {
			p.nextToken()
		}
	}
	return stmt
}

// parseForStatementIterative 迭代解析 for 语句
func (p *Parser) parseForStatementIterative() *ast.ForStatement {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	stmt := &ast.ForStatement{
		Body: []ast.Statement{},
		Pos:  pos,
	}
	p.nextToken()
	if p.curTok.Type == lexer.TOKEN_LPAREN {
		p.nextToken()
		stmt.Init = p.parseStatementIterative()
		if p.curTok.Type == lexer.TOKEN_SEMICOLON {
			p.nextToken()
		}
		stmt.Condition = p.parseExpressionIterative()
		if p.curTok.Type == lexer.TOKEN_SEMICOLON {
			p.nextToken()
		}
		stmt.Update = p.parseStatementIterative()
		if p.curTok.Type == lexer.TOKEN_RPAREN {
			p.nextToken()
		}
	}
	if p.curTok.Type == lexer.TOKEN_LBRACE {
		p.nextToken()
		for p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
			bodyStmt := p.parseStatementIterative()
			if bodyStmt != nil {
				stmt.Body = append(stmt.Body, bodyStmt)
			}
			if p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
				p.nextToken()
			}
		}
		if p.curTok.Type == lexer.TOKEN_RBRACE {
			p.nextToken()
		}
	}
	return stmt
}

// parseSwitchStatementIterative 迭代解析 switch 语句
func (p *Parser) parseSwitchStatementIterative() *ast.SwitchStatement {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	stmt := &ast.SwitchStatement{
		Statements: []ast.Statement{},
		Cases: []ast.CaseStatement{},
		Default: []ast.Statement{},
		Pos:    pos,
	}
	p.nextToken()
	if p.curTok.Type == lexer.TOKEN_LPAREN {
		p.nextToken()
		stmt.Expression = p.parseExpressionIterative()
		if p.curTok.Type == lexer.TOKEN_RPAREN {
			p.nextToken()
		}
	}
	if p.curTok.Type == lexer.TOKEN_LBRACE {
		p.nextToken()
		for p.curTok.Type != lexer.TOKEN_RBRACE {
			if p.curTok.Type == lexer.TOKEN_CASE {
				caseStmt := p.parseCaseStatementIterative()
				stmt.Cases = append(stmt.Cases, *caseStmt)
			} else if p.curTok.Type == lexer.TOKEN_DEFAULT {
				p.nextToken()
				if p.curTok.Type == lexer.TOKEN_COLON {
					p.nextToken()
					for p.curTok.Type != lexer.TOKEN_CASE && p.curTok.Type != lexer.TOKEN_DEFAULT && p.curTok.Type != lexer.TOKEN_RBRACE {
						bodyStmt := p.parseStatementIterative()
						if bodyStmt != nil {
							stmt.Default = append(stmt.Default, bodyStmt)
						}
						if p.curTok.Type != lexer.TOKEN_CASE && p.curTok.Type != lexer.TOKEN_DEFAULT && p.curTok.Type != lexer.TOKEN_RBRACE {
							p.nextToken()
						}
					}
				}
			} else {
				bodyStmt := p.parseStatementIterative()
				if bodyStmt != nil {
					stmt.Statements = append(stmt.Statements, bodyStmt)
				}
				if p.curTok.Type != lexer.TOKEN_RBRACE {
					p.nextToken()
				}
			}
		}
		if p.curTok.Type == lexer.TOKEN_RBRACE {
			p.nextToken()
		}
	}
	return stmt
}

// parseCaseStatementIterative 迭代解析 case 语句
func (p *Parser) parseCaseStatementIterative() *ast.CaseStatement {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	stmt := &ast.CaseStatement{
		Body: []ast.Statement{},
		Pos:  pos,
	}
	p.nextToken()
	stmt.Value = p.parseExpressionIterative()
	if p.curTok.Type == lexer.TOKEN_COLON {
		p.nextToken()
		for p.curTok.Type != lexer.TOKEN_CASE && p.curTok.Type != lexer.TOKEN_DEFAULT && p.curTok.Type != lexer.TOKEN_RBRACE {
			bodyStmt := p.parseStatementIterative()
			if bodyStmt != nil {
				stmt.Body = append(stmt.Body, bodyStmt)
			}
			if p.curTok.Type != lexer.TOKEN_CASE && p.curTok.Type != lexer.TOKEN_DEFAULT && p.curTok.Type != lexer.TOKEN_RBRACE {
				p.nextToken()
			}
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

// parseImportStatementIterative 迭代解析 import 语句
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
	if p.curTok.Type == lexer.TOKEN_IDENT {
		stmt.Module = p.curTok.Value
		p.nextToken()
		for p.curTok.Type == lexer.TOKEN_DOT {
			p.nextToken()
			if p.curTok.Type == lexer.TOKEN_IDENT {
				stmt.Module += "." + p.curTok.Value
				p.nextToken()
			} else {
				break
			}
		}
	}
	return stmt
}

// parseExportStatementIterative 迭代解析 export 语句
func (p *Parser) parseExportStatementIterative() *ast.ExportStatement {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	stmt := &ast.ExportStatement{
		Pos: pos,
	}
	
	// 消耗 export 关键字
	p.nextToken()
	
	// 解析导出类型（可选）
	if p.curTok.Type == lexer.TOKEN_IDENT {
		// 检查是否是类型关键字
		lookahead := p.peekTok
		if lookahead.Type == lexer.TOKEN_IDENT || lookahead.Type == lexer.TOKEN_LPAREN {
			// 可能是导出函数：export fn name()
			switch p.curTok.Value {
			case "fn", "func", "function":
				stmt.Type = "function"
				p.nextToken()
			case "class":
				stmt.Type = "class"
				p.nextToken()
			case "obj", "object":
				stmt.Type = "object"
				p.nextToken()
			case "var", "let", "const":
				stmt.Type = "variable"
				p.nextToken()
			default:
				// 没有类型，直接是名称
				stmt.Type = "function" // 默认是函数
			}
		} else {
			// 直接是名称
			stmt.Type = "function" // 默认是函数
		}
	}
	
	// 解析导出名称
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
	if p.curTok.Type == lexer.TOKEN_IDENT {
		stmt.Type = p.curTok.Value
		p.nextToken()
	}
	if p.curTok.Type == lexer.TOKEN_IDENT {
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
		Fields:      make([]*ast.FieldDeclaration, 0, 16),
		Methods:     make([]*ast.MethodStatement, 0, 16),
		Constructors: make([]*ast.ConstructorStatement, 0, 4),
		Implements:  make([]string, 0, 4),
		Pos:         pos,
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
			
			if p.curTok.Type == lexer.TOKEN_IDENT {
				savedCurTok := p.curTok
				savedPeekTok := p.peekTok
				
				if field := p.parseFieldDeclarationIterative(); field != nil {
					p.log("解析完成字段声明：%s", field.String())
					stmt.Fields = append(stmt.Fields, field)
				} else {
					p.curTok = savedCurTok
					p.peekTok = savedPeekTok
					
					if method := p.parseMethodStatementIterative(); method != nil {
						p.log("解析完成方法声明：%s", method.String())
						stmt.Methods = append(stmt.Methods, method)
					} else {
						p.curTok = savedCurTok
						p.peekTok = savedPeekTok
						
						if p.curTok.Type == lexer.TOKEN_IDENT && p.curTok.Value == stmt.Name {
							p.log("开始解析构造函数")
							constructor := p.parseConstructorStatementIterative()
							if constructor != nil {
								p.log("解析完成构造函数")
								stmt.Constructors = append(stmt.Constructors, constructor)
							}
						} else {
							p.log("跳过 token: %s", lexer.TokenTypeToString(p.curTok.Type))
							p.nextToken()
						}
					}
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
			if p.curTok.Type == lexer.TOKEN_IDENT {
				if method := p.parseMethodStatementIterative(); method != nil {
					stmt.Methods = append(stmt.Methods, method)
					continue
				}
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
			prevTok := p.curTok
			
			if field := p.parseFieldDeclarationIterative(); field != nil {
				stmt.Fields = append(stmt.Fields, field)
			} else if p.curTok.Type == lexer.TOKEN_SEMICOLON {
				p.nextToken()
			} else {
				p.nextToken()
				// 如果 nextToken 后 token 没变，说明无法解析，跳出循环避免死循环
				if p.curTok.Type == prevTok.Type && p.curTok.Value == prevTok.Value {
					break
				}
			}
		}
	}
	p.log("结构体解析完成：%s, 字段数：%d", stmt.Name, len(stmt.Fields))
	return stmt
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
	
	// 尝试解析 "字段名：类型，" 语法（类 C 风格）
	fieldName := p.curTok.Value
	p.nextToken()
	
	// 检查下一个 token 是否是冒号
	if p.curTok.Type != lexer.TOKEN_COLON {
		// 不是字段声明，恢复 token 位置
		p.curTok = savedCurTok
		p.peekTok = savedPeekTok
		return nil
	}
	
	// 跳过冒号
	p.nextToken()
	
	// 解析类型
	if p.curTok.Type != lexer.TOKEN_IDENT {
		p.curTok = savedCurTok
		p.peekTok = savedPeekTok
		return nil
	}
	
	typeName := p.curTok.Value
	p.nextToken()
	
	// 检查是否是逗号或分号
	if p.curTok.Type != lexer.TOKEN_COMMA && p.curTok.Type != lexer.TOKEN_SEMICOLON {
		p.curTok = savedCurTok
		p.peekTok = savedPeekTok
		return nil
	}
	
	// 跳过分隔符
	p.nextToken()
	
	field := &ast.FieldDeclaration{
		Name:     fieldName,
		Type:     typeName,
		Nullable: false,
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
	if p.curTok.Type != lexer.TOKEN_IDENT {
		p.log("不是方法声明，返回 nil")
		p.curTok = savedCurTok
		p.peekTok = savedPeekTok
		return nil
	}
	method.ReturnType = p.curTok.Value
	p.log("解析返回类型：%s", p.curTok.Value)
	p.nextToken()
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
	if p.curTok.Type != lexer.TOKEN_RPAREN {
		for p.curTok.Type != lexer.TOKEN_RPAREN {
			p.log("解析参数，当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
			if p.curTok.Type != lexer.TOKEN_IDENT {
				p.log("不是方法声明，返回 nil")
				p.curTok = savedCurTok
				p.peekTok = savedPeekTok
				return nil
			}
			param := &ast.Param{}
			param.Type = p.curTok.Value
			p.log("跳过参数类型：%s", p.curTok.Value)
			p.nextToken()
			p.log("当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
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

parseMethodBody:
	p.log("解析方法体或分号，当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
	if p.curTok.Type == lexer.TOKEN_SEMICOLON {
		p.log("接口方法声明，跳过分号")
		p.nextToken()
	} else if p.curTok.Type == lexer.TOKEN_LBRACE {
		p.log("跳过 LBRACE token")
		p.nextToken()
		p.log("当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
		for p.curTok.Type != lexer.TOKEN_RBRACE {
			bodyStmt := p.parseStatementIterative()
			if bodyStmt != nil {
				method.Body = append(method.Body, bodyStmt)
			}
			if p.curTok.Type != lexer.TOKEN_RBRACE {
				p.nextToken()
				p.log("当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
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
	if p.curTok.Type != lexer.TOKEN_IDENT {
		p.log("不是构造函数声明，返回 nil")
		return nil
	}
	constructorName := p.curTok.Value
	p.log("解析构造函数名：%s", constructorName)
	p.nextToken()
	p.log("当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
	if p.curTok.Type == lexer.TOKEN_LPAREN {
		p.log("跳过 LPAREN token")
		p.nextToken()
		p.log("当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
		for p.curTok.Type != lexer.TOKEN_RPAREN {
			p.log("解析参数，当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
			if p.curTok.Type != lexer.TOKEN_IDENT {
				p.error(fmt.Sprintf("expected type name, got %s", lexer.TokenTypeToString(p.curTok.Type)))
				break
			}
			param := &ast.Param{}
			param.Type = p.curTok.Value
			p.log("跳过参数类型：%s", p.curTok.Value)
			p.nextToken()
			p.log("当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
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
	} else {
		p.error(fmt.Sprintf("expected (, got %s", lexer.TokenTypeToString(p.curTok.Type)))
		return nil
	}
	if p.curTok.Type == lexer.TOKEN_LBRACE {
		p.log("跳过 LBRACE token")
		p.nextToken()
		p.log("当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
		for p.curTok.Type != lexer.TOKEN_RBRACE {
			bodyStmt := p.parseStatementIterative()
			if bodyStmt != nil {
				constructor.Body = append(constructor.Body, bodyStmt)
			}
			if p.curTok.Type != lexer.TOKEN_RBRACE {
				p.nextToken()
				p.log("当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
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
		op := p.curTok.Type
		opPrecedence := p.precedence(op)
		
		// 如果没有运算符或优先级不够高，退出循环
		if opPrecedence == 0 || precedence >= opPrecedence {
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
		left = &ast.BinaryExpression{
			Left:     left,
			Operator: lexer.TokenTypeToString(op),
			Right:    right,
		}
	}
	
	return left
}

// precedences 运算符优先级表
var precedences = map[lexer.TokenType]int{
	lexer.TOKEN_ASSIGN: 1,
	lexer.TOKEN_OR:     2,
	lexer.TOKEN_AND:    3,
	lexer.TOKEN_EQ:     4,
	lexer.TOKEN_NE:     4,
	lexer.TOKEN_LT:     5,
	lexer.TOKEN_GT:     5,
	lexer.TOKEN_LE:     5,
	lexer.TOKEN_GE:     5,
	lexer.TOKEN_PLUS:   6,
	lexer.TOKEN_MINUS:  6,
	lexer.TOKEN_MULTIPLY: 7,
	lexer.TOKEN_DIVIDE: 7,
	lexer.TOKEN_MOD:    7,
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
		return p.parseIdentifierIterative()
	case lexer.TOKEN_LITERAL_INT:
		return p.parseIntegerLiteralIterative()
	case lexer.TOKEN_LITERAL_FLOAT:
		return p.parseFloatLiteralIterative()
	case lexer.TOKEN_STRING:
		return p.parseStringLiteralIterative()
	case lexer.TOKEN_LPAREN:
		return p.parseGroupedExpressionIterative()
	case lexer.TOKEN_LBRACKET:
		return p.parseIndexExpressionIterative()
	case lexer.TOKEN_PREFIX_REF:
		p.nextToken()
		if p.curTok.Type == lexer.TOKEN_IDENT {
			ident := &ast.Identifier{
				Name: "$" + p.curTok.Value,
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
		if p.curTok.Type == lexer.TOKEN_LPAREN {
			return p.parseCallExpressionIterative(ident)
		}
		return ident
	case lexer.TOKEN_VO:
		ident := &ast.Identifier{
			Name: p.curTok.Value,
		}
		p.nextToken()
		if p.curTok.Type == lexer.TOKEN_DOT {
			p.nextToken()
			if p.curTok.Type == lexer.TOKEN_IDENT {
				memberIdent := &ast.Identifier{
					Name: p.curTok.Value,
				}
				p.nextToken()
				if p.curTok.Type == lexer.TOKEN_LPAREN {
					return p.parseCallExpressionIterative(memberIdent)
				}
				return memberIdent
			}
		}
		return ident
	case lexer.TOKEN_SELF:
		ident := &ast.Identifier{
			Name: p.curTok.Value,
		}
		p.nextToken()
		return ident
	case lexer.TOKEN_NULL:
		ident := &ast.Identifier{
			Name: p.curTok.Value,
		}
		p.nextToken()
		return ident
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
	case lexer.TOKEN_RBRACE, lexer.TOKEN_LBRACE, lexer.TOKEN_DOT, lexer.TOKEN_ASSIGN, lexer.TOKEN_LT, lexer.TOKEN_GT:
		return nil
	default:
		p.error(fmt.Sprintf("unexpected token: %s", lexer.TokenTypeToString(p.curTok.Type)))
		p.nextToken()
		return nil
	}
}

// parseIdentifierIterative 迭代解析标识符（支持多级成员访问）
func (p *Parser) parseIdentifierIterative() ast.Expression {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	
	// 使用 Expression 接口类型，支持 Identifier 和 MemberAccessExpression
	var expr ast.Expression
	expr = &ast.Identifier{
		Name: p.curTok.Value,
		Pos:  pos,
	}
	p.nextToken()
	
	// 循环处理多级成员访问（如 std.io.println）
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
		} else {
			break
		}
	}
	
	// 检查是否是函数调用（没有成员访问的情况）
	if p.curTok.Type == lexer.TOKEN_LPAREN {
		return p.parseCallExpressionIterative(expr)
	}
	
	return expr
}

// parseIntegerLiteralIterative 迭代解析整数字面量
func (p *Parser) parseIntegerLiteralIterative() *ast.IntegerLiteral {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	value, err := strconv.ParseInt(p.curTok.Value, 10, 64)
	if err != nil {
		p.error(fmt.Sprintf("invalid integer literal: %s", p.curTok.Value))
		return &ast.IntegerLiteral{Value: 0, Pos: pos}
	}
	literal := &ast.IntegerLiteral{Value: value, Pos: pos}
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

// parseCallExpressionIterative 迭代解析函数调用表达式
func (p *Parser) parseCallExpressionIterative(function ast.Expression) ast.Expression {
	call := &ast.CallExpression{
		Function: function,
		Args:     []ast.Expression{},
	}
	// 当前 token 是 LPAREN，跳过它
	if p.curTok.Type != lexer.TOKEN_LPAREN {
		return nil
	}
	p.nextToken()
	
	// 解析泛型参数（如果存在）
	if p.curTok.Type == lexer.TOKEN_LT {
		p.nextToken()
		for p.curTok.Type == lexer.TOKEN_IDENT {
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
	for p.curTok.Type != lexer.TOKEN_RPAREN && p.curTok.Type != lexer.TOKEN_EOF {
		prevTok := p.curTok
		
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
	return index
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

// error 报告错误
func (p *Parser) error(message string) {
	suggestion := errors.GenerateSuggestion(message)
	p.errorCollector.AddSyntaxError(message, p.curTok.Line, p.curTok.Column, p.file, suggestion)
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
	
	if !hasMain {
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
		"std":        true,
		"std.base":   true,
		
		// 标准库模块（带 std. 前缀）
		"std.io":         true,
		"std.string":     true,
		"std.memory":     true,
		"std.container":  true,
		"std.math":       true,
		"std.system":     true,
		"std.vo":         true,
		"std.prefix":     true,
		"std.task":       true,
		"std.concurrent": true,
		"std.error":      true,
		"std.async":      true,
		"std.time":       true,
		
		// 兼容旧版（不带 std. 前缀）
		"io":         true,
		"string":     true,
		"memory":     true,
		"container":  true,
		"math":       true,
		"system":     true,
		"vo":         true,
		"prefix":     true,
		"task":       true,
		"concurrent": true,
		"error":      true,
		"base":       true,
		
		// 系统模块
		"windows":    true,
		"syscall":    true,
	}
	
	// 加载第三方库配置，将第三方库名称添加到有效模块列表
	// 尝试多个路径
	stdlibPaths := []string{"stdlib.json", "kaula-compiler/stdlib.json", "../stdlib.json"}
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
			fmt.Printf("Parser: Loaded %d stdlib modules and %d third-party libraries from %s\n", len(stdlibConfig.Modules), len(stdlibConfig.ThirdParty), path)
			break // 加载成功后退出
		}
	}
	
	for _, stmt := range program.Statements {
		if importStmt, ok := stmt.(*ast.ImportStatement); ok {
			if importStmt.Module == "" {
				p.error("import 语句缺少模块名称")
			} else if !validModules[importStmt.Module] {
				p.error(fmt.Sprintf("导入的模块不存在：%s", importStmt.Module))
			}
		}
	}
	
	if p.HasErrors() {
		p.log("验证完成，发现验证错误")
	} else {
		p.log("验证完成，未发现错误")
	}
}

// peekNextTokenType 获取下一个 token 的类型
func (p *Parser) peekNextTokenType() lexer.TokenType {
	return p.peekTok.Type
}
