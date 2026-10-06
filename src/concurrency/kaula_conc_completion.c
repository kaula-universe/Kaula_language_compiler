/*
 * Kaula 并发内核 · 完成队列实现
 */

#include "kaula_conc_completion.h"

#include <string.h>
#include <stdbool.h>

KaulaCompletionQueue g_conc_completion;

/*
 * 消费期开关：完成队列是否真的需要承接 payload。
 *
 * 背景：完成队列的唯一消费者是 kaula_completion_take()（按 task_id 精确
 * 查找最近完成项）。而热路径 await 走的是 kaula_task_await()，只读
 * task->state / task->result，**完全不碰这个队列**。
 *
 * 于是稳态下有界环形缓冲的"写入"是纯冗余：
 *   - 每次 push 要读 head、写 slot、release 写 tail；
 *   - 片容量 1024，稳态下无消费者 → 很快填满 → 每任务一次
 *     total_drops 原子加（实测即大量 noise case）。
 *
 * 但**不能简单地 metrics 关闭就整条跳过**：test_epoch / test_completion
 * 等单元测试会在未开 metrics 的情况下直接验证 push/take 语义与
 * total_drops 计数。整条跳过会让那些测试失败。
 *
 * 因此拆成两件事：
 *   1) total_completions / total_drops —— 纯计数，保留（并收紧为
 *      "拒绝次数"语义，与测试断言一致）。
 *   2) head/slot/tail 的环形缓冲写入 —— 只有真的有消费者时才做。
 *
 * 消费期由 kaula_completion_consumer_begin() 开启：任务完成路径
 * （kaula_task_complete）在 enable_metrics==0 时不开启，稳态热路径
 * 因此省掉全部环形缓冲写；测试与需要 take 的场景显式开启。
 */
static bool g_completion_consumer_active = false;

void kaula_completion_consumer_begin(void) {
    g_completion_consumer_active = true;
}

void kaula_completion_consumer_end(void) {
    g_completion_consumer_active = false;
}

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
    /*
     * 无消费者时：只做计数，不写环形缓冲。
     *
     * total_completions 语义是"完成事件发生了多少次"，与是否有消费者
     * 无关，因此始终递增；total_drops 语义是"入队被拒多少次"，没有
     * 消费者就没有"拒"这回事，保持 0（旧实现在无消费者时会虚增）。
     */
    if (!g_completion_consumer_active) {
        atomic_fetch_add_explicit(&g_conc_completion.total_completions, 1,
                                  memory_order_relaxed);
        return 1;
    }

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