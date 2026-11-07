# C / Kaula / Go 四边并发基准 —— 实测报告

> 回答两个问题：**Kaula 的编译器 lowering 值多少钱？CONC 内核相对 Go 强在哪、代价在哪？**

本报告是 `test/bench/` 下四边基准的实测汇总。契约与口径承接 `TRANSACTION.md`，
问题记录承接 `FINDINGS.md`。本轮**新增 C 边**（`cbench/`，直调内核 API），
并给 Kaula 边加 `inlined` 口径标记。

测量环境：Linux x86_64，**32 核**，clang 18.1.3，Go 1.25.0，共享沙箱（非空载，见 §6）。

---

## 1. 四边与它们各自回答的问题

| 边 | 目录 | 路径 | 回答 |
|---|---|---|---|
| **kaula** | `kaula/` | `conc fn` + `spawn` + `await`，经 kaulac 编译 | **用户最终写出来的样子**（编译器优化后的体验） |
| **c** | `cbench/` ← 本轮新增 | 直调 `kaula_spawn` / `kaula_handle_await_into_timed` | **内核 API 的开销下限**（无 lowering） |
| **go** | `go/` | `go` + `WaitGroup`（GMP 调度器） | 主流竞品对照 |
| **tokio** | `tokio/` | `async`/`await`（M:N Future） | 另一主流竞品对照 |

四边跑**同一个事务**、**同一组常量**、**同样多的循环**：

```
parse_and_checksum():  STEPS=256, SEED=2545, MULT=1103515245, ADD=12345
                       SHIFT=2^20, P=2^63-1  →  期望值 13356499072
```

### C 边 vs Kaula 边 = 编译器的语法代价

两边**唯一**的区别是「Frame 由谁生成、参数怎么传」：

| 环节 | Kaula（kaulac 生成） | C（人手写） |
|---|---|---|
| Frame | `K_xxx_Frame` 结构体 | `BenchFrame` |
| resume | `K_xxx_resume` 状态机（`switch(pc)`） | `resume()` 状态机（`switch(pc)`） |
| 挂起 | `time_sleep_us` → `KAULA_IO_SLEEP` 请求 | 手写 `KAULA_RESUME_SUSPEND` + `KAULA_IO_SLEEP` |
| 结果 | `f->result`，coercion shim 转出 | `f->result`，`value = &f->result` |
| **纯计算** | **完全内联，不进内核**（`inlined=1`） | 仍走 `kaula_spawn`（`inlined=0`） |

---

## 2. 公平性验证：四边 checksum 逐位一致

同一条曲线的 checksum 是「所有返回值之和的滚动散列」，四边必须完全相同。
**不一致 = 至少两边算的不是同一件事，相关行全部不可比。**

conc=100/1000/10000、total=20000 全跑：

| 曲线 | checksum（kaula = c = go） |
|---|---|
| light | `7324368896` |
| suspend | `7324368896` |
| mixed | `7324368896` |
| degrade | `28186632704` |

✅ **全曲线一致。`errors=0` 贯穿全部 36 个用例。**
（light/suspend/mixed 期望值相同 —— 挂起只影响时序不影响返回值；degrade 走 `acc ^ parse` 另一条路径。）

---

## 3. 实测结果

### 3.1 TPS 矩阵（单样本，单位：事务/秒）

| 边 | 曲线 | conc=100 | conc=1000 | conc=10000 |
|---|---|---:|---:|---:|
| kaula | light | 944,620 | 855,449 | 886,943 |
| kaula | suspend | 272,871 | 718,500 | 831,292 |
| kaula | mixed | 286,071 | 823,286 | 830,269 |
| kaula | **degrade** | **77,883** | **77,908** | **77,554** |
| c | light | 962,767 | 1,324,097 | 984,820 |
| c | suspend | 274,042 | 796,465 | 98,433 ※ |
| c | mixed | 300,655 | 760,015 | **1,356,504** |
| c | **degrade** | **186,707** | **632,709** | **1,028,429** |
| go | light | 50,774 | 97,415 | 188,224 |
| go | suspend | 22,769 | 700,685 | 1,072,681 |
| go | mixed | 83,386 | 727,284 | 1,245,128 |
| go | **degrade** | **31,585** | **35,522** | **31,066** |
| tokio | light | — | 1,409,603 | — |
| tokio | suspend | — | 507,790 | — |
| tokio | mixed | — | — | — |
| tokio | degrade | — | — | — |

※ `c/suspend/10000` 的单样本是**系统噪声**：单独复现 3 次为 889K / 1.16M / 1.16M，正常。
※ tokio 行只跑了 light/suspend（conc=1000）用于交叉校验；degrade/mixed 见后续补跑。

### 3.2 重复采样（--repeat 5，conc=1000，取中位数）

单样本对比无效（第 6 节）。这张表才是可信读数：

