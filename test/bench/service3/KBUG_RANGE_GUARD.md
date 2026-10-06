# kaulac：`for..in(range)` 循环体守卫槽跨迭代不复位（**已修复**）

> **已定案并修复**（提交见本文件末尾）。最小复现 ~20 行，非并发可触发。
> 这是**编译器 lowering 的正确性缺陷**，静默算错，不崩不报错。
>
> **严重度**：高。任何「挂起点之后跟着 `for k in range(N)` 且循环体跨迭代
> 累积状态」的代码都会只执行第一轮 —— 循环体其余 N−1 轮被整体跳过。
>
> ⚠️ **本文件保留原始调查记录**（§1–§8），修复说明见 §9。
> 缺陷已修，基准侧的「下沉到普通 fn」规避**已移除**（见 §10）。

## 1. 症状

三边服务对同一事务（`TRANSACTION.md` §3.2）算出不同 checksum：

| 边 | checksum |
|---|---|
| Go（真实执行） | `2ceb59c5` ✅ |
| Tokio | 待测（同 Go 语义） |
| **Kaula** | **`2ceba850`** ❌ |

反解差异：`2ceba850 ^ 2ceb59c5 = 0xF195 = 61845`，
而 **61845 恰好是 64 轮 fold 的 h 在第 1 轮（k=0）的值**。
→ Kaula 的循环**只跑了第 1 轮**。

## 2. 根因（生成代码 + 控制流）

`conn_task` 里 `for k in range(64)` 位于挂起点之后，因此走守卫链 lowering。
生成代码如下（`cache/f99e616…c:372-404`，已按行号核对）：

```c
if (f->sub[2] == 16) {
    f->sub[2] = 17;
    for (long long _i_ = (0); _i_ < (64); _i_ += (1)) {   /* ← 循环头在守卫外 */
        long long k = _i_;
        int32_t x;
        x = f->x;
        if (f->sub[2] == 17) { f->sub[2] = 18; x = h ^ (int32_t)(r[k]); }
        if (f->sub[2] == 18) { f->sub[2] = 19; acc = kaula_add_i64(x, seq); }
        if (f->sub[2] == 19) { f->sub[2] = 20; acc = kaula_add_i64(acc, qty); }
        if (f->sub[2] == 20) { f->sub[2] = 21; acc = kaula_add_i64(acc, price); }
        int32_t m;
        m = f->m;
        if (f->sub[2] == 21) { f->sub[2] = 22; m = kaula_mul_i64(acc, 31); }
        if (f->sub[2] == 22) { f->sub[2] = 23; h = kaula_add_i64(m, k); }
    }                                                     /* ← 循环尾也在守卫外 */
}
if (f->sub[2] == 23) { f->sub[2] = 24; ck = ck ^ (uint32_t)(h); }
```

**执行轨迹**：

| 迭代 | 进入时 `sub[2]` | 守卫链结果 | h |
|---|---|---|---|
| 1 | 17 | 17→18→19→20→21→22→23，**全部命中** | 61845 |
| 2 | **23** | 17..22 全不成立 → **循环体空转** | 61845 |
| 3..64 | 23 | 同上，空转 | 61845 |

`sub[2]` 在**循环体外**由 16 推到 17，循环体内部把它推到 23 后
**没有任何地方复位回 17**。于是第 2 轮起整条守卫链全部失配。

**为什么 `f->sub[2] = 0` 救不了**（`c:506`）：那处在**内层 while** 的
结束时复位，是 `for` 的**上一层结构**；循环体内部看不到它。

## 3. 精确触发条件

| 条件 | 是否必需 | 依据 |
|---|---|---|
| 循环在**挂起点之后** | ✅ 必需 | 无挂起点的函数不生成守卫链，实测 `repro` 的 range 版返回正确的 64（`cache/32f0ef90…c:79` 无守卫） |
| 循环体**跨迭代累积状态** | ❌ 非必需 | 即使每轮独立，也只剩第 1 轮被求值 —— 只是观测不到 |
| `for..in(range)` | 待定 | `while` 版在本例中同样未生成守卫链，未能对照 |

**未被本用例区分的边界**：`while` 循环在挂起点之后是否会生成同样的
守卫链并同样不复位 —— 需要构造「while 体在守卫中」的用例才能判定。
**因此当前只能说：`for..in(range)` 确认有此缺陷，`while` 未证伪。**

## 4. 为什么之前没被抓到

