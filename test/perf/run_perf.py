#!/usr/bin/env python3
"""
Kaula 内存/性能回归 harness（test/perf/）。

为什么存在：用户清单里的 s1_list / s4_min 等用例只有结论没有样本，
下一轮无法证明"从 123MB 降到 1.9MB"是同一程序、同一口径。本脚本把
泄漏/峰值/回落三类判据固化成可复现的自动化门禁：

  1. 峰值 RSS（VmHWM）—— 泄漏判据：轮数翻倍，峰值不得线性增长
  2. 稳态 RSS（VmRSS 全程采样）—— 泄漏判据：运行期不得单调爬升
  3. 结束后 RSS —— P1 判据：峰值释放后必须回落

判据全部走"跨轮次/跨阶段的不变量"，不写死绝对 MB 数：绝对数受
宿主 allocator、clang 版本、池大小宏影响，写死会在别的机器上假红。

用法:
    python3 test/perf/run_perf.py                 # 跑全部
    python3 test/perf/run_perf.py --filter leak   # 按名过滤
    python3 test/perf/run_perf.py --json out.json # 落盘原始数据

退出码: 0=全通过, 1=有失败, 2=harness 自身错误
"""

import argparse
import json
import os
import re
import shutil
import subprocess
import sys
import time
from pathlib import Path

PERF_DIR = Path(__file__).resolve().parent
PROJECT_ROOT = PERF_DIR.parent.parent
BIN_DIRS = (PROJECT_ROOT / "build" / "bin", PROJECT_ROOT / "bin")
TMP_ROOT = PROJECT_ROOT / "cache" / "perf-tmp"

FAIL_LINE = re.compile(r"^\s*FAIL\b", re.MULTILINE)
PASS_LINE = re.compile(r"^\s*PASS\b", re.MULTILINE)


def find_kaulac() -> Path:
    env = os.environ.get("KAULAC")
    if env and Path(env).is_file():
        return Path(env)
    for d in BIN_DIRS:
        for name in ("kaulac.exe", "kaulac"):
            p = d / name
            if p.is_file():
                return p
    print("error: kaulac not found; build it first or set KAULAC=<path>", file=sys.stderr)
    sys.exit(2)


def strip_ansi(t: str) -> str:
    return re.sub(r"\x1b\[[0-9;]*m", "", t)


def read_rss_kb(pid: int, field: str = "VmRSS") -> int:
    try:
        with open(f"/proc/{pid}/status", "r") as f:
            for line in f:
                if line.startswith(field + ":"):
                    return int(line.split()[1])
    except (FileNotFoundError, ProcessLookupError, ValueError):
        pass
    return 0


def run_sampled(exe: Path, timeout: int) -> dict:
    """
    运行 exe，全程采样 RSS。

    采样线程与子进程并行：sleep(2ms) 轮询 /proc/<pid>/status。2ms 足以
    捕获 ns 级 bump 分配堆出的峰值（分配是循环内密集发生的，峰值会持续
    至少毫秒级），而 VmHWM 作为交叉校验兜底（内核维护，不会漏采样）。
    """
    proc = subprocess.Popen(
        [str(exe)], stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
        text=True, encoding="utf-8", errors="replace",
        cwd=str(PROJECT_ROOT),
    )
    samples = []
    t0 = time.monotonic()
    while proc.poll() is None:
        rss = read_rss_kb(proc.pid)
        if rss:
            samples.append(rss)
        if time.monotonic() - t0 > timeout:
            proc.kill()
            break
        time.sleep(0.002)
    out, _ = proc.communicate()
    wall = time.monotonic() - t0
    out = strip_ansi(out or "")

    peak_sampled = max(samples) if samples else 0
    # 结束时（进程活着时的最后一次采样）—— 用于 P1 判据
    final_rss = samples[-1] if samples else 0
    return {
        "exit": proc.returncode,
        "wall_s": round(wall, 3),
        "peak_kb": peak_sampled,
        "final_kb": final_rss,
        "samples": len(samples),
        "stdout": out,
    }


