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

## 前端严格化错误码

前端（sema）在 codegen 之前拦截的自有错误码。任何前端错误都会触发
fail-fast gate：打印诊断后直接退出，codegen 与 clang 不再运行。
方案细节见 [frontend-strictness-plan.md](frontend-strictness-plan.md)。

### 错误（阻止编译）

| 错误码 | 触发场景 |
|---|---|
| `wrong_arg_count_local` | 本地函数实参数量 ≠ 形参数量 |
| `arg_type_mismatch` | 本地函数实参类型不兼容形参（整数/浮点家族互通，其余报错） |
| `return_type_mismatch` | return 表达式类型 ≠ 函数声明返回类型 |
| `return_value_in_void_fn` | void 函数 return 带表达式 |
| `no_such_field` | 访问 struct/class 不存在的字段（带相似字段提示） |
| `assign_type_mismatch` | 赋值左右类型不兼容 |
| `redeclaration` | 同一可见作用域重复声明同名变量（见下方说明） |
| `calling_non_callable` | 调用非函数类型的标识符 |
| `missing_return` | 非 void 函数存在不经过 return 的执行路径 |

> **redeclaration 的判定**：Kaula 的 if/while/for 体不创建新符号表作用域，
> 编译器用块路径跟踪区分两种情况——上一次声明在当前块或祖先块 → **错误**；
> 上一次声明在兄弟控制流块（如前一个循环体）→ 仅警告（正向回归用例
> `test/redecl_sibling_blocks.kl`）。
>
> **missing_return 的豁免**：`#[noreturn]`、`#[test]` 函数豁免。路径分析
> 保守识别：顶层 return、if/else 双分支均返回、match 全分支返回且有 `_`
> 兜底、`while true` 无 break、调用 `#[noreturn]` 函数；识别不了的形态
> 按"可能不返回"处理（宁可多报，不放过未定义返回值）。

### 警告（不阻止编译）

| 警告码 | 触发场景 |
|---|---|
| `redeclaration` | 兄弟控制流块之间复用变量名（合法但可读性差） |
| `ifchain_match` | if-else if 链连续对同一变量做 == 比较，建议改用 match |
| `uninitialized` | 变量声明未初始化 |
| `unreachable_code` | return 之后的不可达语句 |
| `empty_body` | if/while 空语句体 |
| `unused_but_set` / `unused_value` / `literal_range` 等 | 未使用/未读取、纯值语句、字面量超范围 |

### 负向回归用例

`test/errors/p*.kl`（9 个用例，每个断言精确错误码，均在前端拦截）；
兄弟块兼容路径的正向回归为 `test/redecl_sibling_blocks.kl`。

## 最佳实践

1. **收集所有错误**：不要在第一个错误时停止
2. **提供上下文**：显示错误位置周围的源代码
3. **给出建议**：帮助用户理解如何修复错误
4. **错误恢复**：尽量继续编译，报告所有错误
5. **清晰格式**：使用一致的错误格式
