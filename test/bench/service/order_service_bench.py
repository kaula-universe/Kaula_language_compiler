#!/usr/bin/env python3
"""order_service_bench.py —— 真实服务持续压测：Kaula vs Go（串行，防 CPU 争抢）。

每档并发：预热 3s + 压测 DURATION s（窗口 5s），服务端跑完后取后 50%
窗口作为收敛段，输出收敛 TPS 与延迟分位。两服务严格串行执行。
结果 JSON -> /tmp/order_bench_<tag>.json
"""
import json
import os
import signal
import socket
import subprocess
import sys
import time

SVC_DIR = "/home/user/Doubao/chats/38445387077017602/kaula/test/bench/service"
LOADGEN = "/tmp/loadgen"
K_SERVER = "/tmp/kaula_order_server"
G_SERVER = "/tmp/order_server_go"
DURATION = 90      # 压测秒数
WINDOW = 5         # 采样窗口秒数

def run_loadgen(addr, conns, tag):
    out = subprocess.run([LOADGEN, addr, str(conns), str(DURATION), str(WINDOW)],
                         capture_output=True, text=True, timeout=DURATION + 60)
    rows = []
    for ln in out.stdout.strip().splitlines():
        p = ln.split(",")
        if len(p) != 6:
            continue
        rows.append({"win": int(p[0]), "tps": float(p[1]), "p50": int(p[2]),
                     "p95": int(p[3]), "p99": int(p[4]), "p999": int(p[5])})
    return rows

def median(vals):
    if not vals:
        return 0.0
    s = sorted(vals)
    return s[len(s) // 2]

def main():
    conns = int(sys.argv[1]) if len(sys.argv) > 1 else 100
    tag = sys.argv[2] if len(sys.argv) > 2 else "default"

    procs = []
    result = {}
    for name, cmd, port in (("kaula", [K_SERVER], 8123), ("go", [G_SERVER], 8124)):
        print(f"[{tag}] {name}: 启动服务 + 压测 conns={conns} {DURATION}s", flush=True)
        p = subprocess.Popen(cmd, stderr=subprocess.DEVNULL, start_new_session=True)
        procs.append(p)
        # 等端口就绪（最多 10s）
        for _ in range(50):
            try:
                with socket.create_connection(("127.0.0.1", port), timeout=0.3):
                    break
            except OSError:
                time.sleep(0.2)
        rows = run_loadgen(f"127.0.0.1:{port}", conns, name)
        os.killpg(os.getpgid(p.pid), signal.SIGTERM)
        try:
            p.wait(timeout=5)
        except subprocess.TimeoutExpired:
            os.killpg(os.getpgid(p.pid), signal.SIGKILL)
            p.wait()
        # 收敛段：后 50% 窗口
        n = len(rows)
        steady = rows[n // 2:] if n > 2 else rows
        def med(key):
            return median([r[key] for r in steady])
        result[name] = {
            "conns": conns,
            "windows": rows,
            "steady_windows": n // 2,
            "tps": med("tps"),
            "p50_us": med("p50"),
            "p95_us": med("p95"),
            "p99_us": med("p99"),
            "p999_us": med("p999"),
        }
        print(f"[{tag}] {name}: 收敛 TPS={result[name]['tps']:.0f} "
              f"p50={result[name]['p50_us']}us p99={result[name]['p99_us']}us "
              f"p999={result[name]['p999_us']}us", flush=True)

    path = f"/tmp/order_bench_{tag}.json"
    with open(path, "w") as f:
        json.dump(result, f, indent=1)
    k, g = result["kaula"], result["go"]
    print(f"[{tag}] 倍率 TPS: Kaula/Go = {k['tps']/g['tps']:.3f}  "
          f"p99: {k['p99_us']/g['p99_us']:.3f}  p999: {k['p999_us']/g['p999_us']:.3f}")
    print(f"[{tag}] 原始记录 -> {path}")

if __name__ == "__main__":
    main()
