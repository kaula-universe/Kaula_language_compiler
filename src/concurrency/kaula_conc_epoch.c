/*
 * Kaula 并发内核 · Epoch-Based Reclamation 实现
 */

#include "kaula_conc_epoch.h"
#include "kaula_conc_worker.h"
#include "../kmm_scoped_allocator_v4.h"

#include <stdlib.h>
#include <string.h>

KaulaEpochDomain g_conc_epoch;

/* 统计 */
static _Atomic(uint64_t) g_retired_total = 0;
static _Atomic(uint64_t) g_reclaimed_total = 0;

/* pin 期内强制回收被阻止的泄漏兜底计数（可观测性，不进正常输出） */
static _Atomic(uint64_t) g_epoch_pin_forced_leaks = 0;

/* ========== 初始化 ========== */

void kaula_epoch_init(void) {
    atomic_store_explicit(&g_conc_epoch.global_epoch, 1, memory_order_relaxed);
    atomic_store(&g_retired_total, 0);
    atomic_store(&g_reclaimed_total, 0);

    /* 复位所有 Worker 的退休列表 */
    KaulaWorker* w = kaula_conc_current_worker();
    if (w != NULL) {
        w->retired.count = 0;
    }
}

/* ========== 临界区进出 ========== */

/*
 * 非 Worker 线程的 pin 计数。
 *
 * Worker 靠 local_epoch 被回收扫描看见；main / 外部线程没有这个槽位，
 * 于是"不阻碍回收"就等于"随时可能被回收掉脚下的东西"。
 *
 * 后果很具体：main 在 await 里 park，任务完成 → retire → 某个 Worker
 * 立刻 reclaim → main 醒来读 task->result，读的是已释放内存。
 * 而且它未必崩：常常安静地返回上一次那个任务的旧值。
 *
 * 用全局计数而不是 per-thread 槽位，是因为这些读者不参与调度，
 * 只需要一个"有没有人在读"的粗粒度信号。
 */
static _Atomic(uint32_t) g_ext_pins = 0;

void kaula_epoch_pin(void) {
    atomic_fetch_add_explicit(&g_ext_pins, 1, memory_order_seq_cst);
}

void kaula_epoch_unpin(void) {
    atomic_fetch_sub_explicit(&g_ext_pins, 1, memory_order_seq_cst);
}

void kaula_epoch_enter(void) {
    KaulaWorker* w = kaula_conc_current_worker();
    if (w == NULL) {
        /* 非 Worker 线程：用全局 pin 计数登记 */
        kaula_epoch_pin();
        return;
    }
    /*
     * 发布当前访问的 epoch。
     * seq_cst 保证：任意线程若在读取 Task 指针之后才看到本次 enter，
     * 则它在 reclaim 的扫描中必然被看见。
     */
    atomic_store_explicit(&w->local_epoch,
        atomic_load_explicit(&g_conc_epoch.global_epoch, memory_order_acquire),
        memory_order_seq_cst);
}

void kaula_epoch_exit(void) {
    KaulaWorker* w = kaula_conc_current_worker();
    if (w == NULL) {
        kaula_epoch_unpin();
        return;
    }
    /* 推进到当前 global_epoch，表示"我不再持有旧 epoch 的指针" */
    atomic_store_explicit(&w->local_epoch,
        atomic_load_explicit(&g_conc_epoch.global_epoch, memory_order_acquire),
        memory_order_seq_cst);

    kaula_conc_advance_epoch_if_needed();
    kaula_epoch_try_reclaim();
}

/* ========== 退休 ========== */

void kaula_epoch_retire(void* ptr) {
    kaula_epoch_retire_to(ptr, NULL);
}

