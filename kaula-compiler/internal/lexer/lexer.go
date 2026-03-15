package lexer

import (
	"fmt"
	"strings"
	"unicode"

	"kaula-compiler/internal/errors"
)

// TokenType 表示token的类型
type TokenType int

const (
	// 关键字
	TOKEN_VO TokenType = iota
	TOKEN_SPEND
	TOKEN_CALL
	TOKEN_SPEND_CALL
	TOKEN_TASK
	TOKEN_PREFIX
	TOKEN_TREE
	TOKEN_OBJECT
	TOKEN_FUNC
	TOKEN_IF
	TOKEN_ELSE
	TOKEN_WHILE
	TOKEN_FOR
	TOKEN_SWITCH
	TOKEN_CASE
	TOKEN_DEFAULT
	TOKEN_RETURN
	TOKEN_IMPORT
	TOKEN_SELF
	TOKEN_NONLOCAL
	TOKEN_PRINTLN
	TOKEN_CLASS
	TOKEN_INTERFACE
	TOKEN_IMPLEMENTS
	TOKEN_CONSTRUCTOR
	TOKEN_STRUCT

	// 标识符
	TOKEN_IDENT

	// 字面量
	TOKEN_INT
	TOKEN_FLOAT
	TOKEN_STRING
	TOKEN_TRUE
	TOKEN_FALSE

	// 运算符
	TOKEN_PLUS
	TOKEN_MINUS
	TOKEN_MULTIPLY
	TOKEN_DIVIDE
	TOKEN_MOD
	TOKEN_ASSIGN
	TOKEN_EQ
	TOKEN_NE
	TOKEN_LT
	TOKEN_GT
	TOKEN_AND
	TOKEN_OR
	TOKEN_LE
	TOKEN_GE
	TOKEN_PREFIX_REF
	TOKEN_QUESTION
	
	// 特殊值
	TOKEN_NULL

	// 分隔符
	TOKEN_LPAREN
	TOKEN_RPAREN
	TOKEN_LBRACE
	TOKEN_RBRACE
	TOKEN_LBRACKET
	TOKEN_RBRACKET
	TOKEN_SEMICOLON
	TOKEN_COMMA
	TOKEN_COLON
	TOKEN_DOUBLE_COLON
	TOKEN_DOT

	// 其他
	TOKEN_COMMENT
	TOKEN_EOF
)

// Token 表示一个token
type Token struct {
	Type    TokenType
	Value   string
	Line    int
	Column  int
}

// Lexer 表示词法分析器
type Lexer struct {
	input  string
	pos    int
	line   int
	column int
	inputLen int // 缓存输入长度，避免重复计算
	errorCollector *errors.ErrorCollector
	file string
}

// NewLexer 创建一个新的词法分析器
func NewLexer(input string) *Lexer {
	return &Lexer{
		input:  input,
		pos:    0,
		line:   1,
		column: 1,
		inputLen: len(input),
		errorCollector: errors.NewErrorCollector(),
	}
}

