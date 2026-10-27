"""连跑 vla_handle_array 抓 DBG/ERR 错误模式"""
import os
import re
import subprocess
import sys

os.environ["CC"] = r"D:\LLVM\bin\clang.exe"
root = r"C:\Users\28507\AppData\Local\Temp\opencode\kaula"
n = int(sys.argv[1]) if len(sys.argv) > 1 else 10

errs = 0
dbg_lines = []
for i in range(1, n + 1):
    p = subprocess.run(
        [sys.executable, "test/concurrency/run_tests.py", "--only", "vla_handle_array"],
        cwd=root, capture_output=True, text=True, encoding="utf-8", errors="replace",
        timeout=600)
    out = p.stdout + p.stderr
    hits = [ln for ln in out.splitlines() if "DBG" in ln or ("ERR " in ln and "forin" in ln or "while" in ln)]
    if "DBG" in out or "ERR " in out:
        errs += 1
        dbg_lines.append((i, hits))
        print(f"RUN {i}: " + " | ".join(hits))
    else:
        print(f"RUN {i}: ok")

print(f"errs={errs}/{n}")
