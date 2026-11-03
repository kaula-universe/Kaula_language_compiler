#!/usr/bin/env python3
"""仓库二进制产物守门 / No compiled binaries in the repository.

为什么需要它
------------
测试语料 `test/*.kl` 编译出来的可执行文件**没有扩展名**（`test/binary_trees`、
`test/alloc_bench` …）。`.gitignore` 早期靠逐个点名来挡，每新增一个语料名就漏
一次：`8c76dfd` 一次性把 50 个 ELF（5.0 MB）提交进了仓库，而 git 里的二进制
既不能 diff 也不能 review，还会永久占用克隆体积。

`.gitignore` 侧已改为反向白名单（`test/**` 默认忽略，放回带扩展名的源文件与
目录），但**规则被删、被绕过、或产物落在别的目录**这类情况，规则本身不会报警。
本脚本是唯一能持续兜底的那一层：任何被 git 跟踪的二进制产物 = 直接失败。

检查方式
--------
读取 git 索引中的对象内容（不是磁盘状态，避免本地脏文件误报），按魔数识别：

| 类型 | 魔数 | 说明 |
|------|------|------|
| ELF | `\\x7fELF` | Linux/BSD 可执行文件、`.so`、`.o` |
| ar 归档 | `!<arch>\\n` | 静态库 `.a` / `.lib` |
| Mach-O | `0xfeedfacf` / `0xcefaedfe` 等 | macOS 产物 |
| PE | `MZ` + PE 头 | Windows `.exe` / `.dll` |
| 压缩对象 | `PK\\x03\\x04`、`GZIP`、`xZ`、`BZh` | 打包产物 / 镜像 |
| LLVM bitcode | `BC\\xc0\\xde` | `.bc` |

文本、shell 脚本、无扩展名但内容可打印的文件一律放行 —— 判据是内容而非扩展名，
所以 `Makefile`、`LICENSE`、`test/probe` 这类真实源文件不会被误杀。

用法
----
    python3 scripts/check_no_binaries.py            # 检查当前索引（CI 用）
    python3 scripts/check_no_binaries.py --staged   # 只检查已暂存改动（pre-commit）
    python3 scripts/check_no_binaries.py --allow test/foo   # 临时豁免（必须写明理由）

退出码：0 通过；1 发现二进制；2 脚本自身无法执行检查（git 缺失等）。
"""

import argparse
import subprocess
import sys
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parent.parent

# 魔数签名 -> 可读类型名
SIGNATURES = [
    (b"\x7fELF", "ELF 可执行/目标文件"),
    (b"!<arch>\n", "ar 静态库 (.a)"),
    (b"\xca\xfe\xba\xbe", "Mach-O (fat)"),
    (b"\xbe\xba\xfe\xca", "Mach-O (fat, 反字节序)"),
    (b"\xcf\xfa\xed\xfe", "Mach-O 64 位"),
    (b"\xfe\xed\xfa\xcf", "Mach-O 64 位 (反字节序)"),
    (b"\xce\xfa\xed\xfe", "Mach-O 32 位"),
    (b"MZ", "PE/COFF (.exe/.dll)"),
    (b"PK\x03\x04", "ZIP 容器"),
    (b"\x1f\x8b", "gzip"),
    (b"\xfd7zXZ", "xz"),
    (b"BZh", "bzip2"),
    (b"BC\xc0\xde", "LLVM bitcode"),
]

# 资源类魔数：仓库里合法存在的非文本资源（图标/配图）。它们不是构建产物，
# 默认放行，只在统计里计数，避免把 logo.png 这类真实资源误当成缺陷清掉。
RESOURCE_SIGNATURES = [
    (b"\x89PNG\r\n\x1a\n", "PNG 图片"),
    (b"\xff\xd8\xff", "JPEG 图片"),
    (b"GIF87a", "GIF 图片"),
    (b"GIF89a", "GIF 图片"),
    (b"RIFF", "WEBP/AVI 容器"),
    (b"BM", "BMP 图片"),
    (b"\x00\x00\x01\x00", "ICO 图标"),
    (b"icns", "ICNS 图标"),
]


def git(*args, stdin: str = None) -> str:
    proc = subprocess.run(
        ["git", *args], cwd=str(PROJECT_ROOT), capture_output=True, text=True,
        input=stdin, encoding="utf-8", errors="replace")
    if proc.returncode != 0:
        raise RuntimeError(f"git {' '.join(args)} 失败: {proc.stderr.strip()}")
    return proc.stdout


