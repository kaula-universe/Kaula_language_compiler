#!/usr/bin/env python3
"""Kaula 编译器/运行时 A/B 对比 —— 交错测量。

为什么需要这个工具
------------------
`metrics.py` 的矩阵是**按运行时分批**跑的（kaula 全部 → go 全部 → tokio 全部），
不是逐格交错。实测同一格在两次全量矩阵之间 tps 能差 20%，同一次会话内
同一二进制隔两分钟能差 2.2 倍（tokio/light/1000：1077237 → 490773）。

在这种漂移下直接比较「改动前 vs 改动后」是无效的 —— 对照组（源码一个字
没改的 Go/Tokio）摆动的幅度和被测改动一样大。所以：

  1. 两个变体各自一个干净 git worktree，各自构建自己的 kaulac，
     **各自编译自己的 src/**（运行时 C 源码是被测对象的一部分，
     不能拿 HEAD 的头文件去编 A 的编译器）。
  2. 同一份 bench 源码（从当前工作区的模板渲染），喂给两个编译器。
  3. 运行阶段**交错**：ABBA / BAAB 交替，每对相邻，中位数抵消漂移。
  4. 报「每轮比值的中位数」，而不是「两个中位数相除」—— 前者对
     单轮离群值免疫。

用法
----
    # 先准备两个 worktree 并各自 toolkit_build.py --target compiler
    python test/bench/ab_compiler.py --a <worktreeA> --b <worktreeB>
    python test/bench/ab_compiler.py --a <A> --b <B> --reps 9 --curves degrade

A/B 两边都必须是**已提交的干净状态**。工作区里的未提交改动不在测量范围内 ——
否则测的是「基线 vs 基线+WIP」，问题会说不清。

报表演化（对应 FINDINGS F12：补 p99 离散度列）：
  - tps / p99 均取各变体 reps 轮的中位数；
  - tps B/A、p99 B/A 是**逐轮配对比值的中位数**（不是两个中位数相除）；
  - tpsAσ%/tpsBσ%、p99Aσ%/p99Bσ% 是各变体 reps 轮内极差（max/min-1），
    即「自己跟自己比」的噪声量级。判定规则：B/A 改善幅度必须 > max(两边σ%)，
    否则该格差异落在噪声内（本机噪声底 8%~49%，reps 建议 ≥11）。
  - 跑基准时机器须安静，不要同时编译/跑别的负载。
"""
from __future__ import annotations

import argparse
import statistics
import subprocess
import sys
import time
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))

from run_bench import CURVES, EXE_SUFFIX, RESULT_RE, render_kaula  # noqa: E402

REPO = HERE.parent.parent


# ---------- 构建 ----------

def build(worktree: Path, kl_text: str, stem: str, outdir: Path,
          verbose: bool) -> Path:
    """用 worktree 自己的 kaulac 编译 bench 源码，返回 exe 路径。

    编译器必须在 worktree 根目录下运行：它按 cwd 拼 include 路径
    （-I <repo>/src -I <repo>/std ...），cwd 错了就会拿错版本的运行时头文件。
    """
    kaulac = worktree / "build" / "bin" / ("kaulac.exe" if sys.platform.startswith("win") else "kaulac")
    if not kaulac.exists():
        raise SystemExit(f"变体缺少编译器：{kaulac}")

    src = worktree / "test" / "bench" / "kaula" / f"{stem}.kl"
    src.parent.mkdir(parents=True, exist_ok=True)
    src.write_text(kl_text, encoding="utf-8")

    outdir.mkdir(parents=True, exist_ok=True)
    # 旧产物必须删：下面只判 exists()，上次残留的 exe 会被当成本次结果。
    for stale in outdir.glob(f"{stem}{EXE_SUFFIX}"):
        stale.unlink()

    r = subprocess.run([str(kaulac), "-output-dir", str(outdir), str(src)],
                       capture_output=True, encoding="utf-8", errors="replace",
                       cwd=str(worktree), timeout=900)
    exe = outdir / f"{stem}{EXE_SUFFIX}"
    if not exe.exists():
        raise SystemExit(f"{worktree.name}: kaulac 失败\n{r.stdout[-2000:]}\n{r.stderr[-2000:]}")
    if verbose:
        print(f"[ab] {worktree.name}: {exe.name} ok")
    return exe


# ---------- 运行 ----------

