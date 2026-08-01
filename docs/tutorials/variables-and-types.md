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
auto z = 42        # z 类型推导为 int
auto pi = 3.14     # pi 类型推导为 float
```

> 注意：`auto` 的右侧表达式必须能明确推导类型。

**编译期常量使用 `const`：**

```kaula
const max_retries = 3
const app_name = "Kaula"
```

## 多参数输出

`println` 支持同时输出多个值：

```kaula
println("x = ", x)
println("name = ", name, ", score = ", score)
```

## 完整示例

参见 [examples/variables.kl](examples/variables.kl)。
