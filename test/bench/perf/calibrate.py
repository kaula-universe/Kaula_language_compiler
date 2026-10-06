#!/usr/bin/env python3
"""A/A 标定 —— 产出「本档位能检测的最小真实差异」。

为什么必须先做这一步：
    在没有噪声带的机器上比较两组数字，等于在不知道量程的情况下读数。
    commit 0419304 声称的 +140% 收益，就是因为少了这一步而把噪声当信号。

做法：拿**同一个二进制**跑完整的 A/B 流程。任何测出的差异都只能是偏差。
把该档位的偏差特征落盘成「基准文件」，后续 compare.py 用它做判定阈值。

用法：
    # 标定 degrade/100 档位，41 轮
    python test/bench/perf/calibrate.py \\
        --exe test/bench/cbench/bench_c --curve degrade --conc 100 --rounds 41

    # 标定完整矩阵
    python test/bench/perf/calibrate.py --exe ... --sweep
"""
from __future__ import annotations

import argparse
import json
import os
import platform
import sys
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from harness import RunConfig, interleaved_run, tps_of, checksum_consistent
import stats

BASELINE_DIR = Path(__file__).resolve().parent / "baselines"


def machine_tag() -> str:
    try:
        ncpu = os.cpu_count() or 0
    except Exception:
        ncpu = 0
    return f"{platform.machine()} / {ncpu} core / {platform.system()}"


def calibrate(exe: Path, curve: str, conc: int, total: int, rounds: int,
              warmup: int, timeout: float, verbose: bool = True) -> dict:
    cfg_a = RunConfig(exe=exe, curve=curve, conc=conc, total=total,
                      tag="A", timeout=timeout)
    cfg_b = RunConfig(exe=exe, curve=curve, conc=conc, total=total,
                      tag="B", timeout=timeout)

    def progress(i, n, sa, sb):
        if verbose and sa and sb:
            print(f"  [{i:>2}/{n}] A={sa[-1].tps}  B={sb[-1].tps}", flush=True)

    t0 = time.perf_counter()
    res = interleaved_run(cfg_a, cfg_b, rounds, warmup, strict=True,
                          on_round=progress)
    dur = time.perf_counter() - t0

    a, b = tps_of(res["samples_a"]), tps_of(res["samples_b"])
    if len(a) < 4 or len(b) < 4:
        return {"error": "有效样本不足", "errors": res["errors"],
                "curve": curve, "conc": conc}

    rel = stats.rel_ci(a, b)          # A/A 下的「假差异」
    ra, rb = stats.robust_stats(a), stats.robust_stats(b)
    mw = stats.mann_whitney_u(a, b)
    delta = stats.cliffs_delta(a, b)

    ok_a, ck_a = checksum_consistent(res["samples_a"])
    ok_b, ck_b = checksum_consistent(res["samples_b"])

    # 该档位「能检测的最小真实差异」：
    #   直接观察 A/A 假差异的分布 —— 这是同一二进制在同一时段跑出的
    #   相对差，其分布宽度就是「噪声能把两个相同的东西拉多开」。
    #
    # 为什么不用 CV：CV 是单侧样本的离散度，它把「机器慢了一点」这类
    #   整体漂移也算进噪声，导致阈值虚高（实测 15 轮标定给出 6.72%，
    #   而真实 9% 的退化检出后只落在「疑似」）。而 A/A 假差异分布
    #   已经通过交错采样把整体漂移对称化掉了，更贴近判定所需的量。
    #
    # 取 2 倍 |假差异点估计| 作为下限：单次 A/A 观测到的最大假差异
    #   本身就带随机性，乘 2 给一定裕度，同时不至于像 CV 那样过度保守。
    aa_point = abs(rel.get("point_pct", 0.0))
    ci_bound = max(abs(rel.get("lo_pct", 0)), abs(rel.get("hi_pct", 0)))
    # 下限 = max(假差异上界, 2 倍假差异点估计)，并设一个 1% 的物理地板
    detectable = max(ci_bound, aa_point * 2.0, 1.0)
    cv_a = ra["stdev"] / ra["mean"] * 100 if ra["mean"] else 0
    cv_b = rb["stdev"] / rb["mean"] * 100 if rb["mean"] else 0

    return {
        "curve": curve,
        "conc": conc,
        "total": total,
        "rounds": len(a),
        "warmup": warmup,
        "noise": {
            "a_cv_pct": round(cv_a, 2),
            "b_cv_pct": round(cv_b, 2),
            "median_a": ra["median"],
            "median_b": rb["median"],
            "aa_point_pct": round(rel.get("point_pct", 0), 2),
            "aa_ci_pct": [round(rel.get("lo_pct", 0), 2),
                          round(rel.get("hi_pct", 0), 2)],
            "aa_p_value": round(mw["p_value"], 4),
            "aa_effect": round(delta, 4),
        },
        "detectable_min_pct": round(detectable, 2),
        "correctness": {
            "a_checksum_consistent": ok_a, "a_checksum": ck_a,
            "b_checksum_consistent": ok_b, "b_checksum": ck_b,
            "errors": res["errors"],
        },
        "machine": machine_tag(),
        "measured_at": time.strftime("%Y-%m-%dT%H:%M:%S%z"),
        "duration_s": round(dur, 1),
    }


