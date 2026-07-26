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
  python toolkit_build.py --target compiler  # 只构建编译器
  python toolkit_build.py --cc gcc         # 指定使用 gcc 而非默认的 clang
  python toolkit_build.py --clean          # 清理所有构建产物
  python toolkit_build.py --install-dir D:/kaula  # 自定义输出目录
"""

import os
import sys
import subprocess
import argparse
import shutil
import json
from pathlib import Path


class BuildConfig:
    """构建配置"""
    def __init__(self):
        self.project_root = Path(__file__).parent.resolve()
        self.std_dir = self.project_root / "std"
        self.src_dir = self.project_root / "src"
        self.compiler_dir = self.project_root / "kaula-compiler"
        self.pkglib_dir = self.project_root / "pkglib"
        self.runtime_dir = self.compiler_dir / "runtime"

        self.build_dir = self.project_root / "build"
        self.obj_dir = self.build_dir / "obj"
        self.bin_dir = self.build_dir / "bin"
        self.lib_dir = self.build_dir / "lib"
        self.include_dir = self.build_dir / "include"

        self.include_dirs = [
            str(self.std_dir),
            str(self.src_dir),
            str(self.runtime_dir),
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
    def detect_archiver(compiler):
        is_msvc = compiler == "cl"
        if is_msvc:
            try:
                subprocess.run(["lib", "/nologo", "/list"],
                               capture_output=True, text=True, timeout=5)
                print("[+] 归档工具: lib.exe (MSVC)")
                return "lib"
            except (subprocess.TimeoutExpired, FileNotFoundError):
                pass

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

    def _compile_one(self, src_file, out_obj=None):
        if out_obj is None:
            rel = src_file.relative_to(self.config.project_root)
            out_obj = self.config.obj_dir / (str(rel).replace(os.sep, "_") + self.obj_ext)
        out_obj.parent.mkdir(parents=True, exist_ok=True)

        if self.is_msvc:
            cmd = [self.compiler] + self.flags
            for inc in self.config.include_dirs:
                cmd.extend(["/I", inc])
            cmd.extend(["/c", str(src_file), f"/Fo{out_obj}"])
        else:
            cmd = [self.compiler] + self.flags
            for inc in self.config.include_dirs:
                cmd.extend(["-I", inc])
            cmd.extend(["-c", str(src_file), "-o", str(out_obj)])

        try:
            result = subprocess.run(cmd, capture_output=True, text=True, timeout=120)
            if result.returncode != 0:
                err = result.stderr if result.stderr else result.stdout
                print(f"  [-] 失败: {src_file.name}")
                for line in err.strip().split('\n')[:5]:
                    print(f"      {line}")
                return False
            self.obj_files.append(out_obj)
            print(f"  [\u2713] {src_file.name}")
            return True
        except subprocess.TimeoutExpired:
            print(f"  [-] 超时: {src_file.name}")
            return False

    def _make_static_lib(self, output_lib, obj_files):
        output_lib.parent.mkdir(parents=True, exist_ok=True)
        if output_lib.exists():
            output_lib.unlink()

        if self.is_msvc:
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
            print(f"[\u2713] 静态库: {output_lib} ({sz / 1024:.1f} KB)")
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
        for src in sources:
            if self._compile_one(src):
                ok += 1
            else:
                fail += 1

        if ok == 0:
            print("[-] 标准库编译全部失败")
            return False

        lib_name = "kaula_std.lib" if self.is_msvc else "libkaula_std.a"
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
        for src in sources:
            if self._compile_one(src):
                ok += 1
            else:
                fail += 1

        if ok == 0:
            print("[-] 运行时编译全部失败")
            return False

        lib_name = "kaula_runtime.lib" if self.is_msvc else "libkaula_runtime.a"
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
            (self.config.runtime_dir, "runtime"),
        ]

        for src, dest_name in targets:
            if not src.exists():
                continue
            dest = self.config.include_dir / dest_name
            if dest.exists():
                shutil.rmtree(dest)
            shutil.copytree(src, dest, ignore=shutil.ignore_patterns("*.c", "build"))
            print(f"[\u2713] 头文件: {dest_name}/")

        return True


class GoBuilder:
    """Go 代码构建器"""

    def __init__(self, config, go_cmd, release=False):
        self.config = config
        self.go_cmd = go_cmd
        self.release = release

    def _build_go_binary(self, cmd_dir, output_name):
        cmd_path = self.config.compiler_dir / "cmd" / cmd_dir
        if not cmd_path.exists():
            print(f"[-] 未找到: {cmd_path}")
            return False

        print(f"\n=== 构建 {output_name} ===")
        self.config.bin_dir.mkdir(parents=True, exist_ok=True)

        ext = ".exe" if self.config.is_windows else ""
        out_file = self.config.bin_dir / (output_name + ext)

        ldflags = ""
        if self.release:
            ldflags = "-s -w"

        env = os.environ.copy()
        env["CGO_ENABLED"] = "0"

        cmd = [self.go_cmd, "build"]
        if ldflags:
            cmd.extend(["-ldflags", ldflags])
        cmd.extend(["-o", str(out_file), "."])

        try:
            result = subprocess.run(
                cmd, capture_output=True, text=True, timeout=300,
                cwd=str(cmd_path), env=env
            )
            if result.returncode != 0:
                err = result.stderr if result.stderr else result.stdout
                print(f"[-] 构建 {output_name} 失败:")
                print(err)
                return False
            sz = out_file.stat().st_size
            print(f"[\u2713] {output_name}: {out_file} ({sz / 1024:.1f} KB)")
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
        print(f"[\u2713] stdlib.json -> {dest}")
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
    config.bin_dir.mkdir(parents=True, exist_ok=True)
    config.lib_dir.mkdir(parents=True, exist_ok=True)

    results = {}

    c_builder = CBuilder(config, c_compiler, archiver, release)

    results["std_lib"] = c_builder.build_std_lib()
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

    all_ok = all(v is not False for v in results.values())
    if all_ok:
        print("\n\u2705 构建完成!")
        print(f"   可执行文件: {config.bin_dir}")
        print(f"   静态库:     {config.lib_dir}")
        print(f"   头文件:     {config.include_dir}")
    else:
        print("\n\u274c 部分组件构建失败")

    return all_ok


def clean_all(config):
    """清理所有构建产物"""
    print("=" * 60)
    print("  清理构建产物")
    print("=" * 60)

    if config.build_dir.exists():
        shutil.rmtree(config.build_dir)
        print(f"[\u2713] 已删除: {config.build_dir}")

    std_lib_a = config.std_dir / "libkaula_std.a"
    std_lib_lib = config.std_dir / "kaula_std.lib"
    for p in [std_lib_a, std_lib_lib]:
        if p.exists():
            p.unlink()
            print(f"[\u2713] 已删除: {p}")

    obj_dir_old = config.project_root / "build"
    if obj_dir_old.exists() and obj_dir_old != config.build_dir:
        shutil.rmtree(obj_dir_old)
        print(f"[\u2713] 已删除: {obj_dir_old}")

    print("\n\u2705 清理完成!")


def main():
    parser = argparse.ArgumentParser(
        description="Kaula Toolkit 一键编译脚本",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
示例:
  python toolkit_build.py               # 构建所有组件 (Debug, 默认 clang)
  python toolkit_build.py --release     # Release 模式
  python toolkit_build.py --target std  # 只构建标准库
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
                        choices=["all", "std", "runtime", "compiler", "headers"],
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
        if args.target in ("all", "std", "runtime", "headers"):
            print("[-] 错误: 构建 C 组件需要 C 编译器")
            sys.exit(1)

    archiver = ToolDetector.detect_archiver(c_compiler) if c_compiler else None
    go_cmd = ToolDetector.detect_go()

    if args.target == "all":
        success = build_all(config, c_compiler, archiver, go_cmd, args.release)
        sys.exit(0 if success else 1)

    config.build_dir.mkdir(parents=True, exist_ok=True)
    config.obj_dir.mkdir(parents=True, exist_ok=True)
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
