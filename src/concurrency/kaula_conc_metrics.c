/*
 * Kaula 并发内核 · 可观测性实现
 */

#include "kaula_conc_metrics.h"
#include "kaula_conc_worker.h"
#include "kaula_conc_iodriver.h"
#include "kaula_conc_block_lane.h"
#include "kaula_conc_epoch.h"
#include "kaula_conc_evict.h"
#include "kaula_conc_budget.h"

#include <stdio.h>
#include <string.h>

_Atomic(int) kaula_conc_metrics_enabled;

/* 慢路径计数器 */
static _Atomic(uint64_t) g_spawned;
static _Atomic(uint64_t) g_completed;
static _Atomic(uint64_t) g_failed;
static _Atomic(uint64_t) g_would_block;
static _Atomic(int32_t)  g_quantum;

void kaula_metrics_enable(int on) {
    atomic_store(&kaula_conc_metrics_enabled, on ? 1 : 0);
}

void kaula_metrics_inc_spawned_slow(void) {
    atomic_fetch_add_explicit(&g_spawned, 1, memory_order_relaxed);
}

void kaula_metrics_inc_completed_slow(void) {
    atomic_fetch_add_explicit(&g_completed, 1, memory_order_relaxed);
}

void kaula_metrics_inc_failed_slow(void) {
    atomic_fetch_add_explicit(&g_failed, 1, memory_order_relaxed);
}

void kaula_metrics_inc_would_block(void) {
    atomic_fetch_add_explicit(&g_would_block, 1, memory_order_relaxed);
}

void kaula_metrics_set_quantum(int32_t q) {
    atomic_store_explicit(&g_quantum, q, memory_order_relaxed);
}

void kaula_metrics_snapshot(KaulaMetrics* out) {
    if (out == NULL) return;
    memset(out, 0, sizeof(*out));

    out->total_spawned     = atomic_load(&g_spawned);
    out->total_completed   = atomic_load(&g_completed);
    out->total_failed      = atomic_load(&g_failed);
    out->total_would_block = atomic_load(&g_would_block);
    out->current_quantum   = atomic_load(&g_quantum);
    out->worker_count      = g_conc_runtime.worker_count;
    out->evictions         = kaula_evict_total();

    /* 窃取总数需要遍历 Worker */
    for (uint32_t i = 0; i < g_conc_runtime.worker_count; i++) {
        out->total_stolen += atomic_load(&g_conc_workers[i].stats.steal_count);
    }

    /* IO */
    out->io_submits      = atomic_load(&g_conc_iodriver.total_submits);
    out->io_completions  = atomic_load(&g_conc_iodriver.total_completions);
    out->io_timer_fires  = atomic_load(&g_conc_iodriver.total_timer_fires);
    out->io_pending      = kaula_iodriver_pending_count();

    /* 阻塞车道 */
    out->block_submits     = atomic_load(&g_conc_block_lane.total_submits);
    out->block_completions = atomic_load(&g_conc_block_lane.total_completions);
    out->block_threads     = kaula_block_lane_thread_count();
    out->block_would_block = atomic_load(&g_conc_block_lane.total_would_block);

    /* 预算自适应状态：取任一已运行 Worker 的 EMA 作为代表 */
    if (g_conc_runtime.worker_count > 0) {
        out->ema_p90_ns = atomic_load(&g_conc_workers[0].budget_adaptor.ema_p90_ns);
    }

    /* epoch */
    out->epoch_retired   = kaula_epoch_retired_total();
    out->epoch_reclaimed = kaula_epoch_reclaimed_total();
}

void kaula_metrics_dump(void) {
    KaulaMetrics m;
    kaula_metrics_snapshot(&m);

    fprintf(stderr, "\n=== Kaula 并发内核指标 ===\n");
    fprintf(stderr, "任务:\n");
    fprintf(stderr, "  spawn 总数      : %llu\n", (unsigned long long)m.total_spawned);
    fprintf(stderr, "  完成            : %llu\n", (unsigned long long)m.total_completed);
    fprintf(stderr, "  失败            : %llu\n", (unsigned long long)m.total_failed);
    fprintf(stderr, "  WouldBlock      : %llu\n", (unsigned long long)m.total_would_block);
    fprintf(stderr, "  被窃取          : %llu\n", (unsigned long long)m.total_stolen);
    fprintf(stderr, "调度:\n");
    fprintf(stderr, "  Worker 数       : %llu\n", (unsigned long long)m.worker_count);
    fprintf(stderr, "  当前预算量子    : %d\n", m.current_quantum);
    fprintf(stderr, "  执行时长 EMA    : %.1f ns\n", m.ema_p90_ns);
    fprintf(stderr, "  隔离次数        : %llu\n", (unsigned long long)m.evictions);
    fprintf(stderr, "IO:\n");
    fprintf(stderr, "  提交            : %llu\n", (unsigned long long)m.io_submits);
    fprintf(stderr, "  完成            : %llu\n", (unsigned long long)m.io_completions);
    fprintf(stderr, "  定时器触发      : %llu\n", (unsigned long long)m.io_timer_fires);
    fprintf(stderr, "  当前挂起        : %llu\n", (unsigned long long)m.io_pending);
    fprintf(stderr, "阻塞车道:\n");
    fprintf(stderr, "  提交            : %llu\n", (unsigned long long)m.block_submits);
    fprintf(stderr, "  完成            : %llu\n", (unsigned long long)m.block_completions);
    fprintf(stderr, "  活跃线程        : %llu\n", (unsigned long long)m.block_threads);
    fprintf(stderr, "  WouldBlock      : %llu\n", (unsigned long long)m.block_would_block);
    fprintf(stderr, "内存:\n");
    fprintf(stderr, "  epoch 退休      : %llu\n", (unsigned long long)m.epoch_retired);
    fprintf(stderr, "  epoch 已回收    : %llu\n", (unsigned long long)m.epoch_reclaimed);
    fprintf(stderr, "========================\n\n");
}