# 词法分析器 (Lexer)

词法分析器将源代码字符流转换为 Token 序列。实现位于 `internal/lexer/lexer.go`。

## 设计

- **状态机实现**：逐字符扫描，根据当前字符决定扫描策略
- **UTF-8 支持**：处理 ASCII 和非 ASCII 字符（Unicode 字母、数字、空格）
- **错误恢复**：遇到非法字符时记录错误并继续扫描，不会中断

## Token 类型

### 关键字

| Token | 语法 | 说明 |
|-------|------|------|
| `TOKEN_SPEND` | `spend` | 消费语句 |
| `TOKEN_CALL` | `call` | 调用语句 |
| `TOKEN_TASK` | `task` | 任务语句 |
| `TOKEN_ASYNC` | `async` | 异步语句 |
| `TOKEN_PREFIX` | `prefix` | 前缀系统 |
| `TOKEN_TREE` | `tree` | 树系统 |
| `TOKEN_OBJECT` | `object` | 对象声明 |
| `TOKEN_FUNC` | `fn` | 函数声明 |
| `TOKEN_IF` | `if` | 条件语句 |
| `TOKEN_ELSE` | `else` | 否则分支 |
| `TOKEN_WHILE` | `while` | 循环语句 |
| `TOKEN_FOR` | `for` | for 循环 |
| `TOKEN_SWITCH` | `switch` | switch 语句 |
| `TOKEN_CASE` | `case` | case 分支 |
| `TOKEN_DEFAULT` | `default` | 默认分支 |
| `TOKEN_RETURN` | `return` | 返回语句 |
| `TOKEN_IMPORT` | `import` | 导入语句 |
| `TOKEN_EXPORT` | `export` | 导出语句 |
| `TOKEN_PACKAGE` | `package` | 包声明 |
| `TOKEN_PUB` | `pub` | 公开修饰符 |
| `TOKEN_NONLOCAL` | `nonlocal` | 非局部变量 |
| `TOKEN_BREAK` | `break` | 中断循环 |
| `TOKEN_CONTINUE` | `continue` | 继续循环 |
| `TOKEN_CLASS` | `class` | 类定义 |
| `TOKEN_LITERAL_INTERFACE` | `interface` | 接口定义 |
| `TOKEN_IMPLEMENTS` | `implements` | 实现子句 |
| `TOKEN_CONSTRUCTOR` | `constructor` | 构造函数 |
| `TOKEN_STRUCT` | `struct` | 结构体定义 |
| `TOKEN_AUTO` | `auto` | 类型推导 |
| `TOKEN_YIELD` | `yield` | 所有权转移 |
| `TOKEN_RELEASE` | `release` | 释放所有权 |
| `TOKEN_EXTRACT` | `extract` | 提取所有权 |
| `TOKEN_TYPE` | `type` | 类型别名 |

### 类型关键字

| Token | 语法 |
|-------|------|
| `TOKEN_TYPE_INT` | `int` |
| `TOKEN_TYPE_FLOAT` | `float` |
| `TOKEN_TYPE_DOUBLE` | `double` |
| `TOKEN_TYPE_BOOL` | `bool` |
| `TOKEN_TYPE_CHAR` | `char` |
| `TOKEN_TYPE_STRING` | `string` |
| `TOKEN_TYPE_VOID` | `void` |

### 字面量

| Token | 语法 | 示例 |
|-------|------|------|
| `TOKEN_LITERAL_INT` | 整数 | `42`, `0xFF`, `0o77`, `0b1010` |
| `TOKEN_LITERAL_FLOAT` | 浮点数 | `3.14`, `2.0` |
| `TOKEN_LITERAL_CHAR` | 字符 | `'a'`, `'\n'`, `'\\'` |
| `TOKEN_STRING` | 字符串 | `"hello world"` |
| `TOKEN_TRUE` | 布尔 | `true` |
| `TOKEN_FALSE` | 布尔 | `false` |
| `TOKEN_NULL` | 空值 | `null` |

### 运算符

