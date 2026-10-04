# 基准跑出的问题（按发现顺序）

跑三运行时矩阵时冒出来的东西。每条都附复现方式与实测数据，
方便直接转成 issue，而不是「某个用例偶尔超时」这种不可操作的描述。

---

## F1：并发投递有**两处**丢任务竞态 → 挂起曲线随机死锁（已修）

**严重度**：高。丢的是任务本身，不是性能，且**无法自愈** —— worker 那个
1ms park 超时只捞得到「还躺在队列里」的任务，捞不到已蒸发的。

**症状**

| 曲线 | 修复前 |
|---|---|
| `light` | 通过，无一例挂 |
| `degrade` | 通过 |
| `suspend` conc=100 total=1000 | 2/10 通过（80% 挂死） |
| `suspend` conc=50 total=1000 | 2/4 通过 |
| `suspend` conc=20 total=1000 | 4/4 通过 |
| `suspend` / `mixed` conc=100,1000 total=2000 | **0/20 通过**（矩阵里 4 个用例全部 5 次重试耗尽） |

挂死时进程既不退出也不烧 CPU（CPU 停在 0.047s、11 线程），
主流程卡在 `await` 上 —— 它在等一个**永远不会完成**的任务。

**定位过程**（`test/bench/make_trace.py` 给模板插桩，逐窗口 `file_flush` 落盘）

`std.io.println` 在管道下是**全缓冲**，两行都在进程退出瞬间才到，
进程一挂就一行都看不到。改用 `file_printf + file_flush` 后拿到：

```
BEGIN
EXPECTED ok
W issued=100 filled=100 done=0     ← 第 1 窗口填满
D 100                              ← 第 1 窗口 100 个挂起全部完成
W issued=200 filled=100 done=100   ← 第 2 窗口填满，然后永久卡在这里
```

即：窗口能填满、第一批能跑完，**第二批里有一个任务压根没被执行**。

---

### 根因 A：注入队列 push 对 `tail` 无互斥

`src/concurrency/kaula_conc_scheduler.c`（修复前）

```c
int kaula_inject_queue_push(KaulaTask* task) {
    uint64_t tail = atomic_load(&g_inject_queue.tail, relaxed);
    uint64_t head = atomic_load(&g_inject_queue.head, acquire);
    if (tail - head >= CAP) return 0;
    atomic_store(&g_inject_queue.buffer[tail & MASK], task, relaxed);
    atomic_store(&g_inject_queue.tail, tail + 1, release);
    return 1;
}
```

`tail` 是**裸 load + 裸 store**；而同文件 `pop` 对 `head` 用的是正确的 CAS。
两个并发 pusher 读到同一个 `tail`：

```
A 读 tail=T → 写 buffer[T]=taskA
B 读 tail=T → 写 buffer[T]=taskB   ← 覆盖掉 taskA
A 存 tail=T+1
B 存 tail=T+1                       ← 只前进 1
```

`buffer[T]` 只剩 taskB，**taskA 永久丢失**，一次竞态丢一个。

本基准里两个 pusher 是结构性必然的：`main` 填窗口走
`kaula_spawn_to → worker_post(NULL) → inject_push`，IoDriver 定时器到期
走 `resume_task → worker_post → inject_push`（`inject_pop` 取任务时不设
`owner`，所以 owner 恒为 NULL，resume 必然回注入队列），两条路正面撞车。

#### 为什么不是「改成 CAS tail」

最初建议是「push 改 CAS 与 pop 对称」。**这个建议是错的**，原因值得记下来：

`tail` 既是**槽位预留**又是**发布信号** —— push 必须「先写 payload、再推进
tail」，pop 正是靠 tail 的 release 才敢去读 buffer。若用 CAS 先预留：

```
A CAS tail T→T+1 预留槽位 T
   （此时 tail 已是 T+1，pop 看到 head<tail 就会去读 buffer[T]）
A 还没写 payload  ← pop 读到未写入/上一轮清空的槽位，拿到 NULL
```

