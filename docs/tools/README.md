# Kaula 工具链文档

本目录收录 Kaula 附带的全部命令行工具，按用途分类放置，每个工具一份文档，覆盖所有用法。

## 工具索引

### 编译与构建

| 工具 | 文档 | 说明 |
|------|------|------|
| `kaulac` | [kaulac.md](kaulac.md) | 主编译器 CLI：编译、缓存管理、pkglib 管理、裸机与引导 |
| `toolkit_build.py` | [toolkit-build.md](toolkit-build.md) | 一键构建脚本：编译器、标准库、freestanding 库、运行时 |

### 代码质量

| 工具 | 文档 | 说明 |
|------|------|------|
| `kaulafmt` | [kaulafmt.md](kaulafmt.md) | 代码格式化工具（`.kl` 源文件） |

### 诊断与开发

| 工具 | 文档 | 说明 |
|------|------|------|
| `dumpast` | [dumpast.md](dumpast.md) | AST 转储工具：查看语法分析器输出 |
| `loadtest` | [loadtest.md](loadtest.md) | 语义分析负载测试工具 |

## 快速导航

- 日常编译程序 → [kaulac.md](kaulac.md)
- 一键构建整个工具链 → [toolkit-build.md](toolkit-build.md)
- 统一代码风格 → [kaulafmt.md](kaulafmt.md)
- 调试解析器/AST → [dumpast.md](dumpast.md)
- 验证语义分析 → [loadtest.md](loadtest.md)

## 构建后的产物位置

工具构建完成后位于 `build/bin/`（Windows 为 `.exe`）：

```
build/bin/
├── kaulac.exe      # 编译器
├── kaulafmt.exe    # 格式化工具
└── stdlib.json     # 标准库配置（编译器运行依赖，随构建输出）
```

`dumpast` / `loadtest` 为开发期工具，未纳入 `build/bin`，需在各自 `cmd/` 目录下用 `go build` 单独构建。