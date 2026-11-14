# 在 XJ380 OS 上运行最简 Kaula 程序

本目录提供**两条**可复现、已验证的链路，把一个最小 Kaula 程序编译成 XJ380 用户态
静态 ELF，写入 UEFI 可启动镜像，并在 QEMU 中实际运行：

| 路线 | 输入 | 是否需要 C 适配层 | 串口标记 |
| --- | --- | --- | --- |
| **A. shim 版** | `hello.kl` | 需要 `xj380_shim.c` | `KAULA-XJ380-HELLO` |
| **B. 纯 Kaula 直调 XAPI（推荐）** | `hello_xapi.kl` | **不需要**，纯 `.kl` | `KAULA-XAPI-DIRECT` |

> 状态：已在 Ubuntu 24.04 / clang 18 / QEMU 8.2.2 / OVMF 4M 上跑通。
> 两条路线均以 `sys_exit: Thread shell exit with code 0.` 干净退出。

## 1. 原理

Kaula 是「编译到 C，再由 Clang 生成原生代码」的语言，其 `--freestanding` 模式产出
无 libc 依赖的目标文件。XJ380 的用户态是加载地址 `0x200000` 的静态 ELF，入口链为
`_start`（crt0.S）→ `xapi_start`（constart.cpp）→ `main`，输出走
`enter_syscall(XAPI_OUTPUT, ...)`（7381）。

### 路线 A：shim 版

`freestanding.io.println` 最终调用弱符号 `fs_output_putchar`。`xj380_shim.c` 提供
**强符号**覆写，把字符按行缓冲后交给 `XAPI_OUTPUT`，并提供 `main` 转调 `kaula_main`。
反汇编确认该弱钩子通过重定位间接调用、未被内联，覆写有效。

| 环节 | 负责方 |
| --- | --- |
| `kaula_main`、freestanding 运行库 | `kaulac --freestanding -output-format obj` |
| `_start` / `xapi_start` / `enter_syscall` | XJ380 的 `out/xapi/*.o` |
| `main` 转调、覆写输出钩子 | `xj380_shim.c` |
| 链接成 `0x200000` 静态 ELF | `ld -Ttext=0x200000` |
| 写入镜像 `/apps/system/shell/shell.elf` | XJ380 的 `XJ380_USER_APP` 覆盖点 |

### 路线 B：纯 Kaula 直调 XAPI（无 shim）

编译到 C 时 Kaula 的 `string` 是 `String{len, sso/ptr}` 结构体，而 XAPI 需要
`const char*`。桥接不靠 C 代码，靠语言与链接器：

```kaula
type CStr = adopt const char*                 // 零成本套用类型，C 侧 typedef const char* CStr
extern fn enter_syscall(n: u64, p: CStr, ...) -> u64

fn main() -> i32 {
    auto msg = "KAULA-XAPI-DIRECT\n"
    enter_syscall(7381, msg.ptr, 0, 0, 0, 0, 0)   // msg.ptr → string_cdata(msg) → const char*
    return 0
}
```

- `CStr = adopt const char*` **只声明参数类型，不做值转换**；真正把 `String` 变成
  C 字符串指针的是 `msg.ptr`——codegen 对 `String.ptr` 做 SSO 感知改写，生成
  `string_cdata(s)`（短串读 `.sso`，长串读 `.ptr`）。
- shim 的 `main()` 转调改由链接器 `--defsym main=kaula_main` 承担（freestanding
  模板把 `fn main` 生成为 `kaula_main`，而 XJ380 的 `xapi_start` 调用 `main`）。
- `-boot none` 让 Kaula 不自己链接引导（模板里的 `_kaula_entry` 成为不冲突的死代码，
  XJ380 用自己的 `_start`）；`-output-format obj` 让我们接管链接。

> **依赖编译器修复**：此前 freestanding 生成的 C 里 `String` 是「仅 `ptr`」的过时
> 兜底定义，且没有 `string_cdata` 宏，导致任何用到 Kaula string 的 freestanding
> 程序都编译失败（`field designator 'sso'` / `undeclared 'string_cdata'`），必须靠
> `-cflags "-include .../freestanding/base/types.h"` 绕过。现已在
> `compiler/templates/{freestanding,user}.c.tmpl` 与
> `compiler/internal/codegen/codegen.go` 原地修复，**现在无需任何额外 flags**。
> 详见第 6 节。

