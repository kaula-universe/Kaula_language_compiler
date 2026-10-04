#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Kaula 并发内核 · 测试构建与运行脚本

用法：
    python test/concurrency/run_tests.py                # 全部
    python test/concurrency/run_tests.py --only deque   # 只跑名字含 deque 的
    python test/concurrency/run_tests.py --list         # 列出用例
    python test/concurrency/run_tests.py --asan         # 开启 ASan/UBSan
    python test/concurrency/run_tests.py --stress 20    # 重复 20 轮查竞态

设计：
- 每个 test_*.c 独立编译成可执行文件，链接 src/concurrency/ 下的内核。
- 按平台自动注入 KAULA_PLATFORM_* 宏与线程库。
- 退出码非 0 即失败；--stress 在首次失败时停止。
"""

import argparse
import os
import re
import shutil
import subprocess
import sys
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parents[2]
TEST_DIR = PROJECT_ROOT / "test" / "concurrency"
SRC_DIR = PROJECT_ROOT / "src"
CONC_DIR = SRC_DIR / "concurrency"
BUILD_DIR = PROJECT_ROOT / "build" / "concurrency_tests"

# kl 用例的可执行文件跑在主线程上，Windows 默认栈 1MB。
#
# test_full_concurrency 用 `Handle hs[n]` 且 n=100000 —— 10 万个 Handle 落栈，
# 必然溢出（实测 rc=0xC00000FD = STATUS_STACK_OVERFLOW，确定性复现）。
# 这里统一把 kl 用例的栈抬到 32MB，不改任何用例的规模。
#
# 这是给测试用的权宜之计，不是给 VLA 开脱：语言层面 `T v[n]` 在 n 很大时
# 迟早该有上限检查或堆提升。若编译器将来补上，这个 flag 可以撤掉。
KL_CFLAGS = "-Xlinker /STACK:33554432"


def detect_platform():
    """返回 (windows, unix, linux, macos, web, uses_pthread)"""
    if sys.platform.startswith("win"):
        return 1, 0, 0, 0, 0, False
    if sys.platform == "darwin":
        return 0, 1, 0, 1, 0, True
    if sys.platform.startswith("linux"):
        return 0, 1, 1, 0, 0, True
    # 其他 Unix 兜底
    return 0, 1, 0, 0, 0, True


def find_compiler():
    env = os.environ.get("CC")
    if env and shutil.which(env):
        return shutil.which(env)
    for name in ("clang", "gcc", "cc"):
        path = shutil.which(name)
        if path:
            return path
    print("[-] 未找到 C 编译器（clang / gcc / cc），可用 CC 环境变量指定")
    return None


def platform_defines():
    """
    只定义**为真**的平台宏，假的那个干脆不定义。

    之前一律发 -DKAULA_PLATFORM_WINDOWS={w}，在 Linux 上就成了
    -DKAULA_PLATFORM_WINDOWS=0。而 #ifdef 只看「是否定义」、不看值，
    于是 #ifdef KAULA_PLATFORM_WINDOWS 在 Linux 上照样为真 ——
    test_spawn_completion.c 因此去 include <windows.h>，直接编不过。

    不定义为假是安全的：#if KAULA_PLATFORM_WINDOWS 遇未定义标识符按 0 算，
    所以 #if 与 #ifdef 两种写法都拿到正确结果。
    """
    names = ("KAULA_PLATFORM_WINDOWS", "KAULA_PLATFORM_UNIX", "KAULA_PLATFORM_LINUX",
             "KAULA_PLATFORM_MACOS", "KAULA_PLATFORM_WEB")
    w, u, l, m, web, _ = detect_platform()
    return [f"-D{n}=1" for n, on in zip(names, (w, u, l, m, web)) if on]


def common_flags():
    flags = [
        "-std=c11",
        "-Wall",
        "-Wextra",
        "-Wno-unused-parameter",
        "-Wno-unused-function",
    ]
    if not sys.platform.startswith("win"):
        # -std=c11 会定义 __STRICT_ANSI__，glibc 据此隐藏 POSIX 声明
        # （setenv / unsetenv / pthread_sigmask / ...），结果是隐式声明报错。
        # _DEFAULT_SOURCE 正是打开这一组的标准开关。
        flags.append("-D_DEFAULT_SOURCE")
    return flags + platform_defines() + [
        "-I", str(SRC_DIR),
        "-I", str(CONC_DIR),
        "-I", str(TEST_DIR),
    ]


def clang_rt_lib_dir():
    """
    找到 clang 的 sanitizer 运行时目录（Windows）。

    ASan 默认以动态方式链接，需要 clang_rt.asan_dynamic-*.dll
    在 PATH 上，否则可执行文件启动即失败（0xC0000135 STATUS_DLL_NOT_FOUND）。
    """
    clang = shutil.which("clang") or shutil.which("clang-cl")
    if not clang:
        return None
    # <llvm>/bin/clang.exe → <llvm>/lib/clang/<ver>/lib/windows
    llvm_bin = Path(clang).resolve().parent
    clang_lib = llvm_bin.parent / "lib" / "clang"
    if not clang_lib.is_dir():
        return None
    for version in sorted(clang_lib.iterdir(), reverse=True):
        candidate = version / "lib" / "windows"
        if candidate.is_dir():
            return str(candidate)
    return None


def windows_sdk_lib_dirs():
    """
    找到 Windows SDK 的 um 库目录（含 winmm.lib）。

    内核在 Windows 上调 timeBeginPeriod 抬系统时钟分辨率，需要 winmm.lib。
    MSVC 会自动定位，但 clang 直接驱动链接器时不会，所以显式给 -L。

    注意：这里用正斜杠路径。Windows 路径里的反斜杠在 docstring 中
    会被当作转义序列，且 clang 的 -L 参数与路径必须是同一个 argv 元素，
    手工加引号会让引号进入路径本身（subprocess 已负责正确转义）。
    """
    roots = [
        Path("C:/Program Files (x86)/Windows Kits/10/Lib"),
        Path("C:/Program Files/Windows Kits/10/Lib"),
    ]
    dirs = []
    for root in roots:
        if not root.is_dir():
            continue
        for version in sorted(root.iterdir(), reverse=True):
            if not version.is_dir():
                continue
            for arch in ("x64", "x86", "arm64"):
                candidate = version / "um" / arch
                if candidate.is_dir():
                    dirs.append(str(candidate))
                    break
            if dirs:
                break
    return dirs


def link_flags():
    if sys.platform.startswith("win"):
        flags = []
        for d in windows_sdk_lib_dirs():
            flags += ["-L" + d]
        # winmm: timeBeginPeriod/timeEndPeriod，用于抬高系统时钟分辨率。
        #        不抬的话默认 15.625ms 的时钟粒度会把所有亚毫秒超时
        #        抬成一个节拍，测试里的定时精度全部失真。
        #
        # 这里没有 ws2_32：内核的 IO 执行用 read/write，不是 recv/send，
        # 所以不再需要 socket 库。测试用例也改用 pipe 造可读事件。
        flags += ["-lwinmm"]
        return flags
    return ["-pthread"]


def collect_tests(only=None):
    tests = sorted(list(TEST_DIR.glob("test_*.c")) + list(TEST_DIR.glob("test_*.kl")))
    if only:
        tests = [t for t in tests if only in t.stem]
    return tests


def runtime_sources():
    """
    内核运行时代码 TU。

    KMM 分配器是必需的：内核的 Task/帧都从它的 survivor 段分配。
    缺席的模块自动跳过，这样测试可以按模块逐个推进。
    """
    names = [
        "kaula_conc.c",
        "kaula_conc_io_primitives.c",
        "kaula_conc_io_backend.c",
        "kaula_conc_scheduler.c",
        "kaula_conc_worker.c",
        "kaula_conc_iodriver.c",
        "kaula_conc_block_lane.c",
        "kaula_conc_epoch.c",
        "kaula_conc_completion.c",
        "kaula_conc_evict.c",
        "kaula_conc_metrics.c",
        "kaula_conc_budget.c",
        "kaula_conc_task.c",
    ]
    sources = [CONC_DIR / n for n in names if (CONC_DIR / n).exists()]
    kmm = SRC_DIR / "kmm_scoped_allocator_v4.c"
    if kmm.exists():
        sources.append(kmm)
    return sources


# 编译器走管道时本不该输出 ANSI，但 errors 包的 colorEnabled 漏了 tty 检查，
# 于是 kaulac 打出的是 "Status: \x1b[32mSuccess\x1b[0m" —— 转义码夹在中间，
# 按字面匹配 "Status: Success" 永远失败，kl 用例被整片误判成编译失败。
# 这里统一剥壳再匹配，两侧都不再依赖对方是否上色。
_ANSI_RE = re.compile(r"\x1b\[[0-9;]*m")


def strip_ansi(text: str) -> str:
    return _ANSI_RE.sub("", text or "")


def build_one(cc, test_file, extra, verbose):
    BUILD_DIR.mkdir(parents=True, exist_ok=True)
    exe = BUILD_DIR / (test_file.stem + (".exe" if sys.platform.startswith("win") else ""))

    # kl 用例：走 kaulac 编译（编译器内置 conc fn/spawn/Handle 支持），
    # 产出 build/kl-hosted/<stem>/<stem>（与 run_freestanding_tests 同布局）。
    if test_file.suffix == ".kl":
        kaulac = PROJECT_ROOT / "build" / "bin" / ("kaulac.exe" if sys.platform.startswith("win") else "kaulac")
        if not kaulac.exists():
            print(f"[-] kaulac 不存在（先构建编译器）: {kaulac}")
            return None
        outdir = PROJECT_ROOT / "build" / "kl-hosted" / test_file.stem
        cmd = [str(kaulac), "-output-dir", str(outdir)]
        if KL_CFLAGS:
            cmd += ["-cflags", KL_CFLAGS]
        cmd.append(str(test_file))
        if verbose:
            print("[kaulac]", " ".join(cmd))
        proc = subprocess.run(cmd, capture_output=True, text=True,
                              encoding="utf-8", errors="replace", timeout=900)
        diag = (proc.stderr or "") + (proc.stdout or "")
        plain = strip_ansi(diag)
        if proc.returncode != 0 or "Status: Success" not in plain and "成功" not in plain:
            print(f"[-] kl 编译失败: {test_file.name}")
            print(diag[-8000:])
            return None
        exe = outdir / (test_file.stem + (".exe" if sys.platform.startswith("win") else ""))
        if not exe.is_file():
            exe = outdir / test_file.stem
        if not exe.is_file():
            print(f"[-] kl 编译未产出可执行: {test_file.name}")
            print(diag[-2000:])
            return None
        return exe

    compile_only = [str(test_file)]
    compile_only += [str(s) for s in runtime_sources()]

    # clang 在 Windows 上接受 -L<带空格路径> 的单参数形式（无需额外引号）。
    # subprocess 已在 list 形式下对每个元素做正确的命令行转义，
    # 手工加引号反而会让引号本身变成路径的一部分。
    cmd = [cc] + common_flags() + extra + compile_only + link_flags()
    cmd += ["-o", str(exe)]
    # sanitizer 需在编译与链接两处都出现
    if extra:
        cmd += extra

    if verbose:
        print("[cc]", " ".join(cmd))
    proc = subprocess.run(cmd, capture_output=True, text=True,
                          encoding="utf-8", errors="replace")
    # 注意：链接错误（LNK2019/LNK1120）出现在 stdout，不在 stderr。
    # 只看 stderr 会得到一句无信息量的 "linker command failed"。
    diag = (proc.stderr or "") + (proc.stdout or "")
    if proc.returncode != 0:
        print(f"[-] 编译/链接失败: {test_file.name}")
        print(diag[:8000])
        return None
    if "warning" in diag.lower():
        print(f"[warn] {test_file.name}:")
        print(diag[:3000])
    return exe


def run_one(exe, verbose, timeout=180, env=None):
    if verbose:
        print("[run]", exe.name)
    try:
        proc = subprocess.run([str(exe)], capture_output=True, text=True,
                              encoding="utf-8", errors="replace", timeout=timeout,
                              env=env)
    except subprocess.TimeoutExpired as e:
        # 超时也可能有输出，而且那部分输出往往正是定位的线索
        # （"挂在前面的某个用例里" 与 "跑完了才挂" 是完全不同的两个 bug）。
        # 不打出来的话，超时一律呈现为"无输出"，很容易被误读成静默崩溃。
        print(_decode(e.stdout), end="")
        print(_decode(e.stderr), end="")
        print(f"[TIMEOUT] {exe.name} 超过 {timeout}s 未结束（疑似死锁）")
        print("        ↑ 以上是被打断时已产生的输出；最后一条通常就是卡住的位置")
        return 124
    if proc.stdout:
        print(proc.stdout.rstrip())
    if proc.stderr:
        print(proc.stderr.rstrip())
    return proc.returncode


def _decode(raw):
    if raw is None:
        return ""
    if isinstance(raw, bytes):
        return raw.decode("utf-8", errors="replace")
    return raw


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--only", help="只跑名字包含该子串的用例")
    ap.add_argument("--list", action="store_true", help="列出用例")
    ap.add_argument("--verbose", "-v", action="store_true")
    ap.add_argument("--asan", action="store_true", help="开启 ASan/UBSan")
    ap.add_argument("--stress", type=int, default=1, help="重复轮数")
    ap.add_argument("--timeout", type=int, default=180, help="单用例超时秒数")
    args = ap.parse_args()

    tests = collect_tests(args.only)
    if args.list:
        for t in tests:
            print(t.stem)
        return 0
    if not tests:
        print("[-] 没有匹配的用例")
        return 1

    cc = find_compiler()
    if not cc:
        return 1

    extra = []
    run_env = None
    if args.asan:
        if sys.platform == "darwin":
            extra = ["-fsanitize=address"]
        else:
            extra = ["-fsanitize=address,undefined", "-fno-omit-frame-pointer"]
        if sys.platform.startswith("win"):
            # clang 的 ASan 是动态链接：运行时必须能找到对应 DLL
            rt = clang_rt_lib_dir()
            if rt is None:
                print("[-] 找不到 clang sanitizer 运行时目录，--asan 不可用")
                return 1
            run_env = dict(os.environ)
            run_env["PATH"] = rt + os.pathsep + run_env.get("PATH", "")
            print(f"[*] ASan 运行时: {rt}")

    w, _, l, m, _, _ = detect_platform()
    print(f"[*] 编译器   : {cc}")
    print(f"[*] 平台     : {'windows' if w else 'linux' if l else 'macos' if m else 'unix'}")
    print(f"[*] 用例     : {len(tests)} 个 x {args.stress} 轮")
    if extra:
        print(f"[*] sanitizer: {' '.join(extra)}")

    failed = []
    for round_no in range(args.stress):
        if args.stress > 1:
            print(f"\n===== 第 {round_no + 1}/{args.stress} 轮 =====")
        for t in tests:
            exe = build_one(cc, t, extra, args.verbose)
            if exe is None:
                failed.append((t.stem, "build"))
                continue
            rc = run_one(exe, args.verbose, args.timeout, run_env)
            if rc != 0:
                failed.append((t.stem, f"rc={rc}"))
        if failed:
            break

    print("\n" + "=" * 60)
    if failed:
        print("失败：")
        for name, why in failed:
            print(f"  {name}: {why}")
        return 1
    print(f"全部通过：{len(tests)} 个用例 x {args.stress} 轮")
    return 0


if __name__ == "__main__":
    sys.exit(main())