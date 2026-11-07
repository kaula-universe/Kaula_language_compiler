# P3 · 返回链「去逐对象提升」+ TLAB 区域回收

状态：**已实现，回归/压测通过**
样本：`test/bench/clbg/kaula/binary_trees_ptr.kl`（bt10/bt12）、
`test/stress_ptr_tree.kl`、`test/recursive_ptr_tree.kl`、
`test/perf/leak_*.kl`
实现：`compiler/internal/codegen/return_escape_scan.go`、
`src/kmm_scoped_allocator_v4.{h,c}`

---

## 1. 问题：P-A 的浅拷贝仍然按对象付费

P-A（分配点提升）解决了「return 交出的裸指针逃出作用域」，但它是
**按对象**工作的：递归构造 `bottomUpTree` 时，每一层返回一个节点，就要

1. `kmm_v4_scope_promote_p` 把节点从 bump 区浅拷贝到 survivor 段；
2. 在块尾 / return 路径上逐对象 `survivor_free`；
3. 每层函数再建立自己的 KMM 作用域，出口 `scope_pop` 回卷。

链式构造（binary-trees、链表 builder）里，整棵子树本来只需要由**最外层
调用方**的作用域一次性回收，逐层 promote/free 是纯开销，而且浅拷贝只复制
根节点——子树仍在会被回卷的 bump 区，生命周期模型本身是错位的。

P3 的目标：识别这类「整条返回值就是一条新对象链」的函数，让整条链留在
**调用方 bump 区**：

- 不发射函数级 KMM 作用域；
- 不发射 return promote / 逐对象 survivor_free；
- 不发射块级 / if 分支的 mark_restore；
- 链的回收完全交给调用方作用域（`scope_pop` / 循环 `mark_restore`）。

非链逃逸（全局变量、参数、yield/extract 等）继续走原有 P-A 路径，行为不变。

---

## 2. 链构造器识别（`return_escape_scan.go`）

纯词法 AST 预扫描，在 `FlattenDeclarations` 之后、任何函数体生成之前运行。
判定是**全有或全无**的保守集合：

### 2.1 函数形状

- 顶层普通函数，非 `main`、非泛型、非并发/asm/inline/no_kmm；
- 无注解、无闭包捕获；
- 返回**单个指针**；
- 函数体只允许：变量声明、赋值表达式语句、`if`、`return`、块；
- 无循环、无 switch/spend/prefix/tree、无 yield/spawn/release/extract、
  无嵌套函数/lambda、无裸调用语句。

### 2.2 数据流

「链值」只有两类来源：

- bump 分配：`kmm_v4_alloc` / `kmm_v4_alloc_auto` / `kmm_v4_bump`
  （以及 `malloc`/`calloc`/`std_malloc` 的显式封装）；
- 对已确认链构造器的（递归/互递归/转发）调用。

约束：

- 每个链值必须最终归属于 return 交出的那条链（直接 return，或经
  `宿主.字段 = 源` 写穿挂到已归属节点）；
- 所有 return 只能返回归属链值或 `null`；
- 字段写穿的目标必须是链值，源必须是链值或 `null`；
- 链值不得赋给另一个变量（别名）、不得赋给非指针变量、不得参与算术
  （指针相减会泄漏地址）；
- 链值作为普通调用实参时，被调函数必须是 borrow-only；
- 任何不满足 → 整个函数退出链构造器集合，退回 P-A。

### 2.3 borrow-only 不动点

`itemCheck(Node*) i64` 这类只读遍历函数会被算进 borrow-only 集合：

- 显式返回类型且不含 `*`（空返回类型不假设安全，防止漏写返回类型时
  直接把形参指针交出去）；
- 指针形参只出现在成员访问、下标读、null 比较、或传给另一个
  borrow-only 函数；
- 不得赋值、写字段、强转、取地址、return、传给普通函数或闭包捕获。

### 2.4 调用方接收者校验

链调用结果只有作为**声明初始化式的根调用**才可能安全。接收者变量的
所有使用必须：

- 词法上落在声明块内（不能 return、yield、写字段、赋给别名）；
- 是读 / 比较 / 成员访问 / 传给 borrow-only 函数；
- 同名变量在不同兄弟块中重复声明时，按「最内层可见声明」按路径归属，
  不能按名字合并。`stress_ptr_tree.kl` 的两个 `a` 就是这条规则的回归样本。

