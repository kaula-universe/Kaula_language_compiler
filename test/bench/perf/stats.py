#!/usr/bin/env python3
"""统计原语 —— 把「信号」从「噪声」里分出来。

设计约束：
  - 只用标准库。跑基准的机器不一定有 numpy/scipy，工具不能因此跑不起来。
  - 不假设正态分布。基准采样的分布是明显右偏的（偶发的调度抖动产生长尾），
    任何依赖正态假设的检验（t 检验、mean±1.96σ）在这里都是错的。
  - 每个函数都是纯函数，可单测。

自测：
  python test/bench/perf/stats.py --selftest
"""
from __future__ import annotations

import argparse
import math
import random
import statistics
import sys
from bisect import bisect_left, bisect_right
from typing import Sequence


# ========== 描述性统计（抗离群） ==========

def robust_stats(xs: Sequence[float]) -> dict:
    """中位数 + 四分位 + MAD。比均值/标准差更适合被离群污染的样本。

    MAD（median absolute deviation）对离群值的敏感度远低于标准差：
    样本里混进一个 2 倍大的值，标准差会被显著拉大，MAD 几乎不动。
    """
    if not xs:
        return {}
    s = sorted(xs)
    n = len(s)
    med = statistics.median(s)

    def quantile(q: float) -> float:
        if n == 1:
            return s[0]
        pos = q * (n - 1)
        lo = int(pos)
        hi = min(lo + 1, n - 1)
        frac = pos - lo
        return s[lo] * (1 - frac) + s[hi] * frac

    q1, q3 = quantile(0.25), quantile(0.75)
    mad = statistics.median([abs(x - med) for x in s])
    return {
        "n": n, "median": med, "q1": q1, "q3": q3, "iqr": q3 - q1,
        "mad": mad,
        # MAD 转标准差的一致估计量（正态下 1.4826*MAD == σ）
        "mad_sigma": mad * 1.4826,
        "mean": statistics.mean(s),
        "stdev": statistics.stdev(s) if n > 1 else 0.0,
        "min": s[0], "max": s[-1],
    }


# ========== 置信区间（bootstrap） ==========

def bootstrap_ci(xs: Sequence[float], stat=statistics.mean, n_boot: int = 10000,
                 alpha: float = 0.05, seed: int = 12345) -> tuple[float, float]:
    """bootstrap 重采样给出统计量的置信区间。

    为什么不用正态近似的 mean ± 1.96σ/√n：
      基准样本量小（21~41）、分布右偏、且有序列相关（相邻采样不独立，
      因为机器状态是连续的）。bootstrap 不依赖这些假设。

    seed 固定：同样的输入必须给出同样的区间，否则报告不可复现。
    """
    if len(xs) < 2:
        v = xs[0] if xs else 0.0
        return (v, v)
    rng = random.Random(seed)
    n = len(xs)
    boots = []
    for _ in range(n_boot):
        sample = [xs[rng.randrange(n)] for _ in range(n)]
        boots.append(stat(sample))
    boots.sort()
    lo = boots[int(n_boot * (alpha / 2))]
    hi = boots[int(n_boot * (1 - alpha / 2)) - 1]
    return (lo, hi)


def rel_ci(a: Sequence[float], b: Sequence[float], n_boot: int = 10000,
           alpha: float = 0.05, seed: int = 12345) -> dict:
    """「b 相对 a」的相对差 95% 置信区间（百分比）。

    这是给人看的核心数字：+0.55% [-4.1%, +5.2%] 一眼就知道不显著。
    """
    if not a or not b:
        return {}
    rng = random.Random(seed)
    na, nb = len(a), len(b)
    med_a, med_b = statistics.median(a), statistics.median(b)
    point = (med_b - med_a) / med_a * 100.0 if med_a else 0.0

    diffs = []
    for _ in range(n_boot):
        sa = [a[rng.randrange(na)] for _ in range(na)]
        sb = [b[rng.randrange(nb)] for _ in range(nb)]
        ma, mb = statistics.median(sa), statistics.median(sb)
        if ma:
            diffs.append((mb - ma) / ma * 100.0)
    if not diffs:
        return {"point_pct": point, "lo_pct": point, "hi_pct": point,
                "crosses_zero": True}
    diffs.sort()
    lo = diffs[int(len(diffs) * (alpha / 2))]
    hi = diffs[int(len(diffs) * (1 - alpha / 2)) - 1]
    return {
        "point_pct": point,
        "lo_pct": lo,
        "hi_pct": hi,
        "crosses_zero": (lo <= 0.0 <= hi),
    }


