#!/usr/bin/env python3
"""gen_report.py —— 从 results.json 生成 REPORT.md 的表格部分。

只做「读结果 -> 出表」，不改任何测量。所有数字都来自 results.json，
报告里出现的每一个数都能追溯到原始窗口。
"""
import json
import os
import sys
from statistics import median

HERE = os.path.dirname(os.path.abspath(__file__))
NAMES = ["kaula", "go", "tokio"]
LEVELS = ["baseline", "cpu25", "cpu50", "cpu75", "mem25", "mem50", "mem75", "net"]
LNAME = {
    "baseline": "无干扰",
    "cpu25": "CPU 25%（占 8 核）",
    "cpu50": "CPU 50%（占 16 核）",
    "cpu75": "CPU 75%（占 24 核）",
    "mem25": "内存 25%（192MB）",
    "mem50": "内存 50%（384MB）",
    "mem75": "内存 75%（576MB）",
    "net": "回环大流量 ×2",
}
LETTER = {"kaula": "K", "go": "G", "tokio": "T"}


def agg(runs, key):
    vs = [r[key] for r in runs if "error" not in r]
    return median(vs) if vs else None


def spread(runs, key="tps"):
    """跨轮 spread：各轮的 steady 中位数之间的极差比。"""
    vs = [r[key] for r in runs if "error" not in r]
    if len(vs) < 2:
        return 0.0
    m = median(vs) or 1
    return (max(vs) - min(vs)) / m * 100


def within_spread(runs, key="tps"):
    """轮内 spread：取各轮轮内极差的最大值（保守）。"""
    ps = [r["spread_pct"][key] for r in runs if "error" not in r]
    return max(ps) if ps else 0.0


def fmt(v, w=8, d=0):
    if v is None:
        return "—".rjust(w)
    return f"{v:>{w}.{d}f}"


def load():
    p = os.path.join(HERE, "results.json")
    with open(p) as f:
        return json.load(f)


