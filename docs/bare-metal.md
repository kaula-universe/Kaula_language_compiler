# 裸机开发指南

Kaula 编译器提供完整的裸机/系统级编程支持，涵盖内联汇编、外部符号声明、内存映射、原子操作等裸机开发核心能力。

## 快速开始

### 最小裸机程序

```kaula
// boot.kl - 最小裸机入口
#[naked, section(".text.boot")]
fn _start() -> void {
    #[asm("mov %cr3, %rax")]
    kaula_main()
    #[asm("hlt")]
}

fn kaula_main() -> void {
    // 用户入口
}
```

### 编译命令

```bash
kaulac.exe --freestanding \
    --target-triple x86_64-unknown-elf \
    --link-script linker.ld \
    --entry _start \
    --output-format bin \
    boot.kl
```

或通过 `kaula.json` 配置：

```json
{
    "freestanding": true,
    "target_triple": "x86_64-unknown-elf",
    "link_script": "linker.ld",
    "entry": "_start",
    "output_format": "bin"
}
```

## 编译选项

### --freestanding

启用裸机模式，自动添加以下 clang 参数：

| 参数 | 作用 |
|------|------|
| `-ffreestanding` | 不依赖标准库和操作系统 |
| `-nostdlib` | 不链接标准库 |
| `-nostartfiles` | 不链接启动文件（crt0 等） |
| `-DKMM_V4_STATIC_POOL` | KMM 内存分配器使用静态池（BSS 段） |
| `-DKAULA_FREESTANDING` | 全局宏定义，用于条件编译 |

自动链接 freestanding runtime（`kaula_freestanding_runtime.c`），提供 `memset`/`memcpy`/`memmove`/`memcmp`/`strlen` 实现。

头文件裁剪：freestanding 模式下不包含 `<stdio.h>`/`<stdlib.h>`，仅保留 `<stdint.h>`/`<stddef.h>` 等 freestanding 头文件。

### --target-triple

指定目标平台三元组，常见值：

| 三元组 | 平台 |
|--------|------|
| `x86_64-unknown-elf` | x86-64 bare metal |
| `aarch64-none-elf` | ARM64 bare metal |
| `riscv64-unknown-elf` | RISC-V 64 bare metal |
| `x86_64-linux-gnu` | x86-64 Linux 交叉编译 |

### --link-script

指定链接脚本路径（`.lds` 文件），控制内存布局、段放置、入口地址等。

### --entry

指定入口函数名，默认 `main`。裸机下通常为 `_start`。

### --output-format

| 格式 | 说明 | clang 参数 |
|------|------|-----------|
| `elf` | 可执行 ELF（默认） | 无额外参数 |
| `bin` | raw binary 镜像 | `-Wl,--oformat=binary` |
| `obj` | 仅编译为 `.o` 目标文件 | `-c` |

## 表达式级属性

Kaula 使用 `#[name(args)]` 语法统一处理表达式级特殊操作。这种语法既可用于函数注解，也可作为表达式嵌入语句中。

### 内联汇编

```kaula
// 简单形式：读取 CR3 寄存器
let cr3 = #[asm("mov %cr3, %rax")]

// 扩展形式：带输入/输出操作数
let result = #[asm(
    "mov %0, %1\n\t"
    "add %0, $1",
    "=r"(out),
    "r"(in),
    "memory"
)]
```

生成的 C 代码：

```c
// 简单形式
__asm__ __volatile__("mov %cr3, %rax")

// 扩展形式
({ __asm__ __volatile__("mov %0, %1\n\t" "add %0, $1" : "=r"(out) : "r"(in) : "memory"); })
```

### Volatile 内存访问

用于 MMIO 寄存器读写，确保编译器不会优化掉内存访问：

```kaula
// 读取 MMIO 寄存器
let value = #[volatile_load(0xFFFFFFFFFFFE0000)]

// 写入 MMIO 寄存器
#[volatile_store(reg_ctrl, 0x1)]
```

生成的 C 代码：

