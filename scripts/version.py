#!/usr/bin/env python3
"""
Kaula Version Generator / Kaula 版本号生成器

Generates snapshot version from git: YY.M.DD-branch-hash
Generates release version: v1.0.x (x = commit count since v1.0)

Usage / 用法:
    python scripts/version.py              # Print version / 打印版本
    python scripts/version.py --update     # Update version.json / 更新 version.json
    python scripts/version.py --json       # Output JSON / 输出 JSON

Output / 输出:
    Snapshot: 26.8.23-master-67ffac3
    Release:  v1.0.42
"""

import argparse
import json
import os
import subprocess
import sys
from datetime import datetime
from pathlib import Path


def run_git(args: list, cwd: Path = None) -> str:
    """Run git command and return output."""
    try:
        result = subprocess.run(
            ["git"] + args,
            cwd=cwd,
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="replace"
        )
        return result.stdout.strip()
    except Exception:
        return ""


def get_branch(cwd: Path = None) -> str:
    """Get current branch name."""
    branch = run_git(["rev-parse", "--abbrev-ref", "HEAD"], cwd)
    if not branch:
        branch = run_git(["rev-parse", "--short", "HEAD"], cwd)
    return branch or "unknown"


def get_commit_hash(cwd: Path = None) -> str:
    """Get current commit hash (full)."""
    return run_git(["rev-parse", "HEAD"], cwd) or "0000000000000000000000000000000000000000"


def get_commit_short(cwd: Path = None) -> str:
    """Get current commit hash (7 chars)."""
    full = get_commit_hash(cwd)
    return full[:7]


def get_commit_count_since_v1_0(cwd: Path = None) -> int:
    """Count commits since v1.0 tag. If tag doesn't exist, count all commits."""
    # Try to find v1.0 tag
    tag_ref = run_git(["rev-parse", "v1.0", "--quiet"], cwd)
    if tag_ref:
        # Count commits since v1.0
        count = run_git(["rev-list", "--count", "v1.0..HEAD"], cwd)
        try:
            return int(count)
        except ValueError:
            pass
    
    # Try any v1.0.x tags and use the latest
    tags = run_git(["tag", "-l", "v1.0.*"], cwd)
    if tags:
        latest_tag = sorted(tags.split("\n"))[-1]
        count = run_git(["rev-list", "--count", f"{latest_tag}..HEAD"], cwd)
        try:
            return int(count)
        except ValueError:
            pass
    
    # No v1.0 tag yet, count all commits
    count = run_git(["rev-list", "--count", "HEAD"], cwd)
    try:
        return int(count)
    except ValueError:
        return 0


def get_remote_commit_count(cwd: Path = None) -> int:
    """Get remote commit count to sync with remote."""
    # Fetch latest remote info
    run_git(["fetch", "--quiet"], cwd)
    
    remote = get_branch(cwd)
    remote_ref = run_git(["rev-parse", f"origin/{remote}", "--quiet"], cwd)
    if not remote_ref:
        return 0
    
    count = run_git(["rev-list", "--count", f"origin/{remote}"], cwd)
    try:
        return int(count)
    except ValueError:
        return 0


def generate_snapshot_version(cwd: Path = None) -> str:
    """Generate snapshot version: YY.M.DD-branch-hash"""
    if cwd is None:
        cwd = Path.cwd()
    
    now = datetime.now()
    year = now.year % 100  # Last 2 digits
    month = now.month      # No leading zero
    day = now.day          # No leading zero
    
    branch = get_branch(cwd)
    commit_hash = get_commit_short(cwd)
    
    return f"{year}.{month}.{day}-{branch}-{commit_hash}"


def generate_release_version(cwd: Path = None) -> str:
    """Generate release version: v1.0.x"""
    if cwd is None:
        cwd = Path.cwd()
    
    local_count = get_commit_count_since_v1_0(cwd)
    remote_count = get_remote_commit_count(cwd)
    
    # Use the larger of local and remote count, then +1 for the new commit
    x = max(local_count, remote_count) + 1
    
    return f"v1.0.{x}"


def update_version_json(project_root: Path, snapshot: str, release: str, codename: str = "sor-oxide"):
    """Update compiler/version.json with new versions."""
    version_file = project_root / "compiler" / "version.json"
    
    data = {
        "version": release,
        "snapshot": snapshot,
        "codename": codename
    }
    
    version_file.parent.mkdir(parents=True, exist_ok=True)
    with open(version_file, "w", encoding="utf-8") as f:
        json.dump(data, f, indent=2, ensure_ascii=False)
        f.write("\n")
    
    return version_file


def main():
    parser = argparse.ArgumentParser(
        description="Kaula Version Generator / Kaula 版本号生成器",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples / 示例:
  python scripts/version.py              # Print version / 打印版本
  python scripts/version.py --update     # Update version.json / 更新 version.json
  python scripts/version.py --json       # Output JSON / 输出 JSON
  python scripts/version.py --snapshot   # Only snapshot / 仅快照版本
  python scripts/version.py --release    # Only release / 仅发布版本
        """
    )
    parser.add_argument("--update", action="store_true",
                        help="Update compiler/version.json / 更新 version.json")
    parser.add_argument("--json", action="store_true",
                        help="Output as JSON / 输出 JSON 格式")
    parser.add_argument("--snapshot", action="store_true",
                        help="Print only snapshot version / 仅打印快照版本")
    parser.add_argument("--release", action="store_true",
                        help="Print only release version / 仅打印发布版本")
    parser.add_argument("--codename", default="sor-oxide",
                        help="Version codename / 版本代号 (default: sor-oxide)")
    
    args = parser.parse_args()
    
    project_root = Path(__file__).parent.parent.resolve()
    
    snapshot = generate_snapshot_version(project_root)
    release = generate_release_version(project_root)
    
    if args.update:
        version_file = update_version_json(project_root, snapshot, release, args.codename)
        print(f"Updated {version_file}")
        print(f"  release:  {release}")
        print(f"  snapshot: {snapshot}")
        print(f"  codename: {args.codename}")
        return
    
    if args.json:
        print(json.dumps({
            "release": release,
            "snapshot": snapshot,
            "codename": args.codename
        }, indent=2))
        return
    
    if args.snapshot:
        print(snapshot)
        return
    
    if args.release:
        print(release)
        return
    
    # Default: print both
    print(f"kaulac {release} ({snapshot})")


if __name__ == "__main__":
    main()
