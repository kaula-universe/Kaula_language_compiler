# 裸机开发指南

Kaula 编译器提供完整的裸机/系统级编程支持，涵盖内联汇编、外部符号声明、内存映射、原子操作等裸机开发核心能力。

## 快速开始

### 最小裸机程序

使用 `--boot pvh` 时无需自写 `_start`（引导 stub 已内建），只需提供 `kaula_main` 入口：

```kaula
// hello_serial.kl - 最小裸机程序（boot 模式）
fn kaula_main() -> i32 {
    return 0
}
```

不使用内建引导时，可自写入口：

```kaula
// boot.kl - 手写入口
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

### 编译命令（自动引导）

```bash
kaulac.exe --freestanding --boot pvh \
    --template kaula-compiler\templates \
    hello_serial.kl
```

一条命令完成全部构建：生成 C → 编译内核 → 编译内建 boot 引导 → 链接输出可引导 ELF。

或通过 `kaula.json` 配置：

```json
{
    "freestanding": true,
    "boot": "pvh"
}
```

### QEMU 验证

```bash
qemu-system-x86_64 -display none -kernel hello_serial.elf \
    -no-reboot -no-shutdown -serial file:serial_out.txt
cat serial_out.txt   # 串口输出 "Hello, Kaula from bare metal!"
```

## 内建引导（boot）

从 `--freestanding` 输出不可引导的 COFF/EXE 到可引导 ELF 的全过程（boot stub 汇编、链接脚本、链接器调用）已内建到编译器，无需手动编写。

> boot 模式与不带 `--boot` 的 freestanding 是两条独立流水线（见[编译选项](#编译选项)）：boot 模式由 `compileBootKernel` 用 `ld.lld` 手工链接，当前不启用 KMM 静态池宏；不带 `--boot` 的 freestanding 走 clang 直接链接，保留 `-DKMM_V4_STATIC_POOL`。

### --boot

| 模式 | 说明 |
|------|------|
| `pvh` | Xen PVH 引导（QEMU `-kernel` 直接支持 x86_64 ELF） |
| `custom` | 使用 `--boot-file` 指定的自定义引导汇编 |
| `none` | 不自动引导（默认，仅输出编译产物） |

boot 模式需要同时启用 `--freestanding`。

### 内置模板文件

| 文件 | 作用 |
|------|------|
| `templates/boot/x86_64-pvh.S` | x86_64 PVH 引导 stub（32 位入口 → 长模式 → 调 `_kaula_entry`） |
| `templates/linker/x86_64.ld` | x86_64 链接脚本（入口 `_start`，加载地址 1M） |

### 自动构建流水线

```
.kl 源码
  ├─ kaulac 生成 C 代码（freestanding 模板，含 _kaula_entry）
  ├─ clang -c 编译 C        → kernel.o
  ├─ clang -c 编译 boot stub → boot.o
  ├─ clang -c 编译 freestanding runtime → runtime.o (memset/memcpy 等)
  └─ ld.lld -T linker.ld 链接 → 可引导 ELF（或 raw bin）
```

### 已验证示例（x86_64 + QEMU）

串口输出例程（`hello_serial.kl`，通过 QEMU `-serial file:serial_out.txt` 验证 "Hello, Kaula from bare metal!"）：

```kaula
// 串口寄存器（COM1）
const SERIAL_PORT = 0x3F8
const UART_THR = 0
const UART_LSR = 5
const LSR_THRE = 0x20

#[inline, asm]
fn outb(u16 port, u8 val) {
    __asm__ __volatile__("outb %0, %1" : : "a"(val), "d"(port));
}

#[inline]
fn inb(u16 port) u8 {
    u8 ret
    #[asm("inb %1, %0", "=a"(ret), "d"(port))]
    return ret
}

fn serial_wait_ready() {
    u8 status = inb(SERIAL_PORT + UART_LSR)
    while ((status & LSR_THRE) == 0) {
        status = inb(SERIAL_PORT + UART_LSR)
    }
}

fn serial_putc(u8 c) {
    serial_wait_ready()
    outb(SERIAL_PORT + UART_THR, c)
}

fn serial_init() {
    outb(SERIAL_PORT + 1, 0x00)  // 关中断
    outb(SERIAL_PORT + 3, 0x80)  // DLAB 置位
    outb(SERIAL_PORT + 0, 0x01)  // 分频低字节 (115200)
    outb(SERIAL_PORT + 1, 0x00)  // 分频高字节
    outb(SERIAL_PORT + 3, 0x03)  // 8N1
    outb(SERIAL_PORT + 2, 0xC7)  // FIFO 使能
    outb(SERIAL_PORT + 4, 0x03)  // DTR/RTS 使能
}

fn kaula_main() i32 {
    serial_init()
    serial_putc(0x4F)  // 'O'
    serial_putc(0x4B)  // 'K'
    serial_putc(0x0A)  // '\n'
    return 0
}
```

该示例同时验证了：固定大小数组声明 `[N]type`、`if ((a) && (b))` 括号约束、extern 符号链接、boot stub 的 SSE/长模式初始化。

### 定制引导

```bash
# 使用自定义 boot stub（例如非标准初始化序列）
kaulac.exe --freestanding --boot custom --boot-file myboot.S kernel.kl