```c
(*(volatile typeof(*reg_ctrl)*)(reg_ctrl))           // load
(*(volatile typeof(*reg_ctrl)*)(reg_ctrl) = (0x1))   // store
```

### 原子操作

```kaula
// 原子加载
let val = #[atomic_load(ptr)]

// 原子存储
#[atomic_store(ptr, 42)]

// 原子比较交换 (CAS)
let success = #[atomic_cas(ptr, expected, new_val)]

// 原子加法 (Fetch-and-Add)
let old = #[atomic_faa(ptr, 1)]
```

所有原子操作使用 `__ATOMIC_SEQ_CST`（顺序一致性）内存序。

生成的 C 代码：

```c
__atomic_load_n((ptr), __ATOMIC_SEQ_CST)
__atomic_store_n((ptr), (42), __ATOMIC_SEQ_CST)
__atomic_compare_exchange_n((ptr), &(expected), (new_val), 0, __ATOMIC_SEQ_CST, __ATOMIC_SEQ_CST)
__atomic_fetch_add((ptr), (1), __ATOMIC_SEQ_CST)
```

### 内存屏障

```kaula
// 全屏障
#[fence()]
```

生成 `__atomic_thread_fence(__ATOMIC_SEQ_CST)`。

## 声明语法

### extern 外部符号声明

声明链接脚本符号或外部 C/汇编变量，不分配存储：

```kaula
// 变量声明
extern bss_start: u8
extern stack_top: u64
extern kernel_end: u32
```

生成 `extern uint8_t bss_start;` 等。

### extern 函数声明

声明外部 C/汇编函数：

```kaula
// 无参数无返回值
extern fn boot_main() -> void

// 带参数和返回值
extern fn memset(dst: *u8, c: i32, n: usize) -> *u8
extern fn memcpy(dst: *u8, src: *u8, n: usize) -> *u8
```

生成：

```c
extern void boot_main(void);
extern uint8_t* memset(uint8_t* dst, int32_t c, uintptr_t n);
extern uint8_t* memcpy(uint8_t* dst, uint8_t* src, uintptr_t n);
```

### static 静态变量

函数内静态变量，生命周期为整个程序，只初始化一次：

```kaula
fn counter() -> i32 {
    static count: i32 = 0
    count = count + 1
    return count
}
```

全局作用域也支持 `static`，限制符号可见性。

### const 编译期常量

Kaula 的 `const` 支持真正的编译期求值。常量表达式在编译期被求值为字面量，后续引用直接内联：

```kaula
// 基本常量
const PAGE_SIZE: u64 = 4096
const PAGE_SHIFT: u32 = 12

// 编译期算术求值
const PAGE_MASK: u64 = PAGE_SIZE - 1           // 求值为 4095
const KERNEL_BASE: u64 = 0xFFFFFFFF80000000
const KERNEL_STACK: u64 = KERNEL_BASE + 0x1000  // 求值为 0xFFFFFFFF80001000

// 位运算
const PERM_READ: u32 = 1 << 0    // 求值为 1
const PERM_WRITE: u32 = 1 << 1   // 求值为 2
const PERM_EXEC: u32 = 1 << 2    // 求值为 4
const PERM_ALL: u32 = PERM_READ | PERM_WRITE | PERM_EXEC  // 求值为 7
```

支持的编译期运算：`+ - * / % << >> & | ^`

引用 const 变量时，编译器直接内联求值后的字面量，不生成变量引用。

## 结构体位域

用于精确控制内存布局，映射硬件寄存器：

```kaula
struct PageTableEntry {
    present: u32 : 1,
    writable: u32 : 1,
    user: u32 : 1,
    write_through: u32 : 1,
    cache_disable: u32 : 1,
    accessed: u32 : 1,
    dirty: u32 : 1,
    pat: u32 : 1,
    global: u32 : 1,
    ignored: u32 : 3,
    pfn: u32 : 20,
    reserved: u32 : 3,
    protection_key: u32 : 4,
    exec_disable: u32 : 1
}
```

