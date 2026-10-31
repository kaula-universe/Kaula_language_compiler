# P0 — 写穿 + 游标推进的 survivor 段问题（泄漏 + 链损坏）

状态：**已修复并合入（方案 A'，精确判据 `ChainEscaping`）**；实施清单见 §7（全部完成），
最终验证见 §9。
基线：`c983678`
样本：`test/perf/leak_writethrough.kl`（+ 3 个对照，见 `test/perf/`）
数据：`cache/perf-baseline-before.json`
生成代码存档：`cache/codegen-before-writethrough.c`

---

## 1. 触发形态

```kaula
fn build_chain(i64 n) i64 {
    Node* cur = alloc(16)          # 容器：函数局部变量
    i64 head = as<i64>(cur)        # 头指针另存（会返回）
    i64 i = 0
    while i < n {
        Node* nd = alloc(16)
        cur.next = as<i64>(nd)     # ① 写穿：只改容器的字段
        cur = nd                   # ② 游标推进：普通赋值，判定为跨作用域逃逸
        i += 1
    }
    return head
}
```

两个动作单独出现都正常，**只有同时出现才出问题**：

| 形态 | 测试用例 | 基线实测 |
|---|---|---|
| 仅写穿（游标不动） | `leak_writeonly.kl` | PASS，峰值 108KB |
| 仅游标推进（无写穿） | `leak_cursor_assign.kl` | PASS，峰值 1824KB（单轮量，与轮数无关） |
| 纯 churn 对照 | `leak_baseline_flat.kl` | PASS，峰值 208KB |
| **写穿 + 游标** | `leak_writethrough.kl` | **FAIL：链内容错误**（见 §3） |

---

## 2. 生成的 C 代码（问题根源）

```c
while (i < n) {
    kmm_v4_mark_t _loop_scope_start = kmm_v4_mark_save();
    Node* nd = kmm_v4_alloc(16);
    nd->value = i;
    nd->next = -1;
    cur->next = (int64_t)(nd);                                  /* ① 写穿：正确 */
    kmm_v4_survivor_free(cur);  /* release previous survivor copy */   /* ② ← 致命 */
    kmm_v4_scope_promote_p((void**)&nd, 16);                    /* ③ promote */
    cur = nd;                                                   /* ④ 游标推进 */
    i = kaula_add_i64((int64_t)(i), (int64_t)(1));
    /* structural: skip survivor_free for nd (assign-escaped, ownership moved out) */
    kmm_v4_mark_restore(_loop_scope_start);
}
```

### 2.1 链损坏（当前最严重的后果）

② 这条 `kmm_v4_survivor_free(cur)` 是上一轮（`c983678`）为修 `keep = p` 加的
"挂新副本前释放旧副本"。它在**纯游标**场景正确（旧副本只由游标独占），但在
**写穿 + 游标**场景是错的：

- 此时 `cur` 已经通过 ① 被挂进了链（`prev->next == cur`），不再是游标独占；
- ② 释放了 `cur` 指向的**整个对象**，而紧接的 ④ 让 `cur = nd`；
- 下一轮 ① 往"已释放的槽位"写 `cur->next`，该槽位又被下一次 alloc 复用覆盖；
- 结果是链上中间节点成片丢失。

实测（n=10）：链上只剩 2 个节点（头 + 尾），`k=0 value=-1 next=...696`、
`k=1 value=9 next=-1` —— 中间 8 个节点全是空洞。

> 这与用户上一轮"只去 promote（保留回卷）→ 崩溃/死循环、链表被踩成环"的
> 观察是同一根因：`survivor_free(cur)` 在写穿场景下必须被抑制。

### 2.2 survivor 段单调泄漏

③ 的 promote 把 `nd` 的副本放进 survivor 段。该 promote 的持有者 `cur` 是
**函数局部变量** → 被 `markCrossScopePromote` 归类为「内部 promote」→ 只置
`funcEmitsInternalPromote`，**不抑制**函数出口的 `kmm_v4_survivor_rewind`。

但动态池下 `kmm_v4_survivor_rewind` 的真实实现是：

```c
static inline void kmm_v4_survivor_rewind(kmm_survivor_checkpoint_t cp) {
    (void)cp;
    /* 只 flush TLS 垃圾箱（已 free 的延迟槽位） */
}
```

