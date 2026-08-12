# Kaula 构建与部署指南

本文档覆盖 Kaula 工具链的**构建**（从源码产出编译器、标准库、运行时）与**部署**（安装布局、程序分发、交叉编译与裸机部署）。

```
构建工具链 → 安装布局 (bin/include/lib) → kaulac 编译 .kl → 部署产物
```

## 目录

- [1. 环境要求](#1-环境要求)
- [2. 构建工具链 (toolkit_build.py)](#2-构建工具链-toolkit_buildpy)
- [3. 安装布局与部署](#3-安装布局与部署)
- [4. 编译与部署 Kaula 程序](#4-编译与部署-kaula-程序)
- [5. 交叉编译](#5-交叉编译)
- [6. 裸机/嵌入式部署](#6-裸机嵌入式部署)
- [7. CI 集成建议](#7-ci-集成建议)
- [8. 故障排查](#8-故障排查)

---

## 1. 环境要求

| 依赖 | 版本 | 用途 |
|------|------|------|
| Python | 3.8+ | 运行构建脚本 |
| Go | 1.21+ | 编译 kaulac / kaulafmt |
| Clang | 任意 14+（推荐） | 编译 C 运行时/标准库（可用 gcc 替代） |
| 归档工具 | Windows: `llvm-lib` 或 MSVC `lib.exe`；其他: `llvm-ar` / `gcc-ar` / `ar` | 打包静态库 |

跨平台行为一致，推荐统一使用 clang 工具链（LLVM 附带的 `llvm-*` 工具会被自动检测）。

## 2. 构建工具链 (toolkit_build.py)

单一构建入口，位于仓库根目录。一次构建全部组件：标准库、freestanding 库、运行时、编译器、格式化工具与头文件。

```bash
python toolkit_build.py                 # 全量构建（Debug，默认 clang）
python toolkit_build.py --release       # Release 模式（-O2 + NDEBUG）
python toolkit_build.py --cc gcc        # 指定 C 编译器
python toolkit_build.py --install-dir D:/kaula   # 自定义输出目录
python toolkit_build.py --clean         # 清理所有构建产物
```

### 参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `--release` | 关 | Release 模式（`-O2`、`-DNDEBUG`）；Debug 默认 `-g -O0 -DKMM_V4_DEBUG` |
| `--target <t>` | `all` | `all` / `std` / `freestanding` / `runtime` / `compiler` / `headers` |
| `--install-dir <dir>` | `build/` | 输出目录（内部结构固定为 `bin/ lib/ include/`） |
| `--cc <compiler>` | `clang` | C 编译器；不可用时依次回退 `gcc` → `cc` → MSVC `cl` |
| `--clean` | 关 | 删除全部构建产物后退出（不构建） |

`--target` 与产物对应关系：

| 目标 | 产物 |
|------|------|
| `all` | 全部（下方所有） |
| `std` | `lib/kaula_std.lib`（Windows）/ `lib/libkaula_std.a`（其他） |
| `freestanding` | `lib/kaula_freestanding.lib` / `lib/libkaula_freestanding.a` |
| `runtime` | `lib/kaula_runtime.lib` / `lib/libkaula_runtime.a`（src/ + compiler/runtime/） |
| `compiler` | `bin/kaulac(.exe)`、`bin/kaulafmt(.exe)`、`bin/stdlib.json` |
| `headers` | `include/{kaula,runtime,std,freestanding}`（仅复制头文件，跳过 `.c`） |

### 增量构建机制

基于 SHA256 指纹缓存，未变更的源文件秒级跳过：

- C 源码指纹 = `sha256(源文件内容 + 编译flag + include路径)`，缓存于 `build/hash/<rel_path>.sha256`
- Go 二进制指纹 = `sha256(cmd/<name>/*.go + internal/**/*.go + go.mod + release标志)`，各命令独立
- 中间对象文件位于 `build/obj/`，全部缓存命中时整轮构建可在数秒内完成

### 构建产物布局（默认 `build/`）

```
build/
├── bin/
│   ├── kaulac.exe          # 编译器 CLI
│   ├── kaulafmt.exe        # 格式化工具
│   └── stdlib.json         # 标准库模块注册表（编译器运行依赖）
├── include/
│   ├── kaula/              # 运行时头（kaula.h 等）
│   ├── runtime/            # SOR 运行时头
│   ├── std/                # 标准库头，按模块分目录
│   └── freestanding/       # 无依赖库头
└── lib/
    ├── kaula_std.lib       # 标准库静态库（Windows）
    ├── kaula_runtime.lib   # 运行时静态库（Windows）
    └── kaula_freestanding.lib  # freestanding 静态库（Windows）
    （其他平台对应 libkaula_*.a）
```

## 3. 安装布局与部署

### 3.1 两种运行形态

| 形态 | 触发条件 | 行为 |
|------|----------|------|
| **源码树模式**（开发） | 检测到仓库根（含 `src/kaula.h` 或 `include/kaula/kaula.h`），且 `lib/` 缺少静态库 | kaulac 从 `src/ std/ freestanding/` 按需增量编译模块 `.o`（缓存于 `<工作目录>/cache/std-objects/`），再链接 |
| **安装模式**（发布） | 同一根目录下存在 `include/kaula/kaula.h` + `lib/kaula_std.lib` + `lib/kaula_runtime.lib`（或 `.a`） | 直接链接预构建静态库，无需源码树与逐模块编译 |

### 3.2 部署工具链

`toolkit_build.py` 产出的 `build/` 目录本身就是完整的可分发工具链。部署步骤：

```bash
python toolkit_build.py --release
python toolkit_build.py --install-dir D:/kaula   # 或直接把 build/ 拷到目标目录
```

分发后目标机只需保留 `bin/ include/ lib/` 三个子目录（`obj/ hash/` 为构建中间产物，可丢弃）。kaulac 按以下顺序自动定位：

1. `KAULA_HOME` 环境变量
2. 可执行文件所在目录及其上级目录
3. 当前工作目录及其上级目录

```bash
# Windows (PowerShell)
$env:KAULA_HOME = "D:\kaula"
# Linux/macOS
export KAULA_HOME=/opt/kaula
```

设置 `KAULA_HOME` 后，编译器、`stdlib.json`、头文件、静态库均从该根目录解析，可在任意工作目录编译。

### 3.3 部署校验

```bash
kaulac --cache-stats          # 确认编译器可用
ls $KAULA_HOME/lib            # 确认三个静态库就位
# 试编译一个 Hello World 确认安装模式生效（日志出现 "Using installed static libraries"）
```

## 4. 编译与部署 Kaula 程序

### 4.1 托管程序（默认）

```bash
kaulac --release main.kl      # 产物与源文件同目录：main.exe / main
```

- 标准库与运行时均为**静态链接**（`kaula_std.lib` / `kaula_runtime.lib`），产物无外部运行时依赖
- Windows 平台自动附加 `ws2_32/wininet/gdi32/user32/advapi32`；其他平台自动附加 `-lm`
- **部署最终程序只需拷贝单个可执行文件**；目标机无需安装 Kaula 工具链
- 编译缓存位于 `<工作目录>/cache/`，发布构建可用 `--no-cache` 关闭

### 4.2 产物清理与缓存管理

```bash
kaulac --clean-cache    # 清理 7 天以上且超过 1GB 的过期缓存
kaulac --purge-cache    # 清空全部缓存
kaulac --no-cache main.kl
```

## 5. 交叉编译

Kaula 编译到 C，交叉编译 = 交叉编译 C 代码。通过 `--target-triple` 与 C 编译参数透传：

```bash
# 为 Linux x86_64 编译（Windows 宿主）
kaulac --cflags "--target=x86_64-unknown-linux-gnu" --libs pthread main.kl

# 提前构建好目标平台的标准库（在目标平台或借助交叉编译器运行 toolkit_build.py）
python toolkit_build.py --install-dir build-linux
```

> 注意：默认标准库静态库跟随宿主平台构建。交叉目标若平台/ABI 不同，需为各目标平台分别构建 `lib/`。

## 6. 裸机/嵌入式部署

### 6.1 编译模式

```bash
# Raw binary 内核（链接脚本 + 引导模板 + 指定入口）
kaulac --freestanding --target-triple x86_64-unknown-elf \
       --link-script linker.ld --entry _start \
       --output-format bin kernel.kl           # 产物 kernel.bin

# 可引导 ELF（PVH / Multiboot2 引导）
kaulac --freestanding --boot pvh --boot-arch x86_64 kernel.kl     # kernel.elf

# 自定义引导汇编
kaulac --freestanding --boot custom --boot-file myboot.S kernel.kl
```

- `--freestanding` 使用 `-ffreestanding -nostdlib -nostartfiles`，自动链接 freestanding 库（`kaula_freestanding.lib` / `libkaula_freestanding.a`）
- 内置引导模板与链接脚本：`compiler/templates/boot/`（`x86_64-pvh.S`、`x86_64-multiboot.S`、`i386-multiboot.S`、`riscv64.S`、`aarch64.S`）与 `templates/linker/`（`x86_64.ld`、`i386.ld`、`riscv64.ld`、`aarch64.ld`、`user.ld`）
- 输出格式：`elf`（默认，适合 QEMU `-kernel`）或 `bin`（裸映像，适合自定义加载器/烧录）
- 常用入口：`_start`；架构由 `--target-triple` 推断，可用 `--boot-arch` 覆盖

### 6.2 裸机部署流程

```bash
# 1. 构建（含 freestanding 静态库）
python toolkit_build.py --release

# 2. 编译内核为可引导 ELF
kaulac --freestanding --boot multiboot --boot-arch x86_64 kernel.kl

# 3. 部署到模拟器
qemu-system-x86_64 -kernel kernel.elf

# 4. 或输出 raw bin 烧录/交付给引导加载器
kaulac --freestanding --boot none --output-format bin --link-script my.ld kernel.kl
```

完整裸机特性见 [裸机开发指南](bare-metal.md)。

## 7. CI 集成建议

```bash
# 典型流水线：构建（缓存 build/）→ 测试 → 发布
python toolkit_build.py --release
kaulac --no-cache --release test/foo.kl && ./build/bin/kaulac.exe --cache-stats
# 发布物：build/bin、build/lib、build/include（或整目录压缩）
```

注意：

- 构建缓存 `build/hash` + `build/obj` 若可跨流水线保留，增量构建从分钟级降至秒级
- 编译器工作目录缓存 `cache/`（PCH 与 std `.o`）建议在 CI 中保留以加速首次编译
- 多目标平台需在对应平台上分别执行构建（见 [交叉编译](#5-交叉编译)）

## 8. 故障排查

| 现象 | 原因与处理 |
|------|-----------|
| `no archiver found` / 找不到 `llvm-lib` | 缺少 LLVM 归档工具：安装完整 LLVM（含 `llvm-lib`/`llvm-ar`），或 Windows 下使用 VS 的 `lib.exe`（加入 PATH） |
| 编译报 `LNK2005` 重复定义 | 源码树模式与安装模式混用：同一编译中既链接了预编译静态库又链接了 `cache/std-objects` 下的 `.o`。确保使用 `build/` 部署的 kaulac 时删除源码附近的 `cache/`，或统一走源码树模式 |
| `stdlib.json` 找不到 | kaulac 二进制与其同级目录缺少 `stdlib.json`（`--install-dir` 输出时若 bin 被单独拷贝会丢失，需连同 stdlib.json 一起分发） |
| 头文件解析到错误的同名模块 | freestanding 与 std 存在同名头（如 `memory/memory.h`）：保持安装布局 `include/{std,freestanding}` 分离，勿合并目录 |
| 构建产物是旧版本 | `--clean` 后重新构建；Go 侧改动会自动触发重编，C 侧由 SHA256 指纹保证 |

## 相关文档

- 构建脚本参数：[tools/toolkit-build.md](tools/toolkit-build.md)
- 编译器 CLI 全参数：[tools/kaulac.md](tools/kaulac.md)
- 配置文件 kaula.json：[build-config.md](build-config.md)
- 裸机模式：[bare-metal.md](bare-metal.md)