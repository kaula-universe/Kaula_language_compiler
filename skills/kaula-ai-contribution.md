---
name: kaula-ai-contribution
description: "Use ALWAYS before an AI agent contributes anything (code, fix, docs, tests) to the Kaula repository. Mandatory workflow: read skills first, follow the output contract, test, then commit. This is the pinned entry skill referenced at the top of CONTRIBUTING.md."
---

# Kaula AI 代码贡献总纲（AI 必读 / 置顶入口）

你是要向 Kaula 仓库贡献内容的 AI Agent。**本文件是你的强制入口**：
在产出任何代码、修复、文档之前，按本文件流程走完。不允许凭通用语言经验直接动手——
Kaula 是编译到 C 的自有语言，很多「通用习惯」在这里是错的。

## 1. 强制流程（不可跳步）

```
① 读 skill    → 按任务加载 skills/ 下对应 skill（见 §2 索引）
② 定位层级    → 判断改动落在哪一层（Go 编译器 / C 运行时 / .kl 语料 / 文档）
③ 遵守契约    → 涉及 .kl 代码输出：严格执行 skills/kaula-output-contract.md
④ 补测试      → 新特性/修复必须带语料；修 bug 先写能复现的回归语料
⑤ 补文档      → 新功能或行为变更同步 docs/ 对应文档 + CHANGELOG.md [Unreleased]
⑥ 验证全绿    → go test ./... && go vet ./...、scripts/run_tests.py --quick、
                scripts/verify_tutorials.py（改了教程时）、kaulac fmt check
⑦ 提交        → 遵循 commit 规范，一次提交一件事
```

## 2. Skill 加载索引（按任务选读）

| 你的任务 | 必读 skill |
|----------|-----------|
| 写/改 `.kl` 代码（测试语料、示例、标准库示例） | kaula-output-contract.md + kaula-code-style.md |
| 学习 Kaula 语言 / 回答语言问题 | kaula-learning-path.md |
| 运行编译器、构建、测试、调试 | kaula-toolchain.md |
| 改仓库工程流程、分支、发版 | kaula-dev-workflow.md |
| 改 Go 编译器内部 | kaula-dev-workflow.md §6 + docs/ 下对应模块文档（lexer/parser/sema/codegen 等） |
| 改 C 运行时 / KMM | docs/core-runtime.md、docs/timeout-memory.md、src/ 内现有注释风格 |

## 3. 质量门槛（零容忍，AI 不享豁免）

1. **无退化、无回归**：提交前测试全绿；不允许「顺带重构」。
2. **可复现**：bug 修复必须先有复现语料，修复后该语料进入回归集。
3. **数据收敛**：性能/内存改动附基准数据，指标收敛才可提交。
4. **文档同步**：缺文档的功能视为未完成的功能。
5. **诚实报告**：无法在当前环境运行的测试（如缺少 Go/Clang），必须明确说明
   「未在本机验证」及建议的验证命令，禁止声称「已测试」。

## 4. 提交与推送规范

- Commit message：`<type>(<scope>): <description>`，type ∈
  feat/fix/docs/style/refactor/perf/test/chore。
- **GitHub 是唯一主仓**：先推 GitHub（`github.com/NOVAzhe/Kaula_language_compiler`），
  Gitee 由流水线自动同步，不要直接推 Gitee。
- 每完成一轮独立、完整、已验证的工作即提交一次；不堆积半成品。
- 大文件/慢链路推送可用 `git config http.version HTTP/1.1` +
  `git config http.postBuffer 524288000` 缓解。

## 5. 常见错误（AI 最容易犯的）

- 用其他语言的风格写 `.kl`（分号、`/* */` 注释、`if ptr` 裸真值）→ 见 output contract。
- 在教程文档里写无法编译的示例 → `verify_tutorials.py` 会拦，提前自查。
- 直接推 Gitee 或在 Gitee 产生分叉历史 → 破坏镜像体系。
- 修改 `compiler/stdlib.json` 而忘记同步 `std/` 头文件（或反之）→ 标准库不一致。

## 6. 新增 / 修订 skill 的规则

1. 新 skill 放 `skills/`，必须带 frontmatter（`name` + `description`），
   description 用英文写触发条件（"Use when ..."），正文中文。
2. 一个 skill 只解决一类任务，出现重复内容时互相链接而非复制。
3. 新增 skill 后必须同步更新：本文件 §2 索引、CONTRIBUTING.md §0 索引。
4. skill 内容必须与实现同步：语言行为变了，相关 skill 同一轮内一起改。
