#!/usr/bin/env python3
"""A/B 对照判定 —— 给出唯一结论，不让人肉解释数字。

这是「不允许任何性能退化」这条要求的执行工具：它把
    「差异多大」+「这差异是真的吗」+「相对本机噪声带算不算退化」
合成为一个机器判定，避免再出现「在噪声里下结论」。

判定规则（四类，互斥且穷尽）：
  1. 数据未收敛（checksum 跨轮不一致）      → 无效
  2. 统计上不可区分                          → 无差异
       条件：95% CI 跨 0  或  |效应量| < 0.147
  3. 统计上可区分，但幅度在噪声带内          → 疑似（需更多样本/更高精度）
       条件：CI 不跨 0 且 |效应量| >= 0.147，但 |中位差| < 可检测下限
       —— 此时差异是真的，只是小于本档位标称的检测精度。
          不能报「无差异」（那是否认已观测到的证据），
          也不能直接报「退化」（幅度未超噪声带，可能是小样本偶然），
          必须如实标为「疑似」并要求加大样本量复测。
  4. 统计上可区分且幅度超出噪声带            → 改善 / 退化（按符号）

为什么第 2 与第 3 必须分开：
    早期版本把「CI 跨 0」「效应量小」「幅度小于阈值」用「或」连起来，
    结果一个 p=0.0002、CI=[-6.8%,-1.7%] 的真实退化被报成「无差异」——
    这是最危险的漏报方向（把退化藏起来）。
    见 test/bench/perf/README.md 的「灵敏度验证」一节。

用法：
    # 先标定（得到噪声带）
    python test/bench/perf/calibrate.py --exe bench_base --curve degrade --conc 100

    # 再对照（自动读噪声带）
    python test/bench/perf/compare.py --a bench_base --b bench_opt \\
        --curve degrade --conc 100 --rounds 41
"""
from __future__ import annotations

import argparse
import json
import statistics
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from harness import RunConfig, interleaved_run, tps_of, checksum_consistent
import stats

BASELINE_DIR = Path(__file__).resolve().parent / "baselines"

# 效应量的「可忽略」阈值，来自 Romano et al. 2006
DELTA_NEGLIGIBLE = 0.147


def load_threshold(curve: str, conc: int, explicit: float | None) -> tuple[float, str]:
    """读取该档位的标定可检测下限。优先命令行显式值，其次基准文件。"""
    if explicit is not None:
        return explicit, "命令行指定"
    f = BASELINE_DIR / f"{curve}_{conc}.json"
    if f.exists():
        try:
            d = json.loads(f.read_text(encoding="utf-8"))
            return float(d["detectable_min_pct"]), f.name
        except Exception:
            pass
    # 没有基准：给一个保守的兜底，并明确警告
    return 15.0, "兜底值（未标定，不可信）"


def dynamic_threshold(a: list[float], b: list[float],
                      baseline_floor: float, n_boot: int = 2000,
                      seed: int = 999) -> tuple[float, str]:
    """用**本次实验自身**的噪声估计可检测下限，与标定基准取较大者。

    为什么不能只用标定文件：
        实测同一档位在不同时段的 CV 会从 6% 漂到 13%（机器负载在变）。
        用一个陈旧标定的固定阈值，要么过松（漏报退化），要么过严（误报）。

    做法：把 A、B 合并后随机对半分，反复计算「假差异」的分布，
    取其 95 分位的绝对值作为本次数据的噪声上界。
    这是**数据驱动的 A/A 模拟**，不需要额外采样。
    """
    import random
    if len(a) < 6 or len(b) < 6:
        return baseline_floor, "标定基准（样本太少无法动态估计）"
    rng = random.Random(seed)
    pool = list(a) + list(b)
    n = len(pool)
    half = n // 2
    diffs = []
    for _ in range(n_boot):
        rng.shuffle(pool)
        pa, pb = pool[:half], pool[half:half * 2]
        ma = statistics.median(pa)
        mb = statistics.median(pb)
        if ma:
            diffs.append(abs((mb - ma) / ma * 100.0))
    if not diffs:
        return baseline_floor, "标定基准"
    diffs.sort()
    p95 = diffs[int(len(diffs) * 0.95) - 1] if len(diffs) > 1 else diffs[0]
    # 与标定基准取大：标定代表机器固有噪声，动态估计代表本次实验噪声
    thr = max(p95, baseline_floor)
    which = "动态估计" if p95 >= baseline_floor else "标定基准"
    return thr, f"{which}（动态上界 {p95:.2f}%，标定 {baseline_floor:.2f}%）"


