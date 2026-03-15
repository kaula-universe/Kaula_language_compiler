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
		Statements: []ast.Statement{},
		Pos:        pos,
	}

	for p.curTok.Type != lexer.TOKEN_EOF {
		p.log("当前 token: %s, 开始解析语句", lexer.TokenTypeToString(p.curTok.Type))
		stmt := p.parseStatementIterative()
		if stmt != nil {
			program.Statements = append(program.Statements, stmt)
			p.log("解析完成语句：%s", stmt.String())
		}
		if p.curTok.Type != lexer.TOKEN_EOF {
			p.nextToken()
		}
	}
	p.log("程序解析完成，共 %d 条语句", len(program.Statements))
	return program
}

// parseStatementIterative 迭代解析语句
func (p *Parser) parseStatementIterative() ast.Statement {
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
	case lexer.TOKEN_INTERFACE:
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
	case lexer.TOKEN_NONLOCAL:
		return p.parseNonLocalStatementIterative()
	case lexer.TOKEN_PRINTLN:
		return p.parseExpressionStatementIterative()
	case lexer.TOKEN_IDENT:
		if p.peekTok.Type == lexer.TOKEN_IDENT || p.peekTok.Type == lexer.TOKEN_QUESTION || p.peekTok.Type == lexer.TOKEN_MULTIPLY {
			if stmt := p.parseVariableDeclarationIterative(); stmt != nil {
				return stmt
			}
		}
		if p.peekTok.Type == lexer.TOKEN_LBRACE {
			return p.parsePrefixCallStatementIterative()
		}
		return p.parseExpressionStatementIterative()
	case lexer.TOKEN_CONSTRUCTOR:
		return nil
	case lexer.TOKEN_SEMICOLON:
		return nil
	default:
		return p.parseExpressionStatementIterative()
	}
}

