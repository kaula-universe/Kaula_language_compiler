#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Kaula Toolkit 一键编译脚本
编译所有组件：标准库 (std.lib)、运行时、编译器 (kaulac)、格式化工具 (kaulafmt)

默认使用 clang 作为 C 编译器（跨平台一致），可通过 --cc 覆盖。

Usage:
  python toolkit_build.py                  # 默认构建所有组件 (Debug, clang)
  python toolkit_build.py --release        # Release 模式构建所有组件
  python toolkit_build.py --target std     # 只构建标准库
  python toolkit_build.py --target freestanding  # 只构建 freestanding 无依赖标准库
  python toolkit_build.py --target compiler  # 只构建编译器
  python toolkit_build.py --cc gcc         # 指定使用 gcc 而非默认的 clang
  python toolkit_build.py --clean          # 清理所有构建产物
  python toolkit_build.py --install-dir D:/kaula  # 自定义输出目录
"""

import os
import sys
import subprocess
import argparse
import hashlib
import shutil
import time
import threading
from pathlib import Path


class BuildConfig:
    """构建配置"""
    def __init__(self):
        self.project_root = Path(__file__).parent.resolve()
        self.std_dir = self.project_root / "std"
        self.src_dir = self.project_root / "src"
        self.freestanding_dir = self.project_root / "freestanding"
        self.compiler_dir = self.project_root / "kaula-compiler"
        self.pkglib_dir = self.project_root / "pkglib"
        self.runtime_dir = self.compiler_dir / "runtime"

        self.build_dir = self.project_root / "build"
        self.obj_dir = self.build_dir / "obj"
        self.hash_dir = self.build_dir / "hash"  # 增量编译缓存(每个 .c 一个 .sha256)
        self.bin_dir = self.build_dir / "bin"
        self.lib_dir = self.build_dir / "lib"
        self.include_dir = self.build_dir / "include"

        self.include_dirs = [
            str(self.std_dir),
            str(self.src_dir),
            str(self.runtime_dir),
        ]
        # freestanding runtime 专用 include 路径（顺序敏感）：
        # kaula_freestanding_runtime.c 通过 #include "memory/memory.c" 等
        # unity-include freestanding 库，必须让 freestanding 目录优先于 std 解析，
        # 否则会错误地包含 std/memory/memory.c（依赖 libc <string.h>）。
        # 该文件不需要 std 头，故不包含 std_dir。
        self.freestanding_runtime_include_dirs = [
            str(self.freestanding_dir),
            str(self.src_dir),
        ]

        self.c_standard = "c11"

        self.debug_flags = ["-g", "-O0", "-DKMM_V4_DEBUG"]
        self.release_flags = ["-O2", "-DNDEBUG"]
        self.common_flags = [
            f"-std={self.c_standard}",
            "-Wall",
            "-Wextra",
            "-Wno-unused-parameter",
            "-DKMM_THREAD_SAFETY_LEVEL=1",
        ]
        if not sys.platform.startswith("win"):
            self.common_flags.append("-fPIC")
            self.common_flags.append("-D_POSIX_C_SOURCE=200809L")
        else:
            self.common_flags.append("-D_CRT_SECURE_NO_WARNINGS")

        self.skip_files = {
            "kmm_scoped_allocator.c",
        }

        self.is_windows = sys.platform.startswith("win")
        self.is_macos = sys.platform == "darwin"
        self.is_linux = sys.platform.startswith("linux")


class ProgressBar:
    """跨平台进度条，支持终端宽度自适应，无外部依赖"""

    def __init__(self, total, prefix="", width=40, stream=None):
        self.total = max(1, total)
        self.prefix = prefix
        self.width = width
        self.stream = stream or sys.stdout
        self.current = 0
        self.start_time = time.time()
        self.last_update = 0
        self._lock = threading.Lock()
        self._visible = False
        self._last_line = ""
        self._term_width = self._get_term_width()

    def _get_term_width(self):
        try:
            return shutil.get_terminal_size().columns
        except Exception:
            return 80

    def _format_time(self, seconds):
        if seconds < 60:
            return f"{seconds:.0f}s"
        m, s = divmod(seconds, 60)
        return f"{m:.0f}m{s:.0f}s"

    def _eta(self):
        if self.current == 0:
            return "?s"
        elapsed = time.time() - self.start_time
        rate = self.current / elapsed
        remaining = (self.total - self.current) / rate
        return self._format_time(remaining)

    def _render(self):
        if self.total <= 0:
            return ""
        pct = self.current / self.total
        filled = int(self.width * pct)
        # Use ASCII characters for cross-platform compatibility (Windows GBK, etc.)
        bar = "#" * filled + "-" * (self.width - filled)
        elapsed = time.time() - self.start_time
        eta_str = self._eta()
        return f"\r{self.prefix} [{bar}] {self.current}/{self.total} ({pct*100:.0f}%) | {self._format_time(elapsed)} elapsed | ETA: {eta_str}"

    def update(self, n=1):
        with self._lock:
            self.current = min(self.current + n, self.total)
            now = time.time()
            if now - self.last_update >= 0.1 or self.current == self.total:
                self._draw()

    def _draw(self):
        line = self._render()
        if self._visible:
            self.stream.write("\r" + " " * len(self._last_line) + "\r")
        self.stream.write(line)
        self.stream.flush()
        self._last_line = line.strip()
        self._visible = True
        self.last_update = time.time()

    def finish(self, message=None):
        with self._lock:
            self.current = self.total
            if self._visible:
                self.stream.write("\r" + " " * len(self._last_line) + "\r")
            if message:
                self.stream.write(message + "\n")
            else:
                self.stream.write(self._render() + "\n")
            self.stream.flush()
            self._visible = False

    def __enter__(self):
        return self

    def __exit__(self, *args):
        self.finish()


class ToolDetector:
    """工具链检测器"""

    @staticmethod
    def detect_c_compiler(preferred="clang"):
        """检测 C 编译器，默认使用 clang（跨平台一致）。

        preferred: 用户指定的编译器名称（默认 "clang"）。
                   若该编译器可用则直接使用；不可用时回退到自动检测。
        """
        # 1) 优先验证用户指定 / 默认的编译器
        if preferred:
            try:
                result = subprocess.run(
                    [preferred, "--version"],
                    capture_output=True, text=True, timeout=5
                )
                if result.returncode == 0:
                    version_line = result.stdout.split('\n')[0]
                    print(f"[+] C 编译器: {preferred} ({version_line})")
                    return preferred
            except (subprocess.TimeoutExpired, FileNotFoundError):
                print(f"[-] 警告: 编译器 '{preferred}' 不可用，回退到自动检测")

        # 2) 回退检测链：clang -> gcc -> cc（跨平台一致）
        for compiler in ["clang", "gcc", "cc"]:
            try:
                result = subprocess.run(
                    [compiler, "--version"],
                    capture_output=True, text=True, timeout=5
                )
                if result.returncode == 0:
                    version_line = result.stdout.split('\n')[0]
                    print(f"[+] C 编译器: {compiler} ({version_line})")
                    return compiler
            except (subprocess.TimeoutExpired, FileNotFoundError):
                continue

        # 3) Windows 最后回退：MSVC cl.exe
        try:
            result = subprocess.run(
                ["cl", "/nologo"],
                capture_output=True, text=True, timeout=5
            )
            output = (result.stderr if result.stderr else result.stdout).strip()
            if "error D8003" in output or "Compiler Version" in output:
                print(f"[+] C 编译器: MSVC cl.exe")
                return "cl"
        except (subprocess.TimeoutExpired, FileNotFoundError):
            pass

        print("[-] 错误: 未找到可用的 C 编译器 (clang/gcc/cc/msvc)")
        print("    建议: 安装 LLVM/Clang 以获得跨平台一致的构建体验")
        return None

    @staticmethod
    def detect_archiver(is_windows):
        if is_windows:
            for archiver in ["llvm-lib", "lib"]:
                try:
                    subprocess.run([archiver, "/?"],
                                   capture_output=True, text=True, timeout=5)
                    print(f"[+] 归档工具: {archiver} (COFF)")
                    return archiver
                except (subprocess.TimeoutExpired, FileNotFoundError):
                    continue

            print("[-] 错误: Windows 构建需要 llvm-lib 或 lib.exe")
            return None

        for ar in ["llvm-ar", "gcc-ar", "ar"]:
            try:
                result = subprocess.run(
                    [ar, "--version"],
                    capture_output=True, text=True, timeout=5
                )
                if result.returncode == 0:
                    print(f"[+] 归档工具: {ar}")
                    return ar
            except (subprocess.TimeoutExpired, FileNotFoundError):
                continue
        return "ar"

    @staticmethod
    def detect_go():
        try:
            result = subprocess.run(
                ["go", "version"],
                capture_output=True, text=True, timeout=5
            )
            if result.returncode == 0:
                version_line = result.stdout.strip()
                print(f"[+] Go 编译器: {version_line}")
                return "go"
        except (subprocess.TimeoutExpired, FileNotFoundError):
            pass
        print("[-] 警告: 未找到 Go 编译器，将跳过 Go 组件构建")
        return None


class CBuilder:
    """C 代码构建器"""

    def __init__(self, config, compiler, archiver, release=False):
        self.config = config
        self.compiler = compiler
        self.archiver = archiver
        self.release = release
        self.is_msvc = (compiler == "cl")

        if self.is_msvc:
            self.common_flags = [
                "/nologo", "/std:c11", "/W3", "/utf-8",
                "/D_CRT_SECURE_NO_WARNINGS", "/DKMM_THREAD_SAFETY_LEVEL=1",
            ]
            self.debug_flags = ["/Od", "/Zi", "/DKMM_V4_DEBUG"]
            self.release_flags = ["/O2", "/DNDEBUG"]
            self.flags = self.common_flags + (
                self.release_flags if release else self.debug_flags
            )
            self.obj_ext = ".obj"
        else:
            self.flags = self.config.common_flags + (
                self.config.release_flags if release else self.config.debug_flags
            )
            self.obj_ext = ".o"

        self.obj_files = []

    def _find_c_files(self, root_dir, skip_dirs=None):
        if skip_dirs is None:
            skip_dirs = {"build"}
        sources = []
        for root, dirs, files in os.walk(root_dir):
            dirs[:] = [d for d in dirs if d not in skip_dirs]
            for f in sorted(files):
                if f.endswith(".c") and f not in self.config.skip_files:
                    sources.append(Path(root) / f)
        return sources

    def _compile_one(self, src_file, out_obj=None, include_dirs=None):
        """编译单个 .c 文件。

        include_dirs: 自定义 -I 路径列表（用于 kaula_freestanding_runtime.c 等
        顺序敏感的特殊文件）。None 时使用 self.config.include_dirs。
        """
        rel = src_file.relative_to(self.config.project_root)
        rel_key = str(rel).replace(os.sep, "_")
        if out_obj is None:
            out_obj = self.config.obj_dir / (rel_key + self.obj_ext)
        out_obj.parent.mkdir(parents=True, exist_ok=True)

        if include_dirs is None:
            include_dirs = self.config.include_dirs

        # 增量编译:hash = sha256(src | flags | include_dirs),命中且 .o 存在则跳过
        # 注意:include_dirs 参与哈希,保证更换 -I 顺序时会触发重建
        hash_path = self.config.hash_dir / (rel_key + ".sha256")
        hash_input = "\n".join(self.flags) + "\n-I\n" + "\n-I ".join(include_dirs)
        try:
            digest = hashlib.sha256(
                src_file.read_bytes() + b"\0" + hash_input.encode()
            ).hexdigest()
        except OSError:
            digest = None

        if digest and out_obj.exists() and hash_path.exists():
            if hash_path.read_text(encoding="ascii").strip() == digest:
                self.obj_files.append(out_obj)
                print(f"  [cached]  {src_file.name}")
                return True

        if self.is_msvc:
            cmd = [self.compiler] + self.flags
            for inc in include_dirs:
                cmd.extend(["/I", inc])
            cmd.extend(["/c", str(src_file), f"/Fo{out_obj}"])
        else:
            cmd = [self.compiler] + self.flags
            for inc in include_dirs:
                cmd.extend(["-I", inc])
            cmd.extend(["-c", str(src_file), "-o", str(out_obj)])

        try:
            result = subprocess.run(cmd, capture_output=True, text=True, timeout=120)
            if result.returncode != 0:
                err = result.stderr if result.stderr else result.stdout
                print(f"  [-] 失败: {src_file.name}")
                for line in err.strip().split("\n")[:5]:
                    print(f"      {line}")
                return False
            self.obj_files.append(out_obj)
            print(f"  [OK] {src_file.name}")
            if digest:
                hash_path.write_text(digest, encoding="ascii")
            return True
        except subprocess.TimeoutExpired:
            print(f"  [-] 超时: {src_file.name}")
            return False

    def _make_static_lib(self, output_lib, obj_files):
        output_lib.parent.mkdir(parents=True, exist_ok=True)
        if output_lib.exists():
            output_lib.unlink()

        if self.config.is_windows:
            cmd = [self.archiver, "/nologo", f"/OUT:{output_lib}"] + [
                str(f) for f in obj_files
            ]
        else:
            cmd = [self.archiver, "rcs", str(output_lib)] + [
                str(f) for f in obj_files
            ]

        try:
            result = subprocess.run(cmd, capture_output=True, text=True, timeout=60)
            if result.returncode != 0:
                err = result.stderr if result.stderr else result.stdout
                print(f"[-] 创建静态库失败: {err}")
                return False
            sz = output_lib.stat().st_size
            print(f"[OK] 静态库: {output_lib} ({sz / 1024:.1f} KB)")
            return True
        except subprocess.TimeoutExpired:
            print("[-] 创建静态库超时")
            return False

    def build_std_lib(self):
        """构建标准库静态库"""
        print("\n=== 构建 Kaula 标准库 ===")
        sources = self._find_c_files(self.config.std_dir)
        if not sources:
            print("[-] 未找到标准库源文件")
            return False

        print(f"[*] 找到 {len(sources)} 个标准库源文件")

        old_objs = list(self.obj_files)
        self.obj_files = []
        ok = 0
        fail = 0
        with ProgressBar(len(sources), "编译标准库", width=40) as pbar:
            for src in sources:
                if self._compile_one(src):
                    ok += 1
                else:
                    fail += 1
                pbar.update()

        if fail > 0:
            print(f"[-] 标准库编译失败: {fail} 个文件")
            self.obj_files = old_objs
            return False

        lib_name = "kaula_std.lib" if self.config.is_windows else "libkaula_std.a"
        out_lib = self.config.lib_dir / lib_name
        result = self._make_static_lib(out_lib, self.obj_files)
        self.obj_files = old_objs + self.obj_files
        return result

    def build_freestanding_lib(self):
        """构建 freestanding 无依赖标准库静态库。

        纯 freestanding 实现，不依赖 std/ 或 src/，include 路径仅含 freestanding/。
        不定义 KAULA_FREESTANDING 宏，故 memset/memcpy/memmove/memcmp 不参与编译
        （托管安全：弱符号不会被从静态库中提取遮蔽 libc 强符号）；
        fs_alloc/fs_strdup/... 等无冲突函数始终编译。
        裸机用户链接本库后，可通过 -DKAULA_FREESTANDING 重编或覆写弱钩子使用。
        """
        print("\n=== 构建 Kaula freestanding 库 ===")
        sources = self._find_c_files(self.config.freestanding_dir)
        if not sources:
            print("[-] 未找到 freestanding 库源文件")
            return False

        print(f"[*] 找到 {len(sources)} 个 freestanding 源文件")

        old_objs = list(self.obj_files)
        self.obj_files = []
        ok = 0
        fail = 0
        # freestanding 库自包含：仅 -I freestanding/ 即可解析所有 "base/types.h" 等
        freestanding_include_dirs = [str(self.config.freestanding_dir)]
        with ProgressBar(len(sources), "编译 freestanding", width=40) as pbar:
            for src in sources:
                if self._compile_one(src, include_dirs=freestanding_include_dirs):
                    ok += 1
                else:
                    fail += 1
                pbar.update()

        if fail > 0:
            print(f"[-] freestanding 库编译失败: {fail} 个文件")
            self.obj_files = old_objs
            return False

        lib_name = "kaula_freestanding.lib" if self.config.is_windows else "libkaula_freestanding.a"
        out_lib = self.config.lib_dir / lib_name
        result = self._make_static_lib(out_lib, self.obj_files)
        self.obj_files = old_objs + self.obj_files
        return result

    def build_runtime_lib(self):
        """构建运行时库 (src/ + SOR runtime)"""
        print("\n=== 构建运行时库 ===")
        sources = []

        src_c_files = self._find_c_files(self.config.src_dir)
        sources.extend(src_c_files)

        rt_c_files = self._find_c_files(self.config.runtime_dir)
        sources.extend(rt_c_files)

        if not sources:
            print("[-] 未找到运行时源文件")
            return False

        print(f"[*] 找到 {len(sources)} 个运行时源文件")

        old_objs = list(self.obj_files)
        self.obj_files = []
        ok = 0
        fail = 0
        with ProgressBar(len(sources), "编译运行时", width=40) as pbar:
            for src in sources:
                # kaula_freestanding_runtime.c 通过 #include "memory/memory.c" 等
                # unity-include freestanding 库，必须用 freestanding 优先的 -I 顺序，
                # 否则会错误地包含 std/memory/memory.c（依赖 libc <string.h>）。
                if src.name == "kaula_freestanding_runtime.c":
                    if self._compile_one(
                        src,
                        include_dirs=self.config.freestanding_runtime_include_dirs,
                    ):
                        ok += 1
                    else:
                        fail += 1
                else:
                    if self._compile_one(src):
                        ok += 1
                    else:
                        fail += 1
                pbar.update()

        if fail > 0:
            print(f"[-] 运行时编译失败: {fail} 个文件")
            self.obj_files = old_objs
            return False

        lib_name = "kaula_runtime.lib" if self.config.is_windows else "libkaula_runtime.a"
        out_lib = self.config.lib_dir / lib_name
        result = self._make_static_lib(out_lib, self.obj_files)
        self.obj_files = old_objs + self.obj_files
        return result

    def install_headers(self):
        """安装头文件到输出目录"""
        print("\n=== 安装头文件 ===")
        self.config.include_dir.mkdir(parents=True, exist_ok=True)

        targets = [
            (self.config.src_dir, "kaula"),
            (self.config.std_dir, "std"),
            (self.config.freestanding_dir, "freestanding"),
            (self.config.runtime_dir, "runtime"),
        ]

        with ProgressBar(len(targets), "安装头文件", width=40) as pbar:
            for src, dest_name in targets:
                if not src.exists():
                    pbar.update()
                    continue
                dest = self.config.include_dir / dest_name
                if dest.exists():
                    shutil.rmtree(dest)
                shutil.copytree(src, dest, ignore=shutil.ignore_patterns("*.c", "build"))
                pbar.update()

        return True

class GoBuilder:
    """Go 代码构建器"""

    def __init__(self, config, go_cmd, release=False):
        self.config = config
        self.go_cmd = go_cmd
        self.release = release

    def _go_source_digest(self, cmd_dir):
        """计算 Go 二进制的增量 hash。

        覆盖: cmd/<name>/*.go + internal/**/*.go + go.mod/go.sum + release 标志。
        任何一个 .go 源码变化都会触发该二进制重建;只改 kaulafmt 的
        cmd 源码不会让 kaulac 重编(各自独立 digest)。
        """
        files = []
        cmd_path = self.config.compiler_dir / "cmd" / cmd_dir
        if cmd_path.exists():
            for root, _dirs, fs in os.walk(cmd_path):
                for f in sorted(fs):
                    if f.endswith(".go"):
                        files.append(Path(root) / f)
        internal_path = self.config.compiler_dir / "internal"
        if internal_path.exists():
            for root, _dirs, fs in os.walk(internal_path):
                for f in sorted(fs):
                    if f.endswith(".go"):
                        files.append(Path(root) / f)
        for name in ("go.mod", "go.sum"):
            p = self.config.compiler_dir / name
            if p.exists():
                files.append(p)

        files.sort(key=lambda p: str(p))
        h = hashlib.sha256()
        for f in files:
            try:
                h.update(f.read_bytes())
            except OSError:
                continue
            h.update(b"\0")
        h.update(("release" if self.release else "debug").encode())
        return h.hexdigest()

    def _build_go_binary(self, cmd_dir, output_name):
        cmd_path = self.config.compiler_dir / "cmd" / cmd_dir
        if not cmd_path.exists():
            print(f"[-] 未找到: {cmd_path}")
            return False

        print(f"\n=== 构建 {output_name} ===")
        self.config.bin_dir.mkdir(parents=True, exist_ok=True)

        ext = ".exe" if self.config.is_windows else ""
        out_file = self.config.bin_dir / (output_name + ext)

        # 增量编译:hash = sha256(源码 | release 标志),命中且 exe 存在则跳过
        digest = self._go_source_digest(cmd_dir)
        hash_path = self.config.hash_dir / f"go_{output_name}.sha256"
        if digest and out_file.exists() and hash_path.exists():
            if hash_path.read_text(encoding="ascii").strip() == digest:
                sz = out_file.stat().st_size
                print(f"  [cached]  {output_name}.exe (源码未变化, 跳过, {sz / 1024:.1f} KB)")
                return True

        ldflags = ""
        if self.release:
            ldflags = "-s -w"

        env = os.environ.copy()
        env["CGO_ENABLED"] = "0"

        cmd = [self.go_cmd, "build"]
        if ldflags:
            cmd.extend(["-ldflags", ldflags])
        cmd.extend(["-o", str(out_file), "."])

        # 简单的进度指示器 (Go 编译是单步完成的)
        with ProgressBar(1, f"Go 编译 {output_name}", width=30) as pbar:
            try:
                result = subprocess.run(
                    cmd, capture_output=True, text=True, timeout=300,
                    cwd=str(cmd_path), env=env
                )
                pbar.update()
                if result.returncode != 0:
                    err = result.stderr if result.stderr else result.stdout
                    print(f"[-] 构建 {output_name} 失败:")
                    print(err)
                    return False
                sz = out_file.stat().st_size
                print(f"[OK] {output_name}: {out_file} ({sz / 1024:.1f} KB)")
                if digest:
                    self.config.hash_dir.mkdir(parents=True, exist_ok=True)
                    hash_path.write_text(digest, encoding="ascii")
                return True
            except subprocess.TimeoutExpired:
                print(f"[-] 构建 {output_name} 超时")
                return False

    def build_kaulac(self):
        return self._build_go_binary("kaulac", "kaulac")

    def build_kaulafmt(self):
        return self._build_go_binary("kaulafmt", "kaulafmt")

    def install_stdlib_json(self):
        """复制 stdlib.json 到输出目录"""
        src = self.config.compiler_dir / "stdlib.json"
        if not src.exists():
            print("[-] 未找到 stdlib.json")
            return False
        self.config.bin_dir.mkdir(parents=True, exist_ok=True)
        dest = self.config.bin_dir / "stdlib.json"
        shutil.copy2(src, dest)
        print(f"[OK] stdlib.json -> {dest}")
        return True


def build_all(config, c_compiler, archiver, go_cmd, release=False):
    """构建所有组件"""
    print("=" * 60)
    print("  Kaula Toolkit 一键构建系统")
    print("=" * 60)
    print(f"  模式: {'Release' if release else 'Debug'}")
    print(f"  平台: {'Windows' if config.is_windows else 'macOS' if config.is_macos else 'Linux'}")
    print(f"  输出目录: {config.build_dir}")
    print("=" * 60)

    config.build_dir.mkdir(parents=True, exist_ok=True)
    config.obj_dir.mkdir(parents=True, exist_ok=True)
    config.hash_dir.mkdir(parents=True, exist_ok=True)
    config.bin_dir.mkdir(parents=True, exist_ok=True)
    config.lib_dir.mkdir(parents=True, exist_ok=True)

    results = {}

    c_builder = CBuilder(config, c_compiler, archiver, release)

    results["std_lib"] = c_builder.build_std_lib()
    results["freestanding_lib"] = c_builder.build_freestanding_lib()
    results["runtime_lib"] = c_builder.build_runtime_lib()
    results["headers"] = c_builder.install_headers()

    if go_cmd:
        go_builder = GoBuilder(config, go_cmd, release)
        results["kaulac"] = go_builder.build_kaulac()
        results["kaulafmt"] = go_builder.build_kaulafmt()
        results["stdlib_json"] = go_builder.install_stdlib_json()
    else:
        results["kaulac"] = None
        results["kaulafmt"] = None
        results["stdlib_json"] = None

    print("\n" + "=" * 60)
    print("  构建结果汇总")
    print("=" * 60)
    for name, ok in results.items():
        if ok is None:
            status = "跳过"
        elif ok:
            status = "成功"
        else:
            status = "失败"
        print(f"  {name:20s}: {status}")
    print("=" * 60)

    failed_components = [name for name, ok in results.items() if ok is False]
    all_ok = not failed_components
    if all_ok:
        print("\n[OK] 构建完成!")
        print(f"   可执行文件: {config.bin_dir}")
        print(f"   静态库:     {config.lib_dir}")
        print(f"   头文件:     {config.include_dir}")
    else:
        print("\n[FAIL] 部分组件构建失败")
        print(f"   失败组件: {', '.join(failed_components)}")

    return all_ok


def clean_all(config):
    """清理所有构建产物"""
    print("=" * 60)
    print("  清理构建产物")
    print("=" * 60)

    if config.build_dir.exists():
        shutil.rmtree(config.build_dir)
        print(f"[OK] 已删除: {config.build_dir}")

    std_lib_a = config.std_dir / "libkaula_std.a"
    std_lib_lib = config.std_dir / "kaula_std.lib"
    for p in [std_lib_a, std_lib_lib]:
        if p.exists():
            p.unlink()
            print(f"[OK] 已删除: {p}")

    obj_dir_old = config.project_root / "build"
    if obj_dir_old.exists() and obj_dir_old != config.build_dir:
        shutil.rmtree(obj_dir_old)
        print(f"[OK] 已删除: {obj_dir_old}")

    print("\n[OK] 清理完成!")


def main():
    parser = argparse.ArgumentParser(
        description="Kaula Toolkit 一键编译脚本",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
示例:
  python toolkit_build.py               # 构建所有组件 (Debug, 默认 clang)
  python toolkit_build.py --release     # Release 模式
  python toolkit_build.py --target std  # 只构建标准库
  python toolkit_build.py --target freestanding  # 只构建 freestanding 无依赖标准库
  python toolkit_build.py --target compiler  # 只构建 Go 编译器和格式化工具
  python toolkit_build.py --cc gcc      # 使用 gcc 替代默认 clang
  python toolkit_build.py --clean       # 清理所有构建产物
        """
    )
    parser.add_argument("--release", action="store_true",
                        help="Release 模式 (默认 Debug)")
    parser.add_argument("--clean", action="store_true",
                        help="清理所有构建产物")
    parser.add_argument("--target",
                        choices=["all", "std", "freestanding", "runtime", "compiler", "headers"],
                        default="all",
                        help="指定构建目标 (默认: all)")
    parser.add_argument("--install-dir", type=str, default=None,
                        help="自定义输出目录 (默认: build/)")
    parser.add_argument("--cc", type=str, default="clang",
                        help="指定 C 编译器 (默认: clang，跨平台一致)")
    args = parser.parse_args()

    config = BuildConfig()
    if args.install_dir:
        config.build_dir = Path(args.install_dir).resolve()
        config.obj_dir = config.build_dir / "obj"
        config.bin_dir = config.build_dir / "bin"
        config.lib_dir = config.build_dir / "lib"
        config.include_dir = config.build_dir / "include"

    if args.clean:
        clean_all(config)
        return

    c_compiler = ToolDetector.detect_c_compiler(args.cc)
    if not c_compiler:
        if args.target in ("all", "std", "freestanding", "runtime", "headers"):
            print("[-] 错误: 构建 C 组件需要 C 编译器")
            sys.exit(1)

    archiver = ToolDetector.detect_archiver(config.is_windows) if c_compiler else None
    go_cmd = ToolDetector.detect_go()

    if args.target == "all":
        success = build_all(config, c_compiler, archiver, go_cmd, args.release)
        sys.exit(0 if success else 1)

    config.build_dir.mkdir(parents=True, exist_ok=True)
    config.obj_dir.mkdir(parents=True, exist_ok=True)
    config.hash_dir.mkdir(parents=True, exist_ok=True)
    config.bin_dir.mkdir(parents=True, exist_ok=True)
    config.lib_dir.mkdir(parents=True, exist_ok=True)

    c_builder = CBuilder(config, c_compiler, archiver, args.release) if c_compiler else None
    go_builder = GoBuilder(config, go_cmd, args.release) if go_cmd else None

    success = True

    if args.target == "std":
        if c_builder:
            success = c_builder.build_std_lib()
        else:
            print("[-] 错误: 构建 std 需要 C 编译器")
            success = False

    elif args.target == "freestanding":
        if c_builder:
            success = c_builder.build_freestanding_lib()
        else:
            print("[-] 错误: 构建 freestanding 需要 C 编译器")
            success = False

    elif args.target == "runtime":
        if c_builder:
            success = c_builder.build_runtime_lib()
        else:
            print("[-] 错误: 构建 runtime 需要 C 编译器")
            success = False

    elif args.target == "compiler":
        if go_builder:
            ok1 = go_builder.build_kaulac()
            ok2 = go_builder.build_kaulafmt()
            ok3 = go_builder.install_stdlib_json()
            success = ok1 and ok2 and ok3
        else:
            print("[-] 错误: 构建 compiler 需要 Go 编译器")
            success = False

    elif args.target == "headers":
        if c_builder:
            success = c_builder.install_headers()
        else:
            print("[-] 错误: 安装头文件需要 C 构建器")
            success = False

    sys.exit(0 if success else 1)


if __name__ == "__main__":
    main()
