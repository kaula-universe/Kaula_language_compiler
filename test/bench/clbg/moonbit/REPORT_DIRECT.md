# Kaula vs MoonBit：全部 7 道 CLBG 题 native 官方 C 直译对比

> 原则：**以 ref/*.gcc.c（CLBG 官方 C 示例）为蓝本逐行直译，不做任何语言特化**。

> 正确性：native 标准 CLBG 输出逐字节一致（含 32MB PBM 二进制），校验通过才计时。

## 运行环境

| 项 | 值 |
|---|---|
| OS | Linux 6.6.117-45.14.1.tl4.x86_64 (x86_64) |
| CPU | AMD EPYC 9K65 192-Core Processor / 32 核 |
| Kaula | kaulac -release（clang -O3）|
| MoonBit | moon 0.1.20261126（LLVM native 后端）|
| 采样 | 每边 3 轮取中位数；RSS 由 /usr/bin/time -v 单次采样 |

## 正确性校验（native 输出逐字节）

| 题目 | 参数 | 输出字节 | 形态差异 | 结果 |
|---|---:|---:|---|---|
| binarytrees | 21 | 455 | 官方C直译：两边均逐节点分配（Kaula 自动回收 / MoonBit GC） | ✅ PASS |
| nbody | 5000000 | 26 | 官方C直译：结构体数组+同公式（%.9f 用 bigint 精确格式化） | ✅ PASS |
| spectralnorm | 5500 | 12 | 官方C直译：向量+幂迭代（%.9f 用 bigint 精确格式化） | ✅ PASS |
| fannkuchredux | 12 | 29 | 官方C直译：两边均定长数组 | ✅ PASS |
| fasta | 250000 | 2,541,745 | 官方C直译：LCG+累积概率+二分选择，两边同构 | ✅ PASS |
| mandelbrot | 16000 | 32,000,015 | 官方C直译：标量单线程 P4 PBM（隔离 SIMD/线程变量） | ✅ PASS |
| revcomp | fasta输出 | 2,541,745 | 官方C直译：两边均256互补查表+缓冲+60列 / 输入=fasta标准输出 | ✅ PASS |

## 运行性能（native，中位数）

| 题目 | Kaula wall | MoonBit wall | Moon/Kaula | Kaula RSS | MoonBit RSS |
|---|---:|---:|---:|---:|---:|
| binarytrees | 4.187s | 6.755s | 1.61× | 3966MB | 258MB |
| nbody | 0.284s | 0.316s | 1.11× | 2MB | 2MB |
| spectralnorm | 1.342s | 1.356s | 1.01× | 2MB | 2MB |
| fannkuchredux | 36.909s | 38.922s | 1.05× | 2MB | 2MB |
| fasta | 0.034s | 0.052s | 1.55× | 2MB | 26MB |
| mandelbrot | 14.109s | 14.126s | 1.00× | 33MB | 124MB |
| revcomp | 0.010s | 0.028s | 2.70× | 4MB | 28MB |

## 编译速度与产物大小

| 题目 | Kaula 编译 | MoonBit 编译 | Kaula 产物 | MoonBit 产物 |
|---|---:|---:|---:|---:|
| binarytrees | 0.11s | 0.68s | 79,664 B | 492,488 B |
| nbody | 0.14s | 0.76s | 92,528 B | 497,048 B |
| spectralnorm | 0.12s | 0.74s | 89,816 B | 497,112 B |
| fannkuchredux | 0.11s | 0.62s | 79,392 B | 492,384 B |
| fasta | 0.14s | 0.66s | 82,728 B | 492,656 B |
| mandelbrot | 0.10s | 0.61s | 80,216 B | 492,152 B |
| revcomp | 0.13s | 0.62s | 81,224 B | 492,312 B |

## 口径说明

- 所有题目均使用 CLBG 官方标准参数，输出逐字节一致后计时。

- nbody/spectralnorm 的 `%.9f` 用 MoonBit bigint 精确格式化（round-half-even，与 C printf 对齐）。

- binary-trees 为官方 Carson 版逐节点分配形态：官方 C 的 DeleteTree/manual free 在 Kaula 无法
  直译（手动 free 属非法写法，`kmm_v4_free` 为 no-op），Kaula 边为逐节点 `kmm_v4_alloc` 自动回收，
  N=21 实测峰值 ~4GB 可跑完；MoonBit 边为 GC 递归树。

- revcomp 为官方 Prokhorenkov 版的串行直译（256 查表 + 缓冲 + 60 列），线程/pair-lookup 为
  并行向量化变量，按套件惯例隔离。

- 「自然形态」口径（Kaula arena / MoonBit 各自惯用形态）见 run_natural.py 与 REPORT_NATURAL.md，
  两份报告回答的问题不同：本报告=纯移植质量，彼报告=开箱即用表现。

- mandelbrot 为标量单线程 P4 PBM（隔离并行度/向量化变量），输出 32,000,015 字节。

- revcomp 输入为 fasta 的标准输出（CLBG 串联口径），两边用同一输入文件。

- wasm 未纳入：Kaula freestanding 无 stdout/libm/`__multi3`，输出能力不对等。
