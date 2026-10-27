# 标准库使用

Kaula 标准库通过 `import` 语句导入，按模块组织。

## 导入模块

```kaula
import std.io        # 输入输出
import std.string    # 字符串操作
import std.math      # 数学函数
import std.system    # 系统调用
```

简写形式（省略 `std.` 前缀）：

```kaula
import io
import string
```

## 常用模块

### std.io — 输入输出

| 函数 | 说明 |
|------|------|
| `println(...)` | 打印并换行 |
| `print(...)` | 打印不换行 |
| `read_line()` | 读取一行输入 |
| `read_int()` | 读取整数 |
| `read_float()` | 读取浮点数 |

### std.string — 字符串操作

```kaula
import std.string

fn main() {
    string s = string_create("hello")
    char c = string_char_at(s, 0)
    println("First char: ", c)
    string_free(s)
}
```

### std.math — 数学函数

`sin`、`cos`、`sqrt`、`pow`、`abs` 等标准数学函数。

## 模块查找机制

Kaula 的 `import` 语句按以下顺序查找模块：

1. **标准库**：`compiler/stdlib.json` 中注册的 53+ 个模块
2. **本地文件**：相对路径的 `.kl` 文件（`pub` 函数可见）
3. **第三方库**：`pkglib/` 目录下的扩展库

## 完整示例

参见 [examples/hello.kl](examples/hello.kl) 和 [examples/variables.kl](examples/variables.kl)。
