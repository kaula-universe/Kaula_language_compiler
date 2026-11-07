# P1 — 峰值后 RSS 不回落（削峰 / 平谷）

状态：**已修复并验证**
基线：`e72bdc3`
护栏测试：`test/perf/p1_rss_reclaim.kl`
C 层复现：`/tmp/dbg/ctest2.c`（12×4MB 三段式，逐阶段打印 RSS）

---

## 1. 现象

峰值过后 RSS 停在高位不动，**显式调用 `kmm_v4_reset()` 也降不下来**。

C 层复现（12 块 × 4MB，分配 → touch → 全部释放 → reset）：

```
S0 baseline        rss=  1576 kB
S1 peak            rss= 14236 kB    ← 峰值
S2 after-free      rss= 14236 kB    ← 释放后一动不动
S3 after-reset     rss= 14236 kB    ← reset 也无效
S4 after-purge     rss=  1784 kB    ← 只有把池清掉才降
```

最后一行是突破口：**只要清掉页式大对象池，RSS 立刻回落**。

---

## 2. 根因（三层，逐层排除）

| 层 | 机制 | 能否回落 | 结论 |
|---|---|---|---|
| 首段 | `mmap` 预留，Linux 惰性提交；`scope_pop`/`mark_restore` 只回卷 offset，从不通知内核 | 否 | 不是本次主因（本例对象不在首段） |
| 扩展段 | `malloc` 获得，`kmm_v4_pool_trim_tail` 能真 `free` | 能，但 LEVEL≥1 下只在 `kmm_v4_reset()` 调用 | 触发面太窄 |
| **paged 池** | 释放时入 Treiber 栈 + `madvise(MADV_FREE)` | **否** | **真凶** |

`MADV_FREE` 是**惰性标记**：内核只在内存压力下才回收，不施压就永远不动。于是
RSS 峰值被"焊"在高位。

### 2.1 附带发现：glibc 动态 mmap 阈值

第一轮回落到 2.8MB，第二轮 free 掉同样的 48MB 却只降 1MB。原因是 glibc 的
`M_MMAP_THRESHOLD` 自适应——mmap 块被 free 后，glibc 把阈值上调到该块大小
（上限 32MB），此后同等尺寸的分配改走 brk 主堆，**free 不再归还 OS**。
补 `malloc_trim(0)` 后第二轮同样回落到 2.8MB。

### 2.2 附带修复：`std/memory` 的 `kmm_v4_reset` 是残缺实现

`std/memory/memory.c` 用 `KMM_V4_RESET_IMPL` 接管了 reset，却只写了
`g_kmm_v4_offset = 0`，丢掉四项：survivor slab 清空、`large_offset` 归零、
thread heap 失效、`trim_tail(0)`。同一家族的 `kmm_v4_usage`（漏 survivor 段）
和 `kmm_v4_available`（多段下 `size_t` 下溢成天文数字）也一并漂移。

---

## 3. 已实施方案

| # | 方案 | 落点 | 热路径代价 |
|---|---|---|---|
| 3 | reset 单一真相源 `kmm_v4_reset_full()` | `.c` 新增，头文件 inline 版与 `std/memory` 导出版共用 | 0（非热路径） |
| 8 | 页大小缓存 | `kmm_v4_page_size()`，去掉每次释放的 `sysconf` | 省 1 次 libc 调用/释放 |
| 4 | 页式池冷块清瘦 | `kmm_v4_paged_pool_drain(keep_bytes)` | 0（只在 idle 调用） |
| 6 | 迟滞护栏 | 双阈值 + 连续 `KMM_V4_IDLE_RECLAIM_STREAK` 次采样 | 0 |
| 1 | idle 回收钩子 | `kmm_v4_idle_reclaim()`，已导出到 `std.memory` | 0 |
| 2 | 扩展段 trim 下沉 | `kmm_v4_pool_grow` 持 `g_grow_flag` 时调用无锁版 `pool_trim_tail_locked` | 0（复用已有临界区） |

### 3.1 为什么不损失性能

`drain` **只清冷块，保留最近释放的 `KMM_V4_PAGED_POOL_STEADY`（默认 4MB）热块**。
Treiber 栈是 LIFO，栈顶即最近释放、最可能复用的块——峰期"免 mmap 复用"的
快路径完整保留。清掉的只是长期无人复用的冷块，且动作发生在 idle。

### 3.2 并发安全性

摘链用 `atomic exchange` 把桶头置 NULL。push 线程若先读到旧 top 再被摘链，
其 CAS 会因当前值为 NULL 而失败并重试，重试读到的 NULL 或新 keep 链都是合法
状态 → 无丢失更新、无写入已释放节点。

---

## 4. 排除的方案

- **方案 5（大块改 `MADV_DONTNEED`）**：1MB 复用要付约 250μs 缺页重填，与
  "零退化"冲突。改由 drain 在 idle 真 `free` 替代，效果相同但代价只在空闲期。
- **方案 7（TLAB 迟滞自适应）**：多线程下每个线程持有更大 TLAB，反而抬高 RSS，
  与削峰目标相反。

---

## 5. 验证

