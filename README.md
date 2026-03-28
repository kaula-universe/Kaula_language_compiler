# Kaula 编程语言

<div align="center">

**高性能、系统级的编译型编程语言**

[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.21.0-00ADD8.svg?logo=go)](https://go.dev/)
[![Language](https://img.shields.io/badge/language-C-C.svg?logo=c)](https://en.wikipedia.org/wiki/C_(programming_language))

</div>

---

## 📖 项目概述

Kaula 是一款静态类型的编译型编程语言，采用 **Go 语言实现的编译器** 和 **C 语言实现的运行时系统**。

> ⚠️ **注意**：当前版本仅支持 Windows 操作系统

---

## 🏗️ 项目架构

```
Kaula/
├── kaula-compiler/          # 编译器（Go 实现）
│   ├── cmd/kaulac/          # 编译器命令行工具
│   ├── internal/
│   │   ├── lexer/           # 词法分析器
│   │   ├── parser/          # 语法分析器
│   │   ├── sema/            # 语义分析器
│   │   ├── ast/             # 抽象语法树
│   │   ├── codegen/         # C 代码生成器
│   │   ├── config/          # 配置管理
│   │   ├── errors/          # 错误处理
│   │   ├── stdlib/          # 标准库配置
│   │   └── symbol/          # 符号表
│   ├── templates/           # 代码生成模板
│   ├── stdlib.json          # 标准库函数签名
│   ├── thirdparty.json      # 第三方库配置
│   └── go.mod
├── src/                     # 运行时系统（C 实现）
│   ├── kaula.h              # 核心头文件
│   ├── kmm_scoped_allocator_v2.c  # 作用域分配器
│   ├── allocator.c          # 快速分配器
│   ├── vo.c                 # VO 系统
│   ├── spend_call.c         # Spend/Call机制
│   ├── queue.c              # 优先级队列
│   ├── prefix_system.c      # 前缀系统
│   ├── tree_system.c        # 树系统
│   └── time.c               # 时间测量
└── std/                     # 标准库（C 实现）
    ├── base/                # 基础类型
    ├── memory/              # 内存管理
    ├── concurrent/          # 并发原语
    ├── async/               # 异步操作
    ├── container/           # 容器
    ├── io/                  # I/O 操作
    ├── string/              # 字符串处理
    ├── math/                # 数学函数
    ├── system/              # 系统调用
    ├── task/                # 任务调度
    ├── vo/                  # VO 接口
    ├── prefix/              # 前缀接口
    ├── error/               # 错误处理
    └── gui/                 # GUI 支持
```

---

## ✨ 核心特性

### 1. 编译器

Kaula 编译器使用 **Go 1.21+** 实现，包含以下核心组件：

- **词法分析器（Lexer）**：状态机实现，支持关键字、标识符、字面量、运算符等
- **语法分析器（Parser）**：迭代式递归下降解析，构建抽象语法树
- **语义分析器（Semantic）**：符号表管理、类型检查、作用域验证
- **代码生成器（Codegen）**：基于模板生成 C 代码，模块化设计

**编译流程**：
```
源代码 → 词法分析 → 语法分析 → 语义分析 → 代码生成 → C 代码 → 机器码
```

### 2. 运行时系统

运行时使用 **C 语言** 实现，提供以下核心功能：

#### KMM V2 ScopedAllocator（作用域内存管理）

三层 Arena 分配系统：
- Tiny Arena (64KB) - 微小对象（≤16B）
- Small Arena (1MB) - 小对象（≤128B）
- Medium Arena (4MB) - 中对象（≤1KB）
- Safe Heap - 大对象（带保护）

支持 O(1) 批量释放，作用域退出时自动清理。

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

#### 高精度时间测量

基于 Windows QueryPerformanceCounter，纳秒级精度。

### 3. 标准库

提供超过 **400+** 个标准函数，包括：

| 模块 | 功能 |
|------|------|
| **base** | 类型转换、比较、类型判断 |
| **memory** | KMM V2、快速分配器、内存池 |
| **string** | 字符串创建、操作、搜索 |
| **io** | 控制台 I/O、文件操作 |
| **math** | 数学函数、三角函数 |
| **container** | Vector、HashMap、Stack |
| **concurrent** | 线程、互斥锁、条件变量 |
| **async** | 异步任务、事件循环 |
| **system** | 系统信息、进程管理 |
| **task** | 任务创建、调度 |
| **vo** | VO 系统接口 |
| **error** | 错误处理 |

### 4. 语言特性

#### 基本语法

```kaula
# 变量声明
int i = 0
float x = 3.14
string? name = null

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
```

#### 表达式

- 二元表达式：`+ - * / % == != < > <= >= && ||`
- 函数调用：`fn(arg1, arg2)`
- 索引表达式：`array[index]`
- 成员访问：`object.member`
- 前缀引用：`$variable`

---

## 🚀 快速开始

### 环境要求

- **编译器开发**：Go 1.21.0+
- **运行时开发**：MSVC（推荐）或 MinGW
- **操作系统**：Windows

### 编译步骤

```bash
# 1. 编译编译器
cd kaula-compiler
go build -o kaulac.exe cmd/kaulac/main.go

# 2. 编译标准库
cd ../std
build.bat

# 3. 编译运行时
cd ../src
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

## 📄 许可证

本项目采用 Apache License 2.0 许可证 - 查看 [LICENSE](LICENSE) 文件了解详情

---

<div align="center">

**Kaula - 为性能而生**

</div>
