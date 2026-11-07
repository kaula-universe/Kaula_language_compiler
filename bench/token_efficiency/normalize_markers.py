#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""把 solutions/* 的标记统一成"实现区 / 驱动区"两段，保证跨语言可比。

规则：
  BENCH:BEGIN ... BENCH:END        实现区：该语言实现这个函数所需的**全部**代码
                                   （含 import / #include / 类型声明 / 辅助函数）
  BENCH:MAIN:BEGIN ... END         驱动区：只为本地自检服务的 main/测试断言，不计 token

原文件里的 impl 标记会被移除外层，改由本脚本重排；结果幂等。
用法：python3 normalize_markers.py
"""
import re
from pathlib import Path

HERE = Path(__file__).resolve().parent
SOL = HERE / "solutions"

DRIVER_RE = re.compile(
    r"^(if __name__ ==|func main\(|int main\(|fn main\(|static void main\()", re.MULTILINE
)

def normalize(path: Path):
    text = path.read_text(encoding="utf-8")
    # 去掉旧标记
    pre = "#" if path.suffix == ".py" else "//"
    lines = [l for l in text.splitlines() if "BENCH:" not in l]
    body = "\n".join(lines).rstrip() + "\n"
    m = DRIVER_RE.search(body)
    if not m:
        return False
    head, driver = body[:m.start()], body[m.start():]
    # 把 import / #include / use / package 提到实现区之外，单独记为"语言前缀"。
    # 这些是每文件固定样板（且部分只被驱动区用到），计进实现区会同时污染
    # 六种语言的数字，必须同等处理才可比。
    pref = ("import ", "#include", "use ", "package ", "extern crate", "using namespace")
    head_lines = head.splitlines()
    i = 0
    while i < len(head_lines) and (not head_lines[i].strip() or head_lines[i].lstrip().startswith(pref)):
        i += 1
    prefix, body2 = head_lines[:i], head_lines[i:]
    head = "\n".join(body2) + ("\n" if body2 else "")
    new = (f"{pre} BENCH:PREFIX\n" + "\n".join(prefix) + f"\n{pre} BENCH:BEGIN\n" + head + f"{pre} BENCH:END\n\n"
           f"{pre} BENCH:MAIN:BEGIN\n" + driver + f"{pre} BENCH:MAIN:END\n")
    path.write_text(new, encoding="utf-8")
    return True

def main():
    n = 0
    for p in sorted(SOL.glob("*/*")):
        if p.suffix in {".py", ".go", ".cpp", ".c", ".rs", ".kl"}:
            normalize(p)
            n += 1
    print(f"normalized {n} files")

if __name__ == "__main__":
    main()
