# Changelog

本项目遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 与语义化版本。

> 说明：v1.1 系列的首个发布版本号为 **v1.1.1**。仓库历史中不存在、也不会存在 `v1.1.0`
> （v1.1 切版基线归档于 `scripts/version_history.json`，不打 tag）。后续 v1.1 修补按
> v1.1.N 递增。CONC = Continuation-Oriented Native Co-runtime；KMM 当前为 v4。

## [Unreleased]

### Added
- **CONC 并发内核端到端落地**：work-stealing 调度器 + 预算机制、IoDriver + suspend/resume、
  阻塞车道 + Worker 撤离、`conc fn` + `spawn` 语法、内核重写与 lowering、IoDriver lowering 端到端。
- **并发语言接缝**：`for..in(range)` 循环体内挂起、`#[hint(blocking)]` 阻塞车道入口。
- **`:=` 错误传播（类 Rust `?`）**：语法 + 泛型返回类型与 mismatch 检查。
- **高级属性**：`#[test]`、`#[derive(Debug,Clone,Eq)]`、`#[command]` + `#[arg]`；
  `assert(cond)` 内置让 `#[test]` 可真实失败。
- **托管自递归尾调用优化（TCO）**：自递归深度 63 → 无限制。
- **String SSO**（≤15 字节零堆分配）；StringBuilder 暴露。
- **空安全 / 静态分析增强**：路径敏感空安全快照恢复、多项空安全优化与静态分析能力。
- **新 stdlib 模块**：algebra / algorithm / db / ipaddress / obj / panic / hash / limit /
  serialize 等（57 个物理 std/ 目录；stdlib.json 共登记 67 个模块键：62 个 `std.*` + 5 个
  `freestanding.*`），1500+ 函数。
- **跨运行时基准框架**：CONC vs Go GMP vs Rust Tokio（含 F1–F13 实测发现）。
- **公开 ABI 预留字段**：`Spendable`、`FastAllocator`、`KaulaRuntimeConfig`、`KaulaMetrics`、
  `KaulaResumeResult`、`KaulaHandle`、`KaulaTask` 末尾新增 `uint64_t reserved[4]`，后续加字段不破坏栈分配尺寸。

### Performance
- **P0**：per-worker Task 复用池 + ws_deque 弱内存变体 + 协作等待自适应退避；
  owner 独占后 12 条原子 store 改普通读写。
- **P1**：spawn 移除冗余清零、首跑直通 enter 省 switch+守卫、Frame 按需瘦身（160B→48B）、
  守卫槽深度自适应、常量实参 + 无 suspend 的 spawn 完全内联、双形态顺序调用 shim 免全帧 memset。
- **P2**：epoch 推进改 Worker 本地计数；批量 work-stealing；IO 完成批量路由注入 inject queue。
- **P3 测量基建**：ab 补 p99/tps 离散度 + metrics 重复采样；新增 p99.9 与峰值内存 VmHWM。
- **调度/定时延迟**：抬系统时钟分辨率、注入后唤醒 Worker、epoll 后端改 timerfd、
  混合场景 timer O(1) + submit 扩容 + epoch 回收限流。
- **编译速度**：`-pipe` + lld 检测；auto-detect CPU 核并行编译。
- **Frame 热/冷字段分区（缓存行对齐）**：stateful Frame 的字段按访问热度三级排序 ——
  rank 0 状态机管理字段（`pc`/`sub[]`/`branch[]`，每次 resume 与每次挂起都读写）、
  rank 1 迭代与数据字段（`loop_i/end/step`、参数、跨 suspend 局部变量）、
  rank 2 冷字段（`result`）。同 rank 内仍按对齐降序消除填充，`sizeof` 不变。
  实测 `K_worker_Frame`：旧布局 `pc@72 / sub[1]@74 / branch[1]@76` 落在 64B 边界之后，
  每轮循环必跨两条缓存行且**每次 resume 必读的 pc/sub 恰在第二行**；
  新布局把管理字段与 `loop_*` 全部收进第 0 行。