生成：

```c
typedef struct PageTableEntry {
    uint32_t present : 1;
    uint32_t writable : 1;
    uint32_t user : 1;
    // ...
    uint32_t pfn : 20;
    // ...
} PageTableEntry;
```

## 函数与类型属性

### 函数属性

| 属性 | 语法 | 生成的 C 属性 | 说明 |
|------|------|-------------|------|
| `#[naked]` | `#[naked] fn isr() {}` | `__attribute__((naked))` | 无 prologue/epilogue，适用于手写汇编函数 |
| `#[section("...")]` | `#[section(".text.boot")] fn _start() {}` | `__attribute__((section(".text.boot")))` | 指定代码段 |
| `#[weak]` | `#[weak] fn handler() {}` | `__attribute__((weak))` | 弱符号，可被覆盖 |
| `#[deprecated]` | `#[deprecated] fn old_api() {}` | `__attribute__((deprecated))` | 标记弃用 |
| `#[inline]` | `#[inline] fn helper() {}` | `__attribute__((always_inline))` | 强制内联 |
| `#[asm("...")]` | `#[asm("...")] fn f() {}` | 函数体替换为汇编 | 整个函数为内联汇编 |

`#[asm]` 函数注解示例：

```kaula
#[asm("cli", section(".text.boot"))]
fn disable_interrupts() -> void {}
```

生成：

```c
__attribute__((section(".text.boot")))
void disable_interrupts(void) {
    __asm__ __volatile__("cli");
}
```

### 结构体属性

| 属性 | 语法 | 生成的 C 属性 | 说明 |
|------|------|-------------|------|
| `#[packed]` | `#[packed] struct Header {}` | `__attribute__((packed))` | 取消对齐填充 |
| `#[aligned]` | `#[aligned] struct Page {}` | `__attribute__((aligned))` | 默认最大对齐 |
| `#[aligned(N)]` | `#[aligned(16)] struct Page {}` | `__attribute__((aligned(16)))` | 指定 N 字节对齐 |

### 变量属性

| 属性 | 语法 | 生成的 C 属性 | 说明 |
|------|------|-------------|------|
| `#[volatile]` | `#[volatile] reg: u32` | `volatile` | volatile 修饰 |
| `#[section("...")]` | `#[section(".data")] buf: u8` | `__attribute__((section(".data")))` | 指定数据段 |
| `#[aligned(N)]` | `#[aligned(4096)] page: u8` | `__attribute__((aligned(4096)))` | 指定对齐 |
| `#[weak]` | `#[weak] flag: i32` | `__attribute__((weak))` | 弱符号 |

## Freestanding 运行时

Kaula 在 freestanding 模式下自动链接 `kaula_freestanding_runtime.c`，提供以下 C 标准库函数的裸机实现：

| 函数 | 原型 | 说明 |
|------|------|------|
| `memset` | `void* memset(void* s, int c, size_t n)` | 内存填充 |
| `memcpy` | `void* memcpy(void* dst, const void* src, size_t n)` | 内存拷贝（不处理重叠） |
| `memmove` | `void* memmove(void* dst, const void* src, size_t n)` | 内存拷贝（安全处理重叠） |
| `memcmp` | `int memcmp(const void* s1, const void* s2, size_t n)` | 内存比较 |
| `strlen` | `size_t strlen(const char* s)` | 字符串长度 |

## 裸机入口模板

freestanding 模式使用 `freestanding.c.tmpl` 模板，提供：

- `extern int kaula_main(void);` 前向声明
- `_kaula_entry` 入口函数，标记 `__attribute__((noreturn))`
- 调用 `kaula_main()` 后停机
- 架构自适应停机指令：x86 用 `hlt`，其他架构用 `wfi`，最后 `while(1) {}` 死循环

模板占位符：

