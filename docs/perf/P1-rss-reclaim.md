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

## 7. 待决项

**是否把 `kmm_v4_idle_reclaim()` 自动接入 `std/concurrent` 的线程池 worker？**

当前**未接入**，理由：worker 在持锁状态下 `condition_wait`，且回收路径含
`malloc_trim(0)`（遍历 glibc arena，延迟不可控，可达毫秒级），在高频任务场景
有拖慢风险，与"零退化"要求冲突。

如需自动化，建议的落点是 `thread_pool_worker` 中 `pending_tasks` 递减之后
（已释放 mutex，且队列空时即将进入等待），并配套：
1. 拆出不含 `malloc_trim` 的轻量版供 worker 使用；
2. 加最小间隔节流（如两次回收至少间隔 N 毫秒）。

---

## 8. 过程中的一次误判（留档）

早期用 Kaula 写的验证程序保存了 `i=0` 的地址、循环结束后再读回，观察到
"12 次 1MB 分配只有 8 个不同地址、后 4 次覆盖前 4 次"，一度判定为 allocator
静默覆盖。实为**测试程序自身的 use-after-free**：生成的 C 在每轮循环末尾已由
liveness 插入 `kmm_v4_survivor_free(p)`，地址复用是正常行为。改成让对象保持
存活后，12 个地址完全不同、数据全部正确。

教训：验证分配器行为时，必须先用 `-verbose` 看生成的 C 确认释放点位置，
否则容易把编译器的结构化回收误判成分配器缺陷。
