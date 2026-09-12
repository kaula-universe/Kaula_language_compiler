# Extern 声明与 FFI

Kaula 通过 `extern` 直接与 C 代码和底层符号互操作，无需绑定代码。

> **实现状态速览**（kaulac 2026-09-12 逐项实测）：
> - ✅ 已可用：`extern 变量: 类型` 声明、`extern fn` 声明语法（参数 `名: 类型`）、透明指针 `void()`、指针类型 `*u8` / `i32*` 两种书写、`type X = adopt <C类型>`、表达式包装 `adopt(expr)`、显式转换 `as<T>(expr)`、`sizeof(T)`
> - ❌ 未实现/有缺陷：extern 函数的**调用**（codegen 对 extern 调用误加 `kaula_` 前缀，与声明符号不一致，任何 extern 调用都无法编译）、变参 `(...)` 语法、回调签名式记法（extern 参数位置无法解析）、`cast<T>()` / `transmute<T>()`（编译器中不存在）、`cstring()` 内置（生成非法 C 类型名）、手动 `free` extern 分配的 libc 内存（与结构化自动回收重复释放，double-free 崩溃）
>
> 下文代码块展示设计语法；所有 ❌ 项均在使用处再次标注，未验证可运行的示例请勿直接照抄。

## 外部变量

声明链接器符号或 C 全局变量：

```kaula
extern bss_start: u8
extern stack_top: u64
extern _estack: u64
```

（实测可用 ✅。）

## 外部函数

声明 C 函数供 Kaula 调用（FFI）。

为了保持类型安全，**禁止裸 `void*`**，透明指针使用 `void()` 记法；回调的 `void(T...)R` 签名式记法为设计目标（当前解析器不支持，见下文标注）。

以下声明语法可正常解析（注意 `Size` 需先按"套用类型"一节定义；变参 `...` 行当前无法解析）：

```kaula
type Size = adopt size_t

// ❌ 变参语法未实现，暂不能写：
// extern fn printf(fmt: *u8, ...) -> i32
extern fn memset(dst: *u8, c: i32, n: Size) -> *u8
extern fn malloc(size: Size) -> void()
extern fn free(ptr: void()) void
```

调用方式与普通 Kaula 函数相同（**当前不可用**，存在以下实测缺陷）：

```kaula
fn main() {
    auto msg = cstring("Hello from C!")   // ❌ cstring 内置未实现
    printf(msg)                            // ❌ 变参未实现

    void() mem = malloc(64)
    // ...
    free(mem)                              // ❌ 手动 free 与结构化自动回收重复 → double-free
}
```

> ⚠️ 当前所有 `extern fn` 调用都无法编译：codegen 生成调用时误加用户函数前缀（如声明 `abs` 却调用 `kaula_abs`），与声明的 C 符号不一致。修复前，FFI 出向调用请改用 pkglib 库桥接路径。

## `void(T...)R` 记法：禁用裸 `void*`

Kaula 不允许出现裸 `void*`，所有 FFI 边界上的类型擦除指针必须使用 `void(T...)R` 记法明确表达含义：

| Kaula 记法 | C 等价类型 | 含义 |
|-----------|-----------|------|
| `void()` | `void*` | 透明数据指针（指向任意类型的数据） |
| `const void()` | `const void*` | 只读透明数据指针 |
| `*void()` | `void**` | 指向透明指针的指针 |
| `void(void())void` | `void (*)(void*)` | 接受透明指针参数的回调函数 |
| `void(void())void()` | `void* (*)(void*)` | 接受并返回透明指针的回调函数 |
| `void(const void(), const void())CInt` | `int (*)(const void*, const void*)` | 比较函数（`CInt` = 套用 C 的 `int`） |
| `void(void(), Size)void` | `void (*)(void*, size_t)` | 接受指针+大小的回调函数 |
| `void(const GraphNode*, i64, void())void` | `void (*)(const GraphNode*, int64_t, void*)` | 混合普通类型与透明指针的回调 |

> ⚠️ 实测（2026-09-12）：表中透明指针记法（`void()` / `const void()`）可用 ✅；**回调函数指针的签名式记法当前未实现**——作为 `extern fn` 参数无法解析，作为普通函数参数可通过解析但代码生成失败。以下行为设计目标语法。

### 快速记忆法

结构为 `返回值类型(参数类型...)` 但反写为 `void(参数类型...)返回类型`：

```
C:    int  (*)(const void*, const void*)
       │       ╰─── 参数列表 ──────╯
       ╰── 返回
Kaula: void(const void(), const void())CInt
       ╰固定前缀╯  ╰── 参数列表 ──────╯  ╰返回
```

前缀固定写 `void`（表示这是一个"签名式记法"），真正的返回值写在末尾括号之后。

### 为什么禁裸强转 / 裸 `void*`

