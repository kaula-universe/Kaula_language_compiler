#!/usr/bin/env python3
"""
Kaula Language Test Runner / Kaula 语言测试运行器

Compiles and runs every test/*.kl program in both allocator modes
(default and --sor) and reports failures. This is the gate that keeps
language-level regressions out of a release: the CI workflows call this
before packaging.

Why this exists / 为什么需要它:
    Two real bugs shipped through v1.0.x unnoticed because nothing ran the
    corpus automatically:
      - chained calls a.b().c() were dropped by the parser (undefined_function)
      - interface methods returning an interface mis-parsed, with errors
        reported on completely unrelated lines
    Both were caught only by hand-running this corpus.

Usage / 用法:
    python scripts/run_tests.py                    # full run
    python scripts/run_tests.py --quick            # skip slow benchmarks
    python scripts/run_tests.py --filter stress    # only matching programs
    python scripts/run_tests.py --mode default     # single mode
    python scripts/run_tests.py --list             # show what would run
    python scripts/run_tests.py --jobs 4           # parallel (default: CPU count)

Exit code / 退出码:
    0 = all passed, 1 = at least one failure, 2 = harness error
"""

import argparse
import concurrent.futures
import os
import re
import shutil
import subprocess
import sys
import time
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parent.parent
TEST_DIR = PROJECT_ROOT / "test"
# toolkit_build.py --target compiler installs into build/bin (kaulac resolves
# stdlib.json and the toolchain relative to its own path, so the binary has to
# sit next to stdlib.json). bin/ is checked second for older layouts.
BIN_DIRS = (PROJECT_ROOT / "build" / "bin", PROJECT_ROOT / "bin")

# Programs that take seconds each (multi-threaded allocator stress and
# throughput benchmarks). They are correctness tests too, just slow ones, so
# --quick drops them from the default run and --all-slow opts back in.
SLOW_TESTS = {
    "memMT_large",
    "memMT_large_4t",
    "memMT_small",
    "memMT_small_4t",
    "mem_bench",
    "alloc_bench",
    "btree_bench",
}

# Modules without fn main() (e.g. test/stat_helper.kl, imported by other tests)
# are skipped: detection is automatic, see is_module().

MODES = ("default", "sor")

# A test passes only if the process exits 0 AND no line starts with FAIL.
# Several tests use a check(cond, label) helper that prints "FAIL <label>";
# others (the benchmark-style ones) only signal via exit code.
FAIL_LINE = re.compile(r"^\s*FAIL\b", re.MULTILINE)


def find_kaulac() -> Path:
    """Locate the kaulac binary. KAULAC env var wins, then build/bin, then bin/."""
    env = os.environ.get("KAULAC")
    if env:
        p = Path(env)
        if p.is_file():
            return p
        print(f"error: KAULAC={env} is not a file", file=sys.stderr)
        sys.exit(2)
    for d in BIN_DIRS:
        for name in ("kaulac.exe", "kaulac"):
            p = d / name
            if p.is_file():
                if not (d / "stdlib.json").is_file():
                    print(
                        f"warning: {p} found but {d / 'stdlib.json'} is missing; "
                        "kaulac will not be able to resolve stdlib signatures",
                        file=sys.stderr,
                    )
                return p
    print(
        "error: kaulac binary not found. Build it first:\n"
        "    python toolkit_build.py --target std --target runtime --target headers\n"
        "    python toolkit_build.py --target compiler\n"
        f"  (looked in {', '.join(str(d) for d in BIN_DIRS)}; set KAULAC=<path> to override)",
        file=sys.stderr,
    )
    sys.exit(2)


def read_source(path: Path) -> str:
    # utf-8-sig: the repo has historically had files with a BOM; the lexer
    # skips it but the module detection here would not.
    return path.read_text(encoding="utf-8-sig", errors="replace")


def is_module(path: Path) -> bool:
    """True if the file has no fn main() and so cannot be run standalone."""
    src = read_source(path)
    return not re.search(r"(?m)^\s*fn\s+main\s*\(", src)