要修好就得引入 Vyukov 那套**每槽序号**，改结构体、改 memset、改两侧
读写逻辑，改动面远大于收益。这里选择把临界区**整段串行化**（`atomic_flag`
自旋锁），payload/tail 的先后关系与 pop 的契约**一个字都不动**：

```c
static atomic_flag g_inject_push_lock = ATOMIC_FLAG_INIT;
/* 抢锁 → 查满 → 写 payload → release 推 tail → 解锁，单出口 */
```

选 `atomic_flag` 而非 `kaula_p_mutex`：静态初始化即可，不依赖
`kaula_inject_queue_init` 的调用时序；临界区三条指令且不阻塞，
持锁者必然在推进，自旋不会活锁。

---

### 根因 B：WSDeque 的 `bottom` 被非 owner 写（修完 A 之后才暴露）

只修 A 之后：`mixed` 从 0/4 变 6/6，**`suspend` 仍然 0/10** —— 说明还有
第二条路径。它是 `kaula_conc_worker_post` 里的：

```c
if (t_current_worker == w) ok = push_owner(&w->local_queue, task);   /* 非 CAS store bottom */
else                        ok = push_external(&w->local_queue, task);/* CAS bottom       */
```

`push_owner` 与 `pop` 改 `bottom` 用的是**非 CAS 的裸 store**，会把
`push_external` 那次 CAS 的结果直接覆盖回去：

```
owner     读 b=5
external  CAS 5→6 成功，写 buffer[5]=taskE
owner     store bottom=6        ← 把 external 刚占的槽位抹掉
```

要么 `buffer[5]` 被写两次（丢一个 Task），要么 `bottom` 被倒退（已入队的
Task 不可达）。二者都不可自愈。

**判别实验**（先证明再改，不猜）：把 `resume_task` 临时改成强制
`worker_post(NULL, ...)`，即外部线程一律不碰别人的 deque ——

| 配置 | 改前 | 改后 |
|---|---|---|
| suspend conc=100 total=2000 | 0/10 | **10/10** |
| suspend conc=1000 total=2000 | 0/4 | **4/4** |
| mixed conc=100 total=2000 | 0/4 | **4/4** |

确认后把它落成正式修复：Chase-Lev 的原始约束就是 **`bottom` 只有 owner 写**，
所以收紧在 `worker_post` 而不是只堵 `resume_task` 这一个调用点 ——
将来任何外部线程都不该再走 `push_external`。

`push_external` 全仓唯一调用点就是 `worker.c` 这一处（`spawn_to`、
预算重入队、状态机让出这三条都是 `t_current_worker == w` 的 owner 路径），
所以收紧它不会波及别的路径。`test_ws_deque.c` 仍引用该函数，故保留不删，
并在 `kaula_conc_ws_deque.h` 头部补上「不可与 owner push/pop 并发」的契约。

---

### 修复效果与回归

`test/bench` 四条曲线 × conc{100,1000} × 多轮压测：**31/31 通过**，
`errors=0`，checksum 按曲线三边一致（light/suspend/mixed = `30376099840`，
degrade = `3162242304`）。

`python test/concurrency/run_tests.py`：

| 套件 | 结果 |
|---|---|
| completion_stamp / epoch / iodriver / mpsc_queue / platform / task / worker / ws_deque | 全 PASS |
| `test_spawn_completion` | rc=0xC0000005 |

> `test_spawn_completion` 的崩溃**不是本次修复引入的**：把这三个文件
> `git checkout --` 还原到 HEAD 后重跑，同样崩溃。该文件本身是仓库里的
> 在途改动，未做处理。

**改动文件**：`src/concurrency/kaula_conc_scheduler.c`、
`src/concurrency/kaula_conc_worker.c`、`src/concurrency/kaula_conc_ws_deque.h`（仅注释）。

---

### 仍未修：次要缺陷 D2（**已证实不是上面挂死的成因**）

