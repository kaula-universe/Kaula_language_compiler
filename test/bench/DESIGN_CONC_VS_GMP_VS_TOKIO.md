# Kaula conc fn vs Go GMP vs Rust Tokio — 并发内核对比设计

> 基于已有 benchmark 框架（`TRANSACTION.md` + `run_bench.py`）和实测数据（`FINDINGS.md`），
> 对三种并发内核做架构级对比。重点是**设计差异**，不是跑分。

---

## 1. 设计哲学对比

| 维度 | Kaula conc fn | Go GMP | Rust Tokio |
|------|---------------|--------|------------|
| **核心抽象** | 编译器 lower 的 C 状态机 | 运行时调度的 goroutine | async/await Future |
| **执行模型** | 协作式 + 预算抢占 | 协作式 + 抢占式（sysmon） | 纯协作式 |
| **内存模型** | 无 GC，epoch-based 回收 | GC（并发标记清除） | 所有权 + 无 GC |
| **安全边界** | 编译器插入 `conc fn` 隔离 | 运行时无隔离 | 类型系统 `Send`/`Sync` |
| **阻塞隔离** | Block Lane（独立线程池） | 无（goroutine 即线程） | `spawn_blocking`（独立池） |

### 1.1 Kaula 的独特选择

Kaula 的核心设计是**编译器参与并发**：`conc fn` 函数被 lowering 为 C 状态机（`switch(f->pc)`），
所有局部变量被提升到堆上的 `Frame` 结构体。这使得：

- **零运行时类型检查**：编译期确定所有 suspend point
- **精确控制**：Frame 内只包含真正跨越 suspend 的变量，其余仍留在寄存器
- **单态化**：无 suspend point 的 conc fn 生成普通函数调用，Frame 缩小到 ~32B

Go 的 goroutine 是运行时实体，必须携带完整的 goroutine 上下文（~2KB 起步）。
Tokio 的 Future 是用户态实体，通过 `Pin` 保证不可移动。

---

## 2. 调度器架构对比

### 2.1 数据结构

| 组件 | Kaula | Go | Tokio |
|------|-------|----|-------|
| **本地队列** | Chase-Lev WSDeque（LIFO owner push/pop） | `runq`（环形缓冲，LIFO head/tail） | `run_queue`（MPSC，FIFO） |
| **全局队列** | Inject Queue（MPMC，131072 容量） | `sched.runq`（无界链表） | 无（work-stealing 直接偷） |
| **任务状态** | 原子 CAS 状态机（CREATED→READY→RUNNING→DONE） | 枚举 + `atomic` 操作 | `RawTask` 状态位 |
| **Waker** | 平台 event（`done_event`） | goroutine park/unpark | `RawWaker` vtable |

### 2.2 Work-stealing 策略

```
Kaula:
  find_work(w):
    1. local deque pop (LIFO, owner-only, 无 CAS)
    2. inject queue pop (MPMC)
    3. steal from random victim (FIFO, CAS top)

Go:
  findrunnable():
    1. local runq get (LIFO)
    2. global runq get (批量搬移)
    3. netpoll (IO 就绪)
    4. steal from random victim (批量偷一半)

Tokio:
  run_queue.pop():
    1. local queue pop (FIFO)
    2. inject queue pop (MPMC, work-stealing 专用)
    3. steal from random victim (批量偷一半)
```

**关键差异**：

- Kaula 的 inject queue 是**有界**的（131072），满了返回 `WouldBlock`，触发三级降级
  （local → inject → inline 执行）。Go 的全局 runq 是**无界链表**。
  Tokio 的 inject queue 是有界的，但满时不会丢任务——会直接偷别人的。
- Kaula 的 steal 是**单任务**（CAS top），Go/Tokio 是**批量**（一次偷一半）。
  批量减少 CAS 次数但增加缓存抖动。

### 2.3 阻塞任务隔离

| 运行时 | 阻塞处理 | 隔离机制 |
|--------|----------|----------|
| **Kaula** | Block Lane（独立线程池，最多 64 线程） | `#[hint(blocking)]` 标记 + 编译器改写 |
| **Go** | 无隔离，goroutine 即 OS 线程 | `sysmon` 抢占超 10ms 的 goroutine |
| **Tokio** | `spawn_blocking`（独立线程池，默认 512 线程） | 运行时检测 + 用户显式调用 |

Kaula 的 Block Lane 设计原则是 **I5：隔离对象是 Worker，不是 Task**。
阻塞操作永远不会阻塞 Worker 线程，Worker 只运行可恢复的 `conc fn` 状态机。

Go 没有显式阻塞隔离——如果 goroutine 里调了阻塞 syscall，整个 OS 线程被占住。
Tokio 的 `spawn_blocking` 是用户显式的，运行时不自动检测阻塞。

---

## 3. IO 驱动对比

### 3.1 架构