# 使用自定义链接脚本
kaulac.exe --freestanding --boot pvh --link-script mylayout.ld kernel.kl

# 指定目标架构（默认从 --target-triple 推断）
kaulac.exe --freestanding --boot pvh --target-triple x86_64-none-elf kernel.kl
```

### 相关配置项

| 配置项 | 命令行 flag | 说明 |
|--------|-------------|------|
| `boot` | `--boot` | 引导方式：pvh/custom/none |
| `boot_file` | `--boot-file` | 自定义引导汇编路径 |
| `boot_arch` | `--boot-arch` | 引导架构（x86_64/i386/riscv64/aarch64） |
| `link_script` | `--link-script` | 自定义链接脚本（覆盖内置） |
| `target_triple` | `--target-triple` | 目标三元组（决定 boot 架构与 clang 目标） |
| `output_format` | `--output-format` | `elf`（默认）/ `bin`（raw binary） |

`boot` 模式下入口固定为 `_start`（引导 stub 定义，链接脚本 ENTRY），用户代码入口为 `kaula_main`（由模板 `_kaula_entry` 调用），无需再传 `--entry`。

## 编译选项

### --freestanding

启用裸机模式。按是否指定 `--boot` 分为两条流水线：

**1. 不带 `--boot`（clang 直接链接）**，自动添加以下 clang 参数：

| 参数 | 作用 |
|------|------|
| `-ffreestanding` | 不依赖标准库和操作系统 |
| `-nostdlib` | 不链接标准库 |
| `-nostartfiles` | 不链接启动文件（crt0 等） |
| `-DKMM_V4_STATIC_POOL` | KMM 内存分配器使用静态池（BSS 段） |
| `-DKAULA_FREESTANDING` | 全局宏定义，用于条件编译 |

**2. 带 `--boot`（内建引导流水线，见上文）**，clang 编译参数：

| 参数 | 作用 |
|------|------|
| `-ffreestanding` | 不依赖标准库和操作系统 |
| `-nostdlib` | 不链接标准库 |
| `-fno-pic` / `-mcmodel=large` | 内核代码模型（高位地址可寻址） |
| `-DKAULA_FREESTANDING` | 全局宏定义，用于条件编译 |

两条路径都自动链接 freestanding runtime（`kaula_freestanding_runtime.c`），提供 `memset`/`memcpy`/`memmove`/`memcmp`/`strlen` 实现。

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
typedef struct K_PageTableEntry {
    uint32_t present : 1;
    uint32_t writable : 1;
    uint32_t user : 1;
    // ...
    uint32_t pfn : 20;
    // ...
} K_PageTableEntry;
```

