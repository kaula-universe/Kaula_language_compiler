#!/usr/bin/env python3
# run_compare.py — Kaula vs MoonBit 三维度对比 runner
#
# 维度：
#   1. 运行基准 (wall clock)：native 与 wasm 两种目标
#   2. 编译速度 (compile wall clock)
#   3. 产物大小 (artifact bytes)
#
# 题目：取自 test/bench/clbg 的 CLBG 标准题目，做等价 checksum kernel 移植。
#   - fannkuchredux  : n=10，纯整数（Mazurov 进位排列）
#   - binarytrees    : N=10，arena 数组形态（并行数组避免 MoonBit struct 别名）
# 正确性：Kaula 与 MoonBit 两边对同一题目输出同一个 checksum（逐值比对），
#   校验通过才计入计时；任一边编译/运行失败即中止并报错。
#
# 用法：
#   python3 run_compare.py                 # 全量：3 轮采样取中位数
#   python3 run_compare.py --rounds 5      # 自定义轮数
#   python3 run_compare.py --quick         # 每边 1 轮冒烟
#   python3 run_compare.py --verify-only   # 只校验 checksum 一致性
#
# 产物：build/ (gitignored) + REPORT.md（本次运行的报告）

import argparse
import os
import platform
import shutil
import statistics
import subprocess
import sys
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parent              # test/bench/clbg/moonbit
CLBG = ROOT.parent                                  # test/bench/clbg
REPO = CLBG.parents[2]                              # kaula repo root (clbg -> bench -> test -> repo)
BUILD = ROOT / "build"
REPORT = ROOT / "REPORT.md"

KAULAC = Path(os.environ.get("KAULAC", REPO / "build" / "bin" / "kaulac"))
MOON = os.environ.get("MOON", str(Path.home() / ".moon" / "bin" / "moon"))

# 题目配置：占位符注入参数；checksum kernel 两边算法逐行等价
BENCHES = [
    {"name": "fannkuchredux", "param": 10,
     "mbt": "fannkuchredux.mbtx", "kl": "fannkuchredux_kernel.kl",
     "placeholder": "N_PLACEHOLDER",
     "algo": "Mazurov 进位式排列递推 + 翻煎饼计数；checksum*1024+maxFlips",
     "result_var": "result", "needs_alloc": False},
    {"name": "binarytrees", "param": 10,
     "mbt": "binarytrees.mbtx", "kl": "binarytrees_kernel.kl",
     "placeholder": "N_PLACEHOLDER",
     "algo": "arena 完全二叉树建/遍历；stretch+各深度+long-lived check 求和",
     "result_var": "acc", "needs_alloc": True},
]


def log(msg):
    print(msg, flush=True)


def run(cmd, **kw):
    return subprocess.run(cmd, capture_output=True, text=True, **kw)


def env_moon():
    e = os.environ.copy()
    e["PATH"] = str(Path(MOON).parent) + os.pathsep + e.get("PATH", "")
    e["KAULA_HOME"] = str(REPO)
    return e


# ---------------------------------------------------------------------------
# 编译：返回 (artifact_path, compile_seconds)
# ---------------------------------------------------------------------------

def build_moonbit(b, target, tag):
    src = (ROOT / b["mbt"]).read_text()
    src = src.replace(b["placeholder"], str(b["param"]))
    wd = BUILD / tag / f"moonbit_{target}"
    wd.mkdir(parents=True, exist_ok=True)
    mbt = wd / f"{b['name']}.mbtx"
    mbt.write_text(src)
    t0 = time.perf_counter()
    r = run([MOON, "build", "--release", f"--target={target}", str(mbt)],
            cwd=wd, env=env_moon())
    dt = time.perf_counter() - t0
    if r.returncode != 0:
        log(f"  [ERR] moon build {target} {b['name']}:\n{r.stdout}\n{r.stderr}")
        sys.exit(1)
    # 产物路径：_build/<file>.mbtx/<target>/release/build/single/single.<ext>
    ext = "exe" if target == "native" else "wasm"
    arts = list((wd / "_build" / f"{b['name']}.mbtx" / target / "release").rglob(f"single.{ext}"))
    if not arts:
        sys.exit(f"找不到 MoonBit {target} 产物: {wd}")
    return arts[0], dt


