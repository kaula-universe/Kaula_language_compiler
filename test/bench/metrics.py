#!/usr/bin/env python3
"""跨运行时并发基准 · 指标采集与报告（可比核心）。

与 run_bench.py 的分工：
  run_bench.py  只看 TPS 与延迟分位，采样靠每 50ms 起一个 PowerShell 进程
                （采样开销本身污染测量，且只有 real time，没有 user/sys）。
  metrics.py    加一层进程级外部采样：CPU user/sys、RSS、句柄、线程、
                上下文切换、IO 字节，全部由本进程 ctypes 直调，零进程开销。

设计约束（见 TRANSACTION.md）：
  CPU / 内存必须由 orchestrator 从**外部**采，不用各语言自己的 API ——
  三个运行时自报的口径不同，外部采样才可比。本文件严格遵守：
  所有进程级指标一律从 OS 拿，运行时自报的数一律不进这张表。

指标来源标注：
  workload  由被测程序自己算并写进 RESULT 行（TPS、延迟分位、错误数）
  external  由本文件从 OS 采（CPU、内存、句柄、线程、上下文切换、IO）

用法:
    python test/bench/metrics.py --list
    python test/bench/metrics.py --runtimes kaula,go,tokio
    python test/bench/metrics.py --full              # 阶梯加压 + 长稳
    python test/bench/metrics.py --json out.json
    python test/bench/metrics.py --runtimes kaula --repeat 11   # 重复采样测离散度

测量口径（与 ab_compiler.py 一致，对应 FINDINGS F10）:
  - 本机实测噪声底 8%~49%，单样本对比无效。--repeat N 让同一配置
    (runtime/curve/conc) 重复跑 N 次，报告 tps/p99 的**中位数**作主值，
    并附两种离散度：
      spread% = max/min - 1（轮内极差，「自己跟自己比」的噪声量级）；
      cv%     = 标准差/均值（量级无关，跨配置可比）。
  - 跨配置/跨运行时比较：差值必须大过两边 spread% 才算显著，否则判「噪声内」。
    正式结论建议 --repeat>=11（奇数取中位稳健）；默认 --repeat 1 为旧行为。
  - 跑基准时机器须安静：测量期间不要同时编译/跑别的负载，否则自行灌噪声。
"""
from __future__ import annotations

import argparse
import ctypes
import ctypes.wintypes as wt
import json
import os
import platform
import re
import statistics
import subprocess
import sys
import threading
import time
from dataclasses import dataclass, field
from pathlib import Path

HERE = Path(__file__).resolve().parent
REPO = HERE.parent.parent
IS_WINDOWS = platform.system() == "Windows"

# 可执行文件后缀：Windows 带 .exe，Linux/macOS 无扩展名（与 run_bench 保持一致）。
# 注意 kaulac 在 Linux 产出无后缀的二进制，硬编码 .exe 会找不到产物。
from run_bench import EXE_SUFFIX  # noqa: E402

# 事务常量：三边必须一致（TRANSACTION.md）
EXPECTED_CHECKSUM = 13356499072
MAX_SAMPLES = 2000


def log(msg: str) -> None:
    print(f"[metrics] {msg}", flush=True)


# ==========================================================================
# 指标注册表
# ==========================================================================

@dataclass(frozen=True)
class Metric:
    key: str
    unit: str
    source: str        # workload | external | derived
    desc: str


# 顺序即报表列序。source 决定采集路径，不决定是否可比 ——
# 这里登记的每一项在三边都是同一口径，所以能放进同一张表。
REGISTRY: list[Metric] = [
    # ---- workload：被测程序 RESULT 行 ----
    Metric("tps",         "/s",    "workload", "吞吐（总事务数 / 墙钟）"),
    Metric("p50_ns",      "ns",    "workload", "单事务延迟中位数"),
    Metric("p95_ns",      "ns",    "workload", "P95 延迟"),
    Metric("p99_ns",      "ns",    "workload", "P99 延迟"),
    Metric("p999_ns",     "ns",    "workload", "P99.9 延迟（长尾）"),
    Metric("max_ns",      "ns",    "workload", "最大延迟"),
    Metric("errors",      "count", "workload", "结果与期望不符的事务数"),
    Metric("checksum",    "-",      "workload", "所有返回值之和（三边必须一致）"),
    # ---- external：OS 进程级 ----
    Metric("cpu_user_s",  "s",     "external", "进程 user CPU 时间"),
    Metric("cpu_sys_s",   "s",     "external", "进程 sys（kernel）CPU 时间"),
    Metric("rss_peak_b",  "bytes", "external", "运行期间 RSS 峰值"),
    Metric("rss_end_b",   "bytes", "external", "负载结束时 RSS"),
    Metric("priv_end_b",  "bytes", "external", "负载结束时私有字节"),
    Metric("handles_end", "count", "external", "负载结束时句柄数（Windows 的 fd）"),
    Metric("threads_end", "count", "external", "负载结束时线程数"),
    Metric("ctxsw",       "count", "external", "运行期间上下文切换次数"),
    Metric("io_read_b",   "bytes", "external", "读 IO 字节数"),
    Metric("io_write_b",  "bytes", "external", "写 IO 字节数"),
    Metric("page_faults", "count", "external", "缺页次数"),
    # ---- derived：由上面两组算出，仍在同一口径下 ----
    Metric("tps_per_conc",   "-",    "derived", "每并发度吞吐（越高=固定开销越低）"),
    Metric("p99_over_p50",   "-",    "derived", "长尾抖动（越接近 1 越稳）"),
    Metric("tps_per_cpu_s",  "-",    "derived", "单位 CPU 换来的吞吐"),
    Metric("sys_over_user",  "-",    "derived", "sys/user CPU 比"),
    Metric("ctxsw_per_task", "-",    "derived", "每事务上下文切换"),
    Metric("rss_per_task",   "bytes", "derived", "每事务占用 RSS"),
    Metric("handle_growth",  "count", "derived", "负载后句柄相对负载前净增（>0 疑似泄漏）"),
    Metric("rss_reclaim",    "-",    "derived", "静置期 RSS 回落比例（0~1，越高回收越好）"),
]