// Next 返回下一个token
func (l *Lexer) Next() Token {
	for l.pos < l.inputLen {
		char := l.input[l.pos]
		switch {
		case unicode.IsSpace(rune(char)):
			l.skipWhitespace()
		case char == '#':
			l.skipComment()
		case char == '/' && l.peek() == '/':
			l.skipComment()
		case unicode.IsLetter(rune(char)) || char == '_' || (char >= 0x80): // 允许中文字符
			return l.scanIdentifier()
		case unicode.IsDigit(rune(char)):
			return l.scanNumber()
		case char == '"':
			return l.scanString()
		case char == '+':
			l.next()
			return Token{Type: TOKEN_PLUS, Value: "+", Line: l.line, Column: l.column}
		case char == '-':
			l.next()
			return Token{Type: TOKEN_MINUS, Value: "-", Line: l.line, Column: l.column}
		case char == '*':
			l.next()
			return Token{Type: TOKEN_MULTIPLY, Value: "*", Line: l.line, Column: l.column}
		case char == '$':
			l.next()
			return Token{Type: TOKEN_PREFIX_REF, Value: "$", Line: l.line, Column: l.column}
		case char == '/':
			l.next()
			return Token{Type: TOKEN_DIVIDE, Value: "/", Line: l.line, Column: l.column}
		case char == '%':
			l.next()
			return Token{Type: TOKEN_MOD, Value: "%", Line: l.line, Column: l.column}
		case char == '=':
			if l.peek() == '=' {
				l.next()
				l.next()
				return Token{Type: TOKEN_EQ, Value: "==", Line: l.line, Column: l.column}
			} else {
				l.next()
				return Token{Type: TOKEN_ASSIGN, Value: "=", Line: l.line, Column: l.column}
			}
		case char == '!':
			if l.peek() == '=' {
				l.next()
				l.next()
				return Token{Type: TOKEN_NE, Value: "!=", Line: l.line, Column: l.column}
			} else {
				l.error("unexpected token")
				continue
			}
		case char == '<':
			if l.peek() == '=' {
				l.next()
				l.next()
				return Token{Type: TOKEN_LE, Value: "<=", Line: l.line, Column: l.column}
			} else {
				l.next()
				return Token{Type: TOKEN_LT, Value: "<", Line: l.line, Column: l.column}
			}
		case char == '>':
			if l.peek() == '=' {
				l.next()
				l.next()
				return Token{Type: TOKEN_GE, Value: ">=", Line: l.line, Column: l.column}
			} else {
				l.next()
				return Token{Type: TOKEN_GT, Value: ">", Line: l.line, Column: l.column}
			}
		case char == '(':
			l.next()
			return Token{Type: TOKEN_LPAREN, Value: "(", Line: l.line, Column: l.column}
		case char == ')':
			l.next()
			return Token{Type: TOKEN_RPAREN, Value: ")", Line: l.line, Column: l.column}
		case char == '{':
			l.next()
			return Token{Type: TOKEN_LBRACE, Value: "{", Line: l.line, Column: l.column}
		case char == '}':
			l.next()
			return Token{Type: TOKEN_RBRACE, Value: "}", Line: l.line, Column: l.column}
		case char == '[':
			l.next()
			return Token{Type: TOKEN_LBRACKET, Value: "[", Line: l.line, Column: l.column}
		case char == ']':
			l.next()
			return Token{Type: TOKEN_RBRACKET, Value: "]", Line: l.line, Column: l.column}
		case char == ';':
			l.next()
			return Token{Type: TOKEN_SEMICOLON, Value: ";", Line: l.line, Column: l.column}
		case char == ',':
			l.next()
			return Token{Type: TOKEN_COMMA, Value: ",", Line: l.line, Column: l.column}
		case char == ':':
			if l.peek() == ':' {
				l.next()
				l.next()
				return Token{Type: TOKEN_DOUBLE_COLON, Value: "::", Line: l.line, Column: l.column}
			} else {
				l.next()
				return Token{Type: TOKEN_COLON, Value: ":", Line: l.line, Column: l.column}
			}
		case char == '&':
			if l.peek() == '&' {
				l.next()
				l.next()
				return Token{Type: TOKEN_AND, Value: "&&", Line: l.line, Column: l.column}
			} else {
				l.error("unexpected token")
				continue
			}
		case char == '|':
			if l.peek() == '|' {
				l.next()
				l.next()
				return Token{Type: TOKEN_OR, Value: "||", Line: l.line, Column: l.column}
			} else {
				l.error("unexpected token")
				continue
			}
		case char == '.':
			l.next()
			return Token{Type: TOKEN_DOT, Value: ".", Line: l.line, Column: l.column}
		case char == '?':
			l.next()
			return Token{Type: TOKEN_QUESTION, Value: "?", Line: l.line, Column: l.column}
		default:
			l.error(fmt.Sprintf("unexpected character: %c", char))
			continue
		}
	}
	return Token{Type: TOKEN_EOF, Value: "", Line: l.line, Column: l.column}
}

// skipWhitespace 跳过空白字符
func (l *Lexer) skipWhitespace() {
	for l.pos < l.inputLen && unicode.IsSpace(rune(l.input[l.pos])) {
		if l.input[l.pos] == '\n' {
			l.line++
			l.column = 1
		} else {
			l.column++
		}
		l.pos++
	}
}

// skipComment 跳过注释
func (l *Lexer) skipComment() {
	// 跳过注释标记
	if l.pos+1 < l.inputLen && l.input[l.pos] == '/' && l.input[l.pos+1] == '/' {
		l.pos += 2
	} else if l.input[l.pos] == '#' {
		l.pos++
	}
	
	start := l.pos
	for l.pos < l.inputLen && l.input[l.pos] != '\n' {
		l.pos++
	}
	l.column += l.pos - start
}

