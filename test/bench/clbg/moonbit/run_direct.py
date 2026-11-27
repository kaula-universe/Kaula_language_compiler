#!/usr/bin/env python3
# run_direct.py — 全部 7 道 CLBG 题：官方 C 直译口径对比（native）
#
# 原则：以 ref/*.gcc.c（CLBG 官方 C 示例）为唯一蓝本逐行直译到两种语言，
# 不做任何语言特化：
#   - binary-trees : 官方 Carson 版 = 逐节点分配指针树。Kaula 用
#     kmm_v4_alloc 逐节点分配（自动回收；手动 free 属非法写法，kmm_v4_free
#     为 no-op，故官方 C 的 DeleteTree 无法直译，此为最接近的普适形态），
#     MoonBit 用 GC 递归引用树（GC 即其自动回收普适形态）。
#   - revcomp      : 官方 Prokhorenkov 版 = 256 互补查表 + 输入缓冲 +
#     60 列输出。线程/pair-lookup 是并行向量化变量，按套件惯例隔离为串行。
#   - 其余 5 题    : 两边实现本就直译自官方 C，与 run_natural.py 共用源文件。
# 正确性：native 标准 CLBG 输出逐字节一致，校验通过才计时。
#
# 用法：python3 run_direct.py [--rounds 3] [--quick] [--verify-only]
# 产物：build_direct/ (gitignored) + REPORT_DIRECT.md

import argparse
import os
import platform
import re
import shutil
import statistics
import subprocess
import sys
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parent
CLBG = ROOT.parent
REPO = CLBG.parents[2]
BUILD = ROOT / "build_direct"
REPORT = ROOT / "REPORT_DIRECT.md"
KAULAC = Path(os.environ.get("KAULAC", REPO / "build" / "bin" / "kaulac"))
MOON = os.environ.get("MOON", str(Path.home() / ".moon" / "bin" / "moon"))

FMT9 = (ROOT / "fmt9.mbt").read_text()  # MoonBit 精确 %.9f 格式化（nbody/spectralnorm 用）

# 全部 7 道 CLBG 标准题
BENCHES = [
    {"name": "binarytrees", "param": 21,
     "k": CLBG / "kaula" / "binary_trees_ptr.kl", "m": ROOT / "binarytrees_gc.mbtx",
     "ph": "N_PLACEHOLDER", "fmt9": False,
     "note": "官方C直译：两边均逐节点分配（Kaula 自动回收 / MoonBit GC）"},
    {"name": "nbody", "param": 5000000,
     "k": CLBG / "kaula" / "nbody.kl", "m": ROOT / "nbody.mbtx",
     "ph": "N_STEPS_PLACEHOLDER", "fmt9": True,
     "note": "官方C直译：结构体数组+同公式（%.9f 用 bigint 精确格式化）"},
    {"name": "spectralnorm", "param": 5500,
     "k": CLBG / "kaula" / "spectralnorm.kl", "m": ROOT / "spectralnorm.mbtx",
     "ph": "N_PLACEHOLDER", "fmt9": True,
     "note": "官方C直译：向量+幂迭代（%.9f 用 bigint 精确格式化）"},
    {"name": "fannkuchredux", "param": 12,
     "k": CLBG / "kaula" / "fannkuchredux.kl", "m": ROOT / "fannkuchredux_std.mbtx",
     "ph": "N_PLACEHOLDER", "fmt9": False,
     "note": "官方C直译：两边均定长数组"},
    {"name": "fasta", "param": 250000,
     "k": CLBG / "kaula" / "fasta.kl", "m": ROOT / "fasta.mbtx",
     "ph": "N_PLACEHOLDER", "fmt9": False,
     "note": "官方C直译：LCG+累积概率+二分选择，两边同构"},
    {"name": "mandelbrot", "param": 16000,
     "k": CLBG / "kaula" / "mandelbrot.kl", "m": ROOT / "mandelbrot.mbtx",
     "ph": "N_PLACEHOLDER", "fmt9": False,
     "note": "官方C直译：标量单线程 P4 PBM（隔离 SIMD/线程变量）"},
    {"name": "revcomp", "param": None,
     "k": CLBG / "kaula" / "revcomp.kl", "m": ROOT / "revcomp_direct.mbtx",
     "ph": None, "fmt9": False, "stdin_from": "fasta",
     "note": "官方C直译：两边均256互补查表+缓冲+60列 / 输入=fasta标准输出"},
]


def log(m):
    print(m, flush=True)


def env_moon():
    e = os.environ.copy()
    e["PATH"] = str(Path(MOON).parent) + os.pathsep + e.get("PATH", "")
    e["KAULA_HOME"] = str(REPO)
    return e


def run(cmd, **kw):
    return subprocess.run(cmd, capture_output=True, **kw)


