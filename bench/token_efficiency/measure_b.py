#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""B 级：任务归一产出量 —— 同一个功能，六种语言各要写多少 token。

统计对象是 solutions/<lang>/<task> 里 BENCH:BEGIN..BENCH:END 之间的"实现区"
（含 import / #include / 类型声明 / 辅助函数，不含本地自检的 main）。
这正是一个模型"要写出来的那部分"。

输出：
    results/b_normalized.csv   明细（每语言每任务）
    results/b_normalized.md    汇总表（便于贴进 README）
"""
import argparse
import csv
import re
from pathlib import Path

import tiktoken

HERE = Path(__file__).resolve().parent
LANGS = ("py", "go", "cpp", "c", "rs", "kl", "kl_cstyle")
LANG_NAME = {"py": "Python", "go": "Go", "cpp": "C++", "c": "C", "rs": "Rust",
             "kl": "Kaula(地道)", "kl_cstyle": "Kaula(C 风格)"}
EXT = {"py": ".py", "go": ".go", "cpp": ".cpp", "c": ".c", "rs": ".rs", "kl": ".kl",
       "kl_cstyle": ".kl"}
REGION_RE = re.compile(r"BENCH:BEGIN\s*\n(.*?)\n\s*(?://|#)\s*BENCH:END", re.S)


PREFIX_RE = re.compile(r"BENCH:PREFIX\s*\n(.*?)\n\s*(?://|#)\s*BENCH:BEGIN", re.S)


def prefix(path: Path) -> str:
    m = PREFIX_RE.search(path.read_text(encoding="utf-8"))
    return m.group(1) if m else ""


def region(path: Path) -> str:
    s = path.read_text(encoding="utf-8")
    m = REGION_RE.search(s)
    if not m:
        raise SystemExit(f"marker missing: {path}")
    return m.group(1)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--tokenizer", default="o200k_base")
    args = ap.parse_args()

    enc = tiktoken.get_encoding(args.tokenizer)
    tasks = sorted({p.stem for p in (HERE / "solutions" / "kl").glob("*.kl")})

    rows = []
    for task in tasks:
        for lang in LANGS:
            p = HERE / "solutions" / lang / (task + EXT[lang])
            code = region(p)
            pre = prefix(p)
            rows.append({
                "task": task, "lang": lang, "tokenizer": args.tokenizer,
                "tokens": len(enc.encode(code)),
                "bytes": len(code.encode("utf-8")),
                "lines": len(code.splitlines()),
                "prefix_tokens": len(enc.encode(pre)),
            })

    out_csv = HERE / "results" / "b_normalized.csv"
    out_csv.parent.mkdir(parents=True, exist_ok=True)
    with out_csv.open("w", newline="", encoding="utf-8") as f:
        w = csv.DictWriter(f, fieldnames=list(rows[0].keys()))
        w.writeheader()
        w.writerows(rows)

    # 汇总
    tot = {l: 0 for l in LANGS}
    lines = {l: 0 for l in LANGS}
    pref = {l: 0 for l in LANGS}
    for r in rows:
        tot[r["lang"]] += r["tokens"]
        lines[r["lang"]] += r["lines"]
        pref[r["lang"]] += r["prefix_tokens"]
    base = min(tot.values())

    md = ["| 语言 | 实现区 tokens 合计 | 相对最小值 | 行数 | tokens/行 | 前缀样板 tokens |",
          "|---|---|---|---|---|---|"]
    for l in sorted(LANGS, key=lambda x: tot[x]):
        md.append(f"| {LANG_NAME[l]} | {tot[l]} | {tot[l]/base:.2f}x | {lines[l]} | "
                  f"{tot[l]/max(lines[l],1):.2f} | {pref[l]} |")

    # 明细表
    detail = ["| 任务 | " + " | ".join(LANG_NAME[l] for l in LANGS) + " |",
              "|---|" + "---|" * len(LANGS)]
    for task in tasks:
        cells = {r["lang"]: r["tokens"] for r in rows if r["task"] == task}
        detail.append(f"| {task} | " + " | ".join(str(cells[l]) for l in LANGS) + " |")

    md_path = HERE / "results" / "b_normalized.md"
    md_path.write_text("\n".join(md + ["", "### 明细（tokens/实现区）", ""] + detail) + "\n",
                       encoding="utf-8")

    print("\n".join(md))
    print(f"\n明细：{md_path}")


if __name__ == "__main__":
    main()
