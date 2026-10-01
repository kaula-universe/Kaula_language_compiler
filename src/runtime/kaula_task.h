/*
 * Kaula 并发内核 · Task 结构
 *
 * Task 是并发调度的最小单位。
 * 每个 Task 包含一个 resume 函数（编译器产出的状态机）和一个帧（跨 suspend 存活的变量）。
 */

#ifndef KAULA_TASK_H
#define KAULA_TASK_H

/* 标准头文件必须在 platform.h 之前，因为 platform.h 使用了 getenv 等 */
#include <stdint.h>
#include <stddef.h>
#include <stdlib.h>
#include <stdatomic.h>

#include "../platform.h"

/* 缓存行大小（用于对齐，避免 false sharing） */
#ifndef KAULA_CACHE_LINE
#define KAULA_CACHE_LINE 64
#endif

/* ========== 前向声明 ========== */

struct KaulaWorker;

/* ========== 枚举 ========== */

/* Task 状态 */
typedef enum {
    KAULA_TASK_CREATED    = 0,  /* 已分配，未入队 */
    KAULA_TASK_READY      = 1,  /* 在某个 deque 中等待执行 */
    KAULA_TASK_RUNNING    = 2,  /* 正在某个 Worker 上执行 */
    KAULA_TASK_SUSPENDED  = 3,  /* 等待 IO 完成（阶段一暂不使用） */
    KAULA_TASK_BLOCKED    = 4,  /* 在阻塞车道中（阶段一暂不使用） */
    KAULA_TASK_DONE       = 5,  /* 已完成，等待回收 */
} KaulaTaskState;

/* Resume 返回状态 */
typedef enum {
    KAULA_RESUME_DONE             = 0,  /* 函数执行完毕 */
    KAULA_RESUME_SUSPEND          = 1,  /* 遇到 IO suspend 点 */
    KAULA_RESUME_BUDGET_EXHAUSTED = 2,  /* 预算耗尽，让出 Worker */
    KAULA_RESUME_BLOCKED          = 3,  /* 不可 lowering 的阻塞 */
} KaulaResumeStatus;

/* IO 请求类型 */
typedef enum {
    KAULA_IO_READ      = 1,
    KAULA_IO_WRITE     = 2,
    KAULA_IO_CONNECT   = 3,
    KAULA_IO_ACCEPT    = 4,
    KAULA_IO_SLEEP     = 5,  /* 定时器 */
} KaulaIoType;

/* IO 请求 */
typedef struct {
    KaulaIoType   type;
    int           fd;           /* 文件描述符 / socket */
    void*         buf;          /* 读写缓冲区 */
    size_t        buf_len;      /* 缓冲区长度 */
    uint64_t      timeout_ns;   /* 超时（SLEEP 类型使用） */
} KaulaIoRequest;

/* ========== 结构体 ========== */

/* Resume 函数签名（编译器 Lowering 产出） */
typedef struct KaulaResumeResult {
    KaulaResumeStatus status;
    void*             value;       /* DONE 时的返回值 */
    KaulaIoRequest    io_request;  /* SUSPEND 时的 IO 请求 */
} KaulaResumeResult;

typedef KaulaResumeResult (*KaulaResumeFn)(void* frame);

/* Handle：用户持有的并发句柄 */
typedef struct {
    uint64_t          task_id;
    struct KaulaTask* task;
} KaulaHandle;

/*
 * Task 结构体
 *
 * 从 KMM survivor 段分配，Task 完成后延迟回收（epoch-based）。
 * 对齐到缓存行，避免 false sharing。
 */
typedef struct KaulaTask {
    /* ---- 身份 ---- */
    uint64_t                task_id;        /* 全局唯一，单调递增 */

    /* ---- 状态 ---- */
    _Atomic(KaulaTaskState) state;

    /* ---- 执行体（编译器 Lowering 产出） ---- */
    KaulaResumeFn           resume_fn;      /* 状态机 resume 函数 */
    void*                   frame;          /* 状态机帧 */
    size_t                  frame_size;     /* 帧大小（用于回收） */

    /* ---- 属主（I2: 任务单属主） ---- */
    _Atomic(struct KaulaWorker*) owner;

    /* ---- 结果 ---- */
    void*                   result;

    /* ---- IO 上下文（SUSPENDED 时有效） ---- */
    KaulaIoRequest          io_request;     /* 当前挂起的 IO 请求 */
    int                     io_result;      /* IO 完成后的结果（字节数或错误码） */

    /* ---- Handle 关联 ---- */
    KaulaHandle             handle;

    /* ---- 等待机制 ---- */
    _Atomic(int)            done_flag;      /* 1 = 已完成，用于 await 轮询 */

    /* ---- 统计 ---- */
    uint64_t                created_ns;
    uint32_t                resume_count;   /* resume 被调用次数 */
    uint32_t                steal_count;    /* 被窃取次数 */

    /* ---- 链接（用于完成队列等） ---- */
    struct KaulaTask*       next;
} KaulaTask __attribute__((aligned(KAULA_CACHE_LINE)));

/* ========== API ========== */

/* Task 分配 */
extern struct KaulaTask* kaula_task_alloc(void);

/* Task 初始化 */
extern void kaula_task_init(struct KaulaTask* task,
                            KaulaResumeFn resume_fn,
                            void* frame,
                            size_t frame_size);

/* Task 完成 */
extern void kaula_task_complete(struct KaulaTask* task, void* result);

/* Task 回收 */
extern void kaula_task_recycle(struct KaulaTask* task);

/* Handle 操作 */
extern void* kaula_handle_await(KaulaHandle h);
extern int   kaula_handle_poll(KaulaHandle h);

/* ========== 内联辅助 ========== */

/* TLS: 当前 Task 的 IO 结果（resume_fn 读取） */
extern __thread int g_kaula_current_io_result;

static inline KaulaHandle kaula_handle_from_task(struct KaulaTask* task) {
    KaulaHandle h;
    h.task_id = task->task_id;
    h.task = task;
    return h;
}

static inline KaulaHandle kaula_handle_null(void) {
    KaulaHandle h = {0, NULL};
    return h;
}

static inline int kaula_handle_is_null(KaulaHandle h) {
    return h.task == NULL;
}

#endif /* KAULA_TASK_H */