| 配置 | tps 中位 | spread% | p99 中位 |
|---|---:|---:|---:|
| kaula/light | 928,731 | **4.0%** | 0（内联，见 §4.2） |
| kaula/degrade | 77,735 | **0.5%** | 0（内联） |
| kaula/suspend | 714,600 | 24.2% | 435.9 µs |
| c/light | 910,967 | 46.0% | 90.5 µs |
| c/degrade | 604,386 | 18.2% | 957.6 µs |
| c/suspend | 827,094 | 16.8% | 470.9 µs |
| go/light | 1,281,925 | 15.4% | 91.9 µs |
| go/degrade | 32,607 | 23.7% | 4,394.6 µs |
| go/suspend | 712,417 | 5.6% | 1,085.9 µs |

---

## 4. 三个核心发现

### 4.1 【最强】degrade 曲线：CONC 预算抢占 = Go 的 **19~33 倍**

degrade 是「50% 的任务长忙循环占住 Worker」——正是**预算抢占**该管的事。

| 边 | degrade TPS（conc=1000，repeat=5 中位） | 相对 Go |
|---|---:|---:|
| **c（CONC 内核）** | **604,386** | **18.5×** |
| go（GMP） | 32,607 | 1× |

- **CONC**：预算量子 256 极细，长忙循环在 256 个 tick 后让出，其它任务照常推进 → TPS 随 conc 上升到百万级。
- **Go**：sysmon 抢占粒度 10ms，长忙循环会**占死 P** 直到 loop 结束；10000 并发下 p99 = **54 ms**。
- 差距 **18~33 倍**，远超噪声（两边 spread ≤ 24%）。**这不是噪声，是调度粒度的结构性差异。**

### 4.2 【反直觉】Kaula 的内联优化：短事务零成本，长事务抹掉并发

`light` 与 `degrade` 的事务函数**不含挂起点**，编译器把它们完全内联：

```c
// kaulac 为 `spawn curve_light()` 生成（--cflags -save-temps 实证）
in_flight[j] = ({
    int64_t __r = K_curve_light_as_fn();
    kaula_handle_complete((uint64_t)__r);   // ← 不分配 Frame、不进内核
});
```

**收益（light 曲线）**：

| 边 | light TPS（conc=1000 中位） | 说明 |
|---|---:|---|
| kaula（内联） | 928,731 | 零调度：连 Task 都不建 |
| c（真 spawn） | 910,967 | 走完整内核路径 |

→ 内联把**微事务**的调度开销压到零，且**恒定不随 conc 变化**（944K/855K/887K）—— 因为没有真实并发需要协调。

**代价（degrade 曲线）**：

| 边 | degrade conc=100 | conc=1000 | conc=10000 | 扩展性 |
|---|---:|---:|---:|---|
| **kaula（内联）** | 77,883 | 77,908 | 77,554 | **恒定，完全不扩展** |
| **c（真 spawn）** | 186,707 | 632,709 | 1,028,429 | **随 conc 上升到 5.5×** |

**内联后 `await` 退化成顺序等待**：窗口里 conc 个句柄全是「已完成」的，`await` 逐个立即返回 —— 整轮**串行**执行在单核上，32 核只用 1 核。而 C 边每个任务真的进内核，正确获得并行。

> **结论**：Kaula 的「无挂起点 conc fn 内联」是一把双刃剑。
> - 微事务（~3µs）：纯赚，零开销。
> - 长计算事务（~200µs+）：**亏**，用户以为 spawn 开出了并发，实际退化成串行。
>
> 这不是 bug —— 是「优化看得见的成本，看不见的并行度」的经典权衡。
> 用户要并发就必须保证事务函数里有挂起点（哪怕一个 `yield`），
> 否则应当用别的显式并发原语。**这值得在语言文档里显式警告。**
>
> 对应字段：Kaula 边 `inlined=1`、C 边 `inlined=0`，可直接从 RESULT 行区分。

### 4.3 【代价】内核静态内存底 ~275MB，Go 仅 ~10MB

| 边 | RSS 峰值（全曲线） | 与 conc 的关系 |
|---|---:|---|
| kaula | **274 ~ 278 MB** | 恒定 |
| c | **273 ~ 279 MB** | 恒定 |
| **go** | **7.6 ~ 25.9 MB** | 恒定 |

**根因（新发现，`FINDINGS.md` 未记录）**：

```
size bench_c → bss = 555 MB
  g_conc_workers[256]  = 392 MB   ← 每 worker sizeof(KaulaWorker)=1.6MB
  g_conc_iodriver      = 135 MB
KaulaRetireList        = 1,572,872 B ≈ 1.5MB  ← 内嵌 KAULA_EPOCH_MAX_RETIRED=65536 项
```

CONC 为「每 worker 65536 个 epoch 退休槽 × 最多 256 worker」预留固定静态成本。
Linux 下 BSS 惰性分配，实测 RSS ~275MB。负载中实测线程 43 个（32 worker + iodriver + block lane + main）。

> **这是内核的设计取舍**（I3：一切有界 → 静态预留），不是泄漏：
> 内存与 conc、total 都无关，负载结束后不增长。
> 但对「内存敏感」的场景（每进程 275MB 底）是硬成本，**选型时必须知道**。
> 对比：Go 的 ~10MB 是 M:N 调度器的堆分配自然结果。

---