def verdict(a: list[float], b: list[float], threshold: float) -> dict:
    """核心判定。返回结构化结论。

    四类判定见模块 docstring。关键是把「统计上不可区分」与
    「可区分但幅度小」分开 —— 后者必须报「疑似」而不是「无差异」。
    """
    rel = stats.rel_ci(a, b)
    mw = stats.mann_whitney_u(a, b)
    delta = stats.cliffs_delta(a, b)
    ra, rb = stats.robust_stats(a), stats.robust_stats(b)

    med_rel = (rb["median"] - ra["median"]) / ra["median"] * 100 if ra["median"] else 0.0

    crosses_zero = rel.get("crosses_zero", True)
    small_effect = abs(delta) < DELTA_NEGLIGIBLE
    within_band = abs(med_rel) < threshold

    # 第 2 类：统计上不可区分
    if crosses_zero or small_effect:
        label = "无差异"
        reasons = []
        if crosses_zero:
            reasons.append(f"95% CI [{rel['lo_pct']:+.2f}%, {rel['hi_pct']:+.2f}%] 跨 0")
        if small_effect:
            reasons.append(f"|效应量|={abs(delta):.3f} < {DELTA_NEGLIGIBLE}")
        if within_band:
            reasons.append(f"|中位差|={abs(med_rel):.2f}% < 可检测下限 {threshold:.2f}%")
    # 第 3 类：统计上可区分，但幅度在噪声带内
    elif within_band:
        label = "疑似"
        reasons = [
            f"检验显著（p={mw['p_value']:.4f}，95% CI "
            f"[{rel['lo_pct']:+.2f}%, {rel['hi_pct']:+.2f}%] 不跨 0，"
            f"效应量={delta:+.3f}）",
            f"但幅度 {med_rel:+.2f}% 未超出可检测下限 {threshold:.2f}%",
            "→ 方向可信、幅度未定型，请加大 --rounds 复测后再定性",
        ]
    # 第 4 类：可区分且超出噪声带
    else:
        label = "改善" if med_rel > 0 else "退化"
        reasons = [
            f"95% CI [{rel['lo_pct']:+.2f}%, {rel['hi_pct']:+.2f}%] 不跨 0",
            f"效应量={delta:+.3f}（{stats.effect_label(delta)}）",
            f"幅度 {med_rel:+.2f}% 超出可检测下限 {threshold:.2f}%",
        ]

    return {
        "label": label,
        "reasons": reasons,
        "median_rel_pct": round(med_rel, 2),
        "ci_pct": [round(rel.get("lo_pct", 0), 2),
                   round(rel.get("hi_pct", 0), 2)],
        "p_value": round(mw["p_value"], 4),
        "effect": round(delta, 4),
        "effect_label": stats.effect_label(delta),
        "threshold_pct": round(threshold, 2),
        "crosses_zero": crosses_zero,
        "median_a": ra["median"], "median_b": rb["median"],
        "cv_a_pct": round(ra["stdev"] / ra["mean"] * 100, 2) if ra["mean"] else 0,
        "cv_b_pct": round(rb["stdev"] / rb["mean"] * 100, 2) if rb["mean"] else 0,
    }


