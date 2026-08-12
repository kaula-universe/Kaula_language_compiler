package errors

import (
	"fmt"
	"os"
	"strings"
)

// 终端 ANSI 颜色常量（用于终端高亮错误/警告/成功）
const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
)

// 导出别名 (供 main 包 printSummary 使用)
const (
	ColorReset  = ansiReset
	ColorRed    = ansiRed
	ColorGreen  = ansiGreen
	ColorYellow = ansiYellow
)

// colorEnabled 是否启用终端颜色高亮
// 设置 NO_COLOR 环境变量或 TERM=dumb 时自动关闭
var colorEnabled = func() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	return true
}()

// HighlightSpan 表示错误在源码中的高亮区间（用于终端/IDE 专门高亮错误）
type HighlightSpan struct {
	File    string    // 错误所在文件名
	Line    int       // 起始行（1 起）
	Column  int       // 起始列（1 起）
	Length  int       // 高亮字符数（0 表示仅单列）
	Type    ErrorType // 错误类型（决定高亮颜色）
	Message string    // 错误消息
}

// BuildHighlight 构建错误高亮区间
// 传入源码以便自动把高亮扩展到错误位置的整个“词”（token）边界
func BuildHighlight(source string, line, column, length int, file string, typ ErrorType, message string) *HighlightSpan {
	span := &HighlightSpan{
		Type:    typ,
		Message: message,
		File:    file,
		Line:    line,
		Column:  column,
	}
	if length > 0 {
		span.Length = length
		return span
	}
	// 自动扩展到当前词边界，使高亮更醒目
	if source != "" {
		lines := strings.Split(source, "\n")
		if line >= 1 && line <= len(lines) {
			text := lines[line-1]
			start := column - 1
			if start < 0 {
				start = 0
			}
			if start < len(text) {
				end := start
				for end < len(text) && isWordChar(text[end]) {
					end++
				}
				if end > start {
					span.Length = end - start
				}
			}
		}
	}
	if span.Length < 1 {
		span.Length = 1
	}
	return span
}