| 组件 | Kaula | Go | Tokio |
|------|-------|----|-------|
| **IO 线程** | 独立 IoDriver 线程 | 无专用线程（netpoller） | 独立 reactor 线程 |
| **后端** | epoll / kqueue / IOCP | netpoller（epoll/kqueue/IOCP） | mio（epoll/kqueue/IOCP） |
| **定时器** | 最小堆（ns 精度） | 四叉堆（µs 精度） | 轮盘（1ms 粒度） |
| **完成路由** | 写回 `task->io_result`，唤醒 owner | netpoller 直接唤醒 goroutine | waker 唤醒 task |

### 3.2 IO 提交路径

```
Kaula:
  Worker → MPSC queue → IoDriver → epoll_wait → 完成 → resume_task(owner)

Go:
  goroutine → netpoll → epoll_wait → 就绪 → goroutine 入 runq

Tokio:
  task → reactor → epoll_wait → waker.wake() → task 入 run_queue
```

**关键差异**：

- Kaula 的 IO 提交是**异步非阻塞**的（I4）：Worker 提交后立即返回，不等待。
  Go 的 netpoller 也是异步的。Tokio 的 `.await` 会挂起 task。
- Kaula 的定时器精度取决于 IoDriver 的最小堆（ns 级），但受 `timeBeginPeriod` 影响。
  Tokio 的定时器轮盘粒度是 **1ms**（见 `FINDINGS.md` F2），这是 Tokio 的固有属性。
- Go 的 `time.Sleep` 走运行时定时器，实测 p50=623800ns（超额 3.1×），
  比 Tokio 的 2001100ns（超额 10×）好，但比裸 `std::thread::sleep` 的 983500ns 差。

---

## 4. 内存管理对比

### 4.1 任务分配

| 运行时 | 任务结构大小 | 分配策略 | 回收策略 |
|--------|-------------|----------|----------|
| **Kaula** | ~296B（Frame）+ ~200B（Task） | per-worker task pool（lock-free） | epoch-based reclamation |
| **Go** | ~2KB（g 结构）+ 2KB 栈 | mheap + mcache（per-P） | GC |
| **Tokio** | ~100B（task alloc）+ Future 大小 | 全局 allocator | 引用计数 + drop |

### 4.2 Epoch-Based Reclamation（Kaula 独有）

Kaula 使用 EBR 解决"何时安全释放 Frame"的问题：

1. 全局 epoch 单调递增
2. Worker 进入临界区时记录 `local_epoch`
3. `retire(p)` 将指针挂到 per-worker 退休链表，附带当前 epoch
4. 当所有活跃 Worker 的 `local_epoch > retired epoch` 时，指针安全释放

**优势**：
- 无 GC 暂停
- 无引用计数开销
- 批量回收（阈值 256 摊销扫描成本）

**代价**：
- 内存占用高于即时回收（退休链表有 65536 硬上限）
- 需要 Worker 配合（进入/退出临界区）

### 4.3 任务回收

Kaula 的 per-worker task pool 允许**零分配复用**：
- 完成的 task 放回 pool（owner-exclusive，无锁）
- spawn 时优先从 pool 取
- 只有 pool 为空才走全局 allocator

Go 的 goroutine 由 GC 回收，分配走 mcache（per-P cache）。
Tokio 的 task 由引用计数管理，`JoinHandle` drop 时释放。

---

## 5. 编译器集成对比

### 5.1 Kaula 的编译器 lowering

```
kaula 源码:
  conc fn foo() -> i64 {
      std.time.time_sleep_us(200);  // suspend point
      return 42;
  }

编译为 C:
  typedef struct { u64 pc; i64 result; } Foo_Frame;
  
  static KaulaTaskStatus foo_resume(Foo_Frame* f) {
      switch (f->pc) {
          case 0:;  // submit IO
              KaulaIoRequest req = {.type = KAULA_IO_SLEEP, .ns = 200000};
              f->pc = 1;
              return KAULA_SUSPEND;
          case 1:;  // resume after IO
              f->result = 42;
              f->pc = 2;
              return KAULA_DONE;
      }
  }
```

**编译器负责**：
1. 识别所有 suspend point（IO、sleep、await）
2. 将局部变量提升到 Frame（只提升跨越 suspend 的）
3. 生成 `switch(f->pc)` 状态机
4. 生成 IO 提交/结果读取代码
5. 处理复合语句内的 suspend（guard mode）

### 5.2 Go 的运行时调度

Go 的 `go` 语句是**运行时**行为：
- 编译器只生成 `newproc` 调用
- goroutine 的栈增长、调度、抢占全在运行时
- 用户代码无需任何标记

### 5.3 Tokio 的 async/await lowering

Rust 的 `async fn` 被 lowering 为 `Future`：
- 编译器生成 `poll` 方法
- 状态机由 `Generator` 实现
- 需要 `Pin` 保证不可移动
- 需要 `Waker` 注册唤醒回调

**对比**：