METRICS_BY_KEY = {m.key: m for m in REGISTRY}


# ==========================================================================
# 进程采样器
# ==========================================================================

class _WindowsSampler:
    """ctypes 直调 Win32。

    进程范围的 API 逐个验证过（本机）：
      GetProcessTimes / GetProcessMemoryInfo / GetProcessHandleCount /
      GetProcessIoCounters —— 全部可用，且是文档化接口。
      线程数与上下文切换**没有**可用的文档化接口：
        GetProcessNumberOfThreads / GetProcessContextSwitches 不是 kernel32 导出；
        性能计数器 \\System\\Context Switches、\\Thread(*)\\Context Switches
        在本机不存在（PdhAddEnglishCounterW 返回 0xC0000BB9，
        PowerShell Get-Counter 报「找不到指定的文件」）。
      因此走 NtQuerySystemInformation(SystemProcessInformation=5)。
      这是未文档化接口，所以做了一次独立交叉验证：把它的 NumberOfThreads
      与 Toolhelp32 快照逐进程对照，260/260 一致（0 例外），布局可信；
      忙循环 2 秒实测 ctxsw +2930、user time +2.0s，读数合理。
    """

    PROCESS_QUERY_INFORMATION = 0x0400
    PROCESS_VM_READ = 0x0010
    THREAD_SUSPEND_RESUME = 0x0002  # 保留：某些环境 OpenProcess 需要更高权限

    def __init__(self, pid: int) -> None:
        self.pid = pid
        k32 = ctypes.WinDLL("kernel32", use_last_error=True)
        self.k32 = k32
        self.nt = ctypes.WinDLL("ntdll")

        access = self.PROCESS_QUERY_INFORMATION | self.PROCESS_VM_READ
        self.handle = k32.OpenProcess(access, False, pid)
        if not self.handle:
            # 某些进程需要更高权限，退到全权限
            self.handle = k32.OpenProcess(0x1FFFFF, False, pid)
        if not self.handle:
            raise OSError(f"OpenProcess({pid}) failed: {ctypes.get_last_error()}")

        k32.GetProcessTimes.argtypes = [wt.HANDLE, ctypes.POINTER(wt.FILETIME),
                                        ctypes.POINTER(wt.FILETIME),
                                        ctypes.POINTER(wt.FILETIME),
                                        ctypes.POINTER(wt.FILETIME)]
        k32.GetProcessTimes.restype = wt.BOOL
        k32.GetProcessHandleCount.argtypes = [wt.HANDLE, ctypes.POINTER(wt.DWORD)]
        k32.GetProcessHandleCount.restype = wt.BOOL
        k32.GetProcessIoCounters.argtypes = [wt.HANDLE, ctypes.POINTER(_IO_COUNTERS)]
        k32.GetProcessIoCounters.restype = wt.BOOL

        class PMC_EX(ctypes.Structure):
            _fields_ = [
                ("cb", wt.DWORD), ("PageFaultCount", wt.DWORD),
                ("PeakWorkingSetSize", ctypes.c_size_t),
                ("WorkingSetSize", ctypes.c_size_t),
                ("QuotaPeakPagedPoolUsage", ctypes.c_size_t),
                ("QuotaPagedPoolUsage", ctypes.c_size_t),
                ("QuotaPeakNonPagedPoolUsage", ctypes.c_size_t),
                ("QuotaNonPagedPoolUsage", ctypes.c_size_t),
                ("PagefileUsage", ctypes.c_size_t),
                ("PeakPagefileUsage", ctypes.c_size_t),
                ("PrivateUsage", ctypes.c_size_t),
            ]
        self._PMC = PMC_EX
        psapi = ctypes.WinDLL("psapi")
        psapi.GetProcessMemoryInfo.argtypes = [wt.HANDLE, ctypes.POINTER(PMC_EX), wt.DWORD]
        psapi.GetProcessMemoryInfo.restype = wt.BOOL
        self._psapi = psapi

        self.nt.NtQuerySystemInformation.argtypes = [
            ctypes.c_int, ctypes.c_void_p, ctypes.c_ulong,
            ctypes.POINTER(ctypes.c_ulong)]
        self.nt.NtQuerySystemInformation.restype = ctypes.c_long
        self._ntbuf = ctypes.create_string_buffer(4 << 20)
        self._retlen = ctypes.c_ulong()

    def read(self) -> dict | None:
        """返回累计计数器。进程已退出返回 None。"""
        if not self.handle:
            return None
        try:
            c = wt.FILETIME(); e = wt.FILETIME()
            kt = wt.FILETIME(); ut = wt.FILETIME()
            if not self.k32.GetProcessTimes(self.handle, ctypes.byref(c),
                                            ctypes.byref(e), ctypes.byref(kt),
                                            ctypes.byref(ut)):
                return None

            def ft(f: wt.FILETIME) -> int:
                return (f.dwHighDateTime << 32) | f.dwLowDateTime

            p = self._PMC()
            p.cb = ctypes.sizeof(self._PMC)
            self._psapi.GetProcessMemoryInfo(self.handle, ctypes.byref(p),
                                             ctypes.sizeof(self._PMC))

            hd = wt.DWORD()
            self.k32.GetProcessHandleCount(self.handle, ctypes.byref(hd))

            io = _IO_COUNTERS()
            self.k32.GetProcessIoCounters(self.handle, ctypes.byref(io))

            threads, ctxsw = self._query_threads_ctxsw()

            return {
                "cpu_user_s": ft(ut) / 1e7,
                "cpu_sys_s": ft(kt) / 1e7,
                "rss_bytes": int(p.WorkingSetSize),
                "rss_peak_bytes": int(p.PeakWorkingSetSize),
                "private_bytes": int(p.PrivateUsage),
                "page_faults": int(p.PageFaultCount),
                "handles": int(hd.value),
                "threads": threads,
                "ctx_switches": ctxsw,
                "io_read_bytes": int(io.ReadTransferCount),
                "io_write_bytes": int(io.WriteTransferCount),
            }
        except OSError:
            return None

    def _query_threads_ctxsw(self) -> tuple[int, int]:
        """线程数与上下文切换总数，来自同一次 NtQuerySystemInformation。"""
        st = self.nt.NtQuerySystemInformation(5, self._ntbuf, len(self._ntbuf),
                                              ctypes.byref(self._retlen))
        if st & 0xFFFFFFFF == 0xC0000004:  # STATUS_INFO_LENGTH_MISMATCH
            self._ntbuf = ctypes.create_string_buffer(self._retlen.value + 65536)
            st = self.nt.NtQuerySystemInformation(5, self._ntbuf,
                                                  len(self._ntbuf),
                                                  ctypes.byref(self._retlen))
        if st != 0:
            return 0, 0

        # 结构布局（与 nt_probe2.py 的交叉验证一致）：
        #   NextEntryOffset 0, NumberOfThreads 4, UniqueProcessId 80,
        #   Threads 变长数组起点 256，每项 80 字节，ContextSwitches 在项内偏移 64。
        buf = self._ntbuf
        off = 0
        while True:
            nthreads = int.from_bytes(buf[off + 4:off + 8], "little")
            pid = int.from_bytes(buf[off + 80:off + 88], "little")
            if pid == self.pid:
                total = 0
                base = off + 256
                for i in range(nthreads):
                    o = base + i * 80 + 64
                    total += int.from_bytes(buf[o:o + 4], "little")
                return nthreads, total
            nxt = int.from_bytes(buf[off:off + 4], "little")
            if nxt == 0:
                return 0, 0
            off += nxt


