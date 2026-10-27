# kaulac：`conc fn` 内 KMM scope 的提前 return 不弹栈（连接潮汐关闭 → 服务 abort）

> **已修复**（commit `5ad6b08`，2025 轮次）。
> 本文件保留完整调查记录；修复实现与验证证据见文末 **§7 修复落地**。
>
> 发现过程：`service3` 基准定速 6 万请求/秒压测（`--rate 60000`，4096 连接）。
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

## 6. 修复建议（已在 §7 实施）

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

---

## 7. 修复落地（commit `5ad6b08`）

建议 1、2 **两条都实现了**（双保险），并按「对 bug 零容忍」补了
两层回归测试。

### 7.1 codegen 侧（真修复）

`compiler/internal/codegen/stmtgen.go`：

- `generateConcReturn` 原来只发 `f->result = X; f->pc = -1; return DONE;`，
  现在在 DONE 出口**之前**插入弹栈代码；
- 新增 `concKMMExitPopLines()`：按 `cg.kmmScopeDepth`（**与挂起路径
  `conc_io_rewrite.go` 完全相同口径**）emit 对应层数 `kmm_v4_scope_pop()`；
  mark 模式（`cg.kmmUseMark`）改发 `kmm_v4_mark_restore(cg.kmmFnMark)`；
- **guard slot 是纯 C 花括号、不进作用域栈，不计入 pop 层数**。
  （早前误把 `concGuardDepth` 也算进去会多弹一层 → 作用域栈 underflow，
  同样是 abort，已回退。）

生成 C 比对（`KAULAC_DUMP=1`，`test_kmm_scope_early_return.kl`）：
提前 return 的分支现在与 SUSPEND 分支一样，DONE 前都有
`kmm_v4_scope_pop();`（`klc_final.c` `i >= stop - 1` 分支）。

### 7.2 runtime 侧（单点兜底）

`src/concurrency/kaula_conc_worker.c` + `kaula_conc_task.[ch]` +
`kmm_scoped_allocator_v4.h`：

- task 字段新增 `kmm_entry_depth`（UINT32_MAX = 未记录）；
- 首次进入任务体**之前**记录基线 = 当前 TLS 作用域栈深度；
- 任何离开任务体的出口（`KAULA_RESUME_DONE` / `SUSPEND` / `BLOCKED`）
  调用 `kmm_v4_scope_depth_restore(baseline)` 把深度拉回基线；
- `kmm_v4_scope_depth_restore` 语义是「仅在更深时裁剪」，因此对
  本来配平正确的路径是**零开销空操作**。

效果：正确性从「codegen 处处小心」收敛到「runtime 单点兜底」。
即使将来再出现某条漏弹路径，也退化为「runtime 静默修正」而非 abort。

### 7.3 验证证据

| 验证 | 方法 | 结果 |
|---|---|---|
| 复现（无修复） | `test_kmm_scope_early_return.kl`，双修复禁用 | `rc=134` + `KMM FATAL: Scope stack overflow (depth 1024 >= max 1024), aborting` ✅ 与本文档 §1 症状逐字一致 |
| 修复生效 | 同一用例，双修复启用 | `rc=0`，`kmm_scope_early_return ok n=4096 err=0` ✅ |
| Go 单测 | `compiler/internal/codegen/conc_kmm_exit_pop_test.go`（断言 DONE 出口有弹栈、深度口径与挂起路径一致） | 通过；禁用修复则该测试失败 ✅ |
| 全量 Go 测试 | `cd compiler && go test ./...` | 全通过 ✅ |
| 并发测试套件 | `test/concurrency/run_tests.py` 全量 | 除 3 项 **pre-existing** 失败（`test_task` / `test_ws_deque` / `test_null_handle_probe`，基线 HEAD 同样失败，与本修复无关）外全通过 ✅ |
| **真实服务 · 4096 连接** | `SVC_WORKERS=4 ./kaula_kl/order_server` + `loadgen 127.0.0.1:8123 4096 20 5` | `TOTAL ok=1,479,589 mismatch=0 errors=0`，服务 **ALIVE** ✅（此前正是此场景 abort） |
| **真实服务 · 连接关闭潮汐** | 3 轮 × 2048 连接短连接，累计约 6144 次关闭 | 三轮全部 `mismatch=0 errors=0`，服务 **ALIVE** ✅（约为旧阈值 6×） |

### 7.4 新回归测试

- `test/concurrency/test_kmm_scope_early_return.kl`：端到端，
  4096 次序/并行调用，每次都走「块级 KMM 区间内 + 挂起点之后 + 提前 return」。
  修复前必崩、修复后必过，是**防回退闸门**。
  刻意避开另一处**无关**缺陷（带挂起点的 conc fn 里 `break` 会挂死），
  以免泄漏信号被超时淹没。
- `compiler/internal/codegen/conc_kmm_exit_pop_test.go`：codegen 单测。

### 7.5 顺带记录（未在本次修复）

排查中发现另一处**独立**缺陷：**带挂起点的 `conc fn` 里 `break`
会挂死**（含裸 `{ }` 块内的 `break`）。最小复现：

```kaula
conc fn f(stop: i64) -> i64 {
    i64 i = 0
    while i < 1000000 {
        u8* r = std.memory.kmm_v4_alloc(64)
        std.memory.kmm_v4_free(r)
        std.time.time_sleep_us(1)
        if i >= stop - 1 { break }   // ← 挂死
        i = i + 1
    }
    return stop
}
```

已在本文件记录，**留待独立轮次**（与本次 KMM 作用域泄漏无因果关系）。
