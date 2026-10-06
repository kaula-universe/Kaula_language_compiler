#!/usr/bin/env python3
"""A/A 与 A/B 交错对照采样器。

为什么需要这个脚本：
    这台机器（32 核共享实例）单次基准采样噪声可达 3~7 倍。要判定一个
    改动是「真改善」还是「噪声」，必须知道噪声带本身有多宽。

    方法：同一批内交替跑 A 和 B（A,B,A,B,...），每次取一次 TPS 样本。
    交错的意义是把「时间漂移」（热迁移、邻居负载、频率调节）均摊到
    两边，而不是让 A 全跑在前、B 全跑在后。

    A/A 模式（--b 不给）用于测「同二进制 vs 自身」的噪声带：
    如果 A/A 自己就能跑出 ±15%，那么任何 ±15% 以内的 A/B 差异都不
    可判读。

用法：
    # A/A 噪声带：同一 exe 跑 21 次交错
    python test/bench/ab_interleave.py --a build/.../bench_c --curve light \\
        --conc 1000 --total 20000 --rounds 21

    # A/B：两个不同 exe（或同 exe 不同 env）
    python test/bench/ab_interleave.py --a exeA --b exeB --env-b KAULA_X=1 ...

输出：每轮 A/B 原始 TPS、均值、中位数、标准差、CV，以及 B/A 比值。
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

# 注意：13356499072 是**单次事务** parse_and_checksum 的期望值
# （见 TRANSACTION.md / cbench/main.c 的 EXPECTED_SUM），不是总和。
# RESULT 行的 checksum 是多次事务返回值之和（模运算下），
# 与单次值本就不同，不能直接比较 —— 只校验「各轮之间是否一致」。
EXPECTED_SUM_PER_TXN = 13356499072


def run_once(exe: Path, curve: str, conc: int, total: int,
             env: dict | None = None, timeout: float = 300.0) -> dict:
    cmd = [str(exe), curve, str(conc), str(total)]
    full_env = dict(os.environ)
    if env:
        full_env.update(env)
    t0 = time.perf_counter()
    try:
        r = subprocess.run(cmd, capture_output=True, encoding="utf-8",
                           errors="replace", env=full_env, timeout=timeout,
                           cwd=str(exe.parent))
    except subprocess.TimeoutExpired:
        return {"error": f"超时 {timeout}s"}
    wall = time.perf_counter() - t0
    out = (r.stdout or "") + (r.stderr or "")
    if r.returncode != 0 or "RESULT" not in out:
        return {"error": f"rc={r.returncode}: {out.strip()[-300:]}"}
    line = [l for l in out.splitlines() if l.startswith("RESULT")][-1]
    kv = dict((k, int(v)) for k, v in RESULT_RE.findall(line))
    rec = {"tps": kv.get("tps", 0), "p50": kv.get("p50", 0),
           "p99": kv.get("p99", 0), "checksum": kv.get("checksum", 0),
           "n": kv.get("n", 0), "errors": kv.get("errors", 0),
           "spawn_failed": kv.get("spawn_failed", 0),
           "wall_s": round(wall, 3)}
    # 正确性判据：errors / spawn_failed 必须为 0
    # （checksum 的总和值跨轮一致性在 main 里统一校验）
    if rec["errors"] or rec["spawn_failed"]:
        rec["correctness_fail"] = True
    return rec


def stats(xs: list[float]) -> dict:
    if not xs:
        return {}
    m = statistics.mean(xs)
    med = statistics.median(xs)
    sd = statistics.stdev(xs) if len(xs) > 1 else 0.0
    return {
        "n": len(xs),
        "mean": m,
        "median": med,
        "min": min(xs),
        "max": max(xs),
        "stdev": sd,
        "cv_pct": (sd / m * 100.0) if m else 0.0,
        # 噪声带：以中位数为基准的极差百分比
        "spread_pct": ((max(xs) - min(xs)) / med * 100.0) if med else 0.0,
    }


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--a", required=True, help="A 侧可执行文件")
    ap.add_argument("--b", default="", help="B 侧可执行文件；不给则为 A/A 模式")
    ap.add_argument("--env-b", default="", help="B 侧附加环境变量，K=V,K=V 形式")
    ap.add_argument("--curve", default="light")
    ap.add_argument("--conc", type=int, default=1000)
    ap.add_argument("--total", type=int, default=20000)
    ap.add_argument("--rounds", type=int, default=21,
                    help="交错轮数：A、B 各跑 rounds 次，交错执行")
    ap.add_argument("--json", default="")
    ap.add_argument("--case-timeout", type=float, default=300.0)
    a = ap.parse_args()

    exe_a = Path(a.a).resolve()
    aa_mode = not a.b
    exe_b = Path(a.b).resolve() if a.b else exe_a

    env_b = {}
    if a.env_b:
        for pair in a.env_b.split(","):
            if "=" in pair:
                k, v = pair.split("=", 1)
                env_b[k.strip()] = v.strip()

    if not exe_a.exists():
        raise SystemExit(f"A 侧不存在: {exe_a}")
    if not exe_b.exists():
        raise SystemExit(f"B 侧不存在: {exe_b}")

    mode = "A/A（噪声带）" if aa_mode else "A/B"
    print(f"模式: {mode}  curve={a.curve} conc={a.conc} total={a.total} "
          f"rounds={a.rounds}", flush=True)
    if env_b:
        print(f"B 侧环境变量: {env_b}", flush=True)

    a_tps, b_tps, a_p99, b_p99 = [], [], [], []
    a_ck, b_ck = [], []
    errors = []
    for i in range(a.rounds):
        # 交错：同一轮内 A 先 B 后；下一轮 B 先 A 后（对称化顺序偏差）
        if i % 2 == 0:
            order = [("A", exe_a, None), ("B", exe_b, env_b)]
        else:
            order = [("B", exe_b, env_b), ("A", exe_a, None)]
        line_parts = []
        for tag, exe, env in order:
            rec = run_once(exe, a.curve, a.conc, a.total, env,
                           timeout=a.case_timeout)
            if "error" in rec:
                errors.append(f"round{i} {tag}: {rec['error']}")
                line_parts.append(f"{tag}=ERR")
                continue
            if rec.get("correctness_fail"):
                errors.append(f"round{i} {tag}: errors={rec['errors']} "
                              f"spawn_failed={rec['spawn_failed']}")
            if tag == "A":
                a_tps.append(rec["tps"]); a_p99.append(rec["p99"])
                a_ck.append(rec["checksum"])
            else:
                b_tps.append(rec["tps"]); b_p99.append(rec["p99"])
                b_ck.append(rec["checksum"])
            line_parts.append(f"{tag}={rec['tps']}")
        print(f"  [{i+1:>2}/{a.rounds}] " + "  ".join(line_parts), flush=True)

    sa, sb = stats(a_tps), stats(b_tps)
    if not sa or not sb:
        print("采样不足，无法比较", file=sys.stderr)
        return 2

    # 跨轮 checksum 一致性：同一个 exe + 同参数下，总和值必须完全相同。
    # 这是「数据收敛」的正确性判据（与性能噪声无关）。
    ck_report = []
    for tag, cks in (("A", a_ck), ("B", b_ck)):
        uniq = sorted(set(cks))
        ck_report.append((tag, uniq, len(cks)))

    print()
    print("=" * 78)
    print(f"{mode}  结果  curve={a.curve} conc={a.conc} rounds={a.rounds}")
    print("=" * 78)
    hdr = f"{'侧':<6}{'mean':>12}{'median':>12}{'min':>12}{'max':>12}{'stdev':>11}{'CV%':>8}{'极差%':>8}"
    print(hdr)
    print("-" * 78)
    for tag, s in (("A", sa), ("B", sb)):
        print(f"{tag:<6}{s['mean']:>12.0f}{s['median']:>12.0f}{s['min']:>12.0f}"
              f"{s['max']:>12.0f}{s['stdev']:>11.0f}{s['cv_pct']:>8.1f}"
              f"{s['spread_pct']:>8.1f}")

    print()
    d_mean = (sb["mean"] - sa["mean"]) / sa["mean"] * 100.0
    d_med = (sb["median"] - sa["median"]) / sa["median"] * 100.0
    print(f"B/A 均值差  : {d_mean:+.2f}%")
    print(f"B/A 中位差  : {d_med:+.2f}%")
    # 噪声带判定：两侧 std 的合并估计。差异若小于合并 std，则不显著。
    pooled_sd = ((sa["stdev"] ** 2 + sb["stdev"] ** 2) / 2) ** 0.5
    print(f"合并 std    : {pooled_sd:.0f}")
    print(f"|均值差|/std: {abs(sb['mean'] - sa['mean']) / pooled_sd:.2f}"
          if pooled_sd else "")
    if aa_mode:
        print()
        print(f"→ A/A 噪声带（同二进制 vs 自身）：CV="
              f"{max(sa['cv_pct'], sb['cv_pct']):.1f}%，"
              f"极差={max(sa['spread_pct'], sb['spread_pct']):.1f}%")
        print(f"→ 判定阈值：A/B 差异需超过 {max(sa['spread_pct'], sb['spread_pct']):.1f}% "
              f"才可认为不是噪声")

    if errors:
        print()
        print(f"异常 {len(errors)} 条：")
        for e in errors[:10]:
            print(f"  {e}")

    print()
    for tag, uniq, total in ck_report:
        if len(uniq) == 1:
            print(f"{tag} 侧 checksum：{total}/{total} 轮一致 = {uniq[0]}  ✓")
        else:
            print(f"{tag} 侧 checksum：{total} 轮出现 {len(uniq)} 种值 "
                  f"{uniq[:5]}{'...' if len(uniq) > 5 else ''}  ✗ 不收敛")

    if a.json:
        Path(a.json).write_text(json.dumps({
            "mode": mode, "curve": a.curve, "conc": a.conc,
            "rounds": a.rounds, "aa": aa_mode, "env_b": env_b,
            "a_tps": a_tps, "b_tps": b_tps,
            "a_stats": sa, "b_stats": sb, "errors": errors,
        }, indent=2), encoding="utf-8")
        print(f"\n原始记录 -> {a.json}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
