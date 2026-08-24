---
name: kaula-dev-workflow
description: "Use when contributing to the Kaula project. Covers build, test, commit, and PR conventions."
---

# Kaula 开发流程与工程规范

## 1. 项目结构

```
kaula/
├── compiler/                  # Go 编译器（模块名: kaula）
│   ├── cmd/kaulac/           # 主编译器入口
│   ├── cmd/kaulafmt/         # 格式化工具入口
│   ├── internal/             # 内部包
│   │   ├── lexer/            # 词法分析
│   │   ├── parser/           # 语法分析
│   │   ├── sema/             # 语义分析
│   │   ├── codegen/          # C 代码生成
│   │   ├── sor/              # Sub-structural Ownership
│   │   ├── cache/            # 增量编译缓存
│   │   ├── pkgmgr/           # 包管理
│   │   └── ...
│   ├── go.mod                # module kaula
│   └── stdlib.json           # 标准库配置
├── src/                      # C 运行时
│   ├── runtime/              # 运行时实现
│   └── kmm_v4/               # 内存管理器
├── std/                      # 标准库头文件
├── freestanding/             # 无依赖标准库
├── docs/                     # 文档
├── tools/                    # 调试工具
├── scripts/                  # 构建辅助脚本
├── skills/                   # AI agent 配置
├── toolkit_build.py          # 主构建入口
└── codeStyle.md              # 代码规范
```

## 2. 构建命令

```bash
# 全量构建
python toolkit_build.py

# Release 构建
python toolkit_build.py --release

# 仅构建编译器
python toolkit_build.py --target compiler

# 仅构建标准库
python toolkit_build.py --target std

# 清理
python toolkit_build.py --clean
```

## 3. 测试

```bash
# Go 单元测试
cd compiler && go test ./...

# 运行单个测试
cd compiler && go test ./internal/parser/ -run TestParseFunction

# 格式化检查
kaulafmt -c <file>.kl
```

## 4. Git 规范

### Commit Message 格式

```
<type>: <description>

[optional body]
```

### Type 类型

| Type | 说明 |
|------|------|
| `feat` | 新功能 |
| `fix` | 修复 bug |
| `docs` | 文档更新 |
| `style` | 代码格式（不影响功能） |
| `refactor` | 重构 |
| `perf` | 性能优化 |
| `test` | 测试相关 |
| `chore` | 构建/工具/依赖变更 |

### 示例

```
feat: add workspace topological sort
fix: resolve parser panic on empty block
docs: update getting-started tutorial
refactor: extract codegen into separate files
chore: upgrade Go to 1.23
```

### 分支命名

| 类型 | 格式 |
|------|------|
| 功能 | `feat/xxx` |
| 修复 | `fix/xxx` |
| 文档 | `docs/xxx` |
| 发布 | `v1.0.xxx` (tag) |

## 5. 发布流程

```bash
# 1. 更新版本
python scripts/version.py --update

# 2. 本地构建验证
python toolkit_build.py --release

# 3. 提交
git add -A
git commit -m "chore: release v1.0.xxx"

# 4. 打 tag
git tag -a v1.0.xxx -m "Release v1.0.xxx"

# 5. 推送
git push origin master
git push origin v1.0.xxx
```

Tag 推送后 GitHub Actions 自动构建三平台并发布 Release。

## 6. Go 代码规范

- 模块名: `kaula`（不要用 `compiler`）
- 从 `compiler/` 根目录运行 `go build`
- 使用 `go vet ./...` 检查代码
- 新增包必须在 `internal/` 下
- 导入路径: `kaula/internal/xxx`

## 7. 文档规范

- 中文为主，术语保留英文（如 Sub-structural Ownership）
- 代码示例使用 `kaula` 语言高亮块
- API 文档基于实际源码
- 教程文档放在 `docs/tutorials/`
- 工具文档放在 `docs/tools/`

## 8. 新增标准库模块

1. 在 `std/` 下创建模块目录
2. 头文件命名: `std/<module>/<module>.h`
3. 在 `stdlib.json` 中注册
4. 添加文档到 `docs/stdlib-integration.md`

## 9. 常用调试命令

```bash
# 查看编译器 AST
kaulac --dump-ast main.kl

# 查看生成的 C 代码
kaulac --preprocess main.kl

# 查看版本
kaulac --version

# 格式化
kaulafmt -w main.kl
```
