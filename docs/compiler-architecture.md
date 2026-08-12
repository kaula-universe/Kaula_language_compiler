# Kaula 编译器架构

Kaula 编译器使用 Go 1.21+ 实现，零外部依赖，将 `.kl` 源文件编译为 C 代码，再通过 Clang 生成可执行文件。

## 编译流程

```
源代码(.kl)
    │
    ▼
┌─────────────┐
│  词法分析    │  lexer/lexer.go - 状态机实现
│  (Lexer)    │  → Token 序列
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  语法分析    │  parser/parser.go - 递归下降解析
│  (Parser)   │  → AST (抽象语法树)
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  语义分析    │  sema/sema.go - 两遍分析
│  (Semantic) │  → 类型检查、符号表、作用域验证
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  SOR 分析    │  sor/ - 子所有权释放分析（可选）
│  (可选)      │  → 内存分配策略、所有权验证
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  代码生成    │  codegen/ - 模块化生成器
│  (Codegen)  │  → C 源代码
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  Clang 编译  │  调用外部编译器
│             │  → 可执行文件
└─────────────┘
```

## 并发编译管线

编译器支持多阶段并发处理：

```go
// cmd/kaulac/main.go 中的并发管线
Stage 1: Lexing + Parsing        // 并发处理多个文件
Stage 2: Semantic Analysis       // 并发语义分析
Stage 2.5: SOR Analysis          // 可选的所有权分析
Stage 3: Code Generation + C 编译 // 并发生成 C 代码并调用 Clang
```

**典型编译时间**：~2.9s（首次编译） / ~2.6s（缓存命中）

## 目录结构

```
compiler/
├── cmd/
│   ├── kaulac/main.go           # 编译器主入口
│   └── kaulafmt/main.go         # 代码格式化工具
├── internal/
│   ├── ast/ast.go               # AST 节点定义
│   ├── lexer/lexer.go           # 词法分析器
│   ├── parser/parser.go         # 语法分析器
│   ├── sema/sema.go             # 语义分析器（主）
│   ├── semantic/semantic.go     # 语义分析器（简化版）
│   ├── codegen/                 # C 代码生成器
│   │   ├── codegen.go           # 核心生成器
│   │   ├── generator.go         # 生成器接口
│   │   ├── typegen.go           # 类型生成
│   │   ├── funcgen.go           # 函数生成
│   │   ├── exprgen.go           # 表达式生成
│   │   ├── stmtgen.go           # 语句生成
│   │   ├── template.go          # 模板管理
│   │   ├── plugin.go            # 插件系统
│   │   ├── sor_codegen.go       # SOR 集成
│   │   └── sourcemap.go         # 源码映射
│   ├── cache/cache.go           # 增量编译缓存
│   ├── compiler/compiler.go     # 编译入口
│   ├── config/config.go         # 配置管理
│   ├── core/                    # 核心运行时特性（Go 层）
│   ├── errors/errors.go         # 错误处理
│   ├── symbol/symbol.go         # 符号表
│   ├── stdlib/                  # 标准库集成
│   ├── sor/                     # SOR 子所有权释放
│   ├── pkgmgr/mirror.go         # 包镜像管理
│   └── timeout/timeout.go       # 超时与内存控制
├── templates/main.c.tmpl        # 代码生成模板
├── stdlib.json                  # 标准库函数签名
└── go.mod                       # Go 模块定义
```

## 核心设计原则

1. **零外部依赖**：整个编译器仅使用 Go 标准库
2. **模块化代码生成**：类型/函数/表达式/语句生成器分离，支持插件扩展
3. **增量编译**：基于 SHA-256 的缓存验证，跳过未变化的代码生成
4. **资源安全**：内置内存监控和超时保护，防止编译器资源耗尽
5. **SOR 所有权分析**：可选的编译期所有权验证，类似 Rust 但更轻量
6. **跨平台**：Go 实现 + C 运行时，支持 Windows/Linux/macOS
