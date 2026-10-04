# 事务定义（三运行时必须逐位一致）

公平性的全部前提：三个运行时跑的是**同一个函数、同一组常量、同样多的循环**。
任何一处不一致，测出来的差值里就混进了「工作量不同」这一项。

## 负载：内联 LCG，不走内存

负载由一个固定参数的 LCG 在事务内**直接算出来**，不读文件、不走数组。

```
STEPS = 256
SEED  = 2545
MULT  = 1103515245
ADD   = 12345
SHIFT = 1048576            # 2^20
P     = 9223372036854775807   # 2^63 - 1

state = SEED
for k in 0..STEPS:
    state = ((state % SHIFT) * MULT + ADD) % P
    b[k]  = state % 256
```

**为什么先 `% SHIFT` 再乘**：`state < P ≈ 9.2e18`，直接乘 `MULT ≈ 2^30`
会溢出有符号 64 位。先压到 2^20 再乘，积小于 2^51，三边都用普通
`i64` 运算即可，**不需要任何环绕/无符号扩展原语**。

这一点是刻意的：一旦需要 64 位环绕乘，三个语言要用的原语各不相同
（Go 是 `uint64`，Rust 要 `wrapping_mul`，Kaula 尚无可用原语），
差异会混进被测量的东西。

这样做的理由，逐条都是被坑过的：

- **不用文件**：三个语言的文件 API 与编码处理各不相同，一处 BOM 或换行差异
  就足以让「同一份负载」变成三份不同的字节。
- **不用数组/指针**：Kaula 是定长聚合数组 `i64 buf[4]`，把它传进 `conc fn`
  需要额外的指针或全局机制，而 Go/Tokio 是切片。三边写法差异会引入
  「谁传参更快」这一项，那不是内核差异。
- **LCG 而不是语言自带的随机源**：`random` / `rand` 的算法实现可能随版本变，
  用它们等于把「随机数实现」混进被测量的东西。LCG 的 5 行完全确定。

代价要写清楚：**这条曲线测的是计算 + 调度，不是内存带宽**。
负载在寄存器里生成，不产生访存。要测访存特性得另做一组。

## 事务：parse_and_checksum() -> i64

```
checksum = 0
state = SEED
for k in 0..STEPS:                              # 生成 256 个值
    state = ((state % SHIFT) * MULT + ADD) % P
    b = state % 256
    checksum = ((checksum % 1073741824) * 31 + b) % P   # 滚动散列
return checksum
```

> **本文档早先写的是 `checksum = (checksum * 31 + b) % P`，与三边实现不符。**
> 真实实现是 `((checksum % 1073741824) * 31 + b) % P`：先把散列压到 2^30
> 再乘 31，中间值 < 2^35。不压的话 `9.2e18 * 31` 会溢出有符号 64 位，
> 三边的溢出语义又各不相同（Go 静默回绕 / Rust release 回绕 / Kaula 检查
> 并陷阱），差异会直接变成「checksum 不一致」。**以代码为准。**

选它的理由：

- **无分配**：结果是一个整数。绕开三个运行时分配器能力的差异 ——
  否则测出来的是 allocator 差异，不是内核差异。
- **有真实数据依赖**：`* MULT`、`* 31` 的依赖链保证顺序性，
  不会向量化。
- **工作量恒定**：256 步，与并发度无关，所以 TPS 差异只能来自调度与挂起。
- **无分支**：避免分支预测器命中率成为噪声来源。

### 但「编译器不会把循环折叠成常量」这一条只对 Go 成立

原假设是数据依赖链足以阻止常量折叠。实测证伪：

- **Rust / LLVM**：`parse_and_checksum` 是无参数纯函数，函数特化会推断出
  「返回值是常量」，把 256 次循环整个搬到编译期。实测 20000 次事务
  耗时 **0 ns** —— 等于什么都没测。`black_box(f())` 也挡不住（函数指针
  会被去虚化后再折叠），**必须 `black_box(初值)`**，把入口值声明为编译期
  不可知。
