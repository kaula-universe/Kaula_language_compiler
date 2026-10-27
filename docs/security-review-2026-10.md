# Kaula 仓库安全审查汇总

> 来源：2026-10-25 Gitee 仓库 Kaula 安全审查（8 张审查截图整理）
> 审查基线：master @ `ee9c5e2`（含前端严格化首轮）
> 整理日期：2026-10-27

---

## 0. 一句话总评

工程量和自省能力（CHANGELOG、FINDINGS.md 里主动记录未知 bug、方案文档、Z3 模型验证、内核级 C11 内存序）是真实且高于一般业余项目的；调度器/无锁队列这部分是读过的比较干净的实现。

但 README 的三句核心承诺——**编译期消除悬垂指针、编译期消除数据竞争、零运行时开销**——与当前实现差得比较远：

- 所有权分析默认不开、开了也只是"函数体内的显式 move 检查器"；
- 线程那一支是死代码；
- 类型检查的最大问题是"两套类型世界 + 判不出就放行"；
- 前端错误的 gate 只覆盖"入口文件 + error 级别 + 共享 collector"这一子集，import 文件、stdlib 降级、warning 降级三类错误会一路走到 codegen，并被 `default: return "0"` 这类兜底伪装成"编译成功"。

**若要在此之上写代码**：把它当"带自动作用域回收的 C 方言"用最稳——所有安全属性视为不成立，单一 TU、显式检查 std 签名文件可用、关键路径上不要依赖 `--sor` 之外的任何编译期保证。

---

## 1. 类型安全

### 1.1 真检查的部分（sema.go）

- 未定义名、非可调用被调用、本文件函数实参个数；
- 字面量 `/0`；
- 裸标识符的可空状态（路径敏感）；
- 常量下标越界（`sema.go:4499`）；
- `spend` 穷尽性是硬错误；
- `extern` 签名。

### 1.2 核心结构缺陷：没有表达式类型表

sema 自己推一遍类型（`inferExpressionType`），推完就丢，只把 `auto` 变量写回 `stmt.Type`；codegen 在 `codegen.go:1880` 另起一张符号表、用另一套 6 值域（`d/f/s/cstr/obj/u`）再推一遍。中间只靠 AST 传话，`main.go` 没有任何类型信息传递。

于是出现系统性分歧：

| 位置 | 问题 |
|---|---|
| `sema.go:5196-5230` `typesAssignable` | 任一侧为 `""` / `auto` → 放行；整数家族两两互容（`i64→u8` 静默截断+翻符号）；指针只看后缀不看 pointee |
| `sema.go:5182` `isTypeParameterName` | 1–3 个大写字母的类型名一律当泛型形参放行 → 名叫 `ID/OS/URL/Cfg ...` 的 struct 对任何实参免疫 |
| `sema.go:5084` | struct 字段类型永远不被推断；`structDecls` 全仓只用于"字段存在性"检查（`:4551`）→ `p.len = "text"`（`p.len` 是 `i32`）过了 sema，直接吐成 C 赋值 |
| `sema.go:4728` | 跨文件 import 的函数只有名字集合是 `bool`，没有签名（只扫本文件顶层函数）→ 参数个数/类型/adopt 边界全不查；更糟的是 codegen 却拿到了跨文件返回类型（`main.go:2161`）——生成器比检查器知道得多 |
| `sema.go:3985-3992` | `unwrap` 误用的检查块是空的（只有注释）；`adopt/unwrap` 在 codegen 里是纯恒等（`exprgen.go:3266`），两侧都不建模 |
| `exprgen.go:3206` | `as<T>(x)` 是无约束 C 强转，且 `srcType == ""` 先 bail——恰好是上面那些推不出类型的场景，等于强转不需要理由。`ptr↔int` 往返自由 |
| `analyzeInterfaceStatement` | 接口一致性检查不存在（只有 1 行登记），派发靠 C 函数指针强转（`typegen.go:953`）。泛型约束校验函数 `validateGenericInstantiation` 有定义无调用 |
| `funcgen.go:893` `hasReturnStatement` | 只要有一个分支 `return` 就认为够了 → 不注入 `return 0` → C 控制流走到函数尾，clang 只警告 → exit 0 + 垃圾返回值（`missing_return` 未实现） |
| `src/kaula_runtime.h:57-99` | 溢出保护只有 64 位宽（对 `i64/u64` 用 `__builtin_*_overflow`）；`u8 x=200; x=x+x` 先算 u64 再截断存回，截断处无检查。`--release` 整套保护消失（`codegen.go:1934`），且 TU 级 `-fwrapv`（`main.go:2956`）把有符号溢出变成静默回绕 |