def render(v: dict, curve: str, conc: int, rounds: int, src: str) -> str:
    bar = "=" * 68
    mark = {"无差异": "○", "改善": "↑", "退化": "↓", "疑似": "?"}[v["label"]]
    out = [
        bar,
        f"A/B 判定  curve={curve} conc={conc} ({rounds} 轮)",
        bar,
        f"  中位 TPS    : A={v['median_a']:.0f}  B={v['median_b']:.0f}",
        f"  相对差      : {v['median_rel_pct']:+.2f}%  "
        f"95%CI [{v['ci_pct'][0]:+.2f}%, {v['ci_pct'][1]:+.2f}%]",
        f"  检验        : p={v['p_value']}  效应量={v['effect']:+.3f}"
        f"（{v['effect_label']}）",
        f"  噪声(CV)    : A={v['cv_a_pct']}%  B={v['cv_b_pct']}%",
        f"  判定阈值    : {v['threshold_pct']:.2f}%（来源：{src}）",
        "-" * 68,
        f"  {mark} 判定：{v['label']}",
    ]
    for r in v["reasons"]:
        out.append(f"      理由：{r}")
    out.append(bar)
    return "\n".join(out)


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--a", required=True, help="基线可执行文件")
    ap.add_argument("--b", default="", help="待验证可执行文件；--aa 时可省略")
    ap.add_argument("--env-b", default="", help="B 侧环境变量 K=V,K=V")
    ap.add_argument("--curve", default="degrade")
    ap.add_argument("--conc", type=int, default=100)
    ap.add_argument("--total", type=int, default=20000)
    ap.add_argument("--rounds", type=int, default=41)
    ap.add_argument("--warmup", type=int, default=2)
    ap.add_argument("--timeout", type=float, default=300.0)
    ap.add_argument("--threshold", type=float, default=None,
                    help="显式指定可检测下限（%%）；不给则读标定基准")
    ap.add_argument("--aa", action="store_true",
                    help="A/A 模式：忽略 --b，用 --a 自身做对照（自检）")
    ap.add_argument("--json", default="")
    args = ap.parse_args()

    exe_a = Path(args.a).resolve()
    if args.aa:
        exe_b = exe_a
    elif not args.b:
        raise SystemExit("A/B 对照需要 --b（或用 --aa 做自检）")
    else:
        exe_b = Path(args.b).resolve()
    if not exe_a.exists():
        raise SystemExit(f"A 不存在: {exe_a}")
    if not exe_b.exists():
        raise SystemExit(f"B 不存在: {exe_b}")

    env_b = {}
    if args.env_b:
        for pair in args.env_b.split(","):
            if "=" in pair:
                k, val = pair.split("=", 1)
                env_b[k.strip()] = val.strip()

    cfg_a = RunConfig(exe=exe_a, curve=args.curve, conc=args.conc,
                      total=args.total, tag="A", timeout=args.timeout)
    cfg_b = RunConfig(exe=exe_b, curve=args.curve, conc=args.conc,
                      total=args.total, tag="B", env=env_b, timeout=args.timeout)

    mode = "A/A 自检" if args.aa else "A/B 对照"
    print(f"{mode}  curve={args.curve} conc={args.conc} rounds={args.rounds}",
          flush=True)

    def progress(i, n, sa, sb):
        if sa and sb:
            print(f"  [{i:>2}/{n}] A={sa[-1].tps}  B={sb[-1].tps}", flush=True)

    res = interleaved_run(cfg_a, cfg_b, args.rounds, args.warmup,
                          strict=True, on_round=progress)
    a, b = tps_of(res["samples_a"]), tps_of(res["samples_b"])

    if len(a) < 4 or len(b) < 4:
        print("\n有效样本不足，无法判定", file=sys.stderr)
        for e in res["errors"][:10]:
            print(f"  {e}", file=sys.stderr)
        return 2

    # 阈值：标定基准与本次实验动态噪声取较大者，避免用陈旧标定误判
    base_thr, src = load_threshold(args.curve, args.conc, args.threshold)
    thr, src = dynamic_threshold(a, b, base_thr)
    if "兜底" in src:
        print(f"警告：{args.curve}/{args.conc} 未标定，阈值用兜底 {thr:.2f}%，"
              f"结论不可信。请先跑 calibrate.py。", file=sys.stderr)

    v = verdict(a, b, thr)
    print()
    print(render(v, args.curve, args.conc, len(a), src))

    # 正确性报告（与性能结论并列，不可省略）
    ok_a, ck_a = checksum_consistent(res["samples_a"])
    ok_b, ck_b = checksum_consistent(res["samples_b"])
    print(f"  checksum 一致性: A={'✓' if ok_a else '✗'} "
          f"B={'✓' if ok_b else '✗'}")
    if res["errors"]:
        print(f"  采样异常 {len(res['errors'])} 条")
    if not (ok_a and ok_b):
        print("  ！checksum 不一致 —— 数据未收敛，性能结论作废")
        if v["label"] != "无差异":
            v["label"] = "无效（数据未收敛）"

    if args.json:
        Path(args.json).write_text(json.dumps(
            {"curve": args.curve, "conc": args.conc, "rounds": len(a),
             "verdict": v, "errors": res["errors"],
             "a_tps": [s.tps for s in res["samples_a"]],
             "b_tps": [s.tps for s in res["samples_b"]]},
            indent=2, ensure_ascii=False), encoding="utf-8")
        print(f"  -> {args.json}")

    # 退出码约定（供 CI 直接拦截）：
    #   0 = 无差异或改善
    #   1 = 退化
    #   2 = 疑似（方向可信但幅度未定型）或数据无效 —— 不应直接放行
    if v["label"] == "退化":
        return 1
    if v["label"] in ("疑似", "无效"):
        return 2
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
