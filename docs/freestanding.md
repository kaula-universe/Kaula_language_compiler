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

**不带 `--boot`**（`kaulac --freestanding prog.kl`）：clang 一次链接。编译器会
**单独把 `src/kaula_freestanding_runtime.c` 预编译成 `.o`** 再链进去，而不是把
它和生成的 C 放进同一次调用。原因：runtime 用 `#include "memory/memory.c"` 这种
unity 短路径包含本库，而生成代码那次调用的 `-I` 顺序是 `src → std → freestanding`，
短路径会先命中 `std/memory/memory.c`（依赖 libc `<string.h>`，`-nostdlib` 下直接失败）。
单独编译时 `-I freestanding` 排在 `std` 之前，路径唯一。

该 runtime 是 5 个模块的 unity build，**已经覆盖全部 `fs_*` 符号**，所以此时
`cache/std-objects/fs_*.o` 会被跳过（否则 duplicate symbol）。同时链接
`kmm_v4_fs.o`（静态池版 KMM 分配器，与托管版 `kmm_v4.o` 是不同产物，用 `_fs`
后缀区分缓存路径）。

在 Windows 宿主上链接目标仍是宿主 ABI（COFF），此时会额外传
`-Wl,/subsystem:console -Wl,/entry:_kaula_entry`（lld-link 缺这两个参数会直接
报 `subsystem must be defined` / `undefined symbol: _kaula_entry`）。要拿到真正
的裸机 ELF，用带 `--boot` 的流水线。

**带 `--boot`**（`kaulac --freestanding --boot pvh --boot-arch x86_64`）：
分别编译 kernel C / boot stub / freestanding runtime / KMM 分配器，再用
`ld.lld -T <linker script>` 链接出可引导 ELF。中间产物按 `arch-triple` 分目录
（`cache/boot-<arch>-<triple>/`）——不同架构的 `.o` 机器类型不同，共用固定文件名
会在并行构建时互相覆盖。

`--freestanding` 下自动添加的 clang 参数：

| 参数 | 作用 |
|------|------|
| `-ffreestanding` / `-nostdlib` / `-nostartfiles` | 不依赖 libc，不链接 CRT 与启动文件 |
| `-DKAULA_FREESTANDING` | 双环境头文件走裸机分支 |
| `-DKMM_V4_STATIC_POOL` | KMM 分配器用 BSS 静态池（**所有 TU 必须一致**） |
| `-fno-pic` + 代码模型 | boot 路径：x86_64/aarch64 `-mcmodel=large`，riscv64 `medany` |
| `-march/-mabi`（riscv64 缺省） | 见下 |

riscv64 缺省补 `-march=rv64gc -mabi=lp64d`：clang 的 riscv64 默认是
rv64imac + 软浮点 ABI，f64 运算会 lower 成 `__adddf3` / `__ledf2` / `__fixdfsi`
等 libcall，裸机没有 compiler-rt。固定硬件浮点后零 libcall。自己传了
`-march=` / `-mabi=` 就完全不干预。

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

全链路 6 层一次性跑完，共 **100 项检查**：

```bash
python scripts/run_freestanding_tests.py                 # 全量
python scripts/run_freestanding_tests.py --only L1,L2    # 指定层
python scripts/run_freestanding_tests.py --list          # 列出各层
python scripts/run_freestanding_tests.py -v              # 打印每项明细
python scripts/run_freestanding_tests.py --only L5 --quick --filter stress
```

