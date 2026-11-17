# 错误处理

编译器提供结构化的错误处理系统，支持错误收集、格式化显示和修复建议。实现位于 `internal/errors/errors.go`。

## 设计

- **错误收集器**：累积所有编译错误，统一报告
- **源码上下文**：显示错误位置周围的源代码
- **修复建议**：为常见错误提供修复建议
- **多类型支持**：语法错误、语义错误、类型错误、运行时错误、警告

## 错误类型

```go
type ErrorType int

const (
    ErrorSyntax    ErrorType = iota // 语法错误
    ErrorSemantic                    // 语义错误
    ErrorTypeError                   // 类型错误
    ErrorRuntime                     // 运行时错误
    ErrorWarning                     // 警告
)
```

## 错误结构

```go
type Error struct {
    Type          ErrorType  // 错误类型
    Message       string     // 错误消息
    Line          int        // 行号
    Column        int        // 列号
    File          string     // 文件名
    Suggestion    string     // 修复建议
    SourceContext string     // 源码上下文
    SourceLine    string     // 错误行内容
    LineNumberStr string     // 行号字符串
}
```

## 错误收集器

```go
type ErrorCollector struct {
    errors   []*Error  // 错误列表
    warnings []*Error  // 警告列表
}

// 创建错误收集器
ec := errors.NewErrorCollector()

// 添加不同类型的错误
ec.AddSyntaxError(message, line, column, file)
ec.AddSemanticError(message, line, column, file)
ec.AddTypeError(message, line, column, file)
ec.AddRuntimeError(message, line, column, file)
ec.AddWarning(message, line, column, file)
ec.AddSemanticWarning(message, line, column, file)

// 检查是否有错误
if ec.HasErrors() {
    ec.ReportErrors()
}
```

## 源码上下文

错误显示包含源码上下文：

```
=== Compilation Errors ===

[Lexing & Parsing Errors] (2 errors)
  1. Syntax Error at line 7, column 34: unexpected token: PLUS
     Suggestion: Check for missing or extra punctuation
     
     5: fn main() {
     6:     int x = 10
  >  7:     int y = x +     // 错误行
                      ^     // 错误位置
     8:     println(y)
     
  2. Syntax Error at line 7, column 35: unexpected token: RPAREN
     ...
```

### 上下文提取

```go
func ExtractSourceContext(source string, line, column int) (context, sourceLine, lineNumStr string) {
    // 提取错误位置周围的源代码
    // 显示最多 3 行上下文
    // 标记错误位置
}
```

## 修复建议

```go
func GenerateSuggestion(message string) string {
    // 根据错误消息生成修复建议
    switch {
    case strings.Contains(message, "unexpected token"):
        return "Check for missing or extra punctuation"
    case strings.Contains(message, "undefined variable"):
        return "Declare variable before use or check scope"
    case strings.Contains(message, "type mismatch"):
        return "Check variable types and conversions"
    // ...
    }
}
```

## 错误格式化

### 格式化输出

```go
func FormatErrorWithContext(err *Error) string {
    // 格式化错误输出
    // 包含：类型、消息、位置、建议、源码上下文
}
```

### 输出示例

```
=== Compilation Errors ===

[Lexing & Parsing Errors] (1 errors)
  1. Syntax Error at line 7, column 34: unexpected token: PLUS
     Suggestion: Check for missing or extra punctuation
     
     5: fn main() {
     6:     int x = 10
  >  7:     int y = x +
                      ^
     8:     println(y)

[Semantic Analysis Errors] (1 errors)
  1. Type Error at line 12, column 5: undefined variable 'z'
     Suggestion: Declare variable before use or check scope
     
     10: fn calculate() {
     11:     int a = 5
  >  12:     int b = a + z
                 ^        ^
     13:     return b

Total: 2 error(s)
```

## 错误统计

```go
// 获取错误数量
errorCount := len(ec.errors)
warningCount := len(ec.warnings)

// 检查是否有错误
if ec.HasErrors() {
    // 有错误，停止编译
}

// 获取所有错误
allErrors := ec.GetErrors()
allWarnings := ec.GetWarnings()
```

## 错误恢复

编译器在遇到错误时尝试恢复：

1. **词法分析**：跳过非法字符，继续扫描
2. **语法分析**：跳过当前 Token，尝试下一个
3. **语义分析**：记录错误，继续分析其他部分
4. **代码生成**：跳过错误节点，生成部分代码

## 错误示例

### 语法错误

```
Syntax Error at line 7, column 34: unexpected token: PLUS
Suggestion: Check for missing or extra punctuation
```

### 语义错误

```
Semantic Error at line 12, column 5: undefined variable 'x'
Suggestion: Declare variable before use or check scope
```

### 类型错误

```
Type Error at line 15, column 10: cannot convert 'string' to 'int'
Suggestion: Use type conversion or check variable types
```

### 运行时错误

```
Runtime Error at line 20, column 1: null pointer dereference
Suggestion: Check for null before dereferencing
```

### 警告

```
Warning at line 25, column 5: unused variable 'temp'
Suggestion: Remove unused variable or use it
```

## 最佳实践

1. **收集所有错误**：不要在第一个错误时停止
2. **提供上下文**：显示错误位置周围的源代码
3. **给出建议**：帮助用户理解如何修复错误
4. **错误恢复**：尽量继续编译，报告所有错误
5. **清晰格式**：使用一致的错误格式