class _IO_COUNTERS(ctypes.Structure):
    _fields_ = [(n, ctypes.c_uint64) for n in (
        "ReadOperationCount", "WriteOperationCount", "OtherOperationCount",
        "ReadTransferCount", "WriteTransferCount", "OtherTransferCount")]


class _LinuxSampler:
    """/proc 采样。本机是 Windows，此路径未经实跑验证。"""

    def __init__(self, pid: int) -> None:
        self.pid = pid
        self._page = os.sysconf("SC_PAGE_SIZE")

    def read(self) -> dict | None:
        try:
            stat = Path(f"/proc/{self.pid}/stat").read_text()
            # comm 可能含括号，从最后一个 ')' 之后切
            rest = stat.rsplit(")", 1)[1].split()
            # rest[0]=state, ... utime=rest[11], stime=rest[12] (0-based after comm)
            utime = int(rest[11]) / os.sysconf("SC_CLK_TCK")
            stime = int(rest[12]) / os.sysconf("SC_CLK_TCK")
            minflt = int(rest[7])

            status = Path(f"/proc/{self.pid}/status").read_text()
            kv = {}
            for line in status.splitlines():
                if ":" in line:
                    k, v = line.split(":", 1)
                    kv[k.strip()] = v.strip()
            rss = int(kv.get("VmRSS", "0 kB").split()[0]) * 1024
            rss_peak = int(kv.get("VmHWM", "0 kB").split()[0]) * 1024
            priv = int(kv.get("VmData", "0 kB").split()[0]) * 1024
            threads = int(kv.get("Threads", "0"))

            io_rd = io_wr = 0
            try:
                for line in Path(f"/proc/{self.pid}/io").read_text().splitlines():
                    if line.startswith("read_bytes:"):
                        io_rd = int(line.split()[1])
                    elif line.startswith("write_bytes:"):
                        io_wr = int(line.split()[1])
            except OSError:
                pass

            handles = 0
            try:
                handles = len(os.listdir(f"/proc/{self.pid}/fd"))
            except OSError:
                pass

            return {
                "cpu_user_s": utime, "cpu_sys_s": stime,
                "rss_bytes": rss, "rss_peak_bytes": rss_peak,
                "private_bytes": priv, "page_faults": minflt,
                "handles": handles, "threads": threads,
                "ctx_switches": 0,  # /proc 不提供每进程自愿/非自愿切换总数
                "io_read_bytes": io_rd, "io_write_bytes": io_wr,
            }
        except (OSError, ValueError, IndexError):
            return None


def make_sampler(pid: int):
    return _WindowsSampler(pid) if IS_WINDOWS else _LinuxSampler(pid)


# ==========================================================================
# 运行 + 采样
# ==========================================================================

RESULT_RE = re.compile(r"(\w+)=(-?\d+)")
PHASE_RE = re.compile(r"^PHASE\s+(\w+)")


@dataclass
class Sample:
    t: float
    d: dict


