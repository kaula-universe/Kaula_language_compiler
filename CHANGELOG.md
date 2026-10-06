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

### Changed
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
