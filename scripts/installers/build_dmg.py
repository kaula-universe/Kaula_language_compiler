#!/usr/bin/env python3
"""
Build .dmg package for Kaula (macOS)
"""

import os
import sys
import shutil
import subprocess
import tempfile
from pathlib import Path


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


def build_dmg(project_root: Path, build_dir: Path, release_dir: Path):
    version = get_version(project_root)
    pkg_name = f"kaula-{version}-macos-x64"
    pkg_root = release_dir / "dmg" / pkg_name

    # Clean
    if pkg_root.exists():
        shutil.rmtree(pkg_root)

    # Create directory structure
    pkg_root.mkdir(parents=True)
    (pkg_root / "Kaula.app" / "Contents" / "MacOS").mkdir(parents=True)
    (pkg_root / "Kaula.app" / "Contents" / "Resources").mkdir(parents=True)

    # Copy files to app bundle
    app_contents = pkg_root / "Kaula.app" / "Contents"

    # Copy binaries
    bin_src = build_dir / "bin"
    if bin_src.exists():
        shutil.copytree(bin_src, app_contents / "MacOS" / "bin")

    # Copy libraries
    lib_src = build_dir / "lib"
    if lib_src.exists():
        shutil.copytree(lib_src, app_contents / "MacOS" / "lib")

    # Copy headers
    include_src = build_dir / "include"
    if include_src.exists():
        shutil.copytree(include_src, app_contents / "MacOS" / "include")

    # Copy standard library
    std_src = project_root / "std"
    if std_src.exists():
        shutil.copytree(std_src, app_contents / "MacOS" / "std")

    # Copy runtime headers
    src_src = project_root / "src"
    if src_src.exists():
        shutil.copytree(src_src, app_contents / "MacOS" / "src")

    # Copy scripts
    scripts_src = project_root / "scripts"
    if scripts_src.exists():
        shutil.copytree(scripts_src, app_contents / "MacOS" / "scripts")

    # Copy docs
    docs_src = project_root / "docs"
    if docs_src.exists():
        shutil.copytree(docs_src, app_contents / "MacOS" / "docs")

    # Copy tools
    tools_src = project_root / "tools"
    if tools_src.exists():
        shutil.copytree(tools_src, app_contents / "MacOS" / "tools")

    # Copy README and LICENSE
    for f in ["README.md", "LICENSE", "toolkit_build.py"]:
        src = project_root / f
        if src.exists():
            shutil.copy2(src, app_contents / "MacOS" / f)

    # Copy logo
    logo_src = project_root / "logo.png"
    if logo_src.exists():
        shutil.copy2(logo_src, app_contents / "Resources" / "logo.png")

    # Create Info.plist
    plist_content = f"""<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleName</key>
    <string>Kaula</string>
    <key>CFBundleDisplayName</key>
    <string>Kaula Programming Language</string>
    <key>CFBundleIdentifier</key>
    <string>com.kaula.language</string>
    <key>CFBundleVersion</key>
    <string>{version}</string>
    <key>CFBundleShortVersionString</key>
    <string>{version}</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleExecutable</key>
    <string>kaulac</string>
    <key>CFBundleIconFile</key>
    <string>logo.png</string>
    <key>NSHighResolutionCapable</key>
    <true/>
    <key>LSApplicationCategoryType</key>
    <string>public.app-category.developer-tools</string>
</dict>
</plist>
"""
    (app_contents / "Info.plist").write_text(plist_content, encoding="utf-8")

    # Create symlink to /usr/local/bin
    bin_link = pkg_root / "usr" / "local" / "bin"
    bin_link.mkdir(parents=True, exist_ok=True)
    kaulac_link = bin_link / "kaulac"
    if kaulac_link.exists() or kaulac_link.is_symlink():
        kaulac_link.unlink()
    kaulac_link.symlink_to("/Applications/Kaula.app/Contents/MacOS/bin/kaulac")

    # Create DMG
    dmg_output = release_dir / "dmg" / f"{pkg_name}.dmg"
    if dmg_output.exists():
        dmg_output.unlink()

    # Use create-dmg if available, otherwise use hdiutil
    result = subprocess.run(
        ["create-dmg",
         "--volname", "Kaula",
         "--volicon", str(logo_src) if logo_src.exists() else "",
         "--window-pos", "200, 120",
         "--window-size", "600, 400",
         "--icon-size", "100",
         "--icon", "Kaula.app", "175, 190",
         "--hide-extension", "Kaula.app",
         "--app-drop-link", "425, 190",
         str(dmg_output),
         str(pkg_root)],
        capture_output=True, text=True
    )

    if result.returncode != 0:
        # Fallback to hdiutil
        temp_dmg = release_dir / "dmg" / "temp.dmg"
        result = subprocess.run(
            ["hdiutil", "create", "-srcfolder", str(pkg_root),
             "-volname", "Kaula", "-fs", "HFS+",
             "-format", "UDRW", str(temp_dmg)],
            capture_output=True, text=True
        )
        if result.returncode == 0:
            subprocess.run(
                ["hdiutil", "convert", str(temp_dmg), "-format", "UDZO",
                 "-o", str(dmg_output)],
                capture_output=True, text=True
            )
            temp_dmg.unlink(missing_ok=True)

    if dmg_output.exists():
        size_mb = dmg_output.stat().st_size / 1024 / 1024
        print(f"[OK] Created: {dmg_output} ({size_mb:.1f} MB)")
        return dmg_output

    print(f"[ERROR] Failed to create DMG")
    return None


if __name__ == "__main__":
    project_root = Path(__file__).parent.parent.parent.resolve()
    build_dir = project_root / "build"
    release_dir = project_root / "release"
    release_dir.mkdir(exist_ok=True)

    result = build_dmg(project_root, build_dir, release_dir)
    sys.exit(0 if result else 1)
