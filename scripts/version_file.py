#!/usr/bin/env python3
"""版本文件落盘 / Ship compiler/version.json inside every install medium.

为什么需要这个模块
------------------
Kaula 的版本号唯一来源是构建时生成的 `compiler/version.json`（该文件被
gitignore，由 `scripts/version.py --update` 生成）。kaulac 运行期按
`compiler/internal/version/version.go` 的 `LookupPaths()` 顺序查找它，
找不到就降级返回 `dev-local`。

v1.1.377 及之前的所有打包脚本只搬运 bin / lib / include / std / src /
scripts / docs / tools，**没有带上 version.json**，于是四个平台的安装包
装完后 `kaulac --version` 一律显示：

    kaulac dev-local (dev-local)

而不是真实版本号。语言功能没坏，但版本自述失真——用户无法确认自己装的
到底是哪个版本，`--version` 也成了 CI 产物与源码版本不一致时的盲区。

本模块定下两条规则
------------------
1. **硬失败**：版本文件缺失、JSON 非法、或 `version` 字段为空 -> 直接抛
   `VersionFileError`，拒绝出包。宁可流水线红，不要静默发布一个自述
   `dev-local` 的产物（与 `build_deb/build_rpm/build_dmg` 里
   "拿不到版本号就 SystemExit，绝不退化成 1.0.0" 的既有取向一致）。
2. **落两处**，覆盖 Go 侧两条候选路径，任意一种安装布局都能命中：
   - `<install_root>/bin/version.json` —— 可执行文件同目录
     （对应 `exeDir/version.json`，Windows NSIS / macOS .app / deb / rpm
     / tar 归档全部适用，即使产物目录被用户整体挪走也仍然有效）
   - `<install_root>/compiler/version.json` —— 安装根目录下的 compiler/
     （对应 `exeDir/../compiler/version.json`，以及 `KAULA_HOME`
     指向安装根目录时的第一条路径）

用法
----
    from version_file import stage_into
    written = stage_into(project_root, install_root)
    # -> [<install_root>/bin/version.json, <install_root>/compiler/version.json]
"""

import json
import shutil
from pathlib import Path

VERSION_FILENAME = "version.json"
# Go 侧 KAULA_HOME / exeDir 相对定位都以此为目录名，改名会同时打断查找逻辑
COMPILER_DIRNAME = "compiler"
BIN_DIRNAME = "bin"


class VersionFileError(RuntimeError):
    """版本文件不可用（缺失 / 非法 / 字段为空）——出包必须硬失败，不允许降级。"""


def source_version_json(project_root: Path) -> Path:
    """返回构建生成的版本文件路径 `<project_root>/compiler/version.json`。"""
    project_root = Path(project_root).resolve()
    return project_root / COMPILER_DIRNAME / VERSION_FILENAME


def load_version_json(project_root: Path) -> dict:
    """读取并校验版本文件，任何异常都抛 VersionFileError（不返回默认值）。"""
    src = source_version_json(project_root)
    if not src.exists():
        raise VersionFileError(
            f"缺少版本文件 {src}；打包前必须先执行 "
            "`python scripts/version.py --update`（toolkit_build.py 已自动完成）。\n"
            "拒绝出包：安装包内的 kaulac 会静默降级为 dev-local 版本号。"
        )
    try:
        data = json.loads(src.read_text(encoding="utf-8"))
    except (ValueError, OSError) as exc:
        raise VersionFileError(f"版本文件 {src} 解析失败: {exc}") from exc
    if not isinstance(data, dict):
        raise VersionFileError(f"版本文件 {src} 结构非法：期望 JSON 对象")
    for field in ("version", "snapshot"):
        value = data.get(field)
        if not isinstance(value, str) or not value.strip():
            raise VersionFileError(
                f"版本文件 {src} 缺少非空的 `{field}` 字段（实际值: {value!r}）"
            )
    return data


