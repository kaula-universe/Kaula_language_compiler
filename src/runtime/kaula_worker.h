/*
 * Kaula 并发内核 · Worker
 *
 * Worker 是执行 Task 的线程。
 * 每个 Worker 有一个本地 work-stealing deque，从 bottom 端 push/pop。
 * 本地队列空时，从其他 Worker 的队列 steal。
 */

#ifndef KAULA_WORKER_H
#define KAULA_WORKER_H

/* 标准头文件必须在 platform.h 之前，因为 platform.h 使用了 getenv 等 */
#include <stdint.h>
#include <stddef.h>
#include <stdlib.h>
#include <stdatomic.h>
#include <pthread.h>

#include "../platform.h"
#include "kaula_ws_deque.h"
#include "kaula_budget.h"

/* 前向声明 */
struct KaulaTask;

/* Worker 最大数量 */
#define KAULA_MAX_WORKERS 256

/*
 * Worker 结构体
 *
 * 每个 Worker 是一个独立的 OS 线程，持有：
 * - 本地 work-stealing deque
 * - 预算计数器
 * - 当前执行的 Task 指针
 *
 * 对齐到缓存行，避免 false sharing。
 */
typedef struct KaulaWorker {
    /* ---- 线程 ---- */
    pthread_t           thread;         /* OS 线程句柄 */
    uint32_t            worker_id;      /* 全局唯一 [0, N) */

    /* ---- 本地队列 ---- */
    KaulaWSDeque        local_queue;    /* thread-local，无需锁 */

    /* ---- 预算 ---- */
    KaulaBudget         budget;         /* thread-local */

    /* ---- 当前执行 ---- */
    struct KaulaTask*   current_task;   /* thread-local */

    /* ---- 生命周期 ---- */
    _Atomic(int)        running;        /* 1 = 运行中，0 = 退出 */

    /* ---- 统计 ---- */
    uint64_t            tasks_executed;
    uint64_t            steal_count;
    uint64_t            steal_attempts;
} KaulaWorker __attribute__((aligned(KAULA_CACHE_LINE)));

/* ========== TLS 当前 Worker ========== */

/* 当前线程的 Worker 指针（thread-local） */
extern KaulaWorker* kaula_current_worker(void);
extern void kaula_set_current_worker(KaulaWorker* w);

/* ========== Worker 主循环 ========== */

/* Worker 主循环（线程入口函数） */
extern void kaula_worker_loop(KaulaWorker* w);

/* Worker 执行单个 Task */
extern void kaula_worker_execute_task(KaulaWorker* w, struct KaulaTask* task);

/* ========== Worker 生命周期 ========== */

/* 创建 Worker 线程 */
extern int kaula_worker_create(KaulaWorker* w, uint32_t worker_id);

/* 等待 Worker 退出 */
extern void kaula_worker_join(KaulaWorker* w);

/* 停止 Worker */
extern void kaula_worker_stop(KaulaWorker* w);

#endif /* KAULA_WORKER_H */
