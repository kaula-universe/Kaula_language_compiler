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

# 归档旧 minor 基准并切换 minor（切版提交之前运行）
python scripts/version.py --cut-minor 1.1

# 刷新 compiler/version.json（构建时通常自动执行）
python scripts/version.py --update
```

## 文件结构

```
compiler/
  version.json          # 版本数据（生成物，被 .gitignore 忽略，不入库）
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