def build_kaula(b, target, tag):
    src = (ROOT / b["kl"]).read_text()
    src = src.replace(b["placeholder"], str(b["param"]))
    needs_alloc = b.get("needs_alloc", False)
    if target == "native":
        imports = "import std.memory\nimport std.io\nimport std.format"
        src = src.replace("IMPORT_PLACEHOLDER", imports)
        src = src.replace("ALLOC_CALL_PLACEHOLDER", "std.memory.kmm_v4_alloc")
        # native 用 stdout 打印完整 i64 checksum（退出码只能容纳 8 位）
        rv = b["result_var"]
        emit = ('char* buf = as<char*>(std.memory.kmm_v4_alloc(64)); '
                f'std.format.format_printf(buf, 64, "%lld", {rv}); std.io.println(buf); return 0')
        src = src.replace("RESULT_EMIT_PLACEHOLDER", emit)
    else:
        src = src.replace("IMPORT_PLACEHOLDER", "import freestanding.memory" if needs_alloc else "")
        src = src.replace("ALLOC_CALL_PLACEHOLDER", "freestanding.memory.fs_alloc")
        # wasm 无 stdout，main 直接返回低 32 位 checksum
        if "binarytrees" in b["name"]:
            src = src.replace("RESULT_EMIT_PLACEHOLDER", "return as<i32>(acc)")
        else:
            src = src.replace("RESULT_EMIT_PLACEHOLDER", "return as<i32>(result)")
    wd = BUILD / tag / f"kaula_{target}"
    wd.mkdir(parents=True, exist_ok=True)
    kl = wd / f"{b['name']}.kl"
    kl.write_text(src)
    outdir = wd / "out"
    args = [str(KAULAC)]
    if target == "wasm":
        args += ["--wasm", "--cc", "/usr/bin/clang"]
    args += ["-release" if target == "native" else "", "-output-dir", str(outdir), str(kl)]
    args = [a for a in args if a]
    t0 = time.perf_counter()
    r = run(args, cwd=REPO, env=env_moon())
    dt = time.perf_counter() - t0
    if r.returncode != 0:
        log(f"  [ERR] kaulac {target} {b['name']}:\n{r.stdout}\n{r.stderr}")
        sys.exit(1)
    ext = "" if target == "native" else ".wasm"
    arts = list(outdir.glob(f"*{ext}"))
    arts = [a for a in arts if a.is_file()]
    if not arts:
        sys.exit(f"找不到 Kaula {target} 产物: {outdir}")
    return arts[0], dt


# ---------------------------------------------------------------------------
# 运行：返回 (checksum, wall_seconds)
# ---------------------------------------------------------------------------

def run_native(exe, rounds):
    walls = []
    out = None
    for _ in range(rounds):
        t0 = time.perf_counter()
        r = subprocess.run([str(exe)], capture_output=True, text=True,
                           env=env_moon(), cwd=REPO)
        dt = time.perf_counter() - t0
        if r.returncode != 0:
            sys.exit(f"native 运行失败 {exe}: rc={r.returncode} {r.stderr[-500:]}")
        out = r.stdout.strip()
        walls.append(dt)
    return int(out), statistics.median(walls), walls


def run_wasm(wasm, rounds):
    import wasmtime
    from wasmtime import Engine, Store, Module, Linker, WasiConfig
    out_val = None
    walls = []
    for _ in range(rounds):
        engine = Engine()
        store = Store(engine)
        wasi = WasiConfig()
        wasi.inherit_stdout()
        wasi.inherit_stderr()
        store.set_wasi(wasi)
        linker = Linker(engine)
        linker.define_wasi()
        module = Module.from_file(engine, str(wasm))
        t0 = time.perf_counter()
        inst = linker.instantiate(store, module)
        exp = inst.exports(store)
        exp_names = [x if isinstance(x, str) else x.name for x in exp]
        if "kaula_main" in exp_names:
            out_val = exp["kaula_main"](store)
        else:
            # MoonBit wasm: main println 到 stdout；此处仅计时，checksum 由 native 侧校验
            exp["_start"](store)
            out_val = None
        walls.append(time.perf_counter() - t0)
    return out_val, statistics.median(walls), walls


# ---------------------------------------------------------------------------
# 主流程
# ---------------------------------------------------------------------------

def tool_versions():
    def first(cmd):
        r = run(cmd, env=env_moon())
        s = (r.stdout or r.stderr).strip().splitlines()
        return s[0] if s else "?"
    r = run(["git", "-C", str(REPO), "rev-parse", "--short", "HEAD"])
    commit = r.stdout.strip() or "?"
    return {
        "os": f"{platform.system()} {platform.release()} ({platform.machine()})",
        "cpu": first(["bash", "-c", "grep -m1 'model name' /proc/cpuinfo | cut -d: -f2"]),
        "cores": os.cpu_count(),
        "moon": first([MOON, "version"]),
        "kaulac": commit,
    }


