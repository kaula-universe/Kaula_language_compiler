/*
 * Kaula 并发内核 · 调度器
 *
 * 调度器负责：
 * - spawn：将 Task 提交到当前 Worker 的本地队列
 * - steal：从其他 Worker 的队列窃取 Task
 * - 全局注入队列：接收外部线程提交的 Task
 */

#ifndef KAULA_SCHEDULER_H
#define KAULA_SCHEDULER_H

#include <stdint.h>

#include "kaula_task.h"
#include "kaula_worker.h"

/* 全局注入队列容量 */
#define KAULA_INJECT_QUEUE_CAPACITY 8192
#define KAULA_INJECT_QUEUE_MASK (KAULA_INJECT_QUEUE_CAPACITY - 1)

/*
 * 全局注入队列（MPMC）
 *
 * 用于：
 * - spawn 时本地队列满的溢出
 * - 外部线程提交的 Task
 */
typedef struct {
    _Atomic(uint64_t)     head;
    _Atomic(uint64_t)     tail;
    _Atomic(KaulaTask*)   buffer[KAULA_INJECT_QUEUE_CAPACITY];
} KaulaInjectQueue;

/* ========== 调度器 API ========== */

/* spawn：提交 Task 到当前 Worker 的本地队列 */
extern KaulaHandle kaula_spawn(KaulaResumeFn resume_fn, void* frame, size_t frame_size);

/* steal：从其他 Worker 的队列窃取 Task */
extern KaulaTask* kaula_scheduler_steal(KaulaWorker* w);

/* 从全局注入队列取 Task */
extern KaulaTask* kaula_inject_queue_pop(KaulaInjectQueue* q);

/* 向全局注入队列推 Task */
extern int kaula_inject_queue_push(KaulaInjectQueue* q, KaulaTask* task);

/* 初始化全局注入队列 */
extern void kaula_inject_queue_init(KaulaInjectQueue* q);

#endif /* KAULA_SCHEDULER_H */