def stage_into(
    project_root: Path,
    install_root: Path,
    bin_dir: Path = None,
) -> list:
    """把版本文件写入安装介质，返回写入路径列表。

    参数
    ----
    project_root
        仓库根目录，用于定位构建生成的 `compiler/version.json`。
    install_root
        介质内的安装根目录（deb/rpm 的 `usr/local/kaula` 或 `BUILD/`、
        dmg 的 `Kaula.app/Contents/MacOS`、归档的包根目录）。
    bin_dir
        介质内可执行文件目录，默认 `<install_root>/bin`。传 None 时若
        `<install_root>/bin` 不存在会创建它（打包阶段一定会有 bin 目录，
        创建只为让"缺版本文件就报错"的约束在任意调用点都成立）。
    """
    data = load_version_json(project_root)          # 先校验，非法就地抛错
    src = source_version_json(project_root)
    install_root = Path(install_root)
    bin_dir = Path(bin_dir) if bin_dir else install_root / BIN_DIRNAME

    written = []
    for dest_dir in (bin_dir, install_root / COMPILER_DIRNAME):
        dest_dir.mkdir(parents=True, exist_ok=True)
        dest = dest_dir / VERSION_FILENAME
        shutil.copy2(src, dest)
        # 回读一次：确认落盘内容与构建期生成的版本一致（防止只读挂载 /
        # 打包竞态留下半截文件，让缺陷重新变成静默的）。
        back = json.loads(dest.read_text(encoding="utf-8"))
        if back.get("version") != data["version"] or back.get("snapshot") != data["snapshot"]:
            raise VersionFileError(
                f"{dest} 与源版本文件不一致（源 {data['version']} / "
                f"落盘 {back.get('version')}）"
            )
        written.append(dest)
    return written


def verify_installed(install_root: Path, version: str) -> None:
    """断言安装根目录里已经有可被 kaulac 找到的版本文件，且版本号匹配。

    打包完成后调用：把"包里有版本文件"从隐含假设变成出包前置条件。
    """
    install_root = Path(install_root)
    candidates = [
        install_root / BIN_DIRNAME / VERSION_FILENAME,
        install_root / COMPILER_DIRNAME / VERSION_FILENAME,
    ]
    found = [p for p in candidates if p.exists()]
    if not found:
        raise VersionFileError(
            f"介质 {install_root} 内未找到 {VERSION_FILENAME}；"
            "安装后 kaulac --version 会降级成 dev-local，拒绝出包。"
        )
    for path in found:
        try:
            data = json.loads(path.read_text(encoding="utf-8"))
        except (ValueError, OSError) as exc:
            raise VersionFileError(f"介质内的 {path} 解析失败: {exc}") from exc
        got = str(data.get("version", "")).lstrip("v")
        if got != str(version).lstrip("v"):
            raise VersionFileError(
                f"介质内版本号与包版本不一致：{path} -> {data.get('version')!r}, "
                f"期望 {version!r}"
            )


if __name__ == "__main__":
    import argparse
    import sys

    parser = argparse.ArgumentParser(description="检查/落盘安装介质内的版本文件")
    parser.add_argument("--check", metavar="INSTALL_ROOT",
                        help="校验某个安装根目录（需同时给 --version）")
    parser.add_argument("--version", help="期望版本号，如 1.1.378 或 v1.1.378")
    args = parser.parse_args()

    root = Path(__file__).resolve().parent.parent
    try:
        if args.check:
            if not args.version:
                raise VersionFileError("--check 需要同时提供 --version")
            verify_installed(Path(args.check), args.version)
            print(f"[OK] 介质版本文件校验通过: {args.check} -> {args.version}")
        else:
            info = load_version_json(root)
            print(f"[OK] {source_version_json(root)} -> "
                  f"version={info['version']} snapshot={info['snapshot']}")
    except VersionFileError as exc:
        print(f"[FAIL] {exc}", file=sys.stderr)
        sys.exit(1)