- **Go / gc**：不做这种跨过程常量折叠，实测 2341.7 ns/次是真实执行，
  无需防护。

三边最终都在运行时真跑 256 步，checksum 逐位一致（期望值
`13356499072`）。Rust 侧的 `black_box` 只作用于入口值，返回值不变，
语义上不是差异项；但它也是下面那条代码生成差异的来源之一：

> **caveat**：同一事务源码，Go (gc) 实测 2341.7 ns/次，Rust (LLVM)
> 1010.9 ns/次（`txn_state_only` 1836.5 vs 809.7）。checksum 逐位一致
> 但算术慢 2.3 倍，是真实代码生成差异（Go 循环体 ~60 条指令/迭代、无
> IDIV、用魔术数乘法；Rust ~12 cycles/迭代），**不是测量 bug**。
> `light` 曲线的 TPS 差值里含这一项，报告需作为 caveat / 归一化项说明。

## 四个曲线

| 曲线 | 每任务做什么 | 验证什么 |
|---|---|---|
| `light` | 一次 `parse_and_checksum` | conc fn 零开销、调度器 overhead |
| `suspend` | 一次挂起（200µs）+ 一次计算 | Frame、IoDriver、waker 路径 |
| `mixed` | 50% 带挂起 + 50% 不带 | 预算、停滞判定、阻塞车道互不干扰 |
| `degrade` | 按比例混入长忙循环（不是睡） | 混载加重时是否雪崩、是否饿死 |

`degrade` 的长任务必须是**忙循环**而不是睡眠：睡会走定时器，那是 `suspend`
那条路。这里要的是「占住 Worker 的计算」，正是预算与停滞判定该管的东西。

## suspend 原语的可比性（重要）

`parse_and_checksum` 三边逐位一致，**但 suspend 做不到完全一致**，
因为三个运行时的定时器语义不同：

| 运行时 | suspend 原语 | 语义 | 占住执行资源？ |
|---|---|---|---|
| Kaula | `time_sleep_us(200)` | IoDriver 定时器堆，挂起后 Worker 立即转做别的事 | 否 |
| Go | `time.Sleep(200µs)` | 运行时定时器，OS 线程挂起等待 | 否 |
| Tokio | `tokio::time::sleep(200µs)` | 定时器轮盘唤醒，协作式让出 | 否 |

> **本文档早先写的是「Go 没有用户态定时器，`time.Sleep` 会占住一个 OS 线程」，
> 并据此规定 Go 走 `Gosched` 自旋到 deadline。该前提已被实测证伪。**
> 实测 `time.Sleep(200µs)`×5000 真挂起：**CPU/wall = 7.1%**，不是自旋。
> 自旋版反而会把 Go 这一档凭空变慢，结论偏向 Kaula。**以代码为准** ——
> Go 侧走 `time.Sleep`。

### 三边 sleep 的实际超额（同一 `timeBeginPeriod(1)` 条件下实测）

| 原语 | p50 | 超额 |
|---|---|---|
| Go `time.Sleep(200µs)` | 623800 ns | 3.1× |
| `std::thread::sleep(200µs)` | 983500 ns | 4.9× |
| `tokio::time::sleep(200µs)` | **2001100 ns** | 10× |

Tokio 比裸 `std::thread::sleep` 还多一倍，这一层是 Tokio 自己的：
`src/runtime/time/source.rs` 的 `deadline_to_tick` 注释直接写着
**"Round up to the end of a ms"**，`tick_to_duration` 也是
`Duration::from_millis` —— 轮盘粒度 1 ms，再叠 Windows `Sleep` 自身的
1 ms 取整。

这是**运行时属性，不是基准口径问题**：三边都调了 `timeBeginPeriod(1)`
（Kaula 在 792960d，Go/Tokio 各自补调），用的都是各自契约规定的 sleep
原语，同一请求时长、同一时钟。`suspend` / `mixed` 两条曲线的差值里
含这一项，属可比的真实差异。

**代价**：`suspend` 的绝对值主要反映定时器粒度而非调度质量，
不要用它单独下结论；看三者相对形态即可。

