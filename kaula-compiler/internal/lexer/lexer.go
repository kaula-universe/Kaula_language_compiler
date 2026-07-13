package lexer

import (
	"fmt"
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
	TOKEN_ASYNC
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
	TOKEN_EXPORT
	TOKEN_PACKAGE
	TOKEN_PUB
	TOKEN_SELF
	TOKEN_NONLOCAL
	TOKEN_PRINTLN
	TOKEN_BREAK
	TOKEN_CONTINUE
	TOKEN_CLASS
	TOKEN_LITERAL_INTERFACE
	TOKEN_IMPLEMENTS
	TOKEN_CONSTRUCTOR
	TOKEN_STRUCT
	TOKEN_AUTO
	TOKEN_YEIDE
	TOKEN_RELEASE
	TOKEN_EXTRACT
	TOKEN_TYPE
	TOKEN_SIZEOF
	TOKEN_ALIGNOF
	TOKEN_OFFSETOF
	TOKEN_COMPTIME
	TOKEN_TYPE_NAME
	TOKEN_FIELD_COUNT
	TOKEN_FIELD_NAME
	TOKEN_FIELD_TYPE
	TOKEN_TYPE_KIND
	TOKEN_ENUM
	TOKEN_MATCH
	TOKEN_ARROW
	TOKEN_EXTERN
	TOKEN_STATIC
	TOKEN_CONST
	// 类型关键字
	TOKEN_TYPE_INT
	TOKEN_TYPE_FLOAT
	TOKEN_TYPE_DOUBLE
	TOKEN_TYPE_BOOL
	TOKEN_TYPE_CHAR
	TOKEN_TYPE_STRING
	TOKEN_TYPE_VOID

	// 标识符
	TOKEN_IDENT

	// 字面量
	TOKEN_LITERAL_INT
	TOKEN_LITERAL_FLOAT
	TOKEN_LITERAL_CHAR
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
	TOKEN_AMPERSAND
	TOKEN_OR
	TOKEN_LE
	TOKEN_GE
	TOKEN_LSHIFT
	TOKEN_RSHIFT
	TOKEN_PIPE
	TOKEN_TILDE
	TOKEN_XOR
	TOKEN_PREFIX_REF
	TOKEN_QUESTION
	TOKEN_AT // @ 符号用于前缀调用

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
	TOKEN_ATTRIBUTE // #[ 注解标记
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
	source string // 保存完整源码用于错误上下文
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
		source: input,
	}
}

func isASCIISpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v'
}

func isASCIILetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isASCIIDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

func isASCIIAlnum(c byte) bool {
	return isASCIILetter(c) || isASCIIDigit(c)
}

