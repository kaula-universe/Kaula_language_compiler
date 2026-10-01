/*
 * Kaula 并发内核 · 可观测性（文档 §12）
 *
 * P7：调参可解释。目标是任何一次"内核为什么这么做"都能从数字回答：
 * 谁在偷、谁在让、IO 批多大、阻塞池用了几条线程。
 *
 * 所有计数器是 relaxed 原子：统计不参与正确性，允许轻微不一致。
 * enable_metrics 关闭时更新函数是空实现，热路径开销归零。
 */

#ifndef KAULA_CONC_METRICS_H
#define KAULA_CONC_METRICS_H

#include <stdint.h>
#include <stdatomic.h>

#include "kaula_conc_platform.h"
#include "kaula_conc_task.h"

/* 开关：关闭时全部 inline 空实现 */
extern _Atomic(int) kaula_conc_metrics_enabled;

typedef struct {
    /* ---- 全局 ---- */
    uint64_t total_spawned;
    uint64_t total_completed;
    uint64_t total_failed;
    uint64_t total_would_block;
    uint64_t total_stolen;

    /* ---- IO ---- */
    uint64_t io_submits;
    uint64_t io_completions;
    uint64_t io_timer_fires;
    uint64_t io_pending;

    /* ---- 阻塞车道 ---- */
    uint64_t block_submits;
    uint64_t block_completions;
    uint64_t block_threads;
    uint64_t block_would_block;

    /* ---- 预算 ---- */
    int32_t  current_quantum;
    double   ema_p90_ns;

    /* ---- 撤离 ---- */
    uint64_t evictions;
    uint64_t worker_count;

    /* ---- 内存（epoch） ---- */
    uint64_t epoch_retired;
    uint64_t epoch_reclaimed;
} KaulaMetrics;

/* 取一份快照 */
void kaula_metrics_snapshot(KaulaMetrics* out);

/* 打印到 stderr */
void kaula_metrics_dump(void);

/* 启用/关闭 */
void kaula_metrics_enable(int on);

/* 慢路径更新函数 */
void kaula_metrics_inc_spawned_slow(void);
void kaula_metrics_inc_completed_slow(void);
void kaula_metrics_inc_failed_slow(void);
void kaula_metrics_inc_would_block(void);
void kaula_metrics_set_quantum(int32_t q);

/* ========== 热路径更新（内联，开关关闭时完全空） ========== */

static inline void kaula_conc_metrics_inc_spawned(void) {
    if (atomic_load_explicit(&kaula_conc_metrics_enabled, memory_order_relaxed)) {
        kaula_metrics_inc_spawned_slow();
    }
}

static inline void kaula_conc_metrics_inc_completed(void) {
    if (atomic_load_explicit(&kaula_conc_metrics_enabled, memory_order_relaxed)) {
        kaula_metrics_inc_completed_slow();
    }
}

static inline void kaula_conc_metrics_inc_failed(void) {
    if (atomic_load_explicit(&kaula_conc_metrics_enabled, memory_order_relaxed)) {
        kaula_metrics_inc_failed_slow();
    }
}

#endif /* KAULA_CONC_METRICS_H */