| Token | 语法 | 说明 |
|-------|------|------|
| `TOKEN_PLUS` | `+` | 加法 |
| `TOKEN_MINUS` | `-` | 减法 |
| `TOKEN_MULTIPLY` | `*` | 乘法 |
| `TOKEN_DIVIDE` | `/` | 除法 |
| `TOKEN_MOD` | `%` | 取模 |
| `TOKEN_ASSIGN` | `=` | 赋值 |
| `TOKEN_EQ` | `==` | 等于 |
| `TOKEN_NE` | `!=` | 不等于 |
| `TOKEN_LT` | `<` | 小于 |
| `TOKEN_GT` | `>` | 大于 |
| `TOKEN_LE` | `<=` | 小于等于 |
| `TOKEN_GE` | `>=` | 大于等于 |
| `TOKEN_AND` | `&&` | 逻辑与 |
| `TOKEN_OR` | `\|\|` | 逻辑或 |
| `TOKEN_AMPERSAND` | `&` | 按位与/取地址 |
| `TOKEN_XOR` | `^` | 按位异或 |
| `TOKEN_LSHIFT` | `<<` | 左移 |
| `TOKEN_RSHIFT` | `>>` | 右移 |
| `TOKEN_PREFIX_REF` | `$` | 前缀引用 |
| `TOKEN_AT` | `@` | 前缀调用 |
| `TOKEN_QUESTION` | `?` | 可空标记 |

### 分隔符

| Token | 语法 |
|-------|------|
| `TOKEN_LPAREN` | `(` |
| `TOKEN_RPAREN` | `)` |
| `TOKEN_LBRACE` | `{` |
| `TOKEN_RBRACE` | `}` |
| `TOKEN_LBRACKET` | `[` |
| `TOKEN_RBRACKET` | `]` |
| `TOKEN_SEMICOLON` | `;` |
| `TOKEN_COMMA` | `,` |
| `TOKEN_COLON` | `:` |
| `TOKEN_DOUBLE_COLON` | `::` |
| `TOKEN_DOT` | `.` |

### 特殊

| Token | 语法 | 说明 |
|-------|------|------|
| `TOKEN_ATTRIBUTE` | `#[...]` | 注解标记 |
| `TOKEN_COMMENT` | `#` 或 `//` | 注释 |
| `TOKEN_EOF` | - | 文件结束 |

## 扫描策略

### 数字扫描

支持多种进制前缀：

- `0x` / `0X`：十六进制（`0xFF`）
- `0o` / `0O`：八进制（`0o77`）
- `0b` / `0B`：二进制（`0b1010`）
- 无前缀：十进制整数或浮点数

### 字符串扫描

- 双引号 `"..."` 包围
- 支持反斜杠转义（但保持原始内容，由代码生成器处理）
- 未闭合字符串报告错误

### 字符字面量

- 单引号 `'...'` 包围
- 支持转义字符：`\n`, `\r`, `\t`, `\\`, `\'`, `\"`

### 注解扫描

- `#[...]` 格式，收集内容直到 `]` 或换行
- 用于函数注解（`#[prefix]`, `#[tree]`, `#[no_kmm]`, `#[inline]`, `#[sor]`）

### 注释

- `#`：行注释
- `//`：行注释
- 两者等效，扫描到行尾结束

## 核心类型

```go
// Token 表示一个词法单元
type Token struct {
    Type   TokenType  // Token 类型
    Value  string     // Token 值
    Line   int        // 所在行号（1-based）
    Column int        // 所在列号（1-based）
}

// Lexer 词法分析器
type Lexer struct {
    input          string              // 源代码
    pos            int                 // 当前位置
    line           int                 // 当前行号
    column         int                 // 当前列号
    inputLen       int                 // 输入长度（缓存）
    errorCollector *errors.ErrorCollector // 错误收集器
    file           string              // 文件名
    source         string              // 完整源码（用于错误上下文）
}
```

## API

```go
// 创建词法分析器
lexer := NewLexer(sourceCode)
lexer.SetFile("main.kl")

// 逐个获取 Token
for {
    token := lexer.Next()
    if token.Type == TOKEN_EOF {
        break
    }
    // 处理 token...
}

// 错误处理
if lexer.HasErrors() {
    lexer.ReportErrors()
}
```
