#!/usr/bin/env python3
"""
加压定位内存泄漏 + 内核/CPU 指标对比（Kaula CONC vs Go）

设计目标（回答两个问题）：
  1. 泄漏随什么变量线性增长？（请求数 / 连接数 / 时长 / 速率）
     → 步骤一「阶梯加压」：固定连接，递增时长（→ 递增请求数）
     → 步骤二「连接扫描」：固定时长，递增连接数
     若 RSS 增量 ∝ 请求数 → 每请求泄漏；∝ 连接数 → 每连接泄漏。
  2. Kaula 与 Go 在上述各口径下的内核态/用户态成本对比。

采集指标（每个采样点）：
  - 进程：RSS、utime、stime、nvcswch、nivcswch、minflt/majflt（/proc/<pid>/stat）
  - 内核：ss -s（socket/TCP 汇总）、nstat 关键 TCP 计数器
  口径与 loadgen 一致（RTT 由 loadgen 侧测，mismatch 校验）。

用法：
  python3 stress_leak_probe.py --svc kaula --port 8123 --conns 200 --secs 60,120,180
  python3 stress_leak_probe.py --svc go --port 8124 --conns 200 --secs 60,120,180
  python3 stress_leak_probe.py --compare   # 同参数跑两边并输出对比
"""

import argparse
import json
import os
import re
import subprocess
import sys
import time
from pathlib import Path

HERE = Path(__file__).parent
LOADGEN = "/tmp/svc3/loadgen"
NCPU = os.cpu_count() or 32

# 服务可执行文件与端口
SVC = {
    "kaula": {"exe": "/tmp/svc3/multi/order_server", "port": 8123, "env_workers": "4"},
    "go":    {"exe": os.path.join(HERE, "go", "order_server_go"), "port": 8124, "env_workers": "4"},
}


def sh(cmd):
    try:
        r = subprocess.run(cmd, shell=True, capture_output=True, text=True, timeout=30)
        return r.stdout.strip()
    except Exception:
        return ""


def wait_port(port, tries=100, timeout=0.3):
    for _ in range(tries):
        r = subprocess.run(["bash", "-c", f"exec 3<>/dev/tcp/127.0.0.1/{port}"],
                           capture_output=True)
        if r.returncode == 0:
            try:
                subprocess.run(["bash", "-c", "exec 3>&-"], capture_output=True)
            except Exception:
                pass
            return True
        time.sleep(timeout)
    return False


def read_proc(pid):
    """从 /proc/<pid>/stat 读 RSS/utime/stime/cswch/faults。"""
    try:
        with open(f"/proc/{pid}/stat") as f:
            parts = f.read().split()
        # utime=14, stime=15 (1-based→索引13,14), minflt=10,majflt=12,
        # nvcsw=42, nivcsw=43
        return {
            "rss_kb": int(parts[23]) * 4,  # rss 页数 × 4KB
            "utime": int(parts[13]),
            "stime": int(parts[14]),
            "minflt": int(parts[9]),
            "majflt": int(parts[11]),
            "nvcswch": int(parts[41]),
            "nivcswch": int(parts[42]),
        }
    except Exception:
        return {}


def ss_summary():
    """ss -s 汇总：socket 总数 / TCP 各状态。"""
    out = sh("ss -s")
    d = {}
    m = re.search(r"Total:\s*(\d+)", out)
    if m: d["sockets_total"] = int(m.group(1))
    m = re.search(r"TCP:\s*(\d+).*?estab\s*(\d+)", out, re.S)
    if m:
        d["tcp_total"] = int(m.group(1)); d["tcp_estab"] = int(m.group(2))
    for state in ["closed", "orphaned", "timewait", "synrecv", "timewait orphaned"]:
        m = re.search(rf"{state}\s+(\d+)", out)
        if m: d["tcp_" + state.replace(" ", "_")] = int(m.group(1))
    return d


def nstat_key():
    """nstat 关键 TCP 计数器（累积值；用于算增量）。"""
    keys = ["TcpActiveOpens", "TcpPassiveOpens", "TcpInSegs", "TcpOutSegs",
            "TcpRetransSegs", "TcpExtTCPTimeWaitOverflow", "TcpExtListenOverflows",
            "TcpExtListenDrops", "TcpExtTCPBacklogDrop", "TcpCurrEstab"]
    out = sh("nstat -a")
    d = {}
    for line in out.splitlines():
        parts = line.split()
        if len(parts) >= 2 and parts[0] in keys:
            try: d[parts[0]] = int(parts[1])
            except ValueError: pass
    return d