def discover(quick: bool, include_slow: bool, filt: str) -> list[Path]:
    if not TEST_DIR.is_dir():
        print(f"error: {TEST_DIR} not found", file=sys.stderr)
        sys.exit(2)
    out = []
    for p in sorted(TEST_DIR.glob("*.kl")):
        if is_module(p):
            continue
        if quick and not include_slow and p.stem in SLOW_TESTS:
            continue
        if filt and filt not in p.stem:
            continue
        out.append(p)
    return out


def strip_ansi(text: str) -> str:
    return re.sub(r"\x1b\[[0-9;]*m", "", text)


def compile_one(kaulac: Path, src: Path, mode: str, outdir: Path, timeout: int) -> tuple[bool, str]:
    # Each (program, mode) gets its own output directory. Without this the two
    # modes of one program race on the same .exe: clang fails with "unable to
    # remove file: permission denied" while the sibling link is running it, and
    # the loser then executes a half-written binary (observed as exit
    # 0xC0000005). Separate dirs also keep the source tree clean.
    cmd = [str(kaulac), "-output-dir", str(outdir)]
    if mode == "sor":
        cmd.append("--sor")
    cmd.append(str(src))
    # 源码树模式：kaulac 从 KAULA_HOME 下的 src/ std/ 增量编译运行时对象再链接。
    # 未设置时默认指向仓库根，否则只带 bin/ 的工具链会因为找不到源码而全线失败。
    # 运行期链接模式：
    #   - 本地编译器的安装布局（<prefix>/bin + lib）可以走安装模式（直接链静态库）；
    #   - 其他情况（源码树、只带 bin/ 的占位目录）走源码树模式：kaulac 从
    #     KAULA_HOME 下的 src/ std/ 增量编译运行时对象再链接。KAULA_HOME 未设时
    #     指向仓库根，否则只带 bin/ 的工具链会因为找不到源码而全线链接失败。
    env = dict(os.environ)
    if not env.get("KAULA_SOURCE_MODE"):
        src_root = env.get("KAULA_HOME") or str(PROJECT_ROOT)
        if (Path(src_root) / "src" / "kaula.h").is_file():
            env["KAULA_HOME"] = src_root
            env["KAULA_SOURCE_MODE"] = "1"
            # KAULA_HOME 会抢在 exe 目录之前解析 stdlib.json；源码树根下没有
            # stdlib.json 时显式带上仓库里的那一份，避免静默退化为"无签名"。
            std_json = Path(src_root) / "compiler" / "stdlib.json"
            if std_json.is_file():
                cmd.extend(["-stdlib", str(std_json)])
    try:
        r = subprocess.run(
            cmd, capture_output=True, text=True, encoding="utf-8",
            errors="replace", timeout=timeout, cwd=str(PROJECT_ROOT), env=env,
        )
    except subprocess.TimeoutExpired:
        return False, f"compile timed out after {timeout}s"
    out = strip_ansi((r.stdout or "") + (r.stderr or ""))
    if r.returncode != 0 or "Status: Failed" in out or "Status: Success" not in out:
        lines = [ln.strip() for ln in out.splitlines() if "error" in ln.lower()]
        detail = "\n      ".join(lines[:6]) or out.strip()[-400:]
        return False, f"compile failed (exit {r.returncode})\n      {detail}"
    return True, ""


def find_exe(outdir: Path, src: Path) -> Path | None:
    for cand in (outdir / (src.stem + ".exe"), outdir / src.stem):
        if cand.is_file():
            return cand
    return None


def run_one(exe: Path, timeout: int) -> tuple[bool, str]:
    try:
        r = subprocess.run(
            [str(exe)], capture_output=True, text=True, encoding="utf-8",
            errors="replace", timeout=timeout, cwd=str(PROJECT_ROOT),
        )
    except subprocess.TimeoutExpired:
        return False, f"run timed out after {timeout}s"
    out = strip_ansi((r.stdout or "") + (r.stderr or ""))
    if r.returncode != 0:
        tail = "\n      ".join(out.strip().splitlines()[-6:])
        return False, f"exit code {r.returncode}\n      {tail}"
    if FAIL_LINE.search(out):
        bad = [ln.strip() for ln in out.splitlines() if FAIL_LINE.match(ln)]
        return False, "FAIL line(s):\n      " + "\n      ".join(bad[:6])
    return True, ""


