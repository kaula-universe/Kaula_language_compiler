#!/usr/bin/env python3
# run_natural.py — 尊重语言自然形态的 Kaula vs MoonBit 对比
#
# 核心原则：让算法在两种语言上各自用自然形态实现，不为对齐而统一数据结构。
#   - binary-trees: Kaula 用 arena（其特性：连续数组 + 下标寻址），
#                   MoonBit 用 GC 递归引用树（其特性：struct Node{Node?} + 自动回收）。
#                   两边均输出标准 CLBG 格式，逐字节校验一致后才计时。
#   - fannkuch-redux: 两边都是定长数组（该算法的标准形态，无语言特性差异）。
#
# 维度：native 运行时间 + peak RSS、编译速度、产物大小。
# wasm 维度单列：Kaula wasm 走 freestanding 裸机（无 stdout），
#                 MoonBit wasm 带 WASI（有 stdout）——形态不同，分别记录。
#
# 用法：
#   python3 run_natural.py                 # 3 轮中位数
#   python3 run_natural.py --rounds 5 --quick
#
# 产物：build_natural/ (gitignored) + REPORT_NATURAL.md

import argparse
import os
import platform
import shutil
import statistics
import subprocess
import sys
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parent
CLBG = ROOT.parent
REPO = CLBG.parents[2]
BUILD = ROOT / "build_natural"
REPORT = ROOT / "REPORT_NATURAL.md"
KAULAC = Path(os.environ.get("KAULAC", REPO / "build" / "bin" / "kaulac"))
MOON = os.environ.get("MOON", str(Path.home() / ".moon" / "bin" / "moon"))

# 每项：两种语言各自的自然形态源码
BENCHES = [
    {"name": "binarytrees", "param": 21,
     "kaula_src": CLBG / "kaula" / "binary_trees.kl",
     "moon_src": ROOT / "binarytrees_gc.mbtx",
     "note": "Kaula=arena连续数组 / MoonBit=GC递归引用树"},
    {"name": "fannkuchredux", "param": 12,
     "kaula_src": CLBG / "kaula" / "fannkuchredux.kl",
     "moon_src": ROOT / "fannkuchredux_std.mbtx",
     "note": "两边均定长数组（算法标准形态，无语言差异）"},
]


def log(m):
    print(m, flush=True)


def env_moon():
    e = os.environ.copy()
    e["PATH"] = str(Path(MOON).parent) + os.pathsep + e.get("PATH", "")
    e["KAULA_HOME"] = str(REPO)
    return e


def run(cmd, **kw):
    return subprocess.run(cmd, capture_output=True, text=True, **kw)


def build_kaula(b, tag):
    src = b["kaula_src"].read_text().replace("N_PLACEHOLDER", str(b["param"]))
    wd = BUILD / tag / "kaula"
    wd.mkdir(parents=True, exist_ok=True)
    kl = wd / f"{b['name']}.kl"
    kl.write_text(src)
    out = wd / "out"
    t0 = time.perf_counter()
    r = run([str(KAULAC), "-release", "-output-dir", str(out), str(kl)],
            cwd=REPO, env=env_moon())
    dt = time.perf_counter() - t0
    if r.returncode != 0:
        sys.exit(f"kaulac 失败 {b['name']}:\n{r.stdout}\n{r.stderr}")
    arts = [p for p in out.iterdir() if p.is_file()]
    if not arts:
        sys.exit(f"找不到 Kaula 产物: {out}")
    return arts[0], dt


def build_moonbit(b, tag):
    src = b["moon_src"].read_text().replace("N_PLACEHOLDER", str(b["param"]))
    wd = BUILD / tag / "moonbit"
    wd.mkdir(parents=True, exist_ok=True)
    mbt = wd / f"{b['name']}.mbtx"
    mbt.write_text(src)
    t0 = time.perf_counter()
    r = run([MOON, "build", "--release", "--target", "native", str(mbt)],
            cwd=wd, env=env_moon())
    dt = time.perf_counter() - t0
    if r.returncode != 0:
        sys.exit(f"moon build 失败 {b['name']}:\n{r.stdout}\n{r.stderr}")
    arts = list((wd / "_build" / f"{b['name']}.mbtx" / "native" / "release").rglob("single.exe"))
    if not arts:
        sys.exit(f"找不到 MoonBit 产物: {wd}")
    return arts[0], dt