### Fixed
- **构建阻断：Linux/clang 下运行时编不出来（2 处）**：
  - `kaula_conc_epoch.c`：`g_deferred_lock` 声明成 `_Atomic(uint32_t)` 却用
    `__atomic_*` 内置访问，clang 判「atomic 操作的地址必须指向整型/指针」直接报错。
    改普通 `uint32_t`（与所用内置保持一致），语义不变。
  - `kaula_conc_worker.c`：v1.2.1 把 `do_io` 下沉到 Worker 后漏了
    `kaula_conc_io_backend.h`，`kaula_iodriver_do_io` 成隐式声明（C99 起非法）。
- **spawn 常量特化在 Linux 上确定性编译失败**：`specializeSpawnConsts` 把
  特化版形参列表清空（`clone.Params = nil`），但 spawn 调用点仍按原签名传实参、
  emitter 也仍从 AST 的 `Params` 构造 —— shim 定义成 `(void)`、
  Frame 少了参数字段，clang 报 `too many arguments to function call, expected 0, have 1`。
  只要 `spawn` 的实参全是字面量就必现（`test_for_range_suspend.kl` 的
  `spawn loop_nested(5)`）。改为保留形参列表、只把常量代入函数体。
- **coercion shim 前置声明的空参写法**：`f()` 在 C 里是「参数未指定」而非「无参」，
  声明写 `()` 会让实参个数错误推迟到定义处才报。空参改写成 `(void)`。
- **链接 / 公开 API**：补齐 `tree_create/destroy/set_root/add_node/get_node`、
  `prefix_system_create/destroy/enter/leave` 的 thin wrapper，转发到既有内部实现；
  修复 tree/prefix 递归释放泄漏、NULL 检查、`strtok` 改 `strtok_r`。
- **跨归档 duplicate symbol**：`std/container` 的 `priority_create/destroy` 改内部 static，
  公开实现保留在 `src/queue.c`。
- **公开头 C++ 兼容**：所有安装公开头加 `extern "C"` 包裹。
- **B4**：`kaula_runtime_apply_defaults(NULL)` 不再 `memset(NULL)`；const 配置拷局部再填默认值。
- **B5**：`kaula_task_fail` 改与 `complete` 对称的 CAS，杜绝 complete+fail 并发导致
  `active_task_count` 下溢、背压误触发、await 永久挂起。
- **B6**：`fast_alloc`/`fast_calloc` 失败判空并回滚返回 NULL。
- **B7/H4**：`Task.func` 签名统一为 `void(*)(void*)`，删除返回类型不同的函数指针强转；
  `batch_add` priority clamp；修空队列误判；补 NULL 检查。
- **H13**：IoDriver 定时器 `malloc` 失败改为标记任务 `-ENOMEM` 并恢复 READY，不再静默挂起。
- **Task 池高水位收缩**：非 worker 线程 spawn、任务由 worker 完成后归还 worker 独占池，
  原实现池只增不减导致长跑 RSS 线性上涨直至 OOM；归还时超水位（默认 256/worker，
  环境变量 `KAULA_TASK_POOL_HIGH_WATER` 可配）则彻底回收（销毁 `done_event` + survivor_free）。
- **投递丢任务竞态（F1）**：注入队列 pop 竞态移除 relaxed store；外部线程不写 Worker 本地队列。
- **spawn 先置 READY 再投递**：否则覆盖 DONE 致 await 永久挂死；shutdown 先等监控线程退出。
- **Block Lane 抢名额 CAS 重试到成功**；Handle 复制路径传播类型登记，修漏 release 与 await 悬垂读。
- **POSIX/Linux 首次实编实跑**：`_GNU_SOURCE` 暴露 `MAP_ANONYMOUS`、栈尺寸链接标志平台自适应等。
- **KMM 内存安全**：paged free 小块不入池避免 Treiber 节点越界写；别名感知 liveness 提前释放，
  修 use-after-free 与 double-free。
