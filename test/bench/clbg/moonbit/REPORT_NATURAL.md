# Kaula vs MoonBit：全部 7 道 CLBG 题 native 自然形态对比

> 原则：**让算法在两种语言上各自用自然形态实现，不为对齐而统一数据结构**。

> 正确性：native 标准 CLBG 输出逐字节一致（含 32MB PBM 二进制），校验通过才计时。

## 运行环境

| 项 | 值 |
|---|---|
| OS | Linux 6.6.117-45.11.6.tl4.x86_64 (x86_64) |
| CPU | AMD EPYC 9K65 192-Core Processor / 32 核 |
| Kaula | kaulac -release（clang -O3）|
| MoonBit | moon 0.1.20261126（LLVM native 后端）|
| 采样 | 每边 3 轮取中位数；RSS 由 /usr/bin/time -v 单次采样 |

## 正确性校验（native 输出逐字节）

| 题目 | 参数 | 输出字节 | 形态差异 | 结果 |
|---|---:|---:|---|---|
| binarytrees | 21 | 455 | Kaula=arena连续数组 / MoonBit=GC递归引用树 | ✅ PASS |
| nbody | 5000000 | 26 | 两边 Array[Planet]+Double.sqrt / MoonBit 用 bigint 精确 %.9f | ✅ PASS |
| spectralnorm | 5500 | 12 | 两边 Array[Double]+幂迭代 / MoonBit 用 bigint 精确 %.9f | ✅ PASS |
| fannkuchredux | 12 | 29 | 两边均定长数组（算法标准形态，无语言差异） | ✅ PASS |
| fasta | 250000 | 2,541,745 | Kaula 字符缓冲 / MoonBit String+Ref LCG | ✅ PASS |
| mandelbrot | 16000 | 32,000,015 | 两边标量单线程 P4 PBM / MoonBit 用 FFI putchar | ✅ PASS |
| revcomp | fasta输出 | 2,541,745 | 两边 FFI/动态缓冲 + 互补查表 / 输入=fasta标准输出 | ✅ PASS |

## 运行性能（native，中位数）

| 题目 | Kaula wall | MoonBit wall | Moon/Kaula | Kaula RSS | MoonBit RSS |
|---|---:|---:|---:|---:|---:|
| binarytrees | 1.628s | 6.869s | 4.22× | 66MB | 258MB |
| nbody | 0.285s | 0.321s | 1.12× | 2MB | 2MB |
| spectralnorm | 1.340s | 1.359s | 1.01× | 2MB | 2MB |
| fannkuchredux | 36.749s | 38.945s | 1.06× | 2MB | 2MB |
| fasta | 0.034s | 0.053s | 1.57× | 2MB | 26MB |
| mandelbrot | 14.127s | 14.142s | 1.00× | 32MB | 124MB |
| revcomp | 0.011s | 0.055s | 4.94× | 4MB | 15MB |

## 编译速度与产物大小

| 题目 | Kaula 编译 | MoonBit 编译 | Kaula 产物 | MoonBit 产物 |
|---|---:|---:|---:|---:|
| binarytrees | 0.13s | 0.71s | 86,864 B | 492,488 B |
| nbody | 0.14s | 0.78s | 92,528 B | 497,048 B |
| spectralnorm | 0.13s | 0.75s | 89,816 B | 497,112 B |
| fannkuchredux | 0.12s | 0.64s | 79,376 B | 492,384 B |
| fasta | 0.15s | 0.67s | 82,728 B | 492,656 B |
| mandelbrot | 0.12s | 0.63s | 80,200 B | 492,152 B |
| revcomp | 0.14s | 0.63s | 81,208 B | 492,264 B |

## 口径说明

- 所有题目均使用 CLBG 官方标准参数，输出逐字节一致后计时。

- nbody/spectralnorm 的 `%.9f` 用 MoonBit bigint 精确格式化（round-half-even，与 C printf 对齐）。

- mandelbrot 为标量单线程 P4 PBM（隔离并行度/向量化变量），输出 32,000,015 字节。

- revcomp 输入为 fasta 的标准输出（CLBG 串联口径），两边用同一输入文件。

- wasm 未纳入：Kaula freestanding 无 stdout/libm/`__multi3`，输出能力不对等。