// scanIdentifier 扫描标识符
func (l *Lexer) scanIdentifier() Token {
	start := l.pos
	for l.pos < l.inputLen && (unicode.IsLetter(rune(l.input[l.pos])) || unicode.IsDigit(rune(l.input[l.pos])) || l.input[l.pos] == '_') {
		l.pos++
	}
	value := l.input[start:l.pos]
	tokenType := TOKEN_IDENT
	
	// 检查关键字
	switch value {
	case "vo":
		tokenType = TOKEN_VO
	case "spend":
		tokenType = TOKEN_SPEND
	case "call":
		tokenType = TOKEN_CALL
	case "task":
		tokenType = TOKEN_TASK
	case "prefix":
		tokenType = TOKEN_PREFIX
	case "tree":
		tokenType = TOKEN_TREE
	case "object":
		tokenType = TOKEN_OBJECT
	case "fn":
		tokenType = TOKEN_FUNC
	case "if":
		tokenType = TOKEN_IF
	case "else":
		tokenType = TOKEN_ELSE
	case "while":
		tokenType = TOKEN_WHILE
	case "for":
		tokenType = TOKEN_FOR
	case "switch":
		tokenType = TOKEN_SWITCH
	case "case":
		tokenType = TOKEN_CASE
	case "default":
		tokenType = TOKEN_DEFAULT
	case "return":
		tokenType = TOKEN_RETURN
	case "import":
		tokenType = TOKEN_IMPORT
	case "self":
		tokenType = TOKEN_SELF
	case "nonlocal":
		tokenType = TOKEN_NONLOCAL
	case "println":
		tokenType = TOKEN_PRINTLN
	case "class":
		tokenType = TOKEN_CLASS
	case "interface":
		tokenType = TOKEN_INTERFACE
	case "implements":
		tokenType = TOKEN_IMPLEMENTS
	case "constructor":
		tokenType = TOKEN_CONSTRUCTOR
	case "struct":
		tokenType = TOKEN_STRUCT
	case "this":
		tokenType = TOKEN_IDENT
	case "true":
		tokenType = TOKEN_TRUE
	case "false":
		tokenType = TOKEN_FALSE
	case "null":
		tokenType = TOKEN_NULL
	}
	l.column += l.pos - start
	return Token{Type: tokenType, Value: value, Line: l.line, Column: l.column}
}

// scanNumber 扫描数字
func (l *Lexer) scanNumber() Token {
	start := l.pos
	// 整数部分
	for l.pos < l.inputLen && unicode.IsDigit(rune(l.input[l.pos])) {
		l.pos++
	}
	// 小数部分
	if l.pos < l.inputLen && l.input[l.pos] == '.' {
		l.pos++
		for l.pos < l.inputLen && unicode.IsDigit(rune(l.input[l.pos])) {
			l.pos++
		}
		l.column += l.pos - start
		return Token{Type: TOKEN_FLOAT, Value: l.input[start:l.pos], Line: l.line, Column: l.column}
	} else {
		l.column += l.pos - start
		return Token{Type: TOKEN_INT, Value: l.input[start:l.pos], Line: l.line, Column: l.column}
	}
}

// scanString 扫描字符串
func (l *Lexer) scanString() Token {
	l.next() // 跳过开头的 "
	start := l.pos
	for l.pos < l.inputLen && l.input[l.pos] != '"' {
		if l.input[l.pos] == '\\' {
			l.pos++ // 跳过转义字符
		}
		l.pos++
	}
	if l.pos >= l.inputLen {
		l.error("unterminated string")
		return Token{Type: TOKEN_STRING, Value: "", Line: l.line, Column: l.column}
	}
	value := l.input[start:l.pos]
	// 处理转义字符
	value = strings.ReplaceAll(value, "\\n", "\n")
	value = strings.ReplaceAll(value, "\\t", "\t")
	value = strings.ReplaceAll(value, "\\\"", "\"")
	value = strings.ReplaceAll(value, "\\\\", "\\")
	l.next() // 跳过结尾的 "
	l.column += l.pos - start + 2 // +2 for the quotes
	return Token{Type: TOKEN_STRING, Value: value, Line: l.line, Column: l.column}
}

// next 前进到下一个字符
func (l *Lexer) next() {
	if l.pos < l.inputLen {
		if l.input[l.pos] == '\n' {
			l.line++
			l.column = 1
		} else {
			l.column++
		}
		l.pos++
	}
}

// peek 查看下一个字符
func (l *Lexer) peek() byte {
	if l.pos+1 < l.inputLen {
		return l.input[l.pos+1]
	}
	return 0
}

// error 报告错误
func (l *Lexer) error(message string) {
	suggestion := errors.GenerateSuggestion(message)
	l.errorCollector.AddSyntaxError(message, l.line, l.column, l.file, suggestion)
	// 跳过当前字符，继续解析
	l.next()
}

// SetFile 设置文件名
func (l *Lexer) SetFile(file string) {
	l.file = file
}

