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

声明 C 函数供 Kaula 调用（FFI）：

```kaula
extern fn printf(fmt: *u8, ...) -> i32
extern fn memset(dst: *u8, c: i32, n: usize) -> *u8
extern fn malloc(size: usize) -> void*
extern fn free(ptr: void*)
```

调用方式与普通 Kaula 函数相同：

```kaula
fn main() {
    auto msg = cstring("Hello from C!")
    printf(msg)

    void* mem = malloc(64)
    // ...
    free(mem)
}
```

## 被 C 调用

Kaula 函数默认生成标准 C ABI，可直接被 C 代码调用：

```kaula
fn add(int a, int b) int {
    return a + b
}
```

编译后生成的 `add` 函数符合 C ABI，C 代码中 `extern int add(int, int);` 即可调用。

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

参见 [examples/extern_ffi.kl](examples/extern_ffi.kl)。
