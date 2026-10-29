# freestanding 库与全链路验证

本文是 `freestanding/`（零依赖标准库）的参考文档 + 验证说明。
引导流程（boot stub、链接脚本、代码模型）见 [`bare-metal.md`](bare-metal.md)；
模块登记与依赖声明见 [`stdlib-integration.md`](stdlib-integration.md)。

---

## 1. 这是什么

`freestanding/` 是一套**不依赖 libc、不依赖 OS** 的 C 实现：5 个模块、122 个函数，
在 `-ffreestanding -nostdlib` 下可以直接编译链接，跨 x86_64 / i386 / riscv64 /
aarch64 四个架构零外部符号。

| 模块 | 头文件 | 内容 |
|------|--------|------|
| `freestanding.base` | `base/types.h` | 饱和转换 `to_i8`..`to_u64`、比较 `compare_*`、类型判定 `is_*` |
| `freestanding.memory` | `memory/memory.c` | `memset/memcpy/memmove/memcmp` + BSS 静态池 `fs_alloc` 家族 + 64 位除法 libcall |
| `freestanding.string` | `string/string.h` | `strlen/strcmp/str*` 家族 + `is_*`/`to_upper`/`to_lower` + `fs_itoa/fs_atoi/fs_strdup` |
| `freestanding.math` | `math/math.h` | `math_floor/ceil/trunc` 等 f64 变换、整数工具 `clz/ctz/popcount/gcd`、伪随机 |
| `freestanding.io` | `io/io.h` | printf 兼容格式化引擎（`%d %s %x %e %g %p %#-+0 宽度.精度`）+ 弱钩子 `fs_output_putchar` |

设计约束：

- **弱符号（`FS_WEAK`）**：所有实现都标 `__attribute__((weak))`。托管模式下与
  libc / `kaula_runtime` / `std` 的同名强符号共存时强符号优先，静态库里不会被
  误提取出来遮蔽 libc；裸机下没有其他强符号，弱符号正常生效。
- **零系统调用**：内存来自 BSS 静态池（`KMM_V4_STATIC_POOL`），输出走弱钩子。
- **无 freestanding libc**：`std.*` 的线程/时钟/文件/socket/并行模块在裸机下
  没有实现（见 §5 已知边界）。

### 调用约定（重要）

库函数必须写**模块限定名**：

```kaula
import freestanding.memory

char* buf = as<char*>(freestanding.memory.fs_alloc(64))
freestanding.memory.memset(buf, 65, 8)
freestanding.memory.fs_free(buf)
```

裸写 `fs_alloc(64)` 目前不会解析：`import` 只引入头文件与链接单元，不把函数名
注入当前命名空间。这是**语言级限制**（所有 `std.*` 模块都一样），不是本库特有。
只有内建（`println` / `print` / `assert`）可以裸调用。

Kaula 的 `string` 是 `{len, sso/ptr}` 结构体，与 C 的 `char*` 不同：

- 传给 C 接口要显式取地址：`as<char*>(&numbuf[0])`；字符串实参会由代码生成器
  自动经 `string_cdata()` 解包（printf 风格变参同样处理）。
- `size_t` / `int` 返回的 C 函数在 Kaula 侧要用 `as<u64>` / `as<i32>` 桥接。
- 指针运算用 `&arr[n]`，不能写 C 的 `p + n`。

---

## 2. 链接模型

`--freestanding` 有两条流水线，产物不同：

**不带 `--boot`**（`kaulac --freestanding prog.kl`）：目标三元组按 boot 架构推导
（默认 `<arch>-none-elf`），产物是**真正的裸机 ELF**。链接分两步：clang 只负责
`-c` 编成 `.o`，再由 `ld.lld` 直连 —— 不能走 clang 默认链路，交叉到 `*-none-elf`
时 clang 会挑 gcc 当链接驱动（`-###` 里是 `"gcc" ... -fuse-ld=lld`），宿主机上
没有交叉 gcc 就直接 `unable to execute command: program not executable`。

