package parser

import (
	"fmt"
	"kaula-compiler/internal/ast"
	"kaula-compiler/internal/errors"
	"kaula-compiler/internal/lexer"
	"strconv"
	"log"
	"os"
)

// Parser 表示语法分析器
type Parser struct {
	lexer  *lexer.Lexer
	curTok lexer.Token
	peekTok lexer.Token
	errorCollector *errors.ErrorCollector
	logger *log.Logger
	loggingEnabled bool
	file string
}

// NewParser 创建一个新的语法分析器
func NewParser(lexer *lexer.Lexer) *Parser {
	p := &Parser{
		lexer:  lexer,
		errorCollector: errors.NewErrorCollector(),
		logger: log.New(os.Stdout, "[Parser] ", log.LstdFlags),
		loggingEnabled: true, // 启用日志以进行调试
	}
	// 初始化当前和下一个token
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

// nextToken 前进到下一个token
func (p *Parser) nextToken() {
	p.curTok = p.peekTok
	p.peekTok = p.lexer.Next()
}

// parseProgram 解析整个程序
func (p *Parser) parseProgram() *ast.Program {
	p.log("开始解析程序")
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	program := &ast.Program{
		Statements: []ast.Statement{},
		Pos:        pos,
	}
	for p.curTok.Type != lexer.TOKEN_EOF {
		p.log("当前token: %s, 开始解析语句", lexer.TokenTypeToString(p.curTok.Type))
		stmt := p.parseStatement()
		if stmt != nil {
			program.Statements = append(program.Statements, stmt)
			p.log("解析完成语句: %s", stmt.String())
		}
		// 只有当当前token不是EOF时才继续
		if p.curTok.Type != lexer.TOKEN_EOF {
			p.nextToken()
		}
	}
	p.log("程序解析完成，共 %d 条语句", len(program.Statements))
	return program
}

// parseStatement 解析语句
func (p *Parser) parseStatement() ast.Statement {
	switch p.curTok.Type {
	case lexer.TOKEN_VO:
		return p.parseVOStatement()
	case lexer.TOKEN_SPEND:
		return p.parseSpendCallStatement()
	case lexer.TOKEN_SPEND_CALL:
		return p.parseSpendCallStatement()
	case lexer.TOKEN_CALL:
		// 处理call语句
		return p.parseCallStatement()
	case lexer.TOKEN_TASK:
		return p.parseTaskStatement()
	case lexer.TOKEN_PREFIX:
		return p.parsePrefixStatement()
	case lexer.TOKEN_TREE:
		return p.parseTreeStatement()
	case lexer.TOKEN_OBJECT:
		return p.parseObjectStatement()
	case lexer.TOKEN_FUNC:
		return p.parseFunctionStatement()
	case lexer.TOKEN_CLASS:
		return p.parseClassStatement()
	case lexer.TOKEN_INTERFACE:
		return p.parseInterfaceStatement()
	case lexer.TOKEN_STRUCT:
		return p.parseStructStatement()
	case lexer.TOKEN_IF:
		return p.parseIfStatement()
	case lexer.TOKEN_ELSE:
		// 处理else语句，这里应该由parseIfStatement处理
		return nil
	case lexer.TOKEN_WHILE:
		return p.parseWhileStatement()
	case lexer.TOKEN_FOR:
		return p.parseForStatement()
	case lexer.TOKEN_SWITCH:
		return p.parseSwitchStatement()
	case lexer.TOKEN_CASE:
		// 处理case语句，这里应该由parseSwitchStatement处理
		return nil
	case lexer.TOKEN_DEFAULT:
		// 处理default语句，这里应该由parseSwitchStatement处理
		return nil
	case lexer.TOKEN_COLON:
		// 处理冒号，这里应该由上层解析器处理
		return nil
	case lexer.TOKEN_RETURN:
		return p.parseReturnStatement()
	case lexer.TOKEN_IMPORT:
		return p.parseImportStatement()
	case lexer.TOKEN_NONLOCAL:
		return p.parseNonLocalStatement()
	case lexer.TOKEN_PRINTLN:
		// 处理println语句
		return p.parseExpressionStatement()
	case lexer.TOKEN_IDENT:
		// 检查是否是变量声明（类型后跟标识符或可空类型标记）
		if p.peekTok.Type == lexer.TOKEN_IDENT || p.peekTok.Type == lexer.TOKEN_QUESTION {
			// 尝试解析变量声明
			if stmt := p.parseVariableDeclaration(); stmt != nil {
				return stmt
			}
		}
		// 检查是否是标识符后跟左大括号，如 test{
		if p.peekTok.Type == lexer.TOKEN_LBRACE {
			// 这是一个前缀调用语句
			ident := &ast.Identifier{
				Name: p.curTok.Value,
			}
			p.nextToken() // 跳过标识符
			if p.curTok.Type == lexer.TOKEN_LBRACE {
				p.nextToken() // 跳过{
				// 解析语句块
				blockBody := []ast.Statement{}
				for p.curTok.Type != lexer.TOKEN_RBRACE {
					bodyStmt := p.parseStatement()
					if bodyStmt != nil {
						blockBody = append(blockBody, bodyStmt)
					}
					// 只有当不是右大括号时才继续
					if p.curTok.Type != lexer.TOKEN_RBRACE {
						p.nextToken()
					}
				}
				// 跳过右大括号
				if p.curTok.Type == lexer.TOKEN_RBRACE {
					p.nextToken() // 跳过}
				}
				// 创建一个前缀调用表达式
				prefixCall := &ast.PrefixCallExpression{
					Name: ident.Name,
					Body: blockBody,
				}
				// 返回一个表达式语句
				return &ast.ExpressionStatement{
					Expression: prefixCall,
				}
			}
		}
		// 其他标识符情况
		return p.parseExpressionStatement()
	case lexer.TOKEN_CONSTRUCTOR:
		// 构造函数只能在类体中出现，这里返回nil
		return nil
	case lexer.TOKEN_SEMICOLON:
		// 分号在类体和接口体中是字段声明的结束符，这里返回nil
		return nil
	default:
		return p.parseExpressionStatement()
	}
}

// parseVariableDeclaration 解析变量声明
func (p *Parser) parseVariableDeclaration() *ast.VariableDeclaration {
	stmt := &ast.VariableDeclaration{}
	// 解析类型
	if p.curTok.Type == lexer.TOKEN_IDENT {
		stmt.Type = p.curTok.Value
		p.nextToken()
		// 检查是否是可空类型
		if p.curTok.Type == lexer.TOKEN_QUESTION {
			stmt.Nullable = true
			p.nextToken()
		}
		// 解析变量名
		if p.curTok.Type == lexer.TOKEN_IDENT {
			stmt.Name = p.curTok.Value
			p.nextToken()
			// 解析赋值
			if p.curTok.Type == lexer.TOKEN_ASSIGN {
				p.nextToken()
				stmt.Value = p.parseExpression()
			}
			return stmt
		}
	}
	// 不是变量声明，回退
	return nil
}

// parseCallStatement 解析call语句
func (p *Parser) parseCallStatement() *ast.CallStatement {
	p.log("开始解析call语句")
	stmt := &ast.CallStatement{}
	p.nextToken() // 跳过CALL
	if p.curTok.Type == lexer.TOKEN_LPAREN {
		p.nextToken() // 跳过(
		stmt.Target = p.parseExpression()
		p.log("解析call目标")
		if p.curTok.Type == lexer.TOKEN_RPAREN {
			p.nextToken() // 跳过)
		}
	}
	// 解析冒号
	if p.curTok.Type == lexer.TOKEN_COLON {
		p.nextToken() // 跳过:
		// 解析语句块
		callBody := []ast.Statement{}
		p.log("开始解析call语句体")
		// 处理没有花括号的情况
		bodyStmt := p.parseStatement()
		if bodyStmt != nil {
			callBody = append(callBody, bodyStmt)
			p.log("call语句体添加语句")
		}
		// 处理有花括号的情况
		if p.curTok.Type == lexer.TOKEN_LBRACE {
			p.nextToken() // 跳过{
			for p.curTok.Type != lexer.TOKEN_RBRACE {
				// 解析语句，而不是直接解析表达式
				bodyStmt := p.parseStatement()
				if bodyStmt != nil {
					callBody = append(callBody, bodyStmt)
					p.log("call语句体添加语句")
				}
				// 只有当不是右大括号时才继续
				if p.curTok.Type != lexer.TOKEN_RBRACE {
					p.nextToken()
				}
			}
			if p.curTok.Type == lexer.TOKEN_RBRACE {
				p.nextToken() // 跳过}
			}
		}
		stmt.Body = callBody
		p.log("call语句体解析完成，共 %d 条语句", len(callBody))
	}
	p.log("call语句解析完成")
	return stmt
}

// parseVOStatement 解析VO语句
func (p *Parser) parseVOStatement() ast.Statement {
	// 检查是否是vo.data_load这样的表达式
	if p.peekTok.Type == lexer.TOKEN_DOT {
		// 这是一个表达式语句，不是VO语句
		p.nextToken() // 跳过VO
		// 解析点操作符
		p.nextToken() // 跳过.
		if p.curTok.Type == lexer.TOKEN_IDENT {
			// 创建成员访问表达式
			memberIdent := &ast.Identifier{
				Name: p.curTok.Value,
			}
			p.nextToken()
			// 检查是否是函数调用
			if p.curTok.Type == lexer.TOKEN_LPAREN {
				return &ast.ExpressionStatement{
					Expression: p.parseCallExpression(memberIdent),
				}
			}
			// 检查是否是空格后跟着标识符（如 vo.data_load  a）
			if p.curTok.Type == lexer.TOKEN_IDENT {
				bodyStmt := p.parseStatement()
				if bodyStmt != nil {
					return bodyStmt
				}
			}
			return &ast.ExpressionStatement{
				Expression: memberIdent,
			}
		}
	}
	
	p.log("开始解析VO语句")
	stmt := &ast.VOStatement{}
	p.nextToken() // 跳过VO
	// 解析操作类型 (create)
	if p.curTok.Type == lexer.TOKEN_LPAREN {
		p.nextToken() // 跳过(
		if p.curTok.Type == lexer.TOKEN_IDENT {
			// 操作类型
			p.nextToken() // 跳过操作类型
		}
		if p.curTok.Type == lexer.TOKEN_RPAREN {
			p.nextToken() // 跳过)
		}
	}
	// 解析沙箱名称
	if p.curTok.Type == lexer.TOKEN_IDENT {
		p.nextToken() // 跳过沙箱名称
	}
	// 解析self参数
	if p.curTok.Type == lexer.TOKEN_SELF {
		p.nextToken() // 跳过self
		if p.curTok.Type == lexer.TOKEN_LPAREN {
			p.nextToken() // 跳过(
			// 解析参数
			for p.curTok.Type != lexer.TOKEN_RPAREN {
				if p.curTok.Type == lexer.TOKEN_IDENT {
					p.nextToken() // 跳过参数名
					if p.curTok.Type == lexer.TOKEN_ASSIGN {
						p.nextToken() // 跳过=
						p.parseExpression() // 解析值
					}
				} else if p.curTok.Type == lexer.TOKEN_COMMA {
					p.nextToken() // 跳过,
				} else {
					p.nextToken() // 跳过其他
				}
			}
			if p.curTok.Type == lexer.TOKEN_RPAREN {
				p.nextToken() // 跳过)
			}
		}
	}
	// 解析函数体
	if p.curTok.Type == lexer.TOKEN_LBRACE {
		p.nextToken() // 跳过{
		// 解析函数体内的语句
		for p.curTok.Type != lexer.TOKEN_RBRACE {
			bodyStmt := p.parseStatement()
			if bodyStmt != nil {
				// 这里可以将bodyStmt添加到VO语句的body中
			}
			// 只有当不是右大括号时才继续
			if p.curTok.Type != lexer.TOKEN_RBRACE {
				p.nextToken()
			}
		}
		// 跳过右大括号
		if p.curTok.Type == lexer.TOKEN_RBRACE {
			p.nextToken() // 跳过}
		}
	}
	p.log("VO语句解析完成")
	return stmt
}



// parseSpendCallStatement 解析spend/call语句
func (p *Parser) parseSpendCallStatement() *ast.SpendCallStatement {
	p.log("开始解析spend/call语句")
	stmt := &ast.SpendCallStatement{
		Calls: []ast.CallStatement{},
	}
	p.nextToken() // 跳过SPEND
	if p.curTok.Type == lexer.TOKEN_LPAREN {
		p.nextToken() // 跳过(
		stmt.Spend = p.parseExpression()
		p.log("解析spend表达式")
		if p.curTok.Type == lexer.TOKEN_RPAREN {
			p.nextToken() // 跳过)
		}
	}
	// 解析花括号块
	if p.curTok.Type == lexer.TOKEN_LBRACE {
		p.nextToken() // 跳过{
		// 解析call语句
		p.log("开始解析call语句")
		for p.curTok.Type != lexer.TOKEN_RBRACE {
			if p.curTok.Type == lexer.TOKEN_CALL {
				// 使用parseCallStatement函数解析call语句
				callStmt := p.parseCallStatement()
				stmt.Calls = append(stmt.Calls, *callStmt)
				p.log("添加call语句")
			} else {
				// 处理其他语句
				bodyStmt := p.parseStatement()
				if bodyStmt != nil {
					// 这里可以将其他语句添加到适当的位置
				}
			}
			// 只有当不是右大括号时才继续
			if p.curTok.Type != lexer.TOKEN_RBRACE {
				p.nextToken()
			}
		}
		p.log("call语句解析完成，共 %d 个call", len(stmt.Calls))
		// 跳过右大括号
		if p.curTok.Type == lexer.TOKEN_RBRACE {
			p.nextToken() // 跳过}
		}
	}
	p.log("spend/call语句解析完成")
	return stmt
}

// parseTaskStatement 解析task语句
func (p *Parser) parseTaskStatement() *ast.TaskStatement {
	stmt := &ast.TaskStatement{}
	p.nextToken() // 跳过TASK
	if p.curTok.Type == lexer.TOKEN_LPAREN {
		p.nextToken() // 跳过(
		// 解析优先级
		if p.curTok.Type == lexer.TOKEN_INT {
			priority, err := strconv.Atoi(p.curTok.Value)
			if err == nil {
				stmt.Priority = priority
			}
			p.nextToken() // 跳过优先级
		}
		if p.curTok.Type == lexer.TOKEN_COMMA {
			p.nextToken() // 跳过,
			// 解析函数
			stmt.Func = p.parseExpression()
			if p.curTok.Type == lexer.TOKEN_COMMA {
				p.nextToken() // 跳过,
				// 解析参数
				stmt.Arg = p.parseExpression()
			}
		}
		if p.curTok.Type == lexer.TOKEN_RPAREN {
			p.nextToken() // 跳过)
		}
	}
	return stmt
}

// parsePrefixStatement 解析prefix语句
func (p *Parser) parsePrefixStatement() *ast.PrefixStatement {
	stmt := &ast.PrefixStatement{
		Body: []ast.Statement{},
	}
	p.nextToken() // 跳过PREFIX
	if p.curTok.Type == lexer.TOKEN_IDENT {
		stmt.Name = p.curTok.Value
		p.nextToken() // 跳过前缀名
	} else if p.curTok.Type == lexer.TOKEN_STRING {
		stmt.Name = p.curTok.Value
		p.nextToken() // 跳过前缀名字符串
	}
	// 解析花括号块
	if p.curTok.Type == lexer.TOKEN_LBRACE {
		p.nextToken() // 跳过{
		// 解析前缀体
		for p.curTok.Type != lexer.TOKEN_RBRACE {
			bodyStmt := p.parseStatement()
			if bodyStmt != nil {
				stmt.Body = append(stmt.Body, bodyStmt)
			}
			// 只有当不是右大括号时才继续
			if p.curTok.Type != lexer.TOKEN_RBRACE {
				p.nextToken()
			}
		}
		if p.curTok.Type == lexer.TOKEN_RBRACE {
			p.nextToken() // 跳过}
		}
	}
	return stmt
}

// parseTreeStatement 解析tree语句
func (p *Parser) parseTreeStatement() *ast.TreeStatement {
	stmt := &ast.TreeStatement{}
	p.nextToken() // 跳过TREE
	if p.curTok.Type == lexer.TOKEN_LPAREN {
		p.nextToken() // 跳过(
		stmt.Root = p.parseExpression()
		if p.curTok.Type == lexer.TOKEN_RPAREN {
			p.nextToken() // 跳过)
		}
	}
	return stmt
}

// parseObjectStatement 解析object语句
func (p *Parser) parseObjectStatement() *ast.ObjectStatement {
	stmt := &ast.ObjectStatement{
		Fields: []ast.Expression{},
	}
	p.nextToken() // 跳过OBJECT
	// 解析类型
	if p.curTok.Type == lexer.TOKEN_IDENT {
		stmt.Type = p.curTok.Value
		p.nextToken() // 跳过类型
	}
	// 解析名称
	if p.curTok.Type == lexer.TOKEN_IDENT {
		stmt.Name = p.curTok.Value
		p.nextToken() // 跳过名称
	}
	// 解析self参数
	if p.curTok.Type == lexer.TOKEN_SELF {
		p.nextToken() // 跳过self
		if p.curTok.Type == lexer.TOKEN_LPAREN {
			p.nextToken() // 跳过(
			// 解析字段
			for p.curTok.Type != lexer.TOKEN_RPAREN {
				field := p.parseExpression()
				stmt.Fields = append(stmt.Fields, field)
				if p.curTok.Type == lexer.TOKEN_COMMA {
					p.nextToken() // 跳过,
				}
			}
			if p.curTok.Type == lexer.TOKEN_RPAREN {
				p.nextToken() // 跳过)
			}
		}
		// 解析self后面的花括号块
		if p.curTok.Type == lexer.TOKEN_LBRACE {
			p.nextToken() // 跳过{
			// 解析花括号内的字段初始化
			for p.curTok.Type != lexer.TOKEN_RBRACE {
				// 解析字段名
				if p.curTok.Type == lexer.TOKEN_IDENT {
					p.nextToken() // 跳过字段名
					// 跳过冒号
					if p.curTok.Type == lexer.TOKEN_COLON {
						p.nextToken() // 跳过:
						// 解析字段值
						if p.curTok.Type == lexer.TOKEN_STRING || p.curTok.Type == lexer.TOKEN_INT || p.curTok.Type == lexer.TOKEN_FLOAT {
							p.parsePrimaryExpression()
						} else {
							p.parseExpression()
						}
						// 跳过逗号
						if p.curTok.Type == lexer.TOKEN_COMMA {
							p.nextToken() // 跳过,
						}
					}
				} else {
					// 跳过其他token
					p.nextToken()
				}
			}
			if p.curTok.Type == lexer.TOKEN_RBRACE {
				p.nextToken() // 跳过}
			}
		}
	}
	// 解析赋值
	if p.curTok.Type == lexer.TOKEN_ASSIGN {
		p.nextToken() // 跳过=
		// 解析值
		stmt.Value = p.parseExpression()
	}
	// 解析:: [this.obj1]
	if p.curTok.Type == lexer.TOKEN_DOUBLE_COLON {
		p.nextToken() // 跳过::
		if p.curTok.Type == lexer.TOKEN_LBRACKET {
			p.nextToken() // 跳过[
			// 解析自指表达式
			for p.curTok.Type != lexer.TOKEN_RBRACKET {
				p.parseExpression() // 解析表达式但暂时不使用
				if p.curTok.Type != lexer.TOKEN_RBRACKET {
					p.nextToken()
				}
			}
			if p.curTok.Type == lexer.TOKEN_RBRACKET {
				p.nextToken() // 跳过]
			}
		}
	}
	return stmt
}

// parseFunctionStatement 解析函数语句
func (p *Parser) parseFunctionStatement() *ast.FunctionStatement {
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
	}
	p.nextToken() // 跳过FUNC
	if p.curTok.Type == lexer.TOKEN_IDENT {
		stmt.Name = p.curTok.Value
		p.log("解析函数名: %s", stmt.Name)
		p.nextToken() // 跳过函数名
	}
	if p.curTok.Type == lexer.TOKEN_LPAREN {
		p.nextToken() // 跳过(
		// 解析参数
		p.log("开始解析函数参数")
		for p.curTok.Type != lexer.TOKEN_RPAREN {
			if p.curTok.Type == lexer.TOKEN_IDENT {
				stmt.Params = append(stmt.Params, p.curTok.Value)
				p.log("解析参数: %s", p.curTok.Value)
				p.nextToken() // 跳过参数名
			}
			if p.curTok.Type == lexer.TOKEN_COMMA {
				p.nextToken() // 跳过,
			}
		}
		p.log("函数参数解析完成，共 %d 个参数", len(stmt.Params))
		if p.curTok.Type == lexer.TOKEN_RPAREN {
			p.nextToken() // 跳过)
		}
	}
	if p.curTok.Type == lexer.TOKEN_LBRACE {
		p.nextToken() // 跳过{
		// 解析函数体
		p.log("开始解析函数体")
		for p.curTok.Type != lexer.TOKEN_RBRACE {
			bodyStmt := p.parseStatement()
			if bodyStmt != nil {
				stmt.Body = append(stmt.Body, bodyStmt)
				p.log("函数体添加语句: %s", bodyStmt.String())
			}
			// 不要总是调用 nextToken()，让 parseStatement 函数自己处理 token 的跳过
			// 只有当当前 token 不是右大括号且不是下一个语句的开始时才继续
			if p.curTok.Type != lexer.TOKEN_RBRACE {
				// 检查当前 token 是否是一个语句的开始
				isStatementStart := false
				switch p.curTok.Type {
				case lexer.TOKEN_VO, lexer.TOKEN_SPEND, lexer.TOKEN_SPEND_CALL, lexer.TOKEN_CALL, lexer.TOKEN_TASK, lexer.TOKEN_PREFIX, lexer.TOKEN_TREE, lexer.TOKEN_OBJECT, lexer.TOKEN_FUNC, lexer.TOKEN_IF, lexer.TOKEN_WHILE, lexer.TOKEN_FOR, lexer.TOKEN_SWITCH, lexer.TOKEN_RETURN, lexer.TOKEN_IMPORT, lexer.TOKEN_NONLOCAL, lexer.TOKEN_PRINTLN, lexer.TOKEN_IDENT:
					isStatementStart = true
				}
				if !isStatementStart {
					p.nextToken()
				}
			}
		}
		p.log("函数体解析完成，共 %d 条语句", len(stmt.Body))
		// 跳过右大括号
		if p.curTok.Type == lexer.TOKEN_RBRACE {
			p.nextToken() // 跳过}
		}
	}
	p.log("函数语句解析完成")
	return stmt
}

