#!/usr/bin/env python3
"""检查 IO 后端的每个平台分支都定义了全部导出函数。

背景：kaula_conc_iodriver.c 无条件调用 backend_begin /
backend_next_completion / backend_abort_all，不做平台分支。
kqueue 与 IOCP 都实现了，唯独 epoll 分支三个全缺 —— 声明在头文件里、
定义没有，Linux 上就是 undefined symbol，而本机只有 Windows，
这个问题是靠人工检视发现的。

同类问题还会有，所以这里把它变成可自动跑的检查：
每个 backend 分支都必须定义头文件里声明的全部函数。
只做静态检查，不做链接 —— 静态检查已经足够抓住「某个分支漏定义」
这一类，而链接需要真的能编出那个平台的 .o。

用法: python test/concurrency/check_backend_symbols.py
"""
from __future__ import annotations

import re
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent.parent
HDR = ROOT / "src" / "concurrency" / "kaula_conc_io_backend.h"
SRC = ROOT / "src" / "concurrency" / "kaula_conc_io_backend.c"

# 平台分支标记，与 .c 里的 #if / #elif 一一对应
BRANCHES = {
    "epoll": "#if KAULA_P_IO_EPOLL",
    "kqueue": "#elif KAULA_P_IO_KQUEUE",
    "iocp": "#elif KAULA_P_IO_IOCP",
}

# 编译期的公共部分不属于任何平台分支
COMMON_PREFIX_MARKER = "#if KAULA_PLATFORM_WINDOWS"


def declared_functions() -> list[str]:
    """从头文件里抠出导出的函数名。"""
    txt = HDR.read_text(encoding="utf-8")
    # 去掉注释，避免把注释里的调用当成声明
    txt = re.sub(r"/\*.*?\*/", "", txt, flags=re.S)
    txt = re.sub(r"//[^\n]*", "", txt)
    return sorted(set(re.findall(r"\b(kaula_iodriver_backend_\w+)\s*\(", txt)))


def split_branches(txt: str) -> dict[str, str]:
    """把 .c 按平台分支切开，返回 {分支名: 该分支的源码}。"""
    lines = txt.split("\n")

    # 先剥掉编译期公共部分
    try:
        start_common = next(i for i, l in enumerate(lines)
                            if l.strip().startswith(COMMON_PREFIX_MARKER))
    except StopIteration:
        start_common = -1

    out: dict[str, list[str]] = {name: [] for name in BRANCHES}
    current: str | None = None
    for i, line in enumerate(lines):
        if i < start_common:
            continue
        s = line.strip()
        for name, marker in BRANCHES.items():
            if s.startswith(marker):
                current = name
                break
        if current is not None:
            out[current].append(line)

    return {k: "\n".join(v) for k, v in out.items()}


def defined_functions(branch_src: str) -> set[str]:
    txt = re.sub(r"/\*.*?\*/", "", branch_src, flags=re.S)
    txt = re.sub(r"//[^\n]*", "", txt)
    # 只认函数定义：返回类型在前、函数名后紧跟 ( 且参数列表不是 void 单独
    return set(re.findall(
        r"^\s*(?:[A-Za-z_][\w \t*]*?)\b(kaula_iodriver_backend_\w+)\s*\([^;]*?\)\s*\{",
        txt, flags=re.M))


def main() -> int:
    if not HDR.exists() or not SRC.exists():
        print(f"找不到 {HDR} 或 {SRC}", file=sys.stderr)
        return 2

    declared = declared_functions()
    if not declared:
        print("头文件里没解析出任何 backend 函数声明，检查解析规则", file=sys.stderr)
        return 2

    branches = split_branches(SRC.read_text(encoding="utf-8", errors="replace"))

    print(f"头文件声明 {len(declared)} 个 backend 函数\n")
    bad = 0
    for name, src in branches.items():
        if not src.strip():
            print(f"[{name:<7}] 分支未找到（.c 的平台宏是否改名了？）")
            bad += 1
            continue
        defined = defined_functions(src)
        missing = [f for f in declared if f not in defined]
        extra = sorted(defined - set(declared))
        if missing:
            print(f"[{name:<7}] 缺 {len(missing)} 个定义: {', '.join(missing)}")
            bad += 1
        else:
            print(f"[{name:<7}] 全 {len(declared)} 个都有定义")
        if extra:
            print(f"[{name:<7}] 另有多出未在头文件声明的: {', '.join(extra)}")

    if bad:
        print("\n失败：某个平台分支缺定义，在该平台上会链接失败。"
              "本机若只有 Windows 就发现不了，所以这项检查必须常跑。")
        return 1
    print("\n通过：每个平台分支都定义了全部导出函数。")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())