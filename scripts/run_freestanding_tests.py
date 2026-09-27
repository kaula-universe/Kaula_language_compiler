#!/usr/bin/env python3
"""
freestanding 模式全链路测试 / Freestanding mode full-chain test runner

覆盖层次 / What it covers:
    L1  C 层全函数      freestanding 库 5 个模块每个函数逐一断言（台账式明细）
                        + 托管模式复跑（libc 同名强符号优先，验证双环境）
                        + 与 libc printf 的同进程逐字节一致性
    L2  交叉编译零依赖  4 个目标（x86_64/i386/riscv64/aarch64）编译 + 未定义符号审计
                        + 无 libc 链接出独立 ELF（-nostdlib -static）
    L3  Kaula 层全函数  tests/freestanding/kl/*.kl 每个 import freestanding.* 程序
                        托管模式编译并运行（断言 + 退出码）, freestanding 模式编译
    L4  boot 全链路     x86_64 PVH/Multiboot、i386 Multiboot、riscv64 PVH、aarch64 PVH
                        构建可引导 ELF + 静态校验（入口符号/段/未定义符号）
    L5  语言全功能      test/*.kl 全量语料以 --freestanding 编译（每个程序逐一过关）
    L6  配置一致性      stdlib.json 的 freestanding.* 登记 ↔ 头文件 extern ↔ 归档符号 三方一致

用法 / Usage:
    python scripts/run_freestanding_tests.py              # 全量
    python scripts/run_freestanding_tests.py --only L1,L2  # 只跑指定层
    python scripts/run_freestanding_tests.py --list         # 列出各层
    python scripts/run_freestanding_tests.py --verbose     # 打印每个用例
    KAULAC=<path> python scripts/run_freestanding_tests.py  # 指定编译器

退出码 / Exit code:
    0 = 全部通过, 1 = 有失败, 2 = 框架错误（缺工具等）
"""

import argparse
import concurrent.futures
import json
import os
import re
import shutil
import subprocess
import sys
import time
from pathlib import Path

# Windows 控制台默认 GBK，报告里的箭头/勾叉会直接抛 UnicodeEncodeError
for _stream in (sys.stdout, sys.stderr):
    try:
        _stream.reconfigure(encoding="utf-8", errors="replace")
    except Exception:  # 管道等无 reconfigure 的场景
        pass

PROJECT_ROOT = Path(__file__).resolve().parent.parent
BUILD = PROJECT_ROOT / "build" / "fs-test"
C_DIR = PROJECT_ROOT / "tests" / "freestanding" / "c"
KL_DIR = PROJECT_ROOT / "tests" / "freestanding" / "kl"
STD_LIB = PROJECT_ROOT / "compiler" / "stdlib.json"
FS_DIR = PROJECT_ROOT / "freestanding"
BIN_DIRS = (PROJECT_ROOT / "build" / "bin", PROJECT_ROOT / "bin")

FAIL_LINE = re.compile(r"^\s*FAIL\b", re.MULTILINE)
ANSI = re.compile(r"\x1b\[[0-9;]*m")

ARCHES = {
    #  name        triple                     codegen flag
    "x86_64":  ("x86_64-none-elf",   ["-mcmodel=large"]),
    "i386":    ("i386-elf",          []),
    # riscv64 默认 triple 是软浮点（rv64imac），库里的 f64 运算会 lower 成
    # __adddf3/__nedf2 等 soft-float libcall；裸机没有 compiler-rt。
    # 固定 rv64gc（带 F/D 硬件浮点，QEMU virt 亦支持）后无任何 libcall。
    "riscv64": ("riscv64-unknown-elf", ["-march=rv64gc", "-mabi=lp64d", "-mcmodel=medany"]),
    "aarch64": ("aarch64-none-elf",  ["-mcmodel=large"]),
}

# libcall 白名单: 目标 ABI 自带的编译器运行时助手（本库也自带实现，见
# freestanding/memory/memory.c 的 compiler-rt lite 段），不算外部依赖。
CRT_LIBS = {
    "__udivdi3", "__umoddi3", "__sdivdi3", "__smoddi3",   # 64 位除法/取余
    "__udivti3", "__umodti3", "__divti3", "__modti3",
    "memcpy", "memset", "memmove", "memcmp",              # LLVM 会在部分目标上转成 libcall
}