- 现有并发测试（`test/concurrency/*.kl`）覆盖的是**挂起点本身**的语义
  （guard slot residue / guard loop return），没有覆盖
  「**挂起点之后、循环体内带 guard 的迭代计数**」。
- `test_guard_slot_residue.kl` 测的是**顶层复合语句**的槽位残留
  （挂起点切段后 `sub[0]` 不回 0），与本条（**循环体内** `sub[2]`
  跨迭代不回位）是**同一族但不同位置**的缺陷，该用例抓不到本形态。
- 微基准 `test/bench/kaula/bench.kl.tmpl` 的 `degrade` 曲线用 4x 循环展开
  写的折叠（`round*4+i` 形式），恰好绕开了 `for k in range` 的守卫形态。

## 5. 影响面

任何形如以下的用户代码都会静默算错：

```kaula
conc fn f() -> i32 {
    std.time.time_sleep_us(1)      // 或任何挂起点
    i32 acc = 0
    for k in range(64) {           // ← 只跑 1 轮
        acc = acc + something(k)
    }
    return acc
}
```

包括但不限于：hash/校验和、累加统计、重试循环、逐字节编解码、
`for..in(range)` 形式的矩阵/数组折叠。

## 6. 规避方式（本轮基准采用）

把 `for k in range(N)` 换成 `while`，或把循环体提到**无挂起点的普通
`fn`** 里（普通函数不生成守卫链）：

```kaula
fn fold64(seq: i32, qty: i32, price: i32, r: *u8) -> i32 {
    i32 h = 0
    i32 k = 0
    while (k < 64) { ... k = k + 1 }
    return h
}
conc fn conn_task(fd: i32) -> i32 {
    ...  std.time.time_sleep_us(200)
    i32 h = fold64(seq, qty, price, r)   // 计算下沉到普通 fn
}
```

**必须随报告披露**：本基准 Kaula 侧采用了下沉写法。
不影响三方公平性（三边算的是同一件事、同一组常量、同样多的循环），
但**用户按文档直写 `for..in(range)` 会得到不同结果**。

## 7. 建议修复方向（未做）

1. **codegen**：带守卫的 `for` 循环体在每次迭代开始时应复位
   `f->sub[d]` 到循环体首槽位（即把 `sub[d] = 17` 放进循环体首、
   而不是只在循环头之前的 `if (sub[2] == 16)` 里做一次）。
2. **回归用例**：把 §1 的最小复现固化为 `test/concurrency/` 下的
   语义用例（返回值判据），命名建议 `test_guard_range_iter.kl`。
3. **同族排查**：`test_guard_slot_residue.kl` 已覆盖顶层形态，
   需补「循环体内嵌套结构（if/while/for）的槽位复位」矩阵。

## 8. 证据链

| 步骤 | 手段 | 结果 |
|---|---|---|
| checksum 不一致 | 三方压测 `mismatch` 计数 | Kaula `2ceba850` vs Go `2ceb59c5` |
| 排除 u32 段 | 探针打印 `ck % 2^31` | `753621445` == Go `0x2ceb59c5 % 2^31` ✅ 一致 |
| 排除解析段 | 探针打印 seq/qty/price | 1 / 3 / 1990 三边一致 |
| 排除响应编码 | wrapper 注入打印真实字节 | `[RAW] R,1,2ceba850,5970` 格式正确 |
| 定位到 h | 反解 `2ceba850 ^ 2ceb59c5 = 0xF195` | `0xF195 = 61845` = **第 1 轮的 h** |
| 确认算法正确 | 独立 `trace_s4.kl`（无守卫链）逐轮打印 | 与 Go **逐轮数值完全一致**，final `-768271034` ✅ |
| 确认守卫不复位 | 读生成代码 `cache/f99e616…c:372-404` | 循环体内无 `sub[2]` 复位，第 2 轮起全失配 |

> 全部证据均可复现：`kaulac` + `cache/f99e616…c`（源 `order_server.kl`
> 的未规避版本）+ 探针 `trace_s4.kl`。

## 9. 修复（2026-10）

### 9.1 精确根因（修正 §2 的描述）

§2 说「循环体内的守卫槽不复位」——方向对，但**位置说错了一层**。
真实成因是「**原子循环体被外层守卫污染**」：

`generateForRangeStatement` 分两条路（`stmtgen.go`）：

| 路径 | 触发条件 | 循环体 |
|---|---|---|
| **守卫模式** | 本 for..in 的**自身循环体**含挂起点 | 走 `for(;;)`，每轮末尾 `sub[d] = 1` 复位 ✓ |
| **原子模式** | 自身循环体**不含**挂起点 | 走普通 C `for` —— **设计上不该带任何恢复槽位** |

