/*
 * Kaula 并发内核 · Block Lane
 *
 * 阻塞车道：执行不可 lowering 的阻塞操作（FFI、系统调用等）
 * 独立线程池，与 Worker 隔离
 */

#ifndef KAULA_BLOCK_LANE_H
#define KAULA_BLOCK_LANE_H

#include <stdint.h>
#include <stdatomic.h>
#include <pthread.h>

#include "kaula_task.h"
#include "kaula_mpsc_queue.h"

/* Block Lane 最大线程数 */
#define KAULA_BLOCK_LANE_MAX_THREADS 64

/* Block Lane 队列容量 */
#define KAULA_BLOCK_LANE_QUEUE_CAPACITY 4096

/*
 * Block Lane 结构体
 *
 * 独立线程池，负责：
 * 1. 接收 BLOCKED 状态的 Task
 * 2. 在独立线程上执行阻塞操作
 * 3. 完成后将 Task 送回 Worker
 */
typedef struct {
    /* ---- 线程池 ---- */
    pthread_t           threads[KAULA_BLOCK_LANE_MAX_THREADS];
    uint32_t            thread_count;
    uint32_t            max_threads;

    /* ---- 任务队列（Worker → Block Lane） ---- */
    KaulaMpscQueue      task_queue;

    /* ---- 生命周期 ---- */
    _Atomic(int)        running;

    /* ---- 统计 ---- */
    _Atomic(uint64_t)   total_submits;
    _Atomic(uint64_t)   total_completions;
} KaulaBlockLane;

/* ========== API ========== */

/* 初始化 Block Lane */
extern int kaula_block_lane_init(KaulaBlockLane* lane, uint32_t max_threads);

/* 启动 Block Lane 线程 */
extern int kaula_block_lane_start(KaulaBlockLane* lane);

/* 停止 Block Lane */
extern void kaula_block_lane_stop(KaulaBlockLane* lane);

/* 等待 Block Lane 线程退出 */
extern void kaula_block_lane_join(KaulaBlockLane* lane);

/* 销毁 Block Lane */
extern void kaula_block_lane_destroy(KaulaBlockLane* lane);

/* 提交阻塞任务（Worker 调用） */
extern void kaula_block_lane_submit(KaulaBlockLane* lane, KaulaTask* task);

#endif /* KAULA_BLOCK_LANE_H */