// SetErrorCollector 设置错误收集器
func (l *Lexer) SetErrorCollector(errorCollector *errors.ErrorCollector) {
	l.errorCollector = errorCollector
}

// GetErrorCollector 获取错误收集器
func (l *Lexer) GetErrorCollector() *errors.ErrorCollector {
	return l.errorCollector
}

// HasErrors 检查是否有错误
func (l *Lexer) HasErrors() bool {
	return l.errorCollector.HasErrors()
}

// ReportErrors 报告错误
func (l *Lexer) ReportErrors() {
	l.errorCollector.ReportErrors()
}

// TokenTypeToString 将token类型转换为字符串
func TokenTypeToString(tokenType TokenType) string {
	switch tokenType {
	case TOKEN_VO:
		return "VO"
	case TOKEN_SPEND:
		return "SPEND"
	case TOKEN_CALL:
		return "CALL"
	case TOKEN_TASK:
		return "TASK"
	case TOKEN_PREFIX:
		return "PREFIX"
	case TOKEN_TREE:
		return "TREE"
	case TOKEN_OBJECT:
		return "OBJECT"
	case TOKEN_FUNC:
		return "FUNC"
	case TOKEN_IF:
		return "IF"
	case TOKEN_ELSE:
		return "ELSE"
	case TOKEN_WHILE:
		return "WHILE"
	case TOKEN_FOR:
		return "FOR"
	case TOKEN_SWITCH:
		return "SWITCH"
	case TOKEN_CASE:
		return "CASE"
	case TOKEN_DEFAULT:
		return "DEFAULT"
	case TOKEN_RETURN:
		return "RETURN"
	case TOKEN_IMPORT:
		return "IMPORT"
	case TOKEN_SELF:
		return "SELF"
	case TOKEN_NONLOCAL:
		return "NONLOCAL"
	case TOKEN_PRINTLN:
		return "PRINTLN"
	case TOKEN_CLASS:
		return "CLASS"
	case TOKEN_INTERFACE:
		return "INTERFACE"
	case TOKEN_IMPLEMENTS:
		return "IMPLEMENTS"
	case TOKEN_CONSTRUCTOR:
		return "CONSTRUCTOR"
	case TOKEN_STRUCT:
		return "STRUCT"
	case TOKEN_IDENT:
		return "IDENT"
	case TOKEN_INT:
		return "INT"
	case TOKEN_FLOAT:
		return "FLOAT"
	case TOKEN_STRING:
		return "STRING"
	case TOKEN_TRUE:
		return "TRUE"
	case TOKEN_FALSE:
		return "FALSE"
	case TOKEN_PLUS:
		return "PLUS"
	case TOKEN_MINUS:
		return "MINUS"
	case TOKEN_MULTIPLY:
		return "MULTIPLY"
	case TOKEN_DIVIDE:
		return "DIVIDE"
	case TOKEN_MOD:
		return "MOD"
	case TOKEN_ASSIGN:
		return "ASSIGN"
	case TOKEN_EQ:
		return "EQ"
	case TOKEN_NE:
		return "NE"
	case TOKEN_LT:
		return "LT"
	case TOKEN_GT:
		return "GT"
	case TOKEN_LE:
		return "LE"
	case TOKEN_GE:
		return "GE"
	case TOKEN_AND:
		return "AND"
	case TOKEN_OR:
		return "OR"
	case TOKEN_PREFIX_REF:
		return "PREFIX_REF"
	case TOKEN_QUESTION:
		return "QUESTION"
	case TOKEN_NULL:
		return "NULL"
	case TOKEN_LPAREN:
		return "LPAREN"
	case TOKEN_RPAREN:
		return "RPAREN"
	case TOKEN_LBRACE:
		return "LBRACE"
	case TOKEN_RBRACE:
		return "RBRACE"
	case TOKEN_LBRACKET:
		return "LBRACKET"
	case TOKEN_RBRACKET:
		return "RBRACKET"
	case TOKEN_SEMICOLON:
		return "SEMICOLON"
	case TOKEN_COMMA:
		return "COMMA"
	case TOKEN_COLON:
		return "COLON"
	case TOKEN_DOUBLE_COLON:
		return "DOUBLE_COLON"
	case TOKEN_DOT:
		return "DOT"
	case TOKEN_COMMENT:
		return "COMMENT"
	case TOKEN_EOF:
		return "EOF"
	default:
		return "UNKNOWN"
	}
}

// String 将token转换为字符串
func (t Token) String() string {
	return fmt.Sprintf("%s(%q) at line %d, column %d", TokenTypeToString(t.Type), t.Value, t.Line, t.Column)
}