def measure(exe, rounds):
    """返回 (stdout, [wall...], [rss_kb...])；RSS 用 /usr/bin/time -v 采一次。"""
    walls, out = [], None
    for _ in range(rounds):
        t0 = time.perf_counter()
        r = subprocess.run([str(exe)], capture_output=True, text=True, env=env_moon(), cwd=REPO)
        walls.append(time.perf_counter() - t0)
        if r.returncode != 0:
            sys.exit(f"运行失败 {exe}: rc={r.returncode} {r.stderr[-400:]}")
        out = r.stdout
    # RSS：单独一次 /usr/bin/time -v
    rss = None
    if Path("/usr/bin/time").exists():
        tr = subprocess.run(["/usr/bin/time", "-v", str(exe)], capture_output=True, text=True,
                            env=env_moon(), cwd=REPO)
        import re
        m = re.search(r"Maximum resident set size \(kbytes\): (\d+)", tr.stderr)
        if m:
            rss = int(m.group(1)) / 1024  # MB
    return out, statistics.median(walls), rss


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--rounds", type=int, default=3)
    ap.add_argument("--quick", action="store_true")
    args = ap.parse_args()
    rounds = 1 if args.quick else args.rounds

    if BUILD.exists():
        shutil.rmtree(BUILD)
    BUILD.mkdir(parents=True)

    results = []
    for b in BENCHES:
        tag = f"{b['name']}_n{b['param']}"
        log(f"\n===== {b['name']} N={b['param']} =====")
        log(f"  {b['note']}")
        k_art, k_comp = build_kaula(b, tag)
        m_art, m_comp = build_moonbit(b, tag)
        log(f"  [build] kaula={k_comp:.2f}s/{k_art.stat().st_size}B  moonbit={m_comp:.2f}s/{m_art.stat().st_size}B")

        k_out, k_wall, k_rss = measure(k_art, rounds)
        m_out, m_wall, m_rss = measure(m_art, rounds)
        same = (k_out == m_out)
        log(f"  [run] kaula={k_wall:.4f}s/{k_rss}MB  moonbit={m_wall:.4f}s/{m_rss}MB")
        log(f"  [verify] 输出逐字节: {'PASS ✅' if same else 'FAIL ❌'}")
        if not same:
            sys.exit(f"输出不一致: {b['name']}")

        results.append({
            "bench": b, "tag": tag,
            "kaula": {"wall": k_wall, "rss": k_rss, "compile": k_comp, "size": k_art.stat().st_size},
            "moonbit": {"wall": m_wall, "rss": m_rss, "compile": m_comp, "size": m_art.stat().st_size},
        })

    write_report(results, rounds)
    log(f"\n[done] {REPORT}")


def write_report(results, rounds):
    L = ["# Kaula vs MoonBit：语言自然形态对比\n",
         "> 原则：**让算法在两种语言上各自用自然形态实现，不为对齐而统一数据结构**。\n",
         "> 正确性口径：native 输出逐字节一致（CLBG 标准格式），校验通过才计入计时。\n",
         "## 运行环境\n",
         "| 项 | 值 |\n|---|---|",
         f"| OS | {platform.system()} {platform.release()} ({platform.machine()}) |",
         "| CPU | " + (run(["bash", "-c", "grep -m1 'model name' /proc/cpuinfo | cut -d: -f2"]).stdout.strip() or "?") + f" / {os.cpu_count()} 核 |",
         "| Kaula | kaulac -release（内部 clang -O3）| ",
         "| MoonBit | " + (run([MOON, "version"]).stdout.strip().splitlines() or ["?"])[0] + "（LLVM native 后端）|",
         f"| 采样 | 每边 {rounds} 轮取中位数；RSS 由 /usr/bin/time -v 单次采样 |",
         ""]

    L += ["## 正确性校验（native 输出逐字节）\n",
          "| 题目 | 参数 | 形态与差异 | 结果 |\n|---|---:|---|---|"]
    for r in results:
        b = r["bench"]
        L.append(f"| {b['name']} | {b['param']} | {b['note']} | ✅ PASS |")
    L.append("")

    L += ["## 运行性能（native，中位数）\n",
          "| 题目 | Kaula wall | MoonBit wall | MB/K | Kaula RSS | MoonBit RSS |\n|---|---:|---:|---:|---:|---:|"]
    for r in results:
        k, m = r["kaula"], r["moonbit"]
        L.append(f"| {r['bench']['name']} | {k['wall']:.3f}s | {m['wall']:.3f}s | {m['wall']/k['wall']:.2f}× "
                 f"| {k['rss']:.0f}MB | {m['rss']:.0f}MB |")
    L.append("")

    L += ["## 编译速度与产物大小\n",
          "| 题目 | Kaula 编译 | MoonBit 编译 | Kaula 产物 | MoonBit 产物 |\n|---|---:|---:|---:|---:|"]
    for r in results:
        k, m = r["kaula"], r["moonbit"]
        L.append(f"| {r['bench']['name']} | {k['compile']:.2f}s | {m['compile']:.2f}s "
                 f"| {k['size']:,} B | {m['size']:,} B |")
    L.append("")

    L += ["## 解读\n",
          "- **binary-trees**：Kaula arena（连续数组、O(1) 复用）与 MoonBit GC 递归树（逐节点分配、GC 回收）"
          "是两种语言的自然形态，输出逐字节一致。性能差距反映的是**内存管理策略差异**，不是语法或代码生成能力差异。\n",
          "- **fannkuch-redux**：算法本身以定长数组为标准形态，两种语言均无特殊形态可用，"
          "这一项的对比才更接近纯粹的编译器代码生成质量对比。\n",
          "- **wasm 未纳入本报告**：Kaula `--wasm` 走 freestanding 裸机（无 stdout、无 libm、无 `__multi3`），"
          "MoonBit wasm 带 WASI（有 stdout）——两者输出能力不对等，强行对比只会得到误导性数字。"
          "需要 wasm 数据时请使用 `run_compare.py` 的 checksum kernel 口径（已标注限制）。\n"]

    REPORT.write_text("\n".join(L), encoding="utf-8")


if __name__ == "__main__":
    main()
