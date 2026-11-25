---
name: kaula-learning-path
description: "Use when an AI or new contributor needs to learn the Kaula language, its runtime semantics, or the compiler internals. Gives a staged reading path through docs/tutorials/, docs/, and key source files, with milestones to verify understanding."
---

# Kaula 学习路径（面向 AI 与新贡献者）

按阶段阅读，每个阶段末尾有自检里程碑。**不要跳读**：Kaula 的 SOR、spend/call、
前缀系统相互耦合，缺一环就会写出看似正确实则违规的代码。

## 阶段 1：语言语法（写 `.kl` 代码的前提）

按 `docs/tutorials/` 编号顺序读：

1. getting-started → variables-and-types → numeric-type-names → functions →
   control-flow → structs-and-enums
2. class-and-interface → match → generics → pointers（注意编译期可空 `T?`）
3. literals（切片/定长数组/结构体字面量与类型推导）

**里程碑**：能独立写出含结构体、match、泛型函数的可编译程序，并通过
[kaula-output-contract.md](kaula-output-contract.md) 全部检查项。

## 阶段 2：内存与安全模型（Kaula 的灵魂）

这是 Kaula 区别于其他语言的核心，必须完整读：

| 机制 | 文档 | 关键点 |
|------|------|--------|
| SOR（Sub-structural Ownership） | tutorials/sor-ownership.md、docs/sor-system.md | 编译期消除数据竞争与悬垂指针，无运行时开销 |
| 前缀系统（存储前缀与内存作用域） | tutorials/prefix-system.md | 存储前缀决定分配与回收位置 |
| spend / call 强制消费流 | tutorials/spend-and-call.md | 编译期强制全消费，漏消费即编译错误 |
| 错误处理与可捕获 Panic | tutorials/error-and-panic.md | Result/Option、Error 链、panic 恢复 |
| KMM V4 内存管理器 | README「KMM V4」、src/kmm_scoped_allocator_v4.h 注释 | 运行时侧作用域分配器，与编译器插入策略配合 |

**里程碑**：能解释「一段代码为什么不需要手动 free / 为什么不能在此处别名」，
并能读懂 `test/` 下 mem*.kl 系列语料的意图。

## 阶段 3：FFI 与标准库

- extern-and-ffi.md：C/C++ 互操作、FFI 专用类型记法
- stdlib-usage.md + docs/stdlib-integration.md：std 模块与 `compiler/stdlib.json` 注册关系
- freestanding.md：无依赖标准库与裸机场景（docs/bare-metal.md）

**里程碑**：能声明 extern C 函数并从 `.kl` 调用；知道新增 std 模块要同时改
`std/`、`stdlib.json`、文档三处。

## 阶段 4：编译器内部（改 compiler/ 的前提）

按数据流顺序读 `docs/`：

```
lexer.md → parser.md → ast.md → symbol-table.md → semantic-analysis.md
        → sor 侧 sor-system.md → code-generation.md → cache-system.md
```

配合 `docs/compiler-architecture.md` 总览。关键源码入口：

- `compiler/cmd/kaulac/` — CLI 入口与子命令分发
- `compiler/internal/{lexer,parser,sema,codegen,sor,cache}` — 各阶段实现
- `compiler/internal/formatter/` — `kaulac fmt`（输出即风格基准）

**里程碑**：能对一个 `.kl` 输入说出它经过的完整流水线（词法→语法→语义→SOR→
代码生成→C 编译），并知道每个阶段报错的形态。

## 阶段 5：工具链与工程流程

- [kaula-toolchain.md](kaula-toolchain.md) — 命令速查
- docs/build-config.md — `kaula.json` 配置
- docs/tools/package-management.md — pkglib 依赖
- [kaula-dev-workflow.md](kaula-dev-workflow.md) — 构建、测试、提交、发版

**里程碑**：能独立跑通「构建 → 改语料 → run_tests → 提交」完整循环。

## 学习方法约束（AI 专用）

- 回答语言问题时，答案必须能对应到某个教程/文档段落；文档没有的行为不要编造，
  去读 `compiler/internal/` 源码验证，验证不了就明确说「未确认」。
- 学到的语言规则如果与 [kaula-output-contract.md](kaula-output-contract.md) /
  [kaula-code-style.md](kaula-code-style.md) 冲突，以实现与测试语料为准，
  并在对应 skill 提修正（见 kaula-ai-contribution.md §6）。