def find_pid(port):
    """按监听端口找服务进程 PID。"""
    out = sh(f"ss -tlnp 2>/dev/null | grep ':{port} '")
    m = re.search(r"pid=(\d+)", out)
    return int(m.group(1)) if m else None


def start_svc(name):
    cfg = SVC[name]
    env = dict(os.environ)
    env["SVC_WORKERS"] = cfg["env_workers"]
    env["SVC_PORT"] = str(cfg["port"])
    log = f"/tmp/svc3/probe_{name}.log"
    with open(log, "w") as lf:
        p = subprocess.Popen([cfg["exe"]], env=env, stdout=lf, stderr=lf,
                             start_new_session=True)
    ok = wait_port(cfg["port"])
    return p, ok


def stop_svc(p):
    try:
        p.terminate()
        p.wait(timeout=5)
    except Exception:
        try: p.kill()
        except Exception: pass


def sample(pid):
    d = {"proc": read_proc(pid), "ss": ss_summary(), "nstat": nstat_key()}
    return d


def run_single(svc_name, conns, secs, win=10, rate=0):
    """跑一轮 loadgen，返回该轮的 totals + 前后采样差值。"""
    cfg = SVC[svc_name]
    port = cfg["port"]
    pid = find_pid(port)
    if not pid:
        return {"error": f"{svc_name} 未监听 {port}"}

    before = sample(pid)
    t0 = time.time()
    cmd = [LOADGEN, f"127.0.0.1:{port}", str(conns), str(secs), str(win)]
    if rate > 0:
        cmd += [str(rate), str(rate)]
    r = subprocess.run(cmd, capture_output=True, text=True, timeout=secs + 60)
    dur = time.time() - t0
    after = sample(pid)

    out = r.stdout.strip().splitlines()
    total = out[-1] if out else ""
    ok = int(re.search(r"ok=(\d+)", total).group(1)) if "ok=" in total else 0
    mis = int(re.search(r"mismatch=(\d+)", total).group(1)) if "mismatch=" in total else 0
    err = int(re.search(r"errors=(\d+)", total).group(1)) if "errors=" in total else 0

    def d(k): return after["proc"].get(k, 0) - before["proc"].get(k, 0)

    return {
        "svc": svc_name, "conns": conns, "secs": secs, "rate": rate,
        "wall_s": round(dur, 1), "ok": ok, "tps": round(ok / dur, 1),
        "mismatch": mis, "errors": err,
        "rss_before_mb": round(before["proc"].get("rss_kb", 0) / 1024, 1),
        "rss_after_mb": round(after["proc"].get("rss_kb", 0) / 1024, 1),
        "rss_delta_mb": round(d("rss_kb") / 1024, 1),
        "rss_per_req_b": round(d("rss_kb") * 1024 / ok, 1) if ok else 0,
        "utime_dcs": d("utime"), "stime_dcs": d("stime"),
        "cpu_user_pct": round(d("utime") / 100.0 / dur * 100, 1),
        "cpu_sys_pct": round(d("stime") / 100.0 / dur * 100, 1),
        "minflt": d("minflt"), "majflt": d("majflt"),
        "nvcswch": d("nvcswch"), "nivcswch": d("nivcswch"),
        "ss_estab_after": after["ss"].get("tcp_estab", 0),
        "ss_tw_after": after["ss"].get("tcp_timewait", 0),
        "nstat_retrans": after["nstat"].get("TcpRetransSegs", 0) - before["nstat"].get("TcpRetransSegs", 0),
        "nstat_listen_drops": after["nstat"].get("TcpExtListenDrops", 0) - before["nstat"].get("TcpExtListenDrops", 0),
    }


