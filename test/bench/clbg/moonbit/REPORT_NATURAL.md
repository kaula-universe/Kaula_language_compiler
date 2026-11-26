# Kaula vs MoonBit：语言自然形态对比

> 原则：**让算法在两种语言上各自用自然形态实现，不为对齐而统一数据结构**。

> 正确性口径：native 输出逐字节一致（CLBG 标准格式），校验通过才计入计时。

## 运行环境

| 项 | 值 |
|---|---|
| OS | Linux 6.6.117-45.19.1.tl4.x86_64 (x86_64) |
| CPU | AMD EPYC 9K65 192-Core Processor / 32 核 |
| Kaula | kaulac -release（内部 clang -O3）| 
| MoonBit | moon 0.1.20261126 (ccd306a 2026-11-26)（LLVM native 后端）|
| 采样 | 每边 3 轮取中位数；RSS 由 /usr/bin/time -v 单次采样 |

## 正确性校验（native 输出逐字节）

| 题目 | 参数 | 形态与差异 | 结果 |
|---|---:|---|---|
| binarytrees | 21 | Kaula=arena连续数组 / MoonBit=GC递归引用树 | ✅ PASS |
| fannkuchredux | 12 | 两边均定长数组（算法标准形态，无语言差异） | ✅ PASS |

## 运行性能（native，中位数）

| 题目 | Kaula wall | MoonBit wall | MB/K | Kaula RSS | MoonBit RSS |
|---|---:|---:|---:|---:|---:|
| binarytrees | 1.615s | 6.847s | 4.24× | 66MB | 258MB |
| fannkuchredux | 36.367s | 38.702s | 1.06× | 2MB | 2MB |

## 编译速度与产物大小

| 题目 | Kaula 编译 | MoonBit 编译 | Kaula 产物 | MoonBit 产物 |
|---|---:|---:|---:|---:|
| binarytrees | 0.12s | 0.71s | 86,864 B | 492,488 B |
| fannkuchredux | 0.11s | 0.64s | 79,392 B | 492,384 B |

## 解读

- **binary-trees**：Kaula arena（连续数组、O(1) 复用）与 MoonBit GC 递归树（逐节点分配、GC 回收）是两种语言的自然形态，输出逐字节一致。性能差距反映的是**内存管理策略差异**，不是语法或代码生成能力差异。

- **fannkuch-redux**：算法本身以定长数组为标准形态，两种语言均无特殊形态可用，这一项的对比才更接近纯粹的编译器代码生成质量对比。

- **wasm 未纳入本报告**：Kaula `--wasm` 走 freestanding 裸机（无 stdout、无 libm、无 `__multi3`），MoonBit wasm 带 WASI（有 stdout）——两者输出能力不对等，强行对比只会得到误导性数字。需要 wasm 数据时请使用 `run_compare.py` 的 checksum kernel 口径（已标注限制）。
