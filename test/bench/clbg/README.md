# CLBG 三边对比套件：C vs Go vs Kaula

用 [Computer Language Benchmarks Game](https://benchmarksgame-team.pages.de/benchmarksgame/)
（CLBG）**官方标准题目、官方参考实现算法、官方标准参数**，对比三种语言的性能，
核心对照维度是**内存管理模型**：

| 边 | 内存管理 |
|---|---|
| C | **手动**：每节点 `malloc` / `DeleteTree` 递归 `free` |
| Go | **自动**：每节点 `&Node{}` 堆分配，GC 回收 |
| Kaula | **自动**（SOR 编译期所有权分析 + KMM V4 运行时）；本套件标准参数侧提供两种形态（见下） |

## 目录

```
clbg/
├── run_bench.py            # 统一 runner：编译 → checksum 校验 → 采样 → 生成报告
├── README.md               # 本文件（套件契约）
├── REPORT.md               # runner 生成的对比报告（机器相关数据）
├── ref/                    # CLBG 官方参考实现（C + Go，未改动）
│   ├── binarytrees.gcc.c / binarytrees.go
│   ├── nbody.gcc.c / nbody.go
│   ├── spectralnorm.gcc.c / spectralnorm.go
│   └── fannkuchredux.gcc.c / fannkuchredux.go
├── kaula/                  # Kaula 实现（算法与参考逐行对照）
│   ├── binary_trees.kl         # arena 数组形态（标准参数 N=21 可跑）
│   ├── binary_trees_ptr.kl     # 标准递归指针树形态（小参数，语义等价性证明）
│   ├── nbody.kl
│   ├── spectralnorm.kl
│   └── fannkuchredux.kl
└── build/                  # runner 产物（gitignore）
```

## 题目与标准参数

| 题目 | CLBG 标准参数 | 算法 |
|---|---|---|
| binary-trees | N=21 | `bottomUpTree` 递归建完全二叉树、`itemCheck` 递归求和；反复建弃短命树 + 一棵 long-lived 树 |
| n-body | 5,000,000 步 | 5 体太阳系：`offsetMomentum` → `advance × N` → `energy` |
| spectral-norm | N=5500 | 幂迭代求谱范数（`aTimesU`/`atTimesU`/`atATimesU`） |
| fannkuch-redux | n=12 | Mazurov 进位式递推逐排列计算最大翻转数 |

Kaula 侧参数通过编译前占位符注入（`N_PLACEHOLDER` / `N_STEPS_PLACEHOLDER`），
算法实现均与 C/Go 参考逐行对照翻译，**不改变算法结构**。

## 运行

```bash
python3 run_bench.py                    # 全量：编译 → 校验 → 3 轮采样取中位数
python3 run_bench.py --rounds 5         # 自定义采样轮数
python3 run_bench.py --quick            # 每边 1 轮冒烟
python3 run_bench.py --only binarytrees,nbody
python3 run_bench.py --no-kaula-ptr     # 跳过指针版形态验证
```

runner 会：

1. **编译三边**：C 用 `gcc -O3`，Go 用 `go build`（保留参考里的
   `runtime.GOMAXPROCS(4)` 等原始设定），Kaula 用 `kaulac -release`
   （内部 `clang -O3`）。kaulac 定位顺序：`$KAULAC` → 仓库 `build/bin/kaulac`
   → 临时 `go build`。
2. **Checksum 校验**：第 1 轮 stdout 三边对比。binary-trees / n-body /
   spectral-norm 三边逐字节一致；fannkuch-redux 的 C 参考不输出 checksum 行
   （只有 Go/Kaula 输出两行），按「C 行为 Go/Kaula 行子集 + Go/Kaula 逐字节
   一致」校验。
3. **指针版形态验证**（仅 binary-trees）：标准递归指针树实现在缩小参数
   （N=10）下与 C 参考逐字节对比，证明 arena 改写未改变算法语义。
4. **采样计时**：`/usr/bin/time -v` 采集 wall clock 与 peak RSS，每边重复
   多轮取中位数，结果写入 `REPORT.md`。

## Kaula 侧两种形态与口径声明

### arena 数组形态（`binary_trees.kl`，标准参数侧）

单块连续数组 + i32 下标寻址（`left/right` 为下标，-1 表示空），
`build`/`itemCheck` 与标准版逐行等价。选它的原因：**Kaula 的标准递归
指针形态在 CLBG 标准参数（N=21）下不可用**——KMM V4 的 survivor 段只增不减，
弃用的聚合树不回收，理论驻留约 17GB，实测 RSS 到 7.4GB 后 OOM 打断
（见 `docs/core-runtime.md` §2.10 已知限制）。arena 模式峰值内存 O(1)。

> ⚠ 口径提醒：arena 形态的低内存/高速度来自**分配策略**（连续数组复用、
> 缓存友好、无逐节点分配器调用），不代表运行时通用能力。与 C/Go 对比时
> 应理解为「同一算法在不同内存布局下的表现」，其中 Kaula 边是编译器 +
> 运行时当前可跑完整标准参数的形态。

### 标准递归指针树形态（`binary_trees_ptr.kl`，小参数侧）

每节点 `kmm_v4_alloc(16)`、无手动 free——即 Kaula 自然内存管理形态
（SOR 所有权分析决定回收时点）。用于：

- 证明 arena 改写的算法语义等价（N=10 与 C 参考逐字节一致）；
- 展示指针形态的内存特征（survivor 段增长，N=10 即 ~196MB 峰值）。

## 参考实现来源

`ref/` 下 8 个文件来自 CLBG 官方 CVS 镜像
（[Byron/benchmarksgame-cvs-mirror](https://github.com/Byron/benchmarksgame-cvs-mirror)），
未做任何改动。