`kaula_conc_iodriver.c` 第 196 / 244 / 259 / 307 / 331 行调用
`kaula_scheduler_resume_task(task)` 时丢弃返回值。该函数在「本地 deque 满
且 注入队列满」时返回 `KAULA_ERR_WOULD_BLOCK`，任务会蒸发；`kaula_spawn_to`
对同一返回值有「原地执行」兜底，`resume` 侧没有。

**但这不是 F1 挂死的成因**，别照着这条去修（我先猜了，被实测否定）：
在 `resume_task` 里插计数探针，conc=10000 total=20000 跑 45 轮，
`RESUME_DROP` 出现 **0 次**，而其中挂死 2 次 —— 两者不相关。
注入队列（8192）在本基准的 resume 时刻从未满过。详见 F11。

仍是应当补的兜底（同属「任务静默丢失」一类），但优先级低于 F11。

---

## F2：Tokio 定时器轮盘 1ms 粒度，200µs 睡眠实测约 2ms

不是基准口径问题，三边都调了 `timeBeginPeriod(1)`、都用各自契约规定的
sleep 原语、同一时钟。实测：

| 原语 | p50 | 超额 |
|---|---|---|
| Go `time.Sleep(200µs)` | 623800 ns | 3.1× |
| `std::thread::sleep(200µs)` | 983500 ns | 4.9× |
| `tokio::time::sleep(200µs)` | **2001100 ns** | 10× |

Tokio 比裸 `std::thread::sleep` 还多一倍，这一层是 Tokio 自己的
（`tokio-1.53.1/src/runtime/time/source.rs`）：

```rust
pub(crate) fn deadline_to_tick(&self, t: Instant) -> u64 {
    // Round up to the end of a ms
    self.instant_to_tick(t + Duration::from_nanos(999_999))
}
pub(crate) fn tick_to_duration(&self, t: u64) -> Duration {
    Duration::from_millis(t)          // 轮盘 tick = 1ms
}
```

轮盘粒度 1ms，再叠 Windows `Sleep` 自身的 1ms 取整。

**对报告的影响**：`suspend` / `mixed` 的差值里含这一项，属可比的真实运行时差异；
但 `suspend` 的绝对值主要反映定时器粒度而非调度质量，**不要用它单独下结论**。

---

## F3：Go vs Rust 同一事务源码，算术差 2.3 倍

| 实现 | `txn_full` | `txn_state_only` |
|---|---|---|
| Go (gc) | 2341.7 ns/次 | 1836.5 ns/次 |
| Rust (LLVM) | 1010.9 ns/次 | 809.7 ns/次 |

checksum 逐位一致但算术慢 2.3 倍，是**真实代码生成差异**（Go 循环体
~60 条指令/迭代、无 IDIV、用魔术数乘法；Rust ~12 cycles/迭代），
非测量 bug。`light` 曲线的 TPS 差值里含这一项，**报告需作为
caveat / 归一化项说明**。

### 附带的 Rust 折叠陷阱

`parse_and_checksum` 是无参数纯函数，LLVM 函数特化推断「返回值是常量」，
把 256 次循环整个搬到编译期 —— 实测不加防护时 20000 次事务耗时 **0 ns**。
`black_box(f())` 挡不住（函数指针去虚化后再折叠），**必须 `black_box(初值)`**。
Go 的 gc 不做这种跨过程常量折叠，无需防护。三边最终都在运行时真跑。

---

## F4：Go 的 `time.Now()` 在 Windows 上不能用来打延迟点

其单调分量读 `KUSER_SHARED_DATA.InterruptTime`（0x7ffe0008），粒度 10~40µs，
连续读 200 万次有 1999922 次同值 —— 会把四条曲线的 p50 全读成 0。

实测钉死后的四条曲线 p50（Go 侧）：light=38200、suspend=532800、
mixed=239500、degrade=2271500（修复前全为 0）。