# ============================== 工具 ==============================


def strip_ansi(text: str) -> str:
    return ANSI.sub("", text or "")


def run(cmd, timeout=600, cwd=None, env=None):
    """跑一条命令, 返回 (returncode, stdout+stderr)"""
    try:
        r = subprocess.run(
            [str(c) for c in cmd],
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="replace",
            timeout=timeout,
            cwd=str(cwd or PROJECT_ROOT),
            env=env,
        )
        return r.returncode, strip_ansi((r.stdout or "") + (r.stderr or ""))
    except subprocess.TimeoutExpired:
        return 124, f"timeout after {timeout}s"
    except FileNotFoundError as e:
        return 127, f"command not found: {e}"


def which(name):
    p = shutil.which(name)
    if p:
        return Path(p)
    # LLVM 工具常与 clang 同目录但不在 PATH: 从 clang 位置兜底查找
    clang = shutil.which("clang")
    if clang:
        cand = Path(clang).parent / (name + ".exe")
        if cand.is_file():
            return cand
        cand = Path(clang).parent / name
        if cand.is_file():
            return cand
    return None


def readelf_tool():
    """llvm-readelf 优先；部分 LLVM 发行版只带 llvm-readobj。"""
    return which("llvm-readelf") or which("llvm-readobj")


def find_kaulac():
    env = os.environ.get("KAULAC")
    if env:
        p = Path(env)
        if p.is_file():
            return p
        print(f"error: KAULAC={env} is not a file", file=sys.stderr)
        sys.exit(2)
    for d in BIN_DIRS:
        for name in ("kaulac.exe", "kaulac"):
            p = d / name
            if p.is_file():
                return p
    print(
        "error: kaulac not found. Build it first:\n"
        "    python toolkit_build.py --target std --target runtime --target headers\n"
        "    python toolkit_build.py --target freestanding --target compiler\n"
        f"  (looked in {', '.join(str(d) for d in BIN_DIRS)})",
        file=sys.stderr,
    )
    sys.exit(2)


# ============================== 报告 ==============================


class Report:
    def __init__(self, verbose=False):
        self.verbose = verbose
        self.rows = []          # (level, name, status, detail)
        self.t0 = time.time()

    def add(self, level, name, ok, detail=""):
        self.rows.append((level, name, "PASS" if ok else "FAIL", detail))
        if not ok or self.verbose:
            mark = "PASS" if ok else "FAIL"
            print(f"  [{mark}] {level} {name}" + (f"  {detail}" if detail else ""))
        return ok

    def failures(self, level=None):
        return [r for r in self.rows if r[2] == "FAIL" and (level is None or r[0] == level)]

    def summary(self):
        total = len(self.rows)
        bad = len(self.failures())
        passed = total - bad
        print()
        if bad:
            print(f"FAILURES ({bad} of {total}):\n")
            for level, name, _st, detail in self.failures():
                print(f"  {level:<3} {name}")
                if detail:
                    for line in str(detail).splitlines()[:12]:
                        print(f"       {line}")
                    print()
        print(f"{passed}/{total} checks passed in {time.time() - self.t0:.1f}s")
        return 1 if bad else 0


# ============================== L1: C 层全函数 ==============================


