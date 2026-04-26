# Kaula 编程语言

<div align="center">

**高性能、系统级的编译型编程语言**

[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.21.0-00ADD8.svg?logo=go)](https://go.dev/)
[![Language](https://img.shields.io/badge/language-C-C.svg?logo=c)](https://en.wikipedia.org/wiki/C_(programming_language))
[![Version](https://img.shields.io/badge/version-0.1.0--alpha-orange.svg)](https://github.com/yourusername/kaula/releases)
[![Build Status](https://img.shields.io/badge/build-passing-brightgreen.svg)]()

</div>

---

## 📖 项目概述

Kaula 是一款静态类型的编译型编程语言，采用 **Go 语言实现的编译器** 和 **C 语言实现的运行时系统**。

> ⚠️ **注意**：当前版本为 **Alpha 预览版**，主要支持 Windows 操作系统
> 
> 🎯 **发布状态**：v0.1.0-alpha - 核心功能已实现，仍在快速迭代中

---

## 🏗️ 项目架构

```
kaula/
├── kaula-compiler/          # 编译器（Go 实现）
│   ├── cmd/kaulac/          # 编译器命令行工具
│   │   └── main.go          # 主入口（并发编译管线、超时控制）
│   ├── internal/
│   │   ├── ast/             # 抽象语法树定义
│   │   ├── codegen/         # C 代码生成器（模块化架构）
│   │   │   ├── codegen.go   # 核心生成器
│   │   │   ├── typegen.go   # 类型生成
│   │   │   ├── funcgen.go   # 函数生成
│   │   │   ├── exprgen.go   # 表达式生成
│   │   │   ├── stmtgen.go   # 语句生成
│   │   │   ├── template.go  # 模板管理
│   │   │   └── plugin.go    # 插件系统
│   │   ├── compiler/        # 编译器核心逻辑
│   │   ├── config/          # 配置管理
│   │   ├── core/            # 核心运行时特性（Go 层）
│   │   │   ├── vo.go        # VO 系统
│   │   │   ├── spendcall.go # Spend/Call 机制
│   │   │   ├── prefix.go    # 前缀系统
│   │   │   ├── tree.go      # 树系统
│   │   │   └── task.go      # 任务调度
│   │   ├── errors/          # 错误处理
│   │   ├── lexer/           # 词法分析器
│   │   ├── parser/          # 语法分析器（递归下降解析）
│   │   ├── sema/            # 语义分析器
│   │   ├── stdlib/          # 标准库配置加载
│   │   ├── symbol/          # 符号表
│   │   ├── test/            # 测试工具
│   │   └── timeout/         # 超时控制（内存、时间限制）
│   ├── templates/           # 代码生成模板
│   │   └── main.c.tmpl
│   ├── stdlib.json          # 标准库函数签名定义（24 个模块）
│   └── go.mod
├── pkglib/                  # 第三方库自动加载
│   ├── stb_image/
│   ├── nuklear/
│   └── zlib/
├── src/                     # 运行时系统（C 实现）
│   ├── kaula.h              # 核心头文件（跨平台宏、类型定义）
│   ├── platform.h           # 平台检测
│   ├── kmm_scoped_allocator_v4.h # V4 内存管理头文件
│   ├── kmm_scoped_allocator.c   # KMM V4 作用域分配器
│   ├── allocator.c          # 快速分配器
│   ├── vo.c                 # VO 系统
│   ├── spend_call.c         # Spend/Call 机制
│   ├── queue.c              # 优先级队列
│   ├── prefix_system.c      # 前缀系统
│   └── tree_system.c        # 树系统
└── std/                     # 标准库（C 实现，18 个模块）
    ├── async/               # 异步操作（事件循环、协程、I/O）
    ├── base/                # 基础类型转换与比较
    ├── concurrent/          # 并发原语（线程、锁、原子操作、线程池）
    ├── container/           # 容器（Vector、LinkedList、HashMap、Stack）
    ├── error/               # 错误处理
    ├── format/              # 格式化（printf、FormatBuilder）
    ├── gui/                 # GUI 支持（Nuklear 绑定）
    ├── i18n/                # 国际化（多语言、编码转换、UTF-8）
    ├── io/                  # I/O 操作（控制台、文件）
    ├── math/                # 数学函数（标准数学库、随机数）
    ├── memory/              # 内存管理
    ├── prefix/              # 前缀系统接口
    ├── string/              # 字符串处理
    ├── system/              # 系统调用（进程、文件、环境、网络）
    ├── task/                # 任务调度（优先级队列）
    ├── time/                # 时间测量
    ├── vo/                  # VO 系统接口
    └── web/                 # HTTP 服务器/客户端、URL 处理
```

---

## ✨ 核心特性

### 1. 编译器

Kaula 编译器使用 **Go 1.21+** 实现，包含以下核心组件：

- **词法分析器（Lexer）**：状态机实现，支持关键字、标识符、字面量、字符串、注解（`#[...]`）、前缀引用（`$`）、前缀调用（`@`）等
- **语法分析器（Parser）**：迭代式递归下降解析，构建抽象语法树
- **语义分析器（Semantic）**：两遍分析（符号收集 → 函数体分析）、符号表管理、类型检查、作用域验证、泛型约束
- **代码生成器（Codegen）**：基于模板生成 C 代码，模块化设计（类型/函数/表达式/语句生成器分离）
- **泛型系统**：支持泛型函数实例化与缓存

**编译流程**：
```
源代码 → 词法分析 → 语法分析 → 语义分析 → 代码生成 → C 代码 → Clang 编译 → 可执行文件
```

**并发编译**：编译器支持多阶段并发处理（词法/语法分析、语义分析、代码生成、C 编译）

### 2. 运行时系统

运行时使用 **C 语言** 实现，提供以下核心功能：

#### KMM V4 ScopedAllocator（作用域内存管理）

基于 V4 架构的分级内存管理系统：
- 支持 Arena 分级分配
- ThreadCache 线程局部缓存（原子操作，轻量实时线程安全）
- SafeAlloc 安全分配
- Cleanup Stack 自动清理栈
- Union Domain 联合域管理
- O(1) 批量释放，作用域退出时自动清理

#### VO (Virtual On-site) 系统

高效的数据和代码缓存机制：

```kaula
vo create(100)              # 创建 VO 模块
vo_data_load(vo, 1, data)   # 加载数据
vo_code_load(vo, -1, fn)    # 加载代码
vo_associate(vo, 1, -1)     # 关联数据和代码
result = vo_access(vo, 1)   # 访问（自动执行代码）
```

#### Spend/Call 机制

动态组件管理：

```kaula
spend(component1, component2):
    call target1:
        # 处理逻辑
    call target2:
        # 处理逻辑
```

#### 三级优先级队列

任务调度系统：
- Priority 0 (HIGH) - 高优先级任务
- Priority 1 (MEDIUM) - 普通任务
- Priority 2 (LOW) - 低优先级任务

#### 跨平台支持

`kaula.h` 提供跨平台宏定义：
- Windows / Linux / macOS 平台检测
- GCC / Clang / MSVC 编译器检测
- 线程局部存储（TLS）支持
- 原子操作支持（C11 或 GCC 内置）

### 3. 标准库

提供超过 **400+** 个标准函数，包括：

| 模块 | 功能 |
|------|------|
| **base** | 类型转换、比较、类型判断 |
| **memory** | KMM V4、快速分配器、内存池 |
| **string** | 字符串创建、操作、搜索、替换 |
| **io** | 控制台 I/O、文件操作、路径处理 |
| **math** | 数学函数、三角函数、随机数 |
| **container** | Vector、LinkedList、HashMap、Stack |
| **concurrent** | 线程、互斥锁、条件变量、信号量、读写锁、原子操作、线程池 |
| **async** | 异步任务、事件循环、协程、异步 I/O |
| **system** | 系统信息、进程管理、环境变量、文件系统 |
| **task** | 任务创建、优先级队列调度 |
| **vo** | VO 系统接口 |
| **prefix** | 前缀系统接口 |
| **error** | 错误处理、错误类型、错误打印 |
| **format** | 格式化输出、FormatBuilder |
| **time** | 时间测量、日期时间转换 |
| **i18n** | 国际化、多语言支持、编码转换 |
| **gui** | GUI 支持（Nuklear 绑定） |
| **web** | HTTP 服务器/客户端、URL 处理、MIME 类型 |
| **windows** | Windows 特定功能（注册表、进程信息） |
| **syscall** | 系统调用接口 |

---

## 🛠️ 编译器工具链

### kaulac 命令行用法

```bash
# 基本用法
kaulac.exe [选项] <源文件.kl>

# 编译单个文件
kaulac.exe program.kl

# 编译并启用增量编译缓存
kaulac.exe program.kl

# 禁用缓存强制重新编译
kaulac.exe --no-cache program.kl

# 查看缓存统计信息
kaulac.exe --cache-stats

# 清理过期缓存（7 天以上）
kaulac.exe --clean-cache

# 清空所有缓存
kaulac.exe --purge-cache
```

### 命令行选项

| 选项 | 说明 |
|------|------|
| `--no-cache` | 禁用增量编译，强制重新编译 |
| `--cache-stats` | 显示缓存统计信息（条目数、大小、时间范围） |
| `--clean-cache` | 清理过期缓存条目 |
| `--purge-cache` | 清空所有缓存 |
| `-template <path>` | 指定代码生成模板路径（默认：templates） |
| `-include <path>` | 指定 C 头文件包含路径（默认：../std） |
| `-target <lang>` | 指定目标语言（默认：c） |
| `-vo-cache <size>` | 设置 VO 缓存大小（默认：2048） |
| `-queue <size>` | 设置队列大小（默认：100） |
| `-spendable <size>` | 设置可花费组件大小（默认：10） |

### 编译流程

```
┌─────────────┐
│ 源文件.kl    │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ 词法分析    │ 6ms
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ 语法分析    │ (并发)
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ 语义分析    │ 3ms
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ 代码生成    │ 生成 C 代码
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ Clang 编译   │ 2.6s
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ 可执行文件   │
└─────────────┘
```

**典型编译时间**：~2.9s（首次编译） / ~2.6s（缓存命中）

### 增量编译

编译器支持智能增量编译，通过缓存机制加速重复编译：

```bash
# 首次编译（完整流程）
$ kaulac.exe main.kl
[Cache] Stored cache for main.kl (362 bytes)
[Compile] Completed in 2.85s

# 第二次编译（使用缓存）
$ kaulac.exe main.kl
[Cache] Cache hit for main.kl
[Cache] Using cached C code: cache/main.c
[Compile] Completed in 2.64s (cache hit)

# 查看缓存统计
$ kaulac.exe --cache-stats
=== Cache Statistics ===
Total entries: 1
Total size: 479 bytes (0.00 MB)
Oldest entry: 2026-04-26 18:32:08
Newest entry: 2026-04-26 18:32:08
```

**缓存验证机制**：
- SHA256 源文件哈希比对
- 文件大小和修改时间验证
- 编译器版本追踪
- 自动失效机制

### 标准库配置

编译器通过 `stdlib.json` 管理标准库函数签名：

```json
{
  "std.io": {
    "header": "std/io/io.h",
    "println": {"args": ["const char*"], "varargs": true},
    "print_int": {"args": ["i64"]},
    "file_open": {"args": ["const char*", "const char*"], "return": "File"}
  },
  "std.math": {
    "header": "std/math/math.h",
    "math_sqrt": {"args": ["f64"], "return": "f64"},
    "math_pow": {"args": ["f64", "f64"], "return": "f64"}
  }
}
```

**作用**：
- 类型检查时验证函数调用
- 自动生成正确的 C 函数声明
- 追踪模块依赖关系

### 第三方库集成

编译器自动从 `pkglib/` 目录加载第三方库：

```bash
pkglib/
├── stb_image/
│   └── stb_image.json
├── nuklear/
│   └── nuklear.json
└── zlib/
    └── zlib.json
```

**库配置文件格式**：

```json
{
  "name": "stb_image",
  "headers": ["\"stb_image.h\""],
  "libraries": ["stb_image.lib"],
  "functions": {
    "stbi_load": {
      "args": ["const char*", "int*", "int*", "int*", "int"],
      "return": "void*"
    }
  }
}
```

**使用方式**：

```kaula
import stb_image

fn main():
    # 使用 stb_image 加载图片
    void* img = stbi_load("texture.png", &width, &height, &channels, 4)
```

### 编译输出

```bash
$ kaulac.exe program.kl
=== Concurrent Compilation Pipeline ===
Starting at 18:32:08.619

[Stage 1] Lexing + Parsing...
[Stage 1] Lex + Parse completed in 6.0025ms

[Stage 2] Semantic Analysis...
[Stage 2] Semantic Analysis completed in 0.9999ms

[Stage 3] Code Generation + C Compilation...
[Cache] Stored cache for program.kl (362 bytes)
[Compile] Clang command args:
  cache/program.c
  -o program.exe
  -O3
  -I E:\kaula
  -I E:\kaula\src
  -I E:\kaula\std
  E:\kaula\src\kmm_scoped_allocator.c
  E:\kaula\std\io\io.c
[Compile] Successfully compiled: program.exe
[Compile] Completed in 2.8505352s

=== Compilation Results ===
Status: SUCCESS
Output: program.exe
Cache: cache/program.c

=== Timing Breakdown ===
Stage 1 (Lex + Parse):         6.0025ms
Stage 2 (Semantic):            0.9999ms
Stage 3 (Codegen+Compile):    2.8505352s
---------------------------------
Total End-to-End:              2.9200441s
```

### 错误处理

编译器提供详细的错误报告和修复建议：

```bash
=== Compilation Errors ===

[Lexing & Parsing Errors] (2 errors)
  1. Syntax Error at line 7, column 34: unexpected token: PLUS
     Suggestion: Check for missing or extra punctuation
  2. Syntax Error at line 7, column 35: unexpected token: RPAREN
     Suggestion: Check for missing or extra punctuation

[Semantic Analysis Errors] (1 errors)
  1. Type Error at line 12, column 5: undefined variable 'x'
     Suggestion: Declare variable before use or check scope

Total: 3 error(s)
```

### 性能优化选项

**编译器优化**：
- `-O3`：默认使用 Clang 最高优化级别
- 并发编译：多阶段并行处理
- 增量缓存：跳过未变化的代码生成

**运行时优化**：
- KMM V4 内存管理：O(1) 批量释放
- VO 缓存系统：热点数据自动缓存
- 优先级队列：任务调度优化

### 调试技巧

**1. 查看生成的 C 代码**：
```bash
# C 代码保存在 cache/ 目录
cat cache/program.c
```

**2. 禁用优化调试**：
```bash
# 修改 compileCCode 函数中的 -O3 为 -O0 或 -g
```

**3. 查看编译器日志**：
```bash
# 编译器输出包含详细的阶段信息
# 检查 [Stage X] 和 [Cache] 日志
```

**4. 内存泄漏检测**：
```bash
# 编译器内置内存和超时监控
# 超出限制时自动终止并报告
```

### 构建系统

**编译编译器自身**：
```bash
cd kaula-compiler
go build -o kaulac.exe cmd/kaulac/main.go
```

**运行测试**：
```bash
# 运行编译器测试套件
go test ./internal/...

# 运行基准测试
go test -bench=. ./internal/lexer
go test -bench=. ./internal/parser
go test -bench=. ./internal/codegen
```

**交叉编译**：
```bash
# Windows (当前平台)
GOOS=windows GOARCH=amd64 go build -o kaulac.exe

# Linux
GOOS=linux GOARCH=amd64 go build -o kaulac

# macOS
GOOS=darwin GOARCH=amd64 go build -o kaulac
```

### 项目结构最佳实践

**推荐的目录组织**：
```
myproject/
├── src/
│   ├── main.kl          # 主入口
│   ├── utils.kl         # 工具函数
│   └── modules/         # 模块目录
├── cache/               # 编译缓存（自动创建）
├── build.bat            # 构建脚本
└── .gitignore
    cache/
    *.exe
```

**构建脚本示例** (build.bat)：
```batch
@echo off
echo Building Kaula project...

REM 清理旧缓存（可选）
kaulac.exe --clean-cache

REM 编译主程序
kaulac.exe src/main.kl

REM 检查编译结果
if exist src\main.exe (
    echo Build successful!
    src\main.exe
) else (
    echo Build failed!
    exit /b 1
)
```

### 常见问题

**Q: 编译器找不到 clang？**
```bash
# 确保 clang 在 PATH 中
# Windows: 安装 LLVM 并添加到系统 PATH
# Linux: sudo apt install clang
```

**Q: 缓存目录在哪里？**
```bash
# 缓存位于工作目录的 cache/ 子目录
# 可以通过 --cache-stats 查看使用情况
```

**Q: 如何禁用增量编译？**
```bash
# 使用 --no-cache 选项
kaulac.exe --no-cache program.kl
```

**Q: 标准库如何更新？**
```bash
# 修改 stdlib.json 后重新编译
# 编译器会自动加载最新配置
```

**Q: 如何适配第三方 C 库？**

**步骤 1：在 pkglib/ 目录创建库配置文件夹**
```bash
pkglib/
└── mylib/
    └── mylib.json
```

**步骤 2：编写库配置文件（mylib.json）**
```json
{
  "name": "mylib",
  "headers": ["\"mylib.h\""],
  "libraries": ["mylib.lib"],
  "functions": {
    "mylib_init": {
      "args": [],
      "return": "int"
    },
    "mylib_process": {
      "args": ["const char*", "int"],
      "return": "void*"
    },
    "mylib_cleanup": {
      "args": ["void*"],
      "return": "void"
    }
  }
}
```

**字段说明**：
- `name`: 库名称（用于 import 语句）
- `headers`: C 头文件路径列表（相对于项目根目录）
- `libraries`: 需要链接的库文件列表（Windows 为 .lib，Linux 为 .a/.so）
- `functions`: 函数签名定义
  - `args`: 参数类型列表（使用 C 类型）
  - `return`: 返回类型（使用 C 类型）

**步骤 3：在 Kaula 代码中使用**
```kaula
import mylib

fn main() {
    mylib_init()
    void* result = mylib_process("data", 42)
    mylib_cleanup(result)
}
```

**步骤 4：编译时指定库路径**
```bash
# 编译器会自动从 pkglib/加载配置
kaulac.exe program.kl

# 如果需要额外指定库文件路径
# 修改 compileCCode 函数添加 -L 参数
```

**Q: 第三方库如何更新？**

**方法 1：更新头文件**
```bash
# 1. 替换 pkglib/mylib/mylib.h 为新版本
# 2. 检查函数签名是否变化
# 3. 更新 pkglib/mylib/mylib.json 中的函数定义
# 4. 重新编译程序
kaulac.exe --no-cache program.kl
```

**方法 2：更新库文件**
```bash
# 1. 替换编译后的库文件（mylib.lib 或 libmylib.a）
# 2. 确保新版本的 ABI 兼容
# 3. 重新链接程序
kaulac.exe --no-cache program.kl
```

**方法 3：版本升级注意事项**
```bash
# 如果第三方库有破坏性变更（Breaking Changes）：
# 1. 检查头文件中的函数签名变化
# 2. 更新 mylib.json 中的函数定义
# 3. 修改 Kaula 代码中的调用方式
# 4. 清除缓存并重新编译
kaulac.exe --purge-cache
kaulac.exe --no-cache program.kl
```

**示例：stb_image 库配置**
```json
{
  "name": "stb_image",
  "headers": ["\"stb_image.h\""],
  "libraries": [],
  "functions": {
    "stbi_load": {
      "args": ["const char*", "int*", "int*", "int*", "int"],
      "return": "void*"
    },
    "stbi_image_free": {
      "args": ["void*"],
      "return": "void"
    },
    "stbi_write_png": {
      "args": ["const char*", "int", "int", "int", "const void*", "int"],
      "return": "int"
    }
  }
}
```

**Q: 如何处理第三方库的依赖关系？**

如果第三方库依赖其他库（如 zlib 依赖 libpng）：

```json
{
  "name": "zlib",
  "headers": ["\"zlib.h\""],
  "libraries": ["zlib.lib"],
  "dependencies": ["libpng"],  // 声明依赖
  "functions": {
    "compress": {
      "args": ["void*", "unsigned long*", "const void*", "unsigned long"],
      "return": "int"
    }
  }
}
```

编译器会按依赖顺序自动链接所有必需的库。

---

## 📄 许可证

本项目采用 Apache License 2.0 许可证 - 查看 [LICENSE](LICENSE) 文件了解详情

---

<div align="center">

**Kaula -更现代更好用的C**

</div>