@dataclass
class CaseRecord:
    runtime: str
    curve: str
    conc: int
    total: int
    values: dict = field(default_factory=dict)
    phases: dict = field(default_factory=dict)
    series: list[Sample] = field(default_factory=list)
    error: str | None = None
    wall_s: float = 0.0
    # 该用例一共试了几次、其中几次是挂死（超时强杀）。
    # F1 是概率性死锁，一个用例可能「试 5 次成 2 次」——
    # 只报成功那次的数字而不报挂死率，等于把 bug 藏起来。
    attempts: int = 1
    hangs: int = 0
    # 重复采样（--repeat N）：本配置实际成功跑了几次、关键指标的离散度。
    n_repeat: int = 1


def run_case(exe: Path, args: list[str], runtime: str, curve: str,
             conc: int, total: int, interval: float = 0.05,
             ctxsw_interval: float = 0.5, timeout: float = 600.0,
             stop_loss: dict | None = None) -> CaseRecord:
    """跑一个用例，边跑边采样。

    两级采样：
      便宜的（CPU/RSS/句柄/IO）每 interval 秒一次；
      NtQuerySystemInformation 要扫全进程表（~1ms），每 ctxsw_interval 秒一次。
    ctx_switches 单调递增，稀疏采样不影响总量，只影响「何时发生」的精度。
    """
    rec = CaseRecord(runtime=runtime, curve=curve, conc=conc, total=total)
    full_env = dict(os.environ)
    cwd = str(exe.parent)

    cmd = [str(exe)] + args
    t_start = time.perf_counter()
    # 不用 text=True：Windows 上它按 locale（GBK）解码，
    # 而子进程输出的是 UTF-8，中文行会直接 UnicodeDecodeError。
    # 按字节读、自己解 UTF-8，坏字节用替换符兜住。
    try:
        proc = subprocess.Popen(cmd, stdout=subprocess.PIPE,
                                stderr=subprocess.STDOUT, env=full_env,
                                cwd=cwd)
    except OSError as e:
        rec.error = f"启动失败: {e}"
        return rec

    sampler = None
    for _ in range(50):                 # 等进程起来
        try:
            sampler = make_sampler(proc.pid)
            break
        except OSError:
            time.sleep(0.02)
    if sampler is None:
        proc.wait()
        rec.error = "无法附加采样器"
        return rec

    lines: list[str] = []
    # 阶段时间戳必须在**行到达时**记，不能等进程退出后再统一补 ——
    # 后者会让所有阶段共享同一个退出时刻，rss_reclaim 永远算成 0。
    phase_marks: dict[str, float] = {}
    lock = threading.Lock()
    last_full = 0.0

    def pump_stdout() -> None:
        assert proc.stdout is not None
        for raw in proc.stdout:
            line = raw.decode("utf-8", "replace").rstrip("\r\n")
            m = PHASE_RE.match(line)
            if m:
                with lock:
                    phase_marks[m.group(1)] = time.perf_counter()
            lines.append(line)

    thr = threading.Thread(target=pump_stdout, daemon=True)
    thr.start()

    baseline: dict | None = None
    pre_load: dict | None = None
    peak_rss = 0
    breach: str | None = None

    while proc.poll() is None:
        now = time.perf_counter()
        if now - t_start > timeout:
            rec.error = f"超时 {timeout}s，强杀"
            proc.kill()
            break

        snap = sampler.read()
        if snap is not None:
            if baseline is None:
                baseline = dict(snap)
            peak_rss = max(peak_rss, snap.get("rss_bytes", 0))
            rec.series.append(Sample(now, snap))

            # 负载前基线：PHASE load_start 到达后的第一个样。
            # 比「进程一起来就采」准 —— 运行时初始化（Go runtime 建线程、
            # Tokio 建 worker 池）本身就会抬高句柄数，不该算作泄漏。
            with lock:
                started = "load_start" in phase_marks
            if started and pre_load is None:
                pre_load = dict(snap)

            if stop_loss:
                if stop_loss.get("max_handles") and \
                        snap["handles"] >= stop_loss["max_handles"]:
                    breach = f"句柄 {snap['handles']} >= {stop_loss['max_handles']}"
                elif stop_loss.get("max_rss_mb") and \
                        snap["rss_bytes"] >= stop_loss["max_rss_mb"] * 1048576:
                    breach = f"RSS {snap['rss_bytes']//1048576}MB >= {stop_loss['max_rss_mb']}MB"
                if breach:
                    rec.error = f"止损触发: {breach}"
                    proc.kill()
                    break
        time.sleep(interval)

    try:
        proc.wait(timeout=10)
    except subprocess.TimeoutExpired:
        proc.kill()
        proc.wait()
    rec.wall_s = time.perf_counter() - t_start

    # 末次采样（进程可能刚好退出，失败则沿用最后一次成功的）
    final = sampler.read()
    if final is None and rec.series:
        final = rec.series[-1].d

    text = "\n".join(lines)
    # 注意：阶段时间戳已在 pump_stdout 里按到达时刻记好，
    # 这里绝不能再按文本重扫一遍覆盖 —— 那等于全部退回进程退出时刻。
    rec.phases = dict(phase_marks)

    if proc.returncode != 0 and not rec.error:
        rec.error = f"rc={proc.returncode}: {text.strip()[-600:]}"
        return rec

    result_lines = [l for l in lines if l.startswith("RESULT")]
    if not result_lines:
        rec.error = rec.error or f"无 RESULT 行: {text.strip()[-600:]}"
        return rec

    kv = {k: int(v) for k, v in RESULT_RE.findall(result_lines[-1])}
    for want in ("tps", "p50", "p95", "p99", "max", "n", "checksum"):
        if want not in kv:
            rec.error = f"RESULT 缺字段 {want}: {result_lines[-1][:200]}"
            return rec

    v = rec.values
    v["tps"] = kv["tps"]
    v["p50_ns"] = kv["p50"]
    v["p95_ns"] = kv["p95"]
    v["p99_ns"] = kv["p99"]
    v["p999_ns"] = kv.get("p999", 0)
    v["max_ns"] = kv["max"]
    v["errors"] = kv.get("errors", 0)
    v["checksum"] = kv["checksum"]

    if rec.series:
        first = rec.series[0].d
        last = rec.series[-1].d
        v["cpu_user_s"] = round(last["cpu_user_s"] - first["cpu_user_s"], 4)
        v["cpu_sys_s"] = round(last["cpu_sys_s"] - first["cpu_sys_s"], 4)
        v["rss_peak_b"] = max(peak_rss, last.get("rss_peak_bytes", 0))
        v["rss_end_b"] = last["rss_bytes"]
        v["priv_end_b"] = last["private_bytes"]
        v["handles_end"] = last["handles"]
        v["threads_end"] = last["threads"]
        v["ctxsw"] = max(0, last["ctx_switches"] - first["ctx_switches"])
        v["io_read_b"] = max(0, last["io_read_bytes"] - first["io_read_bytes"])
        v["io_write_b"] = max(0, last["io_write_bytes"] - first["io_write_bytes"])
        v["page_faults"] = max(0, last["page_faults"] - first["page_faults"])

    derive(v, conc, pre_load, final, rec)
    return rec


