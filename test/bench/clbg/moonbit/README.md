# Kaula vs MoonBit 三维度对比套件

用 [CLBG](https://benchmarksgame-team.pages.de/benchmarksgame/) 标准题目的等价 checksum kernel，
对比 **Kaula** 与 **MoonBit** 三个维度：

| 维度 | 口径 |
|---|---|
| 运行基准 | native 与 wasm 两种目标的 wall clock（多轮取中位数） |
| 编译速度 | release 编译的单次 wall clock |
| 产物大小 | 可执行文件 / wasm 模块字节数 |

## 为什么用 checksum kernel

Kaula 的 `kaulac --wasm` 走 freestanding 裸机路径（见 `docs/freestanding.md` §6）：

- **无 stdout**：wasm 模块不导出任何输出能力，只有 `main() -> i32` 的返回值；
- **无 libc/libm**：`sqrt` 未链接、`__multi3`（i64×i64）缺符号，所以 nbody/spectralnorm
  这类依赖浮点 sqrt / 大整数乘的题目无法直接在 Kaula wasm 下编译（故本套件当前只覆盖
  纯整数的 fannkuchredux 与 binarytrees）。

因此本套件把题目改写为**纯整数的 checksum kernel**：

- Kaula native：把 checksum 通过 `std.io.println` 打印到 stdout（退出码只有 8 位，不够）；
- Kaula wasm：`main()` 返回 checksum 的低 32 位；
- MoonBit native/wasm：`println(checksum)`（WASI fd_write）。

两边对同一题目、同一参数输出**逐值相同的 checksum**，校验通过才计入计时。

## 题目

| 题目 | 参数 | 算法 | checksum |
|---|---|---|---|
| fannkuchredux | n=10 | Mazurov 进位式排列递推 + 翻煎饼计数 | `checksum * 1024 + maxFlips` |
| binarytrees | N=10 | arena 完全二叉树建/遍历（并行数组避免 MoonBit struct 别名） | `stretch + Σ各深度check + long-lived` |

> 题量说明：CLBG 全标准参数（fannkuch n=12 / binarytrees N=21）下 C/Go/Kaula 三边
> 逐字节校验的完整基准见上层 [`../REPORT.md`](../REPORT.md)。本套件聚焦两种语言的
> 三维对比，受 Kaula wasm freestanding 限制只覆盖可跨四目标移植的 kernel。

## 运行

前置条件：

- kaulac：`/workspace/kaula/build/bin/kaulac`（或设 `KAULAC` 环境变量）
  ```bash
  cd /workspace/kaula/compiler && go build -o ../build/bin/kaulac ./cmd/kaulac/
  ```
- MoonBit：`~/.moon/bin/moon`（安装 `curl -fsSL https://cli.moonbitlang.com/install/unix.sh | bash`）
- Python wasmtime：`python3 -m pip install --break-system-packages wasmtime`

执行：

```bash
cd test/bench/clbg/moonbit
python3 run_compare.py                    # 全量：每边 3 轮取中位数
python3 run_compare.py --rounds 5          # 自定义轮数（收敛性验证）
python3 run_compare.py --quick             # 每边 1 轮冒烟
python3 run_compare.py --verify-only       # 只校验 checksum，不计时
```

产物：

- `build/`：编译中间产物（gitignore）
- `REPORT.md`：由 runner 自动生成的对比报告

## 文件清单

```
moonbit/
├── README.md                       # 本文件（套件契约）
├── REPORT.md                       # 自动生成的对比报告
├── run_compare.py                  # 统一 runner（编译 → 校验 → 计时 → 报告）
├── moon.mod                        # MoonBit 模块定义
├── fannkuchredux.mbtx              # MoonBit fannkuch checksum kernel
├── fannkuchredux_kernel.kl         # Kaula fannkuch checksum kernel
├── binarytrees.mbtx                # MoonBit binarytrees checksum kernel
└── binarytrees_kernel.kl           # Kaula binarytrees checksum kernel
```

## 正确性契约

每一轮运行前，runner 都会：

1. 为四边（Kaula/MoonBit × native/wasm）各自编译产物；
2. 在 native 侧对比 Kaula 与 MoonBit 的完整 checksum 字符串，**必须逐字节相等**；
3. 在 wasm 侧对比 Kaula wasm 的 `main()` 返回值与 native checksum 的低 32 位，**必须相等**；
4. 任一校验失败即 `sys.exit` 中止，不产出报告。

这保证了报告中的计时数据全部来自**算法等价、结果一致**的实现，避免"比了但算的不是同一件事"。

## 已知限制

- Kaula wasm 缺 `__multi3`（i64×i64 编译器辅助函数），因此 rolling hash 若用 i64 乘法
  会链接失败；binarytrees kernel 因此改用 i64 加法求和，已在两边验证一致。
- MoonBit 的 struct 数组 `Array::make(n, Struct{...})` 存在元素别名（所有槽位共享同一
  结构），会破坏 arena 树；kernel 因此改用两条并行 `Array[Int]` 存 left/right 下标。
- wasm 计时包含每次重新实例化模块（Engine/Store/Module/Instance 构建）的固定开销，
  两边口径一致；对毫秒级题目（binarytrees N=10）该开销主导，读表时请关注数量级而非比值。