编译器会**单独把 `src/kaula_freestanding_runtime.c` 预编译成 `.o`** 再链，而不是
和生成的 C 放进同一次调用。原因：runtime 用 `#include "memory/memory.c"` 这种
unity 短路径包含本库，而生成代码那次调用的 `-I` 顺序是 `src → std → freestanding`，
短路径会先命中 `std/memory/memory.c`（依赖 libc `<string.h>`，`-nostdlib` 下直接失败）。
单独编译时 `-I freestanding` 排在 `std` 之前，路径唯一。

该 runtime 是 5 个模块的 unity build，**已经覆盖全部 `fs_*` 符号**，所以此时
`cache/std-objects/fs_*.o` 会被跳过（否则 duplicate symbol）。同时链接
`kmm_v4_fs.o`（静态池版 KMM 分配器，与托管版 `kmm_v4.o` 是不同产物，用 `_fs`
后缀区分缓存路径）。**托管模块（`std.*`）在裸机模式下一律不编译不链接** ——
它们是托管代码（`std/concurrent` 要 `<sys/types.h>`、`std/web` 要 socket 头），
交叉目标没有宿主 SDK，编出来也是一堆 libc 调用。freestanding 库本身由 runtime
这个 unity TU 一次编译全部 5 个模块，编译校验不受影响。

**带 `--boot`**（`kaulac --freestanding --boot pvh --boot-arch x86_64`）：
分别编译 kernel C / boot stub / freestanding runtime / KMM 分配器，再用
`ld.lld -T <linker script>` 链接出可引导 ELF。中间产物按
`arch-triple-引导方式` 分目录（`cache/boot-<arch>-<triple>-<boot>/`）——不同架构
的 `.o` 机器类型不同，x86_64 的 pvh 与 multiboot 用的 boot stub 也不同
（`.note.Xen` vs `.multiboot` 头），共用固定文件名会在并行构建时互相覆盖
（症状是"多架构并行构建时随机的 undefined symbol / 缺引导头"）。

缓存目录**按目标指纹分键**（`cache/std-objects/fs-<triple>-<opt>-<hash>/`）：
`.o` 的机器类型 / 浮点 ABI / 宏集全由目标决定，共用一个目录时交叉编译会静默
复用上一个目标的产物，报错要等到链接期才以 `xxx.o is incompatible with elf_i386`
的形式出现。所有 `.o` 都"先写临时文件再 rename"落盘，并行构建（CI 多任务 /
`make -j`）不会读到写了一半的文件。

`--freestanding` 下自动添加的 clang 参数：

| 参数 | 作用 |
|------|------|
| `-ffreestanding` / `-nostdlib` / `-nostartfiles` | 不依赖 libc，不链接 CRT 与启动文件 |
| `-DKAULA_FREESTANDING` | 双环境头文件走裸机分支 |
| `-DKMM_V4_STATIC_POOL` | KMM 分配器用 BSS 静态池（**所有 TU 必须一致**） |
| `-target <arch>-none-elf` | 未显式指定时按 boot 架构推导，保证产出裸机 ELF |
| `-fno-pic` + 代码模型 | x86_64/aarch64 `-mcmodel=large`，riscv64 `medany` |
| `-march/-mabi`（riscv64 缺省） | 见下 |
| `-I freestanding/include` | 裸机 libc 头替身，见下 |

riscv64 缺省补 `-march=rv64gc -mabi=lp64d`：clang 的 riscv64 默认是
rv64imac + 软浮点 ABI，f64 运算会 lower 成 `__adddf3` / `__ledf2` / `__fixdfsi`
等 libcall，裸机没有 compiler-rt。固定硬件浮点后零 libcall。用户传了
`-march=` / `-mabi=` 就完全不干预。

### 裸机 libc 头替身（`freestanding/include/`）

`std.*` 的头文件会 include `<stdio.h>` / `<sys/types.h>` / `<string.h>` 等，而交叉
目标没有宿主 SDK，这些头根本不存在（53 个语料程序里 28 个撞上 `'stdio.h' file
not found`）。`freestanding/include/` 放的是**只声明不实现**的替身头：
`stdio.h`、`stdlib.h`、`string.h`、`math.h`、`time.h`、`errno.h`、`ctype.h`、
`assert.h`、`setjmp.h`、`unistd.h`、`fcntl.h`、`sys/types.h`、`sys/time.h`、
`sys/socket.h`、`netinet/in.h`、`netdb.h`、`arpa/inet.h`。