def main():
    R = load()
    out = []
    w = out.append

    def ok(lv, nm):
        return [r for r in R.get(lv, {}).get(nm, []) if "error" not in r]

    def errs(lv, nm):
        return [r for r in R.get(lv, {}).get(nm, []) if "error" in r]

    # ---------- 0. 运行完整性 ----------
    w("### 0. 运行完整性\n")
    w("| 档位 | kaula ok/总 | go ok/总 | tokio ok/总 |")
    w("|---|---|---|---|")
    for lv in LEVELS:
        cells = []
        for nm in NAMES:
            tot = len(R.get(lv, {}).get(nm, []))
            cells.append(f"{len(ok(lv, nm))}/{tot}" if tot else "—")
        w(f"| {LNAME.get(lv, lv)} | {cells[0]} | {cells[1]} | {cells[2]} |")
    w("")

    # ---------- 1. baseline 主表 ----------
    w("### 1. baseline：吞吐与尾延迟\n")
    w("| 运行时 | TPS | p50 (µs) | p95 (µs) | p99 (µs) | p999 (µs) |")
    w("|---|---|---|---|---|---|")
    for nm in NAMES:
        r = ok("baseline", nm)
        if not r:
            w(f"| {nm} | 全部失败 | | | | |")
            continue
        w(f"| **{nm}** | {fmt(agg(r,'tps'))} | {fmt(agg(r,'p50'))} | {fmt(agg(r,'p95'))} "
          f"| {fmt(agg(r,'p99'))} | {fmt(agg(r,'p999'))} |")
    w("")
    w("离散度（同档跨轮极差 / 轮内窗口极差，均以 TPS 计）\n")
    w("| 运行时 | 跨轮 spread | 轮内 spread(max) | 轮内 spread(min) |")
    w("|---|---|---|---|")
    for nm in NAMES:
        r = ok("baseline", nm)
        if not r:
            continue
        inner = [x["spread_pct"]["tps"] for x in r]
        w(f"| {nm} | {spread(r,'tps'):.1f}% | {max(inner):.1f}% | {min(inner):.1f}% |")
    w("")

    # ---------- 2. 干扰下的绝对值矩阵 ----------
    w("### 2. 干扰下绝对值矩阵\n")
    for lv in LEVELS:
        if lv == "baseline":
            continue
        desc = ""
        for nm in NAMES:
            r = ok(lv, nm)
            if r:
                desc = r[0].get("disturb_desc", "")
                break
        w(f"**{LNAME.get(lv,lv)}**　<sub>{desc}</sub>\n")
        w("| 运行时 | TPS | p50 (µs) | p99 (µs) | p999 (µs) | spread(TPS) |")
        w("|---|---|---|---|---|---|")
        for nm in NAMES:
            r = ok(lv, nm)
            if not r:
                w(f"| {nm} | — | | | | |")
                continue
            w(f"| {nm} | {fmt(agg(r,'tps'))} | {fmt(agg(r,'p50'))} | {fmt(agg(r,'p99'))} "
              f"| {fmt(agg(r,'p999'))} | {within_spread(r,'tps'):.1f}% |")
        w("")

    # ---------- 3. 退化倍率 ----------
    w("### 3. 抗干扰退化倍率（相对同运行时 baseline）\n")
    w("> TPS 倍率 <1 表示吞吐下降；延迟倍率 >1 表示变慢。倍率越低/越高越差。\n")
    w("| 档位 | 运行时 | TPS× | p50× | p99× | p999× |")
    w("|---|---|---|---|---|---|")

    def ratio(a, b):
        if a is None or b is None or a == 0:
            return None
        return b / a

    for lv in LEVELS:
        if lv == "baseline":
            continue
        for nm in NAMES:
            b, c = ok("baseline", nm), ok(lv, nm)
            if not b or not c:
                w(f"| {LNAME.get(lv,lv)} | {nm} | — | | | |")
                continue
            w(f"| {LNAME.get(lv,lv)} | {nm} "
              f"| {ratio(agg(b,'tps'),agg(c,'tps')):.2f} "
              f"| {ratio(agg(b,'p50'),agg(c,'p50')):.2f} "
              f"| {ratio(agg(b,'p99'),agg(c,'p99')):.2f} "
              f"| {ratio(agg(b,'p999'),agg(c,'p999')):.2f} |")
    w("")

    # ---------- 4. 两个专项：CPU 逐级 / 内存逐级 ----------
    w("### 4. 逐级退化曲线\n")
    for fam, lvs in (("CPU 核心竞争", ["cpu25", "cpu50", "cpu75"]),
                     ("内存压力", ["mem25", "mem50", "mem75"])):
        w(f"**{fam}**\n")
        w("| 运行时 | 指标 | " + " | ".join(LNAME[x] for x in lvs) + " |")
        w("|---" * (len(lvs) + 2) + "|")
        for nm in NAMES:
            b = ok("baseline", nm)
            if not b:
                continue
            for key, label in (("tps", "TPS"), ("p99", "p99(µs)"), ("p999", "p999(µs)")):
                row = []
                for lv in lvs:
                    c = ok(lv, nm)
                    row.append(fmt(agg(c, key)) if c else "—")
                w(f"| {nm} | {label} | " + " | ".join(row) + " |")
        w("")

    # ---------- 5. 失败明细 ----------
    total_err = 0
    detail = []
    for lv in LEVELS:
        for nm in NAMES:
            for r in errs(lv, nm):
                total_err += 1
                detail.append((lv, nm, r.get("error", "?")))
    w(f"### 5. 失败明细（共 {total_err} 条）\n")
    if detail:
        w("| 档位 | 运行时 | 原因 |")
        w("|---|---|---|")
        for lv, nm, e in detail:
            w(f"| {lv} | {nm} | {e} |")
    else:
        w("无失败。三方全部档位均通过正确性门槛（mismatch=0 errors=0）。")
    w("")

    text = "\n".join(out)
    with open(os.path.join(HERE, "report_tables.md"), "w") as f:
        f.write(text)
    print(text)
    print(f"\n[gen_report] 表格已写 -> report_tables.md")


if __name__ == "__main__":
    main()
