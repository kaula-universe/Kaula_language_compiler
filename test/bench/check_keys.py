# 核对三边 RESULT 键集是否逐字一致。
#
# 为什么值得单独一个脚本：大表能横向比的唯一前提就是三边输出同一批键。
# 缺一个键 metrics.py 就得为该运行时单开一列 N/A，整张表的可比性就散了。
# 这个检查在改模板/改任意一侧的输出行时都该跑一遍。
import io
import re
import sys
from pathlib import Path

sys.stdout = io.TextIOWrapper(sys.stdout.buffer, encoding="utf-8")

# 路径锚到脚本所在目录，这样从仓库根跑 `python test/bench/check_keys.py` 也对
HERE = Path(__file__).resolve().parent

SRC = {
    "kaula": "kaula/bench.kl.tmpl",
    "go": "go/bench/main.go",
    "tokio": "tokio/bench/src/main.rs",
}

# 从含 RESULT 那一行里按出现顺序抽 key=
KEY_RE = re.compile(r"([a-z0-9_]+)=")


def keys_of(text: str) -> list[str]:
    # 锚 `RESULT curve=` 而不是裸 `RESULT`：
    # 文件头注释里也有「一行 RESULT，按 key=value 解析」，用裸 RESULT 会
    # 抓到那行注释，得到 ['key'] 这种假结果，检查反而给出反向结论。
    i = text.find("RESULT curve=")
    if i < 0:
        return []
    # 只看这一行，避免抓到别处的 key=（比如文档注释）
    line = text[i:text.find("\n", i)]
    return KEY_RE.findall(line)


got = {}
for name, path in SRC.items():
    with open(HERE / path, encoding="utf-8") as fh:
        got[name] = keys_of(fh.read())
    print(f"{name:<6} {got[name]}")

base = got["kaula"]
ok = True
for name, keys in got.items():
    if keys == base:
        print(f"OK   {name:<6} 与 kaula 键集一致（{len(keys)} 键）")
    else:
        ok = False
        print(f"FAIL {name:<6} 与 kaula 不一致")
        print(f"       多出: {[k for k in keys if k not in base]}")
        print(f"       缺少: {[k for k in base if k not in keys]}")

# 采集器是否「认得」这些键 —— 看它是不是走通用解析，而不是逐个列键名。
# metrics.py 用 RESULT_RE = (\w+)=(-?\d+) 一次性抓全部键再灌进 values，
# 所以键名不逐个出现在源码里是正常的；只有当它是白名单式硬编码时，
# 才会出现「三边一致但某个键被采集器丢掉」的问题。
with open(HERE / "metrics.py", encoding="utf-8") as fh:
    mtext = fh.read()

generic = bool(re.search(r"RESULT_RE\s*=\s*re\.compile\(r\"\(\\w\+\)=\(-\?\\d\+\)\"\)", mtext))
if not generic:
    print("WARN metrics.py 未走通用 RESULT 解析，请人工确认下列键都被采集:")
    print(f"     {base}")
else:
    print("OK   metrics.py 走通用 RESULT 解析，全部键都会进 values / JSON")

sys.exit(0 if ok else 1)