// parseVariableDeclarationIterative 迭代解析变量声明
func (p *Parser) parseVariableDeclarationIterative() *ast.VariableDeclaration {
	stmt := &ast.VariableDeclaration{}
	if p.curTok.Type == lexer.TOKEN_IDENT {
		stmt.Type = p.curTok.Value
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
		if p.curTok.Type == lexer.TOKEN_IDENT {
			stmt.Name = p.curTok.Value
			p.nextToken()
			if p.curTok.Type == lexer.TOKEN_ASSIGN {
				p.nextToken()
				stmt.Value = p.parseExpressionIterative()
			}
			return stmt
		}
	}
	return nil
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
			for p.curTok.Type != lexer.TOKEN_RBRACE {
				bodyStmt := p.parseStatementIterative()
				if bodyStmt != nil {
					callBody = append(callBody, bodyStmt)
					p.log("call 语句体添加语句")
				}
				if p.curTok.Type != lexer.TOKEN_RBRACE {
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
		for p.curTok.Type != lexer.TOKEN_RBRACE {
			bodyStmt := p.parseStatementIterative()
			if bodyStmt != nil {
			}
			if p.curTok.Type != lexer.TOKEN_RBRACE {
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
func (p *Parser) parseSpendCallStatementIterative() *ast.SpendCallStatement {
	p.log("开始解析 spend/call 语句")
	stmt := &ast.SpendCallStatement{
		Calls: []ast.CallStatement{},
	}
	p.nextToken()
	if p.curTok.Type == lexer.TOKEN_LPAREN {
		p.nextToken()
		stmt.Spend = p.parseExpressionIterative()
		p.log("解析 spend 表达式")
		if p.curTok.Type == lexer.TOKEN_RPAREN {
			p.nextToken()
		}
	}
	if p.curTok.Type == lexer.TOKEN_LBRACE {
		p.nextToken()
		p.log("开始解析 call 语句")
		for p.curTok.Type != lexer.TOKEN_RBRACE {
			if p.curTok.Type == lexer.TOKEN_CALL {
				callStmt := p.parseCallStatementIterative()
				stmt.Calls = append(stmt.Calls, *callStmt)
				p.log("添加 call 语句")
			} else {
				bodyStmt := p.parseStatementIterative()
				if bodyStmt != nil {
				}
			}
			if p.curTok.Type != lexer.TOKEN_RBRACE {
				p.nextToken()
			}
		}
		p.log("call 语句解析完成，共 %d 个 call", len(stmt.Calls))
		if p.curTok.Type == lexer.TOKEN_RBRACE {
			p.nextToken()
		}
	}
	p.log("spend/call 语句解析完成")
	return stmt
}

// parseTaskStatementIterative 迭代解析 task 语句
func (p *Parser) parseTaskStatementIterative() *ast.TaskStatement {
	stmt := &ast.TaskStatement{}
	p.nextToken()
	if p.curTok.Type == lexer.TOKEN_LPAREN {
		p.nextToken()
		if p.curTok.Type == lexer.TOKEN_INT {
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
	stmt := &ast.TreeStatement{}
	p.nextToken()
	if p.curTok.Type == lexer.TOKEN_LPAREN {
		p.nextToken()
		stmt.Root = p.parseExpressionIterative()
		if p.curTok.Type == lexer.TOKEN_RPAREN {
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
			for p.curTok.Type != lexer.TOKEN_RBRACE {
				if p.curTok.Type == lexer.TOKEN_IDENT {
					p.nextToken()
					if p.curTok.Type == lexer.TOKEN_COLON {
						p.nextToken()
						if p.curTok.Type == lexer.TOKEN_STRING || p.curTok.Type == lexer.TOKEN_INT || p.curTok.Type == lexer.TOKEN_FLOAT {
							p.parsePrimaryExpressionIterative()
						} else {
							p.parseExpressionIterative()
						}
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
			for p.curTok.Type != lexer.TOKEN_RBRACKET {
				p.parseExpressionIterative()
				if p.curTok.Type != lexer.TOKEN_RBRACKET {
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
	}
	p.nextToken()
	if p.curTok.Type == lexer.TOKEN_IDENT {
		stmt.Name = p.curTok.Value
		p.log("解析函数名：%s", stmt.Name)
		p.nextToken()
	}
	if p.curTok.Type == lexer.TOKEN_LPAREN {
		p.nextToken()
		p.log("开始解析函数参数")
		for p.curTok.Type != lexer.TOKEN_RPAREN && p.curTok.Type != lexer.TOKEN_EOF {
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
			if p.curTok.Type == lexer.TOKEN_COMMA {
				p.nextToken()
			}
		}
		p.log("函数参数解析完成，共 %d 个参数", len(stmt.Params))
		if p.curTok.Type == lexer.TOKEN_RPAREN {
			p.nextToken()
		}
	}
	if p.curTok.Type == lexer.TOKEN_COLON {
		p.nextToken()
	}
	if p.curTok.Type == lexer.TOKEN_LBRACE {
		p.nextToken()
		p.log("开始解析函数体")
		for p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
			bodyStmt := p.parseStatementIterative()
			if bodyStmt != nil {
				stmt.Body = append(stmt.Body, bodyStmt)
				p.log("函数体添加语句：%s", bodyStmt.String())
			}
		}
		p.log("函数体解析完成，共 %d 条语句", len(stmt.Body))
		// 不消费 RBRACE，让 parseProgram 来消费
	}
	p.log("函数语句解析完成")
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
	if p.curTok.Type == lexer.TOKEN_LPAREN {
		p.nextToken()
		stmt.Condition = p.parseExpressionIterative()
		if p.curTok.Type == lexer.TOKEN_RPAREN {
			p.nextToken()
		}
	}
	if p.curTok.Type == lexer.TOKEN_LBRACE {
		p.nextToken()
		for p.curTok.Type != lexer.TOKEN_RBRACE {
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
	if p.curTok.Type == lexer.TOKEN_ELSE {
		p.nextToken()
		if p.curTok.Type == lexer.TOKEN_LBRACE {
			p.nextToken()
			for p.curTok.Type != lexer.TOKEN_RBRACE {
				bodyStmt := p.parseStatementIterative()
				if bodyStmt != nil {
					stmt.Else = append(stmt.Else, bodyStmt)
				}
				if p.curTok.Type != lexer.TOKEN_RBRACE {
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
	if p.curTok.Type == lexer.TOKEN_LPAREN {
		p.nextToken()
		stmt.Condition = p.parseExpressionIterative()
		if p.curTok.Type == lexer.TOKEN_RPAREN {
			p.nextToken()
		}
	}
	if p.curTok.Type == lexer.TOKEN_LBRACE {
		p.nextToken()
		for p.curTok.Type != lexer.TOKEN_RBRACE {
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
		for p.curTok.Type != lexer.TOKEN_RBRACE {
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
		for p.peekTok.Type == lexer.TOKEN_DOT {
			p.nextToken()
			p.nextToken()
			if p.curTok.Type == lexer.TOKEN_IDENT {
				stmt.Module += "." + p.curTok.Value
			}
		}
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
		Fields:      []*ast.FieldDeclaration{},
		Methods:     []*ast.MethodStatement{},
		Constructors: []*ast.ConstructorStatement{},
		Implements:  []string{},
		Pos:         pos,
	}
	p.nextToken()
	if p.curTok.Type == lexer.TOKEN_IDENT {
		stmt.Name = p.curTok.Value
		p.nextToken()
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
		for p.curTok.Type != lexer.TOKEN_RBRACE {
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
		Fields: []*ast.FieldDeclaration{},
		Pos:    pos,
	}
	p.nextToken()
	if p.curTok.Type == lexer.TOKEN_IDENT {
		stmt.Name = p.curTok.Value
		p.nextToken()
	}
	if p.curTok.Type == lexer.TOKEN_LBRACE {
		p.nextToken()
		for p.curTok.Type != lexer.TOKEN_RBRACE && p.curTok.Type != lexer.TOKEN_EOF {
			p.log("当前 token: %s, 开始解析结构体字段", lexer.TokenTypeToString(p.curTok.Type))
			
			if field := p.parseFieldDeclarationIterative(); field != nil {
				p.log("解析完成字段声明：%s", field.String())
				stmt.Fields = append(stmt.Fields, field)
			} else if p.curTok.Type == lexer.TOKEN_SEMICOLON {
				p.log("跳过分号")
				p.nextToken()
			} else {
				p.log("跳过 token: %s", lexer.TokenTypeToString(p.curTok.Type))
				p.nextToken()
			}
		}
		p.log("解析完成结构体体")
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
	
	p.log("开始解析字段声明，当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
	
	if p.curTok.Type != lexer.TOKEN_IDENT {
		p.log("不是字段声明，返回 nil")
		return nil
	}
	
	savedCurTok := p.curTok
	savedPeekTok := p.peekTok
	p.log("保存 token 位置，curTok: %s, 值：%s, peekTok: %s, 值：%s", lexer.TokenTypeToString(savedCurTok.Type), savedCurTok.Value, lexer.TokenTypeToString(savedPeekTok.Type), savedPeekTok.Value)
	
	typeName := p.curTok.Value
	p.log("解析类型：%s", typeName)
	p.nextToken()
	p.log("跳过类型后，当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
	
	nullable := false
	if p.curTok.Type == lexer.TOKEN_QUESTION {
		nullable = true
		p.log("跳过 QUESTION token")
		p.nextToken()
		p.log("跳过 QUESTION 后，当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
	}
	
	if p.curTok.Type != lexer.TOKEN_IDENT {
		p.log("不是字段声明，恢复 token 位置")
		p.curTok = savedCurTok
		p.peekTok = savedPeekTok
		p.log("恢复 token 位置后，curTok: %s, 值：%s, peekTok: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value, lexer.TokenTypeToString(p.peekTok.Type), p.peekTok.Value)
		return nil
	}
	
	p.log("检查下一个 token 是否是分号，peekTok: %s, 值：%s", lexer.TokenTypeToString(p.peekTok.Type), p.peekTok.Value)
	if p.peekTok.Type != lexer.TOKEN_SEMICOLON {
		p.log("不是字段声明，恢复 token 位置")
		p.curTok = savedCurTok
		p.peekTok = savedPeekTok
		p.log("恢复 token 位置后，curTok: %s, 值：%s, peekTok: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value, lexer.TokenTypeToString(p.peekTok.Type), p.peekTok.Value)
		return nil
	}
	
	fieldName := p.curTok.Value
	p.log("解析字段名：%s", fieldName)
	p.nextToken()
	p.log("跳过字段名后，当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
	
	p.log("跳过分号")
	p.nextToken()
	p.log("跳过分号后，当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
	
	field := &ast.FieldDeclaration{
		Name:     fieldName,
		Type:     typeName,
		Nullable: nullable,
		Pos:      pos,
	}
	p.log("字段声明解析完成：%s", field.String())
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
	p.log("开始解析表达式语句，当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
	expr := p.parseExpressionIterative()
	p.log("解析表达式完成，当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
	if p.curTok.Type == lexer.TOKEN_SEMICOLON {
		p.log("消费分号")
		p.nextToken()
		p.log("当前 token: %s, 值：%s", lexer.TokenTypeToString(p.curTok.Type), p.curTok.Value)
	}
	stmt := &ast.ExpressionStatement{
		Expression: expr,
		Pos:        pos,
	}
	return stmt
}

// parseExpressionIterative 迭代解析表达式（使用栈替代递归）
func (p *Parser) parseExpressionIterative() ast.Expression {
	return p.parseBinaryExpressionIterative(0)
}

// parseBinaryExpressionIterative 迭代解析二元表达式（使用显式栈）
func (p *Parser) parseBinaryExpressionIterative(precedence int) ast.Expression {
	left := p.parsePrimaryExpressionIterative()
	
	for precedence < p.precedence(p.curTok.Type) {
		op := p.curTok.Type
		p.nextToken()
		
		var right ast.Expression
		if op == lexer.TOKEN_ASSIGN {
			right = p.parseBinaryExpressionIterative(p.precedence(op) - 1)
		} else {
			right = p.parseBinaryExpressionIterative(p.precedence(op))
		}
		
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
	case lexer.TOKEN_INT:
		return p.parseIntegerLiteralIterative()
	case lexer.TOKEN_FLOAT:
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
				Name: p.curTok.Value,
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
	case lexer.TOKEN_RBRACE, lexer.TOKEN_LBRACE, lexer.TOKEN_DOT, lexer.TOKEN_ASSIGN:
		return nil
	default:
		p.error(fmt.Sprintf("unexpected token: %s", lexer.TokenTypeToString(p.curTok.Type)))
		p.nextToken()
		return nil
	}
}

// parseIdentifierIterative 迭代解析标识符
func (p *Parser) parseIdentifierIterative() ast.Expression {
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
	if p.curTok.Type == lexer.TOKEN_PREFIX_REF {
		p.nextToken()
		return ident
	}
	if p.curTok.Type == lexer.TOKEN_LPAREN {
		return p.parseCallExpressionIterative(ident)
	}
	if p.curTok.Type == lexer.TOKEN_DOT {
		p.nextToken()
		if p.curTok.Type == lexer.TOKEN_IDENT || p.curTok.Type == lexer.TOKEN_PRINTLN {
			memberName := p.curTok.Value
			memberPos := ast.Position{
				Line:   p.curTok.Line,
				Column: p.curTok.Column,
				File:   p.file,
			}
			p.nextToken()
			memberAccess := &ast.MemberAccessExpression{
				Object: ident,
				Member: memberName,
				Pos:    memberPos,
			}
			if p.curTok.Type == lexer.TOKEN_LPAREN {
				return p.parseCallExpressionIterative(memberAccess)
			}
			return memberAccess
		}
	}
	return ident
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
	}
	return expr
}

// parseCallExpressionIterative 迭代解析函数调用表达式
func (p *Parser) parseCallExpressionIterative(function ast.Expression) ast.Expression {
	call := &ast.CallExpression{
		Function: function,
		Args:     []ast.Expression{},
	}
	p.nextToken()
	for p.curTok.Type != lexer.TOKEN_RPAREN {
		if p.curTok.Type == lexer.TOKEN_IDENT && p.peekTok.Type == lexer.TOKEN_COLON {
			p.nextToken()
			p.nextToken()
			arg := p.parseExpressionIterative()
			call.Args = append(call.Args, arg)
		} else {
			arg := p.parseExpressionIterative()
			call.Args = append(call.Args, arg)
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
	index := &ast.IndexExpression{}
	p.nextToken()
	index.Object = p.parseExpressionIterative()
	if p.curTok.Type == lexer.TOKEN_RBRACKET {
		p.nextToken()
	}
	return index
}

// parsePrefixCallStatementIterative 迭代解析前缀调用语句
func (p *Parser) parsePrefixCallStatementIterative() *ast.ExpressionStatement {
	ident := &ast.Identifier{
		Name: p.curTok.Value,
	}
	p.nextToken()
	if p.peekTok.Type == lexer.TOKEN_LBRACE {
		p.nextToken()
		if p.curTok.Type == lexer.TOKEN_LBRACE {
			p.nextToken()
			blockBody := []ast.Statement{}
			for p.curTok.Type != lexer.TOKEN_RBRACE {
				bodyStmt := p.parseStatementIterative()
				if bodyStmt != nil {
					blockBody = append(blockBody, bodyStmt)
				}
				if p.curTok.Type != lexer.TOKEN_RBRACE {
					p.nextToken()
				}
			}
			if p.curTok.Type == lexer.TOKEN_RBRACE {
				p.nextToken()
			}
			prefixCall := &ast.PrefixCallExpression{
				Name: ident.Name,
				Body: blockBody,
			}
			return &ast.ExpressionStatement{
				Expression: prefixCall,
			}
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
	if p.HasErrors() {
		p.log("解析完成，发现错误")
		p.ReportErrors()
	} else {
		p.log("解析完成，未发现错误")
		p.Validate(program)
		if p.HasErrors() {
			p.ReportErrors()
		}
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
		if spendStmt, ok := stmt.(*ast.SpendCallStatement); ok {
			if spendStmt.Spend == nil {
				p.error("spend 语句缺少表达式")
			}
			if len(spendStmt.Calls) == 0 {
				p.error("spend 语句缺少 call 语句")
			}
			for i, call := range spendStmt.Calls {
				if call.Target == nil {
					p.error(fmt.Sprintf("call 语句 %d 缺少目标", i+1))
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