def check_one(kaulac: Path, src: Path, mode: str, outdir: Path, timeout: int) -> tuple[str, str]:
    outdir.mkdir(parents=True, exist_ok=True)
    ok, err = compile_one(kaulac, src, mode, outdir, timeout)
    if not ok:
        return "COMPILE", err
    exe = find_exe(outdir, src)
    if exe is None:
        return "RUN", "no executable produced"
    ok, err = run_one(exe, timeout)
    if not ok:
        return "RUN", err
    return "PASS", ""


def main() -> int:
    ap = argparse.ArgumentParser(description="Run the Kaula language test corpus")
    ap.add_argument("--filter", default="", help="only programs whose name contains this")
    ap.add_argument("--mode", choices=MODES, default="", help="single mode (default: both)")
    ap.add_argument("--quick", action="store_true", help="skip slow benchmarks")
    ap.add_argument("--all-slow", action="store_true", help="with --quick, also run slow ones")
    ap.add_argument("--jobs", type=int, default=0, help="parallel workers (default: CPU count)")
    ap.add_argument("--timeout", type=int, default=180, help="per-program timeout, seconds")
    ap.add_argument("--list", action="store_true", help="list programs and exit")
    args = ap.parse_args()

    programs = discover(args.quick, args.all_slow, args.filter)
    if not programs:
        print("no test programs matched", file=sys.stderr)
        return 2
    modes = [args.mode] if args.mode else list(MODES)

    if args.list:
        for p in programs:
            tags = []
            if p.stem in SLOW_TESTS:
                tags.append("slow")
            print(f"  {p.stem}{'  [' + ','.join(tags) + ']' if tags else ''}")
        print(f"\n{len(programs)} programs x {len(modes)} mode(s)")
        return 0

    kaulac = find_kaulac()
    jobs = [(p, m) for p in programs for m in modes]
    workers = args.jobs if args.jobs > 0 else (os.cpu_count() or 2)
    # Each kaulac run shells out to clang; oversubscribing just adds contention.
    workers = max(1, min(workers, 8))

    print(f"kaulac : {kaulac}")
    print(f"tests  : {len(programs)} programs x {len(modes)} mode(s) = {len(jobs)} runs")
    print(f"jobs   : {workers}\n")

    # Stable per-(program, mode) output dirs, wiped at start. Stability matters:
    # kaulac caches the link step keyed on the output path, so a fresh temp dir
    # every run throws that away (measured: 7s -> 30s for the quick set).
    # Uniqueness matters too: with both modes writing the same .exe, clang fails
    # with "unable to remove file: permission denied" while the sibling is
    # running it, and the loser executes a half-written binary (0xC0000005).
    outroot = PROJECT_ROOT / "build" / "test-run"
    shutil.rmtree(outroot, ignore_errors=True)
    outroot.mkdir(parents=True, exist_ok=True)

    results: dict[tuple[Path, str], tuple[str, str]] = {}
    t0 = time.time()
    done = 0
    with concurrent.futures.ThreadPoolExecutor(max_workers=workers) as pool:
        futs = {
            pool.submit(
                check_one, kaulac, p, m, outroot / f"{p.stem}.{m}", args.timeout
            ): (p, m)
            for p, m in jobs
        }
        for fut in concurrent.futures.as_completed(futs):
            p, m = futs[fut]
            try:
                status, err = fut.result()
            except Exception as e:  # noqa: BLE001 - harness must not crash the run
                status, err = "ERROR", f"{type(e).__name__}: {e}"
            results[(p, m)] = (status, err)
            done += 1
            if status != "PASS":
                print(f"  [{status}] {p.stem} [{m}]")
            elif done % 20 == 0:
                print(f"  ... {done}/{len(jobs)}")

    elapsed = time.time() - t0
    failures = [(k, v) for k, v in results.items() if v[0] != "PASS"]
    passed = len(results) - len(failures)

    print()
    if failures:
        print(f"FAILURES ({len(failures)} of {len(results)}):\n")
        for (p, m), (status, err) in sorted(failures, key=lambda kv: (kv[0][0].stem, kv[0][1])):
            print(f"  {p.stem} [{m}]  {status}")
            print(f"      {err}\n")
    print(f"{passed}/{len(results)} passed in {elapsed:.1f}s")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