## 2. 前置条件

| 组件 | 版本 / 位置 |
| --- | --- |
| Kaula 编译器 | 本仓库 `build/bin/kaulac`（`cd compiler && go build -o ../build/bin/kaulac ./cmd/kaulac/`），需含 freestanding SSO 修复 |
| freestanding 运行库 | `cache/std-objects/fs-*/` 下的 `.o`（首次编译自动生成） |
| XJ380 | `/workspace/OpenXJ380`，已执行 `ninja -f build.ninja all` 与 `vdisk` |
| 工具链 | clang、ld、ninja、QEMU、OVMF、mtools、gdisk、e2fsprogs |

XJ380 侧需要两个小改动（见仓库根目录的 `OpenXJ380-integration.patch`）：

1. `XJ380_USER_APP` 环境变量：允许把外部 ELF 作为默认用户程序，**未设置时行为不变**。
2. 4 MiB OVMF 走 pflash：`-bios` 无法加载 Ubuntu 24.04 的 `OVMF_CODE_4M.fd`，
   按固件大小自动切换为 `-drive if=pflash`。两处都只影响 QEMU 运行与镜像打包，
   不改变内核与默认构建产物。

## 3. 文件

| 文件 | 路线 | 说明 |
| --- | --- | --- |
| `hello.kl` | A | 最简程序：一行 `println`，返回 0 |
| `xj380_shim.c` | A | ABI 适配层（输出钩子 + `main` 转调） |
| `build.sh` | A | 一键编译 → 链接 → 打包镜像 |
| `test_xj380_shim.c` | A | 适配层主机端单元测试（14 项） |
| `hello_xapi.kl` | **B** | 纯 Kaula 直调 XAPI，无 C 适配层 |
| `build_xapi.sh` | **B** | 一键编译 → 零未定义符号自检 → 打包镜像 |
| `run_qemu_stress.sh` | A/B | 多轮 QEMU 引导压测，`MARKER` 选择路线 |

## 4. 一键构建与运行

```bash
# 路线 B（推荐，无 shim）
cd /workspace/kaula-src
./examples/xj380/build_xapi.sh

# 路线 A（shim 版对照）
./examples/xj380/build.sh

# 在 QEMU 中启动，抓串口日志
cd /workspace/OpenXJ380
DEBUG=0 KVM=0 SUDO=0 SMP=2 DISPLAY_BACKEND=none AUDIO_BACKEND=none \
    ninja -f build.ninja justrun
# 路线 B 期望看到：
#   KAULA-XAPI-DIRECT
#   sys_exit: Thread shell exit with code 0.
```

`build_xapi.sh` 的关键阶段：

```bash
kaulac --freestanding -boot none -output-format obj -output-dir "$OUT" hello_xapi.kl
ld -Ttext=0x200000 --defsym main=kaula_main \
   out/xapi/... hello_xapi.elf fs_io_io.o fs_base_types.o \
   kaula_fs_runtime.o kmm_v4_fs.o -o shell_xapi.elf
nm -u shell_xapi.elf   # 必须为 0 个未定义符号
XJ380_USER_APP=shell_xapi.elf ninja -f build.ninja vdisk
```

> 注意：`-output-format obj` 的产物文件名仍是 `*.elf`，但内容是
> **ELF64 可重定位目标文件**（`file`/`nm` 可确认），不是可执行文件。
> `kmm_v4_fs.o` 提供 `g_kmm_v4_*` 与 `kmm_v4_tlab_restore_to`；官方
> `libkaula_freestanding.a` **不含**这些符号，不能替代该 `.o`。

## 5. 测试、压测与回归

```bash
# 路线 A 适配层单元测试（含超长行、容量边界、返回码透传）
clang -std=c11 -O1 -Wall -Wextra -Werror -I examples/xj380 \
    -o /tmp/test_shim examples/xj380/test_xj380_shim.c && /tmp/test_shim

# 压测：MARKER 选择路线（不设默认 KAULA-XJ380-HELLO）
ROUNDS=5 ./examples/xj380/run_qemu_stress.sh                          # A
MARKER=KAULA-XAPI-DIRECT ROUNDS=5 ./examples/xj380/run_qemu_stress.sh  # B

# Kaula 编译器回归
cd compiler && go test ./...
python3 scripts/run_freestanding_tests.py --only L1,L3,L6 --quick -j 8
```

