#!/usr/bin/env python3
"""抗干扰实验：CPU 争抢 / 内存压力注入下，Kaula vs Go 尾延迟退化对比。
每遍：启动服务 → (注入干扰) → loadgen 30s(5s/窗) → 取后 50% 窗口收敛。
"""
import socket, subprocess, time, os, signal, sys, json

def wait_port(port, tries=60):
    for _ in range(tries):
        try:
            s = socket.create_connection(('127.0.0.1', port), timeout=0.5)
            s.close(); return True
        except OSError:
            time.sleep(0.1)
    return False

def start_server(cmd, port):
    p = subprocess.Popen(cmd, stderr=subprocess.DEVNULL, start_new_session=True)
    ok = wait_port(port)
    if not ok:
        p.kill()
        raise RuntimeError(f"port {port} not ready: {cmd[0]}")
    return p

def start_disturber(kind):
    if kind == 'cpu':
        code = "import time\nwhile True: x = 1 + 1  # busy-loop 占一核"
        return subprocess.Popen(['python3', '-c', code], start_new_session=True)
    if kind == 'mem':
        code = ("import time\n"
                "while True:\n"
                "    b = [bytearray(8*1024*1024) for _ in range(96)]  # 768MB 分配\n"
                "    time.sleep(0.15)  # 短暂持有后整体释放 → GC/分配压力\n")
        return subprocess.Popen(['python3', '-c', code], start_new_session=True)
    return None

def parse_windows(out):
    rows = []
    for ln in out.splitlines():
        try:
            parts = ln.split(',')
            if len(parts) == 6:
                rows.append([int(parts[0]), int(parts[1]), int(parts[2]),
                             int(parts[3]), int(parts[4]), int(parts[5])])
        except ValueError:
            pass
    return rows

def bench(name, server, port, kind):
    sp = start_server(server, port)
    d = start_disturber(kind)
    time.sleep(2.5)  # 服务预热 + 干扰就绪
    r = subprocess.run(['/tmp/loadgen', f'127.0.0.1:{port}', '100', '30', '5'],
                       capture_output=True, text=True, timeout=90)
    rows = parse_windows(r.stdout)
    half = rows[len(rows)//2:]
    n = len(half)
    tps = sorted(x[1] for x in half)[n//2]
    p50 = sorted(x[2] for x in half)[n//2]
    p99 = sorted(x[3] for x in half)[n//2]
    p999 = sorted(x[4] for x in half)[n//2]
    for p in (sp, d):
        if p:
            try: os.killpg(os.getpgid(p.pid), signal.SIGKILL)
            except OSError: pass
    print(f"[{name}] {kind}: TPS={tps} p50={p50} p99={p99} p999={p999} (windows={len(rows)})")
    return {'name': name, 'kind': kind, 'tps': tps, 'p50': p50, 'p99': p99, 'p999': p999}

KAULA = ['/tmp/kaula_order_server']
GO = ['/tmp/order_server_go']

def main():
    results = []
    for kind in ('baseline', 'cpu', 'mem'):
        for name, server, port in (('kaula', KAULA, 8123), ('go', GO, 8124)):
            results.append(bench(name, server, port, kind))
    json.dump(results, open('/tmp/disturb_results.json', 'w'), indent=1)
    # 汇总退化
    print("\n=== 退化汇总（vs baseline p99/p999 变化） ===")
    for name in ('kaula', 'go'):
        b = next(x for x in results if x['name'] == name and x['kind'] == 'baseline')
        for kind in ('cpu', 'mem'):
            r = next(x for x in results if x['name'] == name and x['kind'] == kind)
            print(f"{name} [{kind}]: p99 {b['p99']}→{r['p99']} ({r['p99']/b['p99']:.2f}x) "
                  f"p999 {b['p999']}→{r['p999']} ({r['p999']/b['p999']:.2f}x) "
                  f"TPS {b['tps']}→{r['tps']} ({r['tps']/b['tps']:.2f}x)")

if __name__ == '__main__':
    main()
