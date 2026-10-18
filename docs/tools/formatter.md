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

## 独立工具

旧版独立格式化工具 `kaulafmt` 仍然可用，但推荐使用 `kaulac fmt`：

```bash
# 旧版
kaulafmt main.kl          # 输出到 stdout
kaulafmt -w main.kl       # 写回文件

# 新版（推荐）
kaulac fmt file main.kl
kaulac fmt file main.kl --write
```

## 注意事项

1. **格式化是确定性的**：相同输入总是产生相同输出
2. **不改变语义**：格式化仅调整空白和括号，不改变代码含义
3. **解析错误**：如果文件有语法错误，格式化会失败
4. **import 排序**：只对文件开头连续的 import 块进行排序