// parseIfStatement 解析if语句
func (p *Parser) parseIfStatement() *ast.IfStatement {
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
	p.nextToken() // 跳过IF
	if p.curTok.Type == lexer.TOKEN_LPAREN {
		p.nextToken() // 跳过(
		stmt.Condition = p.parseExpression()
		if p.curTok.Type == lexer.TOKEN_RPAREN {
			p.nextToken() // 跳过)
		}
	}
	if p.curTok.Type == lexer.TOKEN_LBRACE {
		p.nextToken() // 跳过{
		// 解析if体
		for p.curTok.Type != lexer.TOKEN_RBRACE {
			bodyStmt := p.parseStatement()
			if bodyStmt != nil {
				stmt.Body = append(stmt.Body, bodyStmt)
			}
			// 只有当不是右大括号时才继续
			if p.curTok.Type != lexer.TOKEN_RBRACE {
				p.nextToken()
			}
		}
		if p.curTok.Type == lexer.TOKEN_RBRACE {
			p.nextToken() // 跳过}
		}
	}
	if p.curTok.Type == lexer.TOKEN_ELSE {
		p.nextToken() // 跳过ELSE
		if p.curTok.Type == lexer.TOKEN_LBRACE {
			p.nextToken() // 跳过{
			// 解析else体
			for p.curTok.Type != lexer.TOKEN_RBRACE {
				bodyStmt := p.parseStatement()
				if bodyStmt != nil {
					stmt.Else = append(stmt.Else, bodyStmt)
				}
				// 只有当不是右大括号时才继续
				if p.curTok.Type != lexer.TOKEN_RBRACE {
					p.nextToken()
				}
			}
			if p.curTok.Type == lexer.TOKEN_RBRACE {
				p.nextToken() // 跳过}
			}
		}
	}
	return stmt
}

