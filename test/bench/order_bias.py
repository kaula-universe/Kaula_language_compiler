#!/usr/bin/env python3
"""顺序偏差实验：验证「先跑的吃亏」这一系统性偏差是否存在。

方法对比：
  --mode interleave : A,B,A,B,... 交错（ab_interleave.py 的做法）
  --mode seq        : 先跑完 A 的 rounds 次，再跑 B 的 rounds 次（顺序）
  --mode seq_swap   : 先跑完 B，再跑完 A（顺序，互换）

若 interleave 下 A/B 无差异、而 seq 下出现显著方向性差异，
则证明「顺序」本身是偏差来源，而非代码差异。

这个实验用的是同一个可执行文件 —— 任何测出的差异都只能是偏差，
不可能是代码效果。这是「噪声/偏差标定」的黄金标准（A/A 对照）。
"""
from __future__ import annotations

import argparse
import json
import os
import re
import statistics
import subprocess
import sys
import time
from pathlib import Path

RESULT_RE = re.compile(r"(\w+)=(-?\d+)")


def run_once(exe: Path, curve: str, conc: int, total: int,
             timeout: float = 300.0) -> dict:
    cmd = [str(exe), curve, str(conc), str(total)]
    try:
        r = subprocess.run(cmd, capture_output=True, encoding="utf-8",
                           errors="replace", timeout=timeout,
                           cwd=str(exe.parent))
    except subprocess.TimeoutExpired:
        return {"error": f"超时 {timeout}s"}
    out = (r.stdout or "") + (r.stderr or "")
    if r.returncode != 0 or "RESULT" not in out:
        return {"error": f"rc={r.returncode}: {out.strip()[-200:]}"}
    line = [l for l in out.splitlines() if l.startswith("RESULT")][-1]
    kv = dict((k, int(v)) for k, v in RESULT_RE.findall(line))
    return {"tps": kv.get("tps", 0), "p50": kv.get("p50", 0),
            "p99": kv.get("p99", 0), "checksum": kv.get("checksum", 0),
            "errors": kv.get("errors", 0)}


def stats(xs):
    if not xs:
        return {}
    m = statistics.mean(xs)
    med = statistics.median(xs)
    sd = statistics.stdev(xs) if len(xs) > 1 else 0.0
    return {"n": len(xs), "mean": m, "median": med, "min": min(xs),
            "max": max(xs), "stdev": sd,
            "cv_pct": (sd / m * 100.0) if m else 0.0}


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--exe", required=True)
    ap.add_argument("--mode", default="interleave",
                    choices=["interleave", "seq", "seq_swap"])
    ap.add_argument("--curve", default="degrade")
    ap.add_argument("--conc", type=int, default=100)
    ap.add_argument("--total", type=int, default=20000)
    ap.add_argument("--rounds", type=int, default=21)
    ap.add_argument("--json", default="")
    a = ap.parse_args()

    exe = Path(a.exe).resolve()
    if not exe.exists():
        raise SystemExit(f"不存在: {exe}")

    print(f"模式={a.mode} exe={exe.name} curve={a.curve} conc={a.conc} "
          f"rounds={a.rounds}  (A 与 B 是同一文件)", flush=True)

    a_tps, b_tps = [], []
    warmup = 2  # 预热两轮丢弃，排除冷启动
    for i in range(a.rounds + warmup):
        if a.mode == "interleave":
            seq = [("A", exe), ("B", exe)] if i % 2 == 0 else [("B", exe), ("A", exe)]
        elif a.mode == "seq":
            seq = [("A", exe)] * 1 if i < a.rounds else [("B", exe)] * 1
        else:  # seq_swap
            seq = [("B", exe)] * 1 if i < a.rounds else [("A", exe)] * 1
        for tag, e in seq:
            rec = run_once(e, a.curve, a.conc, a.total)
            if "error" in rec:
                print(f"  {tag}: {rec['error']}", file=sys.stderr)
                continue
            if i < warmup:
                continue  # 丢弃预热
            (a_tps if tag == "A" else b_tps).append(rec["tps"])

    sa, sb = stats(a_tps), stats(b_tps)
    print()
    print("=" * 70)
    print(f"顺序偏差实验  模式={a.mode}  curve={a.curve} conc={a.conc}")
    print("=" * 70)
    print(f"{'侧':<6}{'mean':>12}{'median':>12}{'min':>12}{'max':>12}{'CV%':>8}")
    print("-" * 70)
    for tag, s in (("A", sa), ("B", sb)):
        if s:
            print(f"{tag:<6}{s['mean']:>12.0f}{s['median']:>12.0f}{s['min']:>12.0f}"
                  f"{s['max']:>12.0f}{s['cv_pct']:>8.1f}")
    if sa and sb:
        dm = (sb["mean"] - sa["mean"]) / sa["mean"] * 100
        dmed = (sb["median"] - sa["median"]) / sa["median"] * 100
        print()
        print(f"B/A 均值差: {dm:+.2f}%   B/A 中位差: {dmed:+.2f}%")
        if a.mode == "seq":
            print("→ 顺序模式（A 先 B 后）：若为负，说明后跑占优")
        elif a.mode == "seq_swap":
            print("→ 顺序模式（B 先 A 后）：若为正，说明后跑占优（与 seq 互证）")
        else:
            print("→ 交错模式：理想应接近 0")

    if a.json:
        Path(a.json).write_text(json.dumps(
            {"mode": a.mode, "curve": a.curve, "conc": a.conc,
             "a_tps": a_tps, "b_tps": b_tps,
             "a_stats": sa, "b_stats": sb}, indent=2), encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
