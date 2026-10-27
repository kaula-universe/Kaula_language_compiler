# 仓库同步与镜像体系

> 本文档说明 Kaula 项目在两个托管平台 —— **GitHub（主仓）** 与 **Gitee（国内镜像）** 之间
> 的完整同步机制。共包含三条链路：
>
> 1. **主仓切换**（一次性，已完成）：GitHub 成为唯一主仓
> 2. **Release / Tag 快照同步**（`sync-gitee.yml`）：发版时把该版本快照 + 产物同步到 Gitee
> 3. **组织仓库双向互同步**（`org-sync.yml` + `scripts/org_sync.py`）：组织内所有仓库的增/改/删自动双向同步
>
> 所有自动化均通过 GitHub Actions 在 **Ubuntu runner** 上运行，凭证使用仓库 Actions Secrets。

---

## 1. 角色定位

| 平台 | 地址 | 角色 |
|------|------|------|
| GitHub（主仓） | `github.com/kaula-universe/Kaula_language_compiler` | **唯一主仓**，日常开发/提交/发版都在此 |
| Gitee（镜像） | `gitee.com/kaula-universe/kaula` | 国内快速访问与拉取，保留各 Release 的 tag + 源码快照 + 产物 |
| Gitee 组织 | `gitee.com/kaula-universe` | 与 GitHub 组织同名，维持各卫星仓库的双向一致 |

**设计原则**：GitHub 是权威主仓，Gitee 仅作镜像，**不保留完整提交历史**（只保留版本快照）；
但对于组织内除主仓外的卫星仓库（termus / kaulaweb / eigen / ml / algebra / tensor），
则做**内容级双向互同步**，保留完整 git 历史。

---

## 2. 主仓切换（已完成）

**背景**：原本 Gitee 是主仓（521 次提交），GitHub 是从某个时间点复制后独立发展的另一条历史（213 次提交），
两条历史**无共同祖先**。

**方案 A 执行**（以 Gitee 521 提交完整历史为 GitHub 新主干）：

- 使用 `git merge --allow-unrelated-histories` 合并 GitHub 213 提交
- 159 个 add/add 冲突统一取 Gitee 主线版本
- 显式保留 GitHub 独有的 12 个文件（`sync-gitee.yml`、`kaulafmt`、`compiler/semantic`、`gui`、`i18n`、`prefix` 等）
- 合并结果 commit `c1c87b1`，共 **735 提交**；`diff gitee/master → HEAD` 仅 +12 文件（4332 行），零覆盖、零删除 → **无回归**
- 旧 GitHub 历史备份于分支 `backup/legacy-master`（不删除）

**结果**：GitHub 现在拥有完整历史，是唯一主仓；Gitee 定位为镜像。

---

## 3. Release / Tag 快照同步（sync-gitee.yml）

**工作流文件**：[`.github/workflows/sync-gitee.yml`](../.github/workflows/sync-gitee.yml)

**目标**：GitHub 发布 Release 或打 `v*` tag 时，自动把该 tag 的**仓库快照** + **Release 产物**同步到 Gitee，
且 **Gitee 侧不保留完整提交历史**。

### 触发方式

| 触发器 | 说明 |
|--------|------|
| `release: published` | GitHub 发布 Release 后触发 |
| `push: tags: ['v*']` | 推送 `v*` tag 触发（主要触发器，因 `release.yml` 用 GITHUB_TOKEN 创建 release 不触发 `release:published` 事件） |
| `workflow_dispatch` + 输入 `tag` | 手动补同步历史 tag |

### 同步逻辑

1. **孤儿快照提交**：`git checkout --orphan kaula-snapshot` 生成一条**无父提交、含完整源码树**的快照
   → `git tag -f "$TAG"` 打上 tag
2. **强制推送 tag 到 Gitee**：`git push --force https://{GITEE_USER}:{GITEE_TOKEN}@gitee.com/...`，
   丢弃 Gitee 原有完整历史，只保留这份快照
3. **轮询等待 GitHub Release 就绪**（最多 40 分钟）：等 `release.yml` 构建的产物上传完毕
4. **同步产物**：在 Gitee 创建对应 Release，逐个下载 GitHub 附件并上传到 Gitee

### 关键坑

- Gitee 拒绝 GitHub 风格的 `x-access-token` 认证 → 必须用 **`用户名:token`**（如 `zhe116:{token}`）
- Gitee 无草稿 Release → 必须等 tag 先存在再创建 Release
- release 与 push tag 可能同时触发 → 用 `concurrency` 按 tag 去重

### 已验证

- ✅ Gitee v1.0.195 已被快照化（parents=0，`release v1.0.195 (snapshot)`）
- ✅ Gitee 认证实测 `zhe116:token` 推送/删除成功

### Secrets 配置

| Secret | 说明 |
|--------|------|
| `GITEE_TOKEN` | Gitee 个人令牌（需 `projects` 权限） |

---

## 4. 组织仓库双向互同步（org-sync.yml + org_sync.py）

