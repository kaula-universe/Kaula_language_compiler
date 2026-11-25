---
name: kaula-toolchain
description: "Use when an AI or developer needs to run the Kaula toolchain: kaulac compile/fmt/pkg/workspace subcommands, dumpast, loadtest, toolkit_build.py, test runners, and debugging. Command-level quick reference with pointers to full docs."
---

# Kaula 工具链速查（AI 操作手册）

完整参数说明见 [docs/tools/kaulac.md](../docs/tools/kaulac.md) 与
[docs/tools/README.md](../docs/tools/README.md)；本文是高频命令的执行级速查。

## 1. 编译与运行

```bash
kaulac main.kl                 # 编译（生成可执行文件，详见 kaulac.md）
kaulac --dump-ast main.kl      # 查看 AST（诊断解析问题首选）
kaulac --preprocess main.kl    # 查看生成的 C 代码（诊断 codegen 问题首选）
kaulac --version               # 版本
```

诊断思路：语法错误先 `--dump-ast`，输出 C 异常先 `--preprocess`，
语义报错行号错乱时对照 `test/` 下相近语料。

## 2. 格式化（fmt）

```bash
kaulac fmt file main.kl --write    # 就地格式化
kaulac fmt check main.kl           # CI 检查模式
kaulac fmt <目录>                   # 批量（详见 docs/tools/formatter.md）
```

要求：格式化后不得出现 `/* unknown statement */` / `/* unknown expression */`；
二次 fmt 必须与首次一致（round-trip 稳定）。

## 3. 包管理与工作空间

```bash
kaulac pkg <子命令>            # pkglib 依赖管理（docs/tools/package-management.md）
kaulac workspace <子命令>      # 多包工作空间（README「工作空间」一节）
```

项目级配置文件为 `kaula.json`（字段参考 docs/build-config.md）；
作为库导入的目录必须含 `kaula.json`（可为空文件）。

## 4. 构建

```bash
python toolkit_build.py                # 全量：编译器 + 标准库 + 运行时
python toolkit_build.py --release      # Release
python toolkit_build.py --target compiler | --target std
python toolkit_build.py --clean
```

依赖：Go >= 1.23、Clang/GCC、Python 3。详见 docs/tools/toolkit-build.md。

## 5. 测试体系（提交前必须跑的门槛）

```bash
cd compiler && go test ./...           # 编译器 Go 单元测试
cd compiler && go vet ./...            # 静态检查
python scripts/run_tests.py --quick    # .kl 语料全量（跳过慢基准）
python scripts/run_tests.py --filter stress          # 按名过滤
python scripts/run_tests.py --mode default | --mode --sor   # 单分配器模式
python scripts/verify_tutorials.py     # 教程文档示例编译运行校验
python scripts/verify_tutorials.py --only prefix-system     # 只验某篇教程
python scripts/run_freestanding_tests.py    # freestanding 库测试
```

要点：

- `run_tests.py` 会把每个 `test/*.kl` 在 **default 与 --sor 两种分配器模式**下
  各跑一遍，退出码 0 才算通过；这是语言级回归守门人。
- 修 bug 时先用 `--filter` 跑复现语料，再全量 `--quick` 确认无回归。
- 负向语料（expect-fail）放在 `test/errors/` 等目录，按 testrunner 标注约定。

## 6. 诊断与调试

```bash
tools/debug/dumpast <file>.kl     # 独立 AST 转储工具
loadtest                          # 语义分析负载测试（docs/tools/loadtest.md）
```

运行时调试（GDB/LLDB、断点、KMM 观察）见 docs/tools/debugging.md；
编译产物是标准 C 程序，可直接用调试器 Attach。

## 7. 版本与发布

```bash
python scripts/version.py --update      # 更新版本
python toolkit_build.py --release       # 发版前本地构建验证
git tag -a v1.0.xxx -m "Release v1.0.xxx" && git push origin v1.0.xxx
```

Tag 推送后 GitHub Actions 自动三平台构建并发 Release，并经
`sync-gitee.yml` 把快照同步到 Gitee（不要手工推 Gitee）。

## 8. AI 操作约束

- 只在确有环境（Go/Clang/kaulac 可用）时才声称「已验证」；否则列出应跑的命令。
- 改 `compiler/` 或 `src/` 后，至少跑 `go test ./...` + `run_tests.py --quick`
  才允许提交；无法运行时必须降级为「文档-only 提交」并注明。
- 超时/内存问题先查 docs/timeout-memory.md，不要猜运行时行为。