## 计时口径

- **单事务延迟**：内核打点 `submit_ns → complete_ns`，即服务时间，
  含任务创建/入队/排队/执行，**不含调用方顺序取回**。
  见 `kaula_conc_task.c`：`submit_ns` 在 `kaula_task_init` 打（进队列之前），
  `complete_ns` 在 `kaula_task_complete` 打（置 DONE 之前）。
- **为什么不是「提交 → 拿到结果」**：并发窗口里按调用方视角量到的是
  排队 + 顺序取回，与服务时间能差两个数量级 —— 实测 p50 = 3.15 s 而整轮
  只用 26 ms（p50 大于总耗时）。该口径由 e55adb9 判定为错误。
- **三边复刻方式**：
  - Kaula：内核打点，`kaula_conc_last_latency_ns()` 取「上一次 await 的服务时间」。
  - Go：`nowTicks()` 取在 `go` 语句之前，任务体算完那一刻取差。
  - Tokio：`Instant::now()` 取在 `tokio::spawn` 之前，任务体 `.await`
    完成那一刻取差。
- **计时源必须单调且三边同源**：Kaula `kaula_p_now_ns` 走 QPC
  （`kaula_conc_platform.h:121-139`）；Go 走原始 QPC（`nowTicks()`）；
  Tokio 用 `std::time::Instant`（Windows 上走 QPC）。三者 100 ns 粒度。
  **Go 的 `time.Now()` 在 Windows 上不可用于延迟打点** —— 其单调分量读
  `KUSER_SHARED_DATA.InterruptTime`，粒度 10~40 µs，连续读 200 万次有
  1999922 次同值，会把四条曲线的 p50 全读成 0。
- **CPU / 内存**：由 orchestrator 从**外部**采样，不用各自语言的 API。
  三个运行时能报告的指标口径不同，外部采样反而更可比。
- **结果校验**：每次运行累加所有返回值，任何一个运行时算错都会
  在 checksum 上暴露，避免「跑得飞快但算错了」。同时自报 `errors`
  （与该曲线 `expected` 不符的事务数），期望值取 `run_curve(curve, false)`。

## 输出契约（三边键集必须一致）

被测程序输出一行 `RESULT` 与三行 `PHASE`：

```
RESULT curve=... tps=... elapsed_ns=... p50=... p95=... p99=... p999=... \
       max=... n=... win=... errors=... checksum=...
PHASE load_start     # 计时开始前
PHASE load_end       # 计时结束后
PHASE settle         # 静置 400ms 后（RSS 回落测量点）
```

缺一个键 `metrics.py` 就要为该运行时单开一列 `N/A`。分位下标统一
`(n*pct)/100`，`p999` 用 `/1000`，均夹到 `n-1`。

### checksum 累加器（三边同式，且必须不溢出）

```
checksum = ((checksum % 1073741824) * 31 + (r % 1073741824)) % P
```

> **本文档早先写的是「累加所有返回值的低位」，实现是
> `checksum = (checksum + r) % P` —— 那个式子在 `degrade` 上必溢出。**
> `r = acc ^ parse ≈ 9.8e18`，第 10 次累加就越过有符号 64 位。Go/Rust
> 静默回绕（同一值 `-4466393588697188912`），Kaula 乘加检查溢出并陷阱
> → `KAULA FATAL: integer overflow in addition`，三边永远对不齐。
> 已改为上面的滚动散列（与事务本体同写法），最大中间值 `32*(2^30-1)`
> 远离 `2^63`。**以代码为准。**

n=2000 的期望值（三边逐位一致，`errors=0`）：

| 曲线 | checksum |
|---|---|
| light / suspend / mixed | `30376099840` |
| degrade | `3162242304` |

单次事务的期望值仍是 `13356499072`（不变，三边预热期各自校验）。

**通用教训**：跨语言基准里任何算术都要先问「三个目标的溢出语义是否一致」。
Go 与 Rust 的静默回绕会掩盖问题，只有检查溢出的目标会把它炸出来。