**工作流文件**：[`.github/workflows/org-sync.yml`](../.github/workflows/org-sync.yml)
**脚本**：[`scripts/org_sync.py`](../scripts/org_sync.py)

**目标**：`kaula-universe` 组织在 GitHub 与 Gitee 两端的仓库**完全自动互同步**，
覆盖 新增 / 修改（内容级双向、保留完整历史）/ 删除。

### 触发方式

| 触发器 | 行为 |
|--------|------|
| `schedule`: `0 */6 * * *`（每 6 小时） | 执行 `--apply` 真实同步（**默认方式**） |
| `workflow_dispatch` + 输入 `apply` | `true` → 真实同步；`false/空` → 仅 dry-run 预览 |

### 同步规则

| 场景 | 处理 |
|------|------|
| **新增**：一端有、另一端无 | 另一端自动创建同名仓库，并 `clone --mirror` + `push --mirror` 完整镜像 |
| **修改**：两端都有 | 拉取两端最新，分支/tag 双向收敛；分叉分支用 worktree + `--allow-unrelated-histories` 自动合并；**真实内容冲突则安全跳过、绝不覆盖** |
| **删除**：GitHub 有、Gitee 无（Gitee 为权威侧） | 先 `clone --mirror` 备份到 artifacts，再从 GitHub 删除 |
| **受保护** | `PROTECT` 列表（`Kaula_language_compiler` / `kaula` / `termus` / `kaulaweb`）**永不自动删除** |

### 安全设计

- **默认 dry-run**：不加 `--apply` 时只读对比、打印将执行的变更，不动远端
- **删除前强制备份**：任何删除先把仓库镜像到 `artifacts/<name>-<timestamp>.git`
- **权威侧（PRIMARY）**：默认 `gitee`，可通过环境变量 `ORG_SYNC_PRIMARY` 切换
- **主仓保护**：主仓与关键卫星仓列入 `PROTECT`，永不参与自动删除

### 运行流程（脚本内部）

1. 通过 GitHub API / Gitee API 拉取两端组织仓库清单
2. 按仓库名做 diff，分类为：需新增 / 需同步 / 需删除
3. 对每个仓库：以 Gitee 为基准 `clone --mirror`，再 fetch GitHub 分支/tag（命名空间隔离到 `refs/remotes/gh/*`）
4. 分支收敛：无则补建，快进则推进，分叉则 worktree 合并
5. tag 并集推送（幂等，同名 tag 已存在视为成功）

### 关键坑

- **worktree 歧义**：mirror 自带的 `origin` remote 的 fetch refspec 是 `+refs/*:refs/*`，
  会覆盖 `refs/remotes/gh/*` → 需 `git remote remove origin`，并用 commit SHA 而非 remote-tracking ref 创建 worktree
- **Windows 只读文件**：git 生成的 pack 文件只读，`shutil.rmtree` 删不掉 → 用 `force_rmtree` 先清只读位
- **runner 无 git 身份**：merge 需创建 merge commit → 合并前 `git config user.name/email`
- **同名 tag 已存在**：push 被拒 → 脚本把 "already exists" 视为幂等成功，不误报失败

### 已验证

- ✅ 本地 dry-run + GitHub Actions dry-run + apply 共 3 次全部成功
- ✅ 边缘卫星仓两端 HEAD 完全一致（termus/kaulaweb/eigen/ml/algebra/tensor）
- ✅ `revert-merge-5-master` 分叉自动合并成功；`develop` 因真实内容冲突被安全跳过（符合预期）

### Secrets 配置

| Secret | 说明 |
|--------|------|
| `GH_TOKEN` | GitHub 全能令牌（`repo` / `admin:org` / `delete_repo` / `workflow` scope） |
| `GITEE_TOKEN` | Gitee 个人令牌（`projects` 权限） |

---

## 5. 运维手册

### 手动触发

- **同步单版本到 Gitee**：Actions → `Sync release snapshot to Gitee` → Run workflow → 填 tag
- **组织双向同步（仅预览）**：Actions → `Org sync GitHub <-> Gitee` → Run workflow → 保持 `apply=false`
- **组织双向同步（执行）**：同上，把 `apply` 填 `true`

### 查看备份

删除仓库的镜像备份会作为 workflow artifact 上传，可在 Actions run 的 **Artifacts** 页下载（`org-sync-artifacts`），
路径为 `/tmp/org-sync-artifacts/<name>-<timestamp>.git`。

### 冲突处理

分叉分支若自动合并失败（内容冲突），脚本会输出 `[MERGE CONFLICT]` 并**跳过、不覆盖**。
需人工进入对应仓库手动解决后再推送，下一次定时同步会自动收敛。

---

## 6. 凭证与安全须知

- 所有令牌均存放在仓库 **Actions Secrets**（加密），**严禁**写入任何提交或公开文档
- `GH_TOKEN` 与 `GITEE_TOKEN` 仅用于同步流程，不用于其他场景
- 组织级自动删除具有破坏性，已通过 `PROTECT` 白名单 + 强制备份 + dry-run 双重保险