修复：`platform_windows.go::nowTicks()` 走原始 QPC（10MHz，最小非零间隔 1 tick）。
三边时钟同源：Kaula `kaula_p_now_ns` 走 QPC、Go `nowTicks()` 走 QPC、
Rust `std::time::Instant` 走 QPC（实测最小非零间隔 100ns）。

---

## F5：`TRANSACTION.md` 三处与实现不符（文档滞后，已改）

| 行 | 原文 | 实测结论 |
|---|---|---|
| `:53` | `checksum = (checksum * 31 + b) % P` | 实现是 `((checksum % 1073741824) * 31 + b) % P` |
| `:87` | 「Go 没有用户态定时器，`time.Sleep` 会占住 OS 线程」，规定走 `Gosched` 自旋 | 实测 `time.Sleep(200µs)`×5000 **真挂起**，CPU/wall=7.1%；Go 侧走 `time.Sleep` |
| `:96-101` | 延迟口径「提交 → 拿到结果」 | 由 e55adb9 判定错误，改为内核打点 `submit_ns → complete_ns` |

另：`:61` 「数据依赖链保证编译器不会折叠循环」只对 Go 成立（见 F3 附）。

---

## F6：编排器自身的两个 bug（已修）

- **`subprocess.run(text=True)` 按 locale(GBK) 解码 UTF-8 诊断** → 在子线程抛
  `UnicodeDecodeError`，主流程只剩半截输出。改为
  `encoding="utf-8", errors="replace"`（`run_bench.py` 4 处、`metrics.py` 3 处）。
- **`build_kaula` 只判 `exe.exists()`** → 若上一次的 `bench_gen.exe` 正在运行导致
  本次 kaulac 链接失败，旧文件仍在，会把**上一条曲线的二进制**当成本次结果跑出来。
  实测踩过：suspend 进程挂着，后续 sweep 全部复用它，一度误判成「全线超时」。
  改为编译前 `exe.unlink(missing_ok=True)`。

---

## F7：checksum 累加器三边语义不同 —— Kaula degrade 直接 fatal

**症状**：`kaula/degrade/conc=100` 与 `conc=1000` 都报
`rc=3221226505: KAULA FATAL: integer overflow in addition`（0xC0000095 STATUS_INTEGER_OVERFLOW）。

**根因**：基准自己的累加式写错了，不是运行时的问题。

```c
checksum = (checksum + r) % 9223372036854775807;   /* r ≈ 9.8e18 */
```

`degrade` 的 `r = acc ^ parse`，`acc` 是自旋 20000 轮后的值 ≈ 9.8e18。
实测**第 10 次累加**就越过有符号 64 位：

```
c=8862008500342011753  r=984667611149112417   和 = 9.85e18 > i64max 9.22e18
```

- **Go**：静默回绕（两's complement），算出 `-4466393588697188912`
- **Rust release**：`overflow-checks = false`，同样静默回绕，与 Go 逐位相同
- **Kaula**：乘加是**检查溢出并陷阱**，直接 FATAL 退出

同一行代码三个目标语义不同 → 三边永远拿不到同一个 checksum。

**修复**（已改，三边同步）：改成滚动散列，与事务本体同一种写法

```c
checksum = ((checksum % 1073741824) * 31 + (r % 1073741824)) % 9223372036854775807;
```

最大中间值 `32*(2^30-1) = 34359738336`，远离 `2^63`。

**验证**：n=2000 的期望值，三边逐位一致，`errors=0`：

| 曲线 | 新 checksum |
|---|---|
| light / suspend / mixed | `30376099840` |
| degrade | `3162242304` |

（旧式 light = `26712998144000`，与 Go/Tokio 实测吻合，可反证模型无误。）

**附带影响**：`--full`（`total=20000`）在旧式下必溢出，修完之后累加器上界与
`total` 无关（恒 `< 2^35`），`--full` 不再需要特殊说明。

