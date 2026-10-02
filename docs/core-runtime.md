# 核心运行时

Kaula 的核心运行时由两部分构成：

- **并发内核**（`src/concurrency/`）—— 任务、Worker、调度、IO 驱动、内存回收
- **KMM V4**（`src/kmm_scoped_allocator_v4.{h,c}`）—— 编译期配置探测 + 运行时内存分配

两者的实现、接口、验证方式**完全独立**：并发内核不要求 KMM，KMM 也不知道有个
调度器存在。本文分两章各自完整介绍，不互相依赖。

> 本文描述的是**已实现**的行为。尚未做到的部分集中列在
> [第 5 章 已知缺口](#第-5-章已知缺口)，不回避。

---

# 第 1 章 · 并发内核

## 1.1 一句话定义

Kaula 是一个**语言级并发执行内核**：用户只写 `conc fn`，编译器不分类，
运行时用「预算 + 工作窃取」自动区分计算与阻塞。同一份函数体，
不加 `spawn` 就是零开销的普通函数，加了 `spawn` 就是并发任务。

```kaula
conc fn fetch(url: string) -> string {
    std.net.net_recv(url)        // 可插桩 IO → suspend 点
    return parse(url)
}

fn main() -> i32 {
    string a = fetch("x")         // 顺序调用：栈上跑完状态机，零调度零堆分配
    Handle h = spawn fetch("y")   // 并发提交：进内核
    string b = h.await            // 取结果
    return 0
}
```

**为什么要有「可插桩」这个概念**：内核不预先判断某个函数是计算还是 IO。
它在 lowering 时维护一张「哪些调用可插桩」的表，命中表内的调用在编译期被
劈成两半（提交 / 取结果），未命中的按普通调用生成。运行时再用预算量子与停滞
检测兜住「表漏了」的情况。分类错了只降吞吐，不会给错结果。

## 1.2 六条不变量

这六条是实现必须满足的约束，违反即为 bug，不做权衡。后面每一节都会指出
哪条不变量在起作用。

| 不变量 | 含义 | 由什么保证 |
|---|---|---|
| **I1** 语义唯一 | 只有 `conc fn` 一种并发函数，没有 async/await 分家，没有编译期 CPU/IO 分类 | 背压满时降级为顺序执行（1.6），用户看不出区别，只失去并行性 |
| **I2** 任务单属主 | 任一时刻 Task 有且仅有一个 owner；迁移是一次原子指针改写，不拷贝执行状态 | `KaulaWorker* owner` 字段 + Chase-Lev 队列的 bottom 独占 |
| **I3** 一切有界 | 本地队列、窃取窗口、完成队列、阻塞车道、全局任务数全部有编译期硬上限 | 见 1.7 的常量表 |
| **I4** 可插桩 IO 不占线程 | 能被 IoDriver 接管的 IO，提交后 Worker 立即转做别的事 | 编译器把调用劈成两段（1.4） |
| **I5** 隔离对象是 Worker，不是任务 | 判定"卡住"时换掉卡住的 Worker，任务原地不动 | 停滞检测与撤离（1.9） |
| **I6** 无栈为默认 | 任务状态放在 Frame 里而不是栈上，跨挂起点不搬栈 | 状态机 lowering（1.3） |

I2 的推论值得展开：任务从一个 Worker 迁到另一个 Worker，只是 `owner` 指针的
一次原子改写，执行状态（Frame）原地不动。这避免了「搬栈导致栈上指针悬垂」
这一类问题——那类问题极难复现，也极难定位。

I6 同理：任务没有自己的栈，所以挂起时不需要保存寄存器上下文，只需要一个
程序计数。代价是所有跨挂起存活的变量必须显式提升到 Frame 字段（1.3）。

## 1.3 两种 lowering 形态

`conc fn` 是否含可插桩调用，决定它被编译成哪种形态：

| | 无挂起点 | 含挂起点 |
|---|---|---|
| Frame | 参数 + 结果槽，**无 `pc`** | `pc` + 参数 + 跨挂起变量 + 结果槽 |
| resume | 一次转发直接 DONE | `switch (f->pc)`，第 i 个 case 承载第 i 段函数体 |
| shim | 内联函数体，直接跑 | 栈上建 Frame，循环驱动 resume |
| spawn | `kaula_spawn_simple`（包装帧 + trampoline） | `kaula_spawn`（Frame 直接交给内核） |

三种 C 产物对每个 conc fn 一律生成：

- **`_Frame`**：跨挂起点存活的全部状态。顺序路径在栈上，spawn 路径在堆上。
- **`_resume(void*) → KaulaResumeResult`**：状态机本体，内核唯一的执行入口。
- **`_as_fn(原参数) → 原返回类型`**：coercion shim。顺序调用 conc fn 时走它，
  于是「`conc fn` 能当 `fn` 用」不需要用户改任何代码——这就是「一键并发」：
  执行模式由调用方式决定，不由声明方式决定。

```kaula
conc fn compute_sum(i64 a, i64 b) -> i64 { return a + b }   // 单一形态

conc fn fetch_after_delay(string url, i64 delay_ms) -> string {
    std.time.time_sleep(delay_ms)     // 可插桩 → 双形态
    return url
}
```

**每个 resume case 承载对应的那一段函数体**，并在段首把跨挂起存活的变量从 Frame
恢复成本地变量。段首的恢复不是可选的：上一段是在另一次 resume 调用（另一个栈帧）
里跑的，那些局部变量早已随那次返回一起销毁。

Frame 字段一律用真实 C 类型，不用 `void*` 占位。`void*` 能与任意整型互转，
编译器不会报错，但值已经被静默截断——宁可编译失败，也不要静默给错答案。

#### 内存归属：Frame 变量按值内嵌，不参与 SOR 堆分配

**契约：跨挂起点存活的变量以按值内嵌进 Frame 字段，不接入 KMM V4 分配。**

```c
K_Point p;        /* 内嵌，不是 K_Point* */
int64_t total;
```

resume 段首的恢复是普通的按值拷贝（`K_Point p = f->p;`）。同一变量若出现在
普通 `fn` 里，`--sor` 会给出堆分配决策（`/* sor: BumpPool */` + 作用域末尾
`survivor_free` 精确回收）；在 `conc fn` 里这条路径**整体不适用**。

理由是生命周期对齐：Frame 随任务 retire，挂在 Frame 上的内嵌值随 Frame 一起
释放，回收时机由 epoch 判定统一裁决，不存在二次释放或提前释放的窗口。若改成
`T*` + KMM 分配，就必须让每个 Frame 字段在任务 retire 时逐个 `free`，而 retire
的时机由 epoch 回收器决定——两套回收机制要在同一时刻正确交错，代价远大于
Frame 变大的成本，且失败模式是静默 UAF。

代价是 Frame 会随局部变量总大小膨胀。已知取舍：

- 局部变量总大小受 KMM/Frame 分配上限约束；大数组应放在堆上由调用方持有，
  在 conc fn 里只跨挂起点传**指针**（指针本身 8 字节，内嵌代价可忽略）。
- `needs_free` 字段目前恒为 0：内嵌模型下 Frame 自身无需逐字段释放。
  改用堆分配模型时该字段才会被启用——它是模型切换的预留位，不是当前的活跃路径。

**防回归**：`conc/frame_test.go` 与 `conc/ownership_test.go` 守住这条契约。
若将来要改用堆分配模型（契约 A → B），必须同时解决 epoch pin 与 retire 的
时序，并更新这两处测试。

### 段内含复合语句：守卫槽位

`pc` 表达的是「函数体按**顶层语句**切成了几段」。挂起点落在循环体/分支体里时，
顶层切分看不见它——而把整条复合语句生成两遍（提交半 + 结果半）意味着恢复后
整个循环体再跑一遍。它不崩，只是把副作用做了两遍。

所以对「体内部含挂起点」的复合语句，`while` / `if` / `block` 整条只生成一次，
改由 Frame 的 `sub[d]` 表达恢复位置：每层一个槽位计数器，语句逐条包进守卫。

```kaula
conc fn loop_sum(i64 n) -> i64 {
    i64 total = 0
    i64 i = 0
    while i < n {
        std.time.time_sleep(1)     // 挂起点在循环体内
        total = total + i
        i = i + 1
    }
    return total
}
```

生成的不是「`while` 出现两次」，而是一个状态机：

```c
case 0: {
    int64_t n     = f->n;
    int64_t total = f->total;      /* 段首恢复副本 */
    int64_t i     = f->i;
    if (f->sub[0] == 0) { f->sub[0] = 1; total = 0; }
    if (f->sub[0] == 1) { f->sub[0] = 2; i     = 0; }
    if (f->sub[0] == 2) {                     /* 复合语句不推进，见下 */
    for (;;) {
        if (f->sub[1] == 0) { f->sub[1] = 1; if (!(i < n)) break; }
        if (f->sub[1] == 1) {
            f->sub[1] = 2;
            KaulaIoRequest r = {0}; r.type = KAULA_IO_SLEEP; ...
            f->total = total; f->i = i;       /* 挂起前写回 */
            f->pc = 0; f->sub[1] = 2;
            return (KaulaResumeResult){ .status = KAULA_RESUME_SUSPEND, .io_request = r };
        }
        if (f->sub[1] == 2) { f->sub[1] = 3; total = total + i; }
        if (f->sub[1] == 3) { f->sub[1] = 4; i = i + 1; }
        f->sub[1] = 0;                         /* 每轮复位 */
    }
    f->sub[0] = 3;
    }
    if (f->sub[0] == 3) { f->sub[0] = 4; f->result = total; ... }
}
```

四条要点：

- **挂起返回必须就地发出**。外层的挂起返回挂在复合语句的花括号之外，
  引用不到循环体里刚构造的 `KaulaIoRequest` —— 那是 `use of undeclared
  identifier`。
- **复合语句所在的槽位进入时不推进**。恢复点在那条语句的**内部**，
  推进了就整条跳过它，连带跳过它里面的循环。推进挪到它执行完之后，
  这正好也是 C 的语义：后面的语句只在循环跑完之后才该执行。
- **段体内的变量声明降级成赋值**。守卫槽位是 `if (...) { ... }`，是嵌套
  作用域；声明留在槽位里的话，循环之后的代码看到的是段首那份，累加结果会丢。
- **活跃判据用遍历序号，不用源码行号**。解析器从不填 `CallExpression.Pos`，
  拿行号当界会让「跨挂起存活的变量」一个都判定不出来——不写回、也不恢复，
  恢复后读到的全是上次 resume 留下的旧值。

守卫槽位能表达的形态有边界（一条槽位一个挂起点、层数有上限），做不到的
形态一律编译期报诊断，见第 3 章「守卫槽位的边界」。

## 1.4 关键 ABI 约定

### 可插桩调用 ↔ IO 操作码

编译器与内核必须对同一批调用给出同一批操作码。这条约定写在
`compiler/internal/codegen/conc/analysis.go`（编译器侧注册表）与
`src/concurrency/kaula_conc_task.h`（内核侧 `KaulaIoType`）：

| Kaula 调用 | `KaulaIoType` | 参数 |
|---|---|---|
| `fs_read` | `KAULA_IO_READ` | `(fd, buf, len)` |
| `fs_write` | `KAULA_IO_WRITE` | `(fd, buf, len)` |
| `time_sleep` / `time_after` | `KAULA_IO_SLEEP` | `(ms)`，内核按纳秒算 |

匹配只看函数名的最后一段。这样 `std.time.time_sleep` 与 `time_sleep` 命中同一项——
分析阶段用的是裸名，两边口径不一致会导致「挂起点识别得出、lowering 找不到」，
静默退回原地阻塞。

映射不上的函数**保守退回普通调用**，而不是猜一个操作码让内核去执行错的操作。
这是 I1「分类错了只降吞吐」的落点。

### IO 语义是 fd，不是 socket

内核的 IO 执行用 `read` / `write`，不是 `recv` / `send`：

```c
case KAULA_IO_READ:  return (int)read(req->fd, req->buf, req->buf_len);
case KAULA_IO_WRITE: return (int)write(req->fd, req->buf, req->buf_len);
```

覆盖面上 `read`/`write` 严格更宽 —— POSIX 里 socket 就是 fd，所以网络照样走
同一条路，而普通文件与 pipe 只有 `read`/`write` 能读。原来的 `recv`/`send` 是
反过来：网络能走，文件与 pipe 一进来就 `ENOTSOCK`。

代价是内核不再需要 socket 头与库：`winsock2.h`（必须排在 `windows.h` 之前，
是个顺序陷阱）与 `ws2_32.lib` 都不再是构建前提。

`net_*` 一律不在插桩表里。`net_recv` / `net_send` 在 POSIX 上本来就能靠
`KAULA_IO_READ` / `WRITE` 走通，但登记它们会让 Windows 上同一段代码静默退化
（SOCKET 不是 CRT fd，`_read` 对它无效）；`net_connect` / `net_accept` 更是
socket 专属，`read`/`write` 表达不了。它们统一退回普通阻塞调用：行为正确，
只是那个 Worker 会真的卡住。这比登记成操作码再让内核回 `ENOSYS` 好 ——
后者是把「退化成阻塞」换成了「静默给出操作失败」。

### I4 的具体形态

把可插桩调用劈成两半，是整个内核里最容易做错的一处。对比：

```c
/* ✗ 状态机形状对，但 Worker 真的卡在 sleep 上 */
case 0: { time_sleep(delay_ms);
         return (KaulaResumeResult){ .status = KAULA_RESUME_SUSPEND }; }
case 1: { return (KaulaResumeResult){ .status = KAULA_RESUME_DONE }; }   /* 空壳 */

/* ✓ 实参求值 → 填请求 → 交 IoDriver → 归还 Worker */
case 0: { KaulaIoRequest __kaula_io_1 = {0};
         __kaula_io_1.type = KAULA_IO_SLEEP;
         __kaula_io_1.timeout_ns = (uint64_t)(delay_ms) * 1000000ull;
         f->pc = 1;
         return (KaulaResumeResult){ .status = KAULA_RESUME_SUSPEND,
                                     .io_request = __kaula_io_1 }; }
case 1: { /* 结果已由内核搬进 kaula_conc_tls_io_result */
         return (KaulaResumeResult){ .status = KAULA_RESUME_DONE,
                                     .value = &f->result }; }
```

第一种写法里一个 Worker 卡在 IO 上就是一个 Worker 卡死，而且它不崩，只是让
整体吞吐悄悄下降。验收用例 `test/concurrency/test_i4.kl` 的判据很硬：
若 sleep 是原地阻塞的，两个 200ms 的任务必然串行到 400ms；走 IoDriver 则接近 200ms。

### await 的结果约定

任务结果是「结果所在内存的**地址**」（`&f->result`），不是结果本身。
两条 spawn 路径必须一致，否则 await 会在其中一条上静默解引用一个当量整数。

必须给地址而不是给值：结果类型可能是结构体（如 `String`），位模式转 `void*`
编不过；给地址是唯一能同时覆盖标量与结构体的形式。

## 1.5 模块结构

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

## 1.6 调度路径

```
spawn f(a,b)
   │
   ├─ 分配 Frame → 写参数 → 置 pc=0
   │
   ├─ 三级背压（I3）：
   │   1. 投到 owner Worker 的本地队列（无锁 push_owner）
   │   2. 队列满 → 投到别的 Worker（CAS push_external）
   │   3. 全满 → **原地执行**，降级为顺序调用（I1 不退化）
   │
   └─ Worker 主循环：
        pop 本地 → 无则 steal → 无则扫注入队列 → 无则 park
        └─ resume_fn(frame)
             ├─ DONE            → complete → 完成队列 → 唤醒 await 者 → 退休
             ├─ SUSPEND         → 交 IoDriver → Worker 立刻转做别的事（I4）
             ├─ BUDGET_EXHAUSTED → 让出 Worker，任务回队
             └─ BLOCKED         → 阻塞车道（I5）
```

**为什么 push 要拆成两个入口**：Chase-Lev 队列的 bottom 端只能由 owner 独占写。
而 IoDriver、阻塞车道、`await` 都从外部投递——若一律走 CAS 版本的 `push`，
owner 自己也用 CAS，就破坏了「bottom 独占」的前提，队列能出现两个 bottom。

**为什么 pop/steal 必须用 `seq_cst` 栅栏**：Chase-Lev 论文 §4 要求。少了这个栅栏，
同一个 Task 可能被 owner 的 `pop` 和另一个 Worker 的 `steal` 各取一次——
任务执行两遍。这类错误不会崩，只是结果不对。

**降级而不是拒绝**：三级背压全满时原地执行，保证 I1「语义唯一」——
用户看不出区别，只是失去并行性。这比返回错误或在无界队列上堆积更符合
「写错代码时退化是平缓的」这一取向。

## 1.7 一切有界（I3）

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
配置接口读到的值也会被钳制到这张表——配置不能突破 I3。

## 1.8 内存回收

Task 完成时**不能立即 `free`**：可能还有别的线程持有 `KaulaHandle` 里的裸指针
正在 `await`。内核用分代 epoch 解决：

- 完成时把 Task 与它的两块帧（内核包装帧 + 用户 Frame）挂进当前 Worker 的退休列表；
- `kaula_epoch_try_reclaim` 在所有活跃 Worker 的 epoch 都越过退休时刻时才真正释放。

**非 Worker 线程（`main` / 外部线程）没有 epoch 槽位**，它们改用一个全局 pin 计数
`g_ext_pins`：只要有人在 pin，`kaula_epoch_min_active` 就返回 0，一律判定
「不可回收」。粒度粗，但这是唯一安全的选择——多留一会儿垃圾，比返回一个悬垂
指针便宜得多。

**句柄持有 pin**：安全期的定义是「从 spawn 到 await 读完」。只在 `await` 里 pin
是马后炮：

```
main: spawn 返回 ──── Worker: 完成 → 退休 → 立刻回收 ──── main: pin, await
                    ^^^^^^^^ 这个窗口里帧已经被释放了
```

所以 `kaula_handle_from_task` 拿 pin，归还点是 `kaula_handle_release`。

**归还点有两个**，都是「句柄不再可能被读」的时刻：`await_into` 读完
（声明位置，编译器走这条），以及作用域出口（编译器为每个 `Handle` 局部量
发一次）。少了第二个，「句柄被丢弃且从不 `await`」时那个 pin 永远不还，
会一直挡住回收直到 shutdown，把内存峰值拖到进程结束。

计数放在 `KaulaTask.pin_refs` 而不是句柄结构里，是为了让 release **幂等**：
编译器在 `await_into` 之后再发一次（作用域出口那一发）是正常的，而句柄是
按值传递的，那次调用看不到第一次已经清过标志。放在共享计数上，第二次会
发现计数已归零并补回去，不会把 epoch pin 多放一次——多放一次就是提前
回收，那是悬垂，比泄漏危险得多。

漏发 release 仍然是可能的（编译器侧的保守退化）：pin 不归还 = 泄漏，
可观测；pin 多归还 = 悬垂，不可观测。方向不能反。

`await_into` 还有一个理由：它由调用点提供目标地址与 `sizeof`，拷贝发生在
pin 期间、栈上、大小已知的位置。返回裸指针等于把一个随时会悬垂的地址交给调用者，
而且它往往不崩，只是安静地返回上一次那个任务的旧值。表达式位置的 `await`
返回的正是那种地址——所以它**不**在内部归还 pin（调用方接着要解引用），
归还交给作用域出口那一发 `release`。

## 1.9 阻塞车道与 Worker 撤离（I5）

不可插桩的阻塞调用（如用户标注的 `#[hint(blocking)]`）走独立的阻塞车道。
车道线程数随并发阻塞需求动态伸缩，空闲后自行缩容。

判据用 `busy_count`（提交 +1 / 终结 -1）而不是队列深度：Worker 取任务很快，
队列常常是空的，用深度判断会一直认为不需要扩容。

「卡住」的判定用停滞阈值（默认 10ms）。触发的处置是**换掉那个 Worker**，
任务原地不动重新投递——这就是 I5。之所以不搬任务，是因为任务的执行状态在
Frame 里（I6），而 Frame 里可能有指向栈的指针；搬任务就得搬栈，搬栈就得
处理所有栈上指针的有效性，那是一类极难验证的 bug。

## 1.10 配置与可观测性

环境变量：`KAULA_WORKERS` / `KAULA_BUDGET` / `KAULA_BLOCK_THREADS` / `KAULA_CPUS`。
所有值都会被钳制到 1.7 的硬上限。

编程接口在 `kaula_conc.h`：`kaula_runtime_ensure_init()`（编译器生成的代码自动调）、
`kaula_runtime_shutdown()`、`kaula_conc_metrics_*`。

**初始化是懒的**：编译器生成的代码不要求用户显式 init。否则任务会投进一个
没有 Worker 消费的注入队列，`await` 永久阻塞——程序挂死且一行输出都没有。

**关闭闩锁**：`shutdown` 之后 `g_conc_shut_down` 单调置位，`ensure_init` 不再拉起
运行时。只看 `g_conc_initialized` 是不够的——`shutdown` 会把它清回 0，于是下一次
`ensure_init` 把整套运行时重新拉起来、清掉 shutdown 标志。症状是「关掉之后再
spawn 居然成功了」，而已经 `join` 过的 Worker 不会被重建，任务投进没有消费者的
队列，`await` 永久阻塞。

`enable_metrics` 默认关闭（热路径开销敏感）。开启后：Worker 统计
`tasks_executed` / `budget_exhausted` / `stall_evictions`；IoDriver 统计
`total_submits` / `total_completions` / `total_timer_fires`；epoch 统计
`retired_total` / `reclaimed_total`。

**预算量子自适应只在 `enable_metrics` 时驱动**——自适应算法要读历史执行时长，
不开启就没数据可读，热路径上白付代价。

## 1.11 平台后端

`kaula_conc_platform.h` 把平台差异收敛成一组函数：线程、可唤醒等待、互斥、
事件、时钟、环境变量。IO 复用后端按平台选 epoll(ET) / kqueue / IOCP，
收敛成 4 个操作：`add` / `del` / `wait` / `next_event`。

内核对 socket 没有依赖：IO 执行用 `read`/`write`（fd 语义），非阻塞设置也
只在调用方已引入 socket 头时才走 `ioctlsocket`。所以 `winsock2.h` 与
`ws2_32.lib` 都不是内核的构建前提（详见 §1.4「IO 语义是 fd，不是 socket」）。

IO 后端上有一个容易踩的坑：**唤醒通道用独立的哨兵 fd 标识**，
不用「读出 `ev.data` 与哨兵比较」以外的方式。早期实现把 Task 指针塞进
`ev.data.ptr`，而唤醒 fd 又用 `ev.data.fd`，同一字段被两种语义混用；
且同一 fd 的第二个 Task 会走 `EPOLL_CTL_MOD` 覆盖掉第一个注册，
导致第一个 Task 永久挂起。现在 `ev.data.fd` 只承载 fd 语义，Task 通过
pending 表按 fd 查找。

Windows 上另有一个必须知道的坑：**可唤醒事件用信号量，不用自动复位事件**。
自动复位事件不保留 signal 计数（两次 `SetEvent` 只有一次 `Wait` 通过），
与其他平台的 `pthread_cond` 语义不一致。

## 1.12 验证

```bash
python test/concurrency/run_tests.py                 # 单轮
python test/concurrency/run_tests.py --stress 2      # 每例跑 2 轮
python test/concurrency/run_tests.py --only worker   # 单跑一个
python test/concurrency/run_tests.py --asan          # AddressSanitizer
```

1245 个断言，7 个用例文件：

| 用例 | 断言数 | 覆盖 |
|---|---|---|
| `test_worker.c` | 476 | 预算、自适应、窃取、状态机 |
| `test_task.c` | 451 | Task 生命周期、await、退休、**Handle drop 的 pin 归还** |
| `test_iodriver.c` | 150 | 挂起表、定时器、IO 分派、阻塞车道伸缩 |
| `test_mpsc_queue.c` | 107 | Vyukov 队列并发正确性 |
| `test_epoch.c` | 27 | epoch 回收 |
| `test_platform.c` | 21 | 平台抽象 |
| `test_ws_deque.c` | 22 | Chase-Lev 队列 |

端到端（走完整编译链：`kaulac` → clang → 链接 → 运行）：

```bash
kaulac test/concurrency/test_phase4.kl && ./test/concurrency/test_phase4.exe
# sum = 7            顺序调用 conc fn（shim）
# spawned sum = 30   spawn + await
# delayed = done     含挂起点的 conc fn 顺序调用
# batch = 12         批量 spawn/await

kaulac test/concurrency/test_i4.kl && ./test/concurrency/test_i4.exe
# fast = 42
# slow = ok
# elapsed < 300      两个 200ms sleep 并发，未串行
# i4 ok

kaulac test/concurrency/test_loop_suspend.kl && ./test/concurrency/test_loop_suspend.exe
# loop_sum(4) = 6             循环体内挂起：循环变量跨挂起存活并推进
# nested_sum(3) = 6           嵌套 while：两层槽位各自独立
# branch_pick(4) = 20         if/else 分支内挂起（走 then）
# branch_pick(5) = 10         走 else：一个分支里两次挂起
# loop_with_jumps(6) = 9      break / continue 与挂起点共存
# spawned loop_sum(4) = 6     同一份代码走内核调度路径
```

`test_loop_suspend` 的每一项都断言**精确值**，而不是「没崩」。恢复位置错了
不会崩，只会安静地把副作用做两遍或者少做几轮——那种 bug 只有断言值才抓得住。

编译器侧对应的单元测试在 `compiler/internal/codegen/conc/`（分析、Frame、
resume 分段）与 `compiler/internal/codegen/`（守卫槽位、Handle pin 归还、
conc fn 签名表隔离），以及 `compiler/internal/parser/`、`compiler/internal/sema/`
（`conc fn` / `spawn` / `await` 的语法与语义）。

---

# 第 2 章 · KMM V4

KMM V4 是 Kaula 的内存分配器。它的职责边界很窄：**按大小分配、按对象释放、
按作用域回退**。它不感知调用方在做什么——不管调用方是函数、是循环还是别的什么，
对它都只是一次 `alloc` 或 `free`。

这个边界是刻意的。让分配器知道调用方的意图会带来两个后果：分配路径上多出分支，
以及两个模块互相牵扯。保持单向依赖（KMM 只服务调用方，不反过来了解调用方），
分配器才能被单独验证、单独调参、单独替换。

## 2.1 定位

编译期把平台信息探测完并固化成宏，运行时只留两条分配路径。目标是：
分配操作尽可能退化为指针加法，释放操作尽可能批量完成。

## 2.2 九层编译期配置

`kmm_scoped_allocator_v4.h` 的结构本身就是文档——从上到下九层，每层只依赖
前面的结论：

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
无 SIMD → 8。默认对齐就是最优对齐——这条推论省掉了一整类手工调参。

## 2.3 两条分配路径

```c
/* 路径 A：per-thread heap，纯 bump */
KMM_V4_SCOPE_NAMED("main") {
    Foo* p = kmm_v4_alloc(sizeof(Foo));   /* 线程私有 offset，零原子操作 */
}
/* 作用域结束：offset 回退，一次性回收全部 */

/* 路径 B：survivor 段，供需要活过作用域的对象使用 */
void* q = kmm_v4_alloc_global(64);
kmm_v4_survivor_free(q);
```

**路径 A（per-thread heap）**：线程私有的 offset，分配就是指针加法，
**无锁、无原子**。作用域的 push/pop 只动本线程的 offset
（`kmm_v4_scope_checkpoint` / `rewind`），多线程互不影响。这条路径上
多个线程同时分配不需要任何同步。

**路径 B（survivor 段）**：slab 分配器 + 49 档 size class + 占用位图，
支持 per-object free。快路径是 TLS 缓存最近命中的桶（分配尺寸局部性强，
同一函数内分配的相似大小对象连续命中），命中后跳过桶号计算与懒分配检查，
是 O(1) 无分支。

## 2.4 survivor 段为什么是 slab 而不是 bump

早期 survivor 段是全局池的一个 bump 子区域，**单调增长、永不回收**——
长循环里每轮泄漏一次分配，最终耗尽。现在是 slab + 位图，两条回收路并存：

```c
/* 路 1：精确回收（编译器自动发射，变量死亡点触发） */
kmm_v4_survivor_free(p);

/* 路 2：批量回卷（兜底，用于没有发射精确 free 的场景） */
kmm_survivor_checkpoint_t cp = kmm_v4_survivor_checkpoint();
/* ... */
kmm_v4_survivor_rewind(cp);
```

编译器发射的是**路 1**，checkpoint/rewind 只是兜底。没有兜底的话，
一旦某条路径漏发 free 就是单调耗尽；只有兜底没有精确回收的话，
长循环的稳态内存会远超实际存活量。

## 2.5 所有权语义：严格池

`KMM_V4_ENABLE_FALLBACK` **默认关闭**（所有模式）。原因：fallback 会把 libc
`malloc` 的指针混进 KMM 的返回值，而 `kmm_v4_free` 是 no-op、
`survivor_free` 只收 KMM 区域指针——那块内存永久泄漏，且所有权语义被破坏
（调用方无法判断一个指针该不该释放）。

宁可池耗尽时返回 `NULL`，也要保证**返回的所有指针都在 KMM 管理区域内**。
需要 libc 语义的用户直接用 `std_malloc`，不要混用两个分配器。

## 2.6 线程安全级别

`KMM_THREAD_SAFETY_LEVEL` 由优化级别推出：debug 构建 = 2（完全线程安全），
优化构建 = 1（原子操作 + per-thread heap），未优化 = 0（单线程零开销）。

原子操作统一用 `__atomic_*` 内建。这里有个类型细节：`__atomic_*` 不接受
`_Atomic` 限定类型（clang 会报 "address argument to atomic operation must be a
pointer to integer or pointer"），所以 `KMM_ATOMIC_TYPE` 保持普通整型，
原子性完全由内建函数提供。

## 2.7 32 位目标的 8 字节对齐

i386 上 `uint64_t` 的自然对齐只有 4 字节。clang 对未对齐的 8 字节原子操作不敢
内联 `cmpxchg8b`，会 lower 成 `__atomic_load` / `__atomic_compare_exchange`
等 libcall——没有 `libatomic` 时链接期直接 undefined symbol。

slab 位图用 `KMM_V4_ALIGN64`（`__attribute__((aligned(8)))`）显式对齐解决。
显式对齐后 clang 全部内联，零外部依赖。这类问题只在特定目标上出现，
所以值得在头文件里留一条注释说明原因——否则后人会觉得这个属性是多余的。

## 2.8 诊断输出的写法

诊断用 `KMM_V4_DIAGF(...)` 宏，**不是** `#define fprintf(...) (0)`。

这个头文件会被代码生成器无条件包含在所有 TU 最前面，用户代码随后 include 的
头（`std/io/io.h` → `<stdio.h>`）里的 `fprintf(` 声明会被宏吃掉，展开成 `(0)`
后系统头直接语法报错——而报错位置落在 SDK 头文件里，与真正的原因隔了十万八千里。
宏在无可用 `stderr` 的目标下降级为 `((void)0)`，常量表达式，编译器整段消掉。

## 2.9 主要配置项

| 宏 | 默认 | 说明 |
|---|---|---|
| `KMM_V4_POOL_SIZE` | 256MB（64 位）/ 16MB（32 位） | 主池大小 |
| `KMM_V4_SURVIVOR_REGION_SIZE` | 16MB | survivor 段大小 |
| `KMM_V4_ALIGNMENT` | 由 SIMD 层级推出 | 对齐要求 |
| `KMM_THREAD_SAFETY_LEVEL` | 由优化级别推出 | 线程安全级别 |
| `KMM_V4_ENABLE_FALLBACK` | 0 | 严格池，不混用 libc |
| `KMM_V4_SLAB_PAGED_THRESHOLD` | 1MB | 超过走页式独立分配 |
| `KMM_V4_DEBUG` | 0 | 打开统计与配置输出 |

## 2.10 已知限制

- **`realloc` 不支持跨区域指针**：KMM 指针与 libc 指针不能混在同一个
  `realloc` 调用里。
- **survivor 的 `rewind` 非线程安全**：回卷会释放该 checkpoint 之后的全部对象，
  调用方需确保那些对象没有被其他线程引用。多线程下安全的做法是用
  `survivor_free` 逐个释放。
- **slab 桶槽位固定**（每桶 256 槽）。极端的单尺寸分布会频繁触发桶满→
  批量归还→重试，而不是升级到更大档。这是有意的选择：避免为一个窄分布
  预留过多常驻内存。
- **KMM V3 的 arena 子系统已移除**，相关开关保留为 `KMM_ENABLE_ARENA 0`，
  供后续版本复用。

---

# 第 3 章 · 已知缺口

这一章只列**并发内核**的缺口（KMM 的限制见 2.10）。这些是尚未做到的，
不是待优化的：

| 缺口 | 影响 | 状态 |
|---|---|---|
| **表达式位置的 await** | `h1.await + h2.await` 拿不到目标类型，只能靠 `Handle` 变量名查表解引用；查不到时退回裸地址并留注释 | 声明位置（`i64 x = h.await`）走安全的 `await_into`；pin 已能在作用域出口归还 |
| **复合语句内取用 await 的返回值** | `while { i64 n = fs_read(fd, buf, len) }` 这类写法不成立：提交与取回结果两半分属同一条语句，恢复到后半时前半已经过去了 | 编译期报诊断，不静默取过期的 TLS 结果 |
| **`for .. in` 体内的挂起点** | 迭代下标是编译器生成的局部量，不在 Frame 里，恢复后会从头再数一遍 | 编译期报诊断 |
| **条件里的挂起点** | `while read() < n`：条件每轮求值，「恢复时该不该重算条件」没有确定答案 | 编译期报诊断 |
| **一条语句里多个挂起点** | 一次挂起要占掉两个相邻槽位（提交 / 取回结果），放不下第二对 | 编译期报诊断 |
| **Windows IOCP 后端** | 当前是简化实现：只处理唤醒包，未投递 overlapped 读写。补齐后 Windows 才能做 IO 复用 | 测试已跳过 |
| **形式化验证** | 只有 ASan 与 C 断言覆盖 | TSan / loom / TLA+ 未做 |

前三类新增缺口都来自「循环体 / 分支体里的挂起点」那一条的收尾：守卫槽位
方案让恢复位置能落进复合语句内部（见 1.9），但它的表达力有边界 —— 一条
槽位只能记一个挂起点，而复合语句的恢复位置要同时表达「哪一层循环」和
「哪一条语句」。做不到的形态一律在编译期报诊断，而不是让代码编过、
跑起来安静地拿到过期的结果。

## 3.1 守卫槽位的边界

复合语句体内部含挂起点时，`while` / `if` / `block` 会被整条只生成一次，
改由 Frame 的 `sub[d]`（每层一个槽位计数器）表达「本轮执行到第几条」。
层数上限 `KAULA_CONC_SUB_DEPTH = 8`（I3：一切有界），超出由编译器报诊断。

几条刻意的取舍：

- **不用 `goto` 跳进循环体**。标签之后的代码按词法作用域解析标识符，
  源码变量名在标签之前声明过，跳过那条声明只会读到未初始化的副本。
  要修正就得给段内声明改名，那会牵动整个符号表。守卫方案不需要改名。
- **段体内的变量声明降级成赋值**，对象统一由段首的恢复副本提供。
  守卫槽位是 `if (...) { ... }`，是嵌套作用域；声明留在槽位里的话，
  循环之后的代码看到的是段首那份，累加结果会丢。
- **不提前释放**。提前释放按语句序号决定，而恢复路径会截断在中途。
  少释放是可观测的保守退化（作用域出口照常回收），重复释放是 double-free。
- **段体里指针类型的跨 suspend 局部量不分配对象**，只从 Frame 恢复。
  指针跨挂起点的所有权语义是另一个话题，与恢复位置正交。

这些缺口的共同点是：不会导致崩溃，退化是平缓的、可观测的。
这与内核的整体取向一致——宁可慢一点、保守一点，也不要给出一个静默错误的结果。

---

# 相关文档

- [代码生成](code-generation.md) — 编译器如何产出 C
- [SOR 子结构所有权](sor-system.md) — 编译器自动发射精确释放
- [语义分析](semantic-analysis.md) — `conc fn` / `spawn` / `await` 的检查
- [构建配置](build-config.md) — 编译期宏与选项