它们只保证"头能解析"，函数一律保持未定义 —— 链接期缺符号就是"裸机没有这个能力"
的如实反馈（例如 `fopen` / `socket` / `time_now_ms`），不会由替身头伪造出可用实现。
该目录只在 `--freestanding` 下加入 `-I`（`-I` 永远先于系统目录，能压过宿主 SDK），
托管模式完全不受影响。

### 入口符号

裸机入口模板（`compiler/templates/freestanding.c.tmpl`）生成 `_kaula_entry`，
它调用 `K_kaula_main`。两种写法都落到同一个 C 符号：

| Kaula 源码 | C 符号 |
|-----------|--------|
| `fn main()` | `K_kaula_main`（非 main 的用户函数统一加 `K_` 前缀） |
| `fn kaula_main()` | `K_kaula_main`（同上） |

`--boot user` 模式用 `user.c.tmpl`，入口是 `user_start`，行为一致。

---

## 3. 验证：`scripts/run_freestanding_tests.py`

全链路 7 层一次性跑完：

```bash
python scripts/run_freestanding_tests.py                 # 全量
python scripts/run_freestanding_tests.py --only L1,L2    # 指定层
python scripts/run_freestanding_tests.py --list          # 列出各层
python scripts/run_freestanding_tests.py -v              # 打印每项明细
python scripts/run_freestanding_tests.py --only L5 --quick --filter stress
python scripts/run_freestanding_tests.py --only L7 --qemu-timeout 120   # 调 QEMU 时限
```

| 层 | 内容 | 项数 | 判据 |
|----|------|------|------|
| **L1** | C 层全函数台账 + printf 一致性 | 2 | 122 函数 / 1644 断言（freestanding）；107 函数 / 1545 断言（托管，双环境）；1028 项与 libc `printf` 逐字节比对 |
| **L2** | 交叉编译零依赖 | 20 | 4 架构各自：编译通过、未定义符号审计（只允许白名单 libcall）、引用确实是本库对象、`-nostdlib` 独立 `ld.lld` 出 ELF、ELF 头合法 |
| **L3** | Kaula 层全函数 | 6 | `test/freestanding/kl/*.kl` 托管模式**编译并运行**（校验行为 + 退出码），`--freestanding` 模式编译链接出裸机 ELF |
| **L4** | boot 全链路 | 6 | 5 种引导配置出可引导 ELF + 静态校验（机器类型、LOAD 段、零未定义符号） |
| **L5** | 语言全功能 | 66 | `test/*.kl` 全量语料（排除 `HOSTED_ONLY_TESTS` 两个托管用例）逐个 `--freestanding`：前端 + 代码生成 + C 编译必须全过 |
| **L6** | 配置一致性 | 15 | `compiler/stdlib.json` ↔ 头文件 `extern` ↔ 归档符号三方一致，依赖声明对齐 |
| **L7** | QEMU 实机引导 | 4+3 | 4 架构真机启动 + 串口断言 `SELFTEST-RESULT=0`；3 项为已定位未修的 boot stub 缺口（见 §6） |

当前：**114 项全过 + 2 项已知缺口**（含 L7 四架构实机引导）。L7 需要本机装 QEMU（缺省会整层跳过并提示）。

源码位置：

- `tests/freestanding/c/` — 单 TU C 测试台账（`fs_test.h` 里是逐函数清单）
  + `arch_uart.c`（按架构的串口钩子，L7 用）+ `uart_probe.c` / `bss_probe.c`
  （最小探针，用来把"串口/映射不对"与"内核没跑到"分开）
- `tests/freestanding/kl/` — Kaula 层测试（5 个模块各一个 + `boot_kernel.kl`）
- 产物全部落在 `build/fs-test/`（gitignored）

### L5 的判据为什么分两段

