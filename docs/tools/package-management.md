# 包管理

Kaula 编译器内置包管理工具，支持从 Git 仓库或本地路径添加第三方库，自动分析 C/C++ 头文件并构建静态库。

## 快速开始

```bash
# 从 Git 仓库添加 C 库
kaulac pkg add https://github.com/nicbarker/clay.git

# 从本地路径添加 Kaula 包
kaulac pkg add ../my-kaula-lib --name mylib

# 列出已安装的包
kaulac pkg list
```

## 命令一览

| 命令 | 说明 |
|------|------|
| `kaulac pkg add <url-or-path>` | 添加包到 pkglib（Git 仓库或本地路径） |
| `kaulac pkg build <name> [--force]` | 构建指定包（C/C++ 库） |
| `kaulac pkg analyze <name>` | 重新分析包并生成 JSON 配置 |
| `kaulac pkg remove <name>` | 从 pkglib 移除包 |
| `kaulac pkg list` | 列出已安装的包 |
| `kaulac pkg fetch` | 强制联网拉取所有项目依赖 |
| `kaulac pkg update [name]` | 更新依赖到最新版本 |
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

```json
{
  "dependencies": {
    "webview": "0.12",
    "clay": "^1.0"
  },
  "registry": "https://gitee.com/kaula-universe"
}
```

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

### 更新依赖

```bash
# 更新所有依赖到最新匹配版本
kaulac pkg update

# 更新单个依赖
kaulac pkg update webview
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
    "webview": "0.12.0"
  },
  "entries": {
    "webview": {
      "version": "0.12.0",
      "resolved_from": "fetch",
      "registry": "https://gitee.com/kaula-universe"
    },
    "mylib": {
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