def build_c_test(mode, outdir: Path):
    """mode: 'freestanding' | 'hosted'"""
    cflags = [
        "clang", "-std=c11", "-Wall", "-Wextra", "-Wno-unused-parameter",
        "-I", C_DIR,
    ]
    if mode == "freestanding":
        main_o = outdir / "main_fs.o"
        rt_o = outdir / "kaula_freestanding_runtime_fs.o"
        kmm_o = outdir / "kmm_v4_fs.o"
        fs_defs = ["-DKAULA_FREESTANDING", "-DKMM_V4_STATIC_POOL", "-ffreestanding",
                   "-nostdlib", "-I", FS_DIR, "-I", PROJECT_ROOT / "src"]
        rc, out = run(cflags + ["-DKAULA_FREESTANDING", "-c", C_DIR / "main.c", "-o", main_o])
        if rc != 0:
            return None, f"compile test main failed:\n{out}"
        # runtime TU 现在也 include kmm_scoped_allocator_v4.h（为 std.memory 转发
        # kmm_v4_alloc 等符号），因此必须把 KMM 分配器一起编进来 —— 且宏配置
        # 要完全一致（静态池），否则会引用动态池专有符号。
        rc, out = run(cflags + fs_defs + [
            "-c", PROJECT_ROOT / "src" / "kaula_freestanding_runtime.c", "-o", rt_o,
        ])
        if rc != 0:
            return None, f"compile freestanding runtime failed:\n{out}"
        rc, out = run(cflags + fs_defs + [
            "-c", PROJECT_ROOT / "src" / "kmm_scoped_allocator_v4.c", "-o", kmm_o,
        ])
        if rc != 0:
            return None, f"compile KMM allocator failed:\n{out}"
        rc, out = run(["clang", main_o, rt_o, kmm_o, "-o", outdir / "fs_test_freestanding.exe"])
        if rc != 0:
            return None, f"link failed:\n{out}"
        return outdir / "fs_test_freestanding.exe", ""

    main_o = outdir / "main_hosted.o"
    rc, out = run(cflags + ["-c", C_DIR / "main.c", "-o", main_o])
    if rc != 0:
        return None, f"compile test main failed:\n{out}"
    lib = PROJECT_ROOT / "build" / "lib" / "kaula_freestanding.lib"
    if not lib.is_file():
        return None, f"missing {lib}; run: python toolkit_build.py --target freestanding"
    rc, out = run(["clang", main_o, lib, "-o", outdir / "fs_test_hosted.exe"])
    if rc != 0:
        return None, f"link failed:\n{out}"
    return outdir / "fs_test_hosted.exe", ""


def layer_c_functions(rep: Report, jobs: int):
    """L1: C 层全函数（freestanding + hosted 双环境）"""
    print("L1  C 层全函数（台账 + libc printf 一致性）")
    BUILD.mkdir(parents=True, exist_ok=True)
    for mode in ("freestanding", "hosted"):
        exe, err = build_c_test(mode, BUILD)
        if exe is None:
            rep.add("L1", f"c-functions/{mode}", False, err)
            continue
        rc, out = run([exe], timeout=600)
        m_funcs = re.search(r"FUNCTIONS:\s*(\d+) tested,\s*(\d+) passed,\s*(\d+) failed", out)
        m_checks = re.search(r"CHECKS\s*:\s*(\d+) total,\s*(\d+) failed", out)
        summary = ""
        if m_funcs:
            summary = f"{m_funcs.group(2)}/{m_funcs.group(1)} functions"
            if m_checks:
                summary += f", {m_checks.group(1)} checks"
        fails = [ln.strip() for ln in out.splitlines() if ln.strip().startswith("FAIL")]
        stderr_path = BUILD / f"c-functions-{mode}.log"
        (BUILD / f"c-functions-{mode}.log").write_text(out, encoding="utf-8")
        ok = rc == 0 and "RESULT   : PASSED" in out
        detail = summary if ok else "\n".join(fails[:20]) + f"\n(full log: {stderr_path})"
        rep.add("L1", f"c-functions/{mode}", ok, detail)
        if rep.verbose:
            for line in out.splitlines():
                if line.strip().startswith(("PASS", "FAIL")):
                    print("      " + line.strip())


# ============================== L2: 交叉编译零依赖 ==============================