### 1.3 数字类型口径三处打架（ABI 级）

- `int` 在 sema 眼里 = 64 位；
- `numeric_names.go` 里 `int` 是被禁拼写且 = 32 位；
- `GetTypeSize("int")` 返回 4；
- pkgblib 的 C 头按 `.kl` 默认是 LP64、实际编译在 LLP64 平台上会**静默改变所有 `long/size_t/uintptr_t` 宽度**（`stdlib/header_gen.go` + `builder.go` 的 `clangArgs` 里没有 `--target`）。

---

## 2. 内存安全

### 2.1 默认不做所有权检查

- `config.DefaultConfig()` 里 `SOR` 字段缺省即 `false`（`config.go:267-281`）；
- 整条流水线在 `main.go:1976 if cfg.SOR`（全文件只此两处门控）；
- 默认只跑一次轻量逃逸分析供分配路由用（`main.go:2030-2036`）。

**README 承诺的编译期保证，在未显式加 `--sor` 时不存在。**

### 2.2 `--sor` 打开的是一个不完整的仿射状态机

- AST→SOR 的桥 `sor/adapter.go:197-377` **不处理**：
  - `ReturnStatement`、`BlockStatement`、`Switch`、`Match`、lambda；
  - 字段/元素写入（LHS 只认裸 `Identifier`）；
  - 没有 `Send` 概念。
- `ErrCrossScopeYield` / `ErrCrossThreadYield` / `ErrThreadWriteOnReleased` 声明了但**全仓无产生点**；
- `sor/cfg.go` 的 `BuildCFG` **零调用** → 控制流被忽略，纯顺序执行；
- 对象查找是"全局拍平、按名首个匹配"（`ownership.go:123-134`），不同函数里同名变量互相冒领状态；
- `resource_registry.go:37` `registerBuiltinResources(){}` 是空函数 → 没有任何类型算资源，`ErrResourceLeak` 永不触发（锁不是资源）。

### 2.3 真正的"默认内存层"是 codegen 的词法活跃度猜测

- `stmtgen.go:3836` 在指针型局部变量的**最后一次词法使用之后**插 `survivor_free`；
- 判定"已死"的依据是**名字**，不是路径；
- 历史上每个修复都在给这个 oracle 打"按名豁免"补丁：
  - `git show 350e86b`：递归指针树 UAF，`n.left = l` 被写穿后又被 early free；需要深度 ≥ 8 才暴露；
  - `b9bce14`：递归 promote 复制链导致 RSS ∝ 深度、OOM at depth 25。

### 2.4 已核实的不对称：promote vs free 集合不一致

- `isPromotableAggregate`（`stmtgen.go:5202-5214`）与 `EnsureDeepPromoteCB`（`codegen.go:691-696`）**只认 `classTypes`**；
- 而 `EnsureDeepFreeCB`（`codegen.go:871-881`）同时认 `classTypes || structTypes`。

→ **struct 会被深释放但只做浅提升**，即带指针字段的 struct 跨作用域返回时，根进幸存段、子字段留在会被回卷的 TLAB。触发面就是"某个函数没被 `return_escape_scan` 认成链式构造器"（其 `default:` 直接 `markUnsafe`）。