void kaula_epoch_retire_to(void* ptr, void** pool) {
    if (ptr == NULL) return;

    atomic_fetch_add_explicit(&g_retired_total, 1, memory_order_relaxed);

    /*
     * 退休列表挂在当前 Worker 上。非 Worker 线程（main 线程
     * await 到同步执行的 Task）也要能回收，故退化用一个全局兜底列表。
     */
    KaulaRetireList* list = kaula_conc_retire_list();
    if (list == NULL) return;

    if (list->count >= KAULA_EPOCH_MAX_RETIRED) {
        /*
         * 列表满：必须强制回收，否则会无限增长。
         * 只有在"无活跃 Worker 处于旧 epoch"时才安全，因此先推进 epoch。
         */
        kaula_conc_advance_epoch_if_needed();
        if (list->count >= KAULA_EPOCH_MAX_RETIRED) {
            /*
             * 仍然太满：先检查是否有外部 pin（main/await 仍持有句柄）。
             *
             * 全量并发压测暴露：10 万任务一次性提交时，每任务 3 次 retire
             * （wrapper + Frame + Task），每 Worker 2.5 万任务 = 7.5 万
             * retire > 旧上限 65536 → 触发强制回收。此时 main 还 pin 着
             * 全部句柄（await 未开始），直接 free/复用会令 await 读到
             * 已回收的 Task/Frame —— 表现为大量"值错位"（got 恒为某个
             * 旧任务的 result，err≈10%）。
             *
             * 安全优先：有 pin 时泄漏兜底（对象留在内存，await 读安全），
             * 不回收。泄漏由 shutdown 的 reclaim_all 清账；极端规模下
             * 这是"慢/多占内存"对"悬垂读"的正确取舍 —— 宁可泄漏不可 UAF。
             */
            if (atomic_load_explicit(&g_ext_pins, memory_order_seq_cst) > 0) {
                atomic_fetch_add_explicit(&g_epoch_pin_forced_leaks, 1,
                                          memory_order_relaxed);
                return;
            }
            if (pool != NULL) {
                /* 列表满时的强制路径也走复用池（保持单链表） */
                KaulaTask* t = (KaulaTask*)ptr;
                t->next_free = *(KaulaTask**)pool;
                *(KaulaTask**)pool = t;
            } else {
                kmm_v4_survivor_free(ptr);
                atomic_fetch_add_explicit(&g_reclaimed_total, 1, memory_order_relaxed);
            }
            return;
        }
    }

    list->items[list->count].ptr = ptr;
    list->items[list->count].epoch =
        atomic_load_explicit(&g_conc_epoch.global_epoch, memory_order_acquire);
    list->items[list->count].pool = pool;
    list->count++;
}

/* ========== 回收 ========== */

/*
 * 找出所有活跃 Worker 中最小的 local_epoch。
 * 没有活跃 Worker 时返回当前 global_epoch（即"无阻碍"）。
 */
static uint64_t kaula_epoch_min_active(void) {
    uint32_t n = g_conc_runtime.worker_count;
    uint64_t min_epoch =
        atomic_load_explicit(&g_conc_epoch.global_epoch, memory_order_acquire);

    /*
     * 有非 Worker 线程在读（await）时，一律判定为"不可回收"。
     *
     * 它们没有 local_epoch 槽位，也就没有更细的粒度可言。
     * 保守判定是唯一安全的选择：多留一会儿垃圾，
     * 比返回一个悬垂指针便宜得多。
     */
    if (atomic_load_explicit(&g_ext_pins, memory_order_seq_cst) > 0) {
        return 0;
    }

    for (uint32_t i = 0; i < n; i++) {
        KaulaWorker* w = &g_conc_workers[i];
        if (!kaula_conc_worker_is_active(w)) continue;

        uint64_t e = atomic_load_explicit(&w->local_epoch, memory_order_acquire);
        /*
         * local_epoch == 0 表示该 Worker 从未 enter 过，
         * 视为"当前 epoch"（不阻碍回收）。
         */
        if (e == 0) continue;
        if (e < min_epoch) min_epoch = e;
    }
    return min_epoch;
}