而 `nd` 已被 `MarkAssignEscaped` 抑制了块尾 `survivor_free`，**从未进入垃圾箱**。
于是每轮新增一份副本、无人回收 → 副本数与迭代次数等比增长，跨调用累积。

**关键结论：编译器的"内部 promote 靠出口 rewind 兜底"这个意图，在动态池
下没有对应的运行时实现。** 这是本次修复要补的东西。

---

## 3. 为什么原来的 checkpoint/rewind 形态无法直接扩展

最初的方案设想是"checkpoint 记录各 slab 桶的已分配槽位数快照，rewind 归还
快照之后新增的槽位"。**该设想不成立**：

`kmm_v4_slab_alloc_from_bucket` 用 `free_hint` **轮转**寻找空槽：

```c
size_t slot = use_mask ? ((hint + attempt) & nmask) : ((hint + attempt) % n);
...
__atomic_store_n(&bucket->free_hint, (slot + 1) & nmask, __ATOMIC_RELAXED);
```

释放后槽位会被重新分配，槽位号**不单调**。"快照之后新增的槽位"无法用序号界定。

---

## 4. 方案 A 的实现形态（已定）

用**位图差集**替代"序号快照"，语义精确且无歧义：

```c
typedef struct {
    uint64_t bitmap_snapshot[KMM_V4_SLAB_BUCKETS][KMM_V4_SLAB_BITMAP_WORDS];
    uint32_t allocated_snapshot[KMM_V4_SLAB_BUCKETS];
    size_t   _large_offset;   /* 静态池沿用 */
} kmm_survivor_checkpoint_t;
```

- 动态池 50 桶 × 256 槽 = 50 × 256bit = **1600 字节**，可直接按值放栈上；
- `checkpoint`：拷贝各桶位图 + `allocated` 计数；
- `rewind`：对每桶执行 `释放当前位图中 未被快照包含 的置位`（差集），
  等价于"回收 checkpoint 之后新占用的槽位"，与槽位分配顺序无关。

### 为什么这天然满足 P2（悬垂风险）

| 场景 | 编译器行为 | rewind 后果 |
|---|---|---|
| 副本被 `return` 带走 | `funcEmitsPromote=true` → **抑制** rewind | 不回收，安全 |
| 副本被赋给全局/形参/外部 | 同上（向外 promote） | 不回收，安全 |
| 副本只留在函数局部容器 | `funcEmitsInternalPromote=true` → **不抑制** rewind | 精确回收，泄漏消除 |

即：安全性仍由编译器已有的「内部/向外 promote」分类把关，运行时只负责
**让这个分类真正生效**。编译器判错的最坏后果是"多回卷了一份本该活的副本"，
而这恰好被 `funcEmitsPromote` 抑制 + `MarkPromoteHolder` 的 return 判定挡住。

### 待验证的风险

1. **checkpoint/rewind 开销**：每次函数出入口 1600 字节位图拷贝。高频小函数
   可能受影响 → 需 benchmark，必要时退化为"只记录非空桶"的稀疏快照。
2. **重入/递归**：mark 模式下每个函数有自己的 token，递归时栈上多份 1600 字节
   → 深度 1024 时约 1.6MB，需确认可接受。
3. **2.1 的链损坏必须单独修**（抑制写穿场景对容器的 `survivor_free`），
   这部分在编译器侧（`canReleasePreviousSurvivorCopy`）。

---

## 5. 实现路径的两次修正（实测推翻）

### 修正一：右值类型转换让 promote 链路整体失效（已确认，独立缺陷）

`generateAssignEscapeStatement` 用 `bin.Right.(*ast.Identifier)` 取源。

但 `cur.next = as<i64>(nd)` 的右值是 `TypeCastExpression` 而非 `Identifier`，
断言失败 → 函数提前 return → **写穿路径的 promote 从不发射**。

探针实测（`fmt.Sprintf("%T", bin.Right)`）：

```
[P-U] ok= false  left= *ast.MemberAccessExpression  right= *ast.UnaryExpression
[P-U] ok= true   left= *ast.MemberAccessExpression  right= *ast.Identifier
[P-U] ok= true   left= *ast.MemberAccessExpression  right= *ast.TypeCastExpression
```

修法：穿透纯地址转换（`TypeCastExpression` / `ParenExpression`，后者字段名是
`Inner` 不是 `Expression`）取内层标识符。

