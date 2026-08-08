# toolkit_build.py — 一键构建脚本

Kaula 的单一构建入口：以一条命令构建编译器、标准库、freestanding 无依赖库、运行时与格式化工具。基于 SHA256 增量缓存，未变更源文件秒级跳过。

## 用法

```
usage: python toolkit_build.py [-h] [--release] [--clean]
                               [--target {all,std,freestanding,runtime,compiler,headers}]
                               [--install-dir INSTALL_DIR] [--cc CC]
```

## 参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `--release` | 关 | Release 模式（默认 Debug） |
| `--clean` | 关 | 清理所有构建产物后退出 |
| `--target <t>` | `all` | 只构建指定目标 |
| `--install-dir <dir>` | `build/` | 自定义输出目录 |
| `--cc <compiler>` | `clang` | 指定 C 编译器（如 gcc） |
| `-h` / `--help` | — | 显示帮助 |

`--target` 取值：

| 目标 | 产物 |
|------|------|
| `all` | 全部（std + freestanding + runtime + compiler + headers） |
| `std` | 标准库（kaula_std） |
| `freestanding` | freestanding 无依赖标准库（kaula_freestanding） |
| `runtime` | 运行时（kaula_runtime） |
| `compiler` | Go 编译器 `kaulac` 与格式化工具 `kaulafmt` |
| `headers` | 头文件 |

## 示例

```bash
python toolkit_build.py                        # 全量构建 (Debug, 默认 clang)
python toolkit_build.py --release              # Release 模式
python toolkit_build.py --target std           # 只构建标准库
python toolkit_build.py --target freestanding  # 只构建 freestanding 库
python toolkit_build.py --target compiler      # 只构建编译器与格式化工具
python toolkit_build.py --cc gcc               # 使用 gcc 替代默认 clang
python toolkit_build.py --install-dir D:/kaula # 自定义输出目录
python toolkit_build.py --clean                # 清理所有构建产物
```

## 依赖

- Python 3.8+
- Go 1.21+
- Clang（或 `--cc gcc` 指定其他编译器）

## 产物位置

默认输出到 `build/`：

```
build/
├── bin/
│   ├── kaulac.exe          # 编译器
│   ├── kaulafmt.exe        # 格式化工具
│   └── stdlib.json         # 标准库配置（随编译器输出）
├── include/                # 头文件（headers 目标）
└── lib/                    # 标准库/运行时静态库
```

编译器运行时自动检测 `stdlib.json`、`pkglib/` 的位置，无需手动指定。

## 相关文档

- 工具索引：[README.md](README.md)
- 编译配置：[build-config.md](../build-config.md)
- 编译器使用：[kaulac.md](kaulac.md)