**教训**：跨语言基准里**任何算术都要先问「三个目标的溢出语义是否一致」**。
Go 和 Rust 的静默回绕会掩盖问题，只有检查溢出的目标会把它炸出来 ——
这类 bug 的正确形态就是「两个目标数字漂亮、第三个目标崩溃」。

---

## F8：`metrics.py` 的 checksum 一致性检查没按曲线分组（假警报）

原实现把所有用例的 checksum 放进一个 `set` 比较：

```python
sums = {r.values["checksum"] for r in ok if "checksum" in r.values}
if len(sums) > 1:  print("!! checksum 不一致 ... 上表不可比")
```

但 light/suspend/mixed 与 degrade 的期望值**本来就不同**（degrade 走
`acc^parse` 路径），混在一起比必然报「不一致」——把「三边全对」误判成
「上表不可比」。已改为按 `curve` 分组比较，逐条打印。

---

## F9：`metrics.py` 对概率性死锁增加重试，并单独报挂死率

F1 让 Kaula 的 `suspend` / `mixed` 在 `total=2000` 下约 50~80% 概率挂死。
不重试则这两列整列为空、无法横向比；只报重试成功的数字又等于把 bug 藏掉。

做法（`--retries`，默认 4，`0` 关闭）：

- **只对「超时强杀」重试**。构建失败 / 止损 / 缺 `RESULT` 行都不重试 ——
  那些重试没用，重试只会把要修的问题藏起来。
- 构建放在重试循环**外**：挂死是运行期的事，重编译不会让它不挂（白花 12s）。
- 报表新增「用例可靠性」一节，逐行打印 `试 N 次，挂死 H 次，最终 ...`，
  并注明「上表该行数字来自成功的那一次，不代表该运行时能稳定跑出这个数」。
- `attempts` / `hangs` 一并写进 JSON。


---

## F10：读大表前必须知道的两条数据质量限制

### （1）每格只有 1 个样本，跨次运行有 ~20% 漂移

矩阵是 `3 运行时 × 4 曲线 × 2 并发度 = 24` 格，**每格只跑一次**（默认快跑）。
同一台机器、同一套参数，先后两次完整矩阵：

| 格 | 第 1 次 | 第 2 次 | 变化 |
|---|---|---|---|
| `kaula/light/100` tps | 187386 | 151772 | −19% |
| `kaula/light/100` p99 | 121.4µs | 610.2µs | **5.0×** |
| `kaula/light/1000` p99 | 125.1µs | 618.3µs | **4.9×** |
| `go/light/100` tps | 552379 | 427250 | −23% |

tps 漂移 ~20%、p99 漂移 ~5×，且方向一致（第 2 次整体更慢）—— 是机器状态的
系统性变化，不是随机噪声。**结论**：

- 表里 tps 的**绝对值不可引用**，只可用于**同一格内横向比三个运行时**
  （三边在同一时刻测，共处同一机器状态，漂移同步抵消）。
- 跨曲线、跨并发度的**比值**（如 suspend/light）比绝对值稳健。
- 想要均值±方差，需要给 `metrics.py` 加重复采样，当前未实现。

#### 复测后的修正：噪声底不是 20%，是 8%~49%

重编编译器后重跑全量矩阵（10-04），**Go 与 Tokio 源码一个字没动**，
却摆动了 −52% ~ +126%：

| 对照组（源码未变） | 前一轮 | 本轮 | 漂移 |
|---|---|---|---|
| `tokio/light/100` | 527106 | 1083188 | **+105%** |
| `tokio/light/1000` | 1019575 | 490773 | **−52%** |
| `tokio/degrade/100` | 22350 | 50413 | **+126%** |
| `go/light/1000` | 501002 | 716229 | +43% |

同一会话内、同一二进制、同一格，隔两分钟复测：`tokio/light/1000`
冒烟 1077237 → 矩阵 490773，**差 2.2 倍**。

`ab_compiler.py` 的交错 A/B（固定二进制，9~21 轮）量出**单个变体自身**
的轮内极差，这才是真正的噪声底：

