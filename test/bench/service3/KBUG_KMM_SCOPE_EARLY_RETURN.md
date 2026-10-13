# kaulac：`conc fn` 内 KMM scope 的提前 return 不弹栈（连接潮汐关闭 → 服务 abort）

> **未修复**——本文件是调查记录与修复建议。发现过程：`service3` 基准
> 定速 6 万请求/秒压测（`--rate 60000`，4096 连接）。
>
> **严重度**：高（可用性）。任何「KMM 托管函数内提前 return」的执行
> 都会在**所在线程的 TLS 作用域栈上永久泄漏一帧**（16 B）。
> 单线程累计 1024 次即 `KMM FATAL ... aborting`。
> 对「每连接一个 conc fn」的服务形态，**连接 churn 就是 DoS**：
> 客户端反复建连/断连，约 `1024 × worker 数` 次后服务崩溃。

## 1. 症状

定速 6 万请求/秒、4096 长连接、`SVC_WORKERS=4` 压测 `kaula_kl/order_server`：

- 压测全程（15s）四窗 TPS 全部 ~60,000，`mismatch=0`——**计算正确**；
- 压测结束、客户端 4096 连接潮汐关闭时，服务进程 `SIGABRT`（rc=-6）：

```
KMM FATAL: Scope stack overflow (depth 1024 >= max 1024), aborting
  innermost managed scope: <unnamed> (legacy KMM_V4_SCOPE_START)
```

- 时间线实验（`/tmp/timeline_test.py`，0.25s 粒度探测）：

| 时刻 | 事件 |
|---|---|
| t=0~18.0s | 压测中，服务存活，TPS 60k/窗 |
| t=18.31s | 客户端取满 15s+3s 预热后开始关连接 → **服务 abort** |
| t=18.31s | 压测器退出（2049 个连接级 errors：EOF 348 + RST 其余） |

- 崩溃阈值与连接数精确相关：4096 连接 ÷ 4 worker = **每线程 1024 次关闭**，
  恰等于 `KMM_V4_MAX_SCOPE_DEPTH`。1024 连接（每线程 256 次关闭）
  复跑 20s：`errors=0`，服务存活——**阈值模型证实**。

## 2. 根因（生成代码）

`KAULAC_DUMP=1 kaulac ...` 导出生成 C（`/tmp/klc_final.c`）。
`conn_task` 的连接体被 lowering 进 `KMM_V4_SCOPE_START { ... }`
（宏展开为 `kmm_v4_scope_push(); do { ... } while(0); kmm_v4_scope_pop();`，
见 `src/kmm_scoped_allocator_v4.h:1807-1820`）。

**挂起 return 与正常退出都配平，唯独提前 return 漏弹栈**：

```c
KMM_V4_SCOPE_START {                          // ← push（klc_final.c:232）
    ...
    if (f->sub[1] == 1) {
        ... 提交 READ ...
        kmm_v4_scope_pop();                   // ← 挂起前显式弹栈 ✓
        return (KaulaResumeResult){ .status = KAULA_RESUME_SUSPEND, ... };
    }
    if (f->sub[1] == 3) {
        f->sub[1] = 4;
if (got <= 0) {
            kaula_close(fd);
            f->result = 0;
            f->pc = -1;
return (KaulaResumeResult){ .status = KAULA_RESUME_DONE, ... };  // ← 漏弹栈 ✗
        }
    }
    ...
} KMM_V4_SCOPE_END                           // ← 正常路径经宏弹栈 ✓
```

`got <= 0` 即客户端关连接（EOF），`conn_task` 从此路径退出——
**每一次连接关闭泄漏一帧**。作用域栈是 per-thread TLS 定长数组
（`frames[1024]`，`kmm_scoped_allocator_v4.h:613`），只增不减，
1024 次后 `scope_push` 命中上限直接 `abort()`（同文件 `:1736`）。

同类漏弹路径还包括：作用域内任何提前 `return`（错误分支等）。
`break` 路径无此问题——`break` 跳出 `do{}while(0)` 后仍会执行到
`while(0); kmm_v4_scope_pop();` 的弹栈语句。

## 3. 影响面

| 形态 | 后果 |
|---|---|
| 每连接一个 `conc fn` + KMM 分配 + 错误/EOF 提前 return | **连接 churn 打满 1024×worker 后服务崩溃** |
| 请求循环内提前 return（如参数校验失败） | 每请求泄漏一帧，高 QPS 下数秒即崩溃 |
| 纯计算 conc fn（无 KMM 分配、无提前 return） | 不受影响 |

本基准的 `order_server.kl` 两条全中：`conn_task` 顶部两次
`kmm_v4_alloc`（KMM 托管），且 `got<=0` 提前 return。

## 4. 复现

```bash
python toolkit_build.py --release
cd test/bench/service3
kaulac kaula_kl/order_server.kl -cflags "kaula_kl/svc_wrap.c -I<repo>/src/concurrency -I<repo>/build/include"
SVC_WORKERS=4 ./kaula_kl/order_server &
# 反复建连/发一个请求/关连接，累计 > 4096 次（4 worker）：
python3 - <<'EOF'
import socket
for i in range(5000):
    s = socket.create_connection(("127.0.0.1", 8123))
    s.sendall(b"O,1,itemA,3,1990\n"); s.recv(64); s.close()
EOF
# 服务日志：KMM FATAL: Scope stack overflow (depth 1024 >= max 1024), aborting
```

（压测器口径：`loadgen 127.0.0.1:8123 4096 <sec> 5 60000`，
跑完看服务进程是否 `SIGABRT`。）

## 5. 规避（本基准采用）

连接数 ≤ 1024（4 worker 时每线程关闭 ≤ 256 次），保证潮汐关闭
不触发上限。**这是规避不是修复**——长周期运行的服务的关闭次数
会累计超限，仍需真修复。

## 6. 修复建议（未实施，待独立轮次）

codegen 侧（`compiler/internal/codegen/`，conc fn lowering）：

1. **提前 return 配平**：lowering `return` 时若函数体内（或当前
   控制流层级）存在未闭合的 `KMM_V4_SCOPE_START`，在 return 前
   按嵌套层数 emit 对应次数的 `kmm_v4_scope_pop()`。挂起 return
   已有先例（`klc_final.c:246`），提前 return 走同一条路径即可。
2. **防御性**：`KAULA_RESUME_DONE` 返回前由 runtime 校验/恢复
   scope 深度（任务激活时记录进入深度，任何出口恢复到该深度），
   把正确性从「codegen 处处小心」收敛到「runtime 单点兜底」。

> 备注：`-DKMM_V4_MAX_SCOPE_DEPTH=N` 只是抬高上限，**不修复泄漏**，
> 不推荐作为修复手段；`#[no_kmm]` 可绕过托管但改变内存语义。
