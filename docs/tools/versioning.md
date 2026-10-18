# 版本号管理

Kaula 使用双版本号系统，结合发布版本和快照版本。

## 版本格式

| 类型 | 格式 | 示例 |
|------|------|------|
| **发布版本** | `v1.0.x` | `v1.0.42` |
| **快照版本** | `YY.M.DD-branch-hash` | `26.8.23-master-67ffac3` |

### 发布版本 (v1.0.x)

- `v1.0` — 主版本号，硬编码在 `version.json`
- `x` — 自进入 v1.0 以来的提交次数
- 每次构建自动计算，确保唯一递增

### 快照版本 (YY.M.DD-branch-hash)

- `YY` — 年份后两位（2026 → 26）
- `M` — 月份（无前导零，8 月 → 8）
- `DD` — 日期（无前导零，23 日 → 23）
- `branch` — 当前分支名（master/dev/feature-xxx）
- `hash` — commit hash 前 7 位

示例输出：
```
26.8.23-master-67ffac3
26.8.23-dev-abc1234
26.9.1-feature-auth-8f3a2b1
```

## 命令行工具

```bash
# 查看完整版本信息
python scripts/version.py
# 输出: kaulac v1.0.42 (26.8.23-master-67ffac3, sor-oxide)

# 仅输出发布版本
python scripts/version.py --release
# 输出: v1.0.42

# 仅输出快照版本
python scripts/version.py --snapshot
# 输出: 26.8.23-master-67ffac3

# 输出 JSON 格式
python scripts/version.py --json
# 输出:
# {
#   "release": "v1.0.42",
#   "snapshot": "26.8.23-master-67ffac3",
#   "codename": "sor-oxide"
# }

# 更新 version.json
python scripts/version.py --update
```

## 版本号计算逻辑

### x 的计算（发布版本）

1. 查找本地 `v1.0` 或最新 `v1.0.x` tag
2. 获取远程 `origin/master` 的提交数
3. 取 `max(本地计数, 远程计数) + 1`

这确保：
- 本地未推送时，x 仍递增
- 远程有更新时，自动同步计数
- PR 合并后，计数不会冲突

### 快照版本

构建时自动从 git 获取：
- 日期：当前系统时间
- 分支：`git rev-parse --abbrev-ref HEAD`
- hash：`git rev-parse HEAD`

## 文件结构

```
compiler/
  version.json          # 版本数据源
scripts/
  version.py            # 版本生成工具
compiler/internal/version/
  version.go            # Go 版本库
toolkit_build.py        # 构建脚本（自动调用 version.py）
```

### version.json 格式

```json
{
  "version": "v1.0.42",
  "snapshot": "26.8.23-master-67ffac3",
  "codename": "sor-oxide"
}
```

### kaulac --version 输出

```
kaulac v1.0.42 (26.8.23-master-67ffac3, sor-oxide)
```

## 构建流程

```bash
# 1. 构建时自动更新版本
python toolkit_build.py
# 控制台输出: [+] Updated compiler/version.json

# 2. 版本写入 version.json
# 3. 编译器从 version.json 读取版本
# 4. kaulac --version 显示版本
```

## 发布流程

```bash
# 1. 打 tag
git tag -a v1.0.42 -m "Release v1.0.42"
git push origin v1.0.42

# 2. 创建 Gitee Release
# 附带二进制产物
```

## 注意事项

1. **不要手动编辑 version.json** — 每次构建会自动覆盖
2. **快照版本包含分支名** — 切换分支会改变版本号
3. **远程同步** — 构建前会自动 fetch 远程信息
4. **Codename** — 硬编码在 version.json，当前为 `sor-oxide`
