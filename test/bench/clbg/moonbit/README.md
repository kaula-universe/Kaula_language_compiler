# Kaula vs MoonBit 对比套件

两套对比，口径不同，不可混用：

| 套件 | runner | 报告 | 原则 |
|---|---|---|---|
| **自然形态对比**（推荐） | `run_natural.py` | `REPORT_NATURAL.md` | **让算法在两种语言上各自用自然形态实现，不为对齐而统一数据结构** |
| checksum kernel 对比 | `run_compare.py` | `REPORT.md` | 为跨 wasm 而统一为纯整数 kernel，受 Kaula wasm 能力限制 |

---

## 一、自然形态对比（推荐）

核心原则：**尊重语言差异**。Arena 是 Kaula 的内存管理特性，MoonBit 就不该模拟它；
MoonBit 的自然形态是 GC 管理的递归引用树，Kaula 的自然形态是 arena 连续数组。
两边用各自最自然的写法实现同一算法，输出逐字节一致后才比性能。

| 题目 | 标准参数 | Kaula 形态 | MoonBit 形态 | 校验 |
|---|---:|---|---|---|
| binary-trees | N=21 | arena 连续数组 + 下标寻址（O(1) 复用） | `struct Node{left/right: Node?}` GC 递归引用树 | 标准 CLBG 输出逐字节一致 |
| fannkuch-redux | n=12 | 定长 i32 栈数组 | 定长数组 | 标准 CLBG 输出逐字节一致 |

> fannkuch 两边都用数组，是因为该算法的标准形态就是定长数组，没有语言特性可发挥——
> 这一项才更接近纯粹的编译器代码生成质量对比。

维度：native 运行（wall clock + peak RSS）、编译速度、产物大小。

```bash
python3 run_natural.py                 # 3 轮取中位数
python3 run_natural.py --quick         # 1 轮冒烟
```

产物：`build_natural/`（gitignore）、`REPORT_NATURAL.md`。

**为什么不含 wasm**：Kaula `--wasm` 走 freestanding 裸机（无 stdout、无 libm、无
`__multi3`），MoonBit wasm 带 WASI（有 stdout）——两边输出能力不对等，强行比只会得到
误导性数字。如果需要 wasm 数据，用下面第二套的 checksum kernel 口径。

---

## 二、checksum kernel 对比（wasm 专用）

Kaula 的 `kaulac --wasm` 走 freestanding 裸机路径（见 `docs/freestanding.md` §6）：

- **无 stdout**：wasm 模块只有 `main() -> i32` 的返回值；
- **无 libc/libm**：`sqrt` 未链接、`__multi3`（i64×i64）缺符号。

因此把题目统一改写成纯整数 checksum kernel，让四边（Kaula/MoonBit × native/wasm）
都能跑。注意：这里的 binarytrees 用两条并行数组——**这是为了跨 wasm 而做的妥协，
不代表任一种语言的自然形态**，请不要用这份数据解读语言特性。

| 题目 | 参数 | checksum |
|---|---|---|
| fannkuchredux | n=10 | `checksum * 1024 + maxFlips` |
| binarytrees | N=10 | `stretch + Σ各深度check + long-lived` |

```bash
python3 run_compare.py                 # 全量：3 轮中位数
python3 run_compare.py --rounds 5      # 自定义轮数
python3 run_compare.py --quick         # 1 轮冒烟
python3 run_compare.py --verify-only   # 只校验 checksum
```

产物：`build/`（gitignore）、`REPORT.md`。

---

## 前置条件

- kaulac：`/workspace/kaula/build/bin/kaulac`（或设 `KAULAC`）
  ```bash
  cd /workspace/kaula/compiler && go build -o ../build/bin/kaulac ./cmd/kaulac/
  ```
- MoonBit：`~/.moon/bin/moon`（安装 `curl -fsSL https://cli.moonbitlang.com/install/unix.sh | bash`）
- Python wasmtime（仅 `run_compare.py` 需要）：`python3 -m pip install --break-system-packages wasmtime`

## 文件清单

```
moonbit/
├── README.md                        # 本文件
├── REPORT_NATURAL.md                # 自然形态对比报告（推荐）
├── REPORT.md                        # checksum kernel 对比报告
├── run_natural.py                   # 自然形态 runner
├── run_compare.py                   # checksum kernel runner
├── binarytrees_gc.mbtx              # MoonBit GC 递归树（自然形态）
├── fannkuchredux_std.mbtx           # MoonBit fannkuch 标准输出（自然形态）
├── fannkuchredux.mbtx               # MoonBit fannkuch checksum kernel
├── fannkuchredux_kernel.kl          # Kaula fannkuch checksum kernel
├── binarytrees.mbtx                 # MoonBit binarytrees checksum kernel
├── binarytrees_kernel.kl            # Kaula binarytrees checksum kernel
└── moon.mod
```

## 正确性契约

两个 runner 都遵循：**校验不通过不产出报告**。

- `run_natural.py`：native 输出逐字节相等（CLBG 标准格式）；
- `run_compare.py`：native checksum 逐值相等 + wasm 返回值等于 native checksum 低 32 位。