- **`kaulac fmt` 静默破坏源文件（6 类，共12 处修复）**：
  - **未覆盖 AST 节点被替换成注释占位**：`formatExpression` 的 default 分支对
    `SpawnExpression`/`AwaitExpression`/数组字面量/条件表达式/括号表达式等一律输出
    `/* unknown expression: %T */`。首次格式化把 `Handle h = spawn f()` 变成注释、
    抹掉并发语义；二次格式化再把注释当 token 重解析，散成乱码，源码彻底不可用。
    已补齐7 个表达式分支，并把 `await` 按仓库真实语法输出为**后缀**形式
    `handle.await`（`parseAwaitExpressionIterative` 只在前缀位置构造节点，
    但现存代码全部写作后缀，输出前缀会在 `hs[i].await` 这类下标句柄上丢语句）。
    全仓 `/* unknown expression` 占位数 **119 → 0**。
  - **lexer 遇到无法识别的字节死循环**：`Next()` 的 default 分支报错后 `continue`
    却不推进 `l.pos`，于是在同一字节上无限吐空 token。孤立高位字节（`\x97`）即可触发，
    1 行输入产出 20000 行空白。已改为报错后推进位置。
  - **UTF-8 多字节标识符被拦腰截断**：`scanIdentifier`用
    `unicode.IsLetter(rune(byte))` 逐字节判断，而 `rune(0xE5)` 是字母 'å'、
    `rune(0x9D)` 是控制字符，于是中文标识符在多字节序列中间被切断。
    改为 `utf8.DecodeRuneInString` 按码点解码；`Next()` 中恒真条件
    `char >= 0x80 && (... || char >= 0x80)`（永真，导致任意高位字节都被当标识符首字符）
    一并修正。
  - **块注释 `/* */` 完全不被识别**：`skipComment()` 只认 `//` 与 `#`，
    块注释被拆成「除号 + 星号 + 内容」重新解析，含中文时连锁失败并触发上面的死循环。
    已支持块注释（跨行、含中文、未闭合时报错定位到开启行）。
  - **一元运算符丢括号（语义静默改变）**：`formatUnaryExpression` 无条件输出
    `op + 操作数`，`!(5 < 3)` 被写成 `!5 < 3`，重新解析变成 `(!5) < 3`——
    语义从「5 不小于 3」(false) 变成「-5 小于 3」(true)，**条件恒真**，
    `test/stress_numeric.kl` 的断言全部失效但测试框架报绿。
    已按「操作数是否原子表达式」补括号，`-(a+b)`→`-(a + b)`、`!ready()` 保持原样。
  - **泛型类型参数被写成方括号**：`fn identity<T>` 被格式化成 `fn identity[T]`，
    `[T]` 会被重新解析成「表达式下标」，泛型语义丢失、编译报 4 个 Semantic Error。
    已统一用 `<T>`（`writeTypeParams`）。
- **`kaulac fmt` 护栏补全（fail-fast，不做静默降级）**：
  - **`--write` 静默不落盘**：fmt 参数预扫描按「下一个 token 是否 `-` 开头」划边界，
    排在文件名之后的旗标被整段漏掉——`kaulac fmt file x.kl --write` 与
    `kaulac fmt file --write x.kl` **两种写法都只打印不落盘**，且无任何提示。
    改为按 fmt 子命令关键字（`file`/`stdin`/`check`）划边界。
  - **解析错误仍被格式化**：`kaulafmt` 迁移到 `kaulac fmt` 时漏抄了
    `if p.HasErrors() { exit(1) }`，语法错误文件仍走完格式化并输出——
    而 parser 的错误恢复会产出畸形 AST。已补回该护栏，
    `fmt file`/`fmt stdin`/`fmt check` 三条路径全部覆盖。
  - **import 可解析性检查被误当语法检查**：该检查依赖**运行环境**
    （工作目录、stdlib 清单、`kaula.json`），与源码语法无关。
    formatter 硬编码 `SetFile("<fmt>")` 导致 `filepath.Dir` = `.`，
    换个目录执行 fmt 就会把合法import 判成「导入的模块不存在」并拒绝格式化。
    parser 新增 `SetSkipImportCheck`，formatter 侧显式跳过。
  - **词法错误收集器与语法错误收集器分离**：lexer 与 parser 各持一个独立收集器，
    「未闭合块注释」「无法识别的字节」只进 lexer 侧。已一并采集。
  - **`fmt check` 永久误报**：`--write` 落盘时写 `formatted+"\n"`，
    而 check拿 `formatted` 比对，于是经`--write` 处理过的文件永远报「未格式化」。
    比对改为同一规范形。