### 2.5 运行期失败的形状是静默腐化，不是 abort

| 位置 | 问题 |
|---|---|
| `src/kmm_scoped_allocator_v4.c:1288-1292` | `kmm_v4_free` 是显式 no-op |
| `.h:1483-1491` | slab 路径的 `kmm_v4_survivor_free` 只按地址算槽位、不查占用位图，重复释放会把在用的槽再压进垃圾表，flush 时把位图清零并双减计数 → **同一槽发两次** |
| — | `>1MB` 的 paged 才有双 magic + alive CAS 校验；V4 无 canary/毒化（带 canary 的 V3 被 `toolkit_build.py:135` 排除在构建外） |
| — | 作用域栈溢出/欠载会 `abort()`；池耗尽返回 `NULL` 而生成码不检查 → SIGSEGV |
| `.c:1310-1322` | `kmm_v4_realloc` 对池内指针按"到池尾"复制，**越界读邻居** |
| `.c:1404-1413` | 对外部指针直接换块且**不复制** → 静默丢数据 |
| `stdlib.json:672` | `std.memory.kmm_v4_reset` 被登记进 stdlib，安全代码可调用，**无守卫** |

### 2.6 panic / longjmp 不还原 KMM 作用域栈

- `std.panic` 是 `setjmp/longjmp`（`std/panic/panic.c:76`）；
- 不还原 KMM 作用域栈（只有 conc worker 会调 `kmm_v4_scope_depth_restore`）；
- → `longjmp` 穿过一个开着 scope 帧的函数后，下一次无关的 `scope_pop` 会把 bump 指针回卷到被中止活动的水位以下，**回收活对象**。

### 2.7 测试侧自欺

- `test/stdlib_memory_safety.kl` 自称"RED 阶段"，判据是"没崩就算过"；
- 它的 `test_kmm_double_free` 之所以通过，只是因为 `free` 是空函数；
- 全仓只有一个 `.kl` 文件提到 `yield/release/extract` 且无人使用；
- `test/sor_model_verify.py` 是用 Z3 证明那个抽象状态机，**不是实现**。

---

## 3. 并发安全

### 3.1 两套并存的原语

- `std.concurrent` / `std.parallel`（裸 pthread 封装）；
- `conc fn` + `spawn` / `await`（绿任务 + Chase-Lev 工作窃取 + EBR）。

### 3.2 "SOR 消除数据竞争"在代码里没有对应实现

- 线程诊断项无产生点（见 §2.2）；
- `grep -i sendable` 在 `compiler/` 和 `std/` 下**零命中** → 把一个别名/非转移值发到另一个线程，既不报错也不警告；
- 唯一真实的跨线程补偿是防 UAF 的名字白名单 `threadSpawnFns = {thread_create, thread_pool_add_task}`（`stmtgen.go:1075-1078`），而它**不含 `spawn` 本身**。

### 3.3 三个可静态确认的洞

1. **spawn 实参不 promote**：`spawn` 的实参按值写进 Frame 却不做 promote（`conc/frame.go:785`），逃逸分析因为看不见 spawn（`escape.go:74-119`）把对象留在会被父 worker `scope_pop` 回卷的 TLAB → 子任务读已回收内存。
2. **String SSO 共享 payload**：`String`（`types.h` 是带 inline SSO 的 `{len, ptr}` 薄壳）拷贝后 `len>15` 时两个 worker 共享同一 payload，而这条路径连 `handleThreadSpawnEscape` 都不走。
3. **h.await 不被识别为挂起点**：`analysis.go:410` 只匹配 `f("...")` 字符串守卫，插桩表登记的 `handle_await` 是 AST 里不存在的节点类型 → 挂起时不保存 `loop_bound`、不弹 KMM 作用域，且 await 走 worker 阻塞分支还会改 `w->current_task` 这个跨 worker 共享字段。

### 3.4 调度器本身的内存序是合格的

全 C11 `<stdatomic.h>`：