def ladder(svc_name, conns, secs_list, win, rate):
    """阶梯加压：逐级增加时长（→ 请求数），看 RSS 增量是否线性。"""
    res = []
    print(f"\n{'='*100}\n【{svc_name}】阶梯加压 — 固定 {conns} 连接，时长 {secs_list}\n{'='*100}")
    hdr = f"{'secs':>5} {'ok':>12} {'tps':>9} {'ΔRSS(MB)':>9} {'B/req':>7} {'u%':>6} {'s%':>6} {'cswch':>10} {'minflt':>10} {'err':>6}"
    print(hdr); print("-" * len(hdr))
    for s in secs_list:
        r = run_single(svc_name, conns, s, win, rate)
        if "error" in r:
            print(r["error"]); continue
        res.append(r)
        print(f"{s:>5} {r['ok']:>12,} {r['tps']:>9,.0f} {r['rss_delta_mb']:>9.1f} "
              f"{r['rss_per_req_b']:>7.1f} {r['cpu_user_pct']:>6.1f} {r['cpu_sys_pct']:>6.1f} "
              f"{r['nvcswch']+r['nivcswch']:>10,} {r['minflt']:>10,} {r['errors']:>6}")
    return res


def conn_sweep(svc_name, conns_list, secs, win, rate):
    """连接扫描：固定时长，递增连接数，看 RSS 是否随连接数跳变。"""
    res = []
    print(f"\n{'='*100}\n【{svc_name}】连接扫描 — 固定 {secs}s，连接数 {conns_list}\n{'='*100}")
    hdr = f"{'conns':>6} {'ok':>12} {'tps':>9} {'ΔRSS(MB)':>9} {'B/req':>7} {'B/conn':>8} {'estab':>7} {'err':>6}"
    print(hdr); print("-" * len(hdr))
    for c in conns_list:
        r = run_single(svc_name, c, secs, win, rate)
        if "error" in r:
            print(r["error"]); continue
        r["rss_per_conn_b"] = round(r["rss_delta_mb"] * 1024 / c, 1) if c else 0
        res.append(r)
        print(f"{c:>6} {r['ok']:>12,} {r['tps']:>9,.0f} {r['rss_delta_mb']:>9.1f} "
              f"{r['rss_per_req_b']:>7.1f} {r['rss_per_conn_b']:>8.1f} "
              f"{r['ss_estab_after']:>7} {r['errors']:>6}")
    return res


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--svc", default="kaula", choices=["kaula", "go", "both"])
    ap.add_argument("--conns", type=int, default=200)
    ap.add_argument("--secs", default="30,60,120")
    ap.add_argument("--conns-list", default="50,100,200,400")
    ap.add_argument("--win", type=int, default=10)
    ap.add_argument("--rate", type=int, default=0)
    ap.add_argument("--mode", default="ladder", choices=["ladder", "conns", "post"])
    ap.add_argument("--out", default="/tmp/svc3/probe_result.json")
    args = ap.parse_args()

    secs_list = [int(x) for x in args.secs.split(",")]
    conns_list = [int(x) for x in args.conns_list.split(",")]
    targets = ["kaula", "go"] if args.svc == "both" else [args.svc]

    all_res = {}
    procs = {}
    for t in targets:
        p, ok = start_svc(t)
        procs[t] = p
        print(f"[启动] {t} pid={p.pid} 监听={SVC[t]['port']} ok={ok}")
        time.sleep(2)

    try:
        for t in targets:
            if args.mode == "ladder":
                all_res[t] = ladder(t, args.conns, secs_list, args.win, args.rate)
            elif args.mode == "conns":
                all_res[t] = conn_sweep(t, conns_list, secs_list[0], args.win, args.rate)
            elif args.mode == "post":
                # 停载后回落观察：跑一轮，然后空载采样 60s
                pid = find_pid(SVC[t]["port"])
                r0 = sample(pid)
                rr = run_single(t, args.conns, secs_list[0], args.win, args.rate)
                all_res[t] = rr
                print(f"[{t}] 跑完 {rr.get('ok')} req，ΔRSS={rr.get('rss_delta_mb')}MB "
                      f"({rr.get('rss_per_req_b')} B/req)")
                print(f"  空载回落观察（60s）：")
                for i in range(6):
                    time.sleep(10)
                    cur = read_proc(pid).get("rss_kb", 0) / 1024
                    print(f"    +{10*(i+1):>2}s  RSS={cur:.1f}MB")
    finally:
        for t, p in procs.items():
            stop_svc(p)
        with open(args.out, "w") as f:
            json.dump(all_res, f, indent=2, ensure_ascii=False)
        print(f"\n结果已保存: {args.out}")


if __name__ == "__main__":
    main()
