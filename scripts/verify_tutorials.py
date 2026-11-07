#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Kaula 教程语料自动验证器 / Tutorial corpus verifier

把 docs/tutorials/*.md 里的每一个 ```kaula 代码块提取出来，包装成可独立
编译运行的完整程序，逐一编译并运行，报告哪些示例失效。

设计目标（与 scripts/run_tests.py 同构）：
    教程示例一旦与语言实现脱节，就是文档 bug。本脚本是守门人，CI 可调用。

包装规则（保守、可预期）：
    1. 块内的 import 行一律提到文件顶部（去重）。
    2. 顶层声明（extern/fn/struct/enum/type/const/pub/use/impl/trait）原样保留在外层。
    3. 其余语句（变量定义、表达式、注释、空行）塞进自动生成的 main()。
    4. 块内已含 fn main() 时不再包装，直接整体编译。
    5. 片段末尾出现的注释（# ... 行内预期输出）保留，不参与执行。

用法：
    python3 scripts/verify_tutorials.py            # 全量
    python3 scripts/verify_tutorials.py --list    # 列出将验证的块
    python3 scripts/verify_tutorials.py --only prefix-system   # 只验某个 md
退出码: 0 全部通过, 1 至少一个失败, 2 harness 错误。
"""

import argparse
import os
import re
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parent.parent
TUTORIAL_DIR = PROJECT_ROOT / "docs" / "tutorials"
BIN_DIRS = (PROJECT_ROOT / "build" / "bin", PROJECT_ROOT / "bin")

_ANSI_RE = re.compile(r"\x1b\[[0-9;]*m")


def strip_ansi(s: str) -> str:
    return _ANSI_RE.sub("", s)

# 顶层声明起始前缀（这些行必须留在函数外）
TOPLEVEL_RE = re.compile(
    r"^\s*(import\b|extern\s+fn\b|extern\s+block\b|fn\b|pub\s+fn\b|"
    r"struct\b|pub\s+struct\b|enum\b|pub\s+enum\b|"
    r"type\b|pub\s+type\b|const\b|pub\s+const\b|"
    r"pub\b|use\b|impl\b|trait\b|interface\b|class\b|"
    r"prefix\b|sor\b)\b"
)


def find_kaulac() -> Path:
    env = os.environ.get("KAULAC")
    if env:
        p = Path(env)
        if p.is_file():
            return p
    for d in BIN_DIRS:
        cand = d / "kaulac"
        if cand.is_file():
            return cand
    sys.exit("kaulac not found; build it first (python toolkit_build.py --target compiler)")


def extract_blocks(md_path: Path):
    """返回 [(block_index, code_str)]。

    兼容两种围栏写法：
      - 标准：围栏独立成行（```kaula 换行后才是代码）
      - 同行：```kaulafn main() {（围栏与代码首行同行），此时去掉
        ```kaula 前缀，剩余内容作为块首行；否则会丢掉 fn/struct 首行，
        包装出的程序残缺无法编译。
    闭块同样兼容同行形式（}``` 或 // 注释``` ），剥离围栏取代码末行。
    """
    text = md_path.read_text(encoding="utf-8")
    blocks = []
    in_block = False
    buf = []
    for line in text.splitlines():
        s = line.strip()
        if not in_block:
            if s.startswith("```kaula"):
                in_block = True
                buf = []
                rest = s[len("```kaula"):]
                if rest.strip():
                    buf.append(rest.strip())  # 同行代码首行（如 fn main() {）
                continue
            continue
        close = s.rfind("```")
        if close >= 0:
            if close == 0:
                blocks.append("\n".join(buf))
                in_block = False
            else:
                buf.append(s[:close].rstrip())
                blocks.append("\n".join(buf))
                in_block = False
            continue
        if in_block:
            buf.append(line)
    return blocks


def wrap_program(code: str) -> str:
    """把一个代码块包装成完整可编译程序。

    块级策略（不逐行切散多行 fn/struct/enum 体）：
      - 已含 fn main()：原样输出（仅把散落的 import 提到顶部去重）。
      - 否则：保留 import；把"顶层声明行"（extern/fn/struct/enum/type/const
        的首行及其配对花括号体）留在外层；其余独立语句缩进塞进自动 main()。
        多行声明用花括号配对跟踪，避免把函数体错切到 main。
    """
    lines = code.splitlines()
    imports = []
    rest = []
    for ln in lines:
        if ln.strip().startswith("import "):
            imports.append(ln.strip())
        else:
            rest.append(ln)

    # 去重 import 保序
    seen = set()
    imp_out = []
    for i in imports:
        if i not in seen:
            seen.add(i)
            imp_out.append(i)

    body = "\n".join(rest)
    if re.search(r"\bfn\s+main\s*\(", body):
        parts = imp_out + [body]
        return "\n\n".join(p for p in parts if p.strip())

    # 无 main：按顶层声明 vs 函数内语句切分。
    # 跟踪花括号深度，让多行声明整体留在外层。
    top_lines = []
    stmt_lines = []
    depth = 0
    in_toplevel = False
    for ln in rest:
        s = ln.strip()
        if depth == 0 and TOPLEVEL_RE.match(ln):
            in_toplevel = True
        if in_toplevel:
            top_lines.append(ln)
        else:
            stmt_lines.append(ln)
        # 更新花括号深度（忽略行内字符串里的括号做近似）
        opens = ln.count("{") - ln.count("'\"")  # 粗近似
        depth += ln.count("{") - ln.count("}")
        if depth <= 0 and in_toplevel:
            in_toplevel = False
            depth = 0

    parts = list(imp_out)
    if top_lines:
        parts.append("\n".join(top_lines))
    if stmt_lines:
        indented = "\n".join("    " + l if l.strip() else l for l in stmt_lines)
        parts.append(f"fn main() {{\n{indented}\n}}")
    else:
        parts.append("fn main() {}")
    return "\n\n".join(p for p in parts if p.strip())


def verify_one(kaulac: Path, code: str, work: Path, timeout: int) -> tuple[bool, str]:
    src = work / "sample.kl"
    src.write_text(code, encoding="utf-8")
    env = dict(os.environ)
    # 强制英文：harness 按字面匹配 "Status: Success"（与 scripts/run_tests.py 一致）
    env["LANG"] = "C.UTF-8"
    env["LC_ALL"] = "C.UTF-8"
    env["LANGUAGE"] = "C"
    env.setdefault("KAULA_HOME", str(PROJECT_ROOT))
    env.setdefault("KAULA_SOURCE_MODE", "1")
    cmd = [str(kaulac), "-output-dir", str(work), str(src)]
    try:
        r = subprocess.run(cmd, capture_output=True, text=True, encoding="utf-8",
                           errors="replace", timeout=timeout, cwd=str(PROJECT_ROOT), env=env)
    except subprocess.TimeoutExpired:
        return False, "compile timeout"
    out = strip_ansi((r.stdout or "") + (r.stderr or ""))
    if "Status: Success" not in out:
        tail = "\n      ".join([l for l in out.splitlines() if l.strip()][-12:])
        return False, f"compile failed\n      {tail}"
    exe = work / "sample"
    if not exe.exists():
        return False, "binary not produced"
    try:
        rr = subprocess.run([str(exe)], capture_output=True, text=True,
                            encoding="utf-8", errors="replace", timeout=timeout)
    except subprocess.TimeoutExpired:
        return False, "run timeout"
    if rr.returncode != 0:
        tail = "\n      ".join((rr.stdout + rr.stderr).strip().splitlines()[-8:])
        return False, f"run exit {rr.returncode}\n      {tail}"
    return True, ""


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--only", help="只验证某个教程文件名（不含 .md）")
    ap.add_argument("--list", action="store_true")
    ap.add_argument("--timeout", type=int, default=60)
    args = ap.parse_args()

    kaulac = find_kaulac()
    mds = sorted(TUTORIAL_DIR.glob("*.md"))
    if args.only:
        mds = [m for m in mds if m.stem == args.only]

    total = 0
    fails = []
    for md in mds:
        blocks = extract_blocks(md)
        for idx, code in enumerate(blocks, 1):
            total += 1
            label = f"{md.stem}#{idx}"
            prog = wrap_program(code)
            if args.list:
                print(f"[{label}]")
                continue
            with tempfile.TemporaryDirectory() as td:
                ok, msg = verify_one(kaulac, prog, Path(td), args.timeout)
            status = "PASS" if ok else "FAIL"
            print(f"  {status:4} {label}")
            if not ok:
                fails.append((label, code, msg))

    if args.list:
        print(f"\n{total} blocks")
        return

    print(f"\n=== {total - len(fails)}/{total} tutorial blocks pass ===")
    if fails:
        print("\nFAILURES:")
        for label, code, msg in fails:
            print(f"\n----- {label} -----")
            print("```kaula")
            print(code)
            print("```")
            print(msg)
        sys.exit(1)


if __name__ == "__main__":
    main()
