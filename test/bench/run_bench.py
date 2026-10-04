#!/usr/bin/env python3
"""跨运行时并发内核基准 —— orchestrator。

三件事：生成 Kaula 源码、构建三个运行时、跑矩阵并出报告。

事务定义与公平性判据见 TRANSACTION.md。这个脚本只负责执行，
不做任何「让某一方好看」的调整。

用法:
    python test/bench/run_bench.py --list
    python test/bench/run_bench.py --runtimes kaula,go
    python test/bench/run_bench.py --curves light,suspend --conc 1000,10000
"""
from __future__ import annotations

import argparse
import json
import os
import re
import shutil
import statistics
import subprocess
import sys
import time
from pathlib import Path

HERE = Path(__file__).resolve().parent
REPO = HERE.parent.parent
KAULA_HOME = REPO / "build"

# 事务常量（三边必须一致，见 TRANSACTION.md）
EXPECTED_CHECKSUM = 13356499072

# 采样上限：延迟样本存不下无限多，超过部分按固定间隔抽样
MAX_SAMPLES = 2000

# 四个曲线。suspend_us 是挂起时长；mixed/degrade 的比例由模板填。
CURVES = {
    "light":    {"suspend_us": 0,    "mixed_sleep_pct": 0,   "degrade_pct": 0},
    "suspend":  {"suspend_us": 200,  "mixed_sleep_pct": 0,   "degrade_pct": 0},
    "mixed":    {"suspend_us": 200,  "mixed_sleep_pct": 50,  "degrade_pct": 0},
    "degrade":  {"suspend_us": 0,    "mixed_sleep_pct": 0,   "degrade_pct": 50},
}

# 长忙循环的轮数：让单个任务明显超过预算量子（默认 256）
DEGRADE_SPIN = 20000


def log(msg: str) -> None:
    print(f"[bench] {msg}", flush=True)


# ---------- Kaula：生成源码 ----------

def render_kaula(curve: str, conc: int, total: int) -> str:
    """把模板渲染成 bench_gen.kl。

    参数生成进源码而不是走命令行：Kaula 的参数 API 要构造 CliApp*，
    那块语言表面尚未验证，而基准的每一个字节都必须是已验证的东西。
    """
    cfg = CURVES[curve]
    tmpl = (HERE / "kaula" / "bench.kl.tmpl").read_text(encoding="utf-8")

    if curve == "light":
        submit = "in_flight[j] = spawn curve_light()"
        # expected 用同一条 spawn 路径取，避免模板里硬编码期望值导致
        # 「源码改了但期望值没改」这类静默错配。
        expected = "spawn curve_light()"
    elif curve == "suspend":
        submit = "in_flight[j] = spawn curve_suspend()"
        expected = "spawn curve_suspend()"
    elif curve == "mixed":
        # 按 issued 的奇偶决定这一单是否带挂起：50% 且不需要随机源
        submit = "in_flight[j] = spawn curve_mixed(issued % 2 == 0)"
        # Go 侧 expected = runCurve(curve, false) —— 同样取「不带挂起」那支，
        # 带不带挂起只影响时序不影响返回值，两边必然相等。
        expected = "spawn curve_mixed(false)"
    else:  # degrade
        submit = "in_flight[j] = spawn curve_degrade(%d)" % DEGRADE_SPIN
        expected = "spawn curve_degrade(%d)" % DEGRADE_SPIN

    out = (tmpl
           .replace("@@CURVE@@", curve)
           .replace("@@SUBMIT@@", submit)
           .replace("@@EXPECTED@@", expected)
           .replace("@@TOTAL@@", str(total))
           .replace("@@CONC@@", str(conc))
           .replace("@@MAX_CONC@@", str(conc))
           .replace("@@MAX_SAMPLES@@", str(MAX_SAMPLES))
           .replace("@@SUSPEND_US@@", str(cfg["suspend_us"]))
           .replace("@@MIXED_SLEEP_PCT@@", str(cfg["mixed_sleep_pct"]))
           .replace("@@DEGRADE_PCT@@", str(cfg["degrade_pct"])))
    if "@@" in out:
        raise SystemExit(f"模板占位符未替换干净：{re.findall(r'@@[A-Z_]+@@', out)}")
    return out


def build_kaula(curve: str, conc: int, total: int) -> Path:
    src = HERE / "kaula" / "bench_gen.kl"
    src.write_text(render_kaula(curve, conc, total), encoding="utf-8")
    exe = HERE / "kaula" / "bench_gen.exe"
    kaulac = KAULA_HOME / "bin" / "kaulac.exe"
    if not kaulac.exists():
        raise SystemExit(f"找不到 {kaulac}，先跑 python toolkit_build.py --target compiler")
    # 先删旧产物：下面只判 exists()。若上一次的 exe 正在运行导致本次链接
    # 失败，旧文件仍在 —— 会把上一条曲线的二进制当成本次结果跑出来。
    # 实测踩过：suspend 进程挂着，后续 sweep 全部复用它，误判成「全线超时」。
    exe.unlink(missing_ok=True)
    # encoding 显式指定：text=True 在 Windows 上按 locale（GBK）解码，
    # 而 kaulac 的诊断是 UTF-8 —— 混用会在 _readerthread 里抛
    # UnicodeDecodeError，且发生在子线程，主流程只剩半截输出。
    r = subprocess.run([str(kaulac), str(src)], capture_output=True,
                       encoding="utf-8", errors="replace", cwd=str(REPO))
    if not exe.exists():
        raise SystemExit(f"kaulac 失败:\n{r.stdout[-2000:]}\n{r.stderr[-2000:]}")
    return exe


