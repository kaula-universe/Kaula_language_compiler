# 包管理

Kaula 编译器内置包管理工具，支持从 Git 仓库、HTTP(S) 归档或本地路径添加第三方库，自动分析 C/C++ 头文件并构建静态库。下载通过**多线程并发**完成，大归档与多依赖场景下显著提速。

## 快速开始

```bash
# 从 Git 仓库添加 C 库
kaulac pkg add https://github.com/nicbarker/clay.git

# 从 HTTP(S) 归档添加（多线程并发下载 + 自动解压）
kaulac pkg add https://example.com/releases/mylib-1.2.3.tar.gz

# 从本地路径添加 Kaula 包
kaulac pkg add ../my-kaula-lib --name mylib

# 列出已安装的包
kaulac pkg list
```

## 命令一览

| 命令 | 说明 |
|------|------|
| `kaulac pkg add <url-or-path> [-j <n>]` | 添加包到 pkglib（Git 仓库、HTTP(S) 归档或本地路径） |
| `kaulac pkg build <name> [--force]` | 构建指定包（C/C++ 库） |
| `kaulac pkg analyze <name>` | 重新分析包并生成 JSON 配置 |
| `kaulac pkg remove <name>` | 从 pkglib 移除包 |
| `kaulac pkg list` | 列出已安装的包 |
| `kaulac pkg fetch` | 强制联网拉取所有项目依赖（多依赖并发） |
| `kaulac pkg update [name]` | 更新依赖到最新版本（多依赖并发） |
| `kaulac pkg lock` | 显示 kaula.lock 锁状态 |

## 添加包

### 从 Git 仓库

```bash
# 基本用法（从 URL 推断包名，默认 main/master 分支）
kaulac pkg add https://github.com/user/lib.git

# 指定包名和分支
kaulac pkg add https://github.com/user/lib.git --name mylib --ref main

# 指定 tag
kaulac pkg add https://github.com/user/lib.git --ref v1.2.3
```

拉取流程：
1. `git clone --depth 1` 到 `~/.kaula/pkglib/<name>/`
2. 自动检测包类型：
   - **C/C++ 源码** → 分析头文件生成 JSON 配置 → 编译静态库
   - **Kaula 源码** → 标记为 kaula 包（编译时由主程序 import）
   - **纯头文件** → 仅生成配置

### 从 HTTP(S) 归档

`pkg add` 自动识别指向**已知归档**的 HTTP(S) URL（`.tar.gz` / `.tgz` / `.tar` / `.zip`），
走**多线程并发下载**，不再依赖 git 环境：

```bash
# 下载 + 解压 + 自动分析/构建静态库（包名从 URL 推断）
kaulac pkg add https://example.com/releases/mylib-1.2.3.tar.gz

# 显式指定包名
kaulac pkg add https://example.com/releases/mylib-1.2.3.tar.gz --name mylib

# 指定分片并发数（默认 min(16, CPU 核数)，上限 64）
kaulac pkg add https://example.com/releases/big-1.0.0.zip -j 8
```

加包流程：
1. **探测**：HEAD 请求解析 `Content-Length` 与 `Accept-Ranges`（拒绝 HEAD 的服务器自动回退 `Range: bytes=0-0` GET 探测）
2. **分片并发下载**：服务器支持 Range 时按并发度切分文件（分片对齐 64KB），worker 池分别下载到临时文件后按序合并
3. **退化**：服务器不支持 Range 或长度未知 → 自动单连接流式下载（功能等价，仅无并发加速）
4. **校验**：分片大小逐段核对 + 全文件 sha256 计算结果一致才落盘
5. **安全解压**：`tar.gz` / `tar` / `zip` 解压到 pkglib，拒绝 `../` 路径穿越、绝对路径与符号链接逃逸
6. **收尾**：归档被单一目录包裹时（如 `mylib-1.2.3/`）自动上移内容到包根；随后按普通包流程自动分析/构建

> 判定规则：URL 以 `http(s)://` 开头**且**路径带已知归档扩展名 → HTTP 归档下载；
> 其他 URL（`.git` 结尾或无归档扩展名）仍走 git clone 路径。归档下载完即删除临时文件，不占用缓存空间。

### 从本地路径

