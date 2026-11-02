#!/usr/bin/env python3
"""
bench_stable.py — 交错 A/B 多轮稳定基准（本地运行，不入库）

问题：单样本跨轮噪声底 8%~49%（F10/F12），"先跑完 Kaula 再跑 Go"的
连续重复也无法对抗时间漂移。本脚本对**同一 (curve, conc) 配置**做
K,G,K,G... 交错采样：每个 A/B 对落在相邻时间窗，环境漂移被平均掉。

用法：
  python test/bench/bench_stable.py --pairs 9 --curves light,suspend,mixed,degrade \
      --conc 1000,10000 --total 20000 --json test/bench/bench_stable_result.json

输出：
  - 每配置每 runtime 的 tps / p999_ns 采样列表
  - 主值 = 中位数（对单轮离群免疫）
  - spread% = max/min - 1（轮内极差，自噪声）；cv% = 标准差/均值
  - 倍率 = Kaula 中位 / Go 中位（TPS）；p999 逐对对比
"""
import argparse
import json
import statistics
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import metrics as M  # noqa: E402


def stats(xs: list[int]) -> dict:
    xs = [x for x in xs if x is not None]
    if not xs:
        return {"n": 0}
    med = statistics.median(xs)
    mn, mx = min(xs), max(xs)
    mean = statistics.mean(xs)
    sd = statistics.stdev(xs) if len(xs) > 1 else 0.0
    return {
        "n": len(xs),
        "median": round(med),
        "mean": round(mean),
        "min": mn,
        "max": mx,
        "spread_pct": round((mx / mn - 1) * 100, 1) if mn else None,
        "cv_pct": round(sd / mean * 100, 1) if mean else None,
        "samples": xs,
    }


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--pairs", type=int, default=9,
                    help="A/B 交错轮数（每轮 K 与 G 相邻各跑一次）。正式结论建议 >=9。")
    ap.add_argument("--curves", default="light,suspend,mixed,degrade")
    ap.add_argument("--conc", default="1000,10000")
    ap.add_argument("--total", type=int, default=20000)
    ap.add_argument("--json", default="")
    ap.add_argument("--skip-build", action="store_true")
    a = ap.parse_args()

    curves = [x for x in a.curves.split(",") if x]
    concs = [int(x) for x in a.conc.split(",") if x]
    pairs = max(1, a.pairs)

    out: list[dict] = []

    if not a.skip_build:
        M.log("构建 Go ...")
        go_exe = M.build_go()
    else:
        go_exe = None

    for curve in curves:
        for conc in concs:
            M.log(f"=== {curve}/conc={conc}（{pairs} 对交错，前 1 对预热丢弃） ===")
            kaula_exe = None if a.skip_build else M.build_kaula(curve, conc, a.total)
            if kaula_exe is None and not a.skip_build:
                out.append({"curve": curve, "conc": conc, "error": "build_kaula failed"})
                continue
            if go_exe is None:
                out.append({"curve": curve, "conc": conc, "error": "build_go failed"})
                continue

            k_tps, k_p999, g_tps, g_p999 = [], [], [], []
            failed_pairs = 0

            def run_one(exe, args, rt):
                rec = M.run_with_retries(
                    exe, args, rt, curve, conc, a.total,
                    interval=0.05, timeout=600.0, stop_loss=None, retries=4)
                return rec

            for i in range(pairs + 1):  # +1 = 第 1 对预热（丢弃）
                kr = run_one(kaula_exe, [], "kaula")
                gr = run_one(go_exe, [curve, str(conc), str(a.total)], "go")
                if i == 0:
                    continue  # 预热对丢弃（缓存/频率冷启动）
                if kr.error or gr.error:
                    failed_pairs += 1
                    M.log(f"  对 {i} 失败: kaula={kr.error or 'ok'} go={gr.error or 'ok'}")
                    continue
                k_tps.append(kr.values["tps"])
                k_p999.append(kr.values.get("p999_ns", 0))
                g_tps.append(gr.values["tps"])
                g_p999.append(gr.values.get("p999_ns", 0))

            row = {
                "curve": curve, "conc": conc, "pairs": pairs,
                "kaula_tps": stats(k_tps),
                "go_tps": stats(g_tps),
                "kaula_p999_ns": stats(k_p999),
                "go_p999_ns": stats(g_p999),
                "failed_pairs": failed_pairs,
            }
            kmed = row["kaula_tps"].get("median")
            gmed = row["go_tps"].get("median")
            if kmed and gmed:
                row["tps_ratio_k_over_g"] = round(kmed / gmed, 3)
            out.append(row)

            kps = row["kaula_tps"]
            gps = row["go_tps"]
            M.log(f"  Kaula tps 中位 {kps.get('median', 0):,.0f} "
                  f"(spread {kps.get('spread_pct')}%, cv {kps.get('cv_pct')}%) | "
                  f"Go tps 中位 {gps.get('median', 0):,.0f} "
                  f"(spread {gps.get('spread_pct')}%, cv {gps.get('cv_pct')}%) | "
                  f"倍率 {row.get('tps_ratio_k_over_g')}")

    if a.json:
        Path(a.json).write_text(json.dumps(out, indent=2), encoding="utf-8")
        M.log(f"原始记录 -> {a.json}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
