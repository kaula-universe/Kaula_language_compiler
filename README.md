# Kaula 编程语言

<div align="center">

**高性能、系统级的编译型编程语言，专为 Windows 平台优化**

[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Platform](https://img.shields.io/badge/platform-Windows-lightgrey.svg)](README.md)
[![Go Version](https://img.shields.io/badge/Go-1.21.0-00ADD8.svg?logo=go)](https://go.dev/)
[![Language](https://img.shields.io/badge/language-C-C.svg?logo=c)](https://en.wikipedia.org/wiki/C_(programming_language))

</div>

---

## 📖 项目概述

Kaula 是一款静态类型的编译型编程语言，采用 **Go 语言实现的编译器** 和 **C 语言实现的运行时系统**。项目专注于为 Windows 平台提供极致的性能优化和高效的资源管理能力。

### 设计理念

- **性能优先**：编译为原生机器码，零运行时开销
- **内存安全**：创新的 ScopedAllocator 系统，自动化内存管理
- **模块化**：编译器、运行时、标准库独立演进
- **Windows 原生**：深度利用 Windows API，发挥平台最佳性能

> ⚠️ **注意**：当前版本仅支持 Windows 操作系统

---

## 🏗️ 项目架构

```
Kaula/
├── kaula-compiler/          # 编译器（Go 实现）
│   ├── cmd/kaulac/          # 编译器命令行工具
│   ├── internal/
│   │   ├── lexer/           # 词法分析器（状态机实现）
│   │   ├── parser/          # 语法分析器（递归下降）
│   │   ├── sema/            # 语义分析器
│   │   ├── ast/             # 抽象语法树定义
│   │   ├── codegen/         # C 代码生成器
│   │   ├── config/          # 配置管理
│   │   ├── errors/          # 错误处理
│   │   ├── stdlib/          # 标准库配置
│   │   └── symbol/          # 符号表管理
│   ├── templates/           # 代码生成模板
│   ├── stdlib.json          # 标准库函数签名
│   ├── thirdparty.json      # 第三方库配置
│   └── go.mod
├── src/                     # 运行时系统（C 实现）
│   ├── kaula.h              # 核心头文件
│   ├── kmm_scoped_allocator_v2.c  # KMM V2 作用域分配器
│   ├── kmm_scoped_allocator_v2.h
│   ├── allocator.c          # 快速分配器
│   ├── vo.c                 # VO 系统实现
│   ├── spend_call.c         # Spend/Call机制
│   ├── queue.c              # 优先级队列
│   ├── prefix_system.c      # 前缀系统
│   ├── tree_system.c        # 树系统
│   └── time.c               # 高精度时间测量
├── std/                     # 标准库（C 实现）
│   ├── base/                # 基础类型定义
│   ├── memory/              # 内存管理（KMM V2）
│   ├── concurrent/          # 并发原语
│   ├── async/               # 异步操作
│   ├── container/           # 容器（Vector/HashMap等）
│   ├── io/                  # I/O 操作
│   ├── string/              # 字符串处理
│   ├── math/                # 数学函数
│   ├── system/              # 系统调用
│   ├── task/                # 任务调度
│   ├── vo/                  # VO 高级接口
│   ├── prefix/              # 前缀高级接口
│   ├── error/               # 错误处理
│   ├── gui/                 # GUI 支持（Nuklear）
│   └── std.h                # 标准库入口
└── thirdparty/              # 第三方库
    ├── nuklear/             # 轻量级 GUI 库
    └── stb/                 # STB 图像库
```

---

## ✨ 核心特性

### 1. 编译器架构

Kaula 编译器采用经典的前端架构，使用 **Go 1.21+** 实现：

#### 编译流程

```
源代码 → 词法分析 → 语法分析 → 语义分析 → 代码生成 → C 代码 → 机器码
```

#### 核心组件

| 组件 | 职责 | 技术实现 |
|------|------|----------|
| **Lexer** | 词法分析 | 状态机 + 输入长度缓存优化 |
| **Parser** | 语法分析 | 迭代式递归下降解析（避免栈溢出） |
| **Semantic** | 语义分析 | 符号表 + 类型检查 + 作用域验证 |
| **Codegen** | 代码生成 | 模板引擎 + 模块化生成器 |

#### 编译器特性

- ✅ **迭代式解析**：使用显式栈替代递归，避免深层嵌套导致的栈溢出
- ✅ **错误恢复**：智能错误收集和继续解析，一次性报告所有错误
- ✅ **作用域管理**：自动注入 `kaula_scope_enter/exit` 代码
- ✅ **第三方库集成**：通过 `thirdparty.json` 动态加载库配置
- ✅ **模块化代码生成**：Type/Function/Expression/Statement 分离生成

### 2. 运行时系统

运行时使用 **C 语言** 实现，针对 Windows 深度优化：

#### 🎯 KMM V2 ScopedAllocator（作用域内存管理）

```
┌─────────────────────────────────────────┐
│         ScopedAllocator 架构            │
├─────────────────────────────────────────┤
│  Tiny Arena (64KB)   - ≤16B 对象        │
│  Small Arena (1MB)   - ≤128B 对象       │
│  Medium Arena (4MB)  - ≤1KB 对象        │
│  Safe Heap           - 大对象 + 保护    │
└─────────────────────────────────────────┘
```

**性能指标**：
- 微小对象分配：**0.05μs**
- 小对象分配：**0.10μs**
- 中对象分配：**0.20μs**
- 批量释放：**O(1)** 时间复杂度

**安全特性**：
- 🔒 红区保护（Red Zone）
- 🔒 Canary 值检测
- 🔒 自动作用域释放
- 🔒 逃逸分析（自动提升到堆）

#### 🧠 VO (Virtual On-site) 系统

高效的数据和代码缓存机制：

```kaula
vo create(100)              # 创建 VO 模块
vo_data_load(vo, 1, data)   # 加载数据到缓存
vo_code_load(vo, -1, fn)    # 加载代码到缓存
vo_associate(vo, 1, -1)     # 关联数据和代码
result = vo_access(vo, 1)   # 访问（自动执行关联代码）
```

**技术细节**：
- 支持最多 **2048** 个缓存项
- LRU 淘汰算法
- 纳秒级访问时间戳
- 数据和代码自动关联执行

#### 🔄 Spend/Call 机制

动态组件管理和调用：

```kaula
spend(component1, component2):
    call target1:
        # 处理逻辑
    call target2:
        # 处理逻辑
```

**实现原理**：
- 组件栈式管理
- 倒序调用（LIFO）
- 自动状态恢复

#### 📊 三级优先级队列

任务调度系统：

```
Priority 0 (HIGH)   → 实时任务
Priority 1 (MEDIUM) → 普通任务
Priority 2 (LOW)    → 后台任务
```

**特性**：
- 每级独立队列（容量 100000）
- 批量添加/执行优化
- 空穴管理（减少内存拷贝）

#### ⏱️ 高精度时间测量

基于 Windows QueryPerformanceCounter：

- **纳秒级** 精度
- 自动频率检测
- 零除保护
- 支持周期转换（NS/US/MS）

### 3. 标准库

超过 **400+** 个标准函数，涵盖：

#### 核心模块

| 模块 | 函数数 | 核心功能 |
|------|--------|----------|
| **base** | 26 | 类型转换、比较、类型判断 |
| **memory** | 28 | KMM V2、快速分配器、内存池 |
| **string** | 44 | 字符串创建、操作、搜索、转换 |
| **io** | 33 | 控制台 I/O、文件操作、路径处理 |
| **math** | 58 | 数学函数、三角函数、随机数 |
| **container** | 37 | Vector、LinkedList、HashMap、Stack |
| **concurrent** | 41 | 线程、互斥锁、条件变量、信号量、原子操作 |
| **async** | 22 | 异步任务、事件循环、协程 |
| **system** | 42 | 系统信息、进程管理、环境变量 |
| **task** | 27 | 任务创建、优先级队列调度 |
| **vo** | 7 | VO 系统高级接口 |
| **error** | 11 | 错误创建、处理、报告 |

#### 标准库特性

- ✅ **模块化设计**：每个模块独立编译
- ✅ **统一错误处理**：标准化错误码和消息
- ✅ **Windows 优化**：使用 Windows API 实现
- ✅ **类型安全**：强类型接口定义
- ✅ **零成本抽象**：内联函数优化

### 4. 语言特性

#### 基本语法

```kaula
# 变量声明（C 式类型在前）
int i = 0
float x = 3.14
string? name = null  # 可空类型

# 函数定义
fn add(int a, int b) int:
    return a + b

# 控制流
if condition:
    # ...
else:
    # ...

while condition:
    # ...

for (int i = 0; i < 10; i++):
    # ...

# 面向对象
class MyClass implements IInterface:
    int field;
    
    fn method(int param) int:
        return param
    
    constructor MyClass(int value):
        this.field = value

# 结构体
struct Point:
    float x;
    float y;

# 接口
interface IShape:
    fn area() float;
    fn perimeter() float;
```

#### 高级特性

**1. VO 系统**
```kaula
vo myVO(100):
    data = create_data()
    code = process_function
    associate(data, code)
```

**2. Spend/Call**
```kaula
spend(components):
    call processor:
        # 处理组件
```

**3. 前缀系统**
```kaula
prefix "myprefix":
    # 前缀作用域内的代码
```

**4. 任务调度**
```kaula
task(0, process_func, arg)  # 高优先级任务
```

**5. 导入模块**
```kaula
import std.io
import std.concurrent
import std.memory
```

#### 表达式系统

- ✅ 二元表达式：`+ - * / % == != < > <= >= && ||`
- ✅ 函数调用：`fn(arg1, arg2)`
- ✅ 索引表达式：`array[index]`
- ✅ 成员访问：`object.member`
- ✅ 多级访问：`std.io.println`
- ✅ 前缀引用：`$variable`
- ✅ 分组表达式：`(expression)`

---

## 🔧 技术亮点

### 1. 性能优化

| 优化技术 | 效果 |
|----------|------|
| 三层 Arena 分配 | 微小对象 0.05μs |
| O(1) 批量释放 | 作用域退出零开销 |
| 内联函数 | 减少调用开销 |
| 迭代式解析 | 避免栈溢出 |
| 输入长度缓存 | 减少重复计算 |
| 空穴管理 | 减少内存拷贝 |

### 2. 安全性保障

- ✅ 类型检查和语义验证
- ✅ 作用域自动管理
- ✅ 内存红区保护
- ✅ Canary 值检测
- ✅ 详细的错误报告和修复建议

### 3. 可扩展性

- ✅ 模块化编译器架构
- ✅ 插件系统支持
- ✅ 第三方库配置
- ✅ 模板化代码生成

---

## 🚀 快速开始

### 环境要求

- **编译器开发**：Go 1.21.0+
- **运行时开发**：MSVC（推荐）或 MinGW
- **操作系统**：Windows 10/11

### 编译项目

```bash
# 1. 编译编译器
cd kaula-compiler
go build -o kaulac.exe cmd/kaulac/main.go

# 2. 编译标准库
cd ../std
build.bat

# 3. 编译运行时
cd ../src
# 使用 MSVC
cl /O2 /I. *.c

# 4. 编译 Kaula 程序
kaulac.exe your_program.kaula
```

### 示例程序

```kaula
import std.io

fn main():
    std.io.println("Hello, Kaula!")
    
    int sum = 0
    for (int i = 1; i <= 100; i++):
        sum = sum + i
    
    std.io.println("Sum: ", sum)
```

---

## 📊 性能对比

| 操作 | Kaula | C | 说明 |
|------|-------|---|------|
| 微小对象分配 | 0.05μs | 0.04μs | 接近原生性能 |
| 字符串连接 | 1.2x | 1.0x | 优化后的实现 |
| 任务调度 | 0.8μs | - | 三级优先级队列 |
| VO 访问 | 15ns | - | 缓存命中场景 |

---

## 🎯 适用场景

- ✅ 系统级编程（操作系统、驱动程序）
- ✅ 高性能计算（科学计算、游戏引擎）
- ✅ 实时系统（工业控制、嵌入式）
- ✅ Windows 平台应用开发
- ✅ 需要精细内存管理的场景

---

## 📚 文档

- [编译器实现文档](kaula-compiler/README.md)
- [标准库 API 文档](std/README.md)
- [语言规范](docs/language-spec.md)
- [示例代码](examples/)

---

## 🤝 贡献指南

欢迎贡献！请遵循以下步骤：

1. Fork 本项目
2. 创建特性分支 (`git checkout -b feature/AmazingFeature`)
3. 提交更改 (`git commit -m 'Add some AmazingFeature'`)
4. 推送到分支 (`git push origin feature/AmazingFeature`)
5. 开启 Pull Request

---

## 📄 许可证

本项目采用 MIT 许可证 - 查看 [LICENSE](LICENSE) 文件了解详情

---

## 👥 团队

Kaula 语言由核心团队开发和维护，专注于 Windows 平台的性能优化和系统级编程体验。

---

## 🔮 路线图

- [ ] macOS 和 Linux 支持
- [ ] GC 垃圾回收（可选）
- [ ] 协程支持
- [ ] 更好的 IDE 集成
- [ ] 包管理器
- [ ] 性能分析工具

---

<div align="center">

**Kaula - 为性能而生**

Made with ❤️ for Windows

</div>
