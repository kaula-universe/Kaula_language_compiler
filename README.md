# Kaula 编程语言

<div align="center">

<img src="logo.png" alt="Kaula Logo" width="200">

**更现代、更好用的 C**

[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8.svg?logo=go)](https://go.dev/)
[![Language](https://img.shields.io/badge/runtime-C-A8B9CC.svg?logo=c)](https://en.wikipedia.org/wiki/C_(programming_language))

</div>

---

## Kaula 是什么

Kaula 是一门**静态类型的系统级编程语言**，编译到 C，由 Clang 生成原生代码。

核心设计目标：**用更简单的语法，获得接近 Rust 的安全保证**——通过 SOR（子结构所有权）在编译期消除数据竞争和悬垂指针，而不引入运行时开销。

---

## 快速示例

### Hello World

```kaula
import std.io

fn main() {
    println("Hello, Kaula!")
}
```

### 变量与类型

```kaula
i32 x = 42               # 显式类型
auto y = 3.14             # 类型推导
string name = "Kaula"
bool flag = true
i32* ptr = &x             # 指针
i32? maybe = null         # 可空类型
const MAX = 1024          # 编译期常量
```

### 函数与泛型

```kaula
fn add(i32 a, i32 b) i32 {
    return a + b
}

# 泛型函数
fn first<T>(T a, T b) T {
    return a
}

# 内联汇编
#[asm]
fn read_cr0() u64 {
    mov %cr0, %rax
}
```

### 控制流

```kaula
# if/else
if x > 0 {
    println("positive")
} else if x == 0 {
    println("zero")
} else {
    println("negative")
}

# while
while x > 0 {
    x = x - 1
}

# for (range 系迭代)
for i in range(10) {
    println(i)
}
# range(start, end) / range(start, end, step) 同样支持
for i in range(0, 10, 2) {
    println(i)
}

# switch
switch x {
    case 1:
        println("one")
    case 2:
        println("two")
    default:
        println("other")
}
```

### struct 与 class

```kaula
# 结构体 - 值类型
#[packed]
struct PacketHeader {
    u8 version
    u8 flags
    u16 length
}

# 类 - 引用语义，支持方法和构造函数
class Vec2 {
    f64 x
    f64 y

    constructor(f64 x, f64 y) {
        self.x = x
        self.y = y
    }

    fn length() f64 {
        return math_sqrt(self.x * self.x + self.y * self.y)
    }
}

# 接口
interface Printable {
    fn to_string() string
}
```

### enum 与 match

```kaula
# 代数数据类型
enum Option<T> {
    Some(T)
    None
}

enum Result<T, E> {
    Ok(T)
    Err(E)
}

# 模式匹配
auto result = fetch_data()
match result {
    Ok(data) => {
        println(data)
    }
    Err(e) => {
        println("error: " + e)
    }
    _ => {
        println("unknown")
    }
}
```

### SOR 子结构所有权

Kaula 的核心安全机制——SOR（Sub-structural Ownership）在编译期追踪资源所有权，无需 GC 或引用计数开销：

```kaula
#[sor]
fn process() {
    auto buf = std.memory.kmm_v4_alloc(1024)

    # yield: 转移所有权，原变量不可再访问
    yield buf -> owner

    # extract: 从集合中提取子结构所有权
    extract owner[0] -> first_byte

    # release: 将所有权分发到多个持有者
    release owner -> [holder_a, holder_b]
}
```

三大原语：
- **yield** — 所有权转移（move 语义）
- **extract** — 子结构提取（从复合类型中取出部分所有权）
- **release** — 所有权分发（将一个资源拆分给多个持有者）

启用方式：`--sor` 全局启用，或 `#[sor]` 单函数启用。

### Prefix 前缀系统

Kaula 独有的声明式代码复用机制：

```kaula
# 定义前缀模板
prefix Logger {
    $message
    $level
    auto log = $level + ": " + $message
    println(log)
}

# 用 @ 调用前缀
@Logger(message = "server started", level = "INFO")

# 前缀内可用 $ 引用参数
prefix Validate {
    $value
    $min
    if $value < $min {
        println("validation failed")
    }
}

@Validate(value = age, min = 0)
```

### Spend/Call 消费式遍历

对集合的逐元素结构化处理：

```kaula
auto items = [10, 20, 30]
spend(items) {
    call(1) {
        println("first")
    }
    call(2) {
        println("second")
    }
    call(3) {
        println("third")
    }
}
```

### 属性注解

```kaula
#[packed]                         # 紧凑内存布局
#[aligned(16)]                    # 对齐要求
#[section(".isr_vector")]         # 链接到指定段
#[volatile]                       # volatile 语义
#[naked]                          # 裸函数（无 prologue/epilogue）
#[inline]                         # 内联提示
#[no_kmm]                         # 禁用 KMM 内存管理
#[deprecated]                     # 标记弃用
#[weak]                           # 弱符号
```

### 类型反射与编译期计算

```kaula
auto sz = sizeof(PacketHeader)        # 类型大小
auto al = alignof(PacketHeader)       # 类型对齐
auto off = offsetof(Packet, length)   # 字段偏移

# 编译期表达式
comptime auto count = #field_count(MyStruct)
comptime auto name = #type_name(i32)
comptime auto kind = #type_kind(MyEnum)
```

### extern 与 FFI

```kaula
# 声明外部符号
extern bss_start: u8

# 声明外部函数
extern fn boot_main() -> void

# 导入标准库
import std.io
import std.math
import std.string
```

### 裸机开发

```kaula
#[naked]
#[section(".init")]
fn _start() {
    # 直接操作硬件寄存器
    #[volatile] u32* reg = 0x40000000
    *reg = 0x01
}

# --freestanding 编译模式
# 自动链接 kaula_freestanding_runtime.c
# 提供 memset/memcpy/memmove 等最小运行时
```

### Freestanding 无依赖标准库

Kaula 提供 **freestanding** 库，作为 std 的无依赖对等体：

```kaula
// 导入 freestanding 模块（与 std 同级别）
import freestanding.base
import freestanding.memory
import freestanding.string
import freestanding.math
import freestanding.io

fn main() {
    // 内存管理（BSS 静态池 bump 分配，无 free list、无系统调用）
    char* buf = as<char*>(fs_alloc(64))
    memset(buf, 65, 8)
    fs_free(buf)

    // 字符串处理
    char* s = fs_strdup("hello")
    fs_itoa(-9876, buf)
    fs_itoa_hex(0xCAFE, buf, true)

    // 数学运算
    math_abs_i32(-42)
    math_gcd(48, 36)
    math_is_pow2(256)

    // I/O（弱符号钩子，托管模式可覆写 fs_output_putchar 重定向到控制台）
    println("Hello freestanding!")
    print_int(2026)
    print_hex(0xDEADBEEF)
    print("printf: %d %s\n", 42, "test")
}
```

**特性**：
- 与 std 相同的调用方式：`import freestanding.memory` 等
- 所有符号均为弱符号（`FS_WEAK`），托管/裸机双环境可跑，不冲突 libc
- 内存管理采用 BSS 静态池 bump 分配器，无 free list、无碎片、无系统调用
- 仅依赖编译器自带 freestanding 头（`<stdint.h>`/`<stddef.h>`/`<stdbool.h>`）
- 裸机模式下自动链接 `kaula_freestanding.lib`/`libkaula_freestanding.a`

---

## 第三方库（pkglib）

**放库即用、零配置**：把任意 C/C++ 库源码目录放进 `pkglib/`，`import` 即可直接调用。编译器自动完成分析 → 桥接 → 构建 → 链接：

```kaula
import stb_image   // C 库
import imgui       // C++ 库：自动生成 extern "C" 桥接（kbridge_* 前缀）

fn main() {
    void* img = stbi_load("texture.png", &width, &height, &channels, 4)
    stbi_image_free(img)
    println(kbridge_GetVersion())  // 1.92.9
}
```

- **自动分析**：无配置/配置过期（自动生成配置 + 源码更新或登记头文件消失）→ 自动用 Clang 重新提取函数签名
- **自动桥接**：C++ 头自动生成 `<lib>_kbridge.h/.cpp` 并编译自检；`T&`→`T*`、其他类型指针→`void*` 还原，重载保留最优签名
- **自动构建**：归档缺失/源码更新时编译 `lib<name>.a`，C++ 库自动链接 `c++/c++abi`
- **自愈合并**：重新分析保留人工链接项（如 imgui 的 `d3d11/dwmapi/d3dcompiler`），`--skip-auto-pkg` 可关闭
- 常用命令：
  - `kaulac --build-pkglib <库名>`（幂等：分析/重分析/构建）、`--build-pkglib all`
  - `kaulac --analyze-pkg <库名>`（强制手动重新解析并重写配置）、`--analyze-pkg-all`
  - 注意：`--analyze-pkg` 直接重写配置、不合并人工链接项；`--build-pkglib` 走 `MergeLibrariesInto` 保留人工项（如 imgui 的系统库），手写配置且想重分析建议用它
  - `--force-pkg` 强制重建归档；`--pkglib <目录>` 指定第三方库目录
- 已知边界：类成员方法、模板值返回不自动暴露；个别库需人工补一次系统链接库（重分析不丢）

---

## KMM V4 内存管理

Kaula 默认使用 KMM V4（Kaula Memory Manager V4）作为内存分配器，基于 per-thread heap + bump allocation + scope-based reclamation 设计，相比传统 malloc/free 有显著性能优势。

### 性能对比（vs C malloc/free）

基于 10M 次迭代基准测试（Windows, Clang 16, x86-64）：

| 场景 | KMM V4 | malloc/free | 加速比 |
|------|--------|-------------|--------|
| 16B 小对象分配+回收 | 51.8 ms | 634.3 ms | **12.2x** |
| 64B 对象分配+回收 | 65.9 ms | 661.5 ms | **10.0x** |
| 256B 对象分配+回收 | 152.3 ms | 711.8 ms | **4.6x** |
| 零初始化分配（calloc 等价） | 66.1 ms | 704.0 ms | **10.6x** |
| 纯分配吞吐量（64B, 1M 次） | 3.5 ms | 72.7 ms | **20.5x** |
| 字符串处理（32+64+128B） | 72.9 ms | 575.0 ms | **7.8x** |
| 链表节点（8x48B） | 139.1 ms | 594.5 ms | **4.2x** |
| 大对象（4KB） | 486.3 ms | 1597.6 ms | **3.2x** |
| 混合负载（16~1024B 交替） | 287.9 ms | 658.1 ms | **2.2x** |

**核心优势**：纯分配路径快 20x+，小对象分配快 10x+，零初始化分配快 10x。

### 设计特点

- **Per-Thread Heap**：每个线程从全局池批量获取内存块，分配时只推进线程本地 offset，无原子操作、无锁
- **Bump Allocation**：分配即指针推进，O(1) 复杂度，无碎片
- **Scope-based Reclamation**：`kmm_v4_scope_push`/`kmm_v4_scope_pop` 保存/恢复线程本地 offset，作用域退出批量回收，无需逐个 free
- **Inline 优化**：编译器将 `std_malloc` 重写为 `kmm_v4_alloc_auto` inline 调用，消除函数调用开销
- **kmm_v4_free 为 no-op**：无需维护 free list，释放零开销

```kaula
// KMM V4 自动作用域管理示例
fn process_data() {
    // 编译器自动插入 kmm_v4_scope_push()
    auto buf1 = std.memory.std_malloc(1024)   // 内联为 kmm_v4_alloc_auto(1024)
    auto buf2 = std.memory.std_malloc(2048)   // 内联为 kmm_v4_alloc_auto(2048)
    // ... 使用 buf1, buf2 ...
    // 编译器自动插入 kmm_v4_scope_pop()，批量回收
}
```

### 线程安全

- **全局 offset**：单调递增，CAS 推进，永不回退
- **Per-thread heap offset**：scope 操作仅影响当前线程，多线程互不干扰
- **Thread Safety Level**：自动根据编译模式选择（0=单线程零开销，1=轻量实时原子，2=完全线程安全）

---

## 标准库

62 个模块，800+ 函数，覆盖从系统编程到 Web 开发：

| 分类 | 模块 |
|------|------|
| **内存** | memory（KMM V4 per-thread heap 作用域分配器，比 malloc 快 2-20x）、bitset |
| **容器** | container（Vector/LinkedList/HashMap/Stack）、deque、heap、trie、graph |
| **字符串** | string、regex、unicode、template |
| **I/O** | io、fs、path |
| **并发** | concurrent（线程/锁/原子/线程池/Channel/Future）、async、parallel |
| **网络** | net、web、ssh、tls |
| **序列化** | json、toml、xml、protobuf、msgpack、serialize |
| **数学** | math、cmath、decimal、random |
| **编码** | encoding、crypto、compress、archive |
| **系统** | system、subprocess、cli、time、datetime、calendar |
| **类型** | option、traits、base、error |
| **运行时** | vo、prefix、task、format、logging、testing、i18n |
| **GUI** | gui（Nuklear 绑定） |
| **Freestanding** | freestanding.base、freestanding.memory、freestanding.string、freestanding.math、freestanding.io（无依赖，弱符号，裸机/托管双环境） |

---

## 编译器

### 安装

```bash
python toolkit_build.py                     # Debug 构建 (默认)
python toolkit_build.py --release           # Release 构建
python toolkit_build.py --target compiler   # 只构建编译器
python toolkit_build.py --target freestanding # 只构建 freestanding 无依赖标准库
python toolkit_build.py --cc gcc            # 指定使用 gcc 而非默认的 clang
python toolkit_build.py --clean             # 清理所有构建产物
python toolkit_build.py --install-dir D:/kaula  # 自定义输出目录
```

依赖：Python 3.8+、Go 1.21+、Clang

该脚本一次性构建：标准库 (kaula_std)、freestanding 库 (kaula_freestanding)、运行时 (kaula_runtime)、编译器 (kaulac)、格式化工具 (kaulafmt)。基于 SHA256 增量缓存，未变更的源文件秒级跳过。

### 编译流程

```
源代码 .kl → 词法分析 → 语法分析 → 语义分析 → C 代码生成 → Clang 编译 → 可执行文件
```

支持多阶段并发编译和基于 SHA256 的增量缓存。

### 用法

```bash
kaulac [选项] <源文件.kl>

kaulac program.kl              # 编译
kaulac --sor program.kl        # 启用 SOR 所有权分析
kaulac --release program.kl    # Release 模式 (-O3)
kaulac --freestanding program.kl  # 裸机模式
kaulac --sourcemap program.kl  # 生成源码映射
kaulac --no-cache program.kl   # 禁用缓存
```

常用选项：

| 选项 | 说明 |
|------|------|
| `--sor` | 启用 SOR 编译时所有权分析 |
| `--release` | Release 模式（-O3） |
| `--freestanding` | 裸机模式（无 OS 依赖，链接 freestanding 库） |
| `--opt <level>` | 优化级别 O0/O1/O2/O3 |
| `--sourcemap` | 生成源码映射 |
| `--no-cache` | 禁用增量编译 |
| `--output-format <fmt>` | 输出格式 elf/bin/obj |

完整选项列表和 kaula.json 配置见 [docs/](docs/)。

---

## 项目结构

```
kaula/
├── kaula-compiler/          # 编译器（Go 实现）
│   ├── cmd/kaulac/          # 编译器 CLI
│   ├── internal/
│   │   ├── lexer/           # 词法分析
│   │   ├── parser/          # 语法分析（递归下降）
│   │   ├── sema/            # 语义分析（两遍、泛型、SOR）
│   │   ├── codegen/         # C 代码生成
│   │   ├── sor/             # SOR 所有权分析引擎
│   │   ├── symbol/          # 符号表
│   │   ├── stdlib/          # 标准库配置
│   │   └── ...
│   └── stdlib.json
├── src/                     # 运行时系统（C 实现）
│   ├── kaula.h              # 跨平台头文件
│   ├── kmm_scoped_allocator_v4.h  # KMM V4 内存管理
│   └── ...
├── std/                     # 标准库（C 实现，62 个模块）
├── pkglib/                  # 第三方库（stb_image, nuklear, zlib）
└── docs/                    # 编译器详细文档
```

---

## 许可证

[Apache License 2.0](LICENSE)，附带 [Kaula Exceptions](LICENSE#kaula-exceptions-to-the-apache-20-license)