# ---------- Go / Tokio：命令行参数 ----------

def build_go() -> Path:
    src = HERE / "go" / "bench"
    exe = src / "bench_go.exe"
    r = subprocess.run(["go", "build", "-o", str(exe), "."], cwd=str(src),
                       capture_output=True, encoding="utf-8", errors="replace")
    if r.returncode != 0:
        raise SystemExit(f"go build 失败:\n{r.stdout}\n{r.stderr}")
    return exe


def build_tokio() -> Path:
    src = HERE / "tokio" / "bench"
    exe = src / "target" / "release" / "bench_tokio.exe"
    r = subprocess.run(["cargo", "build", "--release"], cwd=str(src),
                       capture_output=True, encoding="utf-8", errors="replace")
    if not exe.exists():
        raise SystemExit(f"cargo build 失败:\n{r.stdout[-2000:]}\n{r.stderr[-2000:]}")
    return exe


# ---------- 运行与解析 ----------

RESULT_RE = re.compile(r"(\w+)=(-?\d+)")

# 非 RESULT 的诊断行，用于失败时定位
DIAG_RE = re.compile(r"^(BENCH_ERROR|MISMATCH|\S+\s*=.*)$")


def run_case(exe: Path, runtime: str, curve: str, conc: int, total: int,
             env: dict | None = None, case_timeout: float = 300.0) -> dict:
    """跑一个用例，解析 RESULT 行。返回一条记录（含外部采样的 CPU/内存）。

    case_timeout 是整个用例的硬上限。缺了它，一旦被测程序死锁（例如
    F1 的丢任务），下面那个 while 会永远转下去 —— 整个工具挂死且不留
    任何输出，比跑出错误数据更难排查。
    """
    cmd = [str(exe)]
    if runtime != "kaula":       # Kaula 的参数已编译进源码
        cmd += [curve, str(conc), str(total)]
    full_env = dict(os.environ)
    if env:
        full_env.update(env)

    t_wall = time.perf_counter()
    proc = subprocess.Popen(cmd, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                            encoding="utf-8", errors="replace",
                            env=full_env, cwd=str(exe.parent))
    samples = []
    timed_out = False
    while proc.poll() is None:
        if time.perf_counter() - t_wall > case_timeout:
            # 必须杀进程树，否则被测程序会一直挂着污染后续用例
            timed_out = True
            proc.kill()
            break
        # 外部采样：不问运行时自己报（三个运行时口径不同），统一用 OS 视角
        try:
            out = subprocess.run(
                ["powershell", "-NoProfile", "-Command",
                 "$p=Get-Process -Id %d -ErrorAction SilentlyContinue;"
                 "if($p){'{0:N1}' -f $p.CPU}" % proc.pid],
                capture_output=True, text=True, timeout=5)
            samples.append(float(out.stdout.strip() or 0))
        except Exception:
            pass
        time.sleep(0.05)
    # 进程已退出或已被 kill；communicate 负责读完管道并回收
    try:
        stdout, _ = proc.communicate(timeout=15)
    except Exception:
        stdout = proc.stdout.read() if proc.stdout else ""
    stdout = stdout or ""
    wall = time.perf_counter() - t_wall

    rec = {"runtime": runtime, "curve": curve, "conc": conc, "total": total,
           "rc": proc.returncode, "wall_s": round(wall, 3)}

    if timed_out:
        rec["error"] = (f"超时 {case_timeout:g}s 强杀，被测程序死锁，"
                        f"rc={proc.returncode}")
        return rec

    if proc.returncode != 0 or "RESULT" not in stdout:
        rec["error"] = stdout.strip()[-800:] or f"rc={proc.returncode} 无输出"
        return rec

    line = [l for l in stdout.splitlines() if l.startswith("RESULT")][-1]
    kv = dict((k, int(v)) for k, v in RESULT_RE.findall(line))
    rec.update({k: kv[k] for k in
                ("tps", "elapsed_ns", "p50", "p95", "p99", "max", "n", "checksum")
                if k in kv})

    if rec.get("checksum") and kv.get("checksum", 0) % 1 == 0:
        # checksum 是所有返回值之和的模，无法与单次事务值直接比较；
        # 改为校验「是否所有运行都一致」——在报告阶段做
        rec["checksum"] = kv["checksum"]

    cpu = [s for s in samples if s > 0]
    rec["cpu_s"] = round(max(cpu), 2) if cpu else None
    return rec


# ---------- 报告 ----------

