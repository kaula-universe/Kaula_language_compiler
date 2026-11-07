# 给 Kaula 基准模板插桩，定位 suspend 曲线的死锁卡点。
#
# 为什么用文件而不是 stdout：实测 std.io.println 在管道下是全缓冲，
# 两行都在进程退出瞬间才到 —— 进程一旦挂住就一行都看不到，
# 分不清是卡在填窗口、卡在 await、还是卡在 settle。
# file_printf + file_flush 每个点位立即落盘，挂住时最后一个点位就是答案。
#
# 用法：python make_trace.py <curve> <conc> <total> [outfile]
import sys
import run_bench

curve = sys.argv[1] if len(sys.argv) > 1 else "suspend"
conc = int(sys.argv[2]) if len(sys.argv) > 2 else 100
total = int(sys.argv[3]) if len(sys.argv) > 3 else 1000
out = sys.argv[4] if len(sys.argv) > 4 else r"test\concurrency\probe_trace.kl"

src = run_bench.render_kaula(curve, conc, total)

# 1) main 顶部拿到 trace 句柄
needle = "fn main() -> i32 {"
assert src.count(needle) == 1
src = src.replace(needle, needle + '\n    File tf = std.io.file_open("trace.txt", "w");\n'
                                 '    std.io.file_printf(tf, "BEGIN");\n'
                                 '    std.io.file_flush(tf);')


def ins(anchor: str, code: str) -> None:
    """在 anchor 之后插入 code；anchor 必须唯一，否则说明模板变了。"""
    global src
    assert src.count(anchor) == 1, f"anchor 不唯一或不存在: {anchor!r} x{src.count(anchor)}"
    src = src.replace(anchor, anchor + "\n" + code)


ins("i64 expected = he.await;",
    '    std.io.file_printf(tf, "\\nEXPECTED ok");\n'
    '    std.io.file_flush(tf);')
ins("i64 filled = j;",
    '        std.io.file_printf(tf, "\\nW issued=%d filled=%d done=%d", issued, filled, done);\n'
    '        std.io.file_flush(tf);')
ins("done = done + 1;",
    '            if done % 100 == 0 {\n'
    '                std.io.file_printf(tf, "\\nD %d", done);\n'
    '                std.io.file_flush(tf);\n'
    '            }')
ins("i64 elapsed = std.time.time_now_ns() - t0;",
    '    std.io.file_printf(tf, "\\nLOOPDONE done=%d", done);\n'
    '    std.io.file_flush(tf);')
ins('std.io.println("PHASE load_end");',
    '    std.io.file_printf(tf, "\\nLOADEND");\n'
    '    std.io.file_flush(tf);')
ins('std.io.println("PHASE settle");',
    '    std.io.file_printf(tf, "\\nSETTLE");\n'
    '    std.io.file_flush(tf);')

with open(out, "w", encoding="utf-8") as fh:
    fh.write(src)
print(f"written {out} ({len(src)} bytes)")
