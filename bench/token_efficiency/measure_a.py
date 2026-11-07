#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""A 级：静态 token 密度 —— 同一批代码在固定 tokenizer 下切多少个 token。

无关模型，只跟词表有关；是 B/C 级的底数。
指标：bytes/token（压缩率）、tokens/line、tokens/非空行。

用法：
    python3 measure_a.py                 # 默认：六语言参考实现 + 仓库语料
    python3 measure_a.py --csv out.csv
"""
import argparse
import csv
import glob
import json
from pathlib import Path

import tiktoken

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]

ENCODINGS = ("o200k_base", "cl100k_base")


def read(path: Path) -> str:
    return path.read_text(encoding="utf-8", errors="ignore")


def density(sources, enc):
    """返回 (bytes, tokens, lines, nonempty_lines)。"""
    b = t = l = nl = 0
    for p in sources:
        s = read(p)
        b += len(s.encode("utf-8"))
        t += len(enc.encode(s))
        lines = s.splitlines()
        l += len(lines)
        nl += sum(1 for x in lines if x.strip())
    return b, t, l, nl


def corpus_sets():
    """(标签, 文件列表)。同任务集六语言 + 仓库自有语料。"""
    sets = {}
    for lang in ("py", "go", "cpp", "c", "rs", "kl"):
        sets[f"toy-solutions/{lang}"] = sorted((HERE / "solutions" / lang).glob("*"))
    sets["kaula/test-corpus"] = sorted((ROOT / "test").glob("*.kl"))
    sets["kaula/tutorial-examples"] = sorted((ROOT / "docs" / "tutorials" / "examples").glob("*.kl"))
    sets["kaula/std-runtime-c"] = sorted((ROOT / "std").rglob("*.c"))
    return {k: [p for p in v if p.is_file()] for k, v in sets.items() if v}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--csv", default=str(HERE / "results" / "a_density.csv"))
    args = ap.parse_args()

    encs = {}
    for name in ENCODINGS:
        try:
            encs[name] = tiktoken.get_encoding(name)
        except Exception as e:  # 离线环境可能拿不到 BPE 文件
            print(f"warn: tokenizer {name} unavailable: {e}")

    rows = []
    for label, files in corpus_sets().items():
        byte_n = len(b"".join(read(p).encode("utf-8") for p in files))
        for name, enc in encs.items():
            b, t, l, nl = density(files, enc)
            rows.append({
                "corpus": label, "tokenizer": name, "files": len(files),
                "bytes": b, "tokens": t, "lines": l, "nonempty_lines": nl,
                "bytes_per_token": round(b / max(t, 1), 2),
                "tokens_per_line": round(t / max(l, 1), 2),
                "tokens_per_nonempty_line": round(t / max(nl, 1), 2),
            })

    out = Path(args.csv)
    out.parent.mkdir(parents=True, exist_ok=True)
    with out.open("w", newline="", encoding="utf-8") as f:
        w = csv.DictWriter(f, fieldnames=list(rows[0].keys()))
        w.writeheader()
        w.writerows(rows)

    print(f"{'corpus':28} {'tok':10} {'files':>5} {'bytes':>8} {'tokens':>7} "
          f"{'B/tok':>6} {'tok/line':>9} {'tok/非空行':>10}")
    for r in rows:
        print(f"{r['corpus']:28} {r['tokenizer']:10} {r['files']:5} {r['bytes']:8} "
              f"{r['tokens']:7} {r['bytes_per_token']:6.2f} {r['tokens_per_line']:9.2f} "
              f"{r['tokens_per_nonempty_line']:10.2f}")
    print(f"\nCSV: {out}")


if __name__ == "__main__":
    main()
