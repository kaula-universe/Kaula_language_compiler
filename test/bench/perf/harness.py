#!/usr/bin/env python3
"""采样器 —— 交错执行 A/B，把顺序偏差与冷启动排除掉。

这个模块只负责「取数」，不做统计判定（那在 compare.py）。
每一条设计都对应一个已实测确认的陷阱，注释里写明了原因。

核心保证：
  1. 交错：每轮内 A/B 顺序交替（i 偶数 A 先，i 奇数 B 先）。
     实测不交错的顺序采样可造出 +50% 的假差异。
  2. 预热：前 N 轮丢弃。首轮明显偏低（页表首次触碰、频率爬升）。
  3. 正确性前置：每轮校验 errors/spawn_failed，任一非零立即中止采样。
     性能结论建立在正确的计算之上，算错了跑多快都没意义。
  4. 超时强杀：被测程序死锁时不留半截结果污染样本。
"""
from __future__ import annotations

import os
import re
import subprocess
import time
from dataclasses import dataclass, field
from pathlib import Path

RESULT_RE = re.compile(r"(\w+)=(-?\d+)")


@dataclass
class Sample:
    """单次采样记录。带时间戳是为了事后能看出漂移趋势。"""
    tag: str
    tps: int
    p50: int
    p99: int
    checksum: int
    errors: int
    spawn_failed: int
    wall_s: float
    t_start: float
    index: int


@dataclass
class RunConfig:
    exe: Path
    curve: str
    conc: int
    total: int
    tag: str = "A"
    env: dict = field(default_factory=dict)
    timeout: float = 300.0


def run_once(cfg: RunConfig, index: int) -> tuple[Sample | None, str]:
    """跑一次，返回 (Sample, "") 或 (None, 错误信息)。"""
    cmd = [str(cfg.exe), cfg.curve, str(cfg.conc), str(cfg.total)]
    full_env = dict(os.environ)
    full_env.update(cfg.env)
    t0 = time.perf_counter()
    try:
        r = subprocess.run(cmd, capture_output=True, encoding="utf-8",
                           errors="replace", env=full_env,
                           timeout=cfg.timeout, cwd=str(cfg.exe.parent))
    except subprocess.TimeoutExpired:
        return None, f"超时 {cfg.timeout}s（疑似死锁）"
    wall = time.perf_counter() - t0
    out = (r.stdout or "") + (r.stderr or "")
    if r.returncode != 0 or "RESULT" not in out:
        return None, f"rc={r.returncode}: {out.strip()[-300:]}"
    line = [l for l in out.splitlines() if l.startswith("RESULT")][-1]
    kv = dict((k, int(v)) for k, v in RESULT_RE.findall(line))
    return Sample(
        tag=cfg.tag,
        tps=kv.get("tps", 0),
        p50=kv.get("p50", 0),
        p99=kv.get("p99", 0),
        checksum=kv.get("checksum", 0),
        errors=kv.get("errors", 0),
        spawn_failed=kv.get("spawn_failed", 0),
        wall_s=round(wall, 3),
        t_start=t0,
        index=index,
    ), ""


def interleaved_run(cfg_a: RunConfig, cfg_b: RunConfig, rounds: int,
                    warmup: int = 2, strict: bool = True,
                    on_round=None) -> dict:
    """交错采样 A/B 各 rounds 次（外加 warmup 轮丢弃）。

    strict=True 时，每轮内 A/B 顺序交替；这是消除顺序偏差的关键。
    strict=False 会退化成「A 恒先、B 恒后」，只用于复现偏差本身。

    返回 {samples_a, samples_b, errors, aborted}
    """
    samples_a: list[Sample] = []
    samples_b: list[Sample] = []
    errors: list[str] = []
    aborted = False

    for i in range(rounds + warmup):
        is_warm = i < warmup
        if strict:
            order = [cfg_a, cfg_b] if i % 2 == 0 else [cfg_b, cfg_a]
        else:
            order = [cfg_a, cfg_b]

        for cfg in order:
            s, err = run_once(cfg, i)
            if err:
                errors.append(f"round{i} {cfg.tag}: {err}")
                continue
            if is_warm:
                continue   # 预热轮只把机器跑热，不入样本
            # 正确性前置：算错了就停，不把性能数字建立在错误结果上
            if s.errors or s.spawn_failed:
                errors.append(f"round{i} {s.tag}: errors={s.errors} "
                              f"spawn_failed={s.spawn_failed}")
                aborted = True
                break
            (samples_a if cfg.tag == "A" else samples_b).append(s)

        if aborted:
            break
        if on_round:
            on_round(i + 1, rounds, samples_a, samples_b)

    return {"samples_a": samples_a, "samples_b": samples_b,
            "errors": errors, "aborted": aborted}


def tps_of(samples: list[Sample]) -> list[float]:
    return [float(s.tps) for s in samples]


def checksum_consistent(samples: list[Sample]) -> tuple[bool, int]:
    """跨轮 checksum 是否完全一致。这是「数据收敛」的正确性判据。"""
    vals = {s.checksum for s in samples}
    return (len(vals) <= 1, vals.pop() if len(vals) == 1 else -1)
