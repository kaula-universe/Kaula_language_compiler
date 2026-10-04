/*
 * Kaula 并发内核（CONC）· 任务与运行时配置
 *
 * 这里是并发内核的类型定义层。设计对应文档 §2、§3、§12。
 */

#ifndef KAULA_CONC_TASK_H
#define KAULA_CONC_TASK_H

#include <stdint.h>
#include <stddef.h>
#include <stdbool.h>
#include <string.h>
#include <stdatomic.h>

#include "kaula_conc_platform.h"
#include "kaula_conc_ws_deque.h"
#include "kaula_conc_mpsc_queue.h"

#ifdef __cplusplus
extern "C" {
#endif

/* ========== 编译期硬上限（I3：一切有界） ========== */

#define KAULA_MAX_WORKERS            256
#define KAULA_MAX_TASKS              1000000   /* 全局活跃任务硬上限 */
#define KAULA_BLOCK_LANE_MAX_THREADS 64
#define KAULA_BLOCK_LANE_MIN_THREADS 2
#define KAULA_BLOCK_LANE_QUEUE_CAP   4096
#define KAULA_INJECT_QUEUE_CAPACITY  131072
#define KAULA_INJECT_QUEUE_MASK      (KAULA_INJECT_QUEUE_CAPACITY - 1)

/* 完成队列分片数（文档 §8.1，必须为 2 的幂） */
#define KAULA_COMPLETION_SHARDS      16
#define KAULA_COMPLETION_CAPACITY    1024

/* epoch 退休对象上限（文档 §10.3） */
#define KAULA_EPOCH_MAX_RETIRED      65536
/* try_reclaim 最小回收阈值（摊销频率限制，见 kaula_conc_epoch.c 说明） */
#define KAULA_EPOCH_RECLAIM_THRESHOLD 256

/*
 * 每个 Worker Task 复用池的高水位。
 *
 * 背景：非 Worker 线程（main）spawn 的任务由 Worker 完成，完成后 Task 被
 * 归还到该 Worker 独占的 task_pool；而 main 永不复用 Worker 池、Worker 也
 * 很少反向 spawn —— 池只增不减，长跑下 RSS 线性上涨直至 OOM。
 *
 * 对策：归还时若池长已达高水位，超出的 Task 不再入池，改为彻底回收
 * （销毁同步原语 + survivor_free）。默认 256/Worker，可用环境变量
 * KAULA_TASK_POOL_HIGH_WATER 覆盖（init 时读一次）。
 */
#define KAULA_TASK_POOL_HIGH_WATER_DEFAULT 256

/* 默认预算与停滞阈值（文档 §5.3、§7.4） */
#define KAULA_DEFAULT_BUDGET_QUANTUM 256
#define KAULA_DEFAULT_STALL_NS       (10 * 1000000ULL)   /* 10ms */
#define KAULA_BUDGET_MIN_QUANTUM     16
#define KAULA_BUDGET_MAX_QUANTUM     4096

/* IO 复用批量 */
#define KAULA_IO_BATCH_SIZE          256
#define KAULA_IODRIVER_MAX_PENDING   65536
/* 驱动线程每轮阻塞上限（兜底：即使无事件也要定期复查定时器与提交队列） */
#define KAULA_IO_BATCH_WAIT_NS       50000000ULL  /* 50ms */

/* ========== 错误码 ========== */

/*
 * spawn 的失败原因。文档 §4.3 要求：一切满载都返回 WouldBlock，
 * 而不是崩溃或无界增长。
 */
typedef enum {
    KAULA_OK                = 0,
    KAULA_ERR_WOULD_BLOCK   = 1,   /* 达到硬上限，调用方应稍后重试 */
    KAULA_ERR_OOM           = 2,   /* 帧或 Task 分配失败 */
    KAULA_ERR_SHUTDOWN      = 3,   /* 运行时不接受新任务 */
    KAULA_ERR_CANCELLED     = 4,   /* 任务已被取消 */
    KAULA_ERR_TIMEOUT       = 5,   /* await 超时 */
    KAULA_ERR_IO            = 6,   /* IO 投递/注册失败（非内存原因） */
} KaulaError;

/* ========== 任务状态 ========== */

/*
 * 状态机（文档 §3.1）：
 *
 *   CREATED ──push──▶ READY ──pop──▶ RUNNING ──┬──▶ DONE
 *                                            ├──▶ SUSPENDED ──wake──▶ READY
 *                                            ├──▶ BLOCKED ──unblock──▶ READY
 *                                            └──▶ BUDGET_DONE ──requeue──▶ READY
 */
typedef enum {
    KAULA_TASK_CREATED   = 0,
    KAULA_TASK_READY     = 1,
    KAULA_TASK_RUNNING   = 2,
    KAULA_TASK_SUSPENDED = 3,
    KAULA_TASK_BLOCKED   = 4,
    KAULA_TASK_DONE      = 5,
} KaulaTaskState;

/* resume 函数签名（编译器 lowering 产出，文档 §9.1） */
typedef struct KaulaResumeResult KaulaResumeResult;

typedef enum {
    KAULA_RESUME_DONE             = 0,
    KAULA_RESUME_SUSPEND          = 1,
    KAULA_RESUME_BUDGET_EXHAUSTED = 2,
    KAULA_RESUME_BLOCKED          = 3,
} KaulaResumeStatus;

typedef struct KaulaTask KaulaTask;
typedef struct KaulaWorker KaulaWorker;

typedef KaulaResumeResult (*KaulaResumeFn)(void* frame);

/* ========== IO 请求（文档 §6.2） ========== */

/*
 * IO 请求类型。语义是「fd」而不是「socket」。
 *
 * 读写一律走 read/write：这既覆盖普通文件与 pipe，也在 POSIX 上覆盖
 * socket（socket 就是 fd）。内核因此不需要 socket 头、不需要 winsock、
 * 不需要 ws2_32 —— 见 kaula_conc_io_backend.c 的 do_io。
 *
 * KAULA_IO_CONNECT / KAULA_IO_ACCEPT 是 socket 专属操作（accept 与
 * connect 无法用 read/write 表达），内核不实现，保留编号只为让旧代码
 * 拿到明确的 ENOSYS 而不是落进"未知类型"。编译器也不再把 net_connect /
 * net_accept 映射到内核 —— 那些调用退回普通阻塞调用，行为正确。
 */
typedef enum {
    KAULA_IO_READ    = 1,
    KAULA_IO_WRITE   = 2,
    KAULA_IO_CONNECT = 3,   /* 保留：内核不实现 */
    KAULA_IO_ACCEPT  = 4,   /* 保留：内核不实现 */
    KAULA_IO_SLEEP   = 5,   /* 定时器 */
} KaulaIoType;

/*
 * fd 的宽度按平台实际需要定，不强行统一。
 *
 * POSIX 上就是进程 fd 表里的下标，int 足够。
 * Windows 上要装的是 HANDLE —— 它是指针宽的，用 int 存会把高位
 * 截掉，拿到一个看似合法的野指针。所以那边用 intptr_t。
 *
 * 注意 Windows 分支拿到的是 CRT fd（fopen/_pipe 的产物），不是裸
 * HANDLE：_read/_write 与 _get_osfhandle 都以 CRT fd 为入口，
 * 这是三者之间唯一自洽的口径。
 *
 * 编译器生成的代码写 `(KaulaIoFd)(...)`，不写死 int ——
 * 见 compiler/internal/codegen/conc_io_rewrite.go。
 */
#if KAULA_PLATFORM_WINDOWS
typedef intptr_t KaulaIoFd;
#else
typedef int KaulaIoFd;
#endif

typedef struct {
    KaulaIoType type;
    KaulaIoFd   fd;
    void*       buf;
    size_t      buf_len;
    uint64_t    timeout_ns;
    void*       userdata;
} KaulaIoRequest;

struct KaulaResumeResult {
    KaulaResumeStatus status;
    void*             value;        /* DONE 时的返回值 */
    KaulaIoRequest    io_request;   /* SUSPEND 时的 IO 请求 */
    void*             block_fn;     /* BLOCKED 时的阻塞函数 */
    void*             block_arg;    /* BLOCKED 时的阻塞参数 */
    /* ABI 预留 */
    uint64_t          reserved[4];
};

/* ========== 复合语句恢复深度 ========== */

/*
 * KAULA_CONC_SUB_DEPTH 是「复合语句内恢复槽位」的层数上限。
 *
 * 为什么需要它：resume 的 pc 只能表示「顶层语句切分到哪儿了」。
 * 循环体/分支体里的挂起点落在顶层切分之外，pc 表达不了 ——
 * 症状是恢复后重新执行整个循环体（副作用翻倍），或者更糟：
 * 生成不出能编译的 C。这类错误不崩，只是答案错。
 *
 * 所以对「体内部含挂起点」的复合语句，编译器给每一层开一个槽位计数器，
 * 恢复时按槽位跳过本轮已执行的语句。层数必须编译期定死（I3：一切有界），
 * 超出由编译器报诊断 —— 不静默写坏 Frame。
 */
#define KAULA_CONC_SUB_DEPTH 8

/* ========== 任务标志 ========== */

typedef enum {
    KAULA_TASK_FLAG_NONE          = 0,
    KAULA_TASK_FLAG_HINT_BLOCKING = 1 << 0,  /* #[hint(blocking)] */
    KAULA_TASK_FLAG_UNINTRUSIVE  = 1 << 1,  /* 不可被取消 */
    KAULA_TASK_FLAG_FRAME_ON_HEAP= 1 << 2,  /* 帧需回收（顺序调用在栈上） */
} KaulaTaskFlags;

/* ========== 任务结构（文档 §3.1） ==========
 *
 * !!! 内部结构，非稳定 ABI（B10）!!!
 * 本结构当前整体暴露在安装头中。其内部字段（state/owner/result/pin_refs/
 * io_request/...）随 v1.x 任意调整，用户代码不得直接读写，只能经 Handle API
 * （kaula_handle_*）访问。真正的不透明化（仅保留前向声明、完整定义仅内部 TU
 * 可见）推迟到 v1.2。reserved[4] 已预留，缓解加字段导致的尺寸漂移。
 */
struct KaulaTask {
    /* ---- 身份 ---- */
    uint64_t            task_id;

    /* ---- 状态 ---- */
    _Atomic(int)         state;

    /* ---- 执行体 ---- */
    KaulaResumeFn       resume_fn;
    /* 首跑直通入口（B：enter 无 switch 无守卫，只跑入口段到第一个挂起点）。
     * NULL = 无直通（首跑也走 resume_fn）。 */
    KaulaResumeFn       enter_fn;
    void*               frame;
    size_t              frame_size;

    /*
     * 第二块需回收的内存：包在 frame 里的用户帧。
     *
     * spawn_simple 用一个内核分配的包装帧承载 { 函数指针, 用户帧 }，
     * 用户的 Frame 是生成代码另外分配的。两块都要随任务退休一起释放。
     *
     * 为什么结果必须活到 await 之后：任务结果以 &f->result 的形式返回，
     * await 读的是 Frame 里的字段。提前释放就是 UAF ——
     * 而且它不会崩，会安静地返回上次那个任务的旧值。
     */
    void*               inner_frame;

    /* ---- 属主（I2：任一时刻有且仅有一个 owner） ---- */
    _Atomic(KaulaWorker*) owner;

    /*
     * 单事务服务时间：提交 -> 完成。
     *
     * 为什么必须由内核打点，而不是调用方自己掐表：
     * 调用方拿到结果的时刻取决于「它什么时候轮到去取」，而不是
     * 「任务什么时候真的做完」。并发窗口里两者可以差两个数量级 ——
     * 槽位 999 早就算完了，却要等 0..998 全取完才被看到，
     * 于是量到的是排队 + 顺序取回，不是服务时间。
     *
     * complete_ns 在 kaula_task_complete 里、置 DONE 之前写入，
     * 因此对任何看到 DONE 的读者都可见（happens-before 那次 release 存）。
     */
    uint64_t            submit_ns;    /* kaula_task_init 时打点 */
    uint64_t            complete_ns;  /* complete/fail 时打点 */

    /* ---- 结果 ---- */
    _Atomic(void*)      result;       /* 原子：await 读与 complete 写必须 release/acquire 配对 */
    int                 error;        /* KaulaError */

    /* ---- 取消（文档 §3.3 Handle API） ---- */
    _Atomic(int)        cancelled;

    /*
     * pin_refs：本任务当前被多少个「尚未归还的句柄」持有 pin。
     *
     * 计数放在 Task 而不是句柄结构里，是为了让 kaula_handle_release 幂等：
     * 编译器会在 await_into 之后再发一次（作用域出口），句柄是按值传递的，
     * 那次调用看不到第一次已经把标志清掉。放在共享计数上，第二次会发现
     * 计数已归零并补回去，不会把 epoch pin 多放一次。
     */
    _Atomic(int)        pin_refs;

    /* ---- 完成通知 ---- */
    kaula_p_event       done_event;   /* await 等待此事件 */
    _Atomic(int)        waiters;      /* 当前等待者计数 */

    /* ---- IO 上下文 ---- */
    KaulaIoRequest      io_request;
    int                 io_result;    /* 字节数或 -errno */

    /* ---- 链接 ---- */
    struct KaulaTask*   next;         /* 完成队列 / Block Lane 链表 */
    struct KaulaTask*   next_free;    /* per-Worker 复用池链表（P0-2） */

    /* ---- 统计 ---- */
    uint64_t            created_ns;
    uint64_t            started_ns;
    uint32_t            resume_count;
    uint32_t            steal_count;

    /* ---- 属性 ---- */
    uint32_t            flags;

    /*
     * 内联结果槽：kaula_handle_complete() 用它存标量结果，
     * 避免为每个已完成句柄额外分配一块结果内存。
     * result 原子指针指向 &inline_result，await_into 拷走。
     * 只在 completed-handle 路径使用；正常任务的结果仍在 Frame->result。
     */
    uint64_t            inline_result;

    /* ABI 预留（见 B10：本结构当前仍整体暴露于安装头，v1.2 才不透明化） */
    uint64_t            reserved[4];
};

/* ========== Handle（文档 §3.3） ========== */

typedef struct {
    uint64_t task_id;
    KaulaTask* task;
    /* ABI 预留 */
    uint64_t reserved[4];
} KaulaHandle;

/* ========== 运行时配置（文档 §12.2） ========== */

typedef struct {
    uint32_t worker_count;           /* 0 = 自动检测 CPU 数 */
    int32_t  budget_quantum;         /* 0 = 默认 256 */
    uint32_t max_block_threads;      /* 0 = 默认 64 */
    uint64_t stall_threshold_ns;     /* 0 = 默认 10ms */
    int      enable_evict_monitor;   /* < 0 = 默认开 */
    int      enable_metrics;         /* < 0 = 默认关 */
    uint32_t inject_queue_capacity;  /* 0 = 默认 8192 */
    uint32_t max_tasks;              /* 0 = 默认 KAULA_MAX_TASKS */
    /* ABI 预留：用户栈分配 cfg={0}，后续加字段不破坏尺寸 */
    uint64_t reserved[4];
} KaulaRuntimeConfig;

/* ========== 全局运行时状态 ========== */

typedef struct {
    uint32_t         worker_count;
    int32_t          budget_quantum;
    uint32_t         max_block_threads;
    uint64_t         stall_threshold_ns;
    int              enable_evict_monitor;
    uint32_t         max_tasks;
    uint32_t         steal_batch_max;   /* 批量窃取每受害者一次偷几个；0=关闭批量 */
    _Atomic(int)     shutdown;
    _Atomic(uint64_t) active_task_count;
    _Atomic(uint64_t) next_task_id;
} KaulaRuntimeState;

extern KaulaRuntimeState g_conc_runtime;

/* ========== TLS：当前 Worker ========== */

extern KaulaWorker* kaula_conc_current_worker(void);
extern void          kaula_conc_set_current_worker(struct KaulaWorker* w);

/* ========== TLS：当前任务的 IO 结果 ========== */

/*
 * resume_fn 通过它读取上次挂起 IO 的返回值。
 * 必须是 TLS：多个 Worker 同时 resume 各自的 Task。
 */
extern _Thread_local int kaula_conc_tls_io_result;

/*
 * 上一次 await 的服务时间（complete_ns - submit_ns）。
 *
 * 与「谁先取到结果」无关，所以延迟统计必须用它：
 * 并发窗口里按调用方掐表量到的是排队 + 顺序取回，与服务时间
 * 能差两个数量级（槽位 999 早算完，却要等 0..998 全取完）。
 */
extern _Thread_local uint64_t kaula_conc_tls_last_latency_ns;

/* Task 的服务时间；打点缺失或时钟倒流时返回 0（不编造数值） */
uint64_t  kaula_task_latency_ns(const KaulaTask* task);

/*
 * 取上一次 await 的服务时间。编译器生成的 await 形态不带出参，
 * 基准与诊断代码经 `extern fn kaula_conc_last_latency_ns() -> u64` 取值
 * （extern 形参里放不了 Handle，无参就没这个问题）。
 */
uint64_t  kaula_conc_last_latency_ns(void);

/* ========== Task 等待原语（Task / 完成队列内部使用） ========== */

/*
 * 等待 Task 进入终态（DONE 或失败）。
 *
 * 若当前线程是 Worker，则协作执行其他任务直到目标完成
 * （work-stealing），不空等。
 * 否则 park 到 done_event 上，由 kaula_task_complete 唤醒。
 *
 * timeout_ns == KAULA_P_WAIT_FOREVER 表示无限等待。
 * 返回 1 = 已完成，0 = 超时。
 */
int kaula_task_await(KaulaTask* task, uint64_t timeout_ns);

/* 唤醒所有等待该 Task 的线程（complete 时调用） */
void kaula_task_wake_waiters(KaulaTask* task);

/* ========== 任务生命周期 API ========== */

KaulaTask*  kaula_task_alloc(void);
void        kaula_task_init(KaulaTask* task, KaulaResumeFn fn, KaulaResumeFn enter_fn,
                            void* frame, size_t frame_size);
void        kaula_task_complete(KaulaTask* task, void* result);
void        kaula_task_fail(KaulaTask* task, KaulaError err);
void        kaula_task_recycle(KaulaTask* task);

/*
 * epoch 安全后把完成的 Task 归还到 Worker 复用池。
 *
 * 池长未达高水位 → 入池复用（维护 w->task_pool_len）；
 * 已达高水位   → 彻底回收（destroy done_event + survivor_free），
 *                避免非 Worker 线程持续 spawn 时 Worker 池只增不减导致 OOM。
 * pool 必须是 &worker->task_pool（owner 独占，单线程访问）。
 */
void        kaula_task_pool_return(KaulaTask* task, void** pool);

/* 读取/设置池高水位（init 时从环境变量读一次） */
uint32_t    kaula_task_pool_high_water(void);
void        kaula_task_pool_high_water_init(void);

/* 标记任务需要取消：在下一个 await/suspend 点生效 */
int         kaula_task_cancel(KaulaTask* task);

/* ========== Handle API（文档 §3.3） ========== */

KaulaHandle kaula_handle_from_task(KaulaTask* task);
KaulaHandle kaula_handle_null(void);
int         kaula_handle_is_null(KaulaHandle h);
void*       kaula_handle_await(KaulaHandle h);
void*       kaula_handle_try_await(KaulaHandle h, uint64_t timeout_ns);
int         kaula_handle_poll(KaulaHandle h);

/*
 * kaula_handle_complete：构造一个"已完成"的 Handle。
 *
 * 用途：编译器把「常量实参 + 无 suspend」的 conc fn spawn 完全内联为
 * 直接函数调用时，需要返回一个立即就绪的 Handle 承载结果。
 * 从 task 池分配一个最小已完成任务，结果存在 inline_result 槽里。
 * 比正常 spawn 路径少一次 Frame 堆分配。
 */
KaulaHandle kaula_handle_complete(uint64_t result_val);

/*
 * await_into：把任务结果写进调用者的缓冲区。编译器生成的代码只应走这条。
 *
 * 为什么必须有 into 形态：
 * 任务结果是「Frame 里某字段的地址」，而 Frame 在完成时就随任务 retire。
 * 返回裸地址等于交出一个随时会悬垂的指针 —— 而它未必崩，
 * 常常安静地返回上一次那个任务的旧值。
 *
 * dst 在调用者栈上，大小与类型由调用点确定：拷贝不越界，
 * 也不需要内核分配临时缓冲（那会每 await 漏一次）。
 */
void        kaula_handle_await_into(KaulaHandle h, void* dst, size_t dst_size);
int         kaula_handle_try_await_into(KaulaHandle h, void* dst, size_t dst_size,
                                        uint64_t timeout_ns);

/*
 * await_into_timed：同时给出内核测得的服务时间（纳秒）。
 *
 * out_latency_ns = complete_ns - submit_ns，在 kaula_task_init 与
 * kaula_task_complete 里打点，**与「调用方何时轮到取结果」无关**。
 *
 * 延迟统计必须用它：并发窗口里按调用方掐表量到的是「排队 + 顺序取回」，
 * 与服务时间可以差两个数量级（槽位 999 早算完，却要等 0..998 全取完）。
 *
 * out_latency_ns 可为 NULL。
 */
void        kaula_handle_await_into_timed(KaulaHandle h, void* dst, size_t dst_size,
                                          uint64_t* out_latency_ns);
void        kaula_handle_cancel(KaulaHandle h);
int         kaula_handle_error(KaulaHandle h);

/*
 * 句柄的 drop 语义：归还它持有的那一个 epoch pin。
 *
 * 为什么必须有：pin 是在 kaula_handle_from_task（也就是 spawn 返回那一刻）
 * 拿的，挡的是「任务已完成并被 retire，而调用方还没来得及 await」那个窗口。
 * 代价是句柄**从不** await 时那个 pin 永远不还，会一直挡住回收直到
 * shutdown —— 内存峰值被拖到进程结束。
 *
 * 归还点就是「句柄不再可能被读」的时刻，两个：
 *   - await_into 读完（声明位置，编译器走这条）
 *   - 作用域出口（编译器为每个 Handle 局部量发一次本调用）
 *
 * 幂等：编译器在 await_into 之后再发一次是正常的（作用域出口那一发），
 * 第二次会发现计数已归零并补回，不会把 pin 多放一次。多放一次就是提前
 * 回收，那是悬垂 —— 比泄漏危险得多，所以这里刻意做成可重复调用的。
 *
 * 漏发不是问题：pin 不还 = 泄漏，可观测；pin 多还 = 悬垂，不可观测。
 */
void        kaula_handle_release(KaulaHandle h);

/* 当前还欠多少个 epoch pin（未归还的句柄数）；测试与诊断用 */
int         kaula_handle_pin_debt(KaulaHandle h);


#ifdef __cplusplus
}
#endif
#endif /* KAULA_CONC_TASK_H */