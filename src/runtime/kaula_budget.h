/*
 * Kaula 并发内核 · 预算机制
 *
 * 每个 Worker 有一个预算计数器，每执行一次 resume 消耗一个 tick。
 * 预算耗尽时，当前 Task 被重新入队，Worker 取下一个 Task。
 * 这保证了长任务不会饿死其他任务。
 */

#ifndef KAULA_BUDGET_H
#define KAULA_BUDGET_H

#include <stdint.h>

/* 默认预算量子：每个 Task 每轮最多执行 256 次 resume */
#define KAULA_DEFAULT_BUDGET_QUANTUM 256

/*
 * 预算结构体
 * 每个 Worker 持有一个，thread-local，无需锁。
 */
typedef struct {
    int32_t     remaining;      /* 剩余预算单位（tick） */
    int32_t     quantum;        /* 每次重置的量子 */
    uint64_t    total_consumed; /* 累计消耗（统计用） */
} KaulaBudget;

/* ========== 内联操作 ========== */

/*
 * 重置预算（Task 开始执行前调用）
 */
static inline void kaula_budget_reset(KaulaBudget* b, int32_t quantum) {
    b->remaining = quantum;
    b->quantum = quantum;
}

/*
 * 消耗一个 tick
 * 返回值：1 = 预算耗尽，0 = 仍有预算
 */
static inline int kaula_budget_tick(KaulaBudget* b) {
    b->remaining--;
    b->total_consumed++;
    return b->remaining <= 0;
}

/*
 * 检查预算是否耗尽
 */
static inline int kaula_budget_exhausted(KaulaBudget* b) {
    return b->remaining <= 0;
}

/*
 * 获取剩余预算
 */
static inline int32_t kaula_budget_remaining(KaulaBudget* b) {
    return b->remaining;
}

#endif /* KAULA_BUDGET_H */
