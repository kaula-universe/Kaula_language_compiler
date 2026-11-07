#!/bin/env python3
# -*- coding: utf-8 -*-
"""跨语言工具链适配：把一份源码编译并运行，返回 (ok, 输出)。

六种语言的编译器调用集中在这里，measure_c 的"编译-报错-重试"循环复用它。
"""
import os
import shutil
import subprocess
from pathlib import Path

EXT = {"py": ".py", "go": ".go", "cpp": ".cpp", "c": ".c", "rs": ".rs", "kl": ".kl"}

GO = shutil.which("go") or "/opt/go/bin/go"
RUSTC = shutil.which("rustc") or os.path.expanduser("~/.cargo/bin/rustc")
KAULAC = os.environ.get("KAULAC") or str(
    Path(__file__).resolve().parents[2] / "build" / "bin" / "kaulac"
)


def available(lang: str) -> bool:
    if lang == "py":
        return True
    if lang == "go":
        return os.path.exists(GO)
    if lang in ("c", "cpp"):
        return shutil.which("gcc" if lang == "c" else "g++") is not None
    if lang == "rs":
        return os.path.exists(RUSTC)
    if lang == "kl":
        return os.path.exists(KAULAC)
    return False


def build_and_run(lang: str, source: str, workdir: Path, timeout: int = 180) -> tuple[bool, str]:
    """写入源码 → 编译 → 运行。返回 (是否成功, 编译器/运行输出)。"""
    workdir.mkdir(parents=True, exist_ok=True)
    src = workdir / ("prog" + EXT[lang])
    src.write_text(source, encoding="utf-8")
    exe = workdir / "prog.bin"

    if lang == "py":
        cmd, run = ["python3", str(src)], ["python3", str(src)]
    elif lang == "go":
        cmd, run = [GO, "build", "-o", str(exe), str(src)], [str(exe)]
    elif lang == "cpp":
        cmd, run = ["g++", "-O2", "-o", str(exe), str(src)], [str(exe)]
    elif lang == "c":
        cmd, run = ["gcc", "-O2", "-o", str(exe), str(src), "-lm"], [str(exe)]
    elif lang == "rs":
        cmd, run = [RUSTC, "-O", "-o", str(exe), str(src)], [str(exe)]
    else:  # kl
        cmd, run = [KAULAC, "-output-dir", str(workdir), str(src)], [str(workdir / "prog")]

    try:
        b = subprocess.run(cmd, capture_output=True, text=True, timeout=timeout)
    except subprocess.TimeoutExpired:
        return False, "compiler timeout"
    if b.returncode != 0:
        return False, (b.stdout + b.stderr).strip()[-2000:]

    exe_path = run[0]
    if not os.path.exists(exe_path) and lang == "kl":
        cand = list(workdir.glob("prog*"))
        if not cand:
            return False, "no output binary"
        exe_path = str(cand[0])
        run = [exe_path]

    try:
        r = subprocess.run(run, capture_output=True, text=True, timeout=timeout)
    except subprocess.TimeoutExpired:
        return False, "run timeout"
    out = (r.stdout + r.stderr).strip()
    return r.returncode == 0, out
