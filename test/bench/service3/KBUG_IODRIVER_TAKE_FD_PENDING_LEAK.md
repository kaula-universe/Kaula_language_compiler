# IoDriver：`io_take_fd` 命中节点不释放 → 每次 IO 完成泄漏一个 `KaulaIoPending`

> **已修复**（本轮）。
> 严重度：**高（可用性）**。任何「每请求至少一次挂起 IO」的服务形态
> 都会按 **≈48 B/请求** 线性泄漏进程内存，长时压测必 OOM。
> 本仓 `service3` 基准实测 4,903,287 请求 → RSS 增长 233 MB，**零回收**。

## 1. 症状

`service3` 基准闭环压测 `kaula_kl/order_server`（100 并发连接、
`SVC_WORKERS=4`），RSS 与请求数**严格线性**增长且停载不回落：

| 时长 | ΔRSS | B/req |
|---|---|---|
| 30s | +81.7 MB | 53.10 |
| 120s | +310.5 MB | 49.23 |

Go 对照边同口径 **0.60 B/req**（零泄漏），说明问题在 Kaula 运行时而非基准。

## 2. 归因：先分段，再定位

仅看 RSS 无法区分「KMM 段泄漏」「scope 栈泄漏」「libc malloc 泄漏」三类。
用运行期探针（`/tmp/leakdiag/svc/kmm_diag2.c`）周期性打印各段用量：

```
DIAG t=5  rss=200.8MB slab_alloc=64000 sv_usage=64000 scope_depth=0 libc_uord=3309136
DIAG t=50 rss=311.8MB slab_alloc=64000 sv_usage=64000 scope_depth=0 libc_uord=119730960
DIAG t=95 rss=422.1MB slab_alloc=64000 sv_usage=64000 scope_depth=0 libc_uord=235381872
```

**决定性证据**：

| 段 | 行为 | 结论 |
|---|---|---|
| `slab_alloc` / `sv_usage` | 恒定 64000 | KMM 幸存段**不泄漏** |
| `scope_depth` | 恒定 0 | scope 栈**不泄漏** |
| `libc_uord` | 3.3 MB → 235.4 MB（+232 MB） | **libc malloc 泄漏** |

`libc_uord` 增速换算：`(235381872-3309136) / 4903287 = 47.30 B/req`，
与 glibc `malloc(32)` 的实际 chunk 尺寸 **48 B** 吻合。

## 3. 根因

`src/concurrency/kaula_conc_iodriver.c` 的 `io_take_fd()`：

```c
if ((p->want & events) || (events & KAULA_IO_EV_ERROR)) {
    *link = p->next;                        /* 从桶链表摘下 */
    atomic_fetch_sub_explicit(&g_conc_iodriver.pending_count, 1, ...);
    p->got = events;
    if (n < max_out) out[n++] = p->task;    /* ← 只取了 task，p 本身没释放 */
    else free(p);                           /* ← 只有容量溢出才 free */
}
```

正常路径把节点 `p` 从链表摘除后**只取走 `p->task`**，节点内存再也没有
任何引用也没被 `free` —— **每次 IO 完成泄漏一个 `KaulaIoPending`（32 B，
glibc 实际 chunk 48 B）**。

服务每请求一次 `net_recv` 挂起 → 每请求泄漏一次，与实测 47–49 B/req 精确对应。

`owner` 语义上，`p->task` 指向的 `KaulaTask` 由调度器持有，节点只是一次
挂起登记的载体，摘下即完成使命，可安全释放；`out[]` 只保存 `task` 指针，
不引用节点内存。

## 4. 修复

```c
if (n < max_out) {
    out[n++] = p->task;
    free(p);
} else {
    /* 超出本批容量：丢弃唤醒（不应发生），节点同样必须释放 */
    free(p);
}
```

## 5. 验证

### 5.1 运行期探针（4,987,334 请求，90s）

| 指标 | 修复前 | 修复后 |
|---|---|---|
| `libc_uord` | 3.3 MB → **236.4 MB** | 229,648 → **225,952 B**（零增长） |
| `libc_arena` | 4.3 MB → 236 MB | **2,334,720 B 恒定** |
| `rss` | 200.8 → **422.1 MB** | **197.7 MB 恒定** |
| TPS | 55,340 | 55,203（无退化） |

### 5.2 端到端基准（`run_service3.py`，120s）

| 运行时 | ΔRSS | B/req | minflt | TPS |
|---|---|---|---|---|
| **kaula** | **0.5 MB** | **0.08** | **114** | 55,260 |
| go | 5.4 MB | 0.61 | 12,283 | 77,878 |

修复前 kaula 为 `310.5 MB / 49.23 B/req` —— **B/req 下降 616 倍**，
`minflt` 从 79,486 降至 114（页分配几乎消失）。

### 5.3 长时阶梯（零增长复现）

```
120s  ok=   6,553,893  ΔRSS=0.0MB  B/req=0.0000
240s  ok=  13,329,459  ΔRSS=0.0MB  B/req=0.0000
```

请求数翻倍（6.5M → 13.3M）而 ΔRSS 保持 0 —— 泄漏已彻底消除，
内存占用与压测时长**解耦**。