// Next 返回下一个token
func (l *Lexer) Next() Token {
	for l.pos < l.inputLen {
		char := l.input[l.pos]
		switch {
		case char < 0x80 && isASCIISpace(char):
			l.skipWhitespace()
			continue
		case char >= 0x80 && unicode.IsSpace(rune(char)):
			l.skipWhitespace()
			continue
		case char == '#':
			if l.pos+1 < l.inputLen && l.input[l.pos+1] == '[' {
				// 注解标记 #[...]
				startLine := l.line
				startColumn := l.column
				l.next() // 跳过 #
				l.next() // 跳过 [
				
				// 收集注解内容直到遇到 ]
				content := ""
				for l.pos < l.inputLen && l.input[l.pos] != ']' && l.input[l.pos] != '\n' {
					content += string(l.input[l.pos])
					l.next()
				}
				
				// 跳过 ]
				if l.pos < l.inputLen && l.input[l.pos] == ']' {
					l.next()
				}
				
				return Token{Type: TOKEN_ATTRIBUTE, Value: "#[" + content + "]", Line: startLine, Column: startColumn}
			} else {
				// 注释
				l.skipComment()
				continue
			}
		case char == '/' && l.peek() == '/':
			l.skipComment()
			continue
		case char < 0x80 && (isASCIILetter(char) || char == '_'):
			return l.scanIdentifier()
		case char >= 0x80 && (unicode.IsLetter(rune(char)) || char == '_' || char >= 0x80):
			return l.scanIdentifier()
		case char < 0x80 && isASCIIDigit(char):
			return l.scanNumber()
		case char >= 0x80 && unicode.IsDigit(rune(char)):
			return l.scanNumber()
		case char == '"':
			return l.scanString()
		case char == '\'':
			return l.scanCharLiteral()
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
		case char == '@':
			l.next()
			return Token{Type: TOKEN_AT, Value: "@", Line: l.line, Column: l.column}
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
			} else if l.peek() == '>' {
				l.next()
				l.next()
				return Token{Type: TOKEN_ARROW, Value: "=>", Line: l.line, Column: l.column}
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
			} else if l.peek() == '<' {
				l.next()
				l.next()
				return Token{Type: TOKEN_LSHIFT, Value: "<<", Line: l.line, Column: l.column}
			} else {
				l.next()
				return Token{Type: TOKEN_LT, Value: "<", Line: l.line, Column: l.column}
			}
		case char == '>':
			if l.peek() == '=' {
				l.next()
				l.next()
				return Token{Type: TOKEN_GE, Value: ">=", Line: l.line, Column: l.column}
			} else if l.peek() == '>' {
				l.next()
				l.next()
				return Token{Type: TOKEN_RSHIFT, Value: ">>", Line: l.line, Column: l.column}
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
			l.next()
			return Token{Type: TOKEN_AMPERSAND, Value: "&", Line: l.line, Column: l.column}
		}
		case char == '|':
			if l.peek() == '|' {
				l.next()
				l.next()
				return Token{Type: TOKEN_OR, Value: "||", Line: l.line, Column: l.column}
			} else {
				l.next()
				return Token{Type: TOKEN_PIPE, Value: "|", Line: l.line, Column: l.column}
			}
		case char == '.':
			l.next()
			return Token{Type: TOKEN_DOT, Value: ".", Line: l.line, Column: l.column}
		case char == '?':
			l.next()
			return Token{Type: TOKEN_QUESTION, Value: "?", Line: l.line, Column: l.column}
		case char == '^':
			l.next()
			return Token{Type: TOKEN_XOR, Value: "^", Line: l.line, Column: l.column}
		case char == '~':
			l.next()
			return Token{Type: TOKEN_TILDE, Value: "~", Line: l.line, Column: l.column}
		default:
			l.error(fmt.Sprintf("unexpected character: %c", char))
			continue
		}
	}
	return Token{Type: TOKEN_EOF, Value: "", Line: l.line, Column: l.column}
}

