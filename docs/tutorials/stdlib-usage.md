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
import std.io
import std.string

fn main() {
    string s = std.string.create("hello")
    i64 n = std.string.length(s)
    std.io.println("len = ", n)   # len = 5
    # Kaula 自动内存管理：无需手动 free，作用域结束自动回收
}
```

#### 方法形式（`s.length()` / `s.substring(a, b)`）

`string` 和 `String` 都可以直接用方法，编译器会改写成对应的 std 函数，
不必写全限定名，也不用手动 `as<size_t>` 转换：

```kaula
import std.io
import std.string

fn main() {
    String s = std.string.create("abcdef")
    std.io.println(s.length())        # 6
    String sub = s.substring(1, 3)
    std.io.println(sub.length())     # 2，链式也能用
}
```

等价关系：

| 方法写法 | 实际生成 |
|---|---|
| `s.length()` | `string_length(s)` |
| `s.substring(a, b)` | `string_substring(s, (size_t)a, (size_t)b)` |

> 注意 `s[a:b]` 这种切片写法**还没有**。数组也没有：`xs.length` 不成立，
> 需要显式把长度作为参数传入。`extract` 同样不能用于字符串——它是 SOR 的
> 容器元素所有权提取原语，对 `String` 会报语义错误。

### std.math — 数学函数

`sin`、`cos`、`sqrt`、`pow`、`abs` 等标准数学函数。

## 模块查找机制

Kaula 的 `import` 语句按以下顺序查找模块：

1. **标准库**：`compiler/stdlib.json` 中注册的 67 个模块（62 个 `std.*` + 5 个 `freestanding.*`）
2. **本地文件**：相对路径的 `.kl` 文件（`pub` 函数可见）
3. **第三方库**：`pkglib/` 目录下的扩展库

## 完整示例

参见 [examples/hello.kl](examples/hello.kl) 和 [examples/variables.kl](examples/variables.kl)。
