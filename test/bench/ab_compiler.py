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

from run_bench import CURVES, RESULT_RE, render_kaula  # noqa: E402

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
    for stale in outdir.glob(f"{stem}.exe"):
        stale.unlink()

    r = subprocess.run([str(kaulac), "-output-dir", str(outdir), str(src)],
                       capture_output=True, encoding="utf-8", errors="replace",
                       cwd=str(worktree), timeout=900)
    exe = outdir / f"{stem}.exe"
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
            sums = set()
            errs = []
            for rep in range(args.reps):
                # ABBA / BAAB 交替：相邻两轮里两个变体各占一次「先手」，
                # 谁都不总是跑在热缓存/冷缓存的位置上。
                order = [("A", ea), ("B", eb)]
                if rep % 2:
                    order.reverse()
                for tag, exe in order:
                    r = run(exe, args.timeout)
                    if "error" in r:
                        errs.append(f"{tag}: {r['error']}")
                        continue
                    sums.add(r["checksum"])
                    (sa if tag == "A" else sb).append(r)

            if errs:
                print(f"[ab] {curve}/conc={conc} 失败：{errs[0][:160]}")
            if not sa or not sb:
                print(f"[ab] {curve}/conc={conc} 数据不足（A={len(sa)} B={len(sb)} 轮）")
                continue

            if len(sums) != 1:
                print(f"[ab] {curve}/conc={conc} checksum 不一致：{sums} —— 数据不可用")

            tps_a = med([r["tps"] for r in sa])
            tps_b = med([r["tps"] for r in sb])
            p99_a = med([r["p99"] for r in sa])
            p99_b = med([r["p99"] for r in sb])
            # 每轮比值的中位数：对单轮离群值免疫，比「两个中位数相除」稳
            ratios = sorted(rb["tps"] / ra["tps"] for ra, rb in zip(sa, sb) if ra["tps"])
            rows.append((curve, conc, tps_a, tps_b, med(ratios), p99_a, p99_b,
                         min(sa, key=lambda r: r["tps"])["tps"],
                         max(sa, key=lambda r: r["tps"])["tps"]))

    if not rows:
        print("[ab] 没有可用数据")
        return 1

    print("=" * 104)
    print(f"{'curve/conc':<18}{'A tps':>11}{'B tps':>11}{'B/A':>9}"
          f"{'A p99':>11}{'B p99':>11}{'p99 B/A':>10}{'A 轮内极差':>13}")
    print("-" * 104)
    for curve, conc, ta, tb, rm, pa, pb, lo, hi in rows:
        spread = (hi / lo - 1) * 100 if lo else 0
        print(f"{curve + '/' + str(conc):<18}{ta:>11,.0f}{tb:>11,.0f}"
              f"{fmt_ratio(tb, ta):>9}{pa / 1000:>10.1f}u{pb / 1000:>10.1f}u"
              f"{fmt_ratio(pb, pa):>10}{spread:>12.0f}%")
    print("=" * 104)
    print("tps / p99 取各变体 reps 轮的中位数；B/A 列是**每轮比值的中位数**。")
    print("A 轮内极差 = A 自身最快/最慢之比，用来判断这个格的噪声量级：")
    print("极差与 B/A 同量级时，该格的差异不能当结论。")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())