语料库里的程序大量使用 `std.concurrent`（线程/信号量）、`std.time`（时钟）、
`std.string`、`std.math`、`std.testing` —— 这些在裸机**没有实现**，链接期必然缺符号。
这是预期内的（裸机没有 libc），不是 freestanding 的缺陷。

所以 L5 的**闸门是"生成 + C 编译"必须 100% 通过**：这才是语言特性在 freestanding
代码生成路径下是否完好的判据。链接结果另外统计：当前 66 个程序里 18 个能完整链接
（只用 freestanding 能力的那些），其余记为 `compile-only` 并列出缺失的 OS 依赖符号；
只要缺失符号落在 `OS_ONLY_SYMBOLS` 白名单之外，就判为**真 bug**。

---

## 4. 已知边界

- **`print` / `printf` 一致性验证限 15 位有效十进制数字**。`f64` 精度是
  15.95 位，16 位以后 libc 与本库实现会分道扬镳；这属于浮点表示本身的边界，
  不是实现缺陷。
- **`strcasecmp` / `strncasecmp` 在托管模式不定义**。它们与 POSIX（glibc/musl）
  语义一致，但 MSVC UCRT 没有这两个符号，定义了就与强符号冲突。托管模式用
  libc 的 `_stricmp`，裸机用本库实现。
- **32 位目标的 8 字节原子**。i386 上 `uint64_t` 自然对齐只有 4 字节，clang 不敢
  内联 `cmpxchg8b`，会 lower 成 `__atomic_load` / `__atomic_compare_exchange`
  libcall（裸机没有 libatomic）。`kmm_slab_bucket_t.bitmap` 因此显式标注
  `KMM_V4_ALIGN64`（`__attribute__((aligned(8)))`），标注后全部内联。
- **`std.*` 在裸机只有声明没有实现**。见 §3 的 L5 判据说明：线程、时钟、socket、
  文件、`std.string` / `std.math` / `std.testing` 等托管模块的符号在链接期保持未定义，
  这就是"裸机没有这个能力"的如实反馈。`std.memory` 的分配器转发（`kmm_v4_alloc`
  等）已由 freestanding runtime 补齐。
- **spend 强制消费流与极端并发压测不参与裸机编译（L5 排除）**。
  `spend_and_call` 依赖托管运行时 `src/spend_call.c`（裸机无
  `Spendable`/`spendable_create` 符号）；`stress_concurrent_spawn` 的
  50000 轮高并发 spawn 依赖宿主 pthread 内核（`kaula_conc_platform.h`
  在 `none-elf` 交叉目标下把 glibc 头拉进 include 链）。两者在
  `scripts/run_freestanding_tests.py` 的 `HOSTED_ONLY_TESTS` 中说明跳过；
  轻量 conc 用例（`full_concurrency` / `conc_paths`）仍全量覆盖裸机编译。
- **`#[derive(Debug)]` 在裸机用 `print` 而非 `printf`**。托管模式生成
  `printf`（libc），freestanding 模式生成 `freestanding.io` 的 `print`
  （变参、`%d/%s/%f` 同语义）——两者格式串完全一致，裸机链接期不出现
  未定义符号 `printf`。

---

## 5. QEMU 实机验证

L7 会真的把内核跑起来：把按架构实现的串口钩子（`tests/freestanding/c/arch_uart.c`，
实现弱符号 `fs_output_putchar`）额外链进 boot ELF，然后在 QEMU 里启动，断言串口
出现 `SELFTEST-RESULT=0`。内核跑完停在 `hlt` / `wfi` 死循环（本来就不退出），
由超时终止 —— 判据是串口内容，不是进程退出码。

需要本机装有 QEMU（缺省会整层跳过并提示）。手工复现：