// parseWhileStatement 解析while语句
func (p *Parser) parseWhileStatement() *ast.WhileStatement {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	stmt := &ast.WhileStatement{
		Body: []ast.Statement{},
		Pos:  pos,
	}
	p.nextToken() // 跳过WHILE
	if p.curTok.Type == lexer.TOKEN_LPAREN {
		p.nextToken() // 跳过(
		stmt.Condition = p.parseExpression()
		if p.curTok.Type == lexer.TOKEN_RPAREN {
			p.nextToken() // 跳过)
		}
	}
	if p.curTok.Type == lexer.TOKEN_LBRACE {
		p.nextToken() // 跳过{
		// 解析循环体
		for p.curTok.Type != lexer.TOKEN_RBRACE {
			bodyStmt := p.parseStatement()
			if bodyStmt != nil {
				stmt.Body = append(stmt.Body, bodyStmt)
			}
			// 只有当不是右大括号时才继续
			if p.curTok.Type != lexer.TOKEN_RBRACE {
				p.nextToken()
			}
		}
		if p.curTok.Type == lexer.TOKEN_RBRACE {
			p.nextToken() // 跳过}
		}
	}
	return stmt
}

// parseForStatement 解析for语句
func (p *Parser) parseForStatement() *ast.ForStatement {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	stmt := &ast.ForStatement{
		Body: []ast.Statement{},
		Pos:  pos,
	}
	p.nextToken() // 跳过FOR
	if p.curTok.Type == lexer.TOKEN_LPAREN {
		p.nextToken() // 跳过(
		// 解析初始化语句
		stmt.Init = p.parseStatement()
		if p.curTok.Type == lexer.TOKEN_SEMICOLON {
			p.nextToken() // 跳过;
		}
		// 解析条件表达式
		stmt.Condition = p.parseExpression()
		if p.curTok.Type == lexer.TOKEN_SEMICOLON {
			p.nextToken() // 跳过;
		}
		// 解析更新语句
		stmt.Update = p.parseStatement()
		if p.curTok.Type == lexer.TOKEN_RPAREN {
			p.nextToken() // 跳过)
		}
	}
	if p.curTok.Type == lexer.TOKEN_LBRACE {
		p.nextToken() // 跳过{
		// 解析循环体
		for p.curTok.Type != lexer.TOKEN_RBRACE {
			bodyStmt := p.parseStatement()
			if bodyStmt != nil {
				stmt.Body = append(stmt.Body, bodyStmt)
			}
			// 只有当不是右大括号时才继续
			if p.curTok.Type != lexer.TOKEN_RBRACE {
				p.nextToken()
			}
		}
		if p.curTok.Type == lexer.TOKEN_RBRACE {
			p.nextToken() // 跳过}
		}
	}
	return stmt
}

