# 并发内核与 KMM V4

本文档介绍 Kaula 语言的**并发内核**（`src/concurrency/`）与**内存管理器 KMM V4**
（`src/kmm_scoped_allocator_v4.{h,c}`）。两者是托管模式与裸机模式共用的底座：
同一套代码、同一套测试，只有后端不同。

> 本文档描述的是**已实现**的行为。设计意图与实现的差距，见
> [第 6 章 已知缺口](#第-6-章已知缺口)——那一章不回避问题。

---

## 第 1 章 · 并发内核

### 1.1 一句话定义

Kaula 是一个**语言级并发执行内核**：用户只写 `conc fn`，编译器不分类，
运行时用「预算 + 工作窃取」自动区分计算与阻塞。同一份函数体，
不加 `spawn` 就是零开销的普通函数，加了 `spawn` 就是并发任务。

```kaula
conc fn fetch(url: string) -> string {
    std.net.net_connect(url)     // 可插桩 IO → suspend 点
    return parse(url)
}

fn main() -> i32 {
    string a = fetch("x")         // 顺序调用：栈上跑完状态机，零调度零堆分配
    Handle h = spawn fetch("y")   // 并发提交：进内核
    string b = h.await            // 取结果
    return 0
}
```

### 1.2 两条硬不变量

设计说明书 §1.2 列了六条，这里挑两条**决定实现形态**的：

| 不变量 | 含义 | 体现在哪 |
|---|---|---|
| **I4** 可插桩 IO 不占线程 | 能被 IoDriver 接管的 IO，提交后 Worker 立即转做别的事 | `conc_io_rewrite.go` 把调用劈成「提交」+「取结果」两段 |
| **I5** 隔离对象是 Worker，不是任务 | 判定"卡住"时换掉 Worker，任务原地不动 | `kaula_conc_evict.c` 停滞检测 |

I4 是最容易做错的一条。早期实现生成了这样的代码：

```c
case 0: { time_sleep(delay_ms);                 /* ← 真的阻塞了 */
         return (KaulaResumeResult){ .status = KAULA_RESUME_SUSPEND, .value = NULL }; }
case 1: { return (KaulaResumeResult){ .status = KAULA_RESUME_DONE }; }  /* ← 空壳 */
```

状态机形状是对的，但 Worker 真的卡在 `sleep` 上——一个 Worker 卡死就是一个
Worker 卡死，而且它不崩，只是让整体吞吐悄悄下降。现在的形态是：

```c
case 0: { KaulaIoRequest __kaula_io_1 = {0};
         __kaula_io_1.type = KAULA_IO_SLEEP;
         __kaula_io_1.timeout_ns = (uint64_t)(delay_ms) * 1000000ull;
         f->pc = 1;
         return (KaulaResumeResult){ .status = KAULA_RESUME_SUSPEND,
                                     .io_request = __kaula_io_1 }; }
case 1: { /* 结果已由内核搬进 kaula_conc_tls_io_result */
         return (KaulaResumeResult){ .status = KAULA_RESUME_DONE, .value = &f->result }; }
```

验收用例 `test/concurrency/test_i4.kl` 判据很硬：若 sleep 是原地阻塞的，
两个 200ms 的任务必然串行到 400ms；走 IoDriver 则接近 200ms。

### 1.3 两种 lowering 形态

`conc fn` 是否含可插桩调用，决定它被编译成哪种形态：

| | 无挂起点 | 含挂起点 |
|---|---|---|
| Frame | 参数 + 结果槽，**无 `pc`** | `pc` + 参数 + 跨挂起变量 + 结果槽 |
| resume | 一次转发直接 DONE | `switch (f->pc)`，第 i 个 case 承载第 i 段函数体 |
| shim | 内联函数体，直接跑 | 栈上建 Frame，循环驱动 resume |
| spawn | `kaula_spawn_simple`（包装帧 + trampoline） | `kaula_spawn`（Frame 直接交给内核） |

三种 C 产物对每个 conc fn 一律生成（`Frame` / `_resume` / `_as_fn`）：

- **`_Frame`**：跨挂起点存活的全部状态。顺序路径在栈上，spawn 路径在 KMM 上。
- **`_resume(void*) → KaulaResumeResult`**：状态机本体，内核唯一的执行入口。
- **`_as_fn(原参数) → 原返回类型`**：coercion shim。顺序调用 conc fn 时走它，
  于是「`conc fn` 能当 `fn` 用」不需要用户改任何代码。

### 1.4 关键 ABI 约定

编译器与内核必须对同一批调用给出同一批 op 码（文档 §9.1）。这条约定写在
`compiler/internal/codegen/conc/analysis.go`（编译器侧注册表）与
`src/concurrency/kaula_conc_task.h`（内核侧 `KaulaIoType`）：

| Kaula 调用 | `KaulaIoType` | 备注 |
|---|---|---|
| `net_recv` / `fs_read` | `KAULA_IO_READ` | `(fd, buf, len)` |
| `net_send` / `net_write` / `fs_write` | `KAULA_IO_WRITE` | 同上 |
| `net_connect` | `KAULA_IO_CONNECT` | |
| `net_accept` | `KAULA_IO_ACCEPT` | |
| `time_sleep` / `time_after` | `KAULA_IO_SLEEP` | 实参毫秒，内核按纳秒算 |

映射不上的函数**保守退回普通语义**（退化成阻塞），而不是猜一个 op 码让内核去执行
错的操作。分类错了只降吞吐，不出错结果。

**await 的结果约定**：任务结果是「结果所在内存的**地址**」（`&f->result`），
不是结果本身。两条 spawn 路径必须一致，否则 await 会在其中一条上静默解引用一个
当量整数。

### 1.5 模块结构

```
src/concurrency/
├── kaula_conc.h                  统一对外头（用户代码只需要这一个）
├── kaula_conc_platform.h         平台抽象：线程/事件/可唤醒等待/时间/环境变量
├── kaula_conc_task.{h,c}         Task / Handle / await / 帧退休
├── kaula_conc_ws_deque.h         Chase-Lev 工作窃取双端队列
├── kaula_conc_mpsc_queue.h       Vyukov 序号 MPSC（IoDriver 提交通道）
├── kaula_conc_scheduler.{h,c}    spawn 三级背压 + trampoline
├── kaula_conc_worker.{h,c}       Worker 主循环 / 窃取 / resume 分派
├── kaula_conc_iodriver.{h,c}     IO 驱动：挂起表 + 定时器堆
├── kaula_conc_io_backend.{h,c}   epoll / kqueue / IOCP 收敛层
├── kaula_conc_io_primitives.c    recv / send / accept / connect 薄封装
├── kaula_conc_block_lane.{h,c}   阻塞车道（动态伸缩）
├── kaula_conc_epoch.{h,c}        分代 epoch 回收
├── kaula_conc_completion.{h,c}   完成队列（分片）
├── kaula_conc_evict.{h,c}        停滞检测与 Worker 撤离
├── kaula_conc_budget.{h,c}       预算量子与自适应
├── kaula_conc_metrics.{h,c}      可观测性（默认关闭）
└── kaula_conc.c                  init / shutdown / 配置解析
```

### 1.6 调度路径

```
spawn f(a,b)
   │
   ├─ 分配 Frame（KMM survivor slab）→ 写参数 → 置 pc=0
   │
   ├─ 三级背压：
   │   1. 投到 owner Worker 的本地队列（无锁 push_owner）
   │   2. 队列满 → 投到别的 Worker（CAS push_external）
   │   3. 全满 → **原地执行**，降级为顺序调用（语义唯一 I1 不退化）
   │
   └─ Worker 主循环：
        pop 本地 → 无则 steal → 无则扫注入队列 → 无则 park
        └─ resume_fn(frame)
             ├─ DONE    → complete → 完成队列 → 唤醒 await 者 → epoch 退休
             ├─ SUSPEND → 交 IoDriver → **Worker 立刻转做别的事**（I4）
             ├─ BUDGET_EXHAUSTED → 让出 Worker，任务回队
             └─ BLOCKED → 阻塞车道（I5）
```

**为什么 push 要拆成两个入口**：Chase-Lev 队列的 bottom 端只能由 owner 独占写。
而 `IoDriver`、阻塞车道、`await` 都从外部投递——早期版本一律走 CAS 版本的
`push`，owner 自己也用 CAS，破坏了"bottom 独占"的前提，队列能出现两个 bottom。

**为什么 pop/steal 必须用 `seq_cst` 栅栏**：Chase-Lev 论文 §4 要求。少了这个栅栏，
同一个 Task 可能被 owner 的 `pop` 和另一个 Worker 的 `steal` 各取一次——任务执行两遍。

### 1.7 一切有界（I3）

所有队列与计数都有**编译期硬上限**，满则背压，没有一处是无界的：

| 常量 | 值 | 含义 |
|---|---|---|
| `KAULA_MAX_WORKERS` | 256 | Worker 硬上限 |
| `KAULA_MAX_TASKS` | 1 000 000 | 全局活跃任务硬上限 |
| `KAULA_WS_DEQUE_CAPACITY` | 4096 | 单 Worker 本地队列 |
| `KAULA_INJECT_QUEUE_CAPACITY` | 8192 | 全局注入队列 |
| `KAULA_COMPLETION_SHARDS` × `CAPACITY` | 16 × 1024 | 完成队列 |
| `KAULA_BLOCK_LANE_MAX_THREADS` | 64 | 阻塞车道线程上限 |
| `KAULA_IODRIVER_MAX_PENDING` | 65536 | IO 挂起表上限 |
| `KAULA_EPOCH_MAX_RETIRED` | 65536 | 单线程退休对象上限 |

超出返回 `KAULA_ERR_WOULD_BLOCK`，不崩溃、不无界增长。

### 1.8 内存回收

Task 完成时**不能立即 `free`**：可能还有别的线程持有 `KaulaHandle` 里的裸指针
正在 `await`。内核用分代 epoch（`kaula_conc_epoch.c`）解决：

- 完成时把 Task 与它的两块帧（内核包装帧 + 用户 Frame）挂进当前 Worker 的退休列表；
- `kaula_epoch_try_reclaim` 在所有活跃 Worker 的 epoch 都越过退休时刻时才真正释放。

**非 Worker 线程（`main` / 外部线程）没有 epoch 槽位**，它们改用一个全局 pin 计数
`g_ext_pins`：只要有人在 pin，`kaula_epoch_min_active` 就返回 0，一律判定"不可回收"。
粒度粗，但这是唯一安全的选择——多留一会儿垃圾，比返回一个悬垂指针便宜得多。

**句柄持有 pin**：安全期的定义是「从 spawn 到 await 读完」。只在 `await` 里 pin 是
马后炮：

```
main: spawn 返回 ──── Worker: 完成 → retire → 立刻 reclaim ──── main: pin, await
                    ^^^^^^^^ 这个窗口里帧已经被释放了
```

所以 `kaula_handle_from_task` 拿 pin，`await_into` 读完放掉。

### 1.9 配置与环境变量

`KAULA_WORKERS` / `KAULA_BUDGET` / `KAULA_BLOCK_THREADS` / `KAULA_CPUS`。
所有值都会被**钳制到上表的编译期硬上限**，配置不能突破 I3。

编程接口在 `kaula_conc.h`：`kaula_runtime_ensure_init()`（编译器生成的代码自动调）、
`kaula_runtime_shutdown()`、`kaula_conc_metrics_*`。

**关闭闩锁**：`shutdown` 之后 `g_conc_shut_down` 单调置位，`ensure_init` 不再拉起运行时。
只看 `g_conc_initialized` 是不够的——`shutdown` 会把它清回 0，于是下一次
`ensure_init` 把整套运行时重新拉起来、清掉 shutdown 标志。症状是"关掉之后再 spawn
居然成功了"，而已经 `join` 过的 Worker 不会被重建，任务投进没有消费者的队列，
`await` 永久阻塞。

### 1.10 平台后端

`kaula_conc_platform.h` 把三个平台差异收敛成一组函数：线程、可唤醒等待、
互斥、事件、时钟、环境变量。IO 复用后端按平台选 epoll(ET) / kqueue / IOCP。

Windows 上有一个必须知道的坑：**可唤醒事件用信号量，不用自动复位事件**。
自动复位事件不保留 signal 计数（两次 `SetEvent` 只有一次 `Wait` 通过），
与 POSIX `pthread_cond` 版本语义不一致。

---

## 第 2 章 · KMM V4

### 2.1 定位

KMM V4 是 Kaula 的内存管理器：编译期配置探测 + 运行时 slab / per-thread heap。
它与并发内核的接口只有一处——并发内核的所有运行时分配都走
`kmm_v4_alloc_global` / `kmm_v4_survivor_free`。

托管模式与裸机模式共用同一份代码，只是宏不同：

| | 托管（hosted） | 裸机（freestanding） |
|---|---|---|
| 池来源 | 系统分配 / mmap 预留 | `KMM_V4_STATIC_POOL` 静态池 |
| 默认池大小 | 256MB（64 位）/ 16MB（32 位） | 16MB |
| survivor 段 | 16MB 独立预留 | 池的 1/8 |
| 线程安全级别 | 优化构建 = 1（轻量实时） | 0（单线程零开销） |
| libc | 有 | 无（`-ffreestanding -nostdlib`） |

### 2.2 九层编译期配置

`kmm_scoped_allocator_v4.h` 的结构本身就是文档——从上到下九层，每层只依赖前面的结论：

| 层 | 内容 |
|---|---|
| 1 | 编译器特性检测（GCC/Clang 宏、`__builtin_expect`） |
| 2 | 操作系统检测 |
| 3 | CPU 架构检测 |
| 4 | SIMD 指令集检测（决定对齐要求） |
| 5 | 缓存与内存层次检测 |
| 6 | 性能模式自动选择（`KMM_V4_OPT_LEVEL`） |
| 7 | 功能开关（线程安全级别、回退策略、arena 等） |
| 8 | 编译期验证（冲突组合直接 `#error`） |
| 9 | 配置摘要输出（`KMM_V4_DEBUG`） |

对齐策略直接由 SIMD 层级推出：AVX-512 → 64 字节，AVX2 → 32，SSE/NEON → 16，
无 SIMD → 8。

### 2.3 两条分配路径

```kaula
// 作用域内：per-thread heap bump，零原子操作
KMM_V4_SCOPE_NAMED("main") {
    Foo* p = kmm_v4_alloc(sizeof(Foo));   // 走 bump
}
// 作用域结束：offset 回退，全部一次性回收
```

- **per-thread heap**（`kmm_v4_bump`）：线程私有 offset，分配是纯指针加法，
  **无锁无原子**。作用域 push/pop 只动本线程 offset（`kmm_v4_scope_checkpoint`
  / `rewind`），多线程互不影响。
- **survivor 段**（`kmm_v4_alloc_global`）：需要活过作用域的对象走这里。
  slab 分配器 + 49 档 size class + 占用位图，支持 per-object free。
  快路径是 TLS 缓存最近命中的桶（分配尺寸局部性强），命中后跳过桶号计算与懒分配检查。

### 2.4 survivor 段为什么是 slab 而不是 bump

早期 survivor 段是全局池的一个 bump 子区域，**单调增长、永不回收**——长循环里
每轮泄漏一次分配，最终耗尽。现在是 slab + 位图，两条回收路：

```c
void* p = kmm_v4_alloc_global(64);
kmm_v4_survivor_free(p);                    /* 精确回收（编译器自动发射） */

kmm_survivor_checkpoint_t cp = kmm_v4_survivor_checkpoint();
/* ... */
kmm_v4_survivor_rewind(cp);                  /* 批量回卷（兜底） */
```

编译器发射的是**精确 free**（变量死亡点 → `kmm_v4_survivor_free`），
checkpoint/rewind 只是兜底——没发射 free 时不至于单调耗尽。

### 2.5 所有权语义：严格池

`KMM_V4_ENABLE_FALLBACK` **默认关闭**（所有模式）。原因：fallback 会把 libc
`malloc` 的指针混进 KMM 的返回值，而 `kmm_v4_free` 是 no-op、`survivor_free` 只收
KMM 区域指针——那块内存永久泄漏，且所有权语义被破坏。

宁可池耗尽时返回 `NULL`，也要保证**返回的所有指针都在 KMM 管理区域内**。
用户需要 libc 就直接用 `std_malloc`。

### 2.6 线程安全级别

`KMM_THREAD_SAFETY_LEVEL` 由优化级别推出：debug 构建 = 2（完全线程安全），
优化构建 = 1（原子操作 + per-thread heap），未优化 = 0（单线程零开销）。

原子操作统一用 `__atomic_*` 内建。`__atomic_*` 不接受 `_Atomic` 限定类型
（clang 会报 "address argument to atomic operation must be a pointer to integer
or pointer"），所以 `KMM_ATOMIC_TYPE` 保持普通整型，原子性完全由内建提供。

### 2.7 32 位裸机的一个坑

i386 上 `uint64_t` 的自然对齐只有 4 字节，clang 对未对齐的 8 字节原子操作不敢内联
`cmpxchg8b`，会 lower 成 `__atomic_load` / `__atomic_compare_exchange` 等 libcall
——裸机没有 `libatomic`，链接期直接 undefined symbol。

slab 位图用 `KMM_V4_ALIGN64`（`__attribute__((aligned(8)))`）显式对齐解决。
显式对齐后 clang 全部内联，零外部依赖。

### 2.8 诊断输出的写法

诊断用 `KMM_V4_DIAGF(...)` 宏，**不是** `#define fprintf(...) (0)`。
这个头文件会被代码生成器无条件包含在所有 TU 最前面，用户代码随后 include 的头
（`std/io/io.h` → `<stdio.h>`）里的 `fprintf(` 声明会被宏吃掉，展开成 `(0)` 后
UCRT 头直接语法报错——而报错位置落在 Windows SDK 里，与真正的原因隔了十万八千里。

freestanding 无 `stderr`，诊断降级为 `((void)0)`，常量表达式，编译器整段消掉。

---

## 第 3 章 · 两者的接口

```
编译器
  │
  │ spawn / await / conc fn lowering
  ▼
并发内核 (src/concurrency/)
  │  所有运行时分配
  ▼
KMM V4 (src/kmm_scoped_allocator_v4.h)
```

并发内核是 KMM 的**唯一大客户**，但 KMM 对内核一无所知。这个方向很重要：
内存管理器不该知道有个调度器，否则两个模块会互相牵扯。

---

## 第 4 章 · 测试

### 4.1 C 层

```bash
python test/concurrency/run_tests.py                 # 单轮
python test/concurrency/run_tests.py --stress 2      # 每例跑 2 轮
python test/concurrency/run_tests.py --only worker   # 单跑一个
python test/concurrency/run_tests.py --asan          # AddressSanitizer
```

1248 个断言，7 个用例文件：

| 用例 | 断言数 | 覆盖 |
|---|---|---|
| `test_worker.c` | 476 | 预算、自适应、窃取、状态机 |
| `test_task.c` | 436 | Task 生命周期、await、退休 |
| `test_iodriver.c` | 159 | 挂起表、定时器、阻塞车道伸缩 |
| `test_mpsc_queue.c` | 107 | Vyukov 队列并发正确性 |
| `test_epoch.c` | 27 | epoch 回收 |
| `test_platform.c` | 21 | 平台抽象 |
| `test_ws_deque.c` | 22 | Chase-Lev 队列 |

### 4.2 Go 层

编译器侧：`conc_test.go`（parser / sema）、`analysis_test.go`、`frame_test.go`。

### 4.3 端到端

`test/concurrency/test_phase4.kl` 与 `test_i4.kl` 走完整编译链
（`kaulac` → clang → 链接 → 运行）。

```bash
kaulac test/concurrency/test_phase4.kl && ./test/concurrency/test_phase4.exe
# sum = 7            顺序调用 conc fn（shim）
# spawned sum = 30   spawn + await
# delayed = done     含挂起点的 conc fn 顺序调用
# batch = 12         批量 spawn/await
```

---

## 第 5 章 · 调试与可观测性

`enable_metrics` 默认关闭（热路径开销敏感）。开启后：

- Worker 统计 `tasks_executed` / `budget_exhausted` / `stall_evictions`
- IoDriver 统计 `total_submits` / `total_completions` / `total_timer_fires`
- epoch 统计 `retired_total` / `reclaimed_total`

**预算量子自适应只在 `enable_metrics` 时驱动**——自适应算法要读历史执行时长，
不开启就没数据可读，热路径上白付代价。

---

## 第 6 章 · 已知缺口

这一章不回避。上面描述的是意图与已实现部分，以下是**尚未做到**的：

| 缺口 | 影响 | 状态 |
|---|---|---|
| **loop / branch 内的挂起点** | `concBodySegments` 按**顶层语句**切分状态机。循环体内的挂起不会被正确切分 | 已知边界，测试未覆盖 |
| **Handle 的 drop 语义** | 句柄持有 pin。句柄被丢弃且从不 `await` 时那个 pin 不归还，会挡回收到 shutdown | 保守方向：泄漏可观测、悬垂不可观测 |
| **表达式位置的 await** | `h1.await + h2.await` 拿不到目标类型，只能靠 `Handle` 变量名查表解引用；查不到时退回裸地址并留注释 | 声明位置（`i64 x = h.await`）走安全的 `await_into` |
| **Windows IOCP 后端** | 当前是简化实现，socket 路径在 Windows 不可用 | 测试已跳过 |
| **`concFuncRegistry`** | 包级全局 map，多文件编译会互相污染 | 应改为 per-CodeGenerator |
| **TSan / loom / TLA+** | 只有 ASan 验证过 | 未做 |
| **嵌套在循环内的预算分裂** | 与第一个缺口同源 | |

设计说明书 §16「诚实的代价与定位」里说过：Kaula 卖的是**错误下限**——
写错代码时，退化是平缓的、可观测的、不死锁的、不泄漏的。上表里每一项
都符合这个描述：不会崩，最坏情况是降级或等到 shutdown。

---

## 相关文档

- [代码生成](code-generation.md) — 编译器如何产出 C
- [SOR 子结构所有权](sor-system.md) — 编译器自动发射精确 free
- [裸机支持](bare-metal.md) — freestanding 模式的构建与 KMM 静态池
- [语义分析](semantic-analysis.md) — `conc fn` / `spawn` / `await` 的检查