| 格 | A 自身最快/最慢 |
|---|---|
| `light/100` | **49%** |
| `degrade/1000` | 39% |
| `light/1000` | 35% |
| `suspend/1000` | 22% |
| `mixed/100` | 8% |

**结论修正**：噪声底 **8%~49%**。凡小于 ~50% 的差异 —— 包括本文件此前
所有单样本读数 —— 都落在噪声里，不能当结论。另外矩阵是**按运行时分批**
跑的（kaula 8 条 → go 8 条 → tokio 8 条），不是逐格交错，
这让「运行时」和「时间序」进一步混杂。

要引用任何单格数字，先走 `ab_compiler.py` 的交错测量。

### （2）外部采样的 500ms 级列在快跑模式下取不到样

采样分两级：便宜计数器 50ms，`NtQuerySystemInformation` 500ms。
而**单个用例的墙钟只有 0.45~1.6s（中位 0.54s）**，负载区本身只有 10~40ms ——
500ms 那一级一整轮下来**只有 0~1 个采样点**。

直接证据（同一曲线、只换并发度，数值应同量级）：

| 用例 | ctxsw | threads_end | handles_end |
|---|---|---|---|
| `kaula/light/100` | 1575 | 10 | 2056 |
| `kaula/light/1000` | **0** | **1** | 2056 |
| `kaula/suspend/100` | **0** | **1** | 2058 |
| `kaula/mixed/1000` | **0** | **0** | **0** |
| `go/suspend/100` | **0** | **0** | **0** |

`ctxsw` / `threads_end` / `handles_end` 会随机落在 `0`，纯属采样没赶上，
**不是被测程序真的创建了 0 个线程**。

例外：`page_faults`、`io_read_b` 这类**累计型**计数器即使只有 1 个点也接近真值
（活动集中在进程前段），可作数量级参考 —— 这也是表里 kaula ~7000、go ~1500、
tokio ~600 的 `page_faults` 看起来自洽的原因。

**结论**：`ctxsw*`、`threads_end`、`handles_end`、`io_write_*`、`handle_growth`、
`rss_reclaim` 这几列**在默认快跑下不可用**；`rss_peak_b`（50ms 级，每例 8~10 点）
可用。要读资源列必须用 `--full`（total=20000，负载区拉长一个数量级）。

`sys_over_user` / `tps_per_cpu_s` / `tps_per_conc` 出现 `N/A` 是分母为 0 的
除零保护（`cpu_user_s` 采样到 0），不是缺失。

---

## F11：conc=10000 下仍有 ~2.6% 概率死锁，成因未定（已排除 4 条路径）

**严重度**：中高 —— 不影响 `metrics.py` 默认快跑，但 `run_bench.py` 的
默认参数正是这个并发度。

**复现**

| 并发度 | 轮数 | 挂死 |
|---|---|---|
| `conc=1000`（`metrics.py` 默认） | 57 | **0** |
| `conc=10000`（`run_bench.py` 默认） | 117 | **3（2.6%）** |

具体：`suspend` conc=10000 total=20000 三批分别 3/30、0/45 中 2 次挂、
首批 4 曲线×3 次 0 次；`mixed` 同参数 0/30。另有 1 次 `run_bench.py`
全量跑 900s 未完成（未捕获到具体用例，后续两次全量跑 16/16 正常）。

挂死形态与 F1 一致：进程不退出、CPU 冻结、主线程卡在 `await`。

**已排除的路径（每条都有实测证据，避免重复踩）**