```bash
# x86_64 multiboot（已验证可引导）
qemu-system-x86_64 -machine q35 -m 512M -display none -no-reboot -serial stdio \
    -kernel build/fs-test/boot/boot-x86_64-multiboot/qemu.elf

# i386 multiboot（已验证可引导）
qemu-system-i386 -m 512M -display none -no-reboot -serial stdio \
    -kernel build/fs-test/boot/boot-i386-multiboot/qemu.elf

# aarch64（需要 -cpu cortex-a57：virt 板默认 CPU 是 32 位 c-a15，会拒绝加载）
qemu-system-aarch64 -machine virt -cpu cortex-a57 -m 512M -display none \
    -no-reboot -serial stdio -kernel build/fs-test/boot/boot-aarch64-pvh/qemu.elf

# riscv64（-bios none 让 QEMU 直接从 -kernel 加载点开跑，不走 OpenSBI 约定）
qemu-system-riscv64 -machine virt -bios none -m 512M -display none \
    -no-reboot -serial stdio -kernel build/fs-test/boot/boot-riscv64-pvh/qemu.elf
```

各架构 UART 地址（与 `tests/freestanding/c/arch_uart.c` 一致）：x86 COM1 `0x3F8`、
riscv64 virt NS16550 `0x10000000`、aarch64 virt PL011 `0x09000000`。
`tests/freestanding/c/uart_probe.c` 是一个只打印一行字的最小程序，用来单独验证
"钩子实现 / 串口地址"这一件事（把"串口不对"和"内核没跑到 io 段"分开）。

## 6. 已知缺口（已定位，未修）

这三项 L4 静态校验全过（ELF 头、机器类型、LOAD 段、零未定义符号），L7 实机
引导被卡住。runner 把它们记为 `NOTE`（不计入失败，但始终打印），修好后从
`KNOWN_BOOT_GAPS` 里挪走就会变成硬失败盯着。

| 配置 | 现象 | 根因 | 修复方向 |
|------|------|------|----------|
| `boot-x86_64-pvh` | QEMU 报错消失、镜像被加载，但 SeaBIOS 最后 `jmp 0x0` | QEMU 的 x86 `-kernel` 走 BIOS 路径，没把控制权交给 PVH note 里的 32 位入口；另外 Xen PVH 规范要求入口 2MB 对齐，当前入口在 1M+x | 给 pvh 单独一份 2MB 对齐的链接脚本，让 `_start` 落在加载基址 |
| `boot-riscv64-pvh` | 内核已进入 `K_kaula_main`，但无串口输出 | QEMU 只为已加载镜像建映射，清零超出镜像的 BSS（KMM 静态池 ~18MB）取页故障；boot stub 未建 `satp` 恒等映射 | `riscv64.S` 里加 Sv39 `satp` 恒等映射（与 x86_64 stub 建页表同理） |
| `boot-aarch64-pvh` | `-d int` 显示在 `__bss_start` 处 Data Abort（ESR 0x25，翻译故障），随后按 `VBAR_EL1=0x200` 跳进向量表跑飞 | 同 riscv64：boot stub 未建 `TTBR0` 恒等映射，也没设 `VBAR_EL1`（UART 钩子本身在同一台机器上验证过是可用的） | `aarch64.S` 里加 `TCR_EL1`/`MAIR_EL1` + L0 1GB 粒度恒等映射，并设 `VBAR_EL1` |

顺带记录两个已被 QEMU 实机验证抓出来的真 bug（已修，值得留档，因为静态校验
完全看不出它们）：

- **i386 boot stub 漏开 SSE**。x86_64 的 stub 一直有 `CR4.OSFXSR|OSXMMEXCPT`，
  i386 的没有；而 clang 默认用 SSE 指令做 f64（`print_float` / `fs_itoa` /
  `math_*` 一碰就是），第一条 SSE 指令就 `#UD`，内核当场死掉且**没有任何输出**，
  现象是"QEMU 静默退出"。
- **x86_64 链接脚本没把 PVH note 放进 `PT_NOTE`**。QEMU 只扫程序头找
  `XEN_ELFNOTE_PHYS32_ENTRY`，只看 section 一定失败，报
  `Error loading uncompressed kernel without PVH ELF Note`。修法不只是加
  `PHDRS`：只声明 PHDRS 而不把段挂上去（`:note`），lld 会生成一个
  `Offset=0/FileSize=0` 的空 PT_NOTE —— 头有了内容没有，照样失败。