def run(exe: Path, timeout: float) -> dict:
    """跑一次并解析 RESULT 行。

    刻意不做外部采样：run_case.py 那条路每 50ms 拉一个 PowerShell 子进程，
    而 F10 已经判定 ctxsw/threads/handles 那几列不可用。测量 tps/p99 时
    这些开销纯属往里灌噪声 —— 两个变体同等受害，但会放大轮间方差。

    仍然带硬超时：被测程序死锁时不能把整个工具挂死。
    """
    t0 = time.perf_counter()
    proc = subprocess.Popen([str(exe)], stdout=subprocess.PIPE,
                            stderr=subprocess.STDOUT, encoding="utf-8",
                            errors="replace", cwd=str(exe.parent))
    try:
        stdout, _ = proc.communicate(timeout=timeout)
    except subprocess.TimeoutExpired:
        proc.kill()
        stdout, _ = proc.communicate()
        return {"error": f"超时 {timeout:g}s 强杀（死锁？）rc={proc.returncode}"}
    wall = time.perf_counter() - t0

    if proc.returncode != 0 or "RESULT" not in (stdout or ""):
        return {"error": (stdout or "").strip()[-400:] or f"rc={proc.returncode} 无输出"}
    line = [l for l in stdout.splitlines() if l.startswith("RESULT")][-1]
    kv = {k: int(v) for k, v in RESULT_RE.findall(line)}
    return {"tps": kv.get("tps"), "p50": kv.get("p50"), "p99": kv.get("p99"),
            "checksum": kv.get("checksum"), "wall_s": round(wall, 3)}


# ---------- 统计 ----------

def med(xs):
    return statistics.median(xs) if xs else None


def fmt_ratio(b, a):
    if not a or not b:
        return "   n/a"
    return f"{(b / a - 1) * 100:+6.1f}%"


