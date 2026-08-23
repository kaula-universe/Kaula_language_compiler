# Kaula 编程语言

<div align="center">

<img src="logo.png" alt="Kaula Logo" width="200">

**更现代、更好用的 C**

[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8.svg?logo=go)](https://go.dev/)
[![Language](https://img.shields.io/badge/runtime-C-A8B9CC.svg?logo=c)](https://en.wikipedia.org/wiki/C_(programming_language))

</div>

> **Multi-language Support**：如需阅读英文或其他语言的文档，推荐使用 [docs/tools/translate.py](docs/tools/translate.py) 生成本地化文档方便阅读。Supported languages: English, 日本語, 한국어, Deutsch, Français, Español, Русский.

---

## Kaula 是什么

Kaula 是一门**静态类型的系统级编程语言**，编译到 C，由 Clang 生成原生代码。

核心设计目标：**用更简单的语法，获得接近 Rust 的安全保证**——通过 SOR（子结构所有权）在编译期消除数据竞争和悬垂指针，而不引入运行时开销。

---

## 快速开始

### 安装

```bash
# 构建编译器
python toolkit_build.py

# 或手动构建
cd compiler && go build -o ../bin/kaulac ./cmd/kaulac/
```

依赖：Python 3.8+、Go 1.21+、Clang

### Hello World

```kaula
import std.io

fn main() {
    println("Hello, Kaula!")
}
```

### 编译运行

```bash
# 基本编译
kaulac main.kl

# 启用调试符号
kaulac --debug main.kl

# Release 模式
kaulac --release main.kl
```

---

## 包管理

### 添加第三方库

```bash
# 从 Git 仓库添加
kaulac pkg add https://github.com/nicbarker/clay.git

# 从本地路径添加
kaulac pkg add ../my-kaula-lib --name mylib

# 列出已安装的包
kaulac pkg list
```

### 项目依赖

在 `kaula.json` 中声明依赖：

```json
{
  "dependencies": {
    "webview": "0.12",
    "clay": "^1.0"
  },
  "registry": "https://gitee.com/kaula-universe"
}
```

```bash
# 拉取依赖
kaulac pkg fetch

# 更新依赖
kaulac pkg update

# 查看锁状态
kaulac pkg lock
```

### 本地路径覆盖

用本地目录覆盖远程依赖，适合开发调试：

```json
{
  "patches": {
    "mylib": { "path": "../mylib-dev" }
  }
}
```

### 离线/在线模式

```bash
# CI/CD 中使用离线模式（缓存未命中则报错）
kaulac --offline main.kl

# 强制联网刷新依赖
kaulac --online main.kl
```

详见 [docs/tools/package-management.md](docs/tools/package-management.md)。

---

## 工作空间

多包项目管理：

```bash
# 初始化工作空间
kaulac workspace init packages/core packages/utils app

# 列出成员
kaulac workspace list

# 构建所有成员
kaulac workspace build

# 运行测试
kaulac workspace test
```

在 `kaula.json` 中配置：

```json
{
  "workspace": {
    "members": ["packages/core", "packages/utils", "app"],
    "shared_deps": {
      "testing": "^1.0"
    }
  }
}
```

---

## 调试支持

### DWARF 调试符号

```bash
# 生成调试符号
kaulac --debug main.kl

# 完整调试（含类型信息）
kaulac --debug --debug-level full main.kl

# 配合 GDB 调试
gdb ./main
(gdb) source tools/debug/kaula_pretty_printers.py
(gdb) break main.kl:10
(gdb) run
```

### LLDB 支持

```bash
lldb ./main
(lldb) command script import tools/debug/kaula_lldb_formatters.py
(lldb) breakpoint set --file main.kl --line 10
(lldb) run
```

### 源码映射

```bash
# 生成源码映射文件
kaulac --debug --sourcemap main.kl
```

详见 [docs/tools/debugging.md](docs/tools/debugging.md)。

---

## 代码格式化

```bash
# 格式化文件（输出到 stdout）
kaulac fmt file main.kl

# 格式化并写回原文件
kaulac fmt file main.kl --write

# 批量格式化
kaulac fmt file main.kl utils.kl --write

# 检查文件是否已格式化
kaulac fmt check main.kl
```

格式化特性：
- 4 空格缩进
- Import 自动排序（`std.*` 优先）
- 二元表达式括号保留（基于优先级）
- 支持所有 Kaula 语法结构

详见 [docs/tools/formatter.md](docs/tools/formatter.md)。

---

## 编译选项

| 选项 | 说明 |
|------|------|
| `--opt <level>` | 优化级别 O0/O1/O2/O3 |
| `--release` | Release 模式（-O3） |
| `--sor` | 启用 SOR 所有权分析 |
| `--debug` | 生成 DWARF 调试符号 |
| `--debug-level <level>` | 调试级别：line-tables/full |
| `--sourcemap` | 生成源码映射 |
| `--freestanding` | 裸机模式 |
| `--no-cache` | 禁用增量编译 |
| `--offline` | 强制离线模式 |
| `--online` | 强制在线模式 |
| `--verbose` | 详细输出 |

### 配置文件

```bash
# 生成默认配置
kaulac --init
```

`kaula.json` 完整示例：

```json
{
  "opt_level": "O3",
  "release": true,
  "debug": true,
  "debug_level": "full",
  "sourcemap": true,
  "dependencies": {
    "webview": "0.12"
  },
  "patches": {
    "webview": { "path": "../webview-dev" }
  },
  "workspace": {
    "members": ["packages/core", "app"]
  }
}
```

详见 [docs/build-config.md](docs/build-config.md)。

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

## 第三方库（pkglib）

**放库即用、零配置**：把任意 C/C++ 库源码目录放进 `pkglib/`，`import` 即可直接调用。

```kaula
import stb_image   // C 库
import imgui       // C++ 库：自动生成 extern "C" 桥接

fn main() {
    void* img = stbi_load("texture.png", &width, &height, &channels, 4)
    stbi_image_free(img)
    println(kbridge_GetVersion())
}
```

特性：
- **自动分析**：Clang 解析头文件生成 JSON 配置
- **自动桥接**：C++ 头自动生成 `*_kbridge.h/.cpp`
- **自动构建**：编译静态库 `lib<name>.a`
- **自愈合并**：重新分析保留人工链接项

---

## KMM V4 内存管理

基于 per-thread heap + bump allocation + scope-based reclamation 的高性能内存分配器。

### 性能对比

| 场景 | KMM V4 | malloc/free | 加速比 |
|------|--------|-------------|--------|
| 16B 小对象分配+回收 | 51.8 ms | 634.3 ms | **12.2x** |
| 64B 对象分配+回收 | 65.9 ms | 661.5 ms | **10.0x** |
| 纯分配吞吐量（64B, 1M 次） | 3.5 ms | 72.7 ms | **20.5x** |

### 设计特点

- **Per-Thread Heap**：每个线程从全局池批量获取内存块，无锁
- **Bump Allocation**：分配即指针推进，O(1) 复杂度
- **Scope-based Reclamation**：作用域退出批量回收，无需逐个 free

---

## 项目结构

```
kaula/
├── compiler/                  # 编译器（Go 实现）
│   ├── cmd/kaulac/            # 编译器 CLI
│   └── internal/
│       ├── lexer/             # 词法分析
│       ├── parser/            # 语法分析（递归下降）
│       ├── sema/              # 语义分析（两遍、泛型、SOR）
│       ├── codegen/           # C 代码生成 + 源码映射
│       ├── sor/               # SOR 所有权分析引擎
│       ├── formatter/         # 代码格式化器
│       ├── pkgcmd/            # 包管理命令
│       ├── pkgmgr/            # 包管理核心（解析、构建、锁）
│       ├── workspace/         # 工作空间管理
│       ├── config/            # 配置加载
│       ├── stdlib/            # 标准库配置
│       └── ...
├── src/                       # 运行时系统（C 实现）
│   ├── kaula.h                # 跨平台头文件
│   ├── kmm_scoped_allocator_v4.h  # KMM V4 内存管理
│   └── ...
├── std/                       # 标准库（C 实现，62 个模块）
├── pkglib/                    # 第三方库
├── tools/debug/               # GDB/LLDB 调试工具
│   ├── kaula_pretty_printers.py
│   └── kaula_lldb_formatters.py
└── docs/                      # 详细文档
    ├── tools/
    │   ├── kaulac.md          # 命令行参考
    │   ├── package-management.md  # 包管理文档
    │   ├── debugging.md       # 调试文档
    │   └── formatter.md       # 格式化文档
    └── build-config.md        # 配置文件文档
```

---

## 文档

| 文档 | 说明 |
|------|------|
| [docs/tools/kaulac.md](docs/tools/kaulac.md) | 命令行工具完整参考 |
| [docs/tools/package-management.md](docs/tools/package-management.md) | 包管理指南 |
| [docs/tools/debugging.md](docs/tools/debugging.md) | 调试支持文档 |
| [docs/tools/formatter.md](docs/tools/formatter.md) | 代码格式化文档 |
| [docs/build-config.md](docs/build-config.md) | 配置文件参考 |
| [docs/tools/translate.py](docs/tools/translate.py) | 多语言翻译脚本 |

### 多语言文档

使用翻译脚本生成本地化文档：

```bash
# 生成英文版
python docs/tools/translate.py --lang en

# 生成日文版
python docs/tools/translate.py --lang ja

# 生成所有支持语言
python docs/tools/translate.py --all

# 指定输入目录和输出目录
python docs/tools/translate.py --lang en --input docs --output docs/en
```

支持语言：`zh`（中文，默认）、`en`（英文）、`ja`（日文）、`ko`（韩文）、`de`（德文）、`fr`（法文）

---

## 许可证

[Apache License 2.0](LICENSE)，附带 [Kaula Exceptions](LICENSE#kaula-exceptions-to-the-apache-20-license)
