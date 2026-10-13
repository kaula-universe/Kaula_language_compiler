#!/usr/bin/env python3
"""run_service3.py —— 三方真实混合服务基准（吞吐 / P50·P99·P999 / 抗干扰）。

设计纪律（承接 test/bench/TRANSACTION.md 与 FINDINGS.md F10/F12）：
  * **服务与干扰严格串行**，每档独立冷启动，绝不并发跑两个被测服务。
  * **交错执行**：同一档位下三方按 round-robin 轮转，避免「时间序」混入
    「运行时」差异（F10 记录的按运行时分批是本仓踩过的坑）。
  * **重复采样**：每档 N 轮，报中位数 + 轮内极差（spread%）。
    单样本数字一律不引用（本仓实测噪声底 8%~49%）。
  * **正确性前置**：loadgen 独立复刻服务端算法，mismatch>0 该轮作废。
  * **收敛判据**：每 5s 一窗，丢弃前 50% 窗口（预热+爬坡），
    对剩余窗口取中位数；并报窗口间极差以证明已收敛。

干扰档位（逐级加压）：
    baseline         无干扰
    cpu25/cpu50/cpu75 占住 8 / 16 / 24 个核（32 核机器上 25%/50%/75%）
    mem25/mem50/mem75 内存 balloon 256MB / 512MB / 768MB 常驻 + 持续触碰
    net              额外 loopback 大流量（抢占软中断与带宽）

用法：
    python3 run_service3.py --conns 100 --sec 30 --rounds 3 \
        --levels baseline,cpu50,mem50 --out results.json
"""
import argparse
import json
import os
import re
import signal
import socket
import subprocess
import sys
import time
from statistics import median

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.abspath(os.path.join(HERE, "..", "..", ".."))

# 三方服务：名字 -> (可执行文件, 端口)
SERVICES = {
    "kaula": (os.path.join(HERE, "kaula_kl", "order_server"), 8123),
    "go": (os.path.join(HERE, "go", "order_server_go"), 8124),
    "tokio": (os.path.join(HERE, "tokio", "target", "release", "order_server_tokio"), 8126),
}
LOADGEN = "/tmp/svc3/loadgen"
CPUBURN = "/tmp/cpuburn"

NCPU = os.cpu_count() or 32
WORKERS = 4  # 三边统一的 worker 数（TRANSACTION.md §7）


def wait_port(port, tries=80, timeout=0.5):
    for _ in range(tries):
        try:
            s = socket.create_connection(("127.0.0.1", port), timeout=timeout)
            s.close()
            return True
        except OSError:
            time.sleep(0.1)
    return False


def kill_tree(p):
    if p is None:
        return
    try:
        os.killpg(os.getpgid(p.pid), signal.SIGKILL)
    except (OSError, ProcessLookupError):
        try:
            p.kill()
        except Exception:
            pass
    try:
        p.wait(timeout=5)
    except Exception:
        pass


