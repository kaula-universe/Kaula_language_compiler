# 格式化工具

Kaula 编译器内置代码格式化工具，基于 AST 分析，将源码格式化为统一风格。

## 快速开始

```bash
# 格式化文件（输出到 stdout）
kaulac fmt file main.kl

# 格式化并写回原文件
kaulac fmt file main.kl --write

# 检查文件是否已格式化
kaulac fmt check main.kl
```

## 命令一览

| 命令 | 说明 |
|------|------|
| `kaulac fmt file <file.kl> [--write]` | 格式化文件 |
| `kaulac fmt stdin` | 从 stdin 读取并格式化 |
| `kaulac fmt check <file.kl>` | 检查文件是否已格式化 |

## 格式化规则

### 缩进

- 使用 **4 个空格** 缩进
- 块语句（`fn`、`if`、`while`、`for`、`class`、`match` 等）内层增加缩进

### Import 排序

自动将 import 语句分为两组并排序：

1. **`std.*` 导入** — 标准库模块，按字母排序，组内无空行
2. **其他导入** — 用户/项目模块，按字母排序，与 std 之间空一行

```kla
// 格式化前
import "std.io"
import "utils.math"
import "std.os"
import "core.data"

// 格式化后
import "std.io"
import "std.os"

import "core.data"
import "utils.math"
```

### 括号处理

二元表达式根据运算符优先级自动添加括号，保持原始求值顺序：

```kla
// a + b * c 不需要括号
a + b * c

// a * (b + c) 保留括号
a * (b + c)

// a - (b - c) 保留括号（左结合性）
a - (b - c)
```

### 函数格式化

```kla
// 属性注解
#[sor]
export fn add(x: i32, y: i32) i32 {
    return x + y
}

// 泛型
export fn map[T, R](arr: Slice[T], f: fn(T) -> R) Slice[R] {
    // ...
}

// 无函数体
export fn noop() {}
```

### 控制流格式化

```kla
if (condition) {
    // ...
} else if (other) {
    // ...
} else {
    // ...
}

for (i = 0; i < n; i++) {
    // ...
}

for item in collection {
    // ...
}

while (running) {
    // ...
}
```

### 类型定义

```kla
class Point {
    x: f64
    y: f64
}

interface Drawable {
    fn draw(self)
}

struct Config {
    name: String
    version: i32
}

type Number = i32 | f64

enum Color {
    Red
    Green
    Blue
    Custom(i32, i32, i32)
}
```

### Lambda 表达式

```kla
// 单行
let add = fn(x: i32, y: i32) i32 { x + y }

// 多行
let process = fn(data: Slice[i32]) {
    for item in data {
        // ...
    }
}
```

### Match 表达式

```kla
match(value) {
    0 => "zero"
    1 => "one"
    _ => "other"
}

match(result) {
    Ok(val) => std.io.println(val)
    Err(e) => std.io.println(e)
}
```

### 结构体字面量

```kla
// 点前缀字段名
let p = Point{ .x = 1.0, .y = 2.0 }

// 对象字面量
let obj = object{ name: "test", version: 1 }
```

## 一次处理多个文件

`kaulac fmt file` 支持一次传入多个文件：

```bash
kaulac fmt file main.kl utils.kl --write
```

也可配合 shell 循环处理目录下全部 `.kl`：

```bash
for f in src/*.kl; do kaulac fmt file "$f" --write; done
```

## 注意事项

1. **格式化是确定性的**：相同输入总是产生相同输出
2. **不改变语义**：格式化仅调整空白和括号，不改变代码含义
3. **解析错误**：如果文件有语法错误，格式化会失败
4. **import 排序**：只对文件开头连续的 import 块进行排序

## 限制

- 不支持自定义缩进风格（固定 4 空格）
- **注释在格式化过程中丢失**（AST 不保留注释）。`//`、`#`、`/* */` 三种形式
  都可被词法器正确跳过，但不会出现在格式化输出里
- 手动换行不保留
- 注解的自定义格式不支持

### 失败即退出，不做静默降级

格式化器遵循「宁可报错，也不输出可能丢语义的代码」：

| 情形 | 行为 |
|---|---|
| 源码存在语法错误（含词法错误） | 报错退出，`file` / `stdin` / `check` 三条路径一致；**不输出、不落盘** |
| AST 含formatter 尚未覆盖的节点类型 | 报错退出并列出类型名，提示提交 Issue；**不输出、不落盘** |
| import 目标在本机找不到 | **不视为错误**。该检查依赖运行环境，与语法无关，格式化时跳过 |

因此 `/* unknown expression: %T */` 这类占位**不会出现在输出里**——
一旦出现就说明护栏失效，属于缺陷。当前全仓 114 个 `.kl` 文件格式化
114/114 成功，未覆盖节点数为 0。

>注意：格式化会丢弃注释，但**解析错误不会被"修好"**——
> 括号不闭合、非法字符这类输入会被直接拒绝，而不是给出一份猜测性的修补结果。

## 构建

格式化功能已内置于 `kaulac`：

```bash
python toolkit_build.py                    # 随工具链整体构建
cd compiler && go build -o kaulac ./cmd/kaulac   # 单独构建
```

## 实现位置

| 组件 | 路径 |
|------|------|
| CLI 子命令分发 | `compiler/cmd/kaulac/main.go`（`fmt` 子命令：`file` / `stdin` / `check`） |
| 格式化器 | `compiler/internal/formatter/formatter.go` |

## 相关文档

- 工具索引：[README.md](README.md)
- 编译器命令参考：[kaulac.md](kaulac.md)
- 代码规范：[../../codeStyle.md](../../codeStyle.md)
