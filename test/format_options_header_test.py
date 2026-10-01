"""Verify the public API through the same Clang AST source as stdlib/analyzer.go.
Does not read or rewrite generated stdlib.json.
"""
import json
from pathlib import Path
import subprocess

root = Path(__file__).resolve().parents[1]
result = subprocess.run(
    ["clang", "-x", "c", "-std=c11", "-Xclang", "-ast-dump=json",
     "-fsyntax-only", str(root / "std/format/format.h")],
    check=True, capture_output=True, text=True,
)
ast = json.loads(result.stdout)
functions = {}


def visit(node):
    if node.get("kind") == "FunctionDecl":
        functions[node["name"]] = [
            child["type"]["qualType"].replace("_Bool", "bool") for child in node.get("inner", [])
            if child.get("kind") == "ParmVarDecl"
        ]
    for child in node.get("inner", []):
        visit(child)


visit(ast)
expected = {
    "format_options_create": [],
    "format_options_set_layout": ["FormatOptions *", "i32", "bool", "bool", "i32"],
    "format_options_set_precision": ["FormatOptions *", "i32"],
    "format_options_set_flags": ["FormatOptions *", "bool", "bool", "bool", "bool", "bool"],
    "format_int_text": ["i64", "i32", "const FormatOptions *"],
    "format_uint_text": ["u64", "i32", "const FormatOptions *"],
    "format_float_text": ["f64", "const FormatOptions *"],
    "format_uint_opts": ["char *", "u64", "u64", "i32", "const FormatOptions *"],
}
for name, params in expected.items():
    assert functions.get(name) == params, (name, functions.get(name), params)
print(f"format header: {len(expected)} public API signatures verified via Clang AST")