def render(rep: dict) -> str:
    if "error" in rep:
        return f"标定失败：{rep['error']} {rep.get('errors', '')}"
    n = rep["noise"]
    out = [
        f"A/A 标定结果  curve={rep['curve']} conc={rep['conc']} "
        f"({rep['rounds']} 轮, {rep['duration_s']}s)",
        "=" * 66,
        f"  A/A 假差异（同二进制）  : {n['aa_point_pct']:+.2f}% "
        f"95%CI [{n['aa_ci_pct'][0]:+.2f}%, {n['aa_ci_pct'][1]:+.2f}%]",
        f"  A/A 检验                : p={n['aa_p_value']}  "
        f"效应量={n['aa_effect']:+.3f}",
        f"  噪声（CV）              : A={n['a_cv_pct']}%  B={n['b_cv_pct']}%",
        f"  中位 TPS                : A={n['median_a']:.0f}  B={n['median_b']:.0f}",
        "-" * 66,
        f"  ★ 该档位可检测下限      : {rep['detectable_min_pct']:.2f}%",
        "    小于此幅度的 A/B 差异，本方法一律判为噪声",
    ]
    c = rep["correctness"]
    out.append(f"  checksum 一致性         : A={c['a_checksum_consistent']} "
               f"B={c['b_checksum_consistent']}")
    if c["errors"]:
        out.append(f"  异常 {len(c['errors'])} 条")
    return "\n".join(out)


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--exe", required=True, help="被测可执行文件")
    ap.add_argument("--curve", default="degrade")
    ap.add_argument("--conc", type=int, default=100)
    ap.add_argument("--total", type=int, default=20000)
    ap.add_argument("--rounds", type=int, default=41)
    ap.add_argument("--warmup", type=int, default=2)
    ap.add_argument("--timeout", type=float, default=300.0)
    ap.add_argument("--json", default="")
    ap.add_argument("--sweep", action="store_true")
    ap.add_argument("--conc-list", default="100,1000,10000")
    args = ap.parse_args()

    exe = Path(args.exe).resolve()
    if not exe.exists():
        raise SystemExit(f"不存在: {exe}")

    jobs = []
    if args.sweep:
        for curve in ("light", "suspend", "mixed", "degrade"):
            for conc in [int(x) for x in args.conc_list.split(",") if x]:
                jobs.append((curve, conc))
    else:
        jobs.append((args.curve, args.conc))

    BASELINE_DIR.mkdir(parents=True, exist_ok=True)
    summary = []
    for curve, conc in jobs:
        print(f"\n>>> 标定 {curve}/conc={conc}  ({args.rounds} 轮)", flush=True)
        rep = calibrate(exe, curve, conc, args.total, args.rounds,
                        args.warmup, args.timeout)
        print(render(rep), flush=True)
        out = Path(args.json) if (args.json and len(jobs) == 1) \
            else BASELINE_DIR / f"{curve}_{conc}.json"
        out.write_text(json.dumps(rep, indent=2, ensure_ascii=False),
                       encoding="utf-8")
        print(f"  -> {out}", flush=True)
        summary.append(rep)

    if len(summary) > 1:
        print("\n" + "=" * 66)
        print("标定汇总：可检测下限")
        print("=" * 66)
        print(f"{'curve':<10}{'conc':>8}{'可检测下限':>14}{'CV(A)':>10}")
        print("-" * 42)
        for r in summary:
            if "error" in r:
                print(f"{r.get('curve','?'):<10}{r.get('conc',0):>8}{'失败':>14}")
                continue
            print(f"{r['curve']:<10}{r['conc']:>8}"
                  f"{r['detectable_min_pct']:>13.2f}%"
                  f"{r['noise']['a_cv_pct']:>9.1f}%")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