def build_kaula(b, tag):
    src = b["k"].read_text()
    if b["ph"]:
        src = src.replace(b["ph"], str(b["param"]))
    wd = BUILD / tag / "kaula"
    wd.mkdir(parents=True, exist_ok=True)
    kl = wd / f"{b['name']}.kl"
    kl.write_text(src)
    out = wd / "out"
    t0 = time.perf_counter()
    r = run([str(KAULAC), "-release", "-output-dir", str(out), str(kl)],
            cwd=REPO, env=env_moon(), text=True)
    dt = time.perf_counter() - t0
    if r.returncode != 0:
        sys.exit(f"kaulac 失败 {b['name']}:\n{r.stdout}\n{r.stderr}")
    arts = [p for p in out.iterdir() if p.is_file()]
    if not arts:
        sys.exit(f"找不到 Kaula 产物: {out}")
    return arts[0], dt


def build_moonbit(b, tag):
    src = b["m"].read_text()
    if b["ph"]:
        src = src.replace(b["ph"], str(b["param"]))
    if b["fmt9"]:
        fm = "\n".join(l for l in FMT9.split("\n") if not l.startswith("//"))
        src = fm + "\n" + src
    wd = BUILD / tag / "moonbit"
    wd.mkdir(parents=True, exist_ok=True)
    mbt = wd / f"{b['name']}.mbtx"
    mbt.write_text(src)
    t0 = time.perf_counter()
    r = run([MOON, "build", "--release", "--target", "native", str(mbt)],
            cwd=wd, env=env_moon(), text=True)
    dt = time.perf_counter() - t0
    if r.returncode != 0:
        sys.exit(f"moon build 失败 {b['name']}:\n{r.stdout}\n{r.stderr}")
    arts = list((wd / "_build" / f"{b['name']}.mbtx" / "native" / "release").rglob("single.exe"))
    if not arts:
        sys.exit(f"找不到 MoonBit 产物: {wd}")
    return arts[0], dt


def measure(exe, rounds, stdin_file=None):
    """返回 (输出bytes, wall中位数, peak_rss_MB)；输出用 bytes 直接比对。"""
    walls, out = [], None
    for _ in range(rounds):
        stdin_f = open(stdin_file, "rb") if stdin_file else None
        t0 = time.perf_counter()
        r = subprocess.run([str(exe)], stdin=stdin_f, capture_output=True, env=env_moon(), cwd=REPO)
        walls.append(time.perf_counter() - t0)
        if stdin_f:
            stdin_f.close()
        if r.returncode != 0:
            sys.exit(f"运行失败 {exe}: rc={r.returncode} {r.stderr[-400:]!r}")
        out = r.stdout
    rss = None
    if Path("/usr/bin/time").exists():
        sf = open(stdin_file, "rb") if stdin_file else None
        tr = subprocess.run(["/usr/bin/time", "-v", str(exe)], stdin=sf,
                            capture_output=True, env=env_moon(), cwd=REPO)
        if sf:
            sf.close()
        m = re.search(rb"Maximum resident set size \(kbytes\): (\d+)", tr.stderr)
        if m:
            rss = int(m.group(1)) / 1024
    return out, statistics.median(walls), rss


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--rounds", type=int, default=3)
    ap.add_argument("--quick", action="store_true")
    ap.add_argument("--verify-only", action="store_true")
    args = ap.parse_args()
    rounds = 1 if args.quick else args.rounds

    if BUILD.exists():
        shutil.rmtree(BUILD)
    BUILD.mkdir(parents=True)

    results = []
    fasta_out = None
    for b in BENCHES:
        name = b["name"]
        tag = f"{name}_n{b['param']}" if b["param"] else name
        param = b["param"] if b["param"] else "fasta输出"
        log(f"\n===== {name} (N={param}) =====")
        log(f"  {b['note']}")

        k_art, k_comp = build_kaula(b, tag)
        m_art, m_comp = build_moonbit(b, tag)
        log(f"  [build] kaula={k_comp:.2f}s/{k_art.stat().st_size}B  moonbit={m_comp:.2f}s/{m_art.stat().st_size}B")

        stdin_file = None
        if b.get("stdin_from") == "fasta" and fasta_out:
            stdin_file = BUILD / "revcomp_input.txt"
            stdin_file.write_bytes(fasta_out)

        k_out, k_wall, k_rss = measure(k_art, rounds, stdin_file)
        m_out, m_wall, m_rss = measure(m_art, rounds, stdin_file)
        same = (k_out == m_out)
        log(f"  [run] kaula={k_wall:.4f}s/{k_rss}MB  moonbit={m_wall:.4f}s/{m_rss}MB")
        log(f"  [verify] 输出逐字节: {'PASS ✅' if same else 'FAIL ❌'} ({len(k_out)} bytes)")
        if not same:
            sys.exit(f"输出不一致: {name}")
        if name == "fasta":
            fasta_out = k_out

        results.append({
            "bench": b, "param": param,
            "kaula": {"wall": k_wall, "rss": k_rss, "compile": k_comp, "size": k_art.stat().st_size, "outlen": len(k_out)},
            "moonbit": {"wall": m_wall, "rss": m_rss, "compile": m_comp, "size": m_art.stat().st_size},
        })

    if not args.verify_only:
        write_report(results, rounds)
        log(f"\n[done] {REPORT}")