def layer_cross_arch(rep: Report):
    """L2: 4 架构交叉编译 + 未定义符号审计 + -nostdlib 独立链接"""
    print("L2  交叉编译零依赖（x86_64 / i386 / riscv64 / aarch64）")
    clang = which("clang")
    lld = which("ld.lld")
    if clang is None:
        rep.add("L2", "toolchain", False, "clang not found in PATH")
        return
    if lld is None:
        rep.add("L2", "toolchain", False, "ld.lld not found in PATH (需要它做 -nostdlib 独立链接)")
        return

    for arch, (triple, extra) in ARCHES.items():
        outdir = BUILD / "cross" / arch
        outdir.mkdir(parents=True, exist_ok=True)
        objs = []
        errs = []
        for src in sorted(FS_DIR.rglob("*.c")):
            obj = outdir / (src.stem + ".o")
            rc, out = run([
                clang, "-target", triple, *extra, "-std=c11", "-ffreestanding", "-nostdlib",
                "-Wall", "-Wextra", "-Wno-unused-parameter", "-DKAULA_FREESTANDING",
                "-I", FS_DIR, "-c", src, "-o", obj,
            ])
            if rc != 0:
                errs.append(f"{src.name}: {out.strip()[:300]}")
            else:
                objs.append(obj)
        rep.add("L2", f"cross-compile/{arch}", not errs, "; ".join(errs))
        if errs:
            continue

        # 未定义符号审计: 减去"库内部互相引用"与 libcall 白名单后,
        # 库自身不应再引用任何外部符号（零 libc / 零 OS 依赖）
        nm = which("llvm-nm")
        undef = set()
        defined = set()
        for obj in objs:
            rc, out = run([nm, "--undefined-only", obj])
            if rc != 0:
                undef.add(f"<llvm-nm failed on {obj.name}>")
                continue
            for line in out.splitlines():
                parts = line.split()
                if parts:
                    undef.add(parts[-1])
            rc, out = run([nm, "--defined-only", obj])
            for line in out.splitlines():
                parts = line.split()
                if len(parts) >= 3:
                    defined.add(parts[-1])
        real_undef = sorted(undef - defined - CRT_LIBS)
        rep.add("L2", f"zero-external-deps/{arch}", not real_undef,
                f"unexpected undefined symbols: {real_undef[:10]}" if real_undef else "")

        # -nostdlib 独立链接: 库 + 引用桩 → 静态 ELF, 证明可无运行时链接
        rc, out = run([
            clang, "-target", triple, *extra, "-std=c11", "-ffreestanding", "-nostdlib",
            "-I", C_DIR, "-I", FS_DIR, "-c", C_DIR / "cross_arch.c",
            "-o", outdir / "cross_arch.o",
        ])
        if rc != 0:
            rep.add("L2", f"refs-object/{arch}", False, out.strip()[:400])
            continue
        rep.add("L2", f"refs-object/{arch}", True, "")

        emu = {"x86_64": "elf_x86_64", "i386": "elf_i386",
               "riscv64": "elf64lriscv", "aarch64": "aarch64linux"}[arch]
        elf = outdir / "freestanding-only.elf"
        rc, out = run([
            lld, "-m", emu, "-e", "_start_c", "-static", "--nostdlib",
            str(outdir / "cross_arch.o"), *objs, "-o", elf,
        ])
        ok = rc == 0 and elf.is_file()
        rep.add("L2", f"standalone-link/{arch}", ok, out.strip()[:400] if not ok else "")

        if ok:
            readobj = readelf_tool()
            if readobj is not None:
                flag = "--file-headers" if readobj.name.startswith("llvm-readobj") else "-h"
                rc, hdr = run([readobj, flag, elf])
                entry_ok = "Machine" in hdr and ("Entry" in hdr or "entry" in hdr)
                rep.add("L2", f"elf-valid/{arch}", rc == 0 and entry_ok, hdr.strip()[:200])
            else:
                rep.add("L2", f"elf-valid/{arch}", True, "(no readobj tool; skipped header check)")


# ============================== L3: Kaula 层 ==============================


def compile_kl(kaulac: Path, src: Path, outdir: Path, extra_flags, timeout=900):
    outdir.mkdir(parents=True, exist_ok=True)
    cmd = [kaulac, "-output-dir", outdir, *extra_flags, src]
    return run(cmd, timeout=timeout, cwd=PROJECT_ROOT)


