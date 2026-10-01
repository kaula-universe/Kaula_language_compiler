/*
 * Kaula 并发内核 · 任务与运行时配置
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

/* ========== 编译期硬上限（I3：一切有界） ========== */

#define KAULA_MAX_WORKERS            256
#define KAULA_MAX_TASKS              1000000   /* 全局活跃任务硬上限 */
#define KAULA_BLOCK_LANE_MAX_THREADS 64
#define KAULA_BLOCK_LANE_MIN_THREADS 2
#define KAULA_BLOCK_LANE_QUEUE_CAP   4096
#define KAULA_INJECT_QUEUE_CAPACITY  8192
#define KAULA_INJECT_QUEUE_MASK      (KAULA_INJECT_QUEUE_CAPACITY - 1)

/* 完成队列分片数（文档 §8.1，必须为 2 的幂） */
#define KAULA_COMPLETION_SHARDS      16
#define KAULA_COMPLETION_CAPACITY    1024

/* epoch 退休对象上限（文档 §10.3） */
#define KAULA_EPOCH_MAX_RETIRED      65536

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

typedef enum {
    KAULA_IO_READ    = 1,
    KAULA_IO_WRITE   = 2,
    KAULA_IO_CONNECT = 3,
    KAULA_IO_ACCEPT  = 4,
    KAULA_IO_SLEEP   = 5,   /* 定时器 */

    /* 别名：网络语义，与读写同路径 */
    KAULA_IO_RECV    = 6,
    KAULA_IO_SEND    = 7,
} KaulaIoType;

typedef struct {
    KaulaIoType type;
    int         fd;
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
};

/* ========== 任务标志 ========== */

typedef enum {
    KAULA_TASK_FLAG_NONE          = 0,
    KAULA_TASK_FLAG_HINT_BLOCKING = 1 << 0,  /* #[hint(blocking)] */
    KAULA_TASK_FLAG_UNINTRUSIVE  = 1 << 1,  /* 不可被取消 */
    KAULA_TASK_FLAG_FRAME_ON_HEAP= 1 << 2,  /* 帧需回收（顺序调用在栈上） */
} KaulaTaskFlags;

/* ========== 任务结构（文档 §3.1） ========== */

struct KaulaTask {
    /* ---- 身份 ---- */
    uint64_t            task_id;

    /* ---- 状态 ---- */
    _Atomic(int)         state;

    /* ---- 执行体 ---- */
    KaulaResumeFn       resume_fn;
    void*               frame;
    size_t              frame_size;

    /* ---- 属主（I2：任一时刻有且仅有一个 owner） ---- */
    _Atomic(KaulaWorker*) owner;

    /* ---- 结果 ---- */
    void*               result;
    int                 error;        /* KaulaError */

    /* ---- 取消（文档 §3.3 Handle API） ---- */
    _Atomic(int)        cancelled;

    /* ---- 完成通知 ---- */
    kaula_p_event       done_event;   /* await 等待此事件 */
    _Atomic(int)        waiters;      /* 当前等待者计数 */

    /* ---- IO 上下文 ---- */
    KaulaIoRequest      io_request;
    int                 io_result;    /* 字节数或 -errno */

    /* ---- 链接 ---- */
    struct KaulaTask*   next;         /* 完成队列 / Block Lane 链表 */

    /* ---- 统计 ---- */
    uint64_t            created_ns;
    uint64_t            started_ns;
    uint32_t            resume_count;
    uint32_t            steal_count;

    /* ---- 属性 ---- */
    uint32_t            flags;
};

/* ========== Handle（文档 §3.3） ========== */

typedef struct {
    uint64_t task_id;
    KaulaTask* task;
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
} KaulaRuntimeConfig;

/* ========== 全局运行时状态 ========== */

typedef struct {
    uint32_t         worker_count;
    int32_t          budget_quantum;
    uint32_t         max_block_threads;
    uint64_t         stall_threshold_ns;
    int              enable_evict_monitor;
    uint32_t         max_tasks;
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
void        kaula_task_init(KaulaTask* task, KaulaResumeFn fn, void* frame, size_t frame_size);
void        kaula_task_complete(KaulaTask* task, void* result);
void        kaula_task_fail(KaulaTask* task, KaulaError err);
void        kaula_task_recycle(KaulaTask* task);

/* 标记任务需要取消：在下一个 await/suspend 点生效 */
int         kaula_task_cancel(KaulaTask* task);

/* ========== Handle API（文档 §3.3） ========== */

KaulaHandle kaula_handle_from_task(KaulaTask* task);
KaulaHandle kaula_handle_null(void);
int         kaula_handle_is_null(KaulaHandle h);
void*       kaula_handle_await(KaulaHandle h);
void*       kaula_handle_try_await(KaulaHandle h, uint64_t timeout_ns);
int         kaula_handle_poll(KaulaHandle h);
void        kaula_handle_cancel(KaulaHandle h);
int         kaula_handle_error(KaulaHandle h);

#endif /* KAULA_CONC_TASK_H */