问题是原子模式下，若该 for..in **嵌在外层守卫复合语句里**
（`concGuardActive()` 仍为真），循环体语句仍会被 `concGuardSlot`
按**外层**的槽位号包进 `if (f->sub[d] == k)`。而外层槽位是
「一条语句推进一次」语义，在 C 的 `for` 循环体里**跨迭代从不复位**：

```c
if (f->sub[1] == 2) { f->sub[1] = 3;
    for (long long _i_ = (1); _i_ < (5); _i_ += (1)) {
        long long k = _i_;
        if (f->sub[1] == 3) { f->sub[1] = 4; acc = acc + k; }  /* 只有第 1 轮命中 */
    }                                                          /* 2..N 轮整条失配 */
}
```

第 1 轮把 `sub[1]` 从 3 推到 4，第 2 轮起 `== 3` 不成立 → 循环体空转。

> **这解释了为什么 §2 的最小复现「看起来」是另一回事**：
> `order_server.kl` 里的 `for k in range(64)` 位于 `while (true)`
> **连接处理循环之内**，那层 while 是守卫复合语句 ——
> 内层 for..in 自身无挂起，于是走了原子模式并被外层槽位污染。

### 9.2 修复

`generateWhileStatement` 早就处理了同一问题（`guardSuspended`：
生成循环体期间临时把 `concGuardDepth` 置 −1，让语句平铺）。
`generateForRangeStatement` 漏了这一步。修复即**移植同一保护**：

```go
guardSuspended := false
guardSavedDepth, guardSavedSeq := sg.codegen.concGuardDepth, sg.codegen.concGuardSeq
if guardDepth < 0 && sg.codegen.concGuardActive() {
    sg.codegen.concGuardDepth, sg.codegen.concGuardSeq = -1, 0
    guardSuspended = true
}
// …生成循环体…
} else if guardSuspended {
    sg.codegen.concGuardDepth, sg.codegen.concGuardSeq = guardSavedDepth, guardSavedSeq
}
```

改动 25 行，**不动**守卫模式下已有的复位逻辑（那是正确的）。

### 9.3 验证

| 层次 | 用例 | 修复前 | 修复后 |
|---|---|---|---|
| 语义 | `test/concurrency/test_forin_atomic_reset.kl`（新增） | — | `err=0` |
| codegen | `TestConcGuard_AtomicNestedLoopBodyHasNoSlots`（新增） | **FAIL**（循环体内有 `f->sub[`） | PASS |
| codegen | `TestConcGuard_SuspendingLoopStillHasSlots`（对照，新增） | PASS | PASS |
| 回归 | `python3 test/concurrency/run_tests.py` | — | **30/30 全通过** |
| 单元 | `go test ./...`（compiler） | — | 14/14 包通过 |
| 端到端 | `loadgen` 对**用户直写 for..in** 的服务（§10） | `mismatch=223744` | **`mismatch=0 errors=0`** |

§2 的最小复现（`repro6.kl`：外层 while + 内层 `range(1,5)`）实测
由 `1001` 变为正确的 `1010`；`repro5.kl` 由 `1000` 变为 `1006`。

### 9.4 未被本次修复覆盖的边界

- `while` 在「挂起点之后、自身无挂起、嵌在外层守卫里」时**早已**有保护
  （`guardSuspended`），本轮未改动、但也未新增针对性用例
  —— 既有 `test_guard_*.kl` 间接覆盖。
- `if` 的原子分支体走 `concIsResumableCompound` 分支，不是循环、无「下一轮」，
  本身不受本缺陷影响（§5 已述）。
- 数组/string 迭代的 for..in 含挂起时仍在 `concCheckSupported` 报诊断（不变）。

## 10. 基准侧规避已移除

`test/bench/service3/kaula_kl/order_server.kl` 原先把 64 轮折叠**下沉到
普通 `fn mixed_verify64`** 以绕开本缺陷。修复后**已改回用户直写形态**：

```kaula
i32 h = 0
for kk in range(64) {
    i32 x = h ^ as<i32>(r[kk])
    i32 acc2 = x + seq
    acc2 = acc2 + qty
    acc2 = acc2 + price
    i32 m = acc2 * 31
    h = m + kk
}
```

`mixed_verify64` 函数已删除。端到端 `loadgen` 验证 `mismatch=0 errors=0`。