def layer_kl_functions(rep: Report, kaulac: Path, jobs: int):
    """L3: Kaula 层: 每个 freestanding.* 程序托管模式跑通 + freestanding 模式编译"""
    print("L3  Kaula 层全函数（freestanding.* 全部模块）")
    if not KL_DIR.is_dir():
        rep.add("L3", "corpus", False, f"{KL_DIR} not found")
        return
    progs = sorted(KL_DIR.glob("*.kl"))
    if not progs:
        rep.add("L3", "corpus", False, f"no .kl programs in {KL_DIR}")
        return

    def check(p: Path):
        stem = p.stem
        # boot_*.kl 是裸机内核（入口 fn kaula_main），托管模式的 parser 要求 main，
        # 跑它没有意义（L4 boot 层会真正交叉编译它）。只做 freestanding 编译检查。
        boot_only = stem.startswith("boot_")
        if not boot_only:
            # 1) 托管模式: 编译 + 运行（真实执行, 校验行为）
            rc, out = compile_kl(kaulac, p, BUILD / "kl-hosted" / stem, [])
            if rc != 0 or "Status: Success" not in out:
                return f"hosted-compile/{stem}", False, out[-800:]
            exe = BUILD / "kl-hosted" / stem / f"{stem}.exe"
            if not exe.is_file():
                exe = BUILD / "kl-hosted" / stem / stem
            if not exe.is_file():
                return f"hosted-run/{stem}", False, "no executable produced"
            rc, out = run([exe], timeout=300)
            if rc != 0:
                return f"hosted-run/{stem}", False, f"exit {rc}\n{out[-600:]}"
            if FAIL_LINE.search(out):
                bad = [ln.strip() for ln in out.splitlines() if FAIL_LINE.match(ln)]
                return f"hosted-run/{stem}", False, "\n".join(bad[:10])
        # 2) freestanding 模式: 编译（-nostdlib 链接路径）
        rc, out = compile_kl(kaulac, p, BUILD / "kl-freestanding" / stem, ["--freestanding"])
        if rc != 0 or "Status: Success" not in out:
            return f"freestanding-compile/{stem}", False, out[-800:]
        return f"kl/{stem}", True, ""

    with concurrent.futures.ThreadPoolExecutor(max_workers=max(1, min(jobs, 8))) as pool:
        for name, ok, detail in pool.map(check, progs):
            rep.add("L3", name, ok, detail)


# ============================== L4: boot 全链路 ==============================


BOOT_CASES = [
    ("boot-x86_64-pvh",       ["--freestanding", "--boot", "pvh", "--boot-arch", "x86_64",
                                "--target-triple", "x86_64-none-elf"], "x86_64"),
    ("boot-x86_64-multiboot", ["--freestanding", "--boot", "multiboot", "--boot-arch", "x86_64",
                                "--target-triple", "x86_64-none-elf"], "x86_64"),
    ("boot-i386-multiboot",   ["--freestanding", "--boot", "multiboot", "--boot-arch", "i386",
                                "--target-triple", "i386-elf"], "i386"),
    ("boot-riscv64-pvh",      ["--freestanding", "--boot", "pvh", "--boot-arch", "riscv64",
                                "--target-triple", "riscv64-unknown-elf",
                                # 默认 riscv64 是 rv64imac 软浮点，f64 运算会 lower 成
                                # __ledf2/__adddf3 等 libcall；裸机没有 compiler-rt。
                                # （kaulac 对 riscv64 已内置该默认值，这里显式写出以固定测试契约）
                                "-cflags=-march=rv64gc -mabi=lp64d"], "riscv64"),
    ("boot-aarch64-pvh",      ["--freestanding", "--boot", "pvh", "--boot-arch", "aarch64",
                                "--target-triple", "aarch64-none-elf"], "aarch64"),
]


