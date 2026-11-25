# Kaula 贡献指南（CONTRIBUTING）

感谢参与 Kaula！本文是面向**人类与 AI 贡献者**的统一贡献规范。

> **AI 贡献者必须先阅读的第一份文件：[skills/kaula-ai-contribution.md](skills/kaula-ai-contribution.md)**
> 这是仓库内置的「AI 代码共享 Skill」，定义了 AI 在本仓库产出代码、修复 bug、
> 补全文档前必须遵守的强制流程（读 skill → 遵守输出契约 → 测试 → 提交）。
> AI Agent 在开始任何工作前，应自觉进入 `skills/` 目录阅读相关 skill，而不是凭
> 通用经验直接动手。

## 0. AI Skills 索引（置顶）

全部 AI 定制 skill 位于 [`skills/`](skills/)，各自解决一类任务，**开工前按需加载**：

| Skill | 适用场景 |
|-------|----------|
| [kaula-ai-contribution.md](skills/kaula-ai-contribution.md) | **AI 代码贡献总纲（必须最先读）**：贡献强制流程、质量门槛、提交与文档要求 |
| [kaula-output-contract.md](skills/kaula-output-contract.md) | AI 输出 Kaula (`.kl`) 代码前的强制自检契约：语法、格式、布尔规则、round-trip |
| [kaula-code-style.md](skills/kaula-code-style.md) | `.kl` 代码风格规范：命名、大括号、import、注释（与根目录 `codeStyle.md` 同源） |
| [kaula-learning-path.md](skills/kaula-learning-path.md) | AI/新人学习 Kaula 语言与编译器内部实现的阅读路径 |
| [kaula-toolchain.md](skills/kaula-toolchain.md) | Kaula 工具链使用：kaulac、fmt、pkg、workspace、dumpast、构建与测试脚本 |
| [kaula-dev-workflow.md](skills/kaula-dev-workflow.md) | 仓库工程流程：结构、构建、测试、commit 规范、发版 |

新增 skill 的规则见 [skills/kaula-ai-contribution.md](skills/kaula-ai-contribution.md) 第 6 节。

---

## 1. 仓库构成（决定你的改动属于哪一层）

| 目录 | 内容 | 语言 |
|------|------|------|
| `compiler/` | 编译器（lexer/parser/sema/sor/codegen/cache/pkgmgr/formatter 等） | Go 1.23+，模块名 `kaula` |
| `src/` | C 运行时、KMM V4 内存管理器 | C |
| `std/` | 标准库头文件与注册 | C 头 + `compiler/stdlib.json` |
| `freestanding/` | 无依赖标准库（裸机场景） | C |
| `test/` | 语言测试语料、基准、负向语料（expect-fail） | `.kl` / C++ / Python |
| `docs/` | 编译器文档、语法教程、工具链文档 | Markdown |
| `skills/` | AI agent 配置（本仓库的 skill 体系） | Markdown |
| `scripts/` | 测试运行器、语料校验、打包、版本管理 | Python |
| `toolkit_build.py` | 一键构建入口（编译器 + 标准库 + 运行时） | Python |

改动落在哪一层，就该在哪一层补测试与文档（见第 4 节质量门槛）。

## 2. 开发环境与构建

```bash
# 依赖：Go >= 1.23、Clang/GCC、Python 3
python toolkit_build.py                # 全量构建
python toolkit_build.py --release      # Release 构建
python toolkit_build.py --target compiler   # 仅编译器
python toolkit_build.py --clean        # 清理
```

详见 [docs/tools/toolkit-build.md](docs/tools/toolkit-build.md)。

## 3. 测试（提交前必须全绿）

```bash
cd compiler && go test ./... && go vet ./...      # 编译器单元测试
python scripts/run_tests.py --quick               # 语言语料（双分配器模式）
python scripts/run_tests.py --filter <关键词>      # 只跑相关语料
python scripts/verify_tutorials.py                # 教程文档示例编译运行
kaulac fmt check <file>.kl                        # 格式检查（改动 .kl 时）
```

- `scripts/run_tests.py` 是语言级回归守门人：每个 `test/*.kl` 在 default 与
  `--sor` 两种分配器模式下都要通过。
- 新增语言特性必须同时补正向语料与负向语料（`test/` 下 expect-fail 标注）。
- 性能相关改动（codegen/KMM）附带基准数据（`test/bench/`、`alloc_bench` 等）。

## 4. 质量门槛（零容忍）

1. **无退化、无回归**：提交前上述测试全绿；修 bug 必须先补一个能复现 bug 的
   回归语料，再让修复通过它。
2. **数据收敛**：涉及内存管理 / 性能的改动，用基准语料验证指标收敛，不允许
   「看似更快实则抖动」的提交进入主干。
3. **文档同步**：新功能或行为变更必须同步更新对应文档
   （教程 → `docs/tutorials/`，工具 → `docs/tools/`，内部机制 → `docs/` 对应文档），
   并在 [CHANGELOG.md](CHANGELOG.md) 的 `## [Unreleased]` 下补条目。
4. **格式稳定**：`.kl` 文件改动需通过 `kaulac fmt`，且 fmt 二次运行结果与首次一致。

## 5. 提交规范

### Commit Message

```
<type>(<scope>): <description>

[optional body]
```

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

scope 常用值：`kaulac`、`codegen`、`sema`、`parser`、`sor`、`kmm`、`fmt`、`testrunner`、`std`、`docs`。
示例：`fix(kaulac): 可空形参 ? 后缀未擦除生成非法 C`。

### 分支与同步

- 功能分支：`feat/xxx`；修复分支：`fix/xxx`；文档分支：`docs/xxx`。
- **GitHub 是唯一主仓**（`github.com/NOVAzhe/Kaula_language_compiler`）：
  所有提交先推 GitHub，Gitee（`gitee.com/kaula-universe/kaula`）由流水线
  （`.github/workflows/sync-gitee.yml` / `org-sync.yml`）自动同步，
  **不要直接向 Gitee 主干提交**。详见 [docs/gitee-sync.md](docs/gitee-sync.md)。

### Pull Request

- 一个 PR 只做一件事；标题遵循 commit message 格式。
- PR 描述必须包含：改动动机、测试结果（贴出测试命令与通过情况）、
  文档/CHANGELOG 更新说明。

## 6. 文档规范

- 中文为主，术语保留英文（如 Sub-structural Ownership、SSO）。
- 代码示例使用 `kaula` 高亮块；**所有教程示例必须能通过
  `scripts/verify_tutorials.py` 编译运行**——教程示例失效即文档 bug。
- API/内部机制文档以实际源码为准，不允许写「计划中」的接口。

## 7. 行为准则

- 保持友善、对事不对人；评审意见针对代码而非作者。
- 对 AI 生成的贡献与人类贡献适用同一套质量门槛，AI 不享有豁免。