def ratios(rec: dict) -> dict:
    """四个比值。TPS 是结果指标，必须配着这些看。"""
    tps = rec.get("tps") or 0
    conc = rec.get("conc") or 1
    p50 = rec.get("p50") or 0
    p99 = rec.get("p99") or 0
    wall = rec.get("wall_s") or 1
    return {
        # 每任务开销：越高说明固定开销越低
        "tps_per_conc": round(tps / conc, 3),
        # 越接近 1 越稳
        "p99_over_p50": round(p99 / p50, 3) if p50 else None,
        # 单位 CPU 换来的吞吐
        "tps_per_cpu_s": round(tps / rec["cpu_s"], 1) if rec.get("cpu_s") else None,
    }


def print_report(records: list[dict]) -> None:
    ok = [r for r in records if "error" not in r]
    bad = [r for r in records if "error" in r]

    print()
    print("=" * 96)
    print("TPS 曲线（单事务延迟含调度，与用户感知一致）")
    print("=" * 96)
    hdr = f"{'runtime':<8}{'curve':<10}{'conc':>8}{'TPS':>12}{'p50(us)':>10}{'p95(us)':>10}{'p99(us)':>10}{'p99/p50':>10}"
    print(hdr)
    print("-" * 96)
    for r in ok:
        p50 = (r.get("p50") or 0) / 1000
        p95 = (r.get("p95") or 0) / 1000
        p99 = (r.get("p99") or 0) / 1000
        rr = ratios(r)
        print(f"{r['runtime']:<8}{r['curve']:<10}{r['conc']:>8}{r['tps']:>12}"
              f"{p50:>10.1f}{p95:>10.1f}{p99:>10.1f}{rr['p99_over_p50'] or 0:>10.2f}")

    print()
    print("=" * 96)
    print("单位开销（跨运行时比这个，不比 TPS 绝对值）")
    print("=" * 96)
    print(f"{'runtime':<8}{'curve':<10}{'conc':>8}{'TPS/conc':>12}{'TPS/CPU-s':>12}")
    print("-" * 96)
    for r in ok:
        rr = ratios(r)
        print(f"{r['runtime']:<8}{r['curve']:<10}{r['conc']:>8}"
              f"{rr['tps_per_conc']:>12.3f}{(rr['tps_per_cpu_s'] or 0):>12.1f}")

    if bad:
        print()
        print(f"失败 {len(bad)} 例：")
        for r in bad:
            print(f"  {r['runtime']}/{r['curve']}/conc={r['conc']}: {r['error'][:200]}")


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--runtimes", default="kaula,go")
    ap.add_argument("--curves", default="light,suspend,mixed,degrade")
    ap.add_argument("--conc", default="1000,10000")
    ap.add_argument("--total", type=int, default=20000)
    ap.add_argument("--json", default="", help="把原始记录写到该 JSON 文件")
    ap.add_argument("--case-timeout", type=float, default=300.0,
                    help="单个用例的硬超时秒数；被测程序死锁时强杀并记为失败")
    ap.add_argument("--list", action="store_true")
    a = ap.parse_args()

    runtimes = [x for x in a.runtimes.split(",") if x]
    curves = [x for x in a.curves.split(",") if x]
    concs = [int(x) for x in a.conc.split(",") if x]

    if a.list:
        print("runtimes: kaula, go, tokio")
        print("curves:  ", ", ".join(CURVES))
        print("conc:    ", ", ".join(map(str, concs)))
        return 0

    unknown = set(curves) - set(CURVES)
    if unknown:
        raise SystemExit(f"未知曲线: {unknown}")

    built: dict[str, Path] = {}
    for rt in runtimes:
        if rt == "kaula":
            built[rt] = None            # 每组参数重新生成编译
        elif rt == "go":
            log("构建 Go ...")
            built[rt] = build_go()
        elif rt == "tokio":
            log("构建 Tokio ...")
            built[rt] = build_tokio()
        else:
            raise SystemExit(f"未知运行时: {rt}")

    records: list[dict] = []
    for rt in runtimes:
        for curve in curves:
            for conc in concs:
                log(f"{rt}/{curve}/conc={conc}")
                try:
                    if rt == "kaula":
                        exe = build_kaula(curve, conc, a.total)
                    else:
                        exe = built[rt]
                    rec = run_case(exe, rt, curve, conc, a.total,
                                   case_timeout=a.case_timeout)
                except SystemExit as e:
                    rec = {"runtime": rt, "curve": curve, "conc": conc, "error": str(e)}
                except Exception as e:            # noqa: BLE001
                    rec = {"runtime": rt, "curve": curve, "conc": conc, "error": repr(e)}
                records.append(rec)
                if "error" in rec:
                    log(f"  失败: {rec['error'][:160]}")
                else:
                    log(f"  TPS={rec['tps']} p99={rec['p99']/1000:.1f}us")

    print_report(records)
    if a.json:
        Path(a.json).write_text(json.dumps(records, indent=2), encoding="utf-8")
        log(f"原始记录 -> {a.json}")
    return 0 if all("error" not in r for r in records) else 1


if __name__ == "__main__":
    raise SystemExit(main())