| 假设 | 判定 | 证据 |
|---|---|---|
| 注入队列 push 丢任务（根因 A） | 已修 | 修复前 suspend 0/10 → 修复后 31/31 |
| WSDeque `bottom` 被非 owner 写（根因 B） | 已修 | 判别实验 0/10 → 10/10 |
| D2：resume 返回值被丢 | **否** | 探针 45 轮 `RESUME_DROP`=0，挂死 2 次 |
| worker park 退避导致漏唤醒永久化 | **否** | `worker.c:176` 是硬编码 `1000000ULL`(1ms)，无退避 |
| `spawn` 撞 `max_tasks` 返回空句柄 | **否** | `kaula_conc_task.h:23` = 1,000,000，远大于 10000 |
| SUSPEND/BLOCKED 投递失败被丢 | **否** | `worker.c:318-322`/`335-339` 有 `break→重跑` 兜底；且那样会自旋烧 CPU，与实测 CPU 冻结不符 |

**为什么用插桩定位不了**

`make_trace.py` 的逐窗口 `file_flush` 会改变时序：conc=10000 下跑 30 轮
**0 次复现**（未插桩时 117 轮复现 3 次）。和 F1 时一样，
插桩本身就是缓解手段 —— 需要的是**不进被测主循环**的外部状态转储。

**建议的下一步**（未做）

挂死时从外部 dump `active_task_count`、注入队列 head/tail、各 worker 的
`current_task`。可行做法是给 bench 模板加一个独立的 watchdog 线程，
在 `await` 超过阈值时把上述计数 `file_printf` 落盘 ——
只在**已经挂住**时才写，正常路径零开销，不影响时序。

**已做的缓解**：`run_bench.py` 的 `run_case` 原本**完全没有超时**
（那个 `timeout=5` 只作用于 PowerShell 采样子进程），死锁会让整个工具
挂死且一行输出都没有。已加 `--case-timeout`（默认 300s）：超时强杀并
记为该用例失败，`rc=1`。`metrics.py` 早有 `--timeout` + `--retries`。

## F12：那 10 个提交到底有没有提速 —— 交错 A/B 实测

### 问什么

F10 量出噪声底 8%~49%，意味着任何单样本对比都不可用。本节回答一个具体问题：
**`8b5837e` → `f6ee26f` 这 10 个提交，Kaula 到底变快还是变慢？**

### 怎么测

`test/bench/ab_compiler.py`：

1. 两个**干净 worktree**（A=`8b5837e`，B=`f6ee26f`），各自 `toolkit_build.py`
   构建自己的 kaulac，**各自编译自己的 `src/`** —— 运行时 C 源码是被测对象，
   不能拿 B 的头文件去编 A 的编译器。实测两边注入队列容量确实不同
   （8192 → 131072）。
2. **同一份** bench 源码（从当前工作区模板渲染）喂给两个编译器。
3. 运行阶段**交错** ABBA / BAAB 交替，每对相邻。
4. 报「每轮比值的中位数」而非「两个中位数相除」—— 前者对单轮离群值免疫。
5. 两个变体的 checksum 必须一致，否则该格作废。

参照系确认过：`8b5837e`（10-03 21:53，F1 修复落地的那个提交）是 `f6ee26f`
的祖先，且 `git log 8b5837e ^f6ee26f` 为空 —— 上一轮基准跑的就是 A。

### 结果（reps=9，全 8 格）

| curve/conc | A tps | B tps | B/A | A p99 | B p99 | p99 B/A | A 轮内极差 |
|---|---:|---:|---:|---:|---:|---:|---:|
| light/100 | 179486 | 157802 | −12.1% | 112.1µs | 217.3µs | **+93.8%** | 24% |
| light/1000 | 171896 | 158260 | −7.9% | 211.1µs | 298.8µs | +41.5% | 22% |
| suspend/100 | 123681 | 130131 | +5.2% | 627.4µs | 643.8µs | +2.6% | 18% |
| suspend/1000 | 189901 | 215440 | +13.4% | 1155.4µs | 440.2µs | **−61.9%** | 22% |
| mixed/100 | 126547 | 120409 | −4.9% | 784.8µs | 738.0µs | −6.0% | 8% |
| mixed/1000 | 189665 | 185310 | −2.3% | 2279.2µs | 906.2µs | **−60.2%** | 9% |
| degrade/100 | 72441 | 75354 | +4.0% | 1522.2µs | 1296.0µs | −14.9% | 16% |
| degrade/1000 | 116177 | 137406 | +18.3% | 5302.9µs | 2817.5µs | **−46.9%** | 39% |