def start_disturber(level):
    """返回 (进程, 说明)。level 形如 baseline / cpu50 / mem50 / net。"""
    if level == "baseline":
        return None, "无"
    if level.startswith("cpu"):
        pct = int(level[3:])
        n = max(1, round(NCPU * pct / 100))
        base = NCPU - n          # 从高编号核开始占，和服务尽量错开
        p = subprocess.Popen([CPUBURN, str(n), str(base)],
                             start_new_session=True,
                             stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        return p, f"CPU 占核 {n}/{NCPU}（{pct}%），核 {base}..{base + n - 1}"
    if level.startswith("mem"):
        pct = int(level[3:])
        mb = round(768 * pct / 100)
        p = subprocess.Popen([sys.executable, os.path.join(HERE, "disturb", "mem.py"), str(mb)],
                             start_new_session=True,
                             stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        return p, f"内存 balloon {mb}MB 常驻 + 持续触碰"
    if level == "net":
        # 两个 iperf 式回环流：用 python 起两个大包发送端，抢 loopback 带宽与软中断
        code = ("import socket,time\n"
                "s=socket.socket();s.connect(('127.0.0.1',8129))\n"
                "buf=b'x'*65536\n"
                "while True:\n"
                "    try: s.sendall(buf)\n"
                "    except OSError:\n"
                "        s=socket.socket();s.connect(('127.0.0.1',8129))\n")
        sink = ("import socket\n"
                "s=socket.socket();s.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1)\n"
                "s.bind(('127.0.0.1',8129));s.listen(8)\n"
                "while True:\n"
                "    c,_=s.accept()\n"
                "    while True:\n"
                "        d=c.recv(1<<16)\n"
                "        if not d: break\n")
        sp = subprocess.Popen([sys.executable, "-c", sink], start_new_session=True,
                              stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        time.sleep(0.5)
        procs = [sp]
        for _ in range(2):
            procs.append(subprocess.Popen([sys.executable, "-c", code], start_new_session=True,
                                          stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL))
        return procs, "loopback 大流量 ×2（抢带宽与软中断）"
    raise ValueError(f"未知干扰档位: {level}")


def parse_windows(out):
    rows = []
    total = None
    for ln in out.splitlines():
        if ln.startswith("TOTAL"):
            total = ln
            continue
        parts = ln.split(",")
        if len(parts) == 8:
            try:
                rows.append({
                    "win": int(parts[0]), "tps": float(parts[1]),
                    "p50": int(parts[2]), "p95": int(parts[3]),
                    "p99": int(parts[4]), "p999": int(parts[5]),
                    "mismatch": int(parts[6]), "errors": int(parts[7]),
                })
            except ValueError:
                pass
    return rows, total


def run_once(name, exe, port, level, conns, sec, win, rate=0):
    """跑一次：启动服务 -> 注入干扰 -> 压测 -> 收敛 -> 收尾。

    rate>0 时走 loadgen 的定速（开环）模式：以固定速率发送，
    回答「每秒 N 请求负载下各服务表现如何」。
    """
    if not os.path.exists(exe):
        return {"error": f"二进制不存在: {exe}"}

    env = dict(os.environ)
    env["SVC_WORKERS"] = str(WORKERS)
    if name == "go":
        env["SVC_PORT"] = str(port)
    elif name == "tokio":
        env["SVC_PORT"] = str(port)

    sp = subprocess.Popen([exe], env=env, start_new_session=True,
                          stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    dp = None
    try:
        if not wait_port(port):
            return {"error": f"服务未就绪: {name} :{port}"}
        dp, desc = start_disturber(level)
        time.sleep(2.5)  # 干扰就绪 + 服务预热
        r = subprocess.run([LOADGEN, f"127.0.0.1:{port}", str(conns), str(sec), str(win)]
                           + ([str(rate)] if rate > 0 else []),
                           capture_output=True, text=True, timeout=sec + 120)
        rows, total = parse_windows(r.stdout)
        if not rows:
            return {"error": f"loadgen 无输出: {r.stdout[-300:]} {r.stderr[-300:]}"}

        # 正确性门槛：任何 mismatch/errors 该轮作废。
        # 注意：loadgen 窗口行的 errors 字段恒为 0（历史输出格式），
        # 真实 errors 只在 TOTAL 行——两处都查，避免「连接被杀」被漏检。
        mis = sum(x["mismatch"] for x in rows)
        err = sum(x["errors"] for x in rows)
        if total:
            m = re.search(r"mismatch=(\d+)", total)
            if m:
                mis = max(mis, int(m.group(1)))
            m = re.search(r"errors=(\d+)", total)
            if m:
                err = max(err, int(m.group(1)))
        if mis > 0 or err > 0:
            return {"error": f"正确性失败 mismatch={mis} errors={err}", "disturb_desc": desc}

        # 收敛段：丢前 50% 窗口
        n = len(rows)
        steady = rows[n // 2:] if n > 2 else rows
        keys = ("tps", "p50", "p95", "p99", "p999")
        vals = {k: [x[k] for x in steady] for k in keys}
        out = {k: median(vals[k]) for k in keys}
        # 轮内极差（离散度）：证明是否收敛
        out["spread_pct"] = {}
        for k in keys:
            v = vals[k]
            m = median(v) or 1
            out["spread_pct"][k] = round((max(v) - min(v)) / m * 100, 1)
        out["windows"] = len(rows)
        out["steady_windows"] = len(steady)
        out["disturb_desc"] = desc
        out["raw"] = rows
        out["total"] = total
        return out
    finally:
        for p in (dp if isinstance(dp, list) else [dp]) if dp else []:
            kill_tree(p)
        kill_tree(sp)
        time.sleep(0.6)  # 端口回收


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--conns", type=int, default=100)
    ap.add_argument("--rate", type=int, default=0,
                    help="定速模式速率（req/s，开环令牌桶）；0=默认闭环模式")
    ap.add_argument("--sec", type=int, default=30, help="每档压测秒数（不含 3s 预热）")
    ap.add_argument("--win", type=int, default=5, help="采样窗口秒数")
    ap.add_argument("--rounds", type=int, default=3, help="每档重复轮数")
    ap.add_argument("--levels", default="baseline,cpu25,cpu50,cpu75,mem25,mem50,mem75,net")
    ap.add_argument("--only", default="", help="只跑指定运行时（逗号分隔）")
    ap.add_argument("--out", default=os.path.join(HERE, "results.json"))
    args = ap.parse_args()

    levels = [x for x in args.levels.split(",") if x]
    names = [x for x in args.only.split(",") if x] or list(SERVICES)

    # 交错顺序：每轮内轮转运行时（消除时间序 → 运行时的混淆）
    plan = []
    for lv in levels:
        for rd in range(args.rounds):
            for nm in names:
                plan.append((lv, rd, nm))

    results = {}
    print(f"三方服务基准：{len(names)} 运行时 × {len(levels)} 档 × {args.rounds} 轮 "
          f"= {len(plan)} 次运行，每次 {args.sec}s + 3s 预热", flush=True)
    print(f"并发连接={args.conns} worker={WORKERS} 窗口={args.win}s "
          f"速率={'定速 %d req/s（开环）' % args.rate if args.rate > 0 else '闭环（TPS 为测得结果）'}\n",
          flush=True)

    for i, (lv, rd, nm) in enumerate(plan, 1):
        exe, port = SERVICES[nm]
        t0 = time.time()
        r = run_once(nm, exe, port, lv, args.conns, args.sec, args.win, args.rate)
        dt = time.time() - t0
        results.setdefault(lv, {}).setdefault(nm, []).append(r)
        if "error" in r:
            print(f"[{i}/{len(plan)}] {lv:9s} {nm:6s} ✗ {r['error']}  ({dt:.0f}s)", flush=True)
        else:
            off = ""
            if r.get("total") and "offered=" in r["total"]:
                # 定速模式：offered 为客户端实际发出的请求数；
                # offered≈ok+在途 才说明客户端没被服务拖成瓶颈
                o = int(r["total"].split("offered=")[1].split()[0])
                off = f" offered={o}"
            print(f"[{i}/{len(plan)}] {lv:9s} {nm:6s} "
                  f"TPS={r['tps']:>8.0f} p50={r['p50']:>7.0f} p99={r['p99']:>8.0f} "
                  f"p999={r['p999']:>9.0f}us  spread(tps)={r['spread_pct']['tps']:>5.1f}%{off}  "
                  f"({dt:.0f}s)", flush=True)

    with open(args.out, "w") as f:
        json.dump(results, f, indent=1)
    print(f"\n原始结果 -> {args.out}")
    summarize(results, levels, names)


def agg(runs, key):
    vs = [r[key] for r in runs if "error" not in r]
    return median(vs) if vs else None


def summarize(results, levels, names):
    print("\n" + "=" * 96)
    print("收敛段中位数（跨轮）")
    print("=" * 96)
    for lv in levels:
        print(f"\n--- {lv} ---")
        desc = ""
        for nm in names:
            runs = results.get(lv, {}).get(nm, [])
            ok = [r for r in runs if "error" not in r]
            if ok:
                desc = ok[0].get("disturb_desc", "")
                break
        if desc:
            print(f"    干扰: {desc}")
        print(f"    {'runtime':8s} {'TPS':>10s} {'p50':>7s} {'p99':>8s} {'p999':>9s} "
              f"{'spread':>8s} {'n_ok':>5s}")
        base = {}
        for nm in names:
            runs = results.get(lv, {}).get(nm, [])
            ok = [r for r in runs if "error" not in r]
            if not ok:
                print(f"    {nm:8s} {'—':>10s}   (全部失败)")
                continue
            tps = agg(ok, "tps"); p50 = agg(ok, "p50")
            p99 = agg(ok, "p99"); p999 = agg(ok, "p999")
            sp = max(r["spread_pct"]["tps"] for r in ok)
            base[nm] = {"tps": tps, "p50": p50, "p99": p99, "p999": p999}
            print(f"    {nm:8s} {tps:>10.0f} {p50:>7.0f} {p99:>8.0f} {p999:>9.0f} "
                  f"{sp:>7.1f}% {len(ok):>5d}")

    # 退化倍率
    print("\n" + "=" * 96)
    print("抗干扰退化倍率（相对同运行时 baseline；TPS 越低越差，延迟越高越差）")
    print("=" * 96)
    print(f"    {'level':9s} {'runtime':8s} {'TPS×':>8s} {'p50×':>8s} {'p99×':>8s} {'p999×':>8s}")
    for lv in levels:
        if lv == "baseline":
            continue
        for nm in names:
            b = results.get("baseline", {}).get(nm, [])
            c = results.get(lv, {}).get(nm, [])
            bo = [r for r in b if "error" not in r]
            co = [r for r in c if "error" not in r]
            if not bo or not co:
                print(f"    {lv:9s} {nm:8s}   —")
                continue
            bt, ct = agg(bo, "tps"), agg(co, "tps")
            bp50, cp50 = agg(bo, "p50"), agg(co, "p50")
            bp99, cp99 = agg(bo, "p99"), agg(co, "p99")
            bp999, cp999 = agg(bo, "p999"), agg(co, "p999")
            f = lambda a, b_: (b_ / a) if a else float("nan")
            print(f"    {lv:9s} {nm:8s} {f(bt, ct):>8.2f} {f(bp50, cp50):>8.2f} "
                  f"{f(bp99, cp99):>8.2f} {f(bp999, cp999):>8.2f}")


if __name__ == "__main__":
    main()
