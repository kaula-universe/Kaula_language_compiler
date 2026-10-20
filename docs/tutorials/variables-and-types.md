# 变量与类型

Kaula 提供了丰富的内置类型系统和灵活的变量声明方式。

## 基本类型

| Kaula 类型 | C 类型 | 说明 |
|-----------|--------|------|
| `int` | `int64_t` | 默认整数类型 (64位) |
| `i8` / `i16` / `i32` / `i64` | `int8_t` ~ `int64_t` | 定长有符号整数 |
| `u8` / `u16` / `u32` / `u64` | `uint8_t` ~ `uint64_t` | 定长无符号整数 |
| `cint` | `int` | C 语言原生 `int`，**仅用于 FFI 签名**（见下文） |
| `float` / `f32` | `float` | 单精度浮点 |
| `double` / `f64` | `double` | 双精度浮点 |
| `bool` | `int` | 布尔值 (`true` / `false`) |
| `char` | `char` | 字符 |
| `string` | `String`（结构体，含 `len` + `ptr`） | Kaula 字符串 |
| `cstring` | `const char*` | C 风格零结尾字符串 |
| `void` | `void` | 空类型 |

## FFI 专用：`void()` 记法与透明指针

Kaula **禁止裸 `void*`**。所有 FFI 边界上的类型擦除指针统一使用 `void(T...)R` 记法：

| Kaula 记法 | C 类型 | 说明 |
|-----------|--------|------|
| `void()` | `void*` | 透明数据指针 |
| `const void()` | `const void*` | 只读透明数据指针 |
| `*void()` | `void**` | 指向透明指针的指针 |
| `void(void())void` | `void (*)(void*)` | 回调函数指针（参数含透明指针） |
| `void(const void(), const void())cint` | `int (*)(const void*, const void*)` | 比较函数（见 FFI 文档） |

详细规则和示例见 [extern-and-ffi.md](extern-and-ffi.md)。

## FFI 专用：`cint` 类型

Kaula 的 `int` 固定 64 位，而 C 的 `int` 通常 32 位。调用约定（ABI）不同，**凡涉及 C 函数返回 `int` 或接受 C `int` 参数的 FFI 声明，必须使用 `cint`**：

```kaula
extern fn printf(fmt: cstring, ...) -> cint  # printf 返回 C 的 int
extern fn qsort(..., void(const void(), const void())cint compar) void
```

> 注意：普通业务代码使用 `i32` 表示 32 位整数；`cint` 仅用于与 C 原生 `int` ABI 对齐的场景。

## 变量声明

**显式类型声明：**

```kaula
int x = 42
float y = 3.14
string name = "Kaula"
bool flag = true
```

**类型推导使用 `auto`：**

```kaula
auto z = 42        # z 类型推导为 int（= int64_t）
auto n = -1        # n 类型推导为 int，负数同样是 int
auto pi = 3.14     # pi 类型推导为 f64
auto s = "Kaula"   # s 类型推导为 string
```

> 注意：`auto` 的右侧表达式必须能明确推导类型。

整数字面量**一律**推导为 `int`（64 位有符号），不会按数值大小收窄：

```kaula
auto acc = 0
acc = acc + 1000   # 1000，而不是 232
```

需要 8/16/32 位整型时显式标注类型（`u8 x = 0`），不要指望 `auto` 帮你挑宽度。
窄类型累加会回绕，这类错误在 `auto` 下应该是**不可能发生**的。

**编译期常量使用 `const`：**

```kaula
const max_retries = 3
const app_name = "Kaula"
```

**单条语句内声明多个变量（C 风格逗号分隔）：**

```kaula
i64 a = 1, b = 2      # a、b 都是 i64
i64 c, d              # 先声明，稍后赋值
auto p = 1, q = 2.5   # 各自独立推导类型
const max = 1, retries = 3
i64 buf[4], tail      # 首个声明符可用后缀修饰
```

> 说明：类型写在语句开头，是整条语句的**共享类型**，后续声明符只写名字与初值。
> 声明符按从左到右依次生效，后面的初值可以引用前面的变量（`i64 a = 1, b = a`）。
> 这种写法与拆成多条声明语句完全等价，`kaulac fmt` 会原样保留单条语句的形式。

### nonlocal：绑定外层函数作用域的变量

在嵌套函数内部，可以用 `nonlocal` 显式声明绑定**外层函数**中的同名变量，
读写均作用于外层变量（自动按引用捕获，详见 [functions.md](functions.md) 的
「嵌套函数与闭包捕获」）：

```kaula
fn outer() {
    int total = 0
    fn add(int n) {
        nonlocal int total
        total = total + n
    }
    add(5)
    add(7)
    println("total = ", total)   # total = 12
}
```

校验规则：外层函数必须已声明该变量，且类型一致；用于全局变量或本作用域
已声明同名变量都会产生编译错误。

## 多参数输出

`println` 支持同时输出多个值：

```kaula
println("x = ", x)
println("name = ", name, ", score = ", score)
```

## 类型转换：`as<T>()`

Kaula 的整数类型系统严格，不同整数类型之间不会隐式转换。使用 `as<T>(expr)` 进行显式类型转换：

```kaula
int x = 42
u64 y = as<u64>(x)      # int -> u64
i32 z = as<i32>(x)       # int -> i32
f64 f = as<f64>(x)       # int -> f64

u8 b = 255
int big = as<int>(b)     # u8 -> int
```

支持的转换方向包括：各宽度整数之间（i8/i16/i32/i64/u8/u16/u32/u64/int）、整数与浮点（f32/f64）之间。转换在编译期生成对应的 C 强制类型转换，零运行时开销。

> 注意：Kaula 不支持隐式整数提升，`int + u8`、`int < size_t` 等操作必须先通过 `as<T>()` 统一类型。

## 完整示例

参见 [examples/variables.kl](examples/variables.kl)。