- deque 的 push 用 release-on-bottom；
- pop 用 relaxed 写 + `fence(seq_cst)` + relaxed top 读 + seq_cst CAS（DRYPOP 正确）；
- `steal_batch` 改成逐槽 CAS、"至少留 1 个"；
- MPSC 是 Vyukov 带序号；
- park/wake 不会丢唤醒（计数式 event + 1ms 自愈）。

### 3.5 缺陷都在锁结构之外

- `w->current_task` / `worker_count` / `bucket->allocated` / `g_completion_consumer_active` 等**非原子跨线程读写**；
- 终态转换用了 `compare_exchange_weak`（`task.c:267`）——x86 上无害，**ARM64 上虚假失败会被当成"别人已经完成了它"而永久丢唤醒**；
- EBR 的 retire 列表满时走"强制回收"（已核实 `epoch.c:193-233`：确实不查 `min_active`，但有一层 `g_ext_pins > 0` 则宁可泄漏的保护，所以严重度低于"随时 UAF"）；
- Block Lane 线程不在 epoch 域内且 `join` 是 `sleep(30ms)`。

### 3.6 仓库自己记录的已知并发 bug

- `test/bench/FINDINGS.md:443` **F11**：`conc=10000` 约 **2.6% 概率死锁**，成因未定（已排除 4 条路径）；
- `:557` **F13**：双形态 spawn 在 Windows 上**确定性堆损坏** `0xC0000379`。

### 3.7 CI 并发测试缺工具

- `.github/workflows/test.yml:90` 只跑 `python3 test/concurrency/run_tests.py`；
- 不带 `--asan`（仅 ASan 可选，全仓无 TSan/helgrind/valgrind）。

### 3.8 std.atomic 太薄

- 只有 32 位 `volatile int*`、固定 `seq_cst`、无 64 位/指针原子、无排序选择；
- 互斥量是手动 `unlock`，无 RAII、无解锁归属检查；
- `resource_registry` 空意味着锁不是资源、泄漏检测永不触发。

---

## 4. 前端错误会不会漏到 codegen —— 会，且有明确主干道

### 4.1 好消息

`main.go:2041-2073` 的 fail-fast gate 是真的：

- 位于 sema 之后、`cg.Generate` 之前；
- 命中即 `os.Exit(1)`，不产 C、不写缓存、不落二进制；
- 缓存命中路径仍先跑完整前端，所以前端错误不会被缓存掩盖。

这两点比多数原型编译器好。

### 4.2 坏消息（每条都读过源码）

#### 4.2.1 import 的本地 .kl 文件，sema 诊断整个丢弃

- `main.go:678` `localAnalyzer := sema.NewSemanticAnalyzer()` 在 `sema.go:211-213` 自建私有 collector；
- `:695` `Analyze(localProgram)` 之后，`main.go` 对它的全部 10 处引用**无一读取其错误**（合并进主 collector 的只有该文件的 parse 错误，`main.go:639-650`）；
- 随后 `:733` `localCG.Generate(localProgram)` **无条件生成 C 并注入主输出**。

这是"合法外观的错误二进制"最直接通道：被导入文件里 `i64→i32` 赋值（整数家族互容）一路绿灯，exit 0，静默截断。

#### 4.2.2 stdlib.json 缺失/加载失败 = 纯 WARN，std 调用检查全熄火

- `main.go:1895` 用 `stdlibConfig, _ =` 吞掉 err；
- `sema.go:3906` 的整条 std 链检查以 `sa.stdlibConfig != nil` 为前置；
- `codegen.go:1871-1874` 只 `fprintf` 不记错。

后果：`std.string.` 任何拼错的名字（任意参数）不被拦，连按签名转参（`string→char*`）都跳过；实测这种降级下仍会打 `Status: Failed, errors 0`——**错误计数为 0 的失败**。

#### 4.2.3 最新一次提交把一条 error 降回 warning，并留下零诊断窗口

