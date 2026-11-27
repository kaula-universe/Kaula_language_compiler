# Kaula vs MoonBit 对比套件

三套对比，口径不同，不可混用：

| 套件 | runner | 报告 | 原则 |
|---|---|---|---|
| **官方 C 直译对比** | `run_direct.py` | `REPORT_DIRECT.md` | **以 ref/*.gcc.c（CLBG 官方 C 示例）为蓝本逐行直译，不做任何语言特化**——回答"纯移植质量" |
| **自然形态对比**（推荐） | `run_natural.py` | `REPORT_NATURAL.md` | **让算法在两种语言上各自用自然形态实现，不为对齐而统一数据结构**——回答"开箱即用表现" |
| checksum kernel 对比 | `run_compare.py` | `REPORT.md` | 为跨 wasm 而统一为纯整数 kernel，受 Kaula wasm 能力限制 |

---

## 一、官方 C 直译对比

以 `../ref/*.gcc.c` 为唯一蓝本：binary-trees 用官方 Carson 版逐节点分配指针树、
revcomp 用官方 Prokhorenkov 版的 256 互补查表 + 缓冲 + 60 列（串行口径，线程/
pair-lookup 是被隔离的并行向量化变量），其余 5 题两边本就直译自官方 C，共用源文件。

注意：官方 C 的 `DeleteTree`（逐节点 free）在 Kaula 无法直译——手动 free 属非法写法
（`kmm_v4_free` 为 no-op，与自动回收叠加会 double-free），Kaula 边为逐节点
`kmm_v4_alloc` 自动回收，N=21 实测峰值 ~4GB 可跑完。

```bash
python3 run_direct.py                  # 3 轮取中位数
python3 run_direct.py --quick          # 1 轮冒烟
```

产物：`build_direct/`（gitignore）、`REPORT_DIRECT.md`。

---

## 二、自然形态对比（推荐）

核心原则：**尊重语言差异**。Arena 是 Kaula 的内存管理特性，MoonBit 就不该模拟它；
MoonBit 的自然形态是 GC 管理的递归引用树，Kaula 的自然形态是 arena 连续数组。
两边用各自最自然的写法实现同一算法，输出逐字节一致后才比性能。

**全部 7 道 CLBG 标准题**，官方标准参数，输出逐字节一致（含 32MB PBM 二进制）：

| 题目 | 标准参数 | Kaula 形态 | MoonBit 形态 |
|---|---:|---|---|
| binary-trees | N=21 | arena 连续数组 + 下标寻址（O(1) 复用） | `struct Node{left/right: Node?}` GC 递归引用树 |
| n-body | 5,000,000 步 | 栈上 `Planet[5]` | `Array[Planet]` + `Double.sqrt()` |
| spectral-norm | N=5500 | `kmm_v4_alloc` 堆向量 | `Array[Double]` + `Double.sqrt()` |
| fannkuch-redux | n=12 | 定长 i32 栈数组 | 定长数组（算法标准形态） |
| fasta | n=250000 | 字符缓冲 + LCG | `Array[Char]` + `Ref[Int64]` LCG |
| mandelbrot | N=16000 | 标量 + `kmm_v4_alloc` 位图 | 标量 + `Array[Int]` 位图 + FFI `putchar` |
| reverse-complement | fasta 输出 | 动态 `kmm_v4_realloc` 缓冲 + 查表 | `Array[Int32]` + FFI `getchar/putchar` |

> **尊重语言差异的具体体现**：binary-trees 上 Kaula 用 arena、MoonBit 用 GC 递归树——
> 完全不同但语义等价的内存管理形态。fannkuch 两边都用数组，是因为该算法的标准形态
> 就是定长数组，没有语言特性可发挥——这一项更接近纯粹的编译器代码生成质量对比。
>
> `%.9f` 精确格式化：MoonBit 核心库没有定点格式化，采用 bigint 精确 round-half-even
> 实现（`fmt9.mbt`，源自 moon-boofuzz，GPL-2.0），与 C printf 逐字节对齐。

维度：native 运行（wall clock + peak RSS）、编译速度、产物大小。

```bash
python3 run_natural.py                 # 3 轮取中位数
python3 run_natural.py --quick         # 1 轮冒烟
```

产物：`build_natural/`（gitignore）、`REPORT_NATURAL.md`。

**为什么不含 wasm**：Kaula `--wasm` 走 freestanding 裸机（无 stdout、无 libm、无
`__multi3`），MoonBit wasm 带 WASI（有 stdout）——两边输出能力不对等，强行比只会得到
误导性数字。如果需要 wasm 数据，用下面第三套的 checksum kernel 口径。

---

## 三、checksum kernel 对比（wasm 专用）

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
├── REPORT_DIRECT.md                 # 7 题官方 C 直译对比报告
├── REPORT_NATURAL.md                # 7 题自然形态对比报告（推荐）
├── REPORT.md                        # checksum kernel 对比报告
├── run_direct.py                    # 7 题官方 C 直译 runner
├── run_natural.py                   # 7 题自然形态 runner
├── run_compare.py                   # checksum kernel runner
├── fmt9.mbt                         # MoonBit bigint 精确 %.9f 格式化
├── binarytrees_gc.mbtx              # MoonBit GC 递归树（两种口径共用）
├── nbody.mbtx                       # MoonBit n-body
├── spectralnorm.mbtx                # MoonBit spectral-norm
├── fannkuchredux_std.mbtx           # MoonBit fannkuch 标准输出
├── fasta.mbtx                       # MoonBit fasta
├── mandelbrot.mbtx                  # MoonBit mandelbrot
├── revcomp_direct.mbtx              # MoonBit revcomp 官方 C 直译（查表+缓冲）
├── revcomp.mbtx                     # MoonBit revcomp 自然形态
├── fannkuchredux.mbtx               # MoonBit fannkuch checksum kernel
├── fannkuchredux_kernel.kl          # Kaula fannkuch checksum kernel
├── binarytrees.mbtx                 # MoonBit binarytrees checksum kernel
├── binarytrees_kernel.kl            # Kaula binarytrees checksum kernel
└── moon.mod
```

## 正确性契约

三个 runner 都遵循：**校验不通过不产出报告**。

- `run_direct.py` / `run_natural.py`：native 输出逐字节相等（CLBG 标准格式）；
- `run_compare.py`：native checksum 逐值相等 + wasm 返回值等于 native checksum 低 32 位。