| 占位符 | 内容 |
|--------|------|
| `{{includes}}` | 头文件包含（freestanding 下裁剪） |
| `{{forward_decls}}` | 前向声明 |
| `{{global_vars}}` | 全局变量（含 extern/static/const） |
| `{{type_code}}` | 类型定义（struct/enum/typedef） |
| `{{function_code}}` | 函数定义 |

## KMM 静态池

freestanding 模式下定义 `KMM_V4_STATIC_POOL`，Kaula 内存分配器使用 BSS 段中的静态池，无需 `malloc`/`free`：

- 池内存在 BSS 段分配，编译期确定大小
- 无需操作系统支持
- 适用于裸机/内核环境

## 完整示例

### x86-64 页表操作

```kaula
// 寄存器地址定义
const CR3_ADDR: u64 = 0xFFFFFFFFFFFE0000
const PML4_BASE: u64 = 0xFFFFFFFF80000000

// 页表项结构（位域）
struct PTE {
    present: u64 : 1,
    writable: u64 : 1,
    user: u64 : 1,
    pwt: u64 : 1,
    pcd: u64 : 1,
    accessed: u64 : 1,
    dirty: u64 : 1,
    pat: u64 : 1,
    global: u64 : 1,
    ignored: u64 : 3,
    pfn: u64 : 40,
    reserved: u64 : 7,
    pk: u64 : 4,
    xd: u64 : 1
}

// 外部符号
extern pml4_table: u64

// 读取 CR3
#[naked, section(".text.boot")]
fn read_cr3() -> u64 {
    // naked 函数体由汇编填充
}

// 切换页表
fn switch_page_table(table: *u64) -> void {
    #[asm("mov %0, %%cr3" :: "r"(table))]
}

// MMIO 寄存器读写
fn read_mmio(addr: u64) -> u32 {
    return #[volatile_load(addr)]
}

fn write_mmio(addr: u64, val: u32) -> void {
    #[volatile_store(addr, val)]
}

// 原子计数器
fn atomic_increment(counter: *u32) -> u32 {
    return #[atomic_faa(counter, 1)] + 1
}

// 主入口
fn kaula_main() -> void {
    // 读取当前页表基址
    let cr3 = #[asm("mov %cr3, %rax")]

    // 内存屏障
    #[fence()]

    // 写入 MMIO
    write_mmio(CR3_ADDR, 0x1)
}
```

### 配置文件

```json
{
    "freestanding": true,
    "target_triple": "x86_64-unknown-elf",
    "link_script": "linker.ld",
    "entry": "_kaula_entry",
    "output_format": "bin",
    "opt_level": "O2"
}
```

## 特性总览

| 特性 | 语法 | 状态 |
|------|------|------|
| 表达式级内联汇编 | `#[asm("...")]` | 已实现 |
| extern 变量声明 | `extern name: type` | 已实现 |
| extern 函数声明 | `extern fn name(params) -> ret` | 已实现 |
| static 静态变量 | `static name: type = value` | 已实现 |
| const 编译期常量 | `const name: type = expr` | 已实现（含编译期求值） |
| 位域 | `field: type : width` | 已实现 |
| volatile 内存访问 | `#[volatile_load/store(ptr)]` | 已实现 |
| 原子操作 | `#[atomic_load/store/cas/faa]` | 已实现 |
| 内存屏障 | `#[fence()]` | 已实现 |
| 函数属性 | `#[naked/section/weak/deprecated/inline/asm]` | 已实现 |
| 结构体属性 | `#[packed/aligned]` | 已实现 |
| 变量属性 | `#[volatile/section/aligned/weak]` | 已实现 |
| 目标三元组 | `--target-triple` | 已实现 |
| 链接脚本 | `--link-script` | 已实现 |
| freestanding 模式 | `--freestanding` | 已实现 |
| 入口函数 | `--entry` | 已实现 |
| 输出格式 | `--output-format elf/bin/obj` | 已实现 |
| KMM 静态池 | 自动（freestanding 下） | 已实现 |
| freestanding runtime | 自动（5 个函数） | 已实现 |
| 裸机入口模板 | `freestanding.c.tmpl` | 已实现 |
