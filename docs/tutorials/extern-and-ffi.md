# Extern 声明与 FFI

Kaula 通过 `extern` 直接与 C 代码和底层符号互操作，无需绑定代码。

## 外部变量

声明链接器符号或 C 全局变量：

```kaula
extern bss_start: u8
extern stack_top: u64
extern _estack: u64
```

## 外部函数

声明 C 函数供 Kaula 调用（FFI）。

为了保持类型安全，**禁止裸 `void*`**，必须使用 `void(T...)R` 记法（见下文）来描述透明指针和函数指针。

```kaula
extern fn printf(fmt: *u8, ...) -> i32
extern fn memset(dst: *u8, c: i32, n: usize) -> *u8
extern fn malloc(size: usize) -> void()
extern fn free(ptr: void()) void
```

调用方式与普通 Kaula 函数相同：

```kaula
fn main() {
    auto msg = cstring("Hello from C!")
    printf(msg)

    void() mem = malloc(64)
    // ...
    free(mem)
}
```

## `void(T...)R` 记法：禁用裸 `void*`

Kaula 不允许出现裸 `void*`，所有 FFI 边界上的类型擦除指针必须使用 `void(T...)R` 记法明确表达含义：

| Kaula 记法 | C 等价类型 | 含义 |
|-----------|-----------|------|
| `void()` | `void*` | 透明数据指针（指向任意类型的数据） |
| `const void()` | `const void*` | 只读透明数据指针 |
| `*void()` | `void**` | 指向透明指针的指针 |
| `void(void())void` | `void (*)(void*)` | 接受透明指针参数的回调函数 |
| `void(void())void()` | `void* (*)(void*)` | 接受并返回透明指针的回调函数 |
| `void(const void(), const void())cint` | `int (*)(const void*, const void*)` | 比较函数（`cint` = C 的 `int`） |
| `void(void(), size_t)void` | `void (*)(void*, size_t)` | 接受指针+大小的回调函数 |
| `void(const GraphNode*, i64, void())void` | `void (*)(const GraphNode*, int64_t, void*)` | 混合普通类型与透明指针的回调 |

### 快速记忆法

结构为 `返回值类型(参数类型...)` 但反写为 `void(参数类型...)返回类型`：

```
C:    int  (*)(const void*, const void*)
       │       ╰─── 参数列表 ──────╯
       ╰── 返回
Kaula: void(const void(), const void())cint
       ╰固定前缀╯  ╰── 参数列表 ──────╯  ╰返回
```

前缀固定写 `void`（表示这是一个"签名式记法"），真正的返回值写在末尾括号之后。

### 为什么禁裸强转 / 裸 `void*`

1. **可读性**：看一眼 `void(void(), size_t)void` 就知道这是回调；`void*` 无法区分是数据指针、函数指针还是其他。
2. **类型安全**：使用 `cast<T>()` 或 `transmute<T>()` 显式转换时，编译器能识别来源与目标类型；裸 `void*` 强制转换（如 `(int)ptr`）无法追踪。
3. **跨平台**：`cint` 明确表示 C 的 `int`（32 位），与 Kaula 默认的 `int`（`int64_t`）区分开，避免 ABI 不匹配。

## `cint` 类型

Kaula 的基本类型 `int` 固定为 64 位 (`int64_t`)。而 C 的 `int` 在大多数平台上是 32 位。**FFI 签名中凡涉及 C 的 `int` 返回/参数（例如比较函数、`qsort`、`printf` 返回值等）必须写成 `cint`**，编译器会将其映射为 C 的原生 `int`。

| Kaula | C | 说明 |
|-------|---|------|
| `int` | `int64_t` | Kaula 默认整数 |
| `i32` | `int32_t` | 固定 32 位，ABI 可能与 C `int` 不同 |
| `cint` | `int` | C 语言原生 int，跨平台 FFI 必须用它 |

## 比较函数与 `qsort` 示例

C 标准库 `qsort` 的签名是：

```c
void qsort(void *base, size_t nmemb, size_t size,
           int (*compar)(const void *, const void *));
```

在 Kaula 中写成：

```kaula
extern fn qsort(void() base, size_t nmemb, size_t size,
                void(const void(), const void())cint compar) void

fn cmp_int(const void() a, const void() b) cint {
    i32 x = *(i32*)a
    i32 y = *(i32*)b
    if x < y { return -1 }
    if x > y { return 1 }
    return 0
}

fn main() {
    *i32 arr = cast<*i32>(malloc(sizeof<i32> * 5))
    arr[0] = 3; arr[1] = 1; arr[2] = 4; arr[3] = 1; arr[4] = 5
    qsort(arr, 5, sizeof<i32>, cmp_int)
    println("sorted: ", arr[0], arr[1], arr[2], arr[3], arr[4])
    free(arr)
}
```

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