任一处逃逸 → 从集合摘掉被调链构造器，并级联重验依赖它的调用者，直到
不动点。

---

## 3. Codegen 闸门

链构造器上下文由 `funcgen.go` 注入，`stmtgen.go` 在以下位置统一查询
`IsChainManagedVar` / `InChainFunction`：

| 位置 | 非链函数 | 链构造器 / 安全接收者 |
|---|---|---|
| 函数级 KMM 作用域 | 按分析决定 | 强制关闭 |
| 分配点 P-A 升级 | 命中则 `kmm_v4_alloc_global` | 强制留在 bump |
| return promote | 发射 | 不发射 |
| 块/if 的 mark_restore | 发射 | 不发射 |
| early free / survivor_free | 发射 | 跳过 |

生成形态（bt10 `bottomUpTree`）：

```c
Node* bottomUpTree(int64_t depth) {
    if (depth <= 0) {
        Node* leaf = (Node*)(kmm_v4_alloc(16));
        leaf->left = NULL;
        leaf->right = NULL;
        return leaf;
    }
    Node* l = bottomUpTree(kaula_sub_i64(depth, 1));
    Node* r = bottomUpTree(kaula_sub_i64(depth, 1));
    Node* n = (Node*)(kmm_v4_alloc(16));
    n->left = l;
    n->right = r;
    return n;
}
```

没有 `KMM_V4_SCOPE_NAMED`、没有 promote、没有 per-object free。调用方
`main` 仍保留作用域与循环 `mark_restore`，整条链在那里一次性回收。

---

## 4. 运行时：TLAB 区域登记表

### 4.1 背景

`mark_restore` / `scope_pop` 跨 TLAB（refill 换过 base）时，旧实现只把
**当前** TLAB 的 offset 置 0；保存 base 指向的旧 TLAB 从此不再使用，剩余
容量（最多 `KMM_TLS_BUFFER_SIZE`）永久浪费。嵌套作用域交替 refill 时，
每跨过一块 TLAB 就漏一段死容量。

### 4.2 数据结构（TLS，元数据与 payload 分离）

```c
typedef struct {
    uint8_t* base;
    size_t   capacity;
    size_t   offset;   // 存活水位（仅 suspended 有意义）
    uint64_t seq;      // TLAB 分配序号，越小越老
} kmm_tlab_region_t;

typedef struct {
    kmm_tlab_region_t suspended[512];  // refill 换下、仍可能有存活前缀
    kmm_tlab_region_t free_list[512];  // 整体死亡、可被 refill 复用
    size_t suspended_count, free_count;
    uint64_t next_seq, current_seq;
} kmm_tlab_registry_t;
```

**关键不变量**：登记表只在 TLS 数组里放元数据，绝不向 TLAB payload 写
header / 魔术字。payload 的每个字节都属于用户对象，回卷时可能仍被上层
存活对象引用。

### 4.3 恢复策略：不抢占当前 TLAB

跨 base 回卷时若把当前 TLAB 换回目标 TLAB，循环体（每轮 `mark_save`）
会在 A/B 两块之间乒乓 refill。实测 bt12 从 220ms 退化到 570ms。因此：

1. 命中 `suspended[]` 目标：只把目标的存活水位收缩到保存的 offset；
2. `seq` 比目标新的 suspended 条目整体死亡 → 退役到 `free_list`；
3. **当前 TLAB 保持为 current，offset=0 原地复用**；
4. 目标的存活前缀（offset>0）继续留在 suspended，等更外层的回卷；
5. 目标 offset==0 → 整块退役到 `free_list`。

`base == NULL`（mark 保存在任何 TLAB 之前）走同一原则：suspended 全部
退役到 free_list，当前 TLAB 保留并置 offset=0。若这里清空 base/capacity，
循环里每次 `mark_restore(NULL)` 都会重新 refill（flat churn 2000 轮 ×
128KB，`kmm_v4_usage` 线性增长）——这是压测抓到的回归点。

### 4.4 安全性 / 回退

- 严格依赖 mark/scope 的 **LIFO** 使用：回卷到某 mark 时，seq 更大的
  TLAB 及其分配全部位于被回卷区间内，整体死亡；
- 目标未命中（登记表满 / 陈旧 mark）：保留当前 TLAB，offset=0。安全，
  只浪费容量，绝不错误复用仍存活对象；
