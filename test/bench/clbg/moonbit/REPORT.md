# Kaula vs MoonBit 三维度对比报告

> 由 `test/bench/clbg/moonbit/run_compare.py` 自动生成。题目取自 [CLBG](https://benchmarksgame-team.pages.de/benchmarksgame/) 标准题目，以 checksum kernel 等价移植；Kaula 与 MoonBit 两边 checksum 逐值校验一致后才计入计时。

## 运行环境

| 项 | 值 |
|---|---|
| OS | Linux 6.6.117-45.19.1.tl4.x86_64 (x86_64) |
| CPU | AMD EPYC 9K65 192-Core Processor / 32 核 |
| Kaula | 0cd9aaf (kaulac -release / --wasm) |
| MoonBit | moon 0.1.20261126 (ccd306a 2026-11-26) (moon build --release, native/wasm) |
| wasm 运行时 | Python wasmtime (WASI preview1) |
| 采样口径 | 每边 3 轮取中位数；wall clock = perf_counter |

## Checksum 正确性校验

| 题目 | 参数 | Kaula (native) | MoonBit (native) | Kaula wasm (low32) | 结果 |
|---|---|---:|---:|---:|---|
| fannkuchredux | 10 | 74952742 | 74952742 | 74952742 | ✅ PASS |
| binarytrees | 10 | 135854 | 135854 | 135854 | ✅ PASS |

## 运行基准：native（wall clock 秒，中位数）

| 题目 | Kaula | MoonBit | MoonBit/Kaula |
|---|---:|---:|---:|
| fannkuchredux | 0.2311 | 0.2428 | 1.05× |
| binarytrees | 0.0012 | 0.0012 | 0.99× |

## 运行基准：wasm / wasmtime（wall clock 秒，中位数）

| 题目 | Kaula wasm | MoonBit wasm | MoonBit/Kaula |
|---|---:|---:|---:|
| fannkuchredux | 0.2292 | 0.3544 | 1.55× |
| binarytrees | 0.0011 | 0.0014 | 1.34× |

## 编译速度（秒，中位数口径为单次编译 wall clock）

| 题目 | Kaula native | MoonBit native | Kaula wasm | MoonBit wasm |
|---|---:|---:|---:|---:|
| fannkuchredux | 0.13 | 0.70 | 0.53 | 0.08 |
| binarytrees | 0.12 | 0.69 | 0.52 | 0.08 |

## 产物大小（字节）

| 题目 | Kaula native | MoonBit native | Kaula wasm | MoonBit wasm |
|---|---:|---:|---:|---:|
| fannkuchredux | 79,992 | 492,064 | 9,119 | 11,446 |
| binarytrees | 80,032 | 492,168 | 8,686 | 10,835 |

## 口径与限制说明

- **checksum kernel**：Kaula 的 `--wasm` 走 freestanding 裸机路径，无 stdout、无 libc/libm（缺 `sqrt`/`__multi3`）。因此 wasm 对比采用纯整数或 pre-sqrt 的 checksum kernel，两边算法逐行等价、checksum 逐值一致。

- **运行基准**：native 为 kaulac `-release`（内部 clang -O3）与 `moon build --release --target native`（LLVM 后端）的直接可执行；wasm 为两者产物在同一 wasmtime (WASI preview1) 下运行，隔离运行时差异。

- **编译速度**：单次 release 编译的 wall clock。MoonBit 首次编译含 core 库构建，后续增量会更快；本报告为干净 build 目录的冷编译口径。

- **产物大小**：Kaula wasm 固定 `--initial-memory=32MB` 线性内存，MoonBit wasm 为 WASI 模块；两者运行时模型不同，体积仅作量级参考。

- **题量与参数**：当前 kernel 覆盖 fannkuchredux(n=10) 与 binarytrees(N=10)；CLBG 全标准参数（fannkuch n=12 / binarytrees N=21）的 C/Go/Kaula 三边对比见上层 `../REPORT.md`。

