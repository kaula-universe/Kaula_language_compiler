# CLBG 三边对比报告：C vs Go vs Kaula

> 由 `test/bench/clbg/run_bench.py` 自动生成；题目、算法与参数均为 [CLBG](https://benchmarksgame-team.pages.de/benchmarksgame/) 官方标准口径。

## 运行环境

| 项 | 值 |
|---|---|
| OS | Linux 6.6.117-45.11.3.tl4.x86_64 (x86_64) |
| CPU | AMD EPYC 9K65 192-Core Processor |
| 核心 / 内存 | 32 核 / 123 GiB |
| 编译器 | gcc (Ubuntu 13.3.0-6ubuntu2~24.04) 13.3.0；Ubuntu clang version 18.1.3 (1ubuntu1) |
| Go | go version go1.21.4 linux/amd64 |
| Kaula 仓库 | 0b3e5c3（有未提交改动） |
| 采样口径 | 每边 3 轮取中位数；`/usr/bin/time -v` wall clock + peak RSS |

## Checksum 校验（三边输出一致性）

| 题目 | 标准参数 | 结果 | 说明 |
|---|---|---|---|
| binarytrees | N=21 | ✅ PASS | 三边 stdout 逐字节一致 |
| nbody | steps=5000000 | ✅ PASS | 三边 stdout 逐字节一致 |
| spectralnorm | N=5500 | ✅ PASS | 三边 stdout 逐字节一致 |
| fannkuchredux | n=12 | ✅ PASS | Go/Kaula 逐字节一致；C 参考行为其子集（C 版不输出 checksum 行） |

## 性能（wall clock 秒，peak RSS MB）

| 题目 | C（手动内存管理） | Go（自动 GC） | Kaula（arena） | Kaula/C | Go/C |
|---|---|---|---|---|---|
| binarytrees (N=21) | 15.920s / 257MB | 31.840s / 206MB | 1.650s / 65MB | 0.10× | 2.00× |
| nbody (steps=5000000) | 0.240s / 1MB | 0.360s / 1MB | 0.310s / 1MB | 1.29× | 1.50× |
| spectralnorm (N=5500) | 1.330s / 1MB | 3.910s / 2MB | 1.350s / 13MB | 1.02× | 2.94× |
| fannkuchredux (n=12) | 34.720s / 1MB | 7.040s / 1MB | 37.100s / 1MB | 1.07× | 0.20× |

### 逐题读表说明

- **binary-trees**：Kaula 边为 arena 连续数组形态（下标寻址、建弃即复用），C 边每节点 `malloc`/`free` 且遍历是指针追逐。0.10× 与 65MB 峰值主要来自**内存布局与分配策略**差异，不是算法差异；Go 在与 C 同构的指针形态之上叠加 GC，慢于 C 符合预期。
- **n-body**：三边都是栈上 5 个 Body 小数组、同构数值计算，差异纯代码生成质量：Kaula 为 C 的 1.3×，Go 为 1.5×。
- **spectral-norm**：三边都是大数组线性扫描的数值核，Kaula 与 C 基本打平（1.02×）；Go 受 bounds check 等影响慢约 3×。
- **fannkuch-redux**：⚠ **Go 参考实现自带 `runtime.GOMAXPROCS(4)` 并行分块计算，C/Kaula 边为单线程**——0.20× 是并行度差异，不是语言或内存管理差异。单线程口径下 Kaula 为 C 的 1.07×。

## Kaula 标准指针形态验证（小参数）

binary-trees 另有一份**标准递归指针树**实现（`kaula/binary_trees_ptr.kl`，每节点 `kmm_v4_alloc`、无手动 free，即 Kaula 自然内存管理形态），在缩小参数下与 C 参考逐字节一致，证明 arena 改写未改变算法语义：

| 形态 | 参数 | 与 C 参考输出 | wall | peak RSS |
|---|---|---|---|---|
| binarytrees 指针版 | N=10 | ✅ 一致 | 0.090s | 196MB |

> 指针版在 CLBG 标准参数（N=21）下不可用：KMM V4 的 survivor 段只增不减，弃用的聚合树不回收，理论驻留约 17GB，实测 RSS 到 7.4GB 后 OOM 打断（见 `docs/core-runtime.md` §2.10 已知限制）。标准参数的 Kaula 边因此采用 arena 数组形态，算法语义、checksum 与指针版完全一致。

## 内存管理口径（三边本质差异）

| 边 | 分配策略 | 回收策略 |
|---|---|---|
| C | 每节点 `malloc`（binary-trees）/ 栈上数组 | 递归 `free` 手动精确回收 |
| Go | 每节点 `&Node{}` 堆分配 | 自动 GC，不可预测时点 |
| Kaula（arena 形态） | 单块常驻数组，下标寻址 | 建弃即复用，峰值 O(1) |
| Kaula（指针形态） | `kmm_v4_alloc` 作用域感知 bump allocator | SOR 所有权分析决定回收时点；survivor 段只增不减（已知限制） |

读表注意：arena 形态的 Kaula 峰值内存低是**分配策略**差异（O(1) 复用），不代表运行时通用能力；三边算法同构，计时差异主要来自代码生成质量与内存布局（连续数组 vs 指针追逐），而非题目实现偏差。

## 总结论

1. **正确性**：4 个 CLBG 标准题目、官方标准参数下，三边输出全部一致（fannkuch 的 C 参考不输出 checksum 行，按行子集口径校验）；Kaula 标准递归指针形态在小参数下同样与 C 逐字节一致。
2. **纯计算核（n-body / spectral-norm / fannkuch 单线程口径）**：Kaula ≈ C（1.02×–1.29×），两者都显著优于 Go（1.5×–3×）——Kaula 经 clang -O3 后达到 C 的代码生成水准。
3. **分配密集核（binary-trees）**：取决于内存管理形态——Kaula arena O(1) 复用比 C 手动 malloc/free 快约 10×、内存少约 4×；Go 的自动 GC 在指针追逐负载上比 C 慢约 2×。
4. **Kaula 自动内存管理的当前边界**：标准指针形态在聚合树反复建弃的负载下 survivor 段只增不减（已知限制，见 `docs/core-runtime.md` §2.10），标准参数需借助 arena 形态；这与 Go GC「不预测时点但持续回收」仍有差距，是 Kaula 运行时待补能力。

## 题目与算法对照

| 题目 | 标准参数 | 算法 |
|---|---|---|
| binarytrees | N=21 | 递归构建/遍历完全二叉树，反复建弃短命树 + 一棵 long-lived 树 |
| nbody | steps=5000000 | 5 体太阳系 N 步数值积分（offsetMomentum → advance × N → energy） |
| spectralnorm | N=5500 | 幂迭代求谱范数（aTimesU/atTimesU/atATimesU 三重循环） |
| fannkuchredux | n=12 | Mazurov 进位式递推逐排列计算最大翻转数 |
