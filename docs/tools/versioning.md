# 版本号管理

Kaula 使用双版本号系统：发布版本 + 快照版本。

## 版本格式

| 类型 | 格式 | 示例 |
|------|------|------|
| **发布版本** | `v<MAJOR>.<MINOR>.<PATCH>` | `v1.1.1` |
| **快照版本** | `YY.M.DD-branch-hash` | `26.10.5-master-8717dec` |

## 发布版本：minor 归档 + 严格减法（方案 A）

自 v1.1 起，发布版本号采用**严格减法**，减法基准以 **v1.0 归档的提交总数**
（`base_total = 440`）为**唯一来源**，归档在
[`scripts/version_history.json`](../../scripts/version_history.json)，
该文件是版本基准的**唯一权威来源，不依赖 git tag**。后续 minor（v1.1 起）一律沿用它。

### 计算规则

```
PATCH = 仓库总提交数(git rev-list --count HEAD) − v1.0 归档提交总数(base_total = 440)
版本号 = v<MAJOR>.<MINOR>.<PATCH>
```

- `base_total`：**v1.0 归档的提交总数**，作为后续所有 minor 的唯一减法基准。
- 每个 minor 的**第 1 个提交 PATCH = 1（注意不是 0）**，之后每个提交严格 +1。
- 因为是严格减法，版本号在历史不被重写的前提下天然单调、可复现。

### 切换 minor（人工操作）

minor 升级是**人工决策**，不会自动发生。在“切版提交”创建**之前**运行：

```bash
python scripts/version.py --cut-minor 1.1
```

该命令：
1. 读取旧 minor 最后一个提交的总提交数，作为该 minor 的归档记录；
2. 把 `current_minor` 切到新 minor；
3. 减法基准不变（始终是 v1.0 的 440），随后创建的切版提交即 `v<new>.1`。

**示例（v1.0 → v1.1）**：v1.0 归档提交总数 440（`base_total = 440`）；切版提交总提交
441，`PATCH = 441 − 440 = 1` → **v1.1.1**。

**未来 v1.2**：基准仍是 v1.0 的 440（不变），`v1.2.N = 总提交数 − 440`，
切版提交即 v1.2.1。

### v1.0 历史

v1.0 归档的提交总数为 `440`，作为后续所有 minor 的减法基准。v1.0 历史与现有
tag（`v1.0.185/194/195`，位于 develop/feat 分支、并非 master 祖先）**一律保持
不变，不得改动或删除**。

### 不自动打 tag

`.github/workflows/auto-tag.yml` 已由“push master 自动打 tag”改为**手动触发**
（`workflow_dispatch`）。v1.1 只推进到可发布状态：**不打 tag、不发 Release**；
需要发布时再人工触发或手工打 tag。

## CI 中的版本号来源：构建生成的版本文件

tag 名与 Release 标题展示的版本号，**一律取自构建脚本生成的版本文件
`compiler/version.json`**，而不是在 CI 里重新用 git 计算。

```bash
python scripts/version.py --update             # 生成 compiler/version.json
python scripts/version.py --release --from-file   # 从版本文件读取 → v1.1.302
python scripts/version.py --snapshot --from-file  # 从版本文件读取 → 26.10.18-master-4c8677f
```

### 为什么必须这样做

减法基准需要真实的 `git rev-list --count HEAD`。CI 默认 `actions/checkout` 是
**浅克隆（fetch-depth: 1）**，此时提交数恒为 1，`PATCH = 1 − 440 < 0` 被钳位为 0，
算出的版本号会**静默退化成 `v1.1.0`**（v1.1 系列根本不存在 `.0`）。
这正是历史上 Release 标题出现「Kaula」（版本号为空）的原因。

### 三重防护

| 防护 | 位置 | 行为 |
|------|------|------|
| 完整历史 | `auto-tag.yml` / `release.yml` / `build.yml` 的 `actions/checkout` | `fetch-depth: 0` |
| 版本文件为唯一来源 | `version.py --from-file` | tag / Release 只认 `compiler/version.json` |
| 一致性校验 | `release.yml` 的 `Verify tag matches generated version file` | tag 名 ≠ 版本文件版本 → 报错并终止发布 |

