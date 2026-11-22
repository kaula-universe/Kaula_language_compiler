#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Kaula 标准库统计 / Kaula stdlib statistics.

唯一数据源：`compiler/stdlib.json`（模块键与函数签名）+ `std/` 物理目录布局。
README 与 docs 中的标准库统计应与此脚本输出保持一致。

用法 / Usage:
    python scripts/stdlib_stats.py              # 人类可读摘要
    python scripts/stdlib_stats.py --modules    # 附每模块函数数
    python scripts/stdlib_stats.py --json       # 机器可读 JSON
    python scripts/stdlib_stats.py --check      # 校验登记表并返回非零退出码（出错时）

统计口径 / Definitions:
    - 物理目录：`std/` 下的直接子目录数（含尚未登记的实现目录）。
    - 模块键：`stdlib.json` 的顶层键，如 `std.io` / `freestanding.base`。
    - 函数签名：模块内带 `args` 字段的条目（不含 `header` / `types` 元数据）。
"""

import argparse
import json
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
STDLIB_JSON = ROOT / "compiler" / "stdlib.json"
STD_DIR = ROOT / "std"


def is_function(entry):
    """模块条目是否为函数签名（而非 header/types 元数据）。"""
    return isinstance(entry, dict) and "args" in entry


def header_path(module, header):
    """把 stdlib.json 中的 header 解析为实际文件路径。

    历史登记存在两种写法：项目根相对（`std/io/io.h`、
    `freestanding/base/types.h`）与 `std/` 相对（`error/error.h`）。
    """
    if not header:
        return None
    header = header.replace("\\", "/")
    if header.startswith("std/") or header.startswith("freestanding/"):
        return ROOT / header
    return STD_DIR / header


def module_dir(module, header):
    """模块头文件所属的物理目录名。"""
    if not header:
        return None
    header = header.replace("\\", "/")
    if header.startswith("std/"):
        return header.split("/")[1]
    if header.startswith("freestanding/"):
        return None
    return header.split("/")[0]


def collect():
    config = json.loads(STDLIB_JSON.read_text(encoding="utf-8"))

    std_modules = {k: v for k, v in config.items() if k.startswith("std.")}
    fs_modules = {k: v for k, v in config.items() if k.startswith("freestanding.")}
    other_modules = {k: v for k, v in config.items() if k not in std_modules and k not in fs_modules}

    dirs = sorted(p.name for p in STD_DIR.iterdir() if p.is_dir())
    registered_dirs = set()
    for name, module in std_modules.items():
        directory = module_dir(name, module.get("header"))
        if directory:
            registered_dirs.add(directory)

    def functions(modules):
        return sum(1 for module in modules.values() for entry in module.values() if is_function(entry))

    return {
        "config": config,
        "std_modules": std_modules,
        "fs_modules": fs_modules,
        "other_modules": other_modules,
        "dirs": dirs,
        "registered_dirs": sorted(registered_dirs & set(dirs)),
        "unregistered_dirs": sorted(set(dirs) - registered_dirs),
        "functions": functions(config),
        "std_functions": functions(std_modules),
        "fs_functions": functions(fs_modules),
        "types": sum(1 for module in config.values() if isinstance(module, dict) and "types" in module),
    }


def module_rows(stats):
    rows = []
    for name, module in stats["std_modules"].items():
        rows.append((name, sum(1 for entry in module.values() if is_function(entry))))
    for name, module in stats["fs_modules"].items():
        rows.append((name, sum(1 for entry in module.values() if is_function(entry))))
    return rows


def check(stats):
    errors = []

    if not stats["std_modules"]:
        errors.append("compiler/stdlib.json has no std.* modules")
    if not stats["fs_modules"]:
        errors.append("compiler/stdlib.json has no freestanding.* modules")
    if stats["other_modules"]:
        errors.append(
            "unexpected module namespaces: " + ", ".join(sorted(stats["other_modules"]))
        )

    for name, module in stats["config"].items():
        header = module.get("header")
        if not header:
            errors.append(f"{name}: missing header")
            continue
        path = header_path(name, header)
        if path is None or not path.is_file():
            errors.append(f"{name}: header not found: {header}")
        for fn_name, entry in module.items():
            if isinstance(entry, dict) and "args" in entry and not isinstance(entry["args"], list):
                errors.append(f"{name}.{fn_name}: args is not a list")
        for dirname in [module_dir(name, header)]:
            if dirname and name.startswith("std.") and dirname not in stats["dirs"]:
                errors.append(f"{name}: physical directory std/{dirname} not found")

    total = stats["std_functions"] + stats["fs_functions"]
    if total != stats["functions"]:
        errors.append(
            f"function total mismatch: {total} != {stats['functions']}"
        )

    return errors


def print_summary(stats, show_modules):
    print("Kaula stdlib statistics")
    print(f"  std/ directories    : {len(stats['dirs'])}")
    print(f"  registered dirs     : {len(stats['registered_dirs'])}")
    if stats["unregistered_dirs"]:
        print(f"  unregistered dirs   : {', '.join(stats['unregistered_dirs'])}")
    print(
        "  modules             : "
        f"{len(stats['config'])} "
        f"(std {len(stats['std_modules'])} + freestanding {len(stats['fs_modules'])})"
    )
    print(
        "  functions           : "
        f"{stats['functions']} "
        f"(std {stats['std_functions']} + freestanding {stats['fs_functions']})"
    )
    print(f"  modules with types  : {stats['types']}")
    if show_modules:
        print()
        print(f"  {'module':24} {'functions':>9}")
        for name, count in module_rows(stats):
            print(f"  {name:24} {count:9}")


def main():
    parser = argparse.ArgumentParser(description="Kaula stdlib statistics")
    parser.add_argument("--json", action="store_true", help="output JSON")
    parser.add_argument("--modules", action="store_true", help="include per-module function counts")
    parser.add_argument("--check", action="store_true", help="validate stdlib.json and exit non-zero on errors")
    args = parser.parse_args()

    stats = collect()

    if args.json:
        payload = {
            "dirs": len(stats["dirs"]),
            "registered_dirs": len(stats["registered_dirs"]),
            "unregistered_dirs": stats["unregistered_dirs"],
            "modules": len(stats["config"]),
            "std_modules": len(stats["std_modules"]),
            "freestanding_modules": len(stats["fs_modules"]),
            "functions": stats["functions"],
            "std_functions": stats["std_functions"],
            "freestanding_functions": stats["fs_functions"],
            "types": stats["types"],
        }
        if args.modules:
            payload["modules_detail"] = dict(module_rows(stats))
        print(json.dumps(payload, ensure_ascii=False, indent=2))
    else:
        print_summary(stats, args.modules)

    if args.check:
        errors = check(stats)
        if errors:
            print("\nCHECK FAILED:", file=sys.stderr)
            for error in errors:
                print(f"  - {error}", file=sys.stderr)
            return 1
        print("\nOK: stdlib registry is consistent")
    return 0


if __name__ == "__main__":
    sys.exit(main())