def classify_resource(head: bytes):
    """判定是否为合法资源（图片/图标）；是则返回类型名，否则 None。"""
    for magic, kind in RESOURCE_SIGNATURES:
        if head.startswith(magic):
            return kind
    return None


def classify(head: bytes):
    """按前若干字节判定类型；返回 None 表示放行。"""
    for magic, kind in SIGNATURES:
        if head.startswith(magic):
            return kind
    # 兜底：NUL 字节在文本源文件里不应出现（Go/C/Python/Markdown 皆然）
    if b"\x00" in head:
        return "含 NUL 字节的二进制内容"
    return None


def list_targets(staged_only: bool):
    """返回 (路径, 索引对象名) 列表。

    读索引内容而非磁盘，保证结论与"将要提交/已经提交的东西"一致。
    """
    if staged_only:
        out = git("diff", "--cached", "--name-only", "-z", "--diff-filter=ACM")
        paths = [p for p in out.split("\0") if p]
        if not paths:
            return []
        # 取每个路径在索引中的 blob oid
        lines = git("ls-files", "-s", "--", *paths).splitlines()
        result = []
        for line in lines:
            meta, path = line.split("\t", 1)
            _mode, oid, _stage = meta.split()
            result.append((path, oid))
        return result
    lines = git("ls-files", "-s").splitlines()
    result = []
    for line in lines:
        meta, path = line.split("\t", 1)
        _mode, oid, _stage = meta.split()
        result.append((path, oid))
    return result


def blob_head(oid: str, size: int = 16) -> bytes:
    """读取索引对象的前 size 字节。"""
    proc = subprocess.run(
        ["git", "cat-file", "-p", oid], cwd=str(PROJECT_ROOT),
        capture_output=True)
    if proc.returncode != 0:
        raise RuntimeError(f"git cat-file 失败: {oid}")
    return proc.stdout[:size]


def main() -> int:
    parser = argparse.ArgumentParser(description="检查仓库中是否跟踪了二进制产物")
    parser.add_argument("--staged", action="store_true",
                        help="只检查已暂存的改动（pre-commit 场景）")
    parser.add_argument("--allow", action="append", default=[],
                        help="豁免路径（可重复），必须同时说明理由")
    parser.add_argument("--summary", action="store_true", help="只打印统计")
    args = parser.parse_args()

    try:
        targets = list_targets(args.staged)
    except RuntimeError as exc:
        print(f"[ERROR] {exc}", file=sys.stderr)
        return 2

    allow = set(args.allow)
    offenders = []
    resources = []
    total = 0
    for path, oid in targets:
        try:
            head = blob_head(oid)
        except RuntimeError as exc:
            print(f"[ERROR] {exc}", file=sys.stderr)
            return 2
        if classify_resource(head):
            resources.append((path, classify_resource(head)))
            continue
        kind = classify(head)
        if kind is None:
            continue
        if path in allow:
            print(f"[允许] {path}  ({kind}) —— 已显式豁免")
            continue
        size = int(git("cat-file", "-s", oid).strip())
        offenders.append((path, kind, size))
        total += size

    scope = "已暂存改动" if args.staged else "git 索引"
    if offenders:
        print(f"[FAIL] {scope}中存在 {len(offenders)} 个二进制产物，合计 {total / 1048576:.1f} MB：")
        for path, kind, size in sorted(offenders):
            print(f"    {path:<50} {kind} ({size / 1024:.0f} KB)")
        print()
        print("修复办法：")
        print("    git rm --cached <路径>        # 只摘出跟踪，保留磁盘文件")
        print("    并在 .gitignore 补规则（test/ 已用反向白名单，无需逐个点名）")
        print("确属必要的二进制（如测试用图片）请通过 --allow <路径> 显式豁免并写明理由。")
        return 1

    if resources:
        print(f"[资源] 放行 {len(resources)} 个非文本资源：" +
              ", ".join(f"{p}({k.split()[0]})" for p, k in sorted(resources)))
    if not offenders:
        print(f"[OK] {scope}共 {len(targets)} 个文件，未发现二进制构建产物。")
    return 0


if __name__ == "__main__":
    sys.exit(main())