1. **可读性**：看一眼 `void(void(), Size)void` 就知道这是回调；`void*` 无法区分是数据指针、函数指针还是其他。
2. **类型安全**：显式转换使用编译器内置 `as<T>(expr)`（实测可用：数值宽度转换如 `as<i64>(size_value)`，指针具化如 `i32* p = as<i32*>(raw)`；生成对应的 C 强制转换）。裸 C 风格强转（如 `(int)ptr`）无法追踪。注意：`cast<T>()` / `transmute<T>()` 在编译器中**不存在**（本文档旧版误载，2026-09 修正；调用会得到"未定义的变量: 'cast'"报错）。
3. **跨平台**：`CInt` 明确表示 C 的 `int`（32 位），与 Kaula 默认的 `int`（`int64_t`）区分开，避免 ABI 不匹配。

## 套用类型（adopt）：C 兼容类型的统一入口

Kaula 不再内置 C 兼容裸类型（如 `cint`、`size`、`cstring`）。所有与 C ABI 交互的平台相关类型通过 `adopt` 声明为套用类型：

```kaula
type Size = adopt size_t       // C 的 size_t，平台相关宽度
type CInt = adopt int          // C 的 int，32 位
type CStr = adopt const char*  // C 字符串
```

套用类型的核心特性：
- **零成本**：C 侧即 `typedef size_t Size;`，无包装、无转换、无间接层
- **类型安全**：`Size` 与 `int`、`CInt` 互不隐式转换，即使底层宽度相同
- **adopt 右侧使用原始 C 类型语法**，不经过 Kaula 类型映射，以保留 C 原生语义

Kaula 的基本类型 `int` 固定为 64 位 (`int64_t`)。而 C 的 `int` 在大多数平台上是 32 位。**FFI 签名中凡涉及 C 的 `int` 返回/参数必须写成 `CInt`**（套用类型），编译器会将其映射为 C 的原生 `int`。

| Kaula | C | 说明 |
|-------|---|------|
| `int` | `int64_t` | Kaula 默认整数 |
| `i32` | `int32_t` | 固定 32 位，ABI 可能与 C `int` 不同 |
| `CInt` | `int` | 套用 C 语言原生 int，跨平台 FFI 必须用它 |

## 比较函数与 `qsort` 示例

C 标准库 `qsort` 的签名是：

```c
void qsort(void *base, size_t nmemb, size_t size,
           int (*compar)(const void *, const void *));
```

在 Kaula 中写成（⚠️ 端到端当前不可运行，三处缺陷见文首状态速览：回调签名记法无法解析、extern 调用符号名 mangling、手动 `free` 与自动回收重复释放。示例中 `as<T>(expr)`、`sizeof(T)`、`adopt(expr)`、`i32*` 声明等语法均已单独实测正确）：

```kaula
type Size = adopt size_t
type CInt = adopt int

// ❌ compar 的签名式参数记法当前无法解析
extern fn qsort(base: void(), nmemb: Size, size: Size,
                compar: void(const void(), const void())CInt) void

fn cmp_int(a: const void(), b: const void()) -> CInt {
    i32* x = as<i32*>(a)
    i32* y = as<i32*>(b)
    if x[0] < y[0] { return adopt(-1) }
    if x[0] > y[0] { return adopt(1) }
    return adopt(0)
}

fn main() {
    i32* arr = as<i32*>(malloc(adopt(sizeof(i32) * 5)))
    arr[0] = 3
    arr[1] = 1
    arr[2] = 4
    arr[3] = 1
    arr[4] = 5
    qsort(arr, adopt(5), adopt(sizeof(i32)), cmp_int)
    println("sorted: ", arr[0], arr[1], arr[2], arr[3], arr[4])
    free(arr)
}
```

对比旧版文档示例中的错误语法（均已在 2026-09 修正）：`cast<*i32>(...)` → `as<i32*>(...)`；`sizeof<i32>` → `sizeof(i32)`；`*i32 arr = ...` → `i32* arr = ...`；`*(i32*)a` 裸强转 → `as<i32*>(a)`；extern 参数一律 `名: 类型`。

## 被 C 调用

Kaula 函数默认生成标准 C ABI，可直接被 C 代码调用：

```kaula
fn add(int a, int b) int {
    return a + b
}
```

编译后生成的 `add` 函数符合 C ABI，C 代码中 `extern int64_t add(int64_t, int64_t);` 即可调用。

## 裸机 extern

在 `--freestanding` 模式下，extern 用于访问硬件地址和中断向量：

```kaula
extern fn boot_main() -> void

#[naked] #[section(".isr_vector")]
fn reset_handler() void {
    ldr sp, =_stack_top
    bl boot_main
    b .
}
```

## 完整示例

参见 [stdlib-usage.md](stdlib-usage.md)。
