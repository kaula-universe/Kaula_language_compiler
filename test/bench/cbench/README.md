# C 边基准 —— 直调并发内核 API

`main.c` 是**不经过编译器 lowering**、直接调用 CONC 内核公开 API 的基准驱动。

## 它在四边里回答什么问题

`test/bench/` 下的四边各自回答一个问题：

| 边 | 路径 | 回答 |
|---|---|---|
| `kaula/` | `conc fn` + `spawn` + `await`，经 kaulac 编译 | **用户最终写出来的样子**，编译器优化后的体验 |
| **`cbench/`** | 直调 `kaula_spawn` / `kaula_handle_await_into_timed` | **内核 API 的开销下限**：没有 lowering 时，同一个事务要花多少 |
| `go/` | `go` + `WaitGroup`（GMP） | 主流竞品对照 |
| `tokio/` | `async`/`await`（M:N Future） | 另一主流竞品对照 |

**C 边 vs Kaula 边之差 = 编译器的语法代价（lowering 开销）。**
两边跑同一个 `parse_and_checksum()`、同一组常量、同样的窗口式提交/取回，
唯一差别是「Frame 由谁生成、参数怎么传」：

- Kaula：编译器生成 `K_xxx_Frame` 结构体 + `K_xxx_resume` 状态机 + coercion shim
- C：人手写 `BenchFrame` + `resume()`，语义逐条等价

## 构建

```bash
# 需要先构建内核静态库：
python toolkit_build.py --target runtime

clang -O2 -std=gnu11 -D_POSIX_C_SOURCE=200809L \
  -I build/include -o bench_c main.c \
  build/lib/libkaula_runtime.a -lpthread -lm
```

- `-std=gnu11` + `_POSIX_C_SOURCE=200809L`：内核 `kaula_conc_platform.h` 用了
  `clock_gettime` / `nanosleep` / `pthread_condattr_setclock`，严格 `c11` 下这些不声明。
- 只用 `libkaula_runtime.a`（内核），**不链 `libkaula_std.a`** —— C 边要的是
  内核 API 本身，不需要 Kaula 标准库。

或者直接由编排器构建：`python test/bench/run_bench.py --runtimes c,go`。

## 调用序列

```
kaula_runtime_init(&cfg)                    // cfg.worker_count = 0 → 自动 CPU 数
  ├─ 窗口填满 conc 个：
  │    frame->pc = 0; frame->... = 参数
  │    kaula_spawn(resume_for(curve), frame, sizeof(BenchFrame))   → KaulaHandle
  └─ 逐个取回：
       kaula_handle_await_into_timed(h, &v, sizeof(v), &lat_ns)    // 结果 + 内核打点延迟
       kaula_handle_error(h)                                       // 错误码
       kaula_handle_release(h)                                     // 归还 epoch pin
kaula_runtime_shutdown()
```

### 关键坑（已踩）

**结果必须经 `KaulaResumeResult.value` 返回。**
内核在 Worker 收到 `KAULA_RESUME_DONE` 后执行
`kaula_task_complete(task, result.value)`，`await_into` 拷的是这个 `value`
指向的内存。早期版本只写 `frame->result` 而不设 `value`，症状是
**「跑得飞快但结果全 0」**（`errors` 等于总事务数、`checksum=0`）——
不崩、不报错，正是 `kaula_conc_task.h` 里反复警告的那类静默错误。

**`spawn` 满载会返回空句柄。** 内核在达到硬上限时按 I3（一切有界）返回
`KAULA_ERR_WOULD_BLOCK` 的空句柄，而不是丢弃。基准必须用
`kaula_handle_is_null()` 判定并计入 `errors`，否则下面的 `await` 会等一个
空句柄。

## 与 Kaula lowering 的逐条对照

| 环节 | Kaula（`conc fn` 经 kaulac） | C（本目录） |
|---|---|---|
| Frame | 编译器生成 `K_xxx_Frame`，含 `pc` + 跨挂起点存活变量 | 手写 `BenchFrame`，含 `pc` + `result` + 参数 |
| 首跑 | 有守卫时走 `enter_fn`（无 switch 直通） | 直接走 `resume`，首跑过 `switch(pc)` 的 `case 0` |
| 挂起 | `std.time.time_sleep_us` 被改写成 `KAULA_IO_SLEEP` 请求 | 手写 `KAULA_RESUME_SUSPEND` + `KAULA_IO_SLEEP` |
| 结果 | `f->result`，经 coercion shim 转出 | `f->result`，`value = &f->result` |
| 纯计算 | **完全内联**为直接函数调用，**不进内核**（`inlined=1`） | 仍走 `kaula_spawn`（`inlined=0`） |

最后一行是两边最大的口径差异，也是基准要量化的核心：**Kaula 把无挂起点的
`conc fn` 优化掉了整个调度路径**，用户拿到的是零开销；代价是内核看不到这个
任务的「服务时间」，延迟列随之全 0。详见 `../BENCH_C_VS_KAULA_VS_GO.md`。

## 输出

固定格式，与 `kaula` / `go` / `tokio` 三边键集一致（`run_bench.py` /
`metrics.py` 按 `key=value` 解析）：

```
PHASE load_start
PHASE load_end
PHASE settle
RESULT curve=<c> tps=<n> elapsed_ns=<n> p50=<n> p95=<n> p99=<n> p999=<n> \
       max=<n> n=<n> win=<n> errors=<n> checksum=<n> inlined=0 spawn_failed=<n>
```

三行 `PHASE` 供外部采样器对齐「负载区」与「静置区」。
`inlined=0` 是 C 边的固定标记（见上）；`spawn_failed` 是满载背压命中次数，
正常应为 0。