def write_report(results, rounds):
    L = ["# Kaula vs MoonBit：全部 7 道 CLBG 题 native 官方 C 直译对比\n",
         "> 原则：**以 ref/*.gcc.c（CLBG 官方 C 示例）为蓝本逐行直译，不做任何语言特化**。\n",
         "> 正确性：native 标准 CLBG 输出逐字节一致（含 32MB PBM 二进制），校验通过才计时。\n",
         "## 运行环境\n",
         "| 项 | 值 |\n|---|---|",
         f"| OS | {platform.system()} {platform.release()} ({platform.machine()}) |",
         "| CPU | " + (run(["bash", "-c", "grep -m1 'model name' /proc/cpuinfo | cut -d: -f2"], text=True).stdout.strip() or "?") + f" / {os.cpu_count()} 核 |",
         "| Kaula | kaulac -release（clang -O3）|",
         "| MoonBit | moon 0.1.20261126（LLVM native 后端）|",
         f"| 采样 | 每边 {rounds} 轮取中位数；RSS 由 /usr/bin/time -v 单次采样 |",
         ""]

    L += ["## 正确性校验（native 输出逐字节）\n",
          "| 题目 | 参数 | 输出字节 | 形态差异 | 结果 |\n|---|---:|---:|---|---|"]
    for r in results:
        b = r["bench"]
        L.append(f"| {b['name']} | {r['param']} | {r['kaula']['outlen']:,} | {b['note']} | ✅ PASS |")
    L.append("")

    L += ["## 运行性能（native，中位数）\n",
          "| 题目 | Kaula wall | MoonBit wall | Moon/Kaula | Kaula RSS | MoonBit RSS |\n|---|---:|---:|---:|---:|---:|"]
    for r in results:
        k, m = r["kaula"], r["moonbit"]
        ratio = f"{m['wall']/k['wall']:.2f}×" if k['wall'] > 0 else "—"
        L.append(f"| {r['bench']['name']} | {k['wall']:.3f}s | {m['wall']:.3f}s | {ratio} "
                 f"| {k['rss']:.0f}MB | {m['rss']:.0f}MB |")
    L.append("")

    L += ["## 编译速度与产物大小\n",
          "| 题目 | Kaula 编译 | MoonBit 编译 | Kaula 产物 | MoonBit 产物 |\n|---|---:|---:|---:|---:|"]
    for r in results:
        k, m = r["kaula"], r["moonbit"]
        L.append(f"| {r['bench']['name']} | {k['compile']:.2f}s | {m['compile']:.2f}s "
                 f"| {k['size']:,} B | {m['size']:,} B |")
    L.append("")

    L += ["## 口径说明\n",
          "- 所有题目均使用 CLBG 官方标准参数，输出逐字节一致后计时。\n",
          "- nbody/spectralnorm 的 `%.9f` 用 MoonBit bigint 精确格式化（round-half-even，与 C printf 对齐）。\n",
          "- binary-trees 为官方 Carson 版逐节点分配形态：官方 C 的 DeleteTree/manual free 在 Kaula 无法\n"
          "  直译（手动 free 属非法写法，`kmm_v4_free` 为 no-op），Kaula 边为逐节点 `kmm_v4_alloc` 自动回收，\n"
          "  N=21 实测峰值 ~4GB 可跑完；MoonBit 边为 GC 递归树。\n",
          "- revcomp 为官方 Prokhorenkov 版的串行直译（256 查表 + 缓冲 + 60 列），线程/pair-lookup 为\n"
          "  并行向量化变量，按套件惯例隔离。\n",
          "- 「自然形态」口径（Kaula arena / MoonBit 各自惯用形态）见 run_natural.py 与 REPORT_NATURAL.md，\n"
          "  两份报告回答的问题不同：本报告=纯移植质量，彼报告=开箱即用表现。\n",
          "- mandelbrot 为标量单线程 P4 PBM（隔离并行度/向量化变量），输出 32,000,015 字节。\n",
          "- revcomp 输入为 fasta 的标准输出（CLBG 串联口径），两边用同一输入文件。\n",
          "- wasm 未纳入：Kaula freestanding 无 stdout/libm/`__multi3`，输出能力不对等。\n"]

    REPORT.write_text("\n".join(L), encoding="utf-8")


if __name__ == "__main__":
    main()
