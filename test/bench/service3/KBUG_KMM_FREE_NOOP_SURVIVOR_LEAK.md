# codegen：`kmm_v4_free` 是 no-op + 用户 free 误抑制回收 + conc 守卫死分配 → 每次 resume 泄漏幸存槽

> **已修复**（本轮）。
> 严重度：**高（可用性）**。`conc fn` 循环体内的 KMM 分配在每次恢复时
> 泄漏一个幸存段槽位，RSS 按请求线性增长（实测 **194–197 B/req**），
> 长时压测必 OOM。这是 IoDriver 侧 `KaulaIoPending` 泄漏（见
> `KBUG_IODRIVER_TAKE_FD_PENDING_LEAK.md`）之外**第二个独立泄漏源**。

## 1. 症状

`service3` 基准压测 `kaula_kl/order_server`，RSS 按**请求**线性增长
（与连接数无关），停载不回落。KMM 幸存段（195 MB）在 20s 内饱和后
降级到页式 `malloc`，RSS 从 310 MB 涨到 767 MB。

## 2. 三个叠加缺陷

### 缺陷 1：`kmm_v4_free` 是彻底 no-op

```c
/* src/kmm_scoped_allocator_v4.c:1103 */
void kmm_v4_free(void* ptr) { (void)ptr; }
```

bump allocator 的设计是「靠 scope 回收」，`free` 不做事本身没问题；
问题在于**用户显式 free 被编译器当成了「已回收」的信号**（缺陷 2）。

### 缺陷 2：用户 free 误抑制 survivor_free

`stmtgen.go` 的作用域出口 / return 回收逻辑中：

```go
if sg.codegen.IsUserFreed(name) {
    /* 跳过 survivor_free */
    continue
}
```

用户写了 `std.memory.kmm_v4_free(r)` 后，编译器认为 `r` 已回收，
于是**不发射** `kmm_v4_survivor_free(r)`。而 `kmm_v4_free` 是 no-op，
幸存段槽位**永远不会归还**。

**修复**：KMM 来源变量不得因 `IsUserFreed` 跳过回收（其余来源维持原语义）：

```go
if sg.codegen.IsUserFreed(name) && sg.codegen.GetVarOrigin(name) != OriginKmm {
```

### 缺陷 3：conc 守卫降级发射「死分配」

`conc_guard.go` 的 `concGuardedDecl` 把带初始化的声明降级为「纯声明 +
独立初始化语句」，但降级**之前**已按原表达式发射了一次分配：

```c
uint8_t* r = kmm_v4_alloc_global(sizeof(uint8_t));  /* 死分配 */
r = f->r;                                           /* 立即被覆盖 */
```

每次 resume 执行一次 → 每次 resume 泄漏一个幸存槽。

**修复**：降级前登记 origin/size 并置 `SetInitializerSupplied`，
`generateVariableDeclaration` 检查该标记后**只发射声明**：

```c
uint8_t* r; /* structural: decl only (initializer generated separately) */
r = f->r;
...
r = kmm_v4_alloc(64);
kmm_v4_survivor_free(r); /* structural: per-object reclaim (kmm) */
```

### 子缺陷

- `RecordVarOriginFromAST` 只识别 `*ast.MemberExpression`，而当前解析器
  实际产出 `*ast.MemberAccessExpression` → `std.memory.kmm_v4_alloc(...)`
  的 origin **恒为 unknown**，回收路径选不对。
- SOR 早退分支漏登记 origin（普通声明路径有）。
- `ResetLifetimeFlags` 会清除 `initializerSuppliedVars`，导致
  `concGuardedDecl` 置的信号被后续 `AddSymbol` 抹掉 —— 已从该函数中移除。

## 3. 修复落地

| 文件 | 改动 |
|---|---|
| `compiler/internal/codegen/codegen.go` | 识别 `MemberAccessExpression`；新增 `initializerSuppliedVars` + `Set/IsInitializerSupplied`；`ResetLifetimeFlags` 不移除该字段 |
| `compiler/internal/codegen/conc_guard.go` | 降级前登记 origin/size 并置 decl-only 标记 |
| `compiler/internal/codegen/stmtgen.go` | decl-only 早退（置于 SOR 分支之前）；`IsUserFreed` 跳过限定为非 KMM 来源；SOR 早退分支补登 origin |
| `compiler/internal/codegen/kmm_free_noop_survivor_test.go` | **新增** 3 个回归测试 |

## 4. 验证

### 4.1 回归测试（真实回归锁）

三个新测试在有修复时通过；`git stash` 回退修复后**全部 FAIL**：

```
--- FAIL: TestUserFreeDoesNotSuppressSurvivorFree
--- FAIL: TestNoDeadSurvivorAllocInConcGuard
--- FAIL: TestConcGuardDeclRegistersKMMOrigin
```

- `TestUserFreeDoesNotSuppressSurvivorFree`：断言 KMM 变量即使用户 free
  也必须发射 `kmm_v4_survivor_free(r)`。
- `TestNoDeadSurvivorAllocInConcGuard`：逐行扫描断言无「`kmm_v4_alloc_global`
  后 1~2 行同变量立即被覆盖」的死分配。
- `TestConcGuardDeclRegistersKMMOrigin`：断言存在 `per-object reclaim (kmm)`
  且无 `per-object reclaim (unknown origin`。

### 4.2 生成 C 核验

`KAULAC_DUMP=1` 导出，`order_server.kl` 生成代码：

```
structural: decl only           : 28 处
per-object reclaim (kmm)        :  2 处
per-object reclaim (unknown    :  0 处
死分配（alloc 后立即覆盖）      :  0 处
```

### 4.3 压测

| 场景 | 请求数 | ΔRSS | B/req |
|---|---|---|---|
| 修复前 | 3,532,327 | **621.0 MB** | 184.3 |
| 修复后 | 3,940,646 | **0.0 MB** | 0.000 |
| 阶梯 60s | 3,929,584 | 0.0 MB | 0.000 |
| 阶梯 120s | 7,784,229 | 0.0 MB | 0.000 |
| 阶梯 180s | 11,654,198 | 0.0 MB | 0.000 |

### 4.4 全量测试

`cd compiler && go test ./...` 全量通过（`ok kaula/internal/codegen`），
`go vet ./internal/codegen/` 无告警。

## 5. 补充：`generateConcReturn` 的 per-object 回收

同轮修复中还发现 `generateConcReturn`（`stmtgen.go`）只发射 KMM scope
回卷（`concKMMExitPopLines`），**漏掉 per-object survivor 回收** ——
普通 fn 的 return 路径（`generateReturnStatementNormal`）会调用
`generateSurvivorFreeForReturn`，conc fn 走了降级路径则不会。

典型受害形态是「跨挂起点存活的缓冲区」（如 `conn_task` 的
`u8* buf = kmm_v4_alloc(8192)`、`u8* resp = kmm_v4_alloc(64)`），
按连接泄漏 8192+64 = 8256 B。

**修复**：在 `generateConcReturn` 中，于 `popCode` 之前补发
`generateSurvivorFreeForReturn("")`，与普通 fn 的「先 free 再 pop」次序一致。