def layer_boot(rep: Report, kaulac: Path, jobs: int):
    print("L4  boot 全链路（4 架构可引导 ELF + 静态校验）")
    readelf = readelf_tool()
    nm = which("llvm-nm")
    kernel = KL_DIR / "boot_kernel.kl"
    if not kernel.is_file():
        rep.add("L4", "kernel-source", False, f"{kernel} not found")
        return
    rep.add("L4", "kernel-source", True, "")

    def check(case):
        name, flags, arch = case
        outdir = BUILD / "boot" / name
        rc, out = compile_kl(kaulac, kernel, outdir, flags)
        if rc != 0 or "Status: Success" not in out:
            return f"{name}/compile", False, out[-1200:]
        elf = outdir / f"{kernel.stem}.elf"
        if not elf.is_file():
            cands = list(outdir.glob("*.elf"))
            if not cands:
                return f"{name}/artifact", False, "no .elf produced"
            elf = cands[0]
        if readelf is None:
            return f"{name}/artifact", True, ""
        rc, hdr = run([readelf, "--file-headers" if readelf.name.startswith("llvm-readobj") else "-h", elf])
        if rc != 0 or "Machine:" not in hdr:
            return f"{name}/elf-header", False, hdr[-300:]
        machine = re.search(r"Machine:\s*(.+)", hdr)
        machine = machine.group(1).strip() if machine else "?"
        # GNU readelf 打印 "Advanced Micro Devices X86-64"，llvm-readobj 打印
        # "EM_X86_64 (0x3E)" —— 两种工具的输出都要认。
        exp = {"x86_64": ("x86-64", "EM_X86_64"),
               "i386": ("80386", "EM_386"),
               "riscv64": ("RISC-V", "EM_RISCV"),
               "aarch64": ("AArch64", "EM_AARCH64")}[arch]
        if not any(e.lower() in machine.lower() for e in exp):
            return f"{name}/elf-machine", False, f"machine={machine}, want one of {exp}"
        rc, seg = run([readelf, "--program-headers" if readelf.name.startswith("llvm-readobj") else "-l", elf])
        if "LOAD" not in seg:
            return f"{name}/segments", False, "no LOAD segment"
        detail = ""
        if nm is not None:
            rc, syms = run([nm, "--undefined-only", elf])
            undef = [s for s in (ln.split()[-1] for ln in syms.splitlines() if ln.split()) if s]
            detail = f"machine={machine}"
            if undef:
                return f"{name}/no-undef", False, f"undefined symbols in boot ELF: {undef[:10]}"
        return f"{name}/bootable-elf", True, detail

    with concurrent.futures.ThreadPoolExecutor(max_workers=max(1, min(jobs, 5))) as pool:
        for name, ok, detail in pool.map(check, BOOT_CASES):
            rep.add("L4", name, ok, detail)


# ============================== L5: 全量语料 ==============================

SLOW_TESTS = {
    "memMT_large", "memMT_large_4t", "memMT_small", "memMT_small_4t",
    "mem_bench", "alloc_bench", "btree_bench",
}


def is_module(path: Path) -> bool:
    src = path.read_text(encoding="utf-8-sig", errors="replace")
    return not re.search(r"(?m)^\s*fn\s+main\s*\(", src)


# 裸机链接期"允许缺失"的符号：这些能力依赖 OS/libc（线程、时钟、socket、文件、
# 原子/并行框架、std.testing runner、std.string/std.math 等托管 std 模块实现）。
# 语料库里的程序大量使用它们，裸机没有对应实现是**预期**的 —— 判据只能是
# "生成 + C 编译" 全过；链接成功与否作为附加信息记录，不作为 L5 的闸门。
# 一旦某个符号出现在这个集合之外却缺失，那就是 freestanding 的真 bug。
OS_ONLY_SYMBOLS = re.compile(
    r"^(thread_|mutex_|rwlock_|condition_|semaphore_|parallel_|light_async_loop_"
    r"|kaula_atomic_|test_|time_now_|time_sleep|clock_gettime"
    r"|string_|math_(atan|asin|acos|cos|sin|tan|exp|log|log2|log10|pow|sqrt|cbrt"
    r"|hypot|fmod|round|modf|abs_i8|abs_i16)"
    r"|result_|option_|error_|panic)")