def derive(v: dict, conc: int, pre_load: dict | None,
           final: dict | None, rec: CaseRecord) -> None:
    """由原始量算派生量。缺数据就留 0/N/A，不编数。"""
    tps = v.get("tps") or 0
    v["tps_per_conc"] = round(tps / conc, 3) if conc else 0
    p50 = v.get("p50_ns") or 0
    p99 = v.get("p99_ns") or 0
    v["p99_over_p50"] = round(p99 / p50, 3) if p50 else 0
    cpu = (v.get("cpu_user_s") or 0) + (v.get("cpu_sys_s") or 0)
    v["tps_per_cpu_s"] = round(tps / cpu, 1) if cpu > 0 else 0
    u = v.get("cpu_user_s") or 0
    v["sys_over_user"] = round((v.get("cpu_sys_s") or 0) / u, 3) if u > 0 else 0
    total_tx = rec.total or 0
    v["ctxsw_per_task"] = round((v.get("ctxsw") or 0) / total_tx, 4) if total_tx else 0
    v["rss_per_task"] = round((v.get("rss_peak_b") or 0) / total_tx) if total_tx else 0

    # 句柄净增 = 负载后 − 负载前。
    # 基线取 PHASE load_start 之后的第一个样：用「进程一起来就采」会把
    # 运行时初始化（Go 建线程、Tokio 建 worker 池）也算成泄漏。
    #
    # 两个相位都必须真有样，缺一不可 —— 否则 final["handles"] 会是 0，
    # 算出来的就是「−负载前句柄数」这种反向假数。实测快跑模式下
    # final 相位拿不到样，24/24 行 handle_growth 全为负，与本列
    # 「>0 疑似泄漏」的语义直接矛盾。取不到就留 0（=无数据），不编数。
    if (pre_load and final
            and pre_load.get("handles") and final.get("handles")):
        v["handle_growth"] = final["handles"] - pre_load["handles"]
    else:
        v["handle_growth"] = 0

    # RSS 回落 = (load_end 时 − settle 时) / load_end 时。
    # 依赖被测程序打印 PHASE load_end / PHASE settle，否则显式报 0（拿不到）。
    load_end_rss = _rss_at_phase(rec, "load_end")
    settle_rss = _rss_at_phase(rec, "settle")
    if load_end_rss and settle_rss and load_end_rss > 0:
        v["rss_reclaim"] = round(max(0.0, (load_end_rss - settle_rss) / load_end_rss), 3)
    else:
        v["rss_reclaim"] = 0.0


def _rss_at_phase(rec: CaseRecord, name: str) -> int | None:
    t = rec.phases.get(name)
    if t is None or not rec.series:
        return None
    # 取该时刻之后最近的一个样
    for s in rec.series:
        if s.t >= t:
            return s.d.get("rss_bytes")
    return None


def run_with_retries(exe, args, rt, curve, conc, total, *, interval, timeout,
                     stop_loss, retries) -> CaseRecord:
    """跑一次配置（含 F1 概率性死锁的挂死重试），返回一条 CaseRecord。"""
    attempts = hangs = 0
    while True:
        attempts += 1
        try:
            rec = run_case(exe, args, rt, curve, conc, total,
                           interval=interval, timeout=timeout, stop_loss=stop_loss)
        except SystemExit as e:
            rec = CaseRecord(rt, curve, conc, total, error=str(e))
        except Exception as e:      # noqa: BLE001
            rec = CaseRecord(rt, curve, conc, total, error=repr(e))
        # 只对「挂死」重试：构建失败/止损/缺 RESULT 重试没用，只会把 bug 藏起来。
        hung = bool(rec.error) and "超时" in rec.error
        if hung and hangs < retries:
            hangs += 1
            log(f"  挂死（F1 概率性死锁）-> 重试 {hangs}/{retries}")
            continue
        rec.attempts = attempts
        rec.hangs = hangs
        return rec


