#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""C 级：端到端成本（tokens-to-green）。

三个可分别跑的通道：

1. `--mode oracle`（默认，无需 API key）
   假设模型一次写对：统计 `prompt_tokens + completion_tokens`。
   prompt 用 MultiPL-E 的官方题面（含该语言的函数骨架），completion 用参考实现。
   这是"下界成本"，用来分离"题面成本"与"输出成本"。

2. `--mode llm`（真实生成，需要 OpenAI 兼容端点）
   同一题面喂给模型，抽代码 → 编译 → 失败则把编译器输出回合喂回 → 重试，
   直到通过或超出 `--max-repairs`。统计每一轮的 usage，得到真实的 tokens-to-green。
   这是唯一能反映"模型对该语言熟不熟"的口径。

3. `--mode check`（自检）
   只用 oracle 路径跑一遍"抽代码 + 编译 + 运行"，确认管道本身是通的。

用法：
    python3 measure_c.py                       # oracle 汇总
    python3 measure_c.py --mode check          # 管道自检
    OPENAI_API_KEY=... python3 measure_c.py --mode llm \
        --base-url https://api.openai.com/v1 --model gpt-4o-mini --langs py,kl
"""
import argparse
import csv
import json
import os
import re
import shutil
import tempfile
import urllib.request
from pathlib import Path

import tiktoken

import langtools

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]
LANGS = ("py", "go", "cpp", "c", "rs", "kl")
LANG_NAME = {"py": "Python", "go": "Go", "cpp": "C++", "c": "C", "rs": "Rust", "kl": "Kaula"}

FENCE_RE = re.compile(r"```(?:[a-zA-Z0-9+#]*)\n(.*?)```", re.S)
REGION_RE = re.compile(r"BENCH:BEGIN\s*\n(.*?)\n\s*(?://|#)\s*BENCH:END", re.S)
MAIN_RE = re.compile(r"BENCH:MAIN:BEGIN\s*\n(.*?)\n\s*(?://|#)\s*BENCH:MAIN:END", re.S)


def read(p: Path) -> str:
    return p.read_text(encoding="utf-8")


PREFIX_RE = re.compile(r"BENCH:PREFIX\s*\n(.*?)\n\s*(?://|#)\s*BENCH:BEGIN", re.S)


def parts(lang: str, task: str) -> tuple[str, str, str]:
    """返回 (语言前缀 import/#include, 实现区, 驱动区)。"""
    p = HERE / "solutions" / lang / (task + {"py": ".py", "go": ".go", "cpp": ".cpp",
                                            "c": ".c", "rs": ".rs", "kl": ".kl"}[lang])
    s = read(p)
    pm = PREFIX_RE.search(s)
    return (pm.group(1) if pm else ""), REGION_RE.search(s).group(1), MAIN_RE.search(s).group(1)


def prompt_for(lang: str, task: str) -> str:
    """题面：MultiPL-E 官方 prompt（含该语言函数骨架）；无对应语言时用任务描述。"""
    num = task.split("_")[0]
    if lang == "py":
        for r in json.loads(read(HERE / "raw" / "py.json")):
            if r["task_id"] == "HumanEval/" + num:
                return r["prompt"]
    else:
        raw = HERE / "raw" / f"{lang}.json"
        if raw.exists():
            for r in json.loads(read(raw)):
                if r["name"].startswith("HumanEval_") and r["name"].split("_")[1] == num:
                    return r["prompt"]
    # C / Kaula 没有官方译文：用同一段英文题面 + 该语言自己的骨架，
    # 保证"题面内容"与其他语言等价，只有签名写法不同。
    doc = ""
    for r in json.loads(read(HERE / "raw" / "py.json")):
        if r["task_id"] == "HumanEval/" + num:
            p = r["prompt"]
            m = re.search(r'"""(.*?)"""', p, re.S)
            doc = (m.group(1).strip() if m else "")
            break
    return stub_for(lang, task) + "\n# spec:\n# " + doc.replace("\n", "\n# ") + "\n"


def stub_for(lang: str, task: str) -> str:
    """该语言的起始骨架：题目要求模型从这里续写。"""
    # 骨架含语言前缀（import/#include），这部分是模型也必须写的内容
    impl, _ = parts(lang, task)[1:]
    body = impl.split("\n")
    # 保留到第一个函数签名/类型声明为止，其余交给模型
    for i, line in enumerate(body):
        if re.match(r"\s*(fn |func |def |bool |double |int |float |char\*|String )", line):
            return "\n".join(body[: i + 1])
    return "\n".join(body[:1])


def oracle_cost(enc):
    rows = []
    for task in sorted({p.stem for p in (HERE / "solutions" / "kl").glob("*.kl")}):
        for lang in LANGS:
            prompt = prompt_for(lang, task)
            impl = parts(lang, task)[1]
            stub = stub_for(lang, task)
            rows.append({
                "task": task, "lang": lang,
                "prompt_tokens": len(enc.encode(prompt)),
                "stub_tokens": len(enc.encode(stub)),
                "completion_tokens": len(enc.encode(impl)) - len(enc.encode(stub)),
                "total_tokens": len(enc.encode(prompt)) + len(enc.encode(impl)),
            })
    return rows


def chat(base_url: str, key: str, model: str, messages) -> tuple[str, dict]:
    body = json.dumps({"model": model, "messages": messages, "temperature": 0}).encode()
    req = urllib.request.Request(
        base_url.rstrip("/") + "/chat/completions", data=body,
        headers={"Content-Type": "application/json", "Authorization": f"Bearer {key}"})
    with urllib.request.urlopen(req, timeout=180) as r:
        d = json.load(r)
    return d["choices"][0]["message"]["content"], d.get("usage", {})


def extract_code(text: str) -> str:
    m = FENCE_RE.search(text)
    return m.group(1) if m else text


def llm_run(lang: str, task: str, base_url: str, key: str, model: str,
            max_repairs: int, enc) -> dict:
    prompt = prompt_for(lang, task) + "\n" + stub_for(lang, task)
    prefix, impl, driver = parts(lang, task)
    messages = [{"role": "user", "content": prompt}]
    used = 0
    repairs = 0
    ok = False
    with tempfile.TemporaryDirectory() as td:
        for attempt in range(max_repairs + 1):
            try:
                content, usage = chat(base_url, key, model, messages)
            except Exception as e:
                return {"lang": lang, "task": task, "ok": False, "error": str(e)}
            used += usage.get("total_tokens", 0)
            code = extract_code(content)
            source = prefix + "\n" + prompt + code + "\n" + driver + "\n"
            ok, out = langtools.build_and_run(lang, source, Path(td))
            if ok:
                break
            repairs += 1
            messages += [{"role": "assistant", "content": content},
                         {"role": "user", "content": f"编译/运行失败，输出如下，请修复：\n{out}"}]
    return {"lang": lang, "task": task, "ok": ok, "tokens_to_green": used,
            "repairs": repairs, "attempts": repairs + 1}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--mode", default="oracle", choices=("oracle", "check", "llm"))
    ap.add_argument("--tokenizer", default="o200k_base")
    ap.add_argument("--langs", default=",".join(LANGS))
    ap.add_argument("--base-url", default=os.environ.get("OPENAI_BASE_URL", "https://api.openai.com/v1"))
    ap.add_argument("--model", default=os.environ.get("OPENAI_MODEL", "gpt-4o-mini"))
    ap.add_argument("--max-repairs", type=int, default=3)
    args = ap.parse_args()
    enc = tiktoken.get_encoding(args.tokenizer)
    langs = [l for l in args.langs.split(",") if l]

    if args.mode in ("oracle", "check"):
        rows = oracle_cost(enc)
        out = HERE / "results" / "c_oracle.csv"
        out.parent.mkdir(parents=True, exist_ok=True)
        with out.open("w", newline="", encoding="utf-8") as f:
            w = csv.DictWriter(f, fieldnames=list(rows[0].keys()))
            w.writeheader()
            w.writerows(rows)

        tot = {l: [0, 0] for l in LANGS}
        for r in rows:
            tot[r["lang"]][0] += r["prompt_tokens"]
            tot[r["lang"]][1] += r["completion_tokens"]
        base = min(sum(v) for v in tot.values())
        print("| 语言 | prompt tokens | completion tokens | 合计 | 相对下界 |")
        print("|---|---|---|---|---|")
        for l in sorted(LANGS, key=lambda x: sum(tot[x])):
            s = sum(tot[l])
            print(f"| {LANG_NAME[l]} | {tot[l][0]} | {tot[l][1]} | {s} | {s/base:.2f}x |")

        if args.mode == "check":
            print("\n=== 管道自检：oracle 代码必须能编译运行 ===")
            allok = True
            with tempfile.TemporaryDirectory() as td:
                for task in sorted({p.stem for p in (HERE / "solutions" / "kl").glob("*.kl")}):
                    for lang in langs:
                        prefix, impl, driver = parts(lang, task)
                        ok, outtxt = langtools.build_and_run(
                            lang, prefix + "\n" + impl + "\n" + driver, Path(td))
                        print(f"  {'PASS' if ok else 'FAIL'} {lang}/{task}")
                        allok = allok and ok
            raise SystemExit(0 if allok else 1)
        return

    key = os.environ.get("OPENAI_API_KEY", "")
    if not key:
        raise SystemExit("mode=llm 需要 OPENAI_API_KEY（OpenAI 兼容端点即可）")
    rows = []
    tasks = sorted({p.stem for p in (HERE / "solutions" / "kl").glob("*.kl")})
    for task in tasks:
        for lang in langs:
            r = llm_run(lang, task, args.base_url, key, args.model, args.max_repairs, enc)
            print(r)
            rows.append(r)
    out = HERE / "results" / "c_llm.csv"
    with out.open("w", newline="", encoding="utf-8") as f:
        w = csv.DictWriter(f, fieldnames=sorted({k for r in rows for k in r}))
        w.writeheader()
        w.writerows(rows)
    print(f"CSV: {out}")


if __name__ == "__main__":
    main()
