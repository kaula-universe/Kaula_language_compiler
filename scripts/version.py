#!/usr/bin/env python3
"""
Kaula Version Generator / Kaula 版本号生成器

Generates snapshot version from git: YY.M.DD-branch-hash
Generates release version (方案 A 严格减法 / strict subtraction):
    v<MAJOR>.<MINOR>.<PATCH>
    PATCH = 仓库总提交数 (git rev-list --count HEAD) − v1.0 归档提交总数(base_total)
    减法基准以 v1.0 归档的 base_total 为唯一来源，归档在 scripts/version_history.json
    （版本基准的唯一权威来源，不依赖 git tag）。当前 minor（v1.1 起）沿用它做减法。
    每个 minor 的第 1 个提交 PATCH = 1（注意不是 0）。

切换 minor / Cut a new minor:
    python scripts/version.py --cut-minor 1.1
    在“切版提交”创建之前运行：它把旧 minor 最后一个提交的总提交数作为新 minor 的归档记录；
    随后创建的切版提交（总提交 = 基准 + 1）即 v<new>.1。

Usage / 用法:
    python scripts/version.py              # Print version / 打印版本
    python scripts/version.py --update     # Update version.json / 更新 version.json
    python scripts/version.py --json       # Output JSON / 输出 JSON
    python scripts/version.py --cut-minor 1.1  # Archive & switch minor / 归档并切换 minor

Build-generated version file / 构建生成的版本文件:
    python scripts/version.py --release --from-file   # Read release from compiler/version.json
    python scripts/version.py --snapshot --from-file  # Read snapshot from compiler/version.json
    python scripts/version.py --json --from-file      # Read all fields from compiler/version.json

    compiler/version.json 由构建脚本（toolkit_build.py）自动生成，是 CI 中
    tag / Release 展示版本号的**唯一来源**（--from-file 读取它）。
    浅克隆（fetch-depth=1）会让 git 减法失效，因此 CI 必须 fetch-depth: 0。

Output / 输出:
    Snapshot: 26.8.23-master-67ffac3
    Release:  v1.1.1
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
    return get_commit_hash(cwd)[:7]


def get_total_commit_count(cwd: Path = None) -> int:
    """Total number of commits in repository history (strict subtraction operand)."""
    count = run_git(["rev-list", "--count", "HEAD"], cwd)
    try:
        return int(count)
    except ValueError:
        return 0


def get_history_path(project_root: Path) -> Path:
    return project_root / "scripts" / "version_history.json"


def get_version_file_path(project_root: Path) -> Path:
    """Path of the build-generated version file (compiler/version.json)."""
    return project_root / "compiler" / "version.json"


def load_version_file(project_root: Path) -> dict:
    """Load the build-generated version file.

    compiler/version.json 由 toolkit_build.py / `version.py --update` 生成，
    是 CI 里 tag 与 Release 展示版本号的唯一来源。缺失或格式非法一律硬失败，
    避免静默退化成错误版本号。
    """
    path = get_version_file_path(project_root)
    if not path.exists():
        raise SystemExit(
            f"错误：版本文件不存在：{path}\n"
            "请先生成它：python scripts/version.py --update （或运行 toolkit_build.py）"
        )
    try:
        with open(path, "r", encoding="utf-8") as f:
            data = json.load(f)
    except Exception as exc:
        raise SystemExit(f"错误：版本文件解析失败：{path}（{exc}）")
    if not isinstance(data, dict) or not data.get("version"):
        raise SystemExit(f"错误：版本文件缺少 version 字段：{path}")
    return data


def is_shallow_repository(cwd: Path = None) -> bool:
    """True when the repo is a shallow clone (git subtraction is unreliable)."""
    return run_git(["rev-parse", "--is-shallow-repository"], cwd) == "true"


def load_history(project_root: Path) -> dict:
    """Load the minor baseline archive. Raises on missing/corrupt file (must exist)."""
    path = get_history_path(project_root)
    with open(path, "r", encoding="utf-8") as f:
        return json.load(f)


def save_history(project_root: Path, data: dict):
    path = get_history_path(project_root)
    path.parent.mkdir(parents=True, exist_ok=True)
    with open(path, "w", encoding="utf-8") as f:
        json.dump(data, f, indent=2, ensure_ascii=False)
        f.write("\n")


def get_current_minor(history: dict) -> str:
    return history.get("current_minor", "1.0")


BASELINE_MINOR = "1.0"


def get_base_total(history: dict, minor: str) -> int:
    """Return the subtraction baseline.

    方案 A 的减法基准以 v1.0 归档的提交总数为唯一来源（BASELINE_MINOR），
    后续 minor（v1.1 起）一律沿用它：v<minor>.N = 总提交数 − v1.0 归档值。
    这样基准只有一个权威值，不存在跨 minor 冗余失同步的问题。
    """
    for entry in history.get("versions", []):
        if entry.get("minor") == BASELINE_MINOR:
            try:
                return int(entry.get("base_total", 0))
            except (TypeError, ValueError):
                return 0
    raise KeyError(f"基准 minor {BASELINE_MINOR} 未在 version_history.json 归档")


def get_local_ahead(cwd: Path = None) -> int:
    """Commits locally ahead of origin/<branch> (for the optional +N display)."""
    branch = get_branch(cwd)
    ahead = run_git(["rev-list", "--count", f"origin/{branch}..HEAD"], cwd)
    try:
        return int(ahead)
    except ValueError:
        return 0


def cut_minor(project_root: Path, new_minor: str) -> dict:
    """Archive the old minor baseline and switch current_minor to new_minor.

    Must be run BEFORE creating the cut commit: base_total is the total commit
    count at the old minor's last commit. The cut commit (base_total + 1) then
    computes to v<new_minor>.1.
    """
    history = load_history(project_root)
    old_minor = get_current_minor(history)

    if new_minor == old_minor:
        raise SystemExit(f"当前 minor 已是 {new_minor}，无需切换")

    existing = {e.get("minor") for e in history.get("versions", [])}
    if new_minor in existing:
        raise SystemExit(f"minor {new_minor} 已归档，不能重复归档")

    # 旧 minor 最后一个提交的总提交数 = 新 minor 的归档记录（基准唯一，由 v1.0 持有）
    base_total = get_total_commit_count(project_root)

    history.setdefault("versions", []).append({
        "minor": new_minor,
        "cut_date": datetime.now().strftime("%Y-%m-%d")
    })
    history["current_minor"] = new_minor
    save_history(project_root, history)

    baseline = get_base_total(history, new_minor)
    print(f"已归档 minor：v{new_minor}（切换前 v{old_minor} 最后总提交数 {base_total}）")
    print(f"current_minor: {old_minor} -> {new_minor}")
    print("下一步：创建切版提交；提交后 v" + new_minor + ".1 = 总提交 "
          f"{base_total + 1} − 基准 {baseline}")
    return history


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


def generate_release_version(cwd: Path = None) -> tuple:
    """Generate release version via strict subtraction.
    Returns: (version_str, local_ahead)
    """
    if cwd is None:
        cwd = Path.cwd()

    project_root = cwd
    history = load_history(project_root)
    minor = get_current_minor(history)
    # 减法基准以 v1.0 归档值为唯一来源（与 minor 无关）
    base_total = get_base_total(history, minor)

    if is_shallow_repository(cwd):
        print(
            "警告：当前是浅克隆（git rev-list --count HEAD 不可靠），减法算出的版本号可能错误；"
            "CI 请使用 actions/checkout 的 fetch-depth: 0，或改用 --from-file 读取版本文件。",
            file=sys.stderr
        )

    total = get_total_commit_count(cwd)
    patch = total - base_total
    if patch < 0:
        patch = 0

    return f"v{minor}.{patch}", get_local_ahead(cwd)


def update_version_json(project_root: Path, snapshot: str, release: str, codename: str = "kaula"):
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
  python scripts/version.py --get-version  # Release version without 'v' / 无 v 前缀发布版本
  python scripts/version.py --cut-minor 1.1  # Archive & switch minor / 归档并切换 minor
  python scripts/version.py --release --from-file   # Read from compiler/version.json / 读取版本文件
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
    parser.add_argument("--get-version", action="store_true",
                        help="Print release version without 'v' prefix / 输出无 v 前缀发布版本")
    parser.add_argument("--cut-minor", metavar="MINOR", default=None,
                        help="Archive old minor baseline and switch to MINOR (e.g. 1.1) / 归档旧 minor 基准并切换")
    parser.add_argument("--codename", default="kaula",
                        help="Version codename / 版本代号 (default: kaula)")
    parser.add_argument("--from-file", action="store_true",
                        help="Read version from the build-generated compiler/version.json "
                             "instead of computing from git / 从构建生成的版本文件读取，而非 git 计算")

    args = parser.parse_args()

    project_root = Path(__file__).parent.parent.resolve()

    # --cut-minor 只负责归档与切换；切版提交创建后再用 --update 刷新 version.json
    if args.cut_minor:
        cut_minor(project_root, args.cut_minor)
        return

    if args.update and args.from_file:
        raise SystemExit("错误：--update 与 --from-file 互斥（--update 用于生成版本文件，"
                         "--from-file 用于读取已生成的版本文件）")

    if args.from_file:
        # 版本文件是构建产物，也是 CI 中 tag / Release 展示版本号的唯一来源
        data = load_version_file(project_root)
        release = data.get("version", "")
        snapshot = data.get("snapshot", "")
        codename = data.get("codename", args.codename)
        local_ahead = 0
    else:
        codename = args.codename
        snapshot = generate_snapshot_version(project_root)
        release, local_ahead = generate_release_version(project_root)

    if args.update:
        version_file = update_version_json(project_root, snapshot, release, codename)
        print(f"Updated {version_file}")
        print(f"  release:  {release}")
        print(f"  snapshot: {snapshot}")
        if local_ahead > 0:
            print(f"  ahead:    +{local_ahead}")
        return

    if args.json:
        data = {
            "release": release,
            "snapshot": snapshot,
            "codename": codename
        }
        if local_ahead > 0:
            data["ahead"] = local_ahead
        print(json.dumps(data, indent=2))
        return

    if args.snapshot:
        print(snapshot)
        return

    if args.release:
        print(release)
        return

    if args.get_version:
        print(release[1:] if release.startswith("v") else release)
        return

    # Default: print both with +N if unpushed
    ahead_str = f" +{local_ahead}" if local_ahead > 0 else ""
    print(f"kaulac {release}{ahead_str} ({snapshot})")


if __name__ == "__main__":
    main()