def _repeat_stats(xs: list[int]) -> dict:
    """一组重复采样的离散度统计。xs 非空（调用方保证）。

    median   中位数（主报告用这个，不用均值——对单轮离群值免疫）
    spread%  轮内极差 = max/min - 1。本机噪声底 8%~49%，spread 就是该配置
             「自己跟自己比」的噪声量级：跨配置比较时，差值若小于两边 spread，
             落在噪声内，不能下结论。
    cv%      变异系数 = 标准差/均值。对量级不敏感，跨配置可比。
    """
    if len(xs) < 2:
        return {"n": len(xs), "median": statistics.median(xs) if xs else 0,
                "spread_pct": 0.0, "cv_pct": 0.0, "samples": xs}
    med = statistics.median(xs)
    mean = statistics.mean(xs)
    sd = statistics.pstdev(xs)
    spread = (max(xs) / min(xs) - 1) * 100 if min(xs) else 0.0
    cv = sd / mean * 100 if mean else 0.0
    return {"n": len(xs), "median": med,
            "spread_pct": round(spread, 1), "cv_pct": round(cv, 1),
            "samples": xs}


def aggregate_repeats(recs: list[CaseRecord]) -> CaseRecord:
    """把同一配置的多次重复采样聚合成一行。

    代表行的 tps/p99_ns 改写为多次中位数；离散度（spread%/cv%）挂在
    values["tps_repeat"] / values["p99_repeat"] 里，供报告与 JSON 输出。
    其余进程级指标（cpu/rss…）取代表行那一次，不做重复聚合。
    """
    rep = recs[0]
    tps_list = [r.values["tps"] for r in recs if r.values.get("tps")]
    p99_list = [r.values["p99_ns"] for r in recs if r.values.get("p99_ns")]
    rep.values["tps_repeat"] = _repeat_stats(tps_list)
    rep.values["p99_repeat"] = _repeat_stats(p99_list)
    if tps_list:
        rep.values["tps"] = statistics.median(tps_list)
    if p99_list:
        rep.values["p99_ns"] = statistics.median(p99_list)
    rep.n_repeat = len(recs)
    return rep


# ==========================================================================
# 构建
# ==========================================================================

def build_go() -> Path:
    src = HERE / "go" / "bench"
    exe = src / f"bench_go{EXE_SUFFIX}"
    # encoding 显式指定：text=True 在 Windows 上按 locale（GBK）解码，
    # 而编译器诊断常是 UTF-8 —— 混用会在子线程抛 UnicodeDecodeError，
    # 主流程只剩半截输出。errors="replace" 保证拿不到干净文本也不崩。
    r = subprocess.run(["go", "build", "-o", str(exe), "."], cwd=str(src),
                       capture_output=True, encoding="utf-8", errors="replace")
    if r.returncode != 0:
        raise SystemExit(f"go build 失败:\n{r.stdout}\n{r.stderr}")
    return exe


def build_tokio() -> Path:
    src = HERE / "tokio" / "bench"
    exe = src / "target" / "release" / f"bench_tokio{EXE_SUFFIX}"
    r = subprocess.run(["cargo", "build", "--release"], cwd=str(src),
                       capture_output=True, encoding="utf-8", errors="replace")
    if not exe.exists():
        raise SystemExit(f"cargo build 失败:\n{r.stdout[-2000:]}\n{r.stderr[-2000:]}")
    return exe


def build_kaula(curve: str, conc: int, total: int) -> Path:
    from run_bench import render_kaula          # 复用同一份模板渲染
    src = HERE / "kaula" / "bench_gen.kl"
    src.write_text(render_kaula(curve, conc, total), encoding="utf-8")
    exe = HERE / "kaula" / f"bench_gen{EXE_SUFFIX}"
    kaulac = REPO / "build" / "bin" / f"kaulac{EXE_SUFFIX}"
    if not kaulac.exists():
        raise SystemExit(f"找不到 {kaulac}，先跑 python toolkit_build.py --target compiler")
    # 先删旧产物：下面只判 exists()。若上一次的 exe 正在运行导致本次链接失败，
    # 旧文件仍在 —— 会把上一条曲线的二进制当成本次结果跑出来。
    exe.unlink(missing_ok=True)
    r = subprocess.run([str(kaulac), str(src)], capture_output=True,
                       encoding="utf-8", errors="replace", cwd=str(REPO))
    if not exe.exists():
        raise SystemExit(f"kaulac 失败:\n{r.stdout[-2000:]}\n{r.stderr[-2000:]}")
    return exe


# ==========================================================================
# 报告
# ==========================================================================

def fmt_val(key: str, val) -> str:
    if val is None:
        return "N/A"
    m = METRICS_BY_KEY.get(key)
    if not m:
        return str(val)
    if val == 0 and m.source == "derived" and key in ("sys_over_user",
                                                      "rss_reclaim", "tps_per_cpu_s"):
        return "N/A"          # 算不出来就是算不出来，不当 0 比
    if m.unit == "ns":
        return f"{val/1000:.1f}u" if val < 1_000_000 else f"{val/1e6:.2f}m"
    if m.unit == "bytes":
        return f"{val/1048576:.1f}M"
    if m.unit == "s":
        return f"{val:.2f}"
    if isinstance(val, float):
        return f"{val:.3f}"
    return str(val)


# 报表列：一列一个指标。N/A 表示该运行时在本机拿不到这个数。
COLUMNS = ["tps", "p50_ns", "p95_ns", "p99_ns", "p999_ns", "max_ns",
           "p99_over_p50", "errors", "cpu_user_s", "cpu_sys_s",
           "sys_over_user", "tps_per_cpu_s", "tps_per_conc",
           "rss_peak_b", "handles_end", "threads_end", "ctxsw",
           "ctxsw_per_task", "page_faults", "io_read_b", "io_write_b",
           "handle_growth", "rss_reclaim", "checksum"]