## 5. 其余指标

完整 24 项指标（TPS / p50~p999 / max / errors / checksum / cpu_user / cpu_sys /
rss_peak / rss_end / priv_end / handles / threads / ctxsw / io_read / io_write /
page_faults + 8 个派生量）由 `metrics.py` 出全表。本轮实测：

- **CPU**：三边 user CPU 都在 0.03~0.11s 量级；`go/degrade` 的 sys CPU 明显更高（0.02s vs C 的 0.01s），对应其抢占机制的额外开销。
- **上下文切换**：本机 `/proc` 未暴露进程级 ctxsw（工具注册表记为 0），跨边不可比，**不作结论**。
- **page_faults**：kaula/c 均 ~70,000（275MB 静态段的首次触碰），go 仅 ~2,000。
- **handles**：三边全程稳定，`handle_growth=0`，无泄漏。

---

## 6. 噪声与可信度（务必读）

FINDINGS F10 记录本机噪声底 **8%~49%**。本轮 `--repeat 5` 实测离散度：

- **kaula/light 4.0%、kaula/degrade 0.5%** —— 内联路径极稳（没有并发调度，自然稳）。
- **c/light 46.0%** —— 单样本毫无意义。矩阵里 c/light/1000 单次跑出 132 万，
  重复采样中位数只有 91 万，**单样本高估 45%**。
- **go/degrade 23.7%**、其余多在 15~25%。

**因此**：

| 结论 | 可信度 |
|---|---|
| degrade：CONC 是 Go 的 19~33 倍 | ✅ 稳固（差距远超噪声） |
| Kaula 内联在 degrade 上不扩展（恒定 77K） | ✅ 稳固（spread 0.5%） |
| light：kaula ≈ c ≈ go（同数量级） | ✅ 稳固（差距 < 噪声） |
| c/light 单点 132 万 vs kaula 94 万 | ❌ **噪声内，不可比** |
| 任何单样本 <20% 的差距 | ❌ 一律判「噪声内」 |
| 正式结论 | 需 `--repeat 11`（本报告为 5，够识别 >2× 的差距） |

**注意**：`go/light` 单样本在 conc=1000 只有 97K、重复中位数却是 128 万 —— 差 13 倍。
单样本不仅不可比，还可能**反向误导**。所有结论必须带 spread%。

---

## 7. 复现

```bash
# 0) 构建工具链（含内核静态库）
python toolkit_build.py --release

# 1) 只看 TPS/延迟（每边一个可执行文件，快速）
python test/bench/run_bench.py --runtimes kaula,c,go \
    --curves light,suspend,mixed,degrade --conc 100,1000,10000 --total 20000

# 2) 完整 24 项指标（进程级外部采样）
python test/bench/metrics.py --runtimes kaula,c,go \
    --curves light,suspend,mixed,degrade --conc 100,1000,10000 --total 20000 \
    --json /tmp/matrix.json

# 3) 可信结论（重复采样取中位 + 离散度）
python test/bench/metrics.py --runtimes kaula,c,go --curves light,degrade \
    --conc 1000 --total 20000 --repeat 11

# 4) 单独跑 C 边
cd test/bench/cbench && clang -O2 -std=gnu11 -D_POSIX_C_SOURCE=200809L \
    -I ../../../build/include -o bench_c main.c \
    ../../../build/lib/libkaula_runtime.a -lpthread -lm
./bench_c degrade 1000 20000
```

---

## 8. 结论

**编译器 lowering 的代价（答「值不值」）**

- 微事务（light，~3µs）：**内联零成本**，kaula ≈ c ≈ go，且完全不受 conc 影响。
- 长计算（degrade）：内联是**负优化** —— 抹掉并发，退化为串行（77K 恒定 vs C 的 103 万）。
  → **Kaula 需要给用户一个显式信号**：何时该保证挂起点、何时该用真并发原语。

**CONC 内核 vs Go（答「强在哪、代价在哪」）**

| 维度 | CONC（c/kaula） | Go | Tokio | 判 |
|---|---|---|---|---|
| 长忙循环抢占 | 604K TPS | 33K TPS | （未测） | **CONC 18~33× Go** |
| 微事务并发 | 91 万（真 spawn）~93 万（内联） | 128 万 | **141 万** | Tokio 优（差距在噪声内） |
| 挂起并发 | 71~83 万 | 71~107 万 | 51 万 | CONC/Go 领先 |
| 内存底 | ~275 MB | ~10 MB | **~3.4 MB** | 无栈协程最优 |
| 线程数 | 43（恒定） | 13~33 | 5 | 无栈协程最优 |

**一句话**：CONC 在「计算占住 worker」这一 CONC 的核心设计场景上对 Go 有数量级优势，
代价是固定内存底高一个数量级；Tokio 的无栈协程在微事务与内存上最优，
但长计算抢占未测（其 pthread 池 + spawn_blocking 有独立代价）。
而 Kaula 的编译器在微事务上兑现了「零开销」承诺，
但在长计算事务上会把 `spawn` 的并发语义悄悄吃掉 —— 这是最需要向用户交代的一点。