# ========== 显著性检验（Mann-Whitney U） ==========

def mann_whitney_u(a: Sequence[float], b: Sequence[float]) -> dict:
    """非参数秩和检验（双侧）。返回 U 统计量、正态近似 z、p 值。

    为什么不用 t 检验：分布右偏 + 小样本 + 方差不齐，t 检验的前提全不满足。
    Mann-Whitney 只依赖「秩」，对离群值和分布形状都稳健。

    正态近似（含连续性校正与结修正）在 n>=8 时已足够准；
    本工具的样本量（21~41）远大于此。
    """
    n1, n2 = len(a), len(b)
    if n1 == 0 or n2 == 0:
        return {"p_value": 1.0, "u": 0.0, "z": 0.0}
    combined = [(v, 0) for v in a] + [(v, 1) for v in b]
    combined.sort(key=lambda t: t[0])

    ranks = [0.0] * len(combined)
    i = 0
    tie_correction = 0.0
    while i < len(combined):
        j = i
        while j + 1 < len(combined) and combined[j + 1][0] == combined[i][0]:
            j += 1
        avg_rank = (i + j) / 2.0 + 1.0
        for k in range(i, j + 1):
            ranks[k] = avg_rank
        t = j - i + 1
        if t > 1:
            tie_correction += t ** 3 - t
        i = j + 1

    r1 = sum(ranks[k] for k in range(len(combined)) if combined[k][1] == 0)
    u1 = r1 - n1 * (n1 + 1) / 2.0
    u2 = n1 * n2 - u1
    u = min(u1, u2)

    mu = n1 * n2 / 2.0
    n = n1 + n2
    denom = n * (n - 1)
    sigma_sq = (n1 * n2 / 12.0) * ((n + 1) - (tie_correction / denom if denom else 0))
    if sigma_sq <= 0:
        return {"u": u, "z": 0.0, "p_value": 1.0}
    sigma = sigma_sq ** 0.5
    z = (abs(u - mu) - 0.5) / sigma   # 连续性校正
    if z < 0:
        z = 0.0
    p = 2.0 * (1.0 - _norm_cdf(z))
    return {"u": u, "z": z, "p_value": min(1.0, p)}


def _norm_cdf(z: float) -> float:
    """标准正态 CDF（用 math.erf，精度接近机器精度）。"""
    return 0.5 * (1.0 + math.erf(z / math.sqrt(2.0)))


# ========== 效应量（Cliff's delta） ==========

def cliffs_delta(a: Sequence[float], b: Sequence[float]) -> float:
    """Cliff's delta 效应量，范围 [-1, 1]，0 表示无差异。

    解释（Romano et al. 2006 的常用阈值）：
      |d| < 0.147  negligible（可忽略）
      |d| < 0.33   small
      |d| < 0.474  medium
      else         large

    为什么需要它：p 值只回答「差异是否可能存在」，不回答「差异多大」。
    样本够大时，0.1% 的差异也能 p<0.001 —— 但那没有工程意义。
    效应量给出幅度，两个一起看才不误导。

    定义：delta = P(a > b) - P(a < b)。
      a 整体大于 b  → +1
      a 整体小于 b  → -1
      两组混同      →  0
    """
    n1, n2 = len(a), len(b)
    if n1 == 0 or n2 == 0:
        return 0.0
    sa, sb = sorted(a), sorted(b)
    # 必须分别统计 a>b 与 a<b 的成对数，再按「相等对半分」合成。
    #
    # 不能用 2*P(a>b)-1：后者在「两组大量相等」时会把相等项错误归入一侧。
    # 而 A/A 对照（同一二进制 vs 自身）恰恰会产生大量相等样本 ——
    # 用错公式会让 A/A 恒定为 ±1（看似"稳定变化"），是本工具最危险的失效模式。
    #
    # 方向：bisect_left(sb, x) 给出 sb 中严格小于 x 的个数，
    # 那批元素满足 b_j < x，即 a_i > b_j —— 属于 gt。
    gt = 0   # a > b 的成对数
    lt = 0   # a < b 的成对数
    for x in sa:
        gt += bisect_left(sb, x)
        lt += n2 - bisect_right(sb, x)
    total = n1 * n2
    eq = total - gt - lt
    return ((gt + eq * 0.5) - (lt + eq * 0.5)) / total


