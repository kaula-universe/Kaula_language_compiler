#!/usr/bin/env python3
"""样本量规划 —— 回答「要检出 X% 的差异，需要跑多少轮」。

问题：本机 degrade/100 档位 CV≈7~9%，41 轮下可检测下限约 7%。
      小于 7% 的真实退化检测不出来（会落在「疑似」区间）。
      但很多有意义的退化就是 3~5% —— 必须有办法提高检测力。

原理：要检出的效应量（相对差 d）与所需样本量 n 的关系，用
      「可检测下限 ≈ z * CV / sqrt(n) * 常数」估计。
      本工具不假设这个公式精确，而是**用实测的 CV 反推**，
      再给一个保守的轮数建议。

用法：
    python test/bench/perf/plan_samples.py --curve degrade --conc 100 \\
        --target-detect 3.0
"""
from __future__ import annotations

import argparse
import json
import math
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import stats

BASELINE_DIR = Path(__file__).resolve().parent / "baselines"


def plan(cv_pct: float, target_pct: float, current_rounds: int = 0,
         current_floor: float = 0.0) -> dict:
    """给出达到 target_pct 检测力所需的轮数估计。

    经验关系（由本机实测标定）：
        可检测下限 ≈ k * CV / sqrt(n)
    其中 k 由当前标定反推：k = current_floor * sqrt(current_rounds) / CV。

    若没有当前标定，退化为经典的两组均值比较所需样本量近似：
        n ≈ 2 * (z_{α/2} + z_β)^2 * (CV/d)^2
    取 α=0.05, β=0.20（80% 检验力），z 和约 2.8。
    """
    if target_pct <= 0:
        return {"error": "目标检出幅度必须为正"}
    if cv_pct <= 0:
        return {"error": "CV 必须为正"}

    if current_rounds > 0 and current_floor > 0:
        k = current_floor * math.sqrt(current_rounds) / cv_pct
        # 目标：k * CV / sqrt(n) <= target_pct
        n = (k * cv_pct / target_pct) ** 2
        basis = f"由 {current_rounds} 轮标定反推 k={k:.3f}"
    else:
        n = 2 * (2.8 ** 2) * (cv_pct / target_pct) ** 2
        basis = "经典近似（α=0.05, 检验力 80%）"

    n = int(math.ceil(n))
    # 每组都要这么多，所以实际轮数 = n（A/B 各 n 次在同一轮完成）
    est_seconds = n * 2 * 0.6   # 每次采样约 0.6s（单次 degrade 运行量级）

    return {
        "target_pct": target_pct,
        "cv_pct": cv_pct,
        "rounds_needed": n,
        "basis": basis,
        "est_minutes": round(est_seconds / 60, 1),
    }


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--curve", default="degrade")
    ap.add_argument("--conc", type=int, default=100)
    ap.add_argument("--target-detect", type=float, default=3.0,
                    help="希望检出的相对差异（%%），越小越费时")
    ap.add_argument("--cv", type=float, default=None,
                    help="显式指定 CV%%；不给则读标定文件")
    ap.add_argument("--rounds-now", type=int, default=0)
    ap.add_argument("--floor-now", type=float, default=None)
    a = ap.parse_args()

    cv = a.cv
    cur_rounds = a.rounds_now
    cur_floor = a.floor_now
    src = "命令行"
    if cv is None:
        f = BASELINE_DIR / f"{a.curve}_{a.conc}.json"
        if not f.exists():
            raise SystemExit(
                f"没有 {f}，请先跑 calibrate.py，或用 --cv 显式指定")
        d = json.loads(f.read_text(encoding="utf-8"))
        cv = max(d["noise"]["a_cv_pct"], d["noise"]["b_cv_pct"])
        cur_rounds = d.get("rounds", 0)
        cur_floor = d.get("detectable_min_pct", 0.0)
        src = f.name

    p = plan(cv, a.target_detect, cur_rounds, cur_floor or 0.0)
    if "error" in p:
        print(f"规划失败：{p['error']}", file=sys.stderr)
        return 1

    print(f"样本量规划  curve={a.curve} conc={a.conc}（CV 来源：{src}）")
    print("=" * 60)
    print(f"  当前 CV          : {p['cv_pct']:.2f}%")
    if cur_rounds:
        print(f"  当前标定         : {cur_rounds} 轮 → 可检测下限 {cur_floor:.2f}%")
    print(f"  目标检出幅度     : {p['target_pct']:.2f}%")
    print(f"  推算法            : {p['basis']}")
    print("-" * 60)
    print(f"  ★ 建议轮数       : {p['rounds_needed']}")
    print(f"    预估耗时       : 约 {p['est_minutes']} 分钟")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
