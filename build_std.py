#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Kaula C Standard Library 跨平台一键编译脚本
Usage: python build_std.py [--release] [--clean]
"""

import os
import sys
import subprocess
import argparse
import shutil
from pathlib import Path


class BuildConfig:
    """构建配置"""
    def __init__(self):
        # 路径配置
        self.project_root = Path(__file__).parent.resolve()
        self.std_dir = self.project_root / "std"
        self.src_dir = self.project_root / "src"
        self.build_dir = self.project_root / "build"
        self.output_lib = self.project_root / "std" / "libkaula_std.a"

        # 编译选项
        self.include_dirs = [str(self.std_dir), str(self.src_dir)]
        self.c_standard = "c11"

        # 跨平台优化标志
        self.debug_flags = ["-g", "-O0", "-DKMM_V4_DEBUG"]
        self.release_flags = ["-O2", "-DNDEBUG"]
        # LTO 只在最终链接时使用，不用于创建静态库
        self.lto_flags = ["-flto"]
        self.common_flags = [
            f"-std={self.c_standard}",
            "-Wall",
            "-Wextra",
            "-Wno-unused-parameter",
        ]
        # -fPIC 仅在非 Windows 平台需要
        if not sys.platform.startswith("win"):
            self.common_flags.append("-fPIC")
        else:
            # Windows: 抑制 CRT 安全函数弃用警告
            self.common_flags.append("-D_CRT_SECURE_NO_WARNINGS")

        # 跳过不需要编译的文件
        self.skip_files = set()


class CompilerDetector:
    """编译器检测器"""
    @staticmethod
    def detect():
        """检测可用的C编译器"""
        # 优先检测 GCC/Clang（对 GCC 扩展兼容性更好）
        for compiler in ["gcc", "clang", "cc"]:
            try:
                result = subprocess.run(
                    [compiler, "--version"],
                    capture_output=True,
                    text=True,
                    timeout=5
                )
                if result.returncode == 0:
                    version_line = result.stdout.split('\n')[0]
                    print(f"[+] 检测到编译器: {compiler} ({version_line})")
                    return compiler
            except (subprocess.TimeoutExpired, FileNotFoundError):
                continue

        # 回退到 MSVC (cl.exe)
        try:
            result = subprocess.run(
                ["cl", "/nologo"],
                capture_output=True,
                text=True,
                timeout=5
            )
            # cl 不带参数返回非零，但 stderr 包含错误信息表示存在
            output = (result.stderr if result.stderr else result.stdout).strip()
            if "error D8003" in output or "Compiler Version" in output or "Optimizing Compiler" in output:
                print(f"[+] 检测到编译器: MSVC cl.exe")
                return "cl"
        except (subprocess.TimeoutExpired, FileNotFoundError):
            pass

        print("[-] 错误: 未找到可用的C编译器 (gcc/clang/cc/msvc)")
        sys.exit(1)


class ArchiverDetector:
    """归档工具检测器"""
    @staticmethod
    def detect(compiler):
        """检测可用的归档工具"""
        is_clang = "clang" in compiler.lower()
        is_msvc = compiler == "cl"

        if is_msvc:
            # MSVC 使用 lib.exe
            try:
                result = subprocess.run(
                    ["lib", "/nologo", "/list"],
                    capture_output=True,
                    text=True,
                    timeout=5
                )
                # lib.exe 没有输入文件会报错，但说明存在
                print("[+] 使用归档工具: lib.exe (MSVC)")
                return "lib"
            except (subprocess.TimeoutExpired, FileNotFoundError):
                pass

        # 尝试顺序
        candidates = ["llvm-ar", "gcc-ar", "ar"]

        for ar in candidates:
            try:
                result = subprocess.run(
                    [ar, "--version"],
                    capture_output=True,
                    text=True,
                    timeout=5
                )
                if result.returncode == 0:
                    if "LLVM" in result.stdout or "llvm" in result.stdout.lower():
                        print(f"[+] 使用归档工具: {ar} (LLVM ar)")
                    else:
                        print(f"[+] 使用归档工具: {ar}")
                    return ar
            except (subprocess.TimeoutExpired, FileNotFoundError):
                continue

        # 回退：直接使用 ar
        return "ar"


class BuildSystem:
    """构建系统"""
    def __init__(self, config, release=False):
        self.config = config
        self.compiler = CompilerDetector.detect()
        self.archiver = ArchiverDetector.detect(self.compiler)
        self.is_windows = sys.platform.startswith("win")
        self.is_macos = sys.platform == "darwin"
        self.is_linux = sys.platform.startswith("linux")
        self.is_msvc = (self.compiler == "cl")

        self.release = release
        if self.is_msvc:
            # MSVC 编译标志
            self.common_flags = [
                "/nologo",
                "/std:c11",
                "/W3",
                "/utf-8",
                "/D_CRT_SECURE_NO_WARNINGS",
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

    def find_source_files(self):
        """查找所有需要编译的 C 源文件"""
        source_files = []
        for root, dirs, files in os.walk(self.config.std_dir):
            # 跳过 build 目录
            if "build" in root:
                continue
            for file in sorted(files):
                if file.endswith(".c"):
                    filepath = Path(root) / file
                    if filepath.name not in self.config.skip_files:
                        source_files.append(filepath)
        return source_files

    def compile_file(self, src_file):
        """编译单个源文件"""
        obj_file = self.config.build_dir / (src_file.stem + self.obj_ext)

        if self.is_msvc:
            cmd = [self.compiler] + self.flags
            for inc_dir in self.config.include_dirs:
                cmd.extend(["/I", inc_dir])
            cmd.extend(["/c", str(src_file), f"/Fo{obj_file}"])
        else:
            cmd = [self.compiler] + self.flags
            for inc_dir in self.config.include_dirs:
                cmd.extend(["-I", inc_dir])
            cmd.extend(["-c", str(src_file), "-o", str(obj_file)])

        try:
            result = subprocess.run(
                cmd,
                capture_output=True,
                text=True,
                timeout=60
            )
            if result.returncode != 0:
                print(f"  [-] 编译失败: {src_file.name}")
                # 显示前几行错误
                error_output = result.stderr if result.stderr else result.stdout
                if error_output:
                    error_lines = error_output.strip().split('\n')[:5]
                    for line in error_lines:
                        print(f"      {line}")
                return False
            self.obj_files.append(obj_file)
            print(f"  [✓] {src_file.name}")
            return True
        except subprocess.TimeoutExpired:
            print(f"  [-] 编译超时: {src_file.name}")
            return False

    def create_static_library(self):
        """创建静态库"""
        if not self.obj_files:
            print("[-] 错误: 没有成功编译的目标文件")
            return False

        # 根据编译器类型确定输出库文件
        if self.is_msvc:
            output_lib = self.config.project_root / "std" / "kaula_std.lib"
        else:
            output_lib = self.config.output_lib

        print(f"\n[*] 创建静态库: {output_lib}")

        # 删除旧库
        if output_lib.exists():
            output_lib.unlink()

        # 创建静态库
        if self.is_msvc:
            cmd = [self.archiver, "/nologo", f"/OUT:{output_lib}"] + [
                str(f) for f in self.obj_files
            ]
        else:
            cmd = [self.archiver, "rcs", str(output_lib)] + [
                str(f) for f in self.obj_files
            ]

        try:
            result = subprocess.run(
                cmd,
                capture_output=True,
                text=True,
                timeout=30
            )
            if result.returncode != 0:
                error_output = result.stderr if result.stderr else result.stdout
                print(f"[-] 创建静态库失败: {error_output}")
                return False

            lib_size = output_lib.stat().st_size
            print(f"[✓] 静态库创建成功! 大小: {lib_size / 1024:.1f} KB")
            return True
        except subprocess.TimeoutExpired:
            print("[-] 创建静态库超时")
            return False

    def clean(self):
        """清理构建文件"""
        if self.config.build_dir.exists():
            shutil.rmtree(self.config.build_dir)
            print(f"[+] 已清理: {self.config.build_dir}")

        if self.config.output_lib.exists():
            self.config.output_lib.unlink()
            print(f"[+] 已清理: {self.config.output_lib}")

        # 清理 MSVC 生成的 .lib
        msvc_lib = self.config.project_root / "std" / "kaula_std.lib"
        if msvc_lib.exists():
            msvc_lib.unlink()
            print(f"[+] 已清理: {msvc_lib}")

    def build(self):
        """执行完整构建"""
        # 创建构建目录
        self.config.build_dir.mkdir(parents=True, exist_ok=True)

        # 查找源文件
        source_files = self.find_source_files()
        if not source_files:
            print("[-] 错误: 未找到源文件")
            return False

        print(f"[*] 找到 {len(source_files)} 个源文件")
        print(f"[*] 编译器: {self.compiler}")
        print(f"[*] 模式: {'Release' if self.release else 'Debug'}")
        print(f"[*] 平台: {'Windows' if self.is_windows else 'macOS' if self.is_macos else 'Linux'}")
        print()

        # 编译每个文件
        success_count = 0
        fail_count = 0
        for src_file in source_files:
            if self.compile_file(src_file):
                success_count += 1
            else:
                fail_count += 1

        print(f"\n[*] 编译完成: {success_count} 成功, {fail_count} 失败")

        if fail_count > 0:
            print(f"[-] 警告: {fail_count} 个文件编译失败")

        # 创建静态库
        if success_count == 0:
            print("[-] 错误: 没有成功编译的文件，无法创建静态库")
            return False

        return self.create_static_library()


def main():
    parser = argparse.ArgumentParser(description="Kaula C 标准库一键编译脚本")
    parser.add_argument("--release", action="store_true", help="Release 模式 (默认 Debug)")
    parser.add_argument("--clean", action="store_true", help="清理构建文件")
    args = parser.parse_args()

    config = BuildConfig()
    build_system = BuildSystem(config, release=args.release)

    if args.clean:
        build_system.clean()
        return

    print("=" * 60)
    print("  Kaula C Standard Library Build System")
    print("=" * 60)
    print()

    success = build_system.build()

    print()
    if success:
        print("✅ 构建成功!")
        print(f"   输出: {config.output_lib}")
    else:
        print("❌ 构建失败!")
        sys.exit(1)


if __name__ == "__main__":
    main()