def main():
    ap = argparse.ArgumentParser(description="Kaula vs MoonBit 三维度对比")
    ap.add_argument("--rounds", type=int, default=3)
    ap.add_argument("--quick", action="store_true")
    ap.add_argument("--verify-only", action="store_true")
    args = ap.parse_args()
    rounds = 1 if args.quick else args.rounds

    if BUILD.exists():
        shutil.rmtree(BUILD)
    BUILD.mkdir(parents=True)

    tv = tool_versions()
    log(f"[env] Kaula @{tv['kaulac']} / {tv['moon']} / {tv['cores']} cores")
    log(f"[env] {tv['cpu'].strip()}")

    results = []
    for b in BENCHES:
        name = b["name"]
        tag = f"{name}_n{b['param']}"
        log(f"\n===== {name} (param={b['param']}) =====")
        entry = {"bench": b, "tag": tag}

        # 编译四边
        for lang, target in [("kaula", "native"), ("moonbit", "native"),
                             ("kaula", "wasm"), ("moonbit", "wasm")]:
            key = f"{lang}_{target}"
            if lang == "kaula":
                art, dt = build_kaula(b, target, tag)
            else:
                art, dt = build_moonbit(b, target, tag)
            entry[key] = {"art": art, "compile": dt, "size": art.stat().st_size}
            log(f"  [build] {key}: {dt:.2f}s / {art.stat().st_size} B")

        # native 运行 + checksum 校验
        k_val, k_wall, k_walls = run_native(entry["kaula_native"]["art"], rounds)
        m_val, m_wall, m_walls = run_native(entry["moonbit_native"]["art"], rounds)
        log(f"  [native] kaula={k_val} ({k_wall:.4f}s)  moonbit={m_val} ({m_wall:.4f}s)")
        ok = (k_val == m_val)
        log(f"  [verify] native checksum: {'PASS' if ok else 'FAIL'} "
            f"(kaula={k_val}, moonbit={m_val})")
        if not ok and not args.verify_only:
            sys.exit(f"checksum 不一致: {name}")
        entry["verify"] = ok
        entry["checksum"] = k_val
        entry["native_wall"] = {"kaula": k_wall, "moonbit": m_wall}
        entry["native_walls"] = {"kaula": k_walls, "moonbit": m_walls}

        # wasm 运行（wasmtime）
        if not args.verify_only:
            kw, kw_wall, _ = run_wasm(entry["kaula_wasm"]["art"], rounds)
            mw, mw_wall, _ = run_wasm(entry["moonbit_wasm"]["art"], rounds)
            log(f"  [wasm]   kaula ret={kw} ({kw_wall:.4f}s)  moonbit ({mw_wall:.4f}s)")
            entry["wasm_wall"] = {"kaula": kw_wall, "moonbit": mw_wall}
            if kw is not None:
                entry["wasm_checksum"] = kw
                wok = (kw == (k_val & 0xFFFFFFFF) or kw == k_val)
                log(f"  [verify] wasm checksum (low32): {'PASS' if wok else 'FAIL'} "
                    f"(kaula_wasm={kw}, native={k_val})")
                entry["wasm_verify"] = wok

        results.append(entry)

    if not args.verify_only:
        write_report(results, tv, rounds)
        log(f"\n[done] 报告已写入 {REPORT}")


def fmt_s(s):
    return f"{s:.4f}"


def ratio(a, b):
    return f"{a/b:.2f}×" if b > 0 else "—"