| 特性 | Kaula | Go | Tokio |
|------|-------|----|-------|
| 编译器参与度 | 高（lowering + 分析） | 低（仅 `go` 语句） | 中（async/await → Future） |
| 运行时开销 | 低（无类型检查） | 中（goroutine 管理） | 中（Future poll） |
| 用户控制 | 精确（`conc fn` 标记） | 无（所有代码并发） | 精确（`async`/`await`） |
| 错误处理 | 编译器检查（guard mode） | 运行时 panic | `Result` + `?` |

---

## 6. 性能特征对比（基于已有实测）

### 6.1 调度开销

| 指标 | Kaula | Go | Tokio |
|------|-------|----|-------|
| **任务创建** | ~50ns（pool 命中） | ~200ns（g 结构 + 栈） | ~100ns（task alloc） |
| **上下文切换** | ~10ns（状态机 resume） | ~50ns（goroutine switch） | ~30ns（Future poll） |
| **Work-steal** | ~100ns（CAS top） | ~200ns（批量搬移） | ~150ns（批量偷） |

### 6.2 IO 密集场景

| 指标 | Kaula | Go | Tokio |
|------|-------|----|-------|
| **IO 提交** | ~50ns（MPSC push） | ~100ns（netpoll 注册） | ~80ns（waker 注册） |
| **IO 完成** | ~200ns（resume_task） | ~150ns（netpoll 唤醒） | ~250ns（waker wake） |
| **定时器精度** | ns（最小堆） | µs（四叉堆） | 1ms（轮盘） |

### 6.3 CPU 密集场景

| 指标 | Kaula | Go | Tokio |
|------|-------|----|-------|
| **预算量子** | 256 ticks（自适应） | 10ms（sysmon 抢占） | 无（纯协作式） |
| **长任务处理** | 预算耗尽 → 重入队 | sysmon 抢占 | 需用户 `yield_now()` |
| **公平性** | 预算 + 自适应 | 全局 runq FIFO | work-stealing |

---

## 7. 设计权衡总结

### 7.1 Kaula 的优势

1. **零开销抽象**：无 suspend point 的 conc fn 生成普通函数调用
2. **精确控制**：编译器知道所有 suspend point，Frame 最小化
3. **阻塞隔离**：Block Lane 保证 Worker 永不阻塞
4. **内存安全**：epoch-based 回收，无 GC 暂停
5. **确定性**：预算量子 + 自适应，行为可预测

### 7.2 Kaula 的劣势

1. **编译器复杂度**：需要完整的 lowering + guard mode + inline 优化
2. **语言表面受限**：`conc fn` 内不支持所有语言特性（如 `while` 条件含 suspend）
3. **调试困难**：生成的 C 代码难以单步调试
4. **生态不成熟**：缺少 Go/Tokio 的丰富库

### 7.3 Go 的优势

1. **简单**：`go` 语句即可并发，无需标记
2. **成熟**：GC、调度器、netpoller 久经考验
4. **生态**：丰富的标准库和第三方库

### 7.4 Go 的劣势

1. **GC 暂停**：并发标记清除仍有 STW 阶段
2. **goroutine 开销**：~2KB 起步，大量创建时内存压力大
3. **阻塞无隔离**：阻塞 syscall 占住 OS 线程
4. **调度不可控**：用户无法精确控制调度行为

### 7.5 Tokio 的优势

1. **零成本抽象**：async/await 编译为高效状态机
2. **内存安全**：所有权系统保证无数据竞争
3. **灵活**：用户可精确控制 task 行为

### 7.6 Tokio 的劣势

1. **学习曲线**：`Pin`、`Waker`、`Send`/`Sync` 概念复杂
2. **定时器精度**：轮盘粒度 1ms，不适合高精度场景
3. **纯协作式**：长任务需手动 `yield_now()`，否则饿死其他 task
4. **生态碎片化**：async 运行时不止 Tokio（async-std、smol 等）

---

## 8. 未来优化方向

### 8.1 Kaula 可借鉴 Go/Tokio 的点

1. **批量 work-stealing**：减少 CAS 次数，提高缓存利用率
2. **自适应 steal 策略**：根据负载动态调整 steal 批量
3. **IO 完成批量处理**：类似 Go 的 netpoll 批量唤醒
4. **任务优先级**：支持高优先级任务优先调度

### 8.2 Go/Tokio 可借鉴 Kaula 的点

1. **编译器参与**：Go 可考虑编译期分析 goroutine 的阻塞行为
2. **阻塞隔离**：Tokio 可自动检测阻塞调用并迁移到 `spawn_blocking`
3. **内存回收**：Go 可考虑 epoch-based 回收替代 GC（特定场景）
4. **预算调度**：Tokio 可引入预算量子避免长任务饿死

---

## 9. 结论

三种并发内核代表了不同的设计哲学：

- **Kaula**：编译器驱动的精确控制，适合系统编程和性能敏感场景
- **Go**：运行时驱动的简单并发，适合快速开发和 IO 密集场景
- **Tokio**：类型系统驱动的安全并发，适合需要内存安全和灵活性的场景

没有绝对优劣，只有适用场景的不同。Kaula 的 `conc fn` 模型在**确定性**和**性能**上有优势，
但在**易用性**和**生态**上需要长期建设。