### 修正二：只 promote 源不够，容器自身也必须稳定（未解决）

即使 promote 生效，逐轮地址实测仍显示链断裂：

```
iter=0 nd_addr=139665902354448
iter=1 nd_addr=139665902354448   ← 同一地址被复用
iter=2 nd_addr=139665902354448
walk k=0 addr=... value=2 next=-1   ← 只剩最后写入的节点
```

根因：**容器 `cur` 自身仍在 bump 区**。`cur` 在循环前分配，循环体末尾
`mark_restore` 回卷 bump 后，下一轮 `nd = alloc(16)` 拿到 `cur` 所在的
同一地址并覆盖它，头节点的 `next` 就此被抹掉。

推论：只 promote 源不能解决——必须让"整条链"的存活方式一致。

---

## 6. 决策：方案 A'（按 1.9MB 变体的真实机制）

对 `1.9MB + 回读正确` 那个变体的重新解读：

它之所以同时消除泄漏与保链，**不是**因为"去 promote + 去回卷"这两个动作
各自的功效，而是因为：

> 去掉 promote 后 `nd` 留在 bump 区，**且循环体不再回卷**（`mark_restore` 一并去掉），
> 于是整条链在同一个 bump 段里连续排布、互不覆盖；函数出口 `scope_pop`
> 一锅端回收。

即：正确性依赖 **"整条链都在 bump 区 + 循环体不做 mark_restore"**。

### 方案 A' 的判定与动作

当函数内存在「写穿进函数局部容器」时：

1. 该写穿**不 promote**；
2. 该写穿所在的**循环体不做 `mark_restore`**（放弃循环级回卷）；
3. 对象留在 bump 区，函数出口 `scope_pop` 统一回收。

代价：循环内分配不再逐轮回卷，峰值 = 单轮分配量（对 `s1_list` 是 2000×16=32KB，
跨轮由函数出口回收，累积量不跨调用）。可接受。

### 安全前提（P2 的实质）

必须只在容器确为「**安全局部容器**」时启用，否则对象留在 bump 区会在
`scope_pop` 时悬垂。安全局部容器 = 容器满足全部：

- 在本函数内分配（非形参）；
- 从不作为 `return` 值出现；
- 从不作为调用实参出现（`f(cur)`）；
- 从不作为赋值右值出现（`g = cur` / `other.field = cur`）。

任一条不满足 → 回退到当前行为（promote + 回卷，靠泄漏换正确性）。

### 为什么放弃"运行时补批量回卷"（原方案 A）

1. `kmm_survivor_checkpoint_t` 需扩到 1600 字节（50 桶 × 256bit 位图），
   每次函数出入口拷贝；高频小函数开销未验证，递归深度 1024 时栈上约 1.6MB。
2. 它只解决"泄漏面"，不解决"容器留在 bump 区被覆盖"这个**数据损坏面**
   （见修正二）——两者根因不同，运行时补回卷对后者无效。
3. 方案 A' 已有手工验证的成功先例（1.9MB + 回读 sum=2001000）。

---

## 7. 实施清单

- [x] **A'1** 编译器：`unwrapAssignSource` 穿透 TypeCast/Paren（修右值断言，
      已在验证分支证实必要）
- [x] **A'2** 编译器：函数级 AST 预扫描，算出「安全局部容器集合」
- [x] **A'3** 编译器：集合内容器的写穿点不 promote + 对应循环体抑制 `mark_restore`
- [x] **A'4** 回归：`test/perf/` 5 用例全绿 + 112 用例全量无新增失败
- [x] **A'5** 反向用例：容器逃逸（return / 实参 / 赋值右值）必须走旧路径，
      且回读正确（P2 边界护栏）

---

## 7.5 收尾阶段发现的第三个缺陷：链首随地址值逃逸（已修）

A'1–A'3 落地后 `leak_container_escape.kl`（逃逸容器）与
`leak_writethrough.kl` 双双 SIGSEGV。定位过程与结论：

### 现象

`i64 head = as<i64>(cur); ...; return head` 形式在 n=2000 时必崩，
n=5 时正常。把 `head` 改声明为 `Node*`（同形、仅类型不同）后**不再崩**。

### 根因

`as<i64>(cur)` 把 cur 的**地址值**存进 head，与 `Node* head = cur`
（指针别名）语义等价，但词法上类型不同 → 逃逸传播的别名对识别不到它
→ `cur` 被误判为「从不逃逸」→ 进入 Safe 集合。