def effect_label(d: float) -> str:
    ad = abs(d)
    if ad < 0.147:
        return "可忽略"
    if ad < 0.33:
        return "小"
    if ad < 0.474:
        return "中"
    return "大"


# ========== 自测 ==========

def _selftest() -> int:
    fails = []

    def check(name: str, cond: bool, detail: str = ""):
        if cond:
            print(f"  [PASS] {name}")
        else:
            print(f"  [FAIL] {name} {detail}")
            fails.append(name)

    print("stats.py 自测")
    rng = random.Random(7)

    # 1. 同一分布的两组：不应显著
    a = [rng.gauss(1000, 100) for _ in range(30)]
    b = [rng.gauss(1000, 100) for _ in range(30)]
    mw = mann_whitney_u(a, b)
    d = cliffs_delta(a, b)
    check("同分布 p>0.05", mw["p_value"] > 0.05, f"p={mw['p_value']:.4f}")
    check("同分布 |delta|<0.147", abs(d) < 0.147, f"d={d:.3f}")

    # 2. 明显不同的两组：应显著
    c = [rng.gauss(1300, 100) for _ in range(30)]
    mw2 = mann_whitney_u(a, c)
    d2 = cliffs_delta(a, c)
    check("异分布 p<0.01", mw2["p_value"] < 0.01, f"p={mw2['p_value']:.6f}")
    check("异分布 |delta|>0.474", abs(d2) > 0.474, f"d={d2:.3f}")

    # 3. rel_ci 跨 0 判定
    r = rel_ci(a, b)
    check("同分布 CI 跨 0", r["crosses_zero"], f"{r}")
    r2 = rel_ci(a, c)
    check("异分布 CI 不跨 0", not r2["crosses_zero"], f"{r2}")

    # 4. 离群值稳健性
    base = [1000.0] * 20
    with_outlier = base + [2000.0]
    rs0, rs1 = robust_stats(base), robust_stats(with_outlier)
    check("MAD 抗离群", abs(rs1["mad"] - rs0["mad"]) < 1.0,
          f"{rs0['mad']} -> {rs1['mad']}")
    check("stdev 不抗离群", rs1["stdev"] > rs0["stdev"])

    # 5. bootstrap 可复现
    check("bootstrap 同输入同输出", bootstrap_ci(a) == bootstrap_ci(a))

    # 6. 空输入不崩
    check("空输入 robust_stats", robust_stats([]) == {})
    check("空输入 mann_whitney", mann_whitney_u([], [])["p_value"] == 1.0)
    check("空输入 cliffs_delta", cliffs_delta([], []) == 0.0)

    # 7. 效应量符号与边界
    #    这几个用例曾经各抓到一个真 bug（变量方向写反、结未处理），勿删。
    check("delta a>b 为 +1", cliffs_delta([1300] * 20, [1000] * 20) == 1.0,
          f"{cliffs_delta([1300] * 20, [1000] * 20)}")
    check("delta a<b 为 -1", cliffs_delta([1000] * 20, [1300] * 20) == -1.0,
          f"{cliffs_delta([1000] * 20, [1300] * 20)}")
    check("delta 完全相等为 0", cliffs_delta([1000] * 20, [1000] * 20) == 0.0,
          f"{cliffs_delta([1000] * 20, [1000] * 20)}")
    check("delta 半等半大为 +0.5",
          abs(cliffs_delta([1000] * 10 + [1300] * 10, [1000] * 20) - 0.5) < 1e-9)
    check("delta 半等半小为 -0.5",
          abs(cliffs_delta([1000] * 10 + [700] * 10, [1000] * 20) + 0.5) < 1e-9)
    # A/A 的典型形态：两组几乎相同但有抖动，效应量必须接近 0
    jitter_a = [1000 + rng.randint(-20, 20) for _ in range(30)]
    jitter_b = [1000 + rng.randint(-20, 20) for _ in range(30)]
    dj = cliffs_delta(jitter_a, jitter_b)
    check("A/A 抖动样本 |delta|<0.33", abs(dj) < 0.33, f"d={dj:.3f}")

    print()
    if fails:
        print(f"失败 {len(fails)} 项：{fails}")
        return 1
    print("全部通过")
    return 0


if __name__ == "__main__":
    ap = argparse.ArgumentParser()
    ap.add_argument("--selftest", action="store_true")
    args = ap.parse_args()
    if args.selftest:
        sys.exit(_selftest())
    ap.print_help()