实测结果（修复后全量重跑，含新增回归测试）：

| 项目 | 结果 |
| --- | --- |
| Go 全量单元测试 `go test ./...` | 全部 `ok`，**0 FAIL**（含新增 sema/codegen 回归） |
| Kaula freestanding L1/L3/L6 | **22/22 checks passed** |
| 适配层单元测试 | **14 checks / 0 failures** |
| QEMU 引导压测（两条路线） | **5/5 PASS**，输出 SHA256 全部相同，`distinct_outputs=1` |
| XJ380 单元测试 | 51 项 OK |
| XJ380 `check.tools` / `sgdisk --verify` / `verify_disk_image.py` | 全通过 |
| 纯 Kaula 产物 | 零未定义符号，串口输出 + exit code 0 |

> freestanding 套件首次运行有 2 项因缺少 `build/lib/libkaula_freestanding.a` 失败，
> 执行 `python3 toolkit_build.py --target freestanding` 补齐前置条件后 22/22 通过；
> 属于环境准备，非代码回归。

## 6. 已修复的缺陷

**编译器（freestanding SSO 自洽性，3 处原地修复）**

| # | 位置 | 缺陷 | 修复 |
| --- | --- | --- | --- |
| 1 | `compiler/internal/sema/sema.go` | `isCScalarType` 白名单漏了 `bool_t` / `char_t`（官方头里的标量 typedef），导致 `std/ctypes.kl` 的 `#[transparent] type BoolT = adopt bool_t` 报 `adopt_transparent_non_scalar`，`import std.ctypes` 无法开始编译 | 白名单补 `bool_t` / `char_t`，并保留指针拒绝（回归测试 `adopt_scalar_types_test.go`） |
| 2 | `compiler/templates/freestanding.c.tmpl`、`user.c.tmpl` | `String` 兜底定义是「仅 `ptr`」形态，但 codegen 按 SSO 形态产出 `.sso=` 字面量 → `field designator 'sso'` | 改为与官方头逐字一致的 SSO union（`char sso[16]`），并自带 SSO 感知 `string_data`/`string_cdata` |
| 3 | `compiler/internal/codegen/codegen.go` | `string_cdata` 兜底宏只写在非 freestanding 分支 → freestanding 下 `undeclared 'string_cdata'` | freestanding 分支补 SSO 感知兜底宏 |

三处均用 `#ifndef` 幂等保护（外部头/模板已定义则跳过，不产生宏重定义），
与 `std/base/types.h`、`freestanding/base/types.h` 逐字一致。新增回归测试：
`compiler/internal/sema/adopt_scalar_types_test.go`、
`compiler/internal/codegen/freestanding_sso_string_test.go`。

**示例适配层（上一轮）**：`xj380_shim.c` 初版在行缓冲满时**先丢弃新字符、再冲刷**，
导致超过 255 字节的行静默丢字节。改为「缓冲满则先冲刷、再写入当前字符」，并用
单元测试（600 字节超长行、恰好 255 字节 + 换行）锁住该边界。

## 7. 已知限制

- 只验证了字符串输出路径；文件 I/O、线程、动态内存、浮点格式化等在 XJ380 上的行为未覆盖。
- 该程序替换了默认 shell，因此验证镜像不提供 XJ380 CLI（仅用于跑通验证）。
- XJ380 源码包未附带 `Bf/` 静态资源，镜像按官方源码包内容构建。
- `--defsym main=kaula_main` 与 `-Ttext=0x200000` 属于两个生态的 ABI 适配，不是缺陷，
  编译器修复后仍需保留。
- `XAPI_OUTPUT` 不自动补换行，纯 Kaula 程序需在字符串里自带 `\n`。
- `import std.ctypes` 在**托管模式**已可编译；在 freestanding 下仍会因该模块引用
  宿主 C 类型（`ssize_t` / `bool_t` / `File`）而失败——这是 std/ctypes.kl 自身的既有
  限制，与本次 string 修复无关。路线 B 用本地 `type CStr = adopt const char*`，
  不 import 它，不受影响。