def print_report(records: list[CaseRecord]) -> None:
    ok = [r for r in records if not r.error]
    bad = [r for r in records if r.error]

    hdr = f"{'case':<26}" + "".join(f"{c[:9]:>11}" for c in COLUMNS)
    print()
    print("=" * len(hdr))
    print("指标全表（可比核心 · 每行一个用例 · 同列才可横向比）")
    print("=" * len(hdr))
    print(hdr)
    print("-" * len(hdr))
    for r in ok:
        row = f"{r.runtime+'/'+r.curve+'/'+str(r.conc):<26}"
        for c in COLUMNS:
            row += f"{fmt_val(c, r.values.get(c)):>11}"
        print(row)

    if bad:
        print()
        print(f"失败 {len(bad)} 例：")
        for r in bad:
            print(f"  {r.runtime}/{r.curve}/conc={r.conc}: {r.error[:200]}")

    # 挂死率必须显式报出来：重试让用例拿到了数字，但不能把
    # 「试 5 次挂 4 次」藏掉 —— 那本身就是 F1 的严重度。
    flaky = [r for r in records if r.attempts > 1 or r.hangs > 0]
    if flaky:
        print()
        print("用例可靠性（挂死后重试，对应 FINDINGS.md F1）:")
        for r in flaky:
            state = "成功" if not r.error else f"最终仍失败: {r.error[:80]}"
            print(f"   {r.runtime}/{r.curve}/conc={r.conc}: "
                  f"共试 {r.attempts} 次，挂死 {r.hangs} 次，{state}")
        print("   注意：上表该行的数字来自成功的那一次，"
              "不代表该运行时能稳定跑出这个数。")

    # 重复采样离散度（--repeat N，对应 FINDINGS F10 未完成项）。
    # 上表 tps/p99 已是 N 次中位数；这里把「这格自己晃多厉害」显式列出来，
    # 否则跨配置/跨运行时比较时无法判断差值是否大过噪声。
    repeated = [r for r in ok if r.n_repeat > 1]
    if repeated:
        print()
        print("重复采样离散度（--repeat；spread%=轮内极差 max/min-1，cv%=标准差/均值）:")
        print(f"   {'case':<26}{'n':>4}{'tps中位':>11}{'tps spread%':>12}"
              f"{'tps cv%':>9}{'p99中位':>11}{'p99 spread%':>12}")
        for r in repeated:
            tr = r.values.get("tps_repeat", {})
            pr = r.values.get("p99_repeat", {})
            print(f"   {r.runtime + '/' + r.curve + '/' + str(r.conc):<26}"
                  f"{r.n_repeat:>4}{tr.get('median', 0):>11,.0f}"
                  f"{tr.get('spread_pct', 0):>11.1f}%{tr.get('cv_pct', 0):>8.1f}%"
                  f"{pr.get('median', 0) / 1000:>10.1f}u"
                  f"{pr.get('spread_pct', 0):>11.1f}%")
        print("   判定：跨配置差值若小于两边 spread% 即落在噪声内（本机噪声底 8%~49%）；"
              "正式结论需 --repeat>=11。")

    # 校验：同一条曲线的 checksum 三边必须一致，不一致说明算的不是同一件事。
    #
    # 必须按曲线分组：light/suspend/mixed 与 degrade 的期望值本来就不同
    # （degrade 走 acc^parse 路径），混在一起比必然报「不一致」——
    # 这是个假警报，会把「三边全对」误判成「上表不可比」。
    by_curve: dict[str, set] = {}
    for r in ok:
        if "checksum" in r.values:
            by_curve.setdefault(r.curve, set()).add(r.values["checksum"])
    mismatch = {c: sorted(v) for c, v in by_curve.items() if len(v) > 1}
    if mismatch:
        print()
        print("!! checksum 不一致（按曲线分组后仍有多个值）:")
        for c, v in mismatch.items():
            print(f"   {c}: {v}")
        print("   —— 至少两个运行时算的不是同一件事，相关行不可比")
    elif by_curve:
        print()
        print("checksum 按曲线三边一致:")
        for c in sorted(by_curve):
            print(f"   {c:<10} = {next(iter(by_curve[c]))}")

    # 指标口径图例
    print()
    print("口径：workload=被测程序自报，external=OS 进程级，derived=前两者算出。")
    print("      三边均为同源同口径，故可横向比；跨列比较无效。")
    unavail = [m.key for m in REGISTRY
               if m.key not in COLUMNS and any(
                   hasattr(r, "values") and m.key in r.values for r in ok)]
    if unavail:
        print(f"      已采集但未列入本表: {', '.join(unavail)}")


# ==========================================================================
# 主流程
# ==========================================================================

