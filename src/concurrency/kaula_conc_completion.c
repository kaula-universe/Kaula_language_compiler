/*
 * Kaula 并发内核 · 完成队列实现
 */

#include "kaula_conc_completion.h"

#include <string.h>

KaulaCompletionQueue g_conc_completion;

void kaula_completion_init(void) {
    memset(&g_conc_completion, 0, sizeof(g_conc_completion));
}

void kaula_completion_shutdown(void) {
    /* 队列不持有内存所有权（Task 由 epoch 回收），无需逐个释放 */
    memset(&g_conc_completion, 0, sizeof(g_conc_completion));
}

/* 按 task_id 选片：低位散列，避免相邻 id 落到同一片 */
static inline uint32_t completion_shard_of(uint64_t task_id) {
    uint64_t h = task_id;
    h ^= h >> 33;
    h *= 0xFF51AFD7ED558CCDULL;
    h ^= h >> 29;
    return (uint32_t)(h & (KAULA_COMPLETION_SHARDS - 1));
}

int kaula_completion_push(KaulaTask* task) {
    KaulaCompletionShard* s =
        &g_conc_completion.shards[completion_shard_of(task->task_id)];

    uint64_t tail = atomic_load_explicit(&s->tail, memory_order_relaxed);
    uint64_t head = atomic_load_explicit(&s->head, memory_order_acquire);

    /*
     * 边界是"至少一个空槽"，所以上限为 CAPACITY-1。
     * 若允许写满 CAPACITY 个，head/tail 会追平，
     * 满队列与空队列变得不可区分。
     */
    if (tail - head >= KAULA_COMPLETION_CAPACITY_PER_SHARD - 1) {
        /* 满：不覆盖（I3）。等待者靠 done_event 唤醒，不依赖此队列。 */
        atomic_fetch_add_explicit(&g_conc_completion.total_drops, 1,
                                  memory_order_relaxed);
        return 0;
    }

    atomic_store_explicit(&s->slots[tail % KAULA_COMPLETION_CAPACITY_PER_SHARD],
                          task, memory_order_release);
    atomic_store_explicit(&s->tail, tail + 1, memory_order_release);
    atomic_fetch_add_explicit(&g_conc_completion.total_completions, 1,
                              memory_order_relaxed);
    return 1;
}

KaulaTask* kaula_completion_take(uint64_t task_id) {
    /*
     * 完成队列是 FIFO，而 take 按 id 精确查找，
     * 因此只扫有限窗口（近期完成的那些）。
     * 真正的 await 走 kaula_task_await()，不经过这里。
     */
    KaulaCompletionShard* s =
        &g_conc_completion.shards[completion_shard_of(task_id)];

    uint64_t head = atomic_load_explicit(&s->head, memory_order_relaxed);
    uint64_t tail = atomic_load_explicit(&s->tail, memory_order_acquire);

    for (uint64_t i = head; i < tail; i++) {
        KaulaTask* t = atomic_load_explicit(
            &s->slots[i % KAULA_COMPLETION_CAPACITY_PER_SHARD],
            memory_order_acquire);
        if (t != NULL && t->task_id == task_id) {
            /* 摘走 */
            atomic_store_explicit(
                &s->slots[i % KAULA_COMPLETION_CAPACITY_PER_SHARD],
                NULL, memory_order_relaxed);
            atomic_store_explicit(&s->head, i + 1, memory_order_release);
            return t;
        }
    }
    return NULL;
}

uint64_t kaula_completion_count(void) {
    return atomic_load(&g_conc_completion.total_completions);
}

uint64_t kaula_completion_drops(void) {
    return atomic_load(&g_conc_completion.total_drops);
}