// parseSwitchStatement 解析switch语句
func (p *Parser) parseSwitchStatement() *ast.SwitchStatement {
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
	p.nextToken() // 跳过SWITCH
	if p.curTok.Type == lexer.TOKEN_LPAREN {
		p.nextToken() // 跳过(
		stmt.Expression = p.parseExpression()
		if p.curTok.Type == lexer.TOKEN_RPAREN {
			p.nextToken() // 跳过)
		}
	}
	if p.curTok.Type == lexer.TOKEN_LBRACE {
		p.nextToken() // 跳过{
		// 解析switch语句体中的语句
		for p.curTok.Type != lexer.TOKEN_RBRACE {
			if p.curTok.Type == lexer.TOKEN_CASE {
				caseStmt := p.parseCaseStatement()
				stmt.Cases = append(stmt.Cases, *caseStmt)
			} else if p.curTok.Type == lexer.TOKEN_DEFAULT {
				p.nextToken() // 跳过DEFAULT
				if p.curTok.Type == lexer.TOKEN_COLON {
					p.nextToken() // 跳过:
					// 解析default体
					for p.curTok.Type != lexer.TOKEN_CASE && p.curTok.Type != lexer.TOKEN_DEFAULT && p.curTok.Type != lexer.TOKEN_RBRACE {
						bodyStmt := p.parseStatement()
						if bodyStmt != nil {
							stmt.Default = append(stmt.Default, bodyStmt)
						}
						// 只有当不是case、default或右大括号时才继续
						if p.curTok.Type != lexer.TOKEN_CASE && p.curTok.Type != lexer.TOKEN_DEFAULT && p.curTok.Type != lexer.TOKEN_RBRACE {
							p.nextToken()
						}
					}
				}
			} else {
				// 解析其他语句（如变量声明）
				bodyStmt := p.parseStatement()
				if bodyStmt != nil {
					stmt.Statements = append(stmt.Statements, bodyStmt)
				}
				// 只有当不是右大括号时才继续
				if p.curTok.Type != lexer.TOKEN_RBRACE {
					p.nextToken()
				}
			}
		}
		if p.curTok.Type == lexer.TOKEN_RBRACE {
			p.nextToken() // 跳过}
		}
	}
	return stmt
}

