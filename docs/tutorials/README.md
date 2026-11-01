# Kaula 语法教程

本目录是 Kaula 的语法教程，从入门到标准库实践，建议按编号顺序阅读。

想了解编译器内部实现、命令行工具或构建流程，请看[编译器文档](../README.md)与[工具链文档](../tools/README.md)。

## 教程索引

| # | 教程 | 说明 |
|---|------|------|
| 1 | [入门指南：Hello, Kaula!](getting-started.md) | 环境搭建与第一个程序 |
| 2 | [变量与类型](variables-and-types.md) | 内置类型、声明方式、FFI 专用类型记法 |
| 3 | [数值类型名称](numeric-type-names.md) | 整型/浮点宽度与命名约定 |
| 4 | [函数](functions.md) | 函数定义、参数与调用 |
| 5 | [控制流](control-flow.md) | 条件分支与循环 |
| 6 | [结构体与枚举](structs-and-enums.md) | 数据类型定义 |
| 7 | [类与接口](class-and-interface.md) | 面向对象与抽象 |
| 8 | [模式匹配 (match)](match.md) | 解构匹配与分支 |
| 9 | [泛型](generics.md) | 类型参数与约束 |
| 10 | [指针与可空类型](pointers.md) | 指针语义与空值处理 |
| 11 | [SOR Sub-structural Ownership](sor-ownership.md) | 编译期内存安全机制 |
| 12 | [前缀系统](prefix-system.md) | 存储前缀与内存作用域 |
| 13 | [属性注解与编译期反射](attributes-and-compiletime.md) | `#[]` 属性与编译期求值 |
| 14 | [Extern 声明与 FFI](extern-and-ffi.md) | 外部 C/C++ 互操作 |
| 15 | [std 错误处理与可捕获 Panic](error-and-panic.md) | Result/Option、Error 链、panic 捕获恢复 |
| 16 | [标准库使用](stdlib-usage.md) | 常用 std 模块用法 |
| 17 | [强制消费流（spend / call）](spend-and-call.md) | 编译期强制全消费：语法、错误码、运行时 API |

## 快速导航

- **第一次接触 Kaula** → 从[入门指南](getting-started.md)开始
- **想知道有哪些类型、类型多宽** → [变量与类型](variables-and-types.md)、[数值类型名称](numeric-type-names.md)
- **数据结构怎么定义** → [结构体与枚举](structs-and-enums.md)、[类与接口](class-and-interface.md)
- **分支逻辑怎么写** → [控制流](control-flow.md)、[模式匹配](match.md)
- **内存安全从哪来** → [SOR 所有权](sor-ownership.md)、[指针与可空类型](pointers.md)、[前缀系统](prefix-system.md)
- **要调用 C 库** → [Extern 声明与 FFI](extern-and-ffi.md)
- **要处理错误** → [std 错误处理与可捕获 Panic](error-and-panic.md)
- **要写测试/命令行工具** → [属性注解与编译期反射](attributes-and-compiletime.md)（`#[test]`、`#[command]`）
- **要保证资源/分支"一个都不漏"** → [强制消费流](spend-and-call.md)

## 相关文档

- [标准库集成](../stdlib-integration.md) - 标准库如何被发现、配置与链接
- [SOR 所有权系统](../sor-system.md) - SOR 分析引擎的实现细节
- [`#[]` 高级属性扩展](../attributes.md) - 属性的完整能力清单
- [Kaula 构建与部署指南](../deployment.md) - 工具链构建与交叉编译

## 多语言版本

使用翻译脚本生成本地化教程：

```bash
python ../tools/translate.py --lang en --input . --output en
```

支持语言：`zh`（中文，默认）、`en`（英文）、`ja`（日文）、`ko`（韩文）、`de`（德文）、`fr`（法文）