

# Kaula 编程语言

<div align="center">

**更现代、更好用的 C**

[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8.svg?logo=go)](https://go.dev/)
[![Language](https://img.shields.io/badge/runtime-C-A8B9CC.svg?logo=c)](https://en.wikipedia.org/wiki/C_(programming_language))

</div>

## Kaula 是什么

Kaula 是一门**静态类型的系统级编程语言**，编译到 C，由 Clang 生成原生代码。

核心设计目标：**用更简单的语法，获得接近 Rust 的安全保证**——通过 SOR（子结构所有权）在编译期消除数据竞争和悬垂指针，同时不引入 GC 或引用计数的运行时开销。

## 核心特性

### 1. SOR 子结构所有权 (Sub-structural Ownership)
Kaula 的核心安全机制——SOR 在编译期追踪资源所有权。
- **yield**: 所有权转移（move 语义）。
- **extract**: 子结构提取（从复合类型中取出部分所有权）。
- **release**: 所有权分发（将一个资源拆分给多个持有者）。
- 编译器通过 `kaulac --sor` 启用此功能。

### 2. KMM V4 内存管理
Kaula 默认使用 KMM V4（Kaula Memory Manager V4），基于 **per-thread heap + bump allocation + scope-based reclamation** 设计。
- **性能卓越**：小对象分配比传统 malloc/free 快 10-20 倍。
- **零碎片化**：Bump allocation 模式，无内存碎片。
- **自动回收**：作用域退出时批量回收内存，无需手动 `free`。

### 3. Prefix 前缀系统
Kaula 独有的声明式代码复用机制，允许使用 `$` 变量定义可复用的代码模板。

### 4. Spend/Call 消费式遍历
对集合的逐元素结构化处理，强制消费语义，防止迭代遗漏。

### 5. 完整开发生态
- **标准库**：62 个模块，800+ 函数，覆盖 I/O、容器、网络、并发、GUI 等。
- **Freestanding**：无依赖标准库，专为裸机/嵌入式设计。
- **Pkglib**：放库即用的第三方 C/C++ 库集成（自动分析、桥接、构建）。
- **裸机开发**：支持无 OS 环境的内核开发，自动链接最小运行时。

## 快速开始

### 环境依赖
- **Python 3.8+** (用于构建脚本)
- **Go 1.21+** (用于编译器实现)
- **Clang/LLVM** (用于 C 代码编译与第三方库分析)

### 构建

执行构建脚本生成编译器、标准库及运行时：

```bash
# Debug 构建
python toolkit_build.py

# Release 构建 (优化编译)
python toolkit_build.py --release
```

### 编译与运行

```bash
# 编译源文件
kaulac hello.kl

# 启用 SOR 安全分析
kaulac --sor hello.kl

# Release 模式编译
kaulac --release hello.kl
```

## 项目结构

Kaula 项目的目录结构清晰地分离了编译器实现、标准库与运行时系统：

- **`compiler/`**: 编译器核心逻辑，由 Go 语言实现。
  - `cmd/kaulac/`: 编译器命令行工具。
  - `internal/lexer/`: 词法分析器。
  - `internal/parser/`: 语法分析器。
  - `internal/sema/`: 语义分析与类型检查。
  - `internal/codegen/`: C 代码生成器。
  - `internal/sor/`: SOR 所有权分析引擎。
- **`std/`**: 托管模式标准库（C 实现）。
- **`freestanding/`**: Freestanding 无依赖标准库。
- **`src/`**: 运行时系统（C 实现，包含 KMM V4 内存管理器）。
- **`pkglib/`**: 第三方库存放目录。
- **`docs/`**: 详细的编译器文档、教程与 API 参考。

## 文档

详细的技术文档、教程和 API 参考请查阅 [`docs/`](docs/) 目录。

## 许可证

[Apache License 2.0](LICENSE)。