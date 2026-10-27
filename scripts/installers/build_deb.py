#!/usr/bin/env python3
"""
Build .deb package for Kaula
"""

import os
import sys
import shutil
import subprocess
from pathlib import Path
from datetime import datetime


def get_version(project_root: Path) -> str:
    version_script = project_root / "scripts" / "version.py"
    if version_script.exists():
        result = subprocess.run(
            [sys.executable, str(version_script), "--release"],
            capture_output=True, text=True, cwd=str(project_root)
        )
        if result.returncode == 0:
            return result.stdout.strip().lstrip("v")
    return "1.0.0"


def build_deb(project_root: Path, build_dir: Path, release_dir: Path):
    version = get_version(project_root)
    pkg_name = f"kaula_{version}_amd64"
    pkg_root = release_dir / "deb" / pkg_name

    # Clean
    if pkg_root.exists():
        shutil.rmtree(pkg_root)

    # Create directory structure
    pkg_root.mkdir(parents=True)
    (pkg_root / "DEBIAN").mkdir()
    (pkg_root / "usr" / "local" / "kaula").mkdir(parents=True)
    (pkg_root / "usr" / "local" / "bin").mkdir(parents=True)
    (pkg_root / "usr" / "share" / "doc" / "kaula").mkdir(parents=True)
    (pkg_root / "usr" / "share" / "applications").mkdir(parents=True)

    # Copy files
    kaula_dest = pkg_root / "usr" / "local" / "kaula"

    # Binaries
    bin_src = build_dir / "bin"
    if bin_src.exists():
        shutil.copytree(bin_src, kaula_dest / "bin")

    # Libraries
    lib_src = build_dir / "lib"
    if lib_src.exists():
        shutil.copytree(lib_src, kaula_dest / "lib")

    # Headers
    include_src = build_dir / "include"
    if include_src.exists():
        shutil.copytree(include_src, kaula_dest / "include")

    # Standard library
    std_src = project_root / "std"
    if std_src.exists():
        shutil.copytree(std_src, kaula_dest / "std")

    # Runtime headers
    src_src = project_root / "src"
    if src_src.exists():
        shutil.copytree(src_src, kaula_dest / "src")

    # Scripts
    scripts_src = project_root / "scripts"
    if scripts_src.exists():
        shutil.copytree(scripts_src, kaula_dest / "scripts")

    # Docs
    docs_src = project_root / "docs"
    if docs_src.exists():
        shutil.copytree(docs_src, kaula_dest / "docs")

    # Tools
    tools_src = project_root / "tools"
    if tools_src.exists():
        shutil.copytree(tools_src, kaula_dest / "tools")

    # README and LICENSE
    for f in ["README.md", "LICENSE", "toolkit_build.py"]:
        src = project_root / f
        if src.exists():
            shutil.copy2(src, kaula_dest / f)

    # Create symlinks in /usr/local/bin
    kaulac_bin = kaula_dest / "bin" / "kaulac"
    if kaulac_bin.exists():
        link_dest = pkg_root / "usr" / "local" / "bin" / "kaulac"
        if link_dest.exists() or link_dest.is_symlink():
            link_dest.unlink()
        link_dest.symlink_to("/usr/local/kaula/bin/kaulac")

    # Create control file
    control_content = f"""Package: kaula
Version: {version}
Section: devel
Priority: optional
Architecture: amd64
Depends: clang (>= 10.0), python3 (>= 3.8)
Maintainer: Kaula Universe <kaula@example.com>
Description: Kaula Programming Language
 Kaula is a modern systems programming language that compiles to C.
 It features SOR (Sub-structural Ownership) for memory safety without
 runtime overhead.
 .
 This package includes the Kaula compiler, standard library, runtime,
 and development tools.
"""
    (pkg_root / "DEBIAN" / "control").write_text(control_content, encoding="utf-8")

    # Create postinst script
    postinst_content = """#!/bin/bash
set -e

# Update library cache
if [ -x /sbin/ldconfig ]; then
    /sbin/ldconfig
fi

echo "Kaula installed successfully!"
echo "Run 'kaulac --version' to verify installation."
"""
    postinst_path = pkg_root / "DEBIAN" / "postinst"
    postinst_path.write_text(postinst_content, encoding="utf-8")
    postinst_path.chmod(0o755)

    # Create prerm script
    prerm_content = """#!/bin/bash
set -e

# Remove symlinks
rm -f /usr/local/bin/kaulac

echo "Removing Kaula..."
"""
    prerm_path = pkg_root / "DEBIAN" / "prerm"
    prerm_path.write_text(prerm_content, encoding="utf-8")
    prerm_path.chmod(0o755)

    # Create .desktop file
    desktop_content = """[Desktop Entry]
Name=Kaula
Comment=Kaula Programming Language
Exec=/usr/local/kaula/bin/kaulac
Icon=/usr/local/kaula/logo.png
Type=Application
Categories=Development;IDE;
Terminal=true
"""
    (pkg_root / "usr" / "share" / "applications" / "kaula.desktop").write_text(
        desktop_content, encoding="utf-8"
    )

    # Copy logo if exists
    logo_src = project_root / "logo.png"
    if logo_src.exists():
        shutil.copy2(logo_src, kaula_dest / "logo.png")

    # Build .deb
    deb_output = release_dir / "deb" / f"{pkg_name}.deb"
    if deb_output.exists():
        deb_output.unlink()

    result = subprocess.run(
        ["dpkg-deb", "--build", str(pkg_root), str(deb_output)],
        capture_output=True, text=True
    )

    if result.returncode == 0:
        size_mb = deb_output.stat().st_size / 1024 / 1024
        print(f"[OK] Created: {deb_output} ({size_mb:.1f} MB)")
        return deb_output
    else:
        print(f"[ERROR] dpkg-deb failed: {result.stderr}")
        return None


if __name__ == "__main__":
    project_root = Path(__file__).parent.parent.parent.resolve()
    build_dir = project_root / "build"
    release_dir = project_root / "release"
    release_dir.mkdir(exist_ok=True)

    result = build_deb(project_root, build_dir, release_dir)
    sys.exit(0 if result else 1)
