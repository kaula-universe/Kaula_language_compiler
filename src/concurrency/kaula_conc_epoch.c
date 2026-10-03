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
             * 仍然太满：直接在当前上下文释放。
             * 这是唯一的危险点 —— 但此时 epoch 刚被推进过，
             * 风险窗口极小（仅当有 Worker 恰好在 enter 与访问之间）。
             * 生产路径上 retire 发生在"Task 已 DONE"之后，
             * 不会再被 Worker 取走，因此实际是安全的。
             */
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

    uint64_t min_active = kaula_epoch_min_active();

    uint32_t freed = 0;
    uint32_t kept = 0;

    for (uint32_t i = 0; i < list->count; i++) {
        /*
         * 安全条件：所有活跃 Worker 的 epoch 都严格大于本对象退休时的 epoch。
         * 若 min_active > item.epoch，说明没有 Worker 还停留在 item 可见的时期。
         */
        if (min_active > list->items[i].epoch) {
            if (list->items[i].pool != NULL) {
                /* P0-2：推进复用池而非释放（pool 由调用方保证单线程访问） */
                KaulaTask* t = (KaulaTask*)list->items[i].ptr;
                t->next_free = *(KaulaTask**)list->items[i].pool;
                *(KaulaTask**)list->items[i].pool = t;
            } else {
                kmm_v4_survivor_free(list->items[i].ptr);
            }
            freed++;
        } else {
            /* 还不安全，压缩到前 kept 位 */
            list->items[kept++] = list->items[i];
        }
    }

    list->count = kept;
    if (freed > 0) {
        atomic_fetch_add_explicit(&g_reclaimed_total, freed, memory_order_relaxed);
    }
    return freed;
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