// isWordChar 判断字符是否为词字符（用于扩展高亮区间）
func isWordChar(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// ErrorType 表示错误类型
type ErrorType int

const (
	// 语法错误
	ErrorSyntax ErrorType = iota
	// 语义错误
	ErrorSemantic
	// 类型错误
	ErrorTypeError
	// 运行时错误
	ErrorRuntime
	// 警告
	ErrorWarning
)

// Error 表示一个错误
type Error struct {
	Type          ErrorType
	Message       string
	Line          int
	Column        int
	File          string // 真实文件名（如 test.kl / path/to/test.kl）
	Code          string // 错误类别/错误码（如 undefined_variable / if_chain_should_use_match）
	Suggestion    string
	SourceContext string // 源码上下文
	SourceLine    string // 错误所在的源码行
	LineNumberStr string // 行号字符串（用于对齐）
	Highlight     *HighlightSpan // 源码高亮区间（用于终端/IDE 高亮错误）
}

// String 实现error接口
func (e *Error) String() string {
	var errorType string
	switch e.Type {
	case ErrorSyntax:
		errorType = "Syntax Error"
	case ErrorSemantic:
		errorType = "Semantic Error"
	case ErrorTypeError:
		errorType = "Type Error"
	case ErrorRuntime:
		errorType = "Runtime Error"
	case ErrorWarning:
		errorType = "Warning"
	default:
		errorType = "Unknown Error"
	}

	result := fmt.Sprintf("%s at line %d, column %d: %s", errorType, e.Line, e.Column, e.Message)
	if e.File != "" {
		result = fmt.Sprintf("%s in %s", result, e.File)
	}
	if e.Suggestion != "" {
		result = fmt.Sprintf("%s\nSuggestion: %s", result, e.Suggestion)
	}
	if e.SourceLine != "" {
		result = fmt.Sprintf("%s\n%s", result, e.SourceContext)
	}
	return result
}

// ErrorCollector 表示错误收集器
type ErrorCollector struct {
	errors   []*Error
	source   string // 当前编译单元源码（用于给缺少上下文的错误补充源码与高亮）
	fileName string // 当前编译单元文件名（用于错误首行显示）
}

// NewErrorCollector 创建一个新的错误收集器
func NewErrorCollector() *ErrorCollector {
	return &ErrorCollector{
		errors: []*Error{},
	}
}

// SetFile 设置当前编译单元的文件名；之后添加的错误首行会显示该文件名
func (ec *ErrorCollector) SetFile(fileName string) {
	ec.fileName = fileName
}

// GetFile 返回当前编译单元的文件名
func (ec *ErrorCollector) GetFile() string {
	return ec.fileName
}

// SetSource 设置当前编译单元的源码；
// 之后通过 AddError 系列方法添加的、缺少源码上下文的错误会自动补充源码上下文与高亮区间
func (ec *ErrorCollector) SetSource(source string) {
	ec.source = source
}

// AddError 添加一个错误
func (ec *ErrorCollector) AddError(errorType ErrorType, message string, line, column int, code, suggestion string) {
	err := &Error{
		Type:       errorType,
		Message:    message,
		Line:       line,
		Column:     column,
		File:       ec.fileName, // 真实文件名来自 ErrorCollector（main 设置）
		Code:       code,        // 错误码/类别（如 undefined_variable）
		Suggestion: suggestion,
	}
	if err.SourceContext == "" && ec.source != "" {
		context, sourceLine, lineNumStr := ExtractSourceContext(ec.source, line, column)
		err.SourceContext = context
		err.SourceLine = sourceLine
		err.LineNumberStr = lineNumStr
		err.Highlight = BuildHighlight(ec.source, line, column, 0, code, errorType, message)
	}
	ec.errors = append(ec.errors, err)
}

// AddErrorInstance 添加一个错误实例
func (ec *ErrorCollector) AddErrorInstance(error *Error) {
	ec.errors = append(ec.errors, error)
}

// AddSyntaxError 添加一个语法错误
func (ec *ErrorCollector) AddSyntaxError(message string, line, column int, file, suggestion string) {
	ec.AddError(ErrorSyntax, message, line, column, file, suggestion)
}

// AddSemanticError 添加一个语义错误
func (ec *ErrorCollector) AddSemanticError(message string, line, column int, file, suggestion string) {
	ec.AddError(ErrorSemantic, message, line, column, file, suggestion)
}

// AddTypeError 添加一个类型错误
func (ec *ErrorCollector) AddTypeError(message string, line, column int, file, suggestion string) {
	ec.AddError(ErrorTypeError, message, line, column, file, suggestion)
}

// AddRuntimeError 添加一个运行时错误
func (ec *ErrorCollector) AddRuntimeError(message string, line, column int, file, suggestion string) {
	ec.AddError(ErrorRuntime, message, line, column, file, suggestion)
}

// AddWarning 添加一个警告
func (ec *ErrorCollector) AddWarning(message string, line, column int, file, suggestion string) {
	ec.AddError(ErrorWarning, message, line, column, file, suggestion)
}

// AddSemanticWarning 添加一个语义警告
func (ec *ErrorCollector) AddSemanticWarning(message string, line, column int, file, suggestion string) {
	ec.AddError(ErrorWarning, message, line, column, file, suggestion)
}

// GetWarnings 获取所有警告
func (ec *ErrorCollector) GetWarnings() []*Error {
	return ec.GetErrorsByType(ErrorWarning)
}

// HasWarnings 检查是否有警告
func (ec *ErrorCollector) HasWarnings() bool {
	return len(ec.GetWarnings()) > 0
}

// Errors 返回错误列表
func (ec *ErrorCollector) Errors() []*Error {
	return ec.errors
}

// HasErrors 检查是否有错误
func (ec *ErrorCollector) HasErrors() bool {
	return ec.ErrorCount() > 0
}

// ErrorCount 返回错误数量（不含警告）
func (ec *ErrorCollector) ErrorCount() int {
	return len(ec.errors) - ec.WarningCount()
}

// WarningCount 返回警告数量
func (ec *ErrorCollector) WarningCount() int {
	return len(ec.GetErrorsByType(ErrorWarning))
}

// ReportErrors 报告错误（错误与警告分开统计个数）
func (ec *ErrorCollector) ReportErrors() {
	errorCount, warningCount := ec.ErrorCount(), ec.WarningCount()
	if errorCount+warningCount == 0 {
		return
	}

	if errorCount > 0 {
		fmt.Printf("Found %d error(s), %d warning(s):\n", errorCount, warningCount)
	} else {
		fmt.Printf("Found %d warning(s):\n", warningCount)
	}
	for i, err := range ec.errors {
		fmt.Printf("%d. %s\n", i+1, err.String())
	}
}

// GetErrorSummary 获取错误摘要（错误与警告分开统计个数）
func (ec *ErrorCollector) GetErrorSummary() string {
	errorCount, warningCount := ec.ErrorCount(), ec.WarningCount()
	if errorCount+warningCount == 0 {
		return "No errors found"
	}

	summary := fmt.Sprintf("Found %d error(s), %d warning(s):\n", errorCount, warningCount)
	for i, err := range ec.errors {
		summary += fmt.Sprintf("%d. %s\n", i+1, err.String())
	}
	return summary
}

// Clear 清除所有错误
func (ec *ErrorCollector) Clear() {
	ec.errors = []*Error{}
}

// CountByType 按错误类型统计错误数量
func (ec *ErrorCollector) CountByType() map[ErrorType]int {
	counts := make(map[ErrorType]int)
	for _, err := range ec.errors {
		counts[err.Type]++
	}
	return counts
}

// GetErrorTypes 获取所有错误类型
func (ec *ErrorCollector) GetErrorTypes() []ErrorType {
	types := make([]ErrorType, 0)
	typeMap := make(map[ErrorType]bool)
	for _, err := range ec.errors {
		if !typeMap[err.Type] {
			typeMap[err.Type] = true
			types = append(types, err.Type)
		}
	}
	return types
}

// GetErrorsByType 按错误类型获取错误
func (ec *ErrorCollector) GetErrorsByType(errorType ErrorType) []*Error {
	errors := make([]*Error, 0)
	for _, err := range ec.errors {
		if err.Type == errorType {
			errors = append(errors, err)
		}
	}
	return errors
}

// ErrorTypeToString 将错误类型转换为字符串
func ErrorTypeToString(errorType ErrorType) string {
	switch errorType {
	case ErrorSyntax:
		return "Syntax"
	case ErrorSemantic:
		return "Semantic"
	case ErrorTypeError:
		return "Type"
	case ErrorRuntime:
		return "Runtime"
	case ErrorWarning:
		return "Warning"
	default:
		return "Unknown"
	}
}

// FormatErrorPosition 格式化错误位置
func FormatErrorPosition(file string, line, column int) string {
	if file != "" {
		return fmt.Sprintf("%s:%d:%d", file, line, column)
	}
	return fmt.Sprintf("%d:%d", line, column)
}

// GenerateSuggestion 根据错误信息生成建议
func GenerateSuggestion(message string) string {
	suggestions := map[string]string{
		"unterminated string":                     "Make sure to close all string literals with quotes",
		"unexpected token":                        "Check for missing or extra punctuation",
		"function name already exists":            "Choose a different name for the function",
		"prefix name already exists":              "Choose a different name for the prefix",
		"object statement missing type":           "Add a type for the object",
		"object statement missing name":           "Add a name for the object",
		"spend statement missing expression":      "Add an expression to the spend statement",
		"spend statement missing call statements": "Add call statements to the spend block",
		"prefix statement missing name":           "Add a name for the prefix",
	}

	for key, suggestion := range suggestions {
		if strings.Contains(message, key) {
			return suggestion
		}
	}

	return "Check the syntax and try again"
}

// ExtractSourceContext 从源码中提取错误上下文
func ExtractSourceContext(source string, line, column int) (string, string, string) {
	lines := strings.Split(source, "\n")
	if line < 1 || line > len(lines) {
		return "", "", ""
	}

	sourceLine := lines[line-1]
	lineNumberStr := fmt.Sprintf("%d", line)

	startLine := line - 2
	if startLine < 0 {
		startLine = 0
	}
	endLine := line + 1
	if endLine > len(lines) {
		endLine = len(lines)
	}

	context := ""
	for i := startLine; i < endLine; i++ {
		lineNum := fmt.Sprintf("%d", i+1)
		lineNumPadded := fmt.Sprintf("%4s", lineNum)
		if i+1 == line {
			context += fmt.Sprintf("%s > | %s\n", lineNumPadded, lines[i])
			// 指示行紧跟错误行正下方 (gcc/clang 风格: ^~~~ 紧贴错误行)
			columnStr := ""
			for c := 0; c < column-1 && c < len(lines[i]); c++ {
				if lines[i][c] == '	' {
					columnStr += "    "
				} else {
					columnStr += " "
				}
			}
			columnStr += "^"
			context += "       | " + columnStr + "\n"
		} else {
			context += fmt.Sprintf("%s   | %s\n", lineNumPadded, lines[i])
		}
	}

	return context, sourceLine, lineNumberStr
}

// FormatErrorWithContext 格式化带上下文的错误信息
// 第一行: 文件名:行号:列号: 级别: 类别 (如 test.kl:3:13: Semantic Error: undefined_variable)
// 第二行: 消息；错误为红色、警告为黄色高亮；源码中的错误位置会按高亮区间绘制 ^^^ 标记
func FormatErrorWithContext(err *Error) string {
	var result strings.Builder

	var errorType string
	var color string
	switch err.Type {
	case ErrorSyntax:
		errorType, color = "Syntax Error", ansiRed
	case ErrorSemantic:
		errorType, color = "Semantic Error", ansiRed
	case ErrorTypeError:
		errorType, color = "Type Error", ansiRed
	case ErrorRuntime:
		errorType, color = "Runtime Error", ansiRed
	case ErrorWarning:
		errorType, color = "Warning", ansiYellow
	default:
		errorType, color = "Unknown Error", ansiRed
	}

	// 第一行: 文件名:行号:列号: 级别: 类别 (整行按级别着色: 警告黄/错误红)
	if colorEnabled {
		result.WriteString(ansiBold)
		result.WriteString(color)
	}
	if err.File != "" {
		if err.Line > 0 {
			result.WriteString(fmt.Sprintf("%s:%d:%d: ", err.File, err.Line, err.Column))
		} else {
			result.WriteString(fmt.Sprintf("%s: ", err.File))
		}
	} else {
		if err.Line > 0 {
			result.WriteString(fmt.Sprintf("%d:%d: ", err.Line, err.Column))
		}
	}
	result.WriteString(errorType)
	if err.Code != "" {
		result.WriteString(": " + err.Code)
	}
	if colorEnabled {
		result.WriteString(ansiReset)
	}
	result.WriteString("\n")

	// 第二行: 消息
	if colorEnabled {
		result.WriteString(ansiBold)
		result.WriteString(color)
	}
	result.WriteString(err.Message)
	if colorEnabled {
		result.WriteString(ansiReset)
	}
	result.WriteString("\n")

	if err.SourceLine != "" {
		result.WriteString(highlightSourceContext(err.SourceContext, err.Highlight))
		// context 已以 \n 结尾, 不再追加空行
	}

	if err.Suggestion != "" {
		result.WriteString(fmt.Sprintf("  Suggestion: %s", err.Suggestion))
		result.WriteString("\n")
	}

	return result.String()
}

// highlightSourceContext 在源码上下文中着色并扩展错误高亮标记
// 参照 clang/gcc 输出: ^ 起头 + ~ 填充到 token 长度
// 无颜色环境（NO_COLOR/TERM=dumb）下仍会扩展 ^ 标记为完整区间
func highlightSourceContext(context string, hl *HighlightSpan) string {
	if context == "" {
		return ""
	}
	lines := strings.Split(context, "\n")
	hlColor := ansiRed
	hlLen := 1
	if hl != nil {
		if hl.Type == ErrorWarning {
			hlColor = ansiYellow
		}
		if hl.Length > 1 {
			hlLen = hl.Length
		}
	}
	for i := len(lines) - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		// 高亮行形如 "      |      ^"，去掉分隔符与空格后只剩 ^ 才能判定
		stripped := strings.ReplaceAll(trimmed, "|", "")
		stripped = strings.ReplaceAll(stripped, "^", "")
		if strings.TrimSpace(stripped) != "" || !strings.Contains(trimmed, "^") {
			continue
		}
		caretIdx := strings.Index(lines[i], "^")
		if caretIdx < 0 {
			continue
		}
		base := lines[i][:caretIdx]
		// clang 风格: ^ 起头, 后面 ~ 填充到 hlLen (token 长度)
		marker := "^" + strings.Repeat("~", hlLen-1)
		if colorEnabled {
			lines[i] = base + hlColor + marker + ansiReset
		} else {
			lines[i] = base + marker
		}
		break
	}
	return strings.Join(lines, "\n")
}