- HEAD `ee9c5e2` 把 `0f5a159` 刚立的 `redeclaration` 改回 `AddSemanticWarning`（`sema.go:1849-1855`）；
- 且条件是 `existing.Line != stmt.Pos.Line` → 同一行两次声明连警告都没有；
- 兄弟 `if` 块重复声明是 warning（gate 放行），而 C 侧合法 → exit 0，符号表不分块与 C 分块的语义歧义被静默掉；
- `CHANGELOG.md:24` 与 `test/errors/README.md`（自称"期望每个文件都在前端报错 exit!=0"、p7 即 redecl）已与代码不符；
- 这些用例**不被 `scripts/run_tests.py` 收集**，负向测试自动化为零。

#### 4.2.4 SOR 错误不进 gate

- 所有权诊断只 `print`（`main.go:2000-2008`），不进 collector；
- → gate 放行 → codegen 照跑、C 照进缓存；
- 直到 `main.go:2289/2363` 才计入错误并跳过 clang。

**违反其自家方案 `docs/frontend-strictness-plan.md:117` 的"sema 报错 codegen 不得跑"。**

#### 4.2.5 codegen 的静默兜底正好承接上述遗漏

| 位置 | 行为 |
|---|---|
| `stmtgen.go:163` | `default: return ""`（未支持语句整条蒸发，零诊断） |
| `exprgen.go:327` | `default: return "0"`（未支持表达式变成字面量 0） |
| `main.go:1017-1021` | promote-skip 注释自称"非致命（不阻断编译）"——跳过 promote = 运行期悬垂 |
| `analyzeExpression` case 列表 | `*ast.StructLiteral` 根本不在里面 → 结构体字面量里的未定义变量从不被分析 |
| — | 非 void 函数缺 return 路径只会被 clang 警告 → exit 0 + UB |
| — | `literal_range`（`u8 b=300`）是 warning → exit 0 打印 44 |

#### 4.2.6 缓存降级组合

- `cache.go:232` 只在 link 指纹非空时校验——而"std/pkglib 根找不到"（即 §4.2.2 的降级场景）恰好让指纹为空 → **直接跳过校验、按 mtime 复用旧 exe**，把"环境坏了"伪装成"编译成功"；
- 编译失败时不删除上一版输出二进制，`kaulac x.kl && ./x` 类脚本在 exit 1 时仍可能执行到旧产物。

#### 4.2.7 旁路 driver

`internal/compiler/compiler.go:54-77`：sema 有错只填字符串、codegen 照跑、返回 `err == nil`（当前无调用方，是 LSP 埋雷）。

---

## 5. 性能与基准（已核实）

### 5.1 已修的问题

| 问题 | 状态 | commit |
|---|---|---|
| `-cc gcc` PCH 硬失败 | ✅ 已修 | `a9f5d1a` |
| 没有真 CLBG 题 | ✅ 已补 | `5a9d1e9` + `18b125a` |
| `/usr/bin/time` 缺失致 RSS 读不到 | ✅ 已修 | `394e110`（`/proc VmHWM` 回退） |
| 递归指针树 UAF（depth ≥ 8 必崩） | ✅ 已修 | `350e86b` |
| survivor 段只增不减 | 🟡 部分 | — |

### 5.2 实测数据（本机 2 核，N=21）

```
[round 1] c=12.940s / 257MB   go=14.890s / 213MB
          kaula=1.066s / 65MB
[verify] binarytrees: PASS — 三边 stdout 逐字节一致
```

Kaula arena 版比 C 快 **12×**、比 Go 快 **14×**、内存少 **4×**（独立复现，不是纸面数字）。

### 5.3 但 21× 差距原封不动

worktree 把修复前（`8bbff76`）/后（`ee9c5e2`）两个编译器在同一台机器上交措各跑 5 轮，同一份 `clbg_shape.kl`（M=15，四臂 checksum 全 = 6018305123121）：