另外 `version.py` 检测到浅克隆会向 stderr 打印告警，便于本地/CI 排查。

### 打 tag 与发 Release 的流程

1. GitHub Actions → **Manual Tag** → Run workflow（默认 `master`）。
2. 工作流用完整历史生成版本文件，读取版本号，创建**附注 tag** 并推送。
   - tag 已存在且指向同一提交 → 跳过（幂等）；
   - tag 已存在但指向不同提交 → 报错，**绝不移动已有 tag**。
3. tag push 触发 **Build and Release**，三平台构建 → 校验 tag 名与版本文件一致
   → 创建 Release，标题为 `Kaula <版本文件中的版本号>`。

### 版本文件缺失时的行为

`--from-file` 在版本文件缺失、JSON 损坏或缺少 `version` 字段时**硬失败退出 1**
并给出中文错误信息，不会静默返回错误版本。

## 快照版本 (YY.M.DD-branch-hash)

- `YY` — 年份后两位（2026 → 26）
- `M` — 月份（无前导零）
- `DD` — 日期（无前导零）
- `branch` — 当前分支名（master/develop/feature-xxx）
- `hash` — commit hash 前 7 位

示例：
```
26.10.5-master-8717dec
26.9.1-feature-auth-8f3a2b1
```

## 命令行工具

```bash
# 查看完整版本信息
python scripts/version.py
# 输出: kaulac v1.1.1 (26.10.5-master-8717dec)

# 仅输出发布版本 / 快照版本
python scripts/version.py --release     # v1.1.1
python scripts/version.py --snapshot    # 26.10.5-master-8717dec

# 输出 JSON
python scripts/version.py --json
# {
#   "release": "v1.1.1",
#   "snapshot": "26.10.5-master-8717dec",
#   "codename": "kaula"
# }

# 从构建生成的版本文件读取（CI 打 tag / 发 Release 用这个）
python scripts/version.py --release --from-file    # v1.1.1
python scripts/version.py --snapshot --from-file   # 26.10.5-master-8717dec

# 归档旧 minor 基准并切换 minor（切版提交之前运行）
python scripts/version.py --cut-minor 1.1

# 刷新 compiler/version.json（构建时通常自动执行）
python scripts/version.py --update
```

## 文件结构

```
compiler/
  version.json          # 版本数据（生成物，被 .gitignore 忽略，不入库）
                        # CI 中 tag / Release 展示版本号的唯一来源（--from-file 读取）
scripts/
  version.py            # 版本生成工具（含 --cut-minor）
  version_history.json  # 版本基准归档（入库，唯一权威来源；减法基准 = v1.0 提交总数）
compiler/internal/version/
  version.go            # Go 版本库（读取 version.json）
toolkit_build.py        # 构建脚本（构建时调用 version.py）
```

### version.json 格式（生成物）

```json
{
  "version": "v1.1.1",
  "snapshot": "26.10.5-master-8717dec",
  "codename": "kaula"
}
```

> `compiler/version.json` 由构建过程生成、被 `.gitignore` 忽略；版本基准以
> 入库的 `scripts/version_history.json` 与 git 总提交数为准。

### kaulac --version 输出

```
kaulac v1.1.1 (26.10.5-master-8717dec)
```

## 注意事项

1. **不要手动编辑 `compiler/version.json`** — 每次构建会自动覆盖。
2. **版本基准只看 `version_history.json`** — 切换 minor 必须用 `--cut-minor` 归档。
3. **快照版本包含分支名** — 切换分支会改变快照版本。
4. **Codename** 当前为 `kaula`，不进入 `--version` 输出。
5. **不依赖、也不自动创建 git tag**；v1.0 历史 tag 不得改动。
6. **CI 必须用 `fetch-depth: 0`** — 浅克隆会让减法退化，版本号错成 `v<minor>.0`。
7. **`--update` 与 `--from-file` 互斥** — 前者生成版本文件，后者读取版本文件。