- `free_list` 表满：丢弃容量，安全泄漏；
- `kmm_v4_thread_heap_invalidate`（reset/destroy 路径）清空登记表并重置
  seq；reset 后全局 offset 归零，`trim_tail(0)` 归还扩展段。

### 4.5 移除 `pool_trim_tail` 调用

`mark_restore` / `scope_pop` 原先在 LEVEL 0 调用
`kmm_v4_pool_trim_tail(g_kmm_v4_offset)`。但全局 offset 是单调递增的
跨段额度计数器，回卷只作用于线程 TLAB，不会让全局 offset 回退；用当前
offset 作水位永远摘不掉尾段，只是每次循环/作用域出口做一次无意义段链
遍历。现在扩展段归还统一由 `kmm_v4_reset`（单线程上下文，offset 归零后
`trim_tail(0)`）承担。TLAB 容量复用则由第 4 节的登记表负责。

---

## 5. 实测

环境：Windows / clang，`kaulac --release -no-cache`；warm 数据为 5 次
运行中位数，峰值用 1ms 轮询 `WorkingSet64`。首次运行含缺页，单独列出。

| 用例 | P-A 基线 | P3 最终 | 说明 |
|---|---|---|---|
| bt10 | 348 ms / 5.3 MB | 29.8 ms / 2.19 MB | warm 中位数；首跑约 296 ms |
| bt12 | 1714 ms / 3.1 MB | 30.6 ms / 2.57 MB | warm 中位数；首跑约 291 ms |
| `stress_ptr_tree` | 8.4 s / 6.94 MB | 8.35 s / 6.92 MB | 输出与 checksum 全对齐；链优化已生效 |
| `recursive_ptr_tree` | PASS | 30 ms / 1.75 MB | 三项 checksum 全对齐 |
| `leak_baseline_flat` | 水位随轮数增长 | `after1=afterN=131072`，growth=0 | 对照组 |
| `leak_cursor_assign` | — | growth=0 | |
| `leak_writeonly` | — | growth=0 | |
| `leak_writethrough` | — | growth=0 | |

`stress_ptr_tree` 的峰值与优化前持平（6.9MB），因为它同时保留 long-lived
树与跨批累计；真正证明收益的是时间（热态 30ms）与 leak 用例的零增长。
`depth=16` 的 stretch 树、四档循环 checksum、3 批 RSS 探针全部与期望值
逐字一致。

---

## 6. 验证清单

- `go test ./internal/codegen`：
  - 链扫描正/负例（树构造、循环、未归属分配、外部字段、逃逸接收者、
    非借用调用、别名、指针算术、空返回类型、兄弟块同名重声明、转发链
    级联撤销）；
  - 生成形态测试：链构造器体内无 scope/promote/free/mark_restore，
    main 仍保留作用域与循环回卷；
  - C 级 TLAB 登记表测试：suspended 命中、水位收缩、free_list 复用、
    scope_pop、目标未命中、base==NULL、seq 退役、payload 可写。
- `python scripts/run_tests.py --jobs 4 --timeout 300`：68 程序 × 2 模式
  全部通过（136/136）。
- `python test/perf/run_perf.py`：6/6 通过（Windows 上 `/proc` 采样为 0，
  峰值以本文件第 5 节的原生轮询为准）。
- `go test ./...`：全部包通过（含 Windows 下跳过 POSIX-only 的假编译器
  缓存用例）。
- freestanding：`toolkit_build.py --target freestanding` 构建通过；
  `run_freestanding_tests.py --only L1,L2` 22/22、`--only L3,L4,L6` 25/25。
  L5 全量 `--freestanding` 编译中的失败全部来自本机
  `~/.kaula/pkglib/blib/src/big.c` 这个已损坏的第三方包缓存（1.3MB 二进制
  垃圾，clang 报 `not valid UTF-8`），与本次改动无关。
- `stress_ptr_tree.kl`、`recursive_ptr_tree.kl`、`leak_container_escape.kl`
  均 `DONE`/`PASS`。

已知环境噪声：`go test ./cmd/kaulac` 的
`TestDetectCCFamilyCachesResult` 在 Windows 上因假编译器 `#!/bin/sh`
脚本无法执行而失败，与本次改动无关（Linux CI 通过）。
