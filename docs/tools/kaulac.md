# kaulac — Kaula 编译器

Kaula 编译器命令行工具，负责词法/语法/语义分析、C 代码生成，并调用 Clang 编译链接为原生可执行文件。

```
用法: kaulac [选项] <input.kl>
```

## 一、快速开始

```bash
# 最简单的编译（在当前目录生成 main.exe/main）
kaulac main.kl

# 生成默认配置文件 kaula.json
kaulac --init

# Release 模式
kaulac --release main.kl

# 启用 SOR 所有权分析
kaulac --sor main.kl
```

编译流程：`源代码 .kl → 词法分析 → 语法分析 → 语义分析 → C 代码生成 → Clang 编译 → 可执行文件`。

## 二、输出与命名

| 选项 | 说明 |
|------|------|
| `--output-dir <dir>` | 指定输出目录（默认与源文件同目录） |
| `--output-format <fmt>` | 输出格式：`elf`（默认）/ `bin` / `obj` |

- 输入文件必须为 `.kl` 扩展名
- 默认输出名与源文件名相同（`foo.kl` → `foo.exe` / `foo`，按平台）
- 输出文件名由源码决定，**不支持 `-o` / `--output` 指定**（该 flag 未注册，传入会报 `flag provided but not defined`，仅改目录可用 `--output-dir`）
- 支持子命令名 `compile` / `run` / `build` / `check`（仅用于区分参数，不改变行为）

## 三、优化

| 选项 | 说明 |
|------|------|
| `--opt <level>` | 优化级别 `O0`/`O1`/`O2`/`O3` |
| `--release` | Release 模式（`-O3`） |
| `--sor` | 启用 SOR 编译时所有权分析（默认 `-O3`） |

**优化级别优先级**：`--opt 手动指定 > --sor > --release > 默认 O2`。

## 四、缓存管理

缓存目录位于当前目录 `cache/`，基于 SHA256 增量缓存，未变更的源文件秒级跳过。

| 选项 | 说明 |
|------|------|
| `--clean-cache` | 清理过期缓存（7 天以上且超过 1 GB） |
| `--purge-cache` | 清空所有缓存 |
| `--cache-stats` | 显示缓存统计信息（条数、大小、最旧/最新条目） |
| `--no-cache` | 禁用增量编译缓存（即时编译，不写缓存） |

```bash
kaulac --cache-stats
kaulac --purge-cache
kaulac --no-cache main.kl
```

## 五、资源配置

| 选项 | 说明 |
|------|------|
| `--memory-limit <MB>` | 编译器内存限制（默认 4096 MB） |
| `--timeout <sec>` | 编译超时限制（默认 120 秒） |
| `--queue <n>` | 任务队列大小（默认 100） |
| `--spendable <n>` | 可花费组件大小（默认 10） |

超时会终止编译进程；内存超限会触发 FATAL 退出。

## 六、标准库与路径

| 选项 | 说明 |
|------|------|
| `--include <path>` | C 头文件包含路径（默认 `../std`） |
| `--stdlib <path>` | 标准库路径（默认自动检测） |
| `--template <path>` | 代码生成模板路径（默认 `templates`） |
| `--source-dir <dir>` | 源文件目录 |
| `--target <lang>` | 目标语言（默认 `c`） |
| `--sourcemap` | 生成源码映射文件（`<文件>.map.json`） |

**路径解析**：`kaula.h`、freestanding 源码、`pkglib/` 的查找优先级均为
`KAULA_HOME` 环境变量 > 可执行文件所在目录 > 相对路径。设置方式：

```bash
export KAULA_HOME=/path/to/kaula        # Linux/macOS
set KAULA_HOME=D:\path\to\kaula         # Windows cmd
$env:KAULA_HOME = "D:\path\to\kaula"    # PowerShell
```

## 七、pkglib 第三方库管理

放库即用：库目录放入 `pkglib/`，`import` 即自动分析 → 桥接 → 构建 → 链接。

| 选项 | 说明 |
|------|------|
| `--pkglib <dir>` | 指定第三方库目录（优先于自动检测） |
| `--build-pkglib <name>` | 构建指定库后退出（`all` = 全部）；幂等：无配置先分析、过期自动重分析、再构建归档 |
| `--analyze-pkg <name>` | 强制手动重新解析指定库并重写配置（不合并人工链接项） |
| `--analyze-pkg-all` | 重新解析 pkglib 下全部库 |
| `--force-pkg` | 强制重新构建/重新分析 pkglib 库（配合 `--build-pkglib`） |
| `--skip-auto-pkg` | 禁用使用库时的自动构建/自愈 |
| `--auto-analyze-pkg` | **已废弃**（过期配置默认自动重新分析） |

