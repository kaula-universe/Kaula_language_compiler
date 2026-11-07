#!/usr/bin/env python3
"""ab_compare_base_vs_opt.py — 同窗口 A/B 对比：基线 runtime vs AB 优化 runtime。
同一配置 base/opt 交错跑，各 6 对有效（首对丢弃），取 tps 中位数对比。
本地运行不入库。"""
import re
import statistics
import subprocess
import sys

CONFIGS = [
    ("suspend", 1000), ("suspend", 10000),
    ("mixed", 1000), ("mixed", 10000),
]
PAIRS = 11  # 含首对预热，有效 6 对

RE = re.compile(r"RESULT curve=(\w+) tps=(\d+) elapsed_ns=(\d+) p50=(\d+) p95=(\d+) p99=(\d+) p999=(\d+) max=(\d+) n=(\d+) win=(\d+) errors=(\d+) checksum=(\d+)")


def run_one(exe: str) -> tuple:
    r = subprocess.run([exe], capture_output=True, text=True, timeout=600)
    if r.returncode != 0:
        return None, f"rc={r.returncode} {r.stderr[-120:] or r.stdout[-120:]}"
    m = RE.search(r.stdout)
    if not m:
        return None, "no RESULT line"
    return (int(m.group(2)), int(m.group(7)), int(m.group(11))), None  # tps, p999_ns, errors


for curve, conc in CONFIGS:
    base_exe = f"/tmp/bench_base_{curve}_{conc}"
    opt_exe = f"/tmp/bench_ab_{curve}_{conc}"
    base_tps, opt_tps, base_p999, opt_p999 = [], [], [], []
    errors = 0
    for i in range(PAIRS):
        b, eb = run_one(base_exe)
        o, eo = run_one(opt_exe)
        if eb or eo:
            print(f"{curve}/{conc} 对{i}: base_err={eb} opt_err={eo}")
            errors += 1
            continue
        if i == 0:
            continue  # 预热
        base_tps.append(b[0]); opt_tps.append(o[0])
        base_p999.append(b[1]); opt_p999.append(o[1])

    if not base_tps:
        print(f"{curve}/{conc}: 无有效采样")
        continue
    bm = statistics.median(base_tps); om = statistics.median(opt_tps)
    bp = statistics.median(base_p999); op = statistics.median(opt_p999)
    ratio = om / bm if bm else float("nan")
    bspread = (max(base_tps) / min(base_tps) - 1) * 100
    ospread = (max(opt_tps) / min(opt_tps) - 1) * 100
    print(f"{curve:8s}/{conc:<6} base={bm:>10,.0f} (spread {bspread:5.1f}%)  opt={om:>10,.0f} "
          f"(spread {ospread:5.1f}%)  倍率={ratio:.3f}  p999 base={bp/1000:.0f}us opt={op/1000:.0f}us  err={errors}")
