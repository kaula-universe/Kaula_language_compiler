/*
 * Kaula 并发内核 · 预算机制
 *
 * 设计说明书 §2.7：mode 是 monomorphize 期常量。
 * 顺序形态（不加 spawn）下，整条预算递减指令被编译期消除；
 * 本文件只服务并发形态。
 *
 * 两个层次：
 * - 调度量子：每 resume 消耗一个 tick，耗尽则 Task 重新入队
 *   （文档 §5.1，保证长任务不死循环独占 Worker）。
 * - 自适应：按历史执行时长的 EMA 调整量子（文档 §5.3，
 *   对应设计说明书 §16 未决问题 ①）。
 */

#ifndef KAULA_CONC_BUDGET_H
#define KAULA_CONC_BUDGET_H

#include <stdint.h>
#include <stdatomic.h>

#include "kaula_conc_platform.h"

#ifdef __cplusplus
extern "C" {
#endif

/* 预算计数器：thread-local，无需锁 */
typedef struct {
    int32_t  remaining;       /* 剩余 tick */
    int32_t  quantum;         /* 本轮量子 */
    uint64_t total_consumed;  /* 累计消耗（统计） */
} KaulaBudget;

/* ========== 计数器操作 ========== */

static inline void kaula_budget_reset(KaulaBudget* b, int32_t quantum) {
    b->remaining = quantum;
    b->quantum = quantum;
}

/* 消耗一个 tick，返回 1 = 已耗尽 */
static inline int kaula_budget_tick(KaulaBudget* b) {
    b->remaining--;
    b->total_consumed++;
    return b->remaining <= 0;
}

static inline int kaula_budget_exhausted(const KaulaBudget* b) {
    return b->remaining <= 0;
}

static inline int32_t kaula_budget_remaining(const KaulaBudget* b) {
    return b->remaining;
}

/* ========== 自适应（文档 §5.3） ========== */

/*
 * 以历史执行时长的指数移动平均估算 P90，再反推量子，
 * 使每个量子对应的执行时间接近 target_ns（默认 10μs，L1 缓存友好）。
 */
typedef struct {
    _Atomic(double) ema_p90_ns;
    double         alpha;             /* 平滑系数，默认 0.1 */
    int32_t        quantum;           /* 当前推荐量子 */
    uint64_t       last_adjust_ns;    /* 上次调整时刻 */
    uint64_t       target_ns;         /* 目标执行时长 */
} KaulaBudgetAdaptor;

/* 初始化：alpha=0.1，target=10μs */
void kaula_budget_adaptor_init(KaulaBudgetAdaptor* a);

/*
 * 提交一次执行时长样本。
 *
 * 返回非 0 表示量子被调整了（调用方应读取 a->quantum 更新预算）。
 */
int kaula_budget_adaptor_update(KaulaBudgetAdaptor* a, uint64_t sample_ns);

/* 读取当前推荐量子 */
int32_t kaula_budget_adaptor_quantum(const KaulaBudgetAdaptor* a);


#ifdef __cplusplus
}
#endif
#endif /* KAULA_CONC_BUDGET_H */