> 注意：所有用户定义的 struct 在生成 C 代码时统一添加 `K_` 前缀（如 `K_PageTableEntry`），以避免与系统头文件中的宏/类型冲突（详见[代码生成器文档](code-generation.md#用户类型命名消歧)）。

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

## KMM V4 静态池模式

不带 `--boot` 的 freestanding 路径定义 `KMM_V4_STATIC_POOL`，Kaula 内存分配器使用 BSS 段中的静态池，无需 `malloc`/`free`。

> 注意：`--boot` 内建引导流水线当前不定义 `KMM_V4_STATIC_POOL` 宏，boot 模式下 `std.memory.std_malloc` 等分配函数不可用，需要堆分配时需自行提供实现（如手工编译链接 KMM 或自写 bump allocator）。

### 设计原理

KMM V4 采用 per-thread heap + bump allocation 架构。静态池模式下：

- **池内存**：在 BSS 段分配 `g_kmm_v4_pool[KMM_V4_POOL_SIZE]`，编译期确定大小
- **全局 offset**：单调递增，记录已分配的字节数
- **Per-thread heap**：每个线程从全局池批量获取内存块，线程本地 offset 推进分配
- **作用域回收**：`kmm_v4_scope_push`/`kmm_v4_scope_pop` 保存/恢复线程本地 offset

```c
// 静态池模式下的内存布局
static uint8_t g_kmm_v4_pool[KMM_V4_POOL_SIZE];  // BSS 段
static size_t  g_kmm_v4_offset = 0;               // 全局分配偏移

// Per-thread heap（线程本地）
__thread struct {
    uint8_t* base;     // 当前 thread heap 起始
    size_t   offset;   // 线程本地分配偏移
    size_t   capacity; // thread heap 容量
} g_kmm_v4_thread_heap;
```

### 静态池 vs 动态池

| 特性 | 静态池 (`KMM_V4_STATIC_POOL`) | 动态池（默认） |
|------|-------------------------------|----------------|
| 内存来源 | BSS 段，编译期固定 | 运行时 `malloc` 申请 |
| 池大小 | 默认 16MB（freestanding 优化） | 默认 256MB（64位 hosted） |
| 系统依赖 | 无 | 需要 `malloc` |
| 适用场景 | 裸机/内核/嵌入式 | 用户态应用 |
| 线程安全 | 原子 CAS（Level 1+） | 原子 CAS（Level 1+） |
| `kmm_v4_free` | no-op（作用域回收） | no-op（作用域回收） |

### 池大小配置

通过 `-D` 编译选项覆盖默认池大小：

```bash
# 自定义池大小为 32MB
clang -DKMM_V4_STATIC_POOL -DKMM_V4_POOL_SIZE=$((32*1024*1024)) ...

# 或在 kaula.json 中配置
{
    "freestanding": true,
    "extra_clang_flags": ["-DKMM_V4_POOL_SIZE=33554432"]
}
```

池大小选择建议：

| 场景 | 推荐大小 | 说明 |
|------|----------|------|
| 嵌入式（RAM < 64MB） | 4-8 MB | 节省 BSS 段 |
| 内核开发 | 16-64 MB | 平衡内核内存 |
| 用户态应用（静态池） | 64-256 MB | 充足分配空间 |

### freestanding 下的分配流程

```c
// 1. 线程首次分配：从全局池获取 thread heap
//    kmm_v4_thread_heap_refill() 使用 CAS 从 g_kmm_v4_offset 推进
uint8_t* kmm_v4_thread_heap_refill(size_t min_needed) {
    size_t chunk = KMM_TLS_BUFFER_SIZE;  // 默认 4 * L1 cache
    if (min_needed > chunk) chunk = min_needed;
    
    // CAS 推进全局 offset
    size_t old = atomic_load(&g_kmm_v4_offset);
    size_t new_offset = old + chunk;
    if (new_offset > g_kmm_v4_pool_capacity) return NULL;
    while (!atomic_cas_weak(&g_kmm_v4_offset, old, new_offset)) {
        old = atomic_load(&g_kmm_v4_offset);
        new_offset = old + chunk;
        if (new_offset > g_kmm_v4_pool_capacity) return NULL;
    }
    
    g_kmm_v4_thread_heap.base = g_kmm_v4_pool + old;
    g_kmm_v4_thread_heap.offset = 0;
    g_kmm_v4_thread_heap.capacity = chunk;
    return g_kmm_v4_thread_heap.base;
}

// 2. 后续分配：直接推进 thread heap offset（无原子操作）
static inline void* kmm_v4_alloc_auto(size_t size) {
    size_t aligned = (size + 7) & ~7;  // 8 字节对齐
    if (g_kmm_v4_thread_heap.offset + aligned <= g_kmm_v4_thread_heap.capacity) {
        void* ptr = g_kmm_v4_thread_heap.base + g_kmm_v4_thread_heap.offset;
        g_kmm_v4_thread_heap.offset += aligned;
        return ptr;  // 快速路径，无锁
    }
    // 慢路径：refill thread heap
    ...
}
```

### 性能特点

静态池模式下，KMM V4 同样保持高性能：

- **分配**：O(1) bump pointer，无系统调用
- **释放**：no-op，作用域退出批量回收
- **多线程**：per-thread heap 隔离，CAS 仅在 refill 时触发
- **内存安全**：池越界返回 NULL（严格模式）或回退 malloc（`KMM_V4_ENABLE_FALLBACK=1`）

### 裸机使用示例

适用于不带 `--boot` 的 freestanding 路径（boot 模式下 KMM 不可用，见上文注意）：

```kaula
// 裸机程序使用 KMM V4 静态池
#[naked, section(".text.boot")]
fn _start() -> void {
    kaula_main()
}

fn kaula_main() -> void {
    // KMM V4 自动管理作用域
    auto buf = std.memory.std_malloc(4096)   // 从静态池分配
    // ... 使用 buf ...
    // 函数返回时自动回收
}
```

### 配置宏总览

| 宏 | 默认值 | 说明 |
|----|--------|------|
| `KMM_V4_STATIC_POOL` | 不含 `--boot` 的 freestanding 自动定义 | 启用静态池模式 |
| `KMM_V4_POOL_SIZE` | 16MB (static) / 256MB (dynamic) | 池大小（字节） |
| `KMM_V4_ALIGNMENT` | 8/16/32/64（按 SIMD 自动） | 对齐字节数 |
| `KMM_THREAD_SAFETY_LEVEL` | 1（优化模式） | 线程安全级别 |
| `KMM_V4_ENABLE_FALLBACK` | 0（release） | 越界时回退 malloc |
| `KMM_TLS_BUFFER_SIZE` | 4 * L1 cache | Per-thread heap 批量大小 |

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
| KMM 静态池 | 自动（freestanding，不含 `--boot` 路径） | 已实现 |
| freestanding runtime | 自动（5 个函数） | 已实现 |
| 裸机入口模板 | `freestanding.c.tmpl` | 已实现 |
| 内建引导 | `--boot pvh/custom` + 内置 boot stub/linker 脚本 | 已实现 |