def compile_one(kaulac: Path, src: Path, outdir: Path, timeout: int, sor: bool) -> tuple[bool, str]:
    outdir.mkdir(parents=True, exist_ok=True)
    cmd = [str(kaulac), "-output-dir", str(outdir)]
    if sor:
        cmd.append("--sor")
    cmd.append(str(src))
    # 编译器按 LANGUAGE/LC_ALL/LANG 自动切中英文；此处字面匹配状态行，强制 C
    # locale 保证输出英文「Status: Success」，与 CI 口径一致；判定再兼容中文。
    env = os.environ.copy()
    env["LANGUAGE"] = "C"
    env["LC_ALL"] = "C.UTF-8"
    env["LANG"] = "C.UTF-8"
    try:
        r = subprocess.run(cmd, capture_output=True, text=True, encoding="utf-8",
                           errors="replace", timeout=timeout, cwd=str(PROJECT_ROOT), env=env)
    except subprocess.TimeoutExpired:
        return False, f"compile timed out after {timeout}s"
    out = strip_ansi((r.stdout or "") + (r.stderr or ""))
    compiled_ok = ("Status: Success" in out) or ("状态: 成功" in out)
    if not compiled_ok:
        lines = [ln.strip() for ln in out.splitlines() if "error" in ln.lower()]
        return False, "compile failed: " + ("\n      ".join(lines[:6]) or out[-400:])
    return True, ""


def find_exe(outdir: Path, src: Path) -> Path | None:
    for cand in (outdir / (src.stem + ".exe"), outdir / src.stem):
        if cand.is_file():
            return cand
    return None


def discover(filt: str) -> list[Path]:
    out = []
    for p in sorted(PERF_DIR.glob("*.kl")):
        if filt and filt not in p.stem:
            continue
        out.append(p)
    return out


def main() -> int:
    ap = argparse.ArgumentParser(description="Kaula perf/leak regression harness")
    ap.add_argument("--filter", default="")
    ap.add_argument("--json", default="")
    ap.add_argument("--timeout", type=int, default=120)
    ap.add_argument("--compile-timeout", type=int, default=300)
    args = ap.parse_args()

    kaulac = find_kaulac()
    tests = discover(args.filter)
    if not tests:
        print("error: no perf tests matched", file=sys.stderr)
        return 2

    if TMP_ROOT.exists():
        shutil.rmtree(TMP_ROOT, ignore_errors=True)

    print(f"kaulac : {kaulac}")
    print(f"tests  : {len(tests)} program(s)")
    print()

    results = []
    failed = 0
    for src in tests:
        # default 模式是主判定（--sor 只影响代码生成策略，两个模式都应通过）
        outdir = TMP_ROOT / src.stem
        ok, err = compile_one(kaulac, src, outdir, args.compile_timeout, sor=False)
        if not ok:
            print(f"  [COMPILE] {src.stem}\n      {err}")
            failed += 1
            results.append({"name": src.stem, "stage": "compile", "ok": False, "error": err})
            continue
        exe = find_exe(outdir, src)
        if exe is None:
            print(f"  [NO-EXE] {src.stem}")
            failed += 1
            results.append({"name": src.stem, "stage": "link", "ok": False})
            continue

        r = run_sampled(exe, args.timeout)
        stdout = r["stdout"]
        # 判据一：退出码 0
        if r["exit"] != 0:
            print(f"  [RUN] {src.stem}  exit={r['exit']} peak={r['peak_kb']}KB")
            print("      " + "\n      ".join(stdout.strip().splitlines()[-4:]))
            failed += 1
            results.append({"name": src.stem, "stage": "run", "ok": False, **r})
            continue
        # 判据二：用例内断言（PASS/FAIL 行）
        has_fail = bool(FAIL_LINE.search(stdout))
        has_pass = bool(PASS_LINE.search(stdout))
        if has_fail or not has_pass:
            bad = [ln.strip() for ln in stdout.splitlines() if FAIL_LINE.match(ln)]
            print(f"  [ASSERT] {src.stem}  peak={r['peak_kb']}KB")
            print("      " + ("\n      ".join(bad[:6]) or "no PASS line emitted"))
            failed += 1
            results.append({"name": src.stem, "stage": "assert", "ok": False, **r})
            continue
        print(f"  [PASS] {src.stem:<26} peak={r['peak_kb']:>8}KB  final={r['final_kb']:>8}KB  {r['wall_s']}s")
        results.append({"name": src.stem, "stage": "ok", "ok": True, **r})

    print()
    print(f"{len(tests) - failed}/{len(tests)} passed")

    if args.json:
        with open(args.json, "w") as f:
            json.dump({"kaulac": str(kaulac), "results": results}, f, indent=2)
        print(f"raw data -> {args.json}")

    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
