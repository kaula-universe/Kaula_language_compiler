package errors

import (
	"fmt"
	"strings"
)

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
	Type     ErrorType
	Message  string
	Line     int
	Column   int
	File     string
	Suggestion string
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
	return result
}

// ErrorCollector 表示错误收集器
type ErrorCollector struct {
	errors []*Error
}

// NewErrorCollector 创建一个新的错误收集器
func NewErrorCollector() *ErrorCollector {
	return &ErrorCollector{
		errors: []*Error{},
	}
}

// AddError 添加一个错误
func (ec *ErrorCollector) AddError(errorType ErrorType, message string, line, column int, file, suggestion string) {
	error := &Error{
		Type:       errorType,
		Message:    message,
		Line:       line,
		Column:     column,
		File:       file,
		Suggestion: suggestion,
	}
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
	return len(ec.errors) > 0
}

// ReportErrors 报告错误
func (ec *ErrorCollector) ReportErrors() {
	if len(ec.errors) == 0 {
		return
	}

	fmt.Printf("Found %d error(s):\n", len(ec.errors))
	for i, err := range ec.errors {
		fmt.Printf("%d. %s\n", i+1, err.String())
	}
}

// GetErrorSummary 获取错误摘要
func (ec *ErrorCollector) GetErrorSummary() string {
	if len(ec.errors) == 0 {
		return "No errors found"
	}

	summary := fmt.Sprintf("Found %d error(s):\n", len(ec.errors))
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
		"unterminated string": "Make sure to close all string literals with quotes",
		"unexpected token": "Check for missing or extra punctuation",
		"function name already exists": "Choose a different name for the function",
		"prefix name already exists": "Choose a different name for the prefix",
		"object statement missing type": "Add a type for the object",
		"object statement missing name": "Add a name for the object",
		"spend statement missing expression": "Add an expression to the spend statement",
		"spend statement missing call statements": "Add call statements to the spend block",
		"prefix statement missing name": "Add a name for the prefix",
	}

	for key, suggestion := range suggestions {
		if strings.Contains(message, key) {
			return suggestion
		}
	}

	return "Check the syntax and try again"
}