def main() -> int:
    ap = argparse.ArgumentParser(description="Kaula 编译器/运行时 A/B（交错）")
    ap.add_argument("--a", required=True, help="基线 worktree（如 8b5837e）")
    ap.add_argument("--b", required=True, help="对照 worktree（如 f6ee26f）")
    ap.add_argument("--label-a", default="A")
    ap.add_argument("--label-b", default="B")
    ap.add_argument("--curves", default="light,suspend,mixed,degrade")
    ap.add_argument("--conc", default="100,1000")
    ap.add_argument("--total", type=int, default=2000)
    ap.add_argument("--reps", type=int, default=7, help="每个变体每格的轮数")
    ap.add_argument("--timeout", type=float, default=60.0)
    ap.add_argument("--out", default=str(REPO / "build" / "ab"))
    ap.add_argument("--verbose", action="store_true")
    args = ap.parse_args()

    wa, wb = Path(args.a).resolve(), Path(args.b).resolve()
    out = Path(args.out).resolve()
    curves = [c.strip() for c in args.curves.split(",") if c.strip()]
    concs = [int(c) for c in args.conc.split(",") if c.strip()]
    for c in curves:
        if c not in CURVES:
            raise SystemExit(f"未知曲线 {c}；可选 {sorted(CURVES)}")

    for w in (wa, wb):
        if not (w / "build" / "bin").exists():
            raise SystemExit(f"{w} 下没有 build/bin，先在该 worktree 跑 "
                             f"python toolkit_build.py --target compiler")

    # 记录两个 worktree 的 HEAD，便于报告里说清「比的到底是什么」
    def head(w):
        r = subprocess.run(["git", "-C", str(w), "log", "-1", "--format=%h %s"],
                           capture_output=True, encoding="utf-8", errors="replace")
        return r.stdout.strip()

    print(f"[ab] A({args.label_a}) = {head(wa)}")
    print(f"[ab] B({args.label_b}) = {head(wb)}")
    print(f"[ab] reps={args.reps} total={args.total} 交错执行\n")

    rows = []
    for curve in curves:
        for conc in concs:
            kl = render_kaula(curve, conc, args.total)
            stem = f"ab_{curve}_{conc}"
            ea = build(wa, kl, stem, out / args.label_a, args.verbose)
            eb = build(wb, kl, stem, out / args.label_b, args.verbose)

            sa, sb = [], []
            pairs = []   # 同一 rep 内 (A轮, B轮)，用于逐轮配对比值
            sums = set()
            errs = []
            for rep in range(args.reps):
                # ABBA / BAAB 交替：相邻两轮里两个变体各占一次「先手」，
                # 谁都不总是跑在热缓存/冷缓存的位置上。
                order = [("A", ea), ("B", eb)]
                if rep % 2:
                    order.reverse()
                round_res = {}
                for tag, exe in order:
                    r = run(exe, args.timeout)
                    if "error" in r:
                        errs.append(f"{tag}: {r['error']}")
                        continue
                    sums.add(r["checksum"])
                    round_res[tag] = r
                    (sa if tag == "A" else sb).append(r)
                # 只有该 rep 两个变体都成功，才配成一对进逐轮比值
                if "A" in round_res and "B" in round_res:
                    pairs.append((round_res["A"], round_res["B"]))

            if errs:
                print(f"[ab] {curve}/conc={conc} 失败：{errs[0][:160]}")
            if not sa or not sb:
                print(f"[ab] {curve}/conc={conc} 数据不足（A={len(sa)} B={len(sb)} 轮）")
                continue

            if len(sums) != 1:
                print(f"[ab] {curve}/conc={conc} checksum 不一致：{sums} —— 数据不可用")

            def med(xs):
                return statistics.median(xs) if xs else None

            def spread(xs):
                """轮内极差离散度 = max/min - 1（百分比）。

                同一变体在 reps 轮里最快与最慢之比。实测噪声底 8%~49%，
                这个数就是该格「自己跟自己比」的噪声量级：
                若 B/A 改善幅度 ≤ 本格 A 或 B 的 spread，差异落在噪声内，不能下结论。
                """
                lo, hi = min(xs), max(xs)
                return (hi / lo - 1) * 100 if lo else 0.0

            tps_a = med([r["tps"] for r in sa])
            tps_b = med([r["tps"] for r in sb])
            p99_a = med([r["p99"] for r in sa])
            p99_b = med([r["p99"] for r in sb])
            # 逐轮配对比值的中位数：对单轮离群值免疫，比「两个中位数相除」稳。
            # tps 越大越好、p99 越小越好，统一报 B/A（p99 的 <1 即 B 更快）。
            tps_ratio = med([rb["tps"] / ra["tps"]
                             for ra, rb in pairs if ra["tps"]])
            p99_ratio = med([rb["p99"] / ra["p99"]
                             for ra, rb in pairs if ra["p99"]])
            rows.append((curve, conc,
                         tps_a, tps_b, tps_ratio,
                         spread([r["tps"] for r in sa]),
                         spread([r["tps"] for r in sb]),
                         p99_a, p99_b, p99_ratio,
                         spread([r["p99"] for r in sa]),
                         spread([r["p99"] for r in sb])))

    if not rows:
        print("[ab] 没有可用数据")
        return 1

    print("=" * 132)
    print(f"{'curve/conc':<16}{'A tps':>10}{'B tps':>10}{'tps B/A':>9}"
          f"{'tpsAσ%':>8}{'tpsBσ%':>8}{'A p99':>9}{'B p99':>9}"
          f"{'p99 B/A':>9}{'p99Aσ%':>8}{'p99Bσ%':>8}")
    print("-" * 132)
    for (curve, conc, ta, tb, tr, ts_a, ts_b,
         pa, pb, pr, ps_a, ps_b) in rows:
        # p99 可能为 None：light/degrade 曲线不产生挂起，内核打点恒为 0，
        # 逐轮配对比值被 `if ra["p99"]` 全部过滤掉 → med([]) 返回 None。
        # 此时该列显示 n/a，不能让格式化炸掉整份报告（之前是 TypeError）。
        ta_s = f"{ta:>10,.0f}" if ta is not None else f"{'n/a':>10}"
        tb_s = f"{tb:>10,.0f}" if tb is not None else f"{'n/a':>10}"
        tr_s = f"{(tr - 1) * 100:>+8.0f}%" if tr is not None else f"{'n/a':>9}"
        ts_a_s = f"{ts_a:>7.0f}%" if ts_a is not None else f"{'n/a':>8}"
        ts_b_s = f"{ts_b:>7.0f}%" if ts_b is not None else f"{'n/a':>8}"
        pa_s = f"{pa / 1000:>8.1f}u" if pa is not None else f"{'n/a':>9}"
        pb_s = f"{pb / 1000:>8.1f}u" if pb is not None else f"{'n/a':>9}"
        pr_s = f"{(pr - 1) * 100:>+8.0f}%" if pr is not None else f"{'n/a':>9}"
        ps_a_s = f"{ps_a:>7.0f}%" if ps_a is not None else f"{'n/a':>8}"
        ps_b_s = f"{ps_b:>7.0f}%" if ps_b is not None else f"{'n/a':>8}"
        print(f"{curve + '/' + str(conc):<16}{ta_s}{tb_s}"
              f"{tr_s}{ts_a_s}{ts_b_s}"
              f"{pa_s}{pb_s}"
              f"{pr_s}{ps_a_s}{ps_b_s}")
    print("=" * 132)
    print("tps / p99 取各变体 reps 轮的中位数；tps B/A、p99 B/A 是**逐轮配对比值的中位数**。")
    print("σ% = 该变体 reps 轮内极差（max/min - 1），即「自己跟自己比」的噪声量级。")
    print("判定：B/A 改善幅度必须 > max(tpsAσ%, tpsBσ%)（p99 同理比 p99Aσ/p99Bσ）才算显著；")
    print("      否则该格差异落在噪声内。p99 B/A 为负 = B 尾延迟更低（更好）。")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())