| 项目 | 结果 |
|---|---|
| `scripts/run_tests.py` | **112/112** |
| `scripts/run_tests.py --quick` | **98/98** |
| `test/perf/run_perf.py` | **6/6**（含新增 `p1_rss_reclaim`） |
| `go test ./...` | 全通过 |
| `alloc_bench -sor` RSS | **10124 KB**，与修复前基线 10124 KB 一致，零退化 |
| C 层 RSS 回落 | 两轮均 **14.1MB → 2.8MB** |
| ASan | 干净（修掉一处 `free` 后读 `n->size` 的 use-after-free） |

护栏测试输出：

```
[P1] peak sum=828
[P1] first=0 second=12582912     ← 首次因迟滞不动，第二次回收 12MB
[P1] reclaimed 12582912 bytes, reuse verified
PASS p1_rss_reclaim
```

---

## 6. 使用方式

```kaula
import std.memory

// 在空闲时调用：异步 idle worker、请求间隙、循环外的维护点
i64 freed = as<i64>(std.memory.kmm_v4_idle_reclaim())
```

语义：返还本次归还的字节数；返回 0 表示因迟滞未动作或无内存可回收。
内置迟滞，连续调用安全，不需要调用方自己做节流。

可调宏（`-D` 覆盖）：

| 宏 | 默认 | 含义 |
|---|---|---|
| `KMM_V4_PAGED_POOL_STEADY` | 4MB | idle 回收后保留的热池预算，0 = 全清 |
| `KMM_V4_IDLE_RECLAIM_STREAK` | 2 | 连续满足几次才动作，1 = 不迟滞 |

---

## 7. 是否自动接入线程池 worker（已分析，暂不接入）

### 7.1 实测成本

| 路径 | 延迟 | 备注 |
|---|---|---|
| 快路径（池量未超稳态） | **1.5 ns** | 一次 relaxed load + 一次 store |
| `kmm_v4_paged_pool_drain` | 0.02 ~ 1.05 μs/块 | 160 块共 0.097 ms；76 块曾测到 2.9 ms |
| `malloc_trim(0)` | **0.004 ~ 4.38 ms** | 随待归还量剧烈变化，不可预测 |
| 完整 `idle_reclaim` | 0 ~ 4.30 ms | 同上 |

### 7.2 关键实验：每任务调用（4000 任务，池预热 80MB）

```
总耗时        17.5 ms (4.4 us/任务)   ← 基线不回收为 5.4 us/任务，反而更快
回收延迟 p50  0.0000 ms
回收延迟 p99  0.0000 ms
回收延迟 max  2.7429 ms
>1ms 的调用   1 次 (0.03%)
```

结论是**吞吐与尾延迟的权衡**：

- 99.97% 的调用走快路径，约 1.5 ns，免费；
- 慢路径平均吞吐反而**更快**——池保持精简后，从池里找匹配块的 O(n)
  遍历变短（不回收时池会膨胀到 64MB 上限，alloc 需遍历整条链）；
- 但存在**毫秒级毛刺**（实测 max 2.74 ms），频率约 0.03%。

### 7.3 结论：暂不接入

1. **worker 的"真空闲"在锁内**：`condition_wait` 持 mutex，无法在那里做耗时
   操作；唯一可插入点是"任务刚完成、队列已空"，语义上并非真正空闲。
2. **`malloc_trim` 延迟不可预测**（0.004~4.38 ms），与"零退化"要求冲突。
3. 毛刺虽只有 0.03%，但落在任务完成路径上，对延迟敏感场景不可接受；
   而收益（RSS 回落）只在"峰值已过、确已空闲"时才真正需要。

### 7.4 若确要自动化，推荐形态

**独立低频维护线程**，而非 worker 内联：

- 线程绝大部分时间在 `sleep`，是真 idle，完全不影响任何 worker 的尾延迟；
- 可以包含 `malloc_trim`（慢，但不在关键路径上）；
- 默认关闭，由 `KMM_V4_IDLE_MAINT_THREAD` 之类的开关启用，零默认风险。

若坚持 worker 内联，必须同时满足四点：

1. 用**不含 `malloc_trim`** 的轻量版（剥离最不可控的一项）；
2. **最小间隔节流**（≥50 ms），把毛刺频率压到每小时几次；
3. **单次 drain 块数上限**（如 32 块），封顶单次延迟；
4. 只在**队列空**（即将进入 `condition_wait`）时调用，忙时不插入。

落点：`thread_pool_worker` 中 `kaula_atomic_sub(&pool->pending_tasks, 1)`
之后（此时已 `mutex_unlock`）。无锁读 `pool->task_head == pool->task_tail`
作为"即将空闲"的启发式判断即可，即使偶发误判也无正确性问题。

---

## 8. 过程中的一次误判（留档）

早期用 Kaula 写的验证程序保存了 `i=0` 的地址、循环结束后再读回，观察到
"12 次 1MB 分配只有 8 个不同地址、后 4 次覆盖前 4 次"，一度判定为 allocator
静默覆盖。实为**测试程序自身的 use-after-free**：生成的 C 在每轮循环末尾已由
liveness 插入 `kmm_v4_survivor_free(p)`，地址复用是正常行为。改成让对象保持
存活后，12 个地址完全不同、数据全部正确。

教训：验证分配器行为时，必须先用 `-verbose` 看生成的 C 确认释放点位置，
否则容易把编译器的结构化回收误判成分配器缺陷。