uint32_t kaula_epoch_try_reclaim(void) {
    KaulaRetireList* list = kaula_conc_retire_list();
    if (list == NULL || list->count == 0) return 0;

    /*
     * 频率限制：列表未达阈值不回收。
     *
     * worker_loop 每执行一个任务就 exit 一次 epoch → 每次 try_reclaim
     * 全扫列表。任务完成时每任务 3 次 retire，列表迅速积累 —— 扫描
     * 成本 O(列表长度)，每任务一次 → 总 O(n²)。压测实测：
     * kaula_epoch_try_reclaim 占 CPU 70%（10 万任务每任务 ~40µs）。
     *
     * 阈值以下直接跳过：回收是"延迟"而非"丢失" —— 列表有
     * KAULA_EPOCH_MAX_RETIRED 硬上限兜底（满时强制 advance+回收），
     * 阈值只控制摊销粒度。阈值 256 时总扫描 Σ(256,512,...) ≈ O(n²/512)，
     * 10 万任务摊销到 ~10ms 级。
     */
    if (list->count < KAULA_EPOCH_RECLAIM_THRESHOLD) return 0;

    uint64_t min_active = kaula_epoch_min_active();

    /*
     * 前缀回收：退休列表尾插、global_epoch 单调递增 → 列表按 epoch
     * 升序排列。安全条件（min_active > item.epoch）在升序列表中构成
     * 前缀 —— 扫到第一个不安全项即可停止，无需遍历全列表。
     */
    uint32_t i = 0;
    while (i < list->count && min_active > list->items[i].epoch) {
        if (list->items[i].pool != NULL) {
            /* P0-2：推进复用池而非释放（pool 由调用方保证单线程访问） */
            KaulaTask* t = (KaulaTask*)list->items[i].ptr;
            t->next_free = *(KaulaTask**)list->items[i].pool;
            *(KaulaTask**)list->items[i].pool = t;
        } else {
            kmm_v4_survivor_free(list->items[i].ptr);
        }
        i++;
    }

    if (i > 0) {
        /* 保留段（不安全项）移到前部，列表仍保持升序 */
        uint32_t kept = list->count - i;
        if (kept > 0) {
            memmove(&list->items[0], &list->items[i],
                    kept * sizeof(list->items[0]));
        }
        list->count = kept;
        atomic_fetch_add_explicit(&g_reclaimed_total, i, memory_order_relaxed);
    }
    return i;
}

void kaula_epoch_reclaim_all(void) {
    KaulaRetireList* list = kaula_conc_retire_list();
    if (list == NULL) return;

    for (uint32_t i = 0; i < list->count; i++) {
        if (list->items[i].pool != NULL) {
            KaulaTask* t = (KaulaTask*)list->items[i].ptr;
            t->next_free = *(KaulaTask**)list->items[i].pool;
            *(KaulaTask**)list->items[i].pool = t;
        } else {
            kmm_v4_survivor_free(list->items[i].ptr);
        }
        atomic_fetch_add_explicit(&g_reclaimed_total, 1, memory_order_relaxed);
    }
    list->count = 0;
}

void kaula_epoch_shutdown(void) {
    /* shutdown 时所有 Worker 已 join，单线程上下文下全部释放 */
    kaula_epoch_reclaim_all();

    /* 扫一遍所有 Worker 的退休列表（Worker 已停，列表不会再变动） */
    uint32_t n = g_conc_runtime.worker_count;
    for (uint32_t i = 0; i < n; i++) {
        KaulaRetireList* l = &g_conc_workers[i].retired;
        for (uint32_t j = 0; j < l->count; j++) {
            if (l->items[j].pool != NULL) {
                KaulaTask* t = (KaulaTask*)l->items[j].ptr;
                t->next_free = *(KaulaTask**)l->items[j].pool;
                *(KaulaTask**)l->items[j].pool = t;
            } else {
                kmm_v4_survivor_free(l->items[j].ptr);
            }
            atomic_fetch_add_explicit(&g_reclaimed_total, 1, memory_order_relaxed);
        }
        l->count = 0;
    }
}

/* ========== 统计 ========== */

uint64_t kaula_epoch_retired_total(void) {
    return atomic_load(&g_retired_total);
}

uint64_t kaula_epoch_reclaimed_total(void) {
    return atomic_load(&g_reclaimed_total);
}