#!/usr/bin/env python3
"""
Build .rpm package for Kaula
"""

import os
import sys
import shutil
import subprocess
from datetime import datetime
from pathlib import Path


def get_version(project_root: Path) -> str:
    version_script = project_root / "scripts" / "version.py"
    if version_script.exists():
        result = subprocess.run(
            [sys.executable, str(version_script), "--release", "--from-file"],
            capture_output=True, text=True, cwd=str(project_root)
        )
        if result.returncode == 0:
            return result.stdout.strip().lstrip("v")
        # 版本文件是唯一权威来源：拿不到就硬失败，绝不退化成 1.0.0 这种错版本号
        raise SystemExit(
            "获取版本号失败（构建生成的 compiler/version.json 不可用）：\n"
            + (result.stderr or result.stdout).strip()
        )
    raise SystemExit("找不到 scripts/version.py，无法获取版本号")


def build_rpm(project_root: Path, build_dir: Path, release_dir: Path):
    version = get_version(project_root)
    pkg_name = f"kaula-{version}-1.x86_64"
    pkg_root = release_dir / "rpm" / pkg_name

    # Clean
    if pkg_root.exists():
        shutil.rmtree(pkg_root)

    # Create directory structure
    pkg_root.mkdir(parents=True)
    (pkg_root / "BUILD").mkdir()
    (pkg_root / "RPMS").mkdir()
    (pkg_root / "SOURCES").mkdir()
    (pkg_root / "SPECS").mkdir()
    (pkg_root / "SRPMS").mkdir()

    # Create spec file
    spec_content = f"""Name:           kaula
Version:        {version}
Release:        1
Summary:        Kaula Programming Language
License:        Apache-2.0
URL:            https://gitee.com/zhe116/kaula
BuildArch:      x86_64

Requires:       clang >= 10.0, python3 >= 3.8

%description
Kaula is a modern systems programming language that compiles to C.
It features SOR (Sub-structural Ownership) for memory safety without
runtime overhead.

This package includes the Kaula compiler, standard library, runtime,
and development tools.

# 无源码包：这是"CI 已构建产物"的二进制 RPM，文件已由本脚本放进 BUILD 目录。
# 早先这里写了 Source0 + %setup -q，但从未生成过那个 tar.gz，
# rpmbuild 必然报 "No such file or directory ... (%prep)"。

%build
# Build is done in CI

%install
rm -rf %{{buildroot}}
mkdir -p %{{buildroot}}/usr/local/kaula
mkdir -p %{{buildroot}}/usr/local/bin
mkdir -p %{{buildroot}}/usr/share/doc/kaula
mkdir -p %{{buildroot}}/usr/share/applications

# Copy files
cp -r %{{_topdir}}/BUILD/bin %{{buildroot}}/usr/local/kaula/
cp -r %{{_topdir}}/BUILD/lib %{{buildroot}}/usr/local/kaula/
cp -r %{{_topdir}}/BUILD/include %{{buildroot}}/usr/local/kaula/
cp -r %{{_topdir}}/BUILD/std %{{buildroot}}/usr/local/kaula/
cp -r %{{_topdir}}/BUILD/src %{{buildroot}}/usr/local/kaula/
cp -r %{{_topdir}}/BUILD/scripts %{{buildroot}}/usr/local/kaula/
cp -r %{{_topdir}}/BUILD/docs %{{buildroot}}/usr/local/kaula/
cp -r %{{_topdir}}/BUILD/tools %{{buildroot}}/usr/local/kaula/

# Copy docs
cp %{{_topdir}}/BUILD/README.md %{{buildroot}}/usr/share/doc/kaula/
cp %{{_topdir}}/BUILD/LICENSE %{{buildroot}}/usr/share/doc/kaula/

# Create symlinks
ln -sf /usr/local/kaula/bin/kaulac %{{buildroot}}/usr/local/bin/kaulac

# Create .desktop file
cat > %{{buildroot}}/usr/share/applications/kaula.desktop << 'EOF'
[Desktop Entry]
Name=Kaula
Comment=Kaula Programming Language
Exec=/usr/local/kaula/bin/kaulac
Icon=/usr/local/kaula/logo.png
Type=Application
Categories=Development;IDE;
Terminal=true
EOF

%post
/sbin/ldconfig

%postun
/sbin/ldconfig

%files
%defattr(-,root,root,-)
/usr/local/kaula/*
/usr/local/bin/kaulac
/usr/share/doc/kaula/*
/usr/share/applications/kaula.desktop

%changelog
* {datetime.now().strftime("%a %b %d %Y")} Kaula Universe <kaula@example.com> - {version}-1
- Kaula {version} release
"""
    (pkg_root / "SPECS" / "kaula.spec").write_text(spec_content, encoding="utf-8")

    # Copy source files to BUILD directory
    build_dest = pkg_root / "BUILD"
    for item in ["bin", "lib", "include"]:
        src = build_dir / item
        if src.exists():
            shutil.copytree(src, build_dest / item)

    for item in ["std", "src", "scripts", "docs", "tools"]:
        src = project_root / item
        if src.exists():
            shutil.copytree(src, build_dest / item)

    for f in ["README.md", "LICENSE", "toolkit_build.py"]:
        src = project_root / f
        if src.exists():
            shutil.copy2(src, build_dest / f)

    # Build .rpm
    rpm_output = release_dir / "rpm" / f"{pkg_name}.rpm"
    if rpm_output.exists():
        rpm_output.unlink()

    result = subprocess.run(
        ["rpmbuild", "--define", f"_topdir {pkg_root}",
         "-bb", str(pkg_root / "SPECS" / "kaula.spec")],
        capture_output=True, text=True
    )

    if result.returncode == 0:
        # Find the built rpm
        rpm_files = list((pkg_root / "RPMS" / "x86_64").glob("*.rpm"))
        if rpm_files:
            shutil.copy2(rpm_files[0], rpm_output)
            size_mb = rpm_output.stat().st_size / 1024 / 1024
            print(f"[OK] Created: {rpm_output} ({size_mb:.1f} MB)")
            return rpm_output

    print(f"[ERROR] rpmbuild failed: {result.stderr}")
    return None


if __name__ == "__main__":
    project_root = Path(__file__).parent.parent.parent.resolve()
    build_dir = project_root / "build"
    release_dir = project_root / "release"
    release_dir.mkdir(exist_ok=True)

    result = build_rpm(project_root, build_dir, release_dir)
    sys.exit(0 if result else 1)