// parseCaseStatement 解析case语句
func (p *Parser) parseCaseStatement() *ast.CaseStatement {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	stmt := &ast.CaseStatement{
		Body: []ast.Statement{},
		Pos:  pos,
	}
	p.nextToken() // 跳过CASE
	stmt.Value = p.parseExpression()
	if p.curTok.Type == lexer.TOKEN_COLON {
		p.nextToken() // 跳过:
		// 解析case体
		for p.curTok.Type != lexer.TOKEN_CASE && p.curTok.Type != lexer.TOKEN_DEFAULT && p.curTok.Type != lexer.TOKEN_RBRACE {
			bodyStmt := p.parseStatement()
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

// parseReturnStatement 解析return语句
func (p *Parser) parseReturnStatement() *ast.ReturnStatement {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	stmt := &ast.ReturnStatement{
		Pos: pos,
	}
	p.nextToken() // 跳过RETURN
	stmt.Value = p.parseExpression()
	return stmt
}

// parseImportStatement 解析import语句
func (p *Parser) parseImportStatement() *ast.ImportStatement {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	stmt := &ast.ImportStatement{
		Pos: pos,
	}
	p.nextToken() // 跳过IMPORT
	if p.curTok.Type == lexer.TOKEN_IDENT {
		stmt.Module = p.curTok.Value
		// 解析模块路径
		for p.peekTok.Type == lexer.TOKEN_DOT {
			p.nextToken() // 跳过模块名
			p.nextToken() // 跳过.
			if p.curTok.Type == lexer.TOKEN_IDENT {
				stmt.Module += "." + p.curTok.Value
			}
		}
	}
	return stmt
}

// parseNonLocalStatement 解析nonlocal语句
func (p *Parser) parseNonLocalStatement() *ast.NonLocalStatement {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	stmt := &ast.NonLocalStatement{
		Pos: pos,
	}
	p.nextToken() // 跳过NONLOCAL
	// 解析类型
	if p.curTok.Type == lexer.TOKEN_IDENT {
		stmt.Type = p.curTok.Value
		p.nextToken() // 跳过类型
	}
	// 解析变量名
	if p.curTok.Type == lexer.TOKEN_IDENT {
		stmt.Name = p.curTok.Value
		p.nextToken() // 跳过变量名
	}
	// 解析赋值
	if p.curTok.Type == lexer.TOKEN_ASSIGN {
		p.nextToken() // 跳过=
		stmt.Value = p.parseExpression()
	}
	return stmt
}

// parseClassStatement 解析类定义
func (p *Parser) parseClassStatement() *ast.ClassStatement {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	stmt := &ast.ClassStatement{
		Fields:      []*ast.FieldDeclaration{},
		Methods:     []*ast.MethodStatement{},
		Constructors: []*ast.ConstructorStatement{},
		Implements:  []string{},
		Pos:         pos,
	}
	p.nextToken() // 跳过CLASS
	// 解析类名
	if p.curTok.Type == lexer.TOKEN_IDENT {
		stmt.Name = p.curTok.Value
		p.nextToken() // 跳过类名
	}
	// 解析implements子句
	if p.curTok.Type == lexer.TOKEN_IMPLEMENTS {
		p.nextToken() // 跳过IMPLEMENTS
		for p.curTok.Type == lexer.TOKEN_IDENT {
			stmt.Implements = append(stmt.Implements, p.curTok.Value)
			p.nextToken() // 跳过接口名
			if p.curTok.Type == lexer.TOKEN_COMMA {
				p.nextToken() // 跳过,
			}
		}
	}
	// 解析类体
	if p.curTok.Type == lexer.TOKEN_LBRACE {
		p.nextToken() // 跳过{
		for p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
			// 打印当前token以进行调试
			p.log("当前token: %s, 开始解析类成员", lexer.TokenTypeToString(p.curTok.Type))
			
			// 先检查是否是字段声明
			if p.curTok.Type == lexer.TOKEN_IDENT {
				// 保存当前token位置
				savedCurTok := p.curTok
				savedPeekTok := p.peekTok
				
				// 尝试解析字段声明
				if field := p.parseFieldDeclaration(); field != nil {
					p.log("解析完成字段声明: %s", field.String())
					stmt.Fields = append(stmt.Fields, field)
				} else {
					// 恢复token位置
					p.curTok = savedCurTok
					p.peekTok = savedPeekTok
					
					// 尝试解析方法声明
					if method := p.parseMethodStatement(); method != nil {
						p.log("解析完成方法声明: %s", method.String())
						stmt.Methods = append(stmt.Methods, method)
					} else {
						// 恢复token位置
						p.curTok = savedCurTok
						p.peekTok = savedPeekTok
						
						// 尝试解析构造函数
						// 构造函数的名称与类名相同
						if p.curTok.Type == lexer.TOKEN_IDENT && p.curTok.Value == stmt.Name {
							p.log("开始解析构造函数")
							constructor := p.parseConstructorStatement()
							if constructor != nil {
								p.log("解析完成构造函数")
								stmt.Constructors = append(stmt.Constructors, constructor)
							}
						} else {
							// 跳过其他token
							p.log("跳过token: %s", lexer.TokenTypeToString(p.curTok.Type))
							p.nextToken()
						}
					}
				}
			} else if p.curTok.Type == lexer.TOKEN_SEMICOLON {
				// 跳过分号
				p.log("跳过分号")
				p.nextToken()
			} else {
				// 跳过其他token
				p.log("跳过token: %s", lexer.TokenTypeToString(p.curTok.Type))
				p.nextToken()
			}
		}
		// 不要跳过右大括号，因为parseProgram函数会负责前进到下一个token
		p.log("解析完成类体")
	}
	p.log("类解析完成: %s, 字段数: %d, 方法数: %d, 构造函数数: %d", stmt.Name, len(stmt.Fields), len(stmt.Methods), len(stmt.Constructors))
	return stmt
}

// parseInterfaceStatement 解析接口定义
func (p *Parser) parseInterfaceStatement() *ast.InterfaceStatement {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	stmt := &ast.InterfaceStatement{
		Methods: []*ast.MethodStatement{},
		Pos:     pos,
	}
	p.nextToken() // 跳过 INTERFACE
	// 解析接口名
	if p.curTok.Type == lexer.TOKEN_IDENT {
		stmt.Name = p.curTok.Value
		p.nextToken() // 跳过接口名
	}
	// 解析接口体
	if p.curTok.Type == lexer.TOKEN_LBRACE {
		p.nextToken() // 跳过{
		for p.curTok.Type != lexer.TOKEN_RBRACE {
			if p.curTok.Type == lexer.TOKEN_IDENT {
				// 尝试解析方法声明
				if method := p.parseMethodStatement(); method != nil {
					stmt.Methods = append(stmt.Methods, method)
					continue
				}
				// 跳过其他标识符
				p.nextToken()
			} else {
				// 跳过其他 token
				p.nextToken()
			}
		}
		// 不要跳过右大括号，因为 parseProgram 函数会负责前进到下一个 token
	}

	return stmt
}

// parseStructStatement 解析结构体定义
func (p *Parser) parseStructStatement() *ast.StructStatement {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	stmt := &ast.StructStatement{
		Fields: []*ast.FieldDeclaration{},
		Pos:    pos,
	}
	p.nextToken() // 跳过 STRUCT
	// 解析结构体名
	if p.curTok.Type == lexer.TOKEN_IDENT {
		stmt.Name = p.curTok.Value
		p.nextToken() // 跳过结构体名
	}
	// 解析结构体体
	if p.curTok.Type == lexer.TOKEN_LBRACE {
		p.nextToken() // 跳过{
		for p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
			p.log("当前 token: %s, 开始解析结构体字段", lexer.TokenTypeToString(p.curTok.Type))
			
			// 尝试解析字段声明
			if field := p.parseFieldDeclaration(); field != nil {
				p.log("解析完成字段声明：%s", field.String())
				stmt.Fields = append(stmt.Fields, field)
			} else if p.curTok.Type == lexer.TOKEN_SEMICOLON {
				// 跳过分号
				p.log("跳过分号")
				p.nextToken()
			} else {
				// 跳过其他 token
				p.log("跳过 token: %s", lexer.TokenTypeToString(p.curTok.Type))
				p.nextToken()
			}
		}
		p.log("解析完成结构体体")
	}
	p.log("结构体解析完成：%s, 字段数：%d", stmt.Name, len(stmt.Fields))
	return stmt
}

// parseFieldDeclaration 解析字段声明
func (p *Parser) parseFieldDeclaration() *ast.FieldDeclaration {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	
	p.log("开始解析字段声明，当前token: %s, 值: %s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
	
	// 检查是否可能是字段声明：类型 + 名称 + 分号
	if p.curTok.Type != lexer.TOKEN_IDENT {
		p.log("不是字段声明，返回nil")
		return nil
	}
	
	// 保存当前token位置
	savedCurTok := p.curTok
	savedPeekTok := p.peekTok
	p.log("保存token位置，curTok: %s, 值: %s, peekTok: %s, 值: %s", lexer.TokenTypeToString(savedCurTok.Type), savedCurTok.Value, lexer.TokenTypeToString(savedPeekTok.Type), savedPeekTok.Value)
	
	// 尝试解析类型
	typeName := p.curTok.Value
	p.log("解析类型: %s", typeName)
	p.nextToken()
	p.log("跳过类型后，当前token: %s, 值: %s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
	
	nullable := false
	if p.curTok.Type == lexer.TOKEN_QUESTION {
		nullable = true
		p.log("跳过QUESTION token")
		p.nextToken()
		p.log("跳过QUESTION后，当前token: %s, 值: %s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
	}
	
	// 检查是否有字段名
	if p.curTok.Type != lexer.TOKEN_IDENT {
		p.log("不是字段声明，恢复token位置")
		// 不是字段声明，恢复token位置
		p.curTok = savedCurTok
		p.peekTok = savedPeekTok
		p.log("恢复token位置后，curTok: %s, 值: %s, peekTok: %s, 值: %s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value, lexer.TokenTypeToString(p.peekTok.Type), p.peekTok.Value)
		return nil
	}
	
	// 检查下一个token是否是分号
	p.log("检查下一个token是否是分号，peekTok: %s, 值: %s", lexer.TokenTypeToString(p.peekTok.Type), p.peekTok.Value)
	if p.peekTok.Type != lexer.TOKEN_SEMICOLON {
		p.log("不是字段声明，恢复token位置")
		// 不是字段声明，恢复token位置
		p.curTok = savedCurTok
		p.peekTok = savedPeekTok
		p.log("恢复token位置后，curTok: %s, 值: %s, peekTok: %s, 值: %s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value, lexer.TokenTypeToString(p.peekTok.Type), p.peekTok.Value)
		return nil
	}
	
	// 是字段声明，解析它
	fieldName := p.curTok.Value
	p.log("解析字段名: %s", fieldName)
	p.nextToken()
	p.log("跳过字段名后，当前token: %s, 值: %s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
	
	// 跳过分号
	p.log("跳过分号")
	p.nextToken()
	p.log("跳过分号后，当前token: %s, 值: %s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
	
	field := &ast.FieldDeclaration{
		Name:     fieldName,
		Type:     typeName,
		Nullable: nullable,
		Pos:      pos,
	}
	p.log("字段声明解析完成: %s", field.String())
	return field
}

// parseMethodStatement 解析方法定义
func (p *Parser) parseMethodStatement() *ast.MethodStatement {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	
	// 保存当前token位置
	savedCurTok := p.curTok
	savedPeekTok := p.peekTok
	
	method := &ast.MethodStatement{
		Params: []*ast.Param{},
		Body:   []ast.Statement{},
		Pos:    pos,
	}
	p.log("开始解析方法，当前token: %s, 值: %s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
	// 解析返回类型
	if p.curTok.Type != lexer.TOKEN_IDENT {
		p.log("不是方法声明，返回nil")
		// 恢复token位置
		p.curTok = savedCurTok
		p.peekTok = savedPeekTok
		return nil
	}
	method.ReturnType = p.curTok.Value
	p.log("解析返回类型: %s", p.curTok.Value)
	p.nextToken() // 跳过返回类型
	p.log("当前token: %s, 值: %s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
	// 检查是否是可空类型
	if p.curTok.Type == lexer.TOKEN_QUESTION {
		p.log("跳过QUESTION token")
		p.nextToken() // 跳过?
		p.log("当前token: %s, 值: %s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
	}
	// 解析方法名
	if p.curTok.Type != lexer.TOKEN_IDENT {
		p.log("不是方法声明，返回nil")
		// 恢复token位置
		p.curTok = savedCurTok
		p.peekTok = savedPeekTok
		return nil
	}
	method.Name = p.curTok.Value
	p.log("解析方法名: %s", p.curTok.Value)
	p.log("跳过方法名前，curTok: %s, 值: %s, peekTok: %s, 值: %s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value, lexer.TokenTypeToString(p.peekTok.Type), p.peekTok.Value)
	p.nextToken() // 跳过方法名
	p.log("跳过方法名后，当前token: %s, 值: %s, peekTok: %s, 值: %s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value, lexer.TokenTypeToString(p.peekTok.Type), p.peekTok.Value)
	// 解析参数
	if p.curTok.Type != lexer.TOKEN_LPAREN {
		// 检查是否是右括号，如果是，说明我们可能跳过了左括号
		if p.curTok.Type == lexer.TOKEN_RPAREN {
			p.log("当前token是RPAREN，跳过它")
			p.nextToken() // 跳过)
			p.log("跳过RPAREN后，当前token: %s, 值: %s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
			// 现在检查是否是左大括号
			if p.curTok.Type == lexer.TOKEN_LBRACE {
				// 这是一个没有参数的方法
				p.log("发现左大括号，这是一个没有参数的方法")
				// 直接跳到方法体解析
				goto parseMethodBody
			}
		} else if p.peekTok.Type == lexer.TOKEN_LPAREN {
			p.log("当前token不是LPAREN，但peekTok是，前进一个token")
			p.nextToken() // 前进一个token
		} else {
			p.log("不是方法声明，返回nil")
			// 恢复token位置
			p.curTok = savedCurTok
			p.peekTok = savedPeekTok
			return nil
		}
	}
	p.log("跳过LPAREN token")
	p.nextToken() // 跳过(
	p.log("当前token: %s, 值: %s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
	// 解析参数列表
	if p.curTok.Type != lexer.TOKEN_RPAREN {
		for p.curTok.Type != lexer.TOKEN_RPAREN {
			p.log("解析参数，当前token: %s, 值: %s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
			// 解析参数类型
			if p.curTok.Type != lexer.TOKEN_IDENT {
				// 不是方法声明，返回nil
				p.log("不是方法声明，返回nil")
				// 恢复token位置
				p.curTok = savedCurTok
				p.peekTok = savedPeekTok
				return nil
			}
			param := &ast.Param{}
			param.Type = p.curTok.Value
			p.log("跳过参数类型: %s", p.curTok.Value)
			p.nextToken() // 跳过类型
			p.log("当前token: %s, 值: %s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
			// 检查是否是可空类型
			if p.curTok.Type == lexer.TOKEN_QUESTION {
				param.Nullable = true
				p.log("跳过QUESTION token")
				p.nextToken() // 跳过?
				p.log("当前token: %s, 值: %s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
			}
			// 解析参数名
			if p.curTok.Type != lexer.TOKEN_IDENT {
				p.error(fmt.Sprintf("expected parameter name, got %s", lexer.TokenTypeToString(p.curTok.Type)))
				// 恢复token位置
				p.curTok = savedCurTok
				p.peekTok = savedPeekTok
				return nil
			}
			param.Name = p.curTok.Value
			p.log("跳过参数名: %s", p.curTok.Value)
			p.nextToken() // 跳过参数名
			p.log("当前token: %s, 值: %s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
			method.Params = append(method.Params, param)
			// 处理逗号
			if p.curTok.Type == lexer.TOKEN_COMMA {
				p.log("跳过COMMA token")
				p.nextToken() // 跳过,
				p.log("当前token: %s, 值: %s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
			}
		}
	}
	// 处理右括号
	if p.curTok.Type == lexer.TOKEN_RPAREN {
		p.log("跳过RPAREN token")
		p.nextToken() // 跳过)
		p.log("当前token: %s, 值: %s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
	} else {
		p.error(fmt.Sprintf("expected ), got %s", lexer.TokenTypeToString(p.curTok.Type)))
		p.log("不是方法声明，返回nil")
		// 恢复token位置
		p.curTok = savedCurTok
		p.peekTok = savedPeekTok
		return nil
	}

parseMethodBody:
	// 解析方法体或分号（接口方法声明）
	p.log("解析方法体或分号，当前token: %s, 值: %s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
	if p.curTok.Type == lexer.TOKEN_SEMICOLON {
		// 接口方法声明，只有签名没有实现
		p.log("接口方法声明，跳过分号")
		p.nextToken() // 跳过分号
	} else if p.curTok.Type == lexer.TOKEN_LBRACE {
		// 类方法声明，有实现
		p.log("跳过LBRACE token")
		p.nextToken() // 跳过{
		p.log("当前token: %s, 值: %s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
		for p.curTok.Type != lexer.TOKEN_RBRACE {
			bodyStmt := p.parseStatement()
			if bodyStmt != nil {
				method.Body = append(method.Body, bodyStmt)
			}
			// 只有当不是右大括号时才继续
			if p.curTok.Type != lexer.TOKEN_RBRACE {
				p.nextToken()
				p.log("当前token: %s, 值: %s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
			}
		}
		// 处理右大括号
		if p.curTok.Type == lexer.TOKEN_RBRACE {
			p.log("跳过RBRACE token")
			p.nextToken() // 跳过右大括号
			p.log("当前token: %s, 值: %s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
		} else {
			p.error(fmt.Sprintf("expected }, got %s", lexer.TokenTypeToString(p.curTok.Type)))
		}
	} else {
		// 不是方法声明，返回nil
		p.log("不是方法声明，返回nil")
		// 恢复token位置
		p.curTok = savedCurTok
		p.peekTok = savedPeekTok
		return nil
	}

	p.log("方法解析完成: %s", method.Name)
	return method
}

// parseConstructorStatement 解析构造函数
func (p *Parser) parseConstructorStatement() *ast.ConstructorStatement {
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
	// 检查是否是构造函数声明
	if p.curTok.Type != lexer.TOKEN_IDENT {
		p.log("不是构造函数声明，返回nil")
		return nil
	}
	// 构造函数名就是类名，不需要单独存储
	constructorName := p.curTok.Value
	p.log("解析构造函数名: %s", constructorName)
	p.nextToken() // 跳过构造函数名
	p.log("当前token: %s, 值: %s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
	// 解析参数
	if p.curTok.Type == lexer.TOKEN_LPAREN {
		p.log("跳过LPAREN token")
		p.nextToken() // 跳过(
		p.log("当前token: %s, 值: %s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
		// 解析参数列表
		for p.curTok.Type != lexer.TOKEN_RPAREN {
			p.log("解析参数，当前token: %s, 值: %s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
			// 解析参数类型
			if p.curTok.Type != lexer.TOKEN_IDENT {
				p.error(fmt.Sprintf("expected type name, got %s", lexer.TokenTypeToString(p.curTok.Type)))
				break
			}
			param := &ast.Param{}
			param.Type = p.curTok.Value
			p.log("跳过参数类型: %s", p.curTok.Value)
			p.nextToken() // 跳过类型
			p.log("当前token: %s, 值: %s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
			// 检查是否是可空类型
			if p.curTok.Type == lexer.TOKEN_QUESTION {
				param.Nullable = true
				p.log("跳过QUESTION token")
				p.nextToken() // 跳过?
				p.log("当前token: %s, 值: %s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
			}
			// 解析参数名
			if p.curTok.Type != lexer.TOKEN_IDENT {
				p.error(fmt.Sprintf("expected parameter name, got %s", lexer.TokenTypeToString(p.curTok.Type)))
				break
			}
			param.Name = p.curTok.Value
			p.log("跳过参数名: %s", p.curTok.Value)
			p.nextToken() // 跳过参数名
			p.log("当前token: %s, 值: %s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
			constructor.Params = append(constructor.Params, param)
			// 处理逗号
			if p.curTok.Type == lexer.TOKEN_COMMA {
				p.log("跳过COMMA token")
				p.nextToken() // 跳过,
				p.log("当前token: %s, 值: %s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
			}
		}
		// 处理右括号
		if p.curTok.Type == lexer.TOKEN_RPAREN {
			p.log("跳过RPAREN token")
			p.nextToken() // 跳过)
			p.log("当前token: %s, 值: %s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
		} else {
			p.error(fmt.Sprintf("expected ), got %s", lexer.TokenTypeToString(p.curTok.Type)))
		}
	} else {
		p.error(fmt.Sprintf("expected (, got %s", lexer.TokenTypeToString(p.curTok.Type)))
		return nil
	}
	// 解析构造函数体
	if p.curTok.Type == lexer.TOKEN_LBRACE {
		p.log("跳过LBRACE token")
		p.nextToken() // 跳过{
		p.log("当前token: %s, 值: %s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
		for p.curTok.Type != lexer.TOKEN_RBRACE {
			bodyStmt := p.parseStatement()
			if bodyStmt != nil {
				constructor.Body = append(constructor.Body, bodyStmt)
			}
			// 只有当不是右大括号时才继续
			if p.curTok.Type != lexer.TOKEN_RBRACE {
				p.nextToken()
				p.log("当前token: %s, 值: %s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
			}
		}
		// 处理右大括号
		if p.curTok.Type == lexer.TOKEN_RBRACE {
			p.log("跳过RBRACE token")
			p.nextToken() // 跳过右大括号
			p.log("当前token: %s, 值: %s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
		}
	} else {
		p.error(fmt.Sprintf("expected {, got %s", lexer.TokenTypeToString(p.curTok.Type)))
		return nil
	}
	p.log("构造函数解析完成")
	return constructor
}



// parseExpressionStatement 解析表达式语句
func (p *Parser) parseExpressionStatement() *ast.ExpressionStatement {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	p.log("开始解析表达式语句，当前token: %s, 值: %s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
	expr := p.parseExpression()
	p.log("解析表达式完成，当前token: %s, 值: %s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
	// 消费分号
	if p.curTok.Type == lexer.TOKEN_SEMICOLON {
		p.log("消费分号")
		p.nextToken()
		p.log("当前token: %s, 值: %s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
	}
	stmt := &ast.ExpressionStatement{
		Expression: expr,
		Pos:        pos,
	}
	return stmt
}

// parseExpression 解析表达式
func (p *Parser) parseExpression() ast.Expression {
	return p.parseBinaryExpression(0)
}

// 运算符优先级
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
}

// parseBinaryExpression 解析二元表达式
func (p *Parser) parseBinaryExpression(precedence int) ast.Expression {
	left := p.parsePrimaryExpression()
	for precedence < p.precedence(p.curTok.Type) {
		op := p.curTok.Type
		p.nextToken()
		// 对于赋值操作，使用右结合
		if op == lexer.TOKEN_ASSIGN {
			right := p.parseBinaryExpression(p.precedence(op) - 1)
			left = &ast.BinaryExpression{
				Left:     left,
				Operator: lexer.TokenTypeToString(op),
				Right:    right,
			}
		} else {
			// 对于其他操作，使用左结合
			right := p.parseBinaryExpression(p.precedence(op))
			left = &ast.BinaryExpression{
				Left:     left,
				Operator: lexer.TokenTypeToString(op),
				Right:    right,
			}
		}
	}
	return left
}

// precedence 获取运算符优先级
func (p *Parser) precedence(tokenType lexer.TokenType) int {
	if prec, ok := precedences[tokenType]; ok {
		return prec
	}
	return 0
}

// parsePrimaryExpression 解析基本表达式
func (p *Parser) parsePrimaryExpression() ast.Expression {
	switch p.curTok.Type {
	case lexer.TOKEN_IDENT:
		return p.parseIdentifier()
	case lexer.TOKEN_INT:
		return p.parseIntegerLiteral()
	case lexer.TOKEN_FLOAT:
		return p.parseFloatLiteral()
	case lexer.TOKEN_STRING:
		return p.parseStringLiteral()
	case lexer.TOKEN_LPAREN:
		return p.parseGroupedExpression()
	case lexer.TOKEN_LBRACKET:
		return p.parseIndexExpression()
	case lexer.TOKEN_PREFIX_REF:
		p.nextToken()
		if p.curTok.Type == lexer.TOKEN_IDENT {
			ident := &ast.Identifier{
				Name: p.curTok.Value,
			}
			p.nextToken()
			return ident
		} else if p.curTok.Type == lexer.TOKEN_RBRACE {
			// 处理前缀引用后直接遇到右大括号的情况
			p.error("expected identifier after prefix ref, got RBRACE")
			return nil
		}
		p.error(fmt.Sprintf("expected identifier after prefix ref, got %s", lexer.TokenTypeToString(p.curTok.Type)))
		p.nextToken() // 消费错误的token
		return nil
	case lexer.TOKEN_PRINTLN:
		// 处理println函数
		ident := &ast.Identifier{
			Name: p.curTok.Value,
		}
		p.nextToken()
		// 检查是否是函数调用
		if p.curTok.Type == lexer.TOKEN_LPAREN {
			return p.parseCallExpression(ident)
		}
		return ident
	case lexer.TOKEN_VO:
		// 处理vo表达式（如 vo.access(a)）
		ident := &ast.Identifier{
			Name: p.curTok.Value,
		}
		p.nextToken()
		// 检查是否是点操作符
		if p.curTok.Type == lexer.TOKEN_DOT {
			p.nextToken() // 跳过.
			if p.curTok.Type == lexer.TOKEN_IDENT {
				// 创建成员访问表达式
				memberIdent := &ast.Identifier{
					Name: p.curTok.Value,
				}
				p.nextToken()
				// 检查是否是函数调用
				if p.curTok.Type == lexer.TOKEN_LPAREN {
					return p.parseCallExpression(memberIdent)
				}
				return memberIdent
			}
		}
		return ident
	case lexer.TOKEN_SELF:
		// 处理self关键字
		ident := &ast.Identifier{
			Name: p.curTok.Value,
		}
		p.nextToken()
		return ident
	case lexer.TOKEN_NULL:
		// 处理null关键字
		ident := &ast.Identifier{
			Name: p.curTok.Value,
		}
		p.nextToken()
		return ident
	case lexer.TOKEN_RBRACE, lexer.TOKEN_LBRACE, lexer.TOKEN_DOT, lexer.TOKEN_ASSIGN:
		// 这些token在表达式中是非法的，应该由上层解析器处理
		return nil
	default:
		p.error(fmt.Sprintf("unexpected token: %s", lexer.TokenTypeToString(p.curTok.Type)))
		p.nextToken() // 消费错误的token
		return nil
	}
}

// parseIdentifier 解析标识符
func (p *Parser) parseIdentifier() ast.Expression {
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
	// 检查是否是前缀引用（如 p$）
	if p.curTok.Type == lexer.TOKEN_PREFIX_REF {
		p.nextToken() // 跳过$
		return ident
	}
	// 检查是否是函数调用
	if p.curTok.Type == lexer.TOKEN_LPAREN {
		return p.parseCallExpression(ident)
	}
	// 检查是否是点操作符（如 this.obj1 或 object.method()）
	if p.curTok.Type == lexer.TOKEN_DOT {
		p.nextToken() // 跳过点
		if p.curTok.Type == lexer.TOKEN_IDENT {
			memberName := p.curTok.Value
			memberPos := ast.Position{
				Line:   p.curTok.Line,
				Column: p.curTok.Column,
				File:   p.file,
			}
			p.nextToken()
			// 创建成员访问表达式
			memberAccess := &ast.MemberAccessExpression{
				Object: ident,
				Member: memberName,
				Pos:    memberPos,
			}
			// 检查是否是函数调用
			if p.curTok.Type == lexer.TOKEN_LPAREN {
				return p.parseCallExpression(memberAccess)
			}
			return memberAccess
		}
	}
	return ident
}

// parseIntegerLiteral 解析整数字面量
func (p *Parser) parseIntegerLiteral() *ast.IntegerLiteral {
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

// parseFloatLiteral 解析浮点数字面量
func (p *Parser) parseFloatLiteral() *ast.FloatLiteral {
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

// parseStringLiteral 解析字符串字面量
func (p *Parser) parseStringLiteral() *ast.StringLiteral {
	pos := ast.Position{
		Line:   p.curTok.Line,
		Column: p.curTok.Column,
		File:   p.file,
	}
	literal := &ast.StringLiteral{Value: p.curTok.Value, Pos: pos}
	p.nextToken()
	return literal
}

// parseGroupedExpression 解析分组表达式
func (p *Parser) parseGroupedExpression() ast.Expression {
	p.nextToken() // 跳过(
	expr := p.parseExpression()
	if p.curTok.Type == lexer.TOKEN_RPAREN {
		p.nextToken() // 跳过)
	}
	return expr
}

// parseCallExpression 解析函数调用表达式
func (p *Parser) parseCallExpression(function ast.Expression) ast.Expression {
	call := &ast.CallExpression{
		Function: function,
		Args:     []ast.Expression{},
	}
	p.nextToken() // 跳过(
	// 解析参数
	for p.curTok.Type != lexer.TOKEN_RPAREN {
		// 处理命名参数（如 value:a）
		if p.curTok.Type == lexer.TOKEN_IDENT && p.peekTok.Type == lexer.TOKEN_COLON {
			p.nextToken() // 跳过参数名
			p.nextToken() // 跳过:
			arg := p.parseExpression()
			call.Args = append(call.Args, arg)
		} else {
			// 处理普通参数
			arg := p.parseExpression()
			call.Args = append(call.Args, arg)
		}
		if p.curTok.Type == lexer.TOKEN_COMMA {
			p.nextToken() // 跳过,
		}
	}
	if p.curTok.Type == lexer.TOKEN_RPAREN {
		p.nextToken() // 跳过)
	}
	return call
}

// parseIndexExpression 解析索引表达式
func (p *Parser) parseIndexExpression() ast.Expression {
	index := &ast.IndexExpression{}
	p.nextToken() // 跳过[
	index.Object = p.parseExpression()
	if p.curTok.Type == lexer.TOKEN_RBRACKET {
		p.nextToken() // 跳过]
	}
	return index
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
	if p.HasErrors() {
		p.log("解析完成，发现错误")
		p.ReportErrors()
	} else {
		p.log("解析完成，未发现错误")
		// 执行验证检查
		p.Validate(program)
		if p.HasErrors() {
			p.ReportErrors()
		}
	}
	return program
}

// Validate 验证AST的数据完整性
func (p *Parser) Validate(program *ast.Program) {
	p.log("开始验证AST数据完整性")
	
	// 检查函数定义
	functionNames := make(map[string]bool)
	hasMain := false
	for _, stmt := range program.Statements {
		if fnStmt, ok := stmt.(*ast.FunctionStatement); ok {
			if fnStmt.Name == "" {
				p.error("函数缺少名称")
			} else if functionNames[fnStmt.Name] {
				p.error(fmt.Sprintf("函数名称重复: %s", fnStmt.Name))
			} else {
				functionNames[fnStmt.Name] = true
				if fnStmt.Name == "main" {
					hasMain = true
				}
			}
		}
	}
	
	// 检查是否存在main函数
	if !hasMain {
		p.error("找不到main函数")
	}
	
	// 检查spend/call语句
	for _, stmt := range program.Statements {
		if spendStmt, ok := stmt.(*ast.SpendCallStatement); ok {
			if spendStmt.Spend == nil {
				p.error("spend语句缺少表达式")
			}
			if len(spendStmt.Calls) == 0 {
				p.error("spend语句缺少call语句")
			}
			for i, call := range spendStmt.Calls {
				if call.Target == nil {
					p.error(fmt.Sprintf("call语句 %d 缺少目标", i+1))
				}
			}
		}
	}
	
	// 检查prefix语句
	prefixNames := make(map[string]bool)
	for _, stmt := range program.Statements {
		if prefixStmt, ok := stmt.(*ast.PrefixStatement); ok {
			if prefixStmt.Name == "" {
				p.error("prefix语句缺少名称")
			} else if prefixNames[prefixStmt.Name] {
				p.error(fmt.Sprintf("prefix名称重复: %s", prefixStmt.Name))
			} else {
				prefixNames[prefixStmt.Name] = true
			}
		}
	}
	
	// 检查object语句
	for _, stmt := range program.Statements {
		if objStmt, ok := stmt.(*ast.ObjectStatement); ok {
			if objStmt.Type == "" {
				p.error("object语句缺少类型")
			}
			if objStmt.Name == "" {
				p.error("object语句缺少名称")
			}
		}
	}
	
	// 检查import语句
	validModules := map[string]bool{
		"std":        true,
		"std.vo":     true,
		"std.prefix": true,
		"std.task":   true,
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
		"windows":    true,
		"syscall":    true,
		"async":      true,
	}
	
	for _, stmt := range program.Statements {
		if importStmt, ok := stmt.(*ast.ImportStatement); ok {
			if importStmt.Module == "" {
				p.error("import语句缺少模块名称")
			} else if !validModules[importStmt.Module] {
				p.error(fmt.Sprintf("导入的模块不存在: %s", importStmt.Module))
			}
		}
	}
	
	if p.HasErrors() {
		p.log("验证完成，发现验证错误")
	} else {
		p.log("验证完成，未发现错误")
	}
}