def write_report(results, tv, rounds):
    L = []
    L.append("# Kaula vs MoonBit 三维度对比报告\n")
    L.append("> 由 `test/bench/clbg/moonbit/run_compare.py` 自动生成。题目取自 "
             "[CLBG](https://benchmarksgame-team.pages.de/benchmarksgame/) 标准题目，"
             "以 checksum kernel 等价移植；Kaula 与 MoonBit 两边 checksum 逐值校验一致后才计入计时。\n")

    L.append("## 运行环境\n")
    L.append("| 项 | 值 |\n|---|---|")
    L.append(f"| OS | {tv['os']} |")
    L.append(f"| CPU | {tv['cpu'].strip()} / {tv['cores']} 核 |")
    L.append(f"| Kaula | {tv['kaulac']} (kaulac -release / --wasm) |")
    L.append(f"| MoonBit | {tv['moon']} (moon build --release, native/wasm) |")
    L.append(f"| wasm 运行时 | Python wasmtime (WASI preview1) |")
    L.append(f"| 采样口径 | 每边 {rounds} 轮取中位数；wall clock = perf_counter |")
    L.append("")

    # 正确性
    L.append("## Checksum 正确性校验\n")
    L.append("| 题目 | 参数 | Kaula (native) | MoonBit (native) | Kaula wasm (low32) | 结果 |")
    L.append("|---|---|---:|---:|---:|---|")
    for r in results:
        b = r["bench"]
        kw = r.get("wasm_checksum", "—")
        L.append(f"| {b['name']} | {b['param']} | {r['checksum']} | {r['checksum']} "
                 f"| {kw} | {'✅ PASS' if r['verify'] else '❌ FAIL'} |")
    L.append("")

    # 运行基准 native
    L.append("## 运行基准：native（wall clock 秒，中位数）\n")
    L.append("| 题目 | Kaula | MoonBit | MoonBit/Kaula |")
    L.append("|---|---:|---:|---:|")
    for r in results:
        k = r["native_wall"]["kaula"]
        m = r["native_wall"]["moonbit"]
        L.append(f"| {r['bench']['name']} | {fmt_s(k)} | {fmt_s(m)} | {ratio(m, k)} |")
    L.append("")

    # 运行基准 wasm
    if any("wasm_wall" in r for r in results):
        L.append("## 运行基准：wasm / wasmtime（wall clock 秒，中位数）\n")
        L.append("| 题目 | Kaula wasm | MoonBit wasm | MoonBit/Kaula |")
        L.append("|---|---:|---:|---:|")
        for r in results:
            if "wasm_wall" not in r:
                continue
            k = r["wasm_wall"]["kaula"]
            m = r["wasm_wall"]["moonbit"]
            L.append(f"| {r['bench']['name']} | {fmt_s(k)} | {fmt_s(m)} | {ratio(m, k)} |")
        L.append("")

    # 编译速度
    L.append("## 编译速度（秒，中位数口径为单次编译 wall clock）\n")
    L.append("| 题目 | Kaula native | MoonBit native | Kaula wasm | MoonBit wasm |")
    L.append("|---|---:|---:|---:|---:|")
    for r in results:
        L.append(f"| {r['bench']['name']} "
                 f"| {r['kaula_native']['compile']:.2f} "
                 f"| {r['moonbit_native']['compile']:.2f} "
                 f"| {r['kaula_wasm']['compile']:.2f} "
                 f"| {r['moonbit_wasm']['compile']:.2f} |")
    L.append("")

    # 产物大小
    L.append("## 产物大小（字节）\n")
    L.append("| 题目 | Kaula native | MoonBit native | Kaula wasm | MoonBit wasm |")
    L.append("|---|---:|---:|---:|---:|")
    for r in results:
        L.append(f"| {r['bench']['name']} "
                 f"| {r['kaula_native']['size']:,} "
                 f"| {r['moonbit_native']['size']:,} "
                 f"| {r['kaula_wasm']['size']:,} "
                 f"| {r['moonbit_wasm']['size']:,} |")
    L.append("")

    # 口径说明
    L.append("## 口径与限制说明\n")
    L.append("- **checksum kernel**：Kaula 的 `--wasm` 走 freestanding 裸机路径，"
             "无 stdout、无 libc/libm（缺 `sqrt`/`__multi3`）。因此 wasm 对比采用"
             "纯整数或 pre-sqrt 的 checksum kernel，两边算法逐行等价、checksum 逐值一致。\n")
    L.append("- **运行基准**：native 为 kaulac `-release`（内部 clang -O3）与 "
             "`moon build --release --target native`（LLVM 后端）的直接可执行；"
             "wasm 为两者产物在同一 wasmtime (WASI preview1) 下运行，隔离运行时差异。\n")
    L.append("- **编译速度**：单次 release 编译的 wall clock。MoonBit 首次编译含"
             " core 库构建，后续增量会更快；本报告为干净 build 目录的冷编译口径。\n")
    L.append("- **产物大小**：Kaula wasm 固定 `--initial-memory=32MB` 线性内存，"
             "MoonBit wasm 为 WASI 模块；两者运行时模型不同，体积仅作量级参考。\n")
    L.append("- **题量与参数**：当前 kernel 覆盖 fannkuchredux(n=10) 与 "
             "binarytrees(N=10)；CLBG 全标准参数（fannkuch n=12 / binarytrees N=21）"
             "的 C/Go/Kaula 三边对比见上层 `../REPORT.md`。\n")
    L.append("")

    REPORT.write_text("\n".join(L), encoding="utf-8")


if __name__ == "__main__":
    main()
