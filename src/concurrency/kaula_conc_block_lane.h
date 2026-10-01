/*
 * Kaula 并发内核 · 阻塞车道（Block Lane）
 *
 * 文档 §7：执行不可 lowering 的阻塞操作（FFI、系统调用等）。
 * 与 Worker 隔离 —— 这是 I5 的落地形式：
 *
 *   I5 隔离对象是 Worker，不是任务
 *   —— 判定"卡住"时换掉卡住的 Worker，任务原地不动。
 *
 * 换掉 Worker 的具体做法就是把阻塞工作移到独立车道：
 * Worker 永远不执行可能长阻塞的 resume，Worker 卡死的可能就被消除了。
 *
 * 修复记录：
 * - 原实现传 max_threads=4 再取 min(2, max)，实际永远只有 2 个线程，
 *   文档 §7.2 要求的 0..64 动态伸缩没有实现。现改为按需伸缩。
 * - 原实现在车道队列满时"降级为在 Worker 上原地执行阻塞" ——
 *   那恰好破坏了 I5（把阻塞搬回 Worker）。现改为返回 WouldBlock，
 *   由 Worker 继续按预算 + 停滞判定处理。
 * - 原实现没有 idle_count / 通知机制，线程 100us 轮询空转。现改为 park/unpark。
 */

#ifndef KAULA_CONC_BLOCK_LANE_H
#define KAULA_CONC_BLOCK_LANE_H

#include <stdint.h>
#include <stdatomic.h>

#include "kaula_conc_task.h"
#include "kaula_conc_mpsc_queue.h"

typedef struct {
    /* 线程池：固定数组，按需伸缩（文档 §7.2） */
    kaula_p_thread_t   threads[KAULA_BLOCK_LANE_MAX_THREADS];
    _Atomic(uint32_t)  thread_count;
    uint32_t           max_threads;

    /* Worker → 车道 任务队列 */
    kaula_mpsc_queue_t task_queue;

    /* 空闲统计与唤醒 */
    _Atomic(uint32_t)  idle_count;
    /*
     * 在手任务数：提交时 +1，任务终结时 -1。
     * 动态伸缩的判据（见 .c 里的说明）—— 比 idle_count 可靠，
     * 因为 idle 在线程唤醒前后才变动，提交方读到的是陈旧值。
     */
    _Atomic(uint32_t)  busy_count;
    kaula_p_event      notify;

    /* 生命周期 */
    _Atomic(int)       running;
    _Atomic(int)       stopping;

    /* 统计 */
    _Atomic(uint64_t)  total_submits;
    _Atomic(uint64_t)  total_completions;
    _Atomic(uint64_t)  total_spawned_threads;
    _Atomic(uint64_t)  total_would_block;
} KaulaBlockLane;

extern KaulaBlockLane g_conc_block_lane;

/* 生命周期 */
int  kaula_block_lane_init(uint32_t max_threads);
int  kaula_block_lane_start(void);
void kaula_block_lane_stop(void);
void kaula_block_lane_join(void);
void kaula_block_lane_destroy(void);

/*
 * 提交一个阻塞 Task。
 * 返回 0 = 已移交车道；非 0 = 车道不可用（队列满或线程数触顶）。
 * 调用方不得在 Worker 上原地执行阻塞。
 */
int kaula_block_lane_submit(KaulaTask* task);

/* 测试钩子：设置当前线程的车道线程数（驱动伸缩逻辑） */
uint32_t kaula_block_lane_thread_count(void);

#endif /* KAULA_CONC_BLOCK_LANE_H */