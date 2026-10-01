/*
 * Kaula 并发内核 · 预算自适应实现
 */

#include "kaula_conc_budget.h"
#include "kaula_conc_task.h"

#include <math.h>

void kaula_budget_adaptor_init(KaulaBudgetAdaptor* a) {
    atomic_store(&a->ema_p90_ns, 0.0);
    a->alpha = 0.1;
    a->quantum = KAULA_DEFAULT_BUDGET_QUANTUM;
    a->last_adjust_ns = 0;
    a->target_ns = 10000.0;  /* 10μs */
}

int32_t kaula_budget_adaptor_quantum(const KaulaBudgetAdaptor* a) {
    return a->quantum;
}

int kaula_budget_adaptor_update(KaulaBudgetAdaptor* a, uint64_t sample_ns) {
    if (sample_ns == 0) return 0;

    /* 指数移动平均 */
    double prev = atomic_load(&a->ema_p90_ns);
    double next = a->alpha * (double)sample_ns + (1.0 - a->alpha) * prev;
    atomic_store(&a->ema_p90_ns, next);

    /* 每 100ms 调整一次，避免抖动 */
    uint64_t now = kaula_p_now_ns();
    if (a->last_adjust_ns != 0 &&
        now - a->last_adjust_ns < 100000000ULL) {
        return 0;
    }
    a->last_adjust_ns = now;

    /*
     * 上一次样本期间，量子为 a->quantum，故单 tick 平均耗时
     * 约为 ema / quantum。新量子 = 目标耗时 / 单 tick 耗时。
     */
    if (a->quantum <= 0) a->quantum = KAULA_DEFAULT_BUDGET_QUANTUM;

    double per_tick_ns = next / (double)a->quantum;
    if (per_tick_ns < 1.0) per_tick_ns = 1.0;   /* 防除零 */

    double want = a->target_ns / per_tick_ns;
    int32_t new_quantum = (int32_t)want;

    if (new_quantum < KAULA_BUDGET_MIN_QUANTUM) new_quantum = KAULA_BUDGET_MIN_QUANTUM;
    if (new_quantum > KAULA_BUDGET_MAX_QUANTUM) new_quantum = KAULA_BUDGET_MAX_QUANTUM;

    if (new_quantum == a->quantum) {
        return 0;
    }
    a->quantum = new_quantum;
    return 1;
}