// skipWhitespace 跳过空白字符
func (l *Lexer) skipWhitespace() {
	for l.pos < l.inputLen {
		c := l.input[l.pos]
		if c < 0x80 && isASCIISpace(c) {
			if c == '\n' {
				l.line++
				l.column = 1
			} else {
				l.column++
			}
			l.pos++
		} else if c >= 0x80 && unicode.IsSpace(rune(c)) {
			if c == '\n' {
				l.line++
				l.column = 1
			} else {
				l.column++
			}
			l.pos++
		} else {
			break
		}
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
	for l.pos < l.inputLen {
		c := l.input[l.pos]
		if c < 0x80 && (isASCIILetter(c) || isASCIIDigit(c) || c == '_') {
			l.pos++
		} else if c >= 0x80 && (unicode.IsLetter(rune(c)) || unicode.IsDigit(rune(c)) || c == '_') {
			l.pos++
		} else {
			break
		}
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
	case "async":
		tokenType = TOKEN_ASYNC
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
	case "export":
		tokenType = TOKEN_EXPORT
	case "package":
		tokenType = TOKEN_PACKAGE
	case "pub":
		tokenType = TOKEN_PUB
	case "self":
		tokenType = TOKEN_SELF
	case "nonlocal":
		tokenType = TOKEN_NONLOCAL
	case "println":
		tokenType = TOKEN_PRINTLN
	case "break":
		tokenType = TOKEN_BREAK
	case "continue":
		tokenType = TOKEN_CONTINUE
	case "class":
		tokenType = TOKEN_CLASS
	case "interface":
		tokenType = TOKEN_LITERAL_INTERFACE
	case "implements":
		tokenType = TOKEN_IMPLEMENTS
	case "constructor":
		tokenType = TOKEN_CONSTRUCTOR
	// 类型关键字
	case "int":
		tokenType = TOKEN_TYPE_INT
	case "float":
		tokenType = TOKEN_TYPE_FLOAT
	case "double":
		tokenType = TOKEN_TYPE_DOUBLE
	case "bool":
		tokenType = TOKEN_TYPE_BOOL
	case "char":
		tokenType = TOKEN_TYPE_CHAR
	case "string":
		tokenType = TOKEN_TYPE_STRING
	case "void":
		tokenType = TOKEN_TYPE_VOID
	case "struct":
		tokenType = TOKEN_STRUCT
	case "auto":
		tokenType = TOKEN_AUTO
	case "yeide":
		tokenType = TOKEN_YEIDE
	case "release":
		tokenType = TOKEN_RELEASE
	case "extract":
		tokenType = TOKEN_EXTRACT
	case "type":
		tokenType = TOKEN_TYPE
	case "sizeof":
		tokenType = TOKEN_SIZEOF
	case "alignof":
		tokenType = TOKEN_ALIGNOF
	case "offsetof":
		tokenType = TOKEN_OFFSETOF
	case "comptime":
		tokenType = TOKEN_COMPTIME
	case "type_name":
		tokenType = TOKEN_TYPE_NAME
	case "field_count":
		tokenType = TOKEN_FIELD_COUNT
	case "field_name":
		tokenType = TOKEN_FIELD_NAME
	case "field_type":
		tokenType = TOKEN_FIELD_TYPE
	case "type_kind":
		tokenType = TOKEN_TYPE_KIND
	case "enum":
		tokenType = TOKEN_ENUM
	case "match":
		tokenType = TOKEN_MATCH
	case "extern":
		tokenType = TOKEN_EXTERN
	case "static":
		tokenType = TOKEN_STATIC
	case "const":
		tokenType = TOKEN_CONST
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

// scanNumber 扫描数字（支持 0x/0o/0b 前缀）
func (l *Lexer) scanNumber() Token {
	start := l.pos

	// 检查 0x/0o/0b 前缀
	if l.pos < l.inputLen && l.input[l.pos] == '0' && l.pos+1 < l.inputLen {
		next := l.input[l.pos+1]
		if next == 'x' || next == 'X' {
			// 十六进制 0x...
			l.pos += 2
			for l.pos < l.inputLen {
				c := l.input[l.pos]
				if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
					l.pos++
				} else {
					break
				}
			}
			l.column += l.pos - start
			return Token{Type: TOKEN_LITERAL_INT, Value: l.input[start:l.pos], Line: l.line, Column: l.column}
		} else if next == 'o' || next == 'O' {
			// 八进制 0o...
			l.pos += 2
			for l.pos < l.inputLen {
				c := l.input[l.pos]
				if c >= '0' && c <= '7' {
					l.pos++
				} else {
					break
				}
			}
			l.column += l.pos - start
			return Token{Type: TOKEN_LITERAL_INT, Value: l.input[start:l.pos], Line: l.line, Column: l.column}
		} else if next == 'b' || next == 'B' {
			// 二进制 0b...
			l.pos += 2
			for l.pos < l.inputLen {
				c := l.input[l.pos]
				if c == '0' || c == '1' {
					l.pos++
				} else {
					break
				}
			}
			l.column += l.pos - start
			return Token{Type: TOKEN_LITERAL_INT, Value: l.input[start:l.pos], Line: l.line, Column: l.column}
		}
	}

	// 普通十进制/浮点数
	for l.pos < l.inputLen {
		c := l.input[l.pos]
		if c < 0x80 && isASCIIDigit(c) {
			l.pos++
		} else if c >= 0x80 && unicode.IsDigit(rune(c)) {
			l.pos++
		} else {
			break
		}
	}
	if l.pos < l.inputLen && l.input[l.pos] == '.' {
		l.pos++
		for l.pos < l.inputLen {
			c := l.input[l.pos]
			if c < 0x80 && isASCIIDigit(c) {
				l.pos++
			} else if c >= 0x80 && unicode.IsDigit(rune(c)) {
				l.pos++
			} else {
				break
			}
		}
		l.column += l.pos - start
		return Token{Type: TOKEN_LITERAL_FLOAT, Value: l.input[start:l.pos], Line: l.line, Column: l.column}
	} else {
		l.column += l.pos - start
		return Token{Type: TOKEN_LITERAL_INT, Value: l.input[start:l.pos], Line: l.line, Column: l.column}
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
	// 不处理转义字符，保持原始字符串内容
	// 让代码生成器决定如何处理换行符
	l.next() // 跳过结尾的 "
	l.column += l.pos - start + 2 // +2 for the quotes
	return Token{Type: TOKEN_STRING, Value: value, Line: l.line, Column: l.column}
}

// scanCharLiteral 扫描单引号字符字面量，如 '/'、'\n'
func (l *Lexer) scanCharLiteral() Token {
	l.next() // 跳过开头的 '
	if l.pos >= l.inputLen {
		l.error("unterminated character literal")
		return Token{Type: TOKEN_LITERAL_CHAR, Value: "", Line: l.line, Column: l.column}
	}
	
	charValue := ""
	if l.input[l.pos] == '\\' {
		// 转义字符
		l.pos++
		if l.pos >= l.inputLen {
			l.error("unterminated character literal")
			return Token{Type: TOKEN_LITERAL_CHAR, Value: "", Line: l.line, Column: l.column}
		}
		escapeChar := l.input[l.pos]
		switch escapeChar {
		case 'n':
			charValue = "\\n"
		case 'r':
			charValue = "\\r"
		case 't':
			charValue = "\\t"
		case '\\':
			charValue = "\\\\"
		case '\'':
			charValue = "\\'"
		case '"':
			charValue = "\\\""
		default:
			charValue = string(escapeChar)
		}
	} else {
		charValue = string(l.input[l.pos])
	}
	l.pos++
	
	if l.pos < l.inputLen && l.input[l.pos] == '\'' {
		l.next() // 跳过结尾的 '
	} else {
		l.error("unterminated character literal")
	}
	
	return Token{Type: TOKEN_LITERAL_CHAR, Value: charValue, Line: l.line, Column: l.column}
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
	context, sourceLine, lineNumStr := errors.ExtractSourceContext(l.source, l.line, l.column)
	err := &errors.Error{
		Type:       errors.ErrorSyntax,
		Message:    message,
		Line:       l.line,
		Column:     l.column,
		File:       l.file,
		Suggestion: suggestion,
		SourceContext: context,
		SourceLine: sourceLine,
		LineNumberStr: lineNumStr,
	}
	l.errorCollector.AddErrorInstance(err)
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

// GetSource 获取源码
func (l *Lexer) GetSource() string {
	return l.source
}

// GetPosition 获取当前位置
func (l *Lexer) GetPosition() int {
	return l.pos
}

// SetPosition 设置当前位置
func (l *Lexer) SetPosition(pos int) {
	l.pos = pos
}

// ScanUntilRbrace 扫描直到遇到 '}'，返回扫描的内容（不包括 '}'）
// 用于 #[asm] 函数体的原始内容透传
func (l *Lexer) ScanUntilRbrace() string {
	result := ""
	for l.pos < l.inputLen && l.input[l.pos] != '}' {
		result += string(l.input[l.pos])
		l.next()
	}
	return result
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
	case TOKEN_ASYNC:
		return "ASYNC"
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
	case TOKEN_PACKAGE:
		return "PACKAGE"
	case TOKEN_PUB:
		return "PUB"
	case TOKEN_SELF:
		return "SELF"
	case TOKEN_NONLOCAL:
		return "NONLOCAL"
	case TOKEN_PRINTLN:
		return "PRINTLN"
	case TOKEN_BREAK:
		return "BREAK"
	case TOKEN_CONTINUE:
		return "CONTINUE"
	case TOKEN_CLASS:
		return "CLASS"
	case TOKEN_LITERAL_INTERFACE:
		return "INTERFACE"
	case TOKEN_IMPLEMENTS:
		return "IMPLEMENTS"
	case TOKEN_CONSTRUCTOR:
		return "CONSTRUCTOR"
	case TOKEN_STRUCT:
		return "STRUCT"
	case TOKEN_TYPE:
		return "TYPE"
	case TOKEN_TYPE_INT:
		return "INT"
	case TOKEN_TYPE_FLOAT:
		return "FLOAT"
	case TOKEN_TYPE_DOUBLE:
		return "DOUBLE"
	case TOKEN_TYPE_BOOL:
		return "BOOL"
	case TOKEN_TYPE_CHAR:
		return "CHAR"
	case TOKEN_TYPE_STRING:
		return "STRING"
	case TOKEN_TYPE_VOID:
		return "VOID"
	case TOKEN_AUTO:
		return "AUTO"
	case TOKEN_YEIDE:
		return "YEIDE"
	case TOKEN_RELEASE:
		return "RELEASE"
	case TOKEN_EXTRACT:
		return "EXTRACT"
	case TOKEN_SIZEOF:
		return "SIZEOF"
	case TOKEN_ALIGNOF:
		return "ALIGNOF"
	case TOKEN_OFFSETOF:
		return "OFFSETOF"
	case TOKEN_COMPTIME:
		return "COMPTIME"
	case TOKEN_TYPE_NAME:
		return "TYPE_NAME"
	case TOKEN_FIELD_COUNT:
		return "FIELD_COUNT"
	case TOKEN_FIELD_NAME:
		return "FIELD_NAME"
	case TOKEN_FIELD_TYPE:
		return "FIELD_TYPE"
	case TOKEN_TYPE_KIND:
		return "TYPE_KIND"
	case TOKEN_ENUM:
		return "ENUM"
	case TOKEN_MATCH:
		return "MATCH"
	case TOKEN_ARROW:
		return "ARROW"
	case TOKEN_EXTERN:
		return "EXTERN"
	case TOKEN_STATIC:
		return "STATIC"
	case TOKEN_CONST:
		return "CONST"
	case TOKEN_IDENT:
		return "IDENT"
	case TOKEN_LITERAL_INT:
		return "INT"
	case TOKEN_LITERAL_FLOAT:
		return "FLOAT"
	case TOKEN_LITERAL_CHAR:
		return "CHAR"
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
	case TOKEN_LSHIFT:
		return "LSHIFT"
	case TOKEN_RSHIFT:
		return "RSHIFT"
	case TOKEN_XOR:
		return "XOR"
	case TOKEN_PIPE:
		return "PIPE"
	case TOKEN_TILDE:
		return "TILDE"
	case TOKEN_AND:
		return "AND"
	case TOKEN_AMPERSAND:
		return "AMPERSAND"
	case TOKEN_OR:
		return "OR"
	case TOKEN_PREFIX_REF:
		return "PREFIX_REF"
	case TOKEN_AT:
		return "AT"
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
	case TOKEN_ATTRIBUTE:
		return "ATTRIBUTE"
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