CURVES_DEFAULT = "light,suspend,mixed,degrade"


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--runtimes", default="kaula,go,tokio")
    ap.add_argument("--curves", default=CURVES_DEFAULT)
    ap.add_argument("--conc", default="100,1000")
    ap.add_argument("--total", type=int, default=2000)
    ap.add_argument("--interval", type=float, default=0.05)
    ap.add_argument("--timeout", type=float, default=600.0)
    ap.add_argument("--retries", type=int, default=4,
                    help="挂死（超时强杀）后重试次数。F1 是概率性死锁，"
                         "不重试的话 Kaula 的 suspend/mixed 整列都是空的；"
                         "设 0 关闭重试。挂死率会单独报出来，不会被藏掉。")
    ap.add_argument("--repeat", type=int, default=1,
                    help="同一 (runtime/curve/conc) 配置重复采样 N 次，报告 tps/p99 "
                         "的中位数 + 离散度（spread%=轮内极差、cv%=变异系数）。"
                         "本机噪声底 8%~49%，单样本对比无效：正式结论至少 --repeat 11。"
                         "默认 1 = 旧行为（每格一个样本）。")
    ap.add_argument("--json", default="")
    ap.add_argument("--full", action="store_true",
                    help="完整协议：阶梯加压 + 长稳 + 止损")
    ap.add_argument("--list", action="store_true")
    ap.add_argument("--skip-build", action="store_true")
    a = ap.parse_args()

    runtimes = [x for x in a.runtimes.split(",") if x]
    curves = [x for x in a.curves.split(",") if x]
    concs = [int(x) for x in a.conc.split(",") if x]

    if a.list:
        print("runtimes: kaula, go, tokio")
        print("curves:  ", ", ".join(["light", "suspend", "mixed", "degrade"]))
        print()
        print(f"{'key':<16}{'unit':<8}{'source':<10}desc")
        print("-" * 88)
        for m in REGISTRY:
            print(f"{m.key:<16}{m.unit:<8}{m.source:<10}{m.desc}")
        return 0

    if a.full:
        # 阶梯加压：100 -> 1k -> 10k，每档加压到 total 变大
        concs = [100, 1000, 10000]
        a.total = 20000
        a.timeout = 1800.0
        log("--full: conc=100,1000,10000 total=20000 timeout=1800s")

    stop_loss = {"max_handles": 20000, "max_rss_mb": 4096} if a.full else None

    # 构建
    built: dict[str, Path] = {}
    if not a.skip_build:
        for rt in runtimes:
            if rt == "kaula":
                built[rt] = None          # 每组参数重新生成编译
            elif rt == "go":
                log("构建 Go ...")
                built[rt] = build_go()
            elif rt == "tokio":
                log("构建 Tokio ...")
                built[rt] = build_tokio()
            else:
                raise SystemExit(f"未知运行时: {rt}")

    records: list[CaseRecord] = []
    for rt in runtimes:
        for curve in curves:
            for conc in concs:
                log(f"{rt}/{curve}/conc={conc}")
                # 构建放在重试循环外：挂死是运行期的事，重编译不会让它不挂，
                # 白白多花 12s。构建失败也不重试——重试只会掩盖要修的问题。
                try:
                    if rt == "kaula":
                        exe = build_kaula(curve, conc, a.total)
                        args = []
                    else:
                        exe = built[rt]
                        args = [curve, str(conc), str(a.total)]
                except SystemExit as e:
                    rec = CaseRecord(rt, curve, conc, a.total, error=str(e))
                    records.append(rec)
                    log(f"  失败: {rec.error[:160]}")
                    continue
                except Exception as e:      # noqa: BLE001
                    rec = CaseRecord(rt, curve, conc, a.total, error=repr(e))
                    records.append(rec)
                    log(f"  失败: {rec.error[:160]}")
                    continue

                repeat = max(1, a.repeat)
                rep_records: list[CaseRecord] = []
                for r_i in range(repeat):
                    rec = run_with_retries(
                        exe, args, rt, curve, conc, a.total,
                        interval=a.interval, timeout=a.timeout,
                        stop_loss=stop_loss, retries=a.retries)
                    if rec.error:
                        # 任何一次采样失败（挂死重试耗尽/构建后报错）：停止本配置采样，
                        # 用已成功的样本聚合；一个都没成就把这条 error 记录进表。
                        break
                    rep_records.append(rec)
                    if repeat > 1:
                        log(f"  样本 {r_i + 1}/{repeat}: "
                            f"tps={rec.values.get('tps')} "
                            f"p99={rec.values.get('p99_ns', 0) / 1000:.1f}us")

                if not rep_records:
                    records.append(rec)
                    log(f"  失败: {rec.error[:160]}")
                    continue
                # 代表行：tps/p99 改写为多次中位数，离散度挂在 values["*_repeat"]。
                rec = aggregate_repeats(rep_records) if repeat > 1 else rep_records[0]
                records.append(rec)
                tps_rep = rec.values.get("tps_repeat", {})
                p99_rep = rec.values.get("p99_repeat", {})
                disp_note = (f" tpsσ={tps_rep.get('spread_pct')}%/"
                             f"cv={tps_rep.get('cv_pct')}% "
                             f"p99σ={p99_rep.get('spread_pct')}%/"
                             f"cv={p99_rep.get('cv_pct')}%"
                             if rec.n_repeat > 1 else "")
                log(f"  tps={rec.values.get('tps')} "
                    f"p99={rec.values.get('p99_ns', 0) / 1000:.1f}us "
                    f"err={rec.values.get('errors')} "
                    f"cpu={(rec.values.get('cpu_user_s', 0) + rec.values.get('cpu_sys_s', 0)):.2f}s"
                    f" n={rec.n_repeat}{disp_note}")

    print_report(records)

    if a.json:
        out = [{"runtime": r.runtime, "curve": r.curve, "conc": r.conc,
                "total": r.total, "wall_s": r.wall_s, "error": r.error,
                "attempts": r.attempts, "hangs": r.hangs,
                "n_repeat": r.n_repeat,
                "values": r.values,
                "series": [{"t": s.t, **s.d} for s in r.series]}
               for r in records]
        Path(a.json).write_text(json.dumps(out, indent=2), encoding="utf-8")
        log(f"原始记录 -> {a.json}")

    return 0 if all(not r.error for r in records) else 1


if __name__ == "__main__":
    raise SystemExit(main())
