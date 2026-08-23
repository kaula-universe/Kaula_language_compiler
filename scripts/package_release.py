#!/usr/bin/env python3
"""
Kaula Release Packager / Kaula 发布包打包工具

为当前平台创建发布包：
- Windows: kaula-windows-x64.zip
- Linux: kaula-linux-x64.tar.gz
- macOS: kaula-macos-x64.tar.gz

Usage / 用法:
    python scripts/package_release.py                  # 打包当前平台
    python scripts/package_release.py --platform all   # 打包所有平台
    python scripts/package_release.py --clean          # 清理临时文件
"""

import argparse
import os
import platform
import shutil
import subprocess
import sys
import tarfile
import zipfile
from datetime import datetime
from pathlib import Path


class ReleasePackager:
    """发布包打包器"""

    def __init__(self, project_root: Path):
        self.project_root = project_root
        self.build_dir = project_root / "build"
        self.bin_dir = self.build_dir / "bin"
        self.release_dir = project_root / "release"
        self.version = self.get_version()

    def get_version(self) -> str:
        """获取当前版本号"""
        version_script = self.project_root / "scripts" / "version.py"
        if version_script.exists():
            result = subprocess.run(
                [sys.executable, str(version_script), "--release"],
                capture_output=True, text=True, cwd=str(self.project_root)
            )
            if result.returncode == 0:
                return result.stdout.strip()
        return "v1.0.0"

    def get_snapshot(self) -> str:
        """获取快照版本号"""
        version_script = self.project_root / "scripts" / "version.py"
        if version_script.exists():
            result = subprocess.run(
                [sys.executable, str(version_script), "--snapshot"],
                capture_output=True, text=True, cwd=str(self.project_root)
            )
            if result.returncode == 0:
                return result.stdout.strip()
        return "unknown"

    def get_platform_info(self) -> tuple:
        """获取平台信息: (name, arch, ext)"""
        system = platform.system().lower()
        machine = platform.machine().lower()

        if machine in ("x86_64", "amd64"):
            arch = "x64"
        elif machine in ("arm64", "aarch64"):
            arch = "arm64"
        else:
            arch = machine

        if system == "windows":
            return "windows", arch, ".zip"
        elif system == "darwin":
            return "macos", arch, ".tar.gz"
        else:
            return "linux", arch, ".tar.gz"

    def build_compiler(self) -> bool:
        """构建编译器"""
        print("[1/4] Building compiler...")

        toolkit = self.project_root / "toolkit_build.py"
        if not toolkit.exists():
            print(f"  Error: {toolkit} not found")
            return False

        result = subprocess.run(
            [sys.executable, str(toolkit), "--target", "compiler"],
            cwd=str(self.project_root)
        )
        return result.returncode == 0

    def prepare_package_dir(self) -> Path:
        """准备打包目录"""
        print("[2/4] Preparing package directory...")

        pkg_name = f"kaula-{self.version}"
        pkg_dir = self.release_dir / pkg_name

        # 清理旧目录
        if pkg_dir.exists():
            shutil.rmtree(pkg_dir)

        pkg_dir.mkdir(parents=True)

        # 复制二进制文件
        if self.bin_dir.exists():
            shutil.copytree(self.bin_dir, pkg_dir / "bin")

        # 复制必要的文件
        files_to_copy = [
            "README.md",
            "LICENSE",
            "toolkit_build.py",
        ]

        for f in files_to_copy:
            src = self.project_root / f
            if src.exists():
                shutil.copy2(src, pkg_dir / f)

        # 复制脚本
        scripts_dir = self.project_root / "scripts"
        if scripts_dir.exists():
            shutil.copytree(scripts_dir, pkg_dir / "scripts")

        # 复制调试工具
        tools_dir = self.project_root / "tools"
        if tools_dir.exists():
            shutil.copytree(tools_dir, pkg_dir / "tools")

        # 复制文档
        docs_dir = self.project_root / "docs"
        if docs_dir.exists():
            shutil.copytree(docs_dir, pkg_dir / "docs")

        # 复制标准库（头文件）
        std_dir = self.project_root / "std"
        if std_dir.exists():
            shutil.copytree(std_dir, pkg_dir / "std")

        # 复制运行时头文件
        src_dir = self.project_root / "src"
        if src_dir.exists():
            shutil.copytree(src_dir, pkg_dir / "src")

        return pkg_dir

    def create_archive(self, pkg_dir: Path) -> Path:
        """创建压缩包"""
        print("[3/4] Creating archive...")

        _, _, ext = self.get_platform_info()
        archive_name = f"{pkg_dir.name}{ext}"
        archive_path = self.release_dir / archive_name

        if ext == ".zip":
            with zipfile.ZipFile(archive_path, 'w', zipfile.ZIP_DEFLATED) as zf:
                for root, dirs, files in os.walk(pkg_dir):
                    for file in files:
                        file_path = Path(root) / file
                        arcname = file_path.relative_to(self.release_dir)
                        zf.write(file_path, arcname)
        else:
            with tarfile.open(archive_path, 'w:gz') as tf:
                tf.add(pkg_dir, arcname=pkg_dir.name)

        return archive_path

    def cleanup(self, pkg_dir: Path):
        """清理临时目录"""
        print("[4/4] Cleaning up...")
        if pkg_dir.exists():
            shutil.rmtree(pkg_dir)

    def package(self) -> Path:
        """执行打包流程"""
        sys_name, arch, ext = self.get_platform_info()
        print(f"Kaula Release Packager")
        print(f"  Version:  {self.version}")
        print(f"  Snapshot: {self.get_snapshot()}")
        print(f"  Platform: {sys_name}-{arch}")
        print(f"  Output:   {sys_name}-{arch}{ext}")
        print()

        if not self.build_compiler():
            print("Error: Build failed")
            return None

        pkg_dir = self.prepare_package_dir()
        archive_path = self.create_archive(pkg_dir)
        self.cleanup(pkg_dir)

        print()
        print(f"Release package created:")
        print(f"  {archive_path}")
        print(f"  Size: {archive_path.stat().st_size / 1024 / 1024:.1f} MB")

        return archive_path


def main():
    parser = argparse.ArgumentParser(
        description="Kaula Release Packager",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples / 示例:
  python scripts/package_release.py                  # Current platform / 当前平台
  python scripts/package_release.py --clean          # Clean release dir / 清理
        """
    )
    parser.add_argument("--clean", action="store_true",
                        help="Clean release directory / 清理发布目录")

    args = parser.parse_args()

    project_root = Path(__file__).parent.parent.resolve()
    packager = ReleasePackager(project_root)

    if args.clean:
        release_dir = project_root / "release"
        if release_dir.exists():
            shutil.rmtree(release_dir)
            print("Release directory cleaned")
        return

    packager.package()


if __name__ == "__main__":
    main()