- **格式化器丢失语言修饰符与错误传播语义（3 处）**：
  - `conc fn` 的 `conc`、`pub fn` 的 `pub` 全部丢失，并发函数与导出可见性被静默改写；
  - `export fn` 被输出成 `pub export fn`（parser 对 `export fn` 同时置
    `IsExported` 与 `IsPublic`，两者都输出就得到 parser 不接受的组合，
    二次解析失败并把函数体降级成垃圾）；
  - 泛型函数被误加 `export`（`stmt.Generic` 的含义是「是否泛型函数」，
    早期实现错用它当导出标志）；
  - `x := f()` 的错误传播赋值被写成 `auto x = ...`（parser 对 `:=` 同时置
    `IsAuto` 与 `IsTryAssign`），`:=` 语义与类型推断来源一并丢失。
- **新增回归测试（4 个文件、40+ 用例）**：`lexer/comment_test.go`（注释三形态、
  UTF-8 截断、死循环）、`formatter/new_syntax_test.go`（spawn/await/修饰符/
  `:=`/一元括号/幂等）、`formatter/guard_test.go`（解析错误、unknown 护栏、
  输出爆炸上限）、`kaulac/fmtargs_test.go`（`--write` 参数边界）。
- **验证结论**：全仓 114 个 `.kl` 文件格式化 114/114 成功、unknown 占位 0、
  解析错误 0；与基线逐文件A/B 对照中 **19 个此前被破坏的文件（15 个concurrency +
  `stress_concurrent_spawn` / `stress_numeric` / `try_assign`）由 BREAK 转OK，
  零反向回归**；`run_tests.py --quick` 基线与修复版均为 76/90 且失败集合逐项一致
  （`error_handling`/`probe_err`/`probe_panic`/`stdlib_async_safety`/
  `stdlib_parallel_safety`/`stress_option`/`stress_panic`，均为既存失败）。

### Changed
- **格式化入口统一为 `kaulac fmt`，移除独立 `kaulafmt` 二进制（BREAKING）**：
  `compiler/cmd/kaulafmt/` 与 `kaulac fmt` 是同一份格式化逻辑的两份拷贝
  （除构造函数名与少数 API 包装外逐行一致），长期存在改动不同步的漂移风险。
  现删除重复实现，`kaulac fmt` 成为唯一入口。
  迁移：`kaulafmt -w f.kl` → `kaulac fmt file f.kl --write`；
  `kaulafmt f.kl` → `kaulac fmt file f.kl`；
  新增 `kaulac fmt check f.kl`（校验）与 `kaulac fmt stdin`（stdin 管道）。
  已在仓库全部 99 个可解析 `.kl` 文件上验证：新旧入口输出**逐字节一致**，零行为变更。
- **CONC 术语正式定义与统一**：CONC = Continuation-Oriented Native Co-runtime。
- **文档重构**：`concurrency-kernel.md` 改名 `core-runtime.md`，重写为"并发内核与 KMM V4"。
- `for` 语句统一为 `for x in range(...)`，移除 C 风格三段式 for。
- 删除不可调用的 `std.prefix` 模块；`c::` 命名空间改编译器自动推断。
- 公开 `--version` 不再输出 codename 后缀（codename 固定为 `kaula`，不进入版本串）。
- CI：Linux job 由 advisory 转 blocking，接入 `go test ./...`、C 内核 27 用例、
  freestanding L1–L6；拆分重复 `--target`；修复 `--install-dir` 漏更新 `hash_dir`。

### Deprecated
- `fast_free`：`fast_allocator` 为 bump 分配器不支持逐个释放，`fast_free` 当前为 no-op，
  仅保留符号兼容；内存经 `fast_allocator_reset()` 批量回收。

### Known issues
- F11：`conc=10000` 下仍有低频死锁，成因未定。
- F13：kl 双形态 spawn 在 Windows 上确定性堆损坏。
- `struct KaulaTask` 完整定义仍暴露于安装头，已标注为内部/非稳定 ABI；真正的不透明化推迟到 v1.2。
- struct literal 语法、PCH 加速在 v1.1 暂为关闭状态（见源码注释），后续版本恢复或改为明确报错。
- LSan 在短生命周期 C 内核测试二进制退出时报 128–320B pthread 描述符小泄漏：测试进程未调用
  `kaula_runtime_shutdown` 即退出，属进程退出 teardown 的良性残留（OS 回收全部资源），非运行期泄漏。