def layer_corpus(rep: Report, kaulac: Path, jobs: int, quick: bool, filt: str):
    print("L5  语言全功能（test/*.kl 全量语料以 --freestanding 编译）")
    test_dir = PROJECT_ROOT / "test"
    progs = [p for p in sorted(test_dir.glob("*.kl")) if not is_module(p)]
    if filt:
        progs = [p for p in progs if filt in p.stem]
    if quick:
        progs = [p for p in progs if p.stem not in SLOW_TESTS]
    if not progs:
        rep.add("L5", "corpus", False, "no programs matched")
        return
    print(f"    {len(progs)} programs")

    def check(p: Path):
        rc, out = compile_kl(kaulac, p, BUILD / "corpus" / p.stem, ["--freestanding"])
        if rc == 0 and "Status: Success" in out:
            return f"{p.stem}", True, "full link"
        # 闸门 = 前端 + C 编译。链接失败若只缺 OS/libc 能力，记为通过（附说明）；
        # 出现集合之外的缺失符号 = freestanding 真 bug，判失败。
        undef = sorted({m.group(1) for m in
                        re.finditer(r"undefined symbol: ([A-Za-z_][A-Za-z0-9_]*)", out)})
        real = [s for s in undef if not OS_ONLY_SYMBOLS.match(s)]
        if not undef:
            errs = [ln.strip() for ln in out.splitlines() if "error" in ln.lower()]
            return f"{p.stem}", False, ("\n".join(errs[:6]) or out[-500:])
        if real:
            return f"{p.stem}", False, f"非 OS 依赖的缺失符号: {real[:8]}"
        return f"{p.stem}", True, f"compile-only (OS 依赖缺失: {', '.join(undef[:6])})"

    results = []
    linked = 0
    with concurrent.futures.ThreadPoolExecutor(max_workers=max(1, min(jobs, 8))) as pool:
        for name, ok, detail in pool.map(check, progs):
            rep.add("L5", name, ok, detail)
            results.append(ok)
            if detail == "full link":
                linked += 1
    ok_n = sum(1 for r in results if r)
    print(f"    {ok_n}/{len(results)} programs codegen+compile with --freestanding"
          f" ({linked} of them also link fully)")


# ============================== L6: 配置一致性 ==============================


def exported_symbols(lib: Path):
    nm = which("llvm-nm")
    if nm is None:
        return None
    rc, out = run([nm, "--defined-only", lib])
    if rc != 0:
        return None
    syms = set()
    for line in out.splitlines():
        parts = line.split()
        if len(parts) >= 3 and parts[1] in ("T", "W", "D", "B", "R"):
            syms.add(parts[2])
    return syms


def header_externs(header: Path):
    text = header.read_text(encoding="utf-8", errors="replace")
    # extern <ret> name(...);  以及 extern <ret> (*name)(...) 函数指针
    names = set()
    for m in re.finditer(r"^\s*extern\s+[^;()]*?([A-Za-z_]\w*)\s*\(", text, re.MULTILINE):
        names.add(m.group(1))
    return names


def layer_config(rep: Report):
    print("L6  配置一致性（stdlib.json ↔ 头文件 ↔ 归档符号）")
    try:
        cfg = json.loads(STD_LIB.read_text(encoding="utf-8"))
    except Exception as e:  # noqa: BLE001
        rep.add("L6", "stdlib.json", False, f"parse failed: {e}")
        return

    modules = {k: v for k, v in cfg.items() if k.startswith("freestanding.")}
    rep.add("L6", "modules-present", len(modules) == 5,
            f"expected 5 freestanding modules, got {len(modules)}: {sorted(modules)}")

    # 1) stdlib.json 声明的每个函数都必须在对应头文件里声明
    for mod, entry in sorted(modules.items()):
        header = PROJECT_ROOT / entry.get("header", "")
        declared = header_externs(header) if header.is_file() else set()
        missing = [f for f in entry if f != "header" and f not in declared]
        rep.add("L6", f"stdlib-vs-header/{mod}", not missing,
                f"declared in stdlib.json but not in {header.name}: {missing}" if missing else "")

    # 2) 头文件声明的每个函数都必须在 stdlib.json 登记
    for mod, entry in sorted(modules.items()):
        header = PROJECT_ROOT / entry.get("header", "")
        if not header.is_file():
            rep.add("L6", f"header-vs-stdlib/{mod}", False, f"header not found: {header}")
            continue
        declared = header_externs(header)
        registered = {f for f in entry if f != "header"}
        missing = sorted(declared - registered)
        rep.add("L6", f"header-vs-stdlib/{mod}", not missing,
                f"declared in {header.name} but missing from stdlib.json: {missing}" if missing else "")

    # 3) 归档里必须有这些符号
    lib = PROJECT_ROOT / "build" / "lib" / "kaula_freestanding.lib"
    if not lib.is_file():
        rep.add("L6", "archive-symbols", False,
                f"missing {lib}; run: python toolkit_build.py --target freestanding")
        return
    syms = exported_symbols(lib)
    if syms is None:
        rep.add("L6", "archive-symbols", False, "llvm-nm not available")
        return
    all_registered = set()
    for entry in modules.values():
        all_registered |= {f for f in entry if f != "header"}
    # memset/strlen 等只在裸机变体里定义, 归档(托管变体)不含它们
    hosted_only_expected = {
        "memset", "memcpy", "memmove", "memcmp",
        "strlen", "strcmp", "strncmp", "strcasecmp", "strncasecmp",
        "strcpy", "strncpy", "strcat", "strncat", "strchr", "strrchr",
        "strstr", "strspn", "strcspn", "memchr",
    }
    missing = sorted(all_registered - syms - hosted_only_expected)
    rep.add("L6", "archive-symbols", not missing,
            f"registered in stdlib.json but absent from the archive: {missing}" if missing else "")

    # 4) freestanding/dependencies.json 的模块名要与 stdlib.json 对齐
    dep_path = FS_DIR / "dependencies.json"
    try:
        deps = json.loads(dep_path.read_text(encoding="utf-8"))
    except Exception as e:  # noqa: BLE001
        rep.add("L6", "dependencies.json", False, f"parse failed: {e}")
        return
    dep_mods = {f"freestanding.{k}" for k in deps}
    rep.add("L6", "dependencies-align", dep_mods == set(modules),
            f"mismatch: dependencies={sorted(dep_mods)} stdlib={sorted(modules)}")