| 臂 | wall | ns/节点 | peak RSS |
|---|---|---|---|
| kaula 修复前 | 2.668 s | 414 | 509.6 MB |
| kaula 修复后 | 2.810 s | 436 | 509.1 MB |
| go | 0.144 s | 22.3 | 7.4 MB |
| c malloc+free | 0.113 s | 17.5 | 5.2 MB |

生成码直接对比：

```
旧 kaulac: promote_p=4  survivor_free=11  alloc_global=0  deepfree=0
新 kaulac: promote_p=4  survivor_free=12  alloc_global=2  deepfree=4 ← 新机制装了，但没走我们这题
他们 ptr:  promote_p=0 ← 快速路径命中
```

### 5.4 定位到具体字段：P3 返回链预扫描的误拒

`return_escape_scan.go:256`：

> 所有字段写穿的目标必须是链值，源必须是链值或 null

规则不区分字段类型——所以 `n.item = item`（往 `i64` 域写一个标量）也会导致整个构造器被拒。逐个隔离验证：

| 变体 | 相对 V3 的唯一改动 | promote_p | 时间 | RSS |
|---|---|---|---|---|
| V3 | 照抄他们构造器（单参数、叶先返回、父最后分配） | 4 | 2.52 s | 509 MB |
| V1 | `item` 源改字面量 `7` | 4 | — | — |
| V5 | 删掉 `item: i64` 这个字段 | 2 | 0.14 s | 29.8 MB |

**一个 `i64` 数据字段 = 18× 时间 + 17× 内存。** 也就是说：他们的指针版能进快速路径，是因为 `struct Node { left; right; }` 不带数据；真实程序（包括 CLBG 原版带 `item` 的节点）一写标量域就掉出去。

### 5.5 口径问题

他们的 `itemCheck` 是 `1 + l + r`，而官方是 `item + 2·check(left) + check(right)`——那个 2× 权重和 `item` 项都没有。所以 `binary_trees_ptr.kl` 是**去掉了载荷的简化版**（`REPORT.md §3` 写"arena 与 ptr 形态 checksum 完全一致"是指它们两者彼此一致，不是与官方版一致），而 fast path 恰恰是靠这个简化才命中的。

### 5.6 develop 分支

`develop` 领先 master 20 个提交，其中 `70c4b18` `fix(kmm): 方案A落地——slab密档覆盖大对象+页式分...`。

---

## 6. 修复优先级建议

| 优先级 | 项 | 理由 |
|---|---|---|
| **P0** | import 文件 sema 错误并入主 collector（§4.2.1） | "合法外观的错误二进制"最直接通道，改动小 |
| **P0** | SOR 错误并入 collector 进 gate（§4.2.4） | 违反自家方案，一行改 |
| **P0** | codegen `default: return ""` / `return "0"` 改为硬错误（§4.2.5） | 未支持语句/表达式静默蒸发是 UB 温床 |
| **P0** | 缓存指纹为空时拒绝复用（§4.2.6） | "环境坏了"伪装成"编译成功" |
| **P1** | 数字类型口径统一（§1.3） | ABI 级，跨平台静默错 |
| **P1** | spawn 实参 promote + h.await 挂起点识别（§3.3） | 并发 UAF 主干道 |
| **P1** | `missing_return` 实现 + 非 void 函数缺 return 硬错（§1.2） | exit 0 + 垃圾返回值 |
| **P1** | `kmm_v4_realloc` 越界读邻居修掉（§2.5） | 静默腐化 |
| **P2** | 建立集中 `types.Env`（sema/codegen/sor 共用） | 根治"两套类型世界" |
| **P2** | develop 分支 20 个提交评估合并 | 可能有未吸收的 KMM 修复 |
| **P2** | CI 加 TSan/ASan（§3.7） | F11/F13 再现工具 |
| **P3** | `kmm_v4_free` no-op 改 abort-on-double-free | 调试友好 |
| **P3** | std.panic longjmp 还原 KMM 栈（§2.6） | 边缘但严重 |