```bash
# 添加本地目录（Windows 上直接引用源目录，Linux/macOS 使用符号链接）
kaulac pkg add ../local-lib --name mylib
```

## 构建包

```bash
# 构建指定包（仅在源码变更时重建）
kaulac pkg build webview

# 强制重新构建
kaulac pkg build webview --force
```

构建流程：
1. 扫描 `.c`/`.cpp` 源文件
2. 并行编译（`clang -c`，C 用 `-std=c11`，C++ 用 `-std=c++17`）
3. 打包为静态库（`webview.lib` 或 `libwebview.a`）

## 分析包

```bash
# 重新分析头文件并生成 JSON 配置
kaulac pkg analyze webview
```

分析流程：
1. 使用 `clang -ast-dump=json` 解析头文件
2. 提取函数签名、类型定义、链接库信息
3. 生成 `<name>.json` 配置文件

## 项目依赖管理

### 在 kaula.json 中声明依赖

支持两种依赖格式：

#### 版本约束（从注册表下载）

```json
{
  "dependencies": {
    "webview": "0.12",
    "clay": "^1.0"
  },
  "registry": "https://gitee.com/kaula-universe"
}
```

#### Git 仓库（从指定仓库下载）

```json
{
  "dependencies": {
    "mylib": {
      "git": "https://github.com/user/repo.git",
      "tag": "v1.0.0"
    },
    "another-lib": {
      "git": "https://gitlab.com/team/project.git",
      "branch": "main"
    }
  }
}
```

Git 依赖支持以下字段：
- `git`：git 仓库 URL（必填）
- `tag`：指定标签（可选，默认最新标签）
- `branch`：指定分支（可选，默认主分支）
- `commit`：指定 commit（可选）

### 离线/在线模式

| 场景 | 行为 |
|------|------|
| 有 lock + 缓存命中 | 离线复用（默认） |
| 无 lock / 缓存缺失 | 联网拉取 |
| `--offline` + 缓存缺失 | 报错 |
| `--online` | 强制联网刷新 |

```bash
# 强制离线模式（CI/CD 推荐）
kaulac --offline main.kl

# 强制在线模式
kaulac --online main.kl
```

### 拉取依赖

```bash
# 强制联网拉取所有项目依赖（忽略锁缓存）
kaulac pkg fetch
```

多个依赖之间也并行拉取（worker 池，默认并发 8），输出仍按声明顺序排列，保证确定性。

### 更新依赖

```bash
# 更新所有依赖到最新匹配版本
kaulac pkg update

# 更新单个依赖
kaulac pkg update webview
```

同样并发拉取；任一依赖失败会在全部任务收敛后统一报错，不会留下部分更新。

### 并发度环境变量

| 环境变量 | 控制对象 | 默认值 | 说明 |
|----------|----------|--------|------|
| `KAULAC_DL_CONCURRENCY` | 单文件分片下载并发 | `min(16, CPU 核数)` | 上限 64；`pkg add -j <n>` 优先级更高 |
| `KAULAC_FETCH_CONCURRENCY` | 多依赖 fetch/update 并发 | `8` | 上限 32 |

```bash
# 16 线程下载、16 路并发拉依赖
KAULAC_DL_CONCURRENCY=16 KAULAC_FETCH_CONCURRENCY=16 kaulac pkg fetch
```

### 查看锁状态

```bash
kaulac pkg lock
# 输出:
# Locked dependencies (2):
#   webview             0.12.0       (via fetch)
#   mylib               local:../dev (via local)
```

## 本地路径覆盖 (Patches)

用本地目录覆盖远程依赖，适合开发调试：

```json
{
  "dependencies": {
    "webview": "0.12",
    "mylib": "^1.0"
  },
  "patches": {
    "mylib": { "path": "../mylib-dev" }
  }
}
```

优先级：`patches` > `kaula.lock` > 远程拉取

## kaula.lock 格式

```json
{
  "packages": {
    "webview": "0.12.0",
    "mylib": "1.0.0"
  },
  "entries": {
    "webview": {
      "version": "0.12.0",
      "resolved_from": "fetch",
      "registry": "https://gitee.com/kaula-universe"
    },
    "mylib": {
      "version": "1.0.0",
      "resolved_from": "fetch",
      "git": "https://github.com/user/repo.git"
    },
    "another-lib": {
      "version": "local:../mylib-dev",
      "resolved_from": "local"
    }
  }
}
```