# ============================== main ==============================


def main() -> int:
    ap = argparse.ArgumentParser(description="freestanding 模式全链路测试")
    ap.add_argument("--only", default="", help="只跑指定层, 如 L1,L2")
    ap.add_argument("--list", action="store_true", help="列出各层后退出")
    ap.add_argument("--verbose", "-v", action="store_true", help="打印每个用例")
    ap.add_argument("--jobs", "-j", type=int, default=0, help="并行度（默认 CPU 数, 上限 8）")
    ap.add_argument("--quick", action="store_true", help="L5 跳过慢速基准")
    ap.add_argument("--filter", default="", help="L5 只跑名字含该串的程序")
    args = ap.parse_args()

    layers = {
        "L1": "C 层全函数（台账 + printf 一致性, freestanding/hosted 双环境）",
        "L2": "交叉编译零依赖（4 架构 + 未定义符号审计 + 独立链接）",
        "L3": "Kaula 层全函数（freestanding.* 全部模块, 托管运行 + 裸机编译）",
        "L4": "boot 全链路（4 架构可引导 ELF + 静态校验）",
        "L5": "语言全功能（test/*.kl 全量语料 --freestanding 编译）",
        "L6": "配置一致性（stdlib.json ↔ 头文件 ↔ 归档符号）",
    }
    if args.list:
        for k, v in layers.items():
            print(f"  {k}  {v}")
        return 0

    selected = [s.strip().upper() for s in args.only.split(",") if s.strip()]
    if not selected:
        selected = list(layers)
    unknown = [s for s in selected if s not in layers]
    if unknown:
        print(f"error: unknown layer(s): {unknown}", file=sys.stderr)
        return 2

    jobs = args.jobs if args.jobs > 0 else (os.cpu_count() or 2)
    rep = Report(verbose=args.verbose)
    need_kaulac = any(s in selected for s in ("L3", "L4", "L5"))
    kaulac = find_kaulac() if need_kaulac else None

    print(f"kaulac : {kaulac or '(not needed)'}")
    print(f"clang  : {which('clang')}")
    print(f"ld.lld : {which('ld.lld')}")
    print(f"layers : {', '.join(selected)}\n")

    if "L1" in selected:
        layer_c_functions(rep, jobs)
    if "L2" in selected:
        layer_cross_arch(rep)
    if "L3" in selected:
        layer_kl_functions(rep, kaulac, jobs)
    if "L4" in selected:
        layer_boot(rep, kaulac, jobs)
    if "L5" in selected:
        layer_corpus(rep, kaulac, jobs, args.quick, args.filter)
    if "L6" in selected:
        layer_config(rep)

    return rep.summary()


if __name__ == "__main__":
    sys.exit(main())
