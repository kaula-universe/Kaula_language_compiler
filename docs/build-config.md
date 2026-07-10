# 编译配置文件 (kaula.json)

Kaula 编译器支持通过 `kaula.json` 配置文件管理所有编译参数，无需每次都通过命令行指定。

## 快速开始

### 生成默认配置

```bash
kaulac.exe --init
```

这会在当前目录生成 `kaula.json`：

```json
{
  "base_path": "C:\\Users\\you\\project",
  "template_path": "templates",
  "include_path": "../std",
  "target_language": "c",
  "sor": false,
  "vo_cache_size": 2048,
  "queue_size": 100,
  "spendable_size": 10,
  "memory_limit_mb": 4096,
  "timeout_sec": 120
}
```

### 编译项目

```bash
# 使用配置文件编译
kaulac.exe main.kl

# 命令行参数覆盖配置文件
kaulac.exe --release main.kl
kaulac.exe --sor main.kl
```

## 配置参数

### 基础路径

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `base_path` | string | 当前目录 | 项目根目录 |
| `template_path` | string | `"templates"` | 代码生成模板路径 |
| `include_path` | string | `"../std"` | C 头文件包含路径 |
| `stdlib_path` | string | `""` | 标准库路径（自动检测） |
| `pkglib_path` | string | `""` | 第三方库路径（自动检测） |
| `source_dir` | string | `""` | 源文件目录 |
| `output_dir` | string | `""` | 输出目录（默认与源文件同目录） |

### 目标与优化

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `target_language` | string | `"c"` | 目标语言 |
| `opt_level` | string | `""` | 优化级别：`O0`/`O1`/`O2`/`O3` |
| `release` | bool | `false` | Release 模式（等效 `--opt O3`） |

**优化级别优先级**：

```
--opt 手动指定 > --sor > --release > 默认 O2
```

### 编译模式

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `sor` | bool | `false` | 启用 SOR 编译时所有权分析 |
| `release` | bool | `false` | Release 模式（-O3 优化） |
| `no_cache` | bool | `false` | 禁用增量编译缓存 |

### 运行时配置

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `vo_cache_size` | int | `2048` | VO 缓存大小 |
| `queue_size` | int | `100` | 任务队列大小 |
| `spendable_size` | int | `10` | 可花费组件大小 |

### 资源限制

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `memory_limit_mb` | int | `4096` | 编译器内存限制 (MB) |
| `timeout_sec` | int | `120` | 编译超时限制 (秒) |

### C 编译器选项

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `c_flags` | string[] | `[]` | 额外的 C 编译器参数 |
| `c_defines` | string[] | `[]` | 额外的 C 宏定义 |
| `c_libs` | string[] | `[]` | 额外的链接库 |

### 裸机/交叉编译

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `freestanding` | bool | `false` | 裸机模式（`-ffreestanding -nostdlib -nostartfiles`） |
| `target_triple` | string | `""` | 目标三元组（如 `x86_64-unknown-elf`、`aarch64-none-elf`） |
| `link_script` | string | `""` | 链接脚本路径（`.lds`） |
| `entry` | string | `""` | 入口函数名（默认 `main`，裸机可为 `_start`） |
| `output_format` | string | `""` | 输出格式：`elf`（默认）/ `bin` / `obj` |

### 其他

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `sourcemap` | bool | `false` | 生成源码映射文件 |

## 完整配置示例

```json
{
  "base_path": ".",
  "template_path": "templates",
  "include_path": "../std",
  "target_language": "c",

  "opt_level": "O3",
  "sor": true,
  "release": false,
  "no_cache": false,

  "vo_cache_size": 4096,
  "queue_size": 200,
  "spendable_size": 20,

  "memory_limit_mb": 8192,
  "timeout_sec": 300,

  "c_flags": ["-Wall", "-Werror"],
  "c_defines": ["DEBUG=1", "PLATFORM_WINDOWS"],
  "c_libs": ["pthread", "m"],

  "sourcemap": true
}
```

## 参数优先级

```
命令行参数 > kaula.json 配置文件 > 默认值
```

例如：

```bash
# kaula.json 中 opt_level = "O2"
kaulac.exe main.kl                    # 使用 O2
kaulac.exe --opt O3 main.kl           # 命令行覆盖，使用 O3
kaulac.exe --release main.kl          # 使用 O3
kaulac.exe --sor main.kl              # SOR 模式，默认 O3
```

## 命令行参数对照

| 命令行参数 | kaula.json 参数 | 说明 |
|-----------|----------------|------|
| `--template <path>` | `template_path` | 模板路径 |
| `--include <path>` | `include_path` | 包含路径 |
| `--stdlib <path>` | `stdlib_path` | 标准库路径 |
| `--pkglib <path>` | `pkglib_path` | 第三方库路径 |
| `--target <lang>` | `target_language` | 目标语言 |
| `--opt <level>` | `opt_level` | 优化级别 |
| `--sor` | `sor` | 启用 SOR |
| `--release` | `release` | Release 模式 |
| `--no-cache` | `no_cache` | 禁用缓存 |
| `--vo-cache <size>` | `vo_cache_size` | VO 缓存大小 |
| `--queue <size>` | `queue_size` | 队列大小 |
| `--spendable <size>` | `spendable_size` | 可花费组件大小 |
| `--memory-limit <MB>` | `memory_limit_mb` | 内存限制 |
| `--timeout <sec>` | `timeout_sec` | 超时限制 |
| `--cflags <flags>` | `c_flags` | C 编译器参数 |
| `--defines <macros>` | `c_defines` | C 宏定义 |
| `--libs <libs>` | `c_libs` | 链接库 |
| `--sourcemap` | `sourcemap` | 源码映射 |
| `--freestanding` | `freestanding` | 裸机模式 |
| `--target-triple <triple>` | `target_triple` | 目标三元组 |
| `--link-script <path>` | `link_script` | 链接脚本路径 |
| `--entry <name>` | `entry` | 入口函数名 |
| `--output-format <fmt>` | `output_format` | 输出格式：elf/bin/obj |

## 场景示例

### 开发环境

```json
{
  "opt_level": "O0",
  "no_cache": true,
  "sourcemap": true,
  "c_defines": ["DEBUG=1"],
  "timeout_sec": 60
}
```

### 发布构建

```json
{
  "opt_level": "O3",
  "release": true,
  "no_cache": false,
  "c_flags": ["-Wall"],
  "timeout_sec": 300
}
```

### SOR 模式

```json
{
  "sor": true,
  "opt_level": "O3",
  "c_defines": ["KMM_THREAD_SAFETY_LEVEL=1"],
  "memory_limit_mb": 8192
}
```

### 交叉编译

```json
{
  "target_language": "c",
  "c_flags": ["--target=x86_64-linux-gnu"],
  "c_libs": ["pthread"]
}
```

### 裸机/系统级开发

```json
{
  "freestanding": true,
  "target_triple": "x86_64-unknown-elf",
  "link_script": "linker.ld",
  "entry": "_start",
  "output_format": "bin"
}
```

详见 [裸机开发指南](bare-metal.md)。

## 配置文件位置

编译器按以下顺序查找 `kaula.json`：

1. 当前工作目录
2. 命令行指定的源文件所在目录

配置文件必须命名为 `kaula.json`，位于项目根目录。

## 注意事项

1. **JSON 格式**：配置文件必须是合法的 JSON
2. **路径处理**：相对路径会自动转换为绝对路径
3. **默认值**：未指定的参数使用默认值
4. **命令行优先**：命令行参数总是覆盖配置文件
5. **版本控制**：建议将 `kaula.json` 纳入版本控制