```bash
kaulac --build-pkglib all
kaulac --build-pkglib cjson --force-pkg
kaulac --analyze-pkg imgui
kaulac --pkglib D:/mylibs main.kl
```

> 注意：`--analyze-pkg` 直接重写配置、不走 `MergeLibrariesInto`，会把旧配置人工补充的
> 链接库（如 imgui 的 `d3d11/dwmapi/d3dcompiler`）丢掉；配置含人工链接项时建议用
> `--build-pkglib <name>`（自愈合并保留人工项）。

## 八、裸机/交叉编译

| 选项 | 说明 |
|------|------|
| `--freestanding` | 裸机模式（`-ffreestanding -nostdlib -nostartfiles`），自动链接 freestanding 库 |
| `--boot <mode>` | 引导方式：`pvh` / `multiboot` / `custom` / `none`（默认 `none`，需配合 `--freestanding`） |
| `--boot-file <path>` | 自定义引导汇编文件（`boot=custom` 时使用） |
| `--boot-arch <arch>` | 引导架构：`x86_64` / `i386` / `riscv64` / `aarch64`（默认从 `--target-triple` 推断） |
| `--target-triple <triple>` | 目标三元组（如 `x86_64-unknown-elf`、`aarch64-none-elf`） |
| `--link-script <path>` | 链接脚本路径（`.lds`） |
| `--entry <name>` | 入口函数名（默认 `main`，裸机可为 `_start`） |

```bash
# 编译引导裸机内核（ELF 输出）
kaulac --freestanding --boot pvh --boot-arch x86_64 kernel.kl

# 自定义引导
kaulac --freestanding --boot custom --boot-file myboot.S --output-format elf kernel.kl

# 无自动引导（none），仅链接用户程序
kaulac --freestanding --boot none main.kl
```

`--boot-file` 优先级高于内置模板 `<templates>/boot/<arch>-<boot>.S`；`boot=custom` 必须提供 `--boot-file`。

## 九、C 编译器透传

| 选项 | 说明 |
|------|------|
| `--cflags <flags>` | 额外的 C 编译器参数（空格分隔） |
| `--defines <macros>` | 额外的 C 宏定义（逗号分隔） |
| `--libs <libs>` | 额外的链接库（逗号分隔） |

Windows 平台自动附加 `ws2_32/wininet/gdi32/user32/advapi32`；非 Windows 附加 `-lm`。

## 十、配置文件 kaula.json

命令行参数优先级高于配置文件。选项与 `kaula.json` 字段一一对应：

| 命令行 | 配置文件字段 |
|--------|--------------|
| `--template` | `template_path` |
| `--include` | `include_path` |
| `--stdlib` | `stdlib_path` |
| `--pkglib` | `pkglib_path` |
| `--source-dir` | `source_dir` |
| `--output-dir` | `output_dir` |
| `--target` | `target_language` |
| `--opt` / `--release` | `opt_level` / `release` |
| `--sor` | `sor` |
| `--no-cache` | `no_cache` |
| `--queue` / `--spendable` | `queue_size` / `spendable_size` |
| `--memory-limit` / `--timeout` | `memory_limit_mb` / `timeout_sec` |
| `--cflags` / `--defines` / `--libs` | `c_flags` / `c_defines` / `c_libs` |
| `--freestanding` | `freestanding` |
| `--target-triple` | `target_triple` |
| `--link-script` | `link_script` |
| `--entry` | `entry` |
| `--output-format` | `output_format` |
| `--sourcemap` | `sourcemap` |
| `--build-pkglib` / `--analyze-pkg` 等 | 同名小写字段 |

完整配置说明见 [../build-config.md](../build-config.md)。

## 十一、退出行为

- `--init`、`--build-pkglib`、`--analyze-pkg(-all)`、缓存管理命令执行完即退出，无需输入文件
- 无输入文件且无上述命令 → 打印 Usage 并退出
- 输入文件非 `.kl` → 报错退出
- 编译失败 → 打印错误、退出码非零