### 怎么读

**tps 一列全部落在噪声里。** 每格的 |B/A| 是 2%~18%，而 A 自身轮内极差
是 8%~39% —— 没有一格的差异站得住。这直接否掉了「这批提交让 Kaula 更快了」
这个说法：**在 tps 上无法证实，也无法证伪。**

**p99 上有两个方向相反、且重复出现的信号**（`light` 加跑到 21 轮复现）：

| | reps=9 | reps=21 |
|---|---|---|
| `light/100` p99 B/A | +93.8% | +63.7% |
| `light/1000` p99 B/A | +41.5% | **+253.2%** |

- **回退**：`light`（纯计算曲线，最直接反映 codegen/运行时快路径）的尾延迟
  一致变差，4 次测量 B 全部高于 A。tps 没动，**尾变长了**。
- **改善**：`conc=1000` 且带挂起/降级的那三格（suspend/mixed/degrade），
  p99 改善 −47%~−62%，且这三格的 A 轮内极差只有 9%~22%（相对可信）。

两个信号方向相反，但都不该当成定论 —— p99 那一列我没量 B 自身的轮内极差，
量级判断比 tps 弱一档。要坐实需要给 `ab_compiler.py` 也补 p99 的极差列，
并把 `light` 的回退定位到具体提交（`f02225f` 挂起点提升？`3b1482f` epoch
改本地计数？`f6ee26f` Frame 瘦身？三者都动过 conc 快路径）。

### 顺带确立的方法论

- **同二进制 + 同参数 + 同格 + 交错多轮**，是当前唯一能读出 <50% 差异的手段。
- 单样本矩阵（`metrics.py` 快跑）**只能用于差距 ≥2× 的排序判断**，
  且因为按运行时分批，跨运行时的比较一律无效。
- 报告里凡出现百分比，必须附「该格噪声底」；没有这一列的百分比应当删掉。


## F13：kl 双形态 spawn 在 Windows 上确定性堆损坏（0xC0000379），与 9160 行改动无关

**复现**：spawn 一个含挂起点（sleep 定时器）的 conc fn，await 取结果。任意规模都崩，单任务必现。

`
conc fn slow_i(i64 x) -> i64 {
    std.time.time_sleep(1)   // 挂起点
    return x + 1
}
fn main() -> i32 {
    Handle h = spawn slow_i(41)
    i64 r = h.await
    return 0
}
`

**证据链**：
- HEAD（0f0ca87，无任何未提交改动）编译同一用例同样崩 → 与本次 9160 行改动无关（git stash 对照）。
- 顺序调用（coercion shim → kaula_iodriver_blocking_wait）正常，rc=0 输出正确 → 崩在**并发投递路径**（Worker → SUSPEND → IoDriver 定时器 → resume_task → Worker）。
- C 层并发测试（epoch/mpsc/ws_deque/iodriver）全部通过，但**没有任一 C 测试覆盖 KAULA_IO_SLEEP 定时器路径** → 这条路径只有 kl 用例在走。
- ASan 版时过时崩（0xC0000409 fastfail）→ 疑似竞态型 UAF；普通版每次必崩。
- Linux 上同批 kl 用例全过（22 用例回归）→ 平台相关。

**已排查**：注入队列（F1 修复在位）、MPSC submit 队列、io_timer_add/fire 内存管理、Worker SUSPEND 提交、resume_task、KMM alloc_global（单形态同路径通过）、KaulaResumeResult/io_request ABI（同头同 clang）。

**未定**：Windows 上 Worker/Driver/await 三线程的单任务竞态窗口（ASan 时崩时不崩提示有未初始化/提前回收路径）。需要 PageHeap 或竞态插桩进一步定位。**当前 kl 并发回归在 Windows 上的有效范围仅限单形态用例**。
