# Kaula 文档索引

本目录包含 Kaula 全部文档。文档分为三类，**按读者目的选择**：

| 分类 | 适合谁 | 入口 |
|------|--------|------|
| 编译器文档（本页） | 读源码、修改或扩展编译器的人 | [编译器架构](compiler-architecture.md) |
| 工具链文档 | 用命令行工具做构建、格式化、调试、打包的人 | [tools/README.md](tools/README.md) |
| 语法教程文档 | 写 Kaula 代码的人 | [tutorials/README.md](tutorials/README.md) |

项目主页与安装说明见 [根 README](../README.md)。

## 编译器文档

了解 Kaula 编译器（Go 实现）各组件的内部机制。

### 架构与配置

- [编译器架构](compiler-architecture.md) - 整体架构、编译流程、目录结构
- [编译配置文件 (kaula.json)](build-config.md) - 配置系统、所有参数说明

### 编译流水线组件

- [词法分析器 (Lexer)](lexer.md) - Token 类型、扫描策略、API
- [语法分析器 (Parser)](parser.md) - 语法规则、递归下降解析、错误恢复
- [抽象语法树 (AST)](ast.md) - 节点类型、层次结构、遍历机制
- [语义分析](semantic-analysis.md) - 两遍分析、类型检查、符号管理
- [符号表 (Symbol Table)](symbol-table.md) - 层次化作用域、泛型支持
- [代码生成器 (Codegen)](code-generation.md) - 模块化生成、模板系统、SOR 集成

### 编译系统与内存

- [SOR Sub-structural Ownership](sor-system.md) - 所有权原语、分析流程、内存优化
- [核心运行时](core-runtime.md) - 并发内核（conc fn lowering、IoDriver、阻塞车道、epoch 回收）与 KMM V4（slab / per-thread heap）
- [增量编译缓存](cache-system.md) - SHA-256 验证、原子写入、自动清理
- [超时与内存控制](timeout-memory.md) - 资源监控、阶段统计、调试输出

### 集成与扩展

- [标准库集成](stdlib-integration.md) - 配置驱动、自动发现、第三方库
- [`#[]` 高级属性扩展](attributes.md) - #[test] 测试、#[derive] 派生、#[command] 命令行解析
- [错误处理](error-handling.md) - 错误类型、源码上下文、修复建议
- [数值格式化选项](format-options.md) - std.format 的 FormatOptions 全部选项
- [裸机开发指南](bare-metal.md) - freestanding 模式、内联汇编、原子操作、位域、extern 声明
- [freestanding 库与全链路验证](freestanding.md) - 零依赖模块、链接模型、含 QEMU 实机引导的 7 层验证、已知边界

## 工具链文档

见 [tools/README.md](tools/README.md)，按用途分类收录全部命令行工具：

- [构建与部署指南](deployment.md) - 工具链构建、安装布局、程序分发、交叉编译与裸机部署
- [编译器 kaulac](tools/kaulac.md) - 全参数编译、缓存管理、pkglib 管理、裸机模式
- [构建脚本 toolkit_build.py](tools/toolkit-build.md) - 一键全量/分目标构建
- [代码格式化工具 (kaulafmt)](tools/kaulafmt.md) - 格式化规则、使用方法
- [AST 转储 (dumpast)](tools/dumpast.md) - 调试解析器的 AST 树输出
- [语义负载测试 (loadtest)](tools/loadtest.md) - 语义分析验证

## 语法教程文档

见 [tutorials/README.md](tutorials/README.md)，共 16 篇，建议按顺序阅读：

- [入门指南：Hello, Kaula!](tutorials/getting-started.md) - 环境搭建与第一个程序
- [变量与类型](tutorials/variables-and-types.md) - 内置类型、声明方式、FFI 专用类型记法
- [数值类型名称](tutorials/numeric-type-names.md) - 整型/浮点宽度与命名约定
- [函数](tutorials/functions.md) - 函数定义、参数与调用
- [控制流](tutorials/control-flow.md) - 条件分支与循环
- [结构体与枚举](tutorials/structs-and-enums.md) - 数据类型定义
- [类与接口](tutorials/class-and-interface.md) - 面向对象与抽象
- [模式匹配 (match)](tutorials/match.md) - 解构匹配与分支
- [泛型](tutorials/generics.md) - 类型参数与约束
- [指针与可空类型](tutorials/pointers.md) - 指针语义与空值处理
- [SOR Sub-structural Ownership](tutorials/sor-ownership.md) - 编译期内存安全机制
- [前缀系统](tutorials/prefix-system.md) - 存储前缀与内存作用域
- [属性注解与编译期反射](tutorials/attributes-and-compiletime.md) - `#[]` 属性与编译期求值
- [Extern 声明与 FFI](tutorials/extern-and-ffi.md) - 外部 C/C++ 互操作
- [std 错误处理与可捕获 Panic](tutorials/error-and-panic.md) - Result/Option、Error 链、errno 桥接、panic_protect 捕获恢复
- [标准库使用](tutorials/stdlib-usage.md) - 常用 std 模块用法

## 快速导航

### 想了解编译流程？

从 [编译器架构](compiler-architecture.md) 开始，了解从源代码到可执行文件的完整流程。

### 想了解语言语法？

从 [tutorials/](tutorials/README.md) 的入门指南开始逐篇学习；只想查语法规则可直接看 [语法分析器 (Parser)](parser.md)。

### 想了解内存管理？

查看 [SOR Sub-structural Ownership](sor-system.md) 和 [核心运行时](core-runtime.md)。

KMM V4 是 Kaula 的默认内存分配器，基于 per-thread heap + bump allocation，在基准测试中比 malloc/free 快 **2-20x**：

| 场景 | KMM V4 | malloc/free | 加速比 |
|------|--------|-------------|--------|
| 纯分配吞吐量（64B, 1M 次） | 3.5 ms | 72.7 ms | **20.5x** |
| 16B 小对象分配+回收 | 51.8 ms | 634.3 ms | **12.2x** |
| 64B 对象分配+回收 | 65.9 ms | 661.5 ms | **10.0x** |
| 混合负载（16~1024B 交替） | 287.9 ms | 658.1 ms | **2.2x** |

相关文档：
- [代码生成器](code-generation.md) - KMM inline 机制、作用域插入策略
- [裸机开发指南](bare-metal.md) - 内建引导（--boot pvh/custom）、KMM V4 静态池模式（freestanding）
- [SOR Sub-structural Ownership](sor-system.md) - SOR 与 KMM 集成、内存分配决策

### 想了解如何扩展编译器？

查看 [代码生成器 (Codegen)](code-generation.md) 中的插件系统部分。

### 想做裸机/系统级开发？

查看 [裸机开发指南](bare-metal.md)，了解 freestanding 模式、内联汇编、原子操作、位域、extern 声明等特性。

## 文档约定

- 代码示例使用 Kaula 语法或 Go 语法
- API 说明基于实际源码
- 类型定义直接引用源码中的结构体