| 层 | 内容 | 项数 | 判据 |
|----|------|------|------|
| **L1** | C 层全函数台账 + printf 一致性 | 2 | 122 函数 / 1644 断言（freestanding）；107 函数 / 1545 断言（托管，双环境）；1028 项与 libc `printf` 逐字节比对 |
| **L2** | 交叉编译零依赖 | 20 | 4 架构各自：编译通过、未定义符号审计（只允许白名单 libcall）、引用确实是本库对象、`-nostdlib` 独立 `ld.lld` 出 ELF、ELF 头合法 |
| **L3** | Kaula 层全函数 | 6 | `tests/freestanding/kl/*.kl` 托管模式**编译并运行**（校验行为 + 退出码），`--freestanding` 模式编译链接 |
| **L4** | boot 全链路 | 6 | 5 种引导配置出可引导 ELF + 静态校验（机器类型、LOAD 段、零未定义符号） |
| **L5** | 语言全功能 | 53 | `test/*.kl` 全量语料逐个 `--freestanding`：前端 + 代码生成 + C 编译必须全过 |
| **L6** | 配置一致性 | 15 | `compiler/stdlib.json` ↔ 头文件 `extern` ↔ 归档符号三方一致，依赖声明对齐 |

源码位置：

- `tests/freestanding/c/` — 单 TU C 测试台账（`fs_test.h` 里是逐函数清单）
- `tests/freestanding/kl/` — Kaula 层测试（5 个模块各一个 + `boot_kernel.kl`）
- 产物全部落在 `build/fs-test/`（gitignored）

### L5 的判据为什么分两段

语料库里的程序大量使用 `std.concurrent`（线程/信号量）、`std.time`（时钟）、
`std.string`、`std.math`、`std.testing` —— 这些在裸机**没有实现**，链接期必然缺符号。
这是预期内的（裸机没有 libc），不是 freestanding 的缺陷。

所以 L5 的**闸门是"生成 + C 编译"必须 100% 通过**：这才是语言特性在 freestanding
代码生成路径下是否完好的判据。链接结果另外统计：当前 53 个程序里 10 个能完整链接
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
- **Windows 宿主上的非 boot `--freestanding` 产出 PE 而非 ELF**。这条链路的意义
  是"用 freestanding 库把程序编链接通"，产物不可引导；要可引导 ELF 请用 `--boot`。
- **32 位目标的 8 字节原子**。i386 上 `uint64_t` 自然对齐只有 4 字节，clang 不敢
  内联 `cmpxchg8b`，会 lower 成 `__atomic_load` / `__atomic_compare_exchange`
  libcall（裸机没有 libatomic）。`kmm_slab_bucket_t.bitmap` 因此显式标注
  `KMM_V4_ALIGN64`（`__attribute__((aligned(8)))`），标注后全部内联。
- **`cache/std-objects/` 不按目标三元组分键**。用同一份缓存目录交替交叉编译多个
  目标时，模块 `.o` 可能被复用。跨目标并行构建请分开的 `KAULA_HOME`/工作目录，
  或清 `--clean-cache`。boot 路径不受影响（中间产物已按 arch 分目录）。

---

## 5. QEMU 验证（需自行准备 QEMU）

本机无 QEMU，boot 链路的验证是**静态**的（ELF 头、机器类型、LOAD 段、零未定义
符号）。有 QEMU 时可直接跑：

```bash
# x86_64 PVH（内核加载到 0x100000）
qemu-system-x86_64 -machine q35,accel=tcg -m 512M \
    -kernel build/fs-test/boot/boot-x86_64-pvh/boot_kernel.elf -nographic -no-reboot

# i386 Multiboot
qemu-system-i386 -m 512M \
    -kernel build/fs-test/boot/boot-i386-multiboot/boot_kernel.elf -nographic

# riscv64 PVH
qemu-system-riscv64 -machine virt -bios none -m 512M \
    -kernel build/fs-test/boot/boot-riscv64-pvh/boot_kernel.elf -nographic

# aarch64 PVH
qemu-system-aarch64 -machine virt -bios none -m 512M \
    -kernel build/fs-test/boot/boot-aarch64-pvh/boot_kernel.elf -nographic
```

串口输出示例见 [`bare-metal.md`](bare-metal.md) 的 `hello_serial.kl`（不同架构
UART 地址不同，测试内核 `boot_kernel.kl` 不直接访问串口，而是把自检结果作为
`kaula_main` 返回值：0 = 全通过，便于人工/脚本核对）。