随后 liveness 在 `cur = nd` 之前的最后使用点发射
`kmm_v4_survivor_free(cur)` —— 而 `cur` 此刻**正是链首节点**
（`value = -1`，`next` 指向第 0 号节点）。该槽位被释放后，下一轮
`build_chain` 的分配器把它回收另作他用，链首被覆写。

n 小时 slab 池（256 槽/桶）尚未耗尽，被释放的槽位没被复用 → 看不出来；
n=2000 时远超单桶容量，复用必然发生 → 必崩。这解释了
「小规模正常、大规模崩溃」的分裂现象。

### 修法（两处，精确判据，刻意不加宽）

1. **扫描层**（`safe_container_scan.go`）：新增 `ChainEscaping` 集合。
   识别 `unwrapAddressSource`（`as<T>(c)` 形式）登记的「地址值别名」，
   配合 `collectRootIdentifiers` 收集的 return 返回值。当
   「容器 c 被写穿过 + c 的地址值别名随 return 出去」时，把 c 移出 Safe、
   并入 Escaped。
2. **代码生成层**（`generateEarlyFreeForStmtIndex`）：新增
   `IsChainEscaping(name)` 豁免 guard。

### 为什么必须用 `ChainEscaping` 而不是现成的 `Escaped`

第一版曾直接用 `IsEscapedVar` 当闸门，**立刻引入 7GB 内存回归**：
`escapedVars` 是词法粗判集合，包含大量「只是出现在 return/实参表达式里、
但对象本身仍归本函数」的变量（`alloc_bench` 阶段 F 的循环内作用域缓冲
`nodes` 即典型：它被 `build(nodes,...)` / `inorder(nodes,0)` 借用，词法上
"出现在实参里"→ 被判逃逸）。用它当闸门会误伤这些变量的正常提前释放，
作用域缓冲不再回收 → `alloc_bench [sor]` RSS 9.9MB → 7GB 后被 OOM kill。

故退回到精确判据 `ChainEscaping`：只覆盖「链首确实被带出函数」这一种情形。
修复后 `alloc_bench` 生成 C 与基线**逐字节一致**（仅时间戳不同），
RSS 10.1MB ≈ 基线 9.9MB。

### 用例口径拆分（重要）

`leak_writethrough.kl` 原稿的注释写"容器 cur 随函数返回死亡"，但函数体里
`i64 head = as<i64>(cur); return head` 实际把链首交了出去 —— 用例自身
口径矛盾。据此按两种语义拆成两个文件，各自断言：

| 文件 | 容器语义 | 断言 |
| --- | --- | --- |
| `leak_writethrough.kl` | **安全**（不逃逸，链在函数内消费完） | `growth == 0`（水位不随轮数涨） |
| `leak_container_escape.kl` | **逃逸**（链首随 return 出去） | 只要求回读正确；允许水位上涨（旧路径"用泄漏换正确性"的既定代价） |

`leak_writethrough.kl` 改为在函数内就地读链求和、只返回标量，
这才真正落在「安全局部容器」定义里。修复后 `growth=0`，
峰值 29MB → 212KB。

---

## 8. 附：右值类型转换缺陷的独立价值

修正一（`unwrapAssignSource`）与 P0 修复可解耦：它本身是一处**静默缺陷**——
任何"把新对象经类型转换挂进容器"的代码（`as<i64>(p)` / `as<void*>(p)`）
都不会触发 promote，在跨作用域场景下产生悬垂。建议即使 A' 方案有变，
这个穿透也应保留。

---

## 9. 本轮最终验证

| 项 | 结果 |
| --- | --- |
| `go test ./...`（compiler） | 全通过 |
| `scripts/run_tests.py`（56×2） | **112/112** |
| `scripts/run_tests.py --quick`（49×2） | **98/98** |
| `test/perf/run_perf.py` | **5/5**（writethrough 峰值 29MB → 212KB，growth=0） |
| `alloc_bench [sor]` | RSS 10.1MB（基线 9.9MB），生成 C 与基线逐字节一致 |

顺带修掉一个长期存在于基线的崩溃：`stdlib_async_safety` 的
「重复销毁」用例（`std/async/async.c` 的 `light_async_loop_destroy`
幂等化），此前在 `4a19705..c983678` 全区间稳定 exit 139。