来源类型：
- `lock` — 从锁文件复用（离线）
- `fetch` — 联网拉取
- `local` — 本地路径覆盖
- `update` — 联网更新到新版本

## 包类型

### C/C++ 库

自动检测源文件并编译：
- `.c` 文件用 `clang -std=c11` 编译
- `.cpp/.cc/.cxx` 文件用 `clang -std=c++17` 编译
- 输出静态库：`<name>.lib`（Windows）或 `lib<name>.a`（Linux/macOS）
- 构建产物存放在 `<pkgDir>/kbuild/` 目录

### Kaula 源码包

包含 `.kl` 文件的包，编译时由主程序 import：
- `pub fn` / `export fn` 声明的符号对外可见
- `import "pkglib/<name>"` 即可引用
- 不需要预编译步骤

### 纯头文件库

如 `stb_image.h` 等 header-only 库：
- 只需要 JSON 配置，不需要构建
- 通过 `implement_macro` 字段指定实现宏（如 `STB_IMAGE_IMPLEMENTATION`）

## pkglib 目录结构

```
~/.kaula/pkglib/
  webview/
    webview.json          # JSON 配置
    webview.lib           # 静态库（Windows）
    kbuild/               # 构建产物
      *.o                 # 目标文件
    core/
      include/            # 头文件
      src/                # 源文件
  mylib/
    mylib.json
    libmylib.a            # 静态库（Linux/macOS）
    utils.kl              # Kaula 源文件
```

## 搜索路径

pkglib 目录按以下优先级搜索：

1. `--pkglib <dir>` 命令行参数
2. `<exeDir>/pkglib/`
3. `<project_root>/pkglib/`
4. `<exeDir>/../pkglib/`
5. `./pkglib/`（当前目录）
6. `~/.kaula/pkglib/`（用户主目录）

## 发布库 (publish)

`kaulac pkg publish` 把本地库一键上传到 kaula-universe 双注册源
（Gitee / GitHub 组织仓库），自动完成建仓 → 推送代码 → 打版本 tag → 远端复核。

```bash
kaulac pkg publish                          # 发布 ./ 到 gitee + github（默认）
kaulac pkg publish ../mylib --tag 0.2.0     # 发布指定库与版本
kaulac pkg publish --target github          # 只发 github
kaulac pkg publish --org zhe116             # 发到个人命名空间
kaulac pkg publish --dry-run                # 只打印计划，零写入
kaulac pkg publish --force                  # 覆盖已存在但指向别处的同版本 tag
```

### 鉴权

token 优先级：`--gitee-token` / `--github-token` 旗标 > 环境变量
`KAULA_GITEE_TOKEN` / `KAULA_GITHUB_TOKEN` > 全局配置 `~/.kaula/config.json`：

```json
{
  "gitee_token": "...",
  "github_token": "..."
}
```

- Gitee：私人令牌（access_token），在 gitee.com → 设置 → 私人令牌生成；
- GitHub：Personal Access Token（PAT），需 `repo` 范围（发组织仓库还需
  `read:org` / `write:org` 或相应 Organization 权限）。

### 版本与 tag 约定

- 版本确定顺序：`--tag` > `kaula.json "version"` > 仓库最近一次
  `git describe --tags`；
- 发布时自动打 `v<version>` tag（如 `v0.2.0`）并推送到远端；
- 幂等：远端已有同版本 tag 且指向当前 HEAD → 跳过；
- 冲突：同版本 tag 指向别的 commit → 报错，须 `--force`；
- 消费者用 `kaulac pkg add <url>` 或 `kaula.json dependencies` 按
  semver 约束拉取已发布的版本。

### 建仓与命名空间

目标仓库不存在时经平台 API 自动创建：

- 默认组织 `kaula-universe`（与拉取侧 `DefaultRegistry` 对齐）；
- `--org` 可覆盖；指向个人命名空间时自动走个人建仓接口
  （Gitee `/user/repos`、GitHub `/user/repos`），否则走组织接口；
- 仓库名取包名（`kaula.json "name"`，缺省为目录名）；
- 默认分支：Gitee `master` / GitHub `main`（与平台默认一致）。
