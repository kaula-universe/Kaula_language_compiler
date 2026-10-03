/*
 * Kaula 并发内核 · WSDeque（Chase-Lev 无锁工作窃取队列）
 *
 * owner 独占 bottom 端 push/pop（LIFO，保证缓存局部性）
 * thief  从 top 端 steal（FIFO，拿最老的任务，最大化并行）
 *
 * 关键修复（对应缺陷 #2、#3）：
 *
 * 缺陷 #2：原实现只提供 owner 独占的 push_tail/pop。但 IoDriver 完成、
 *         Block Lane 完成、handle_await 帮忙执行都会从"别的线程"
 *         往 owner 的 bottom 端 push —— 多个生产者并发写 bottom，
 *         Chase-Lev 的正确性前提被破坏。
 *
 * 修复（**已重新审视，见下方「重要」**）：拆成两个接口。
 *   kaula_ws_deque_push_owner()  仅 owner 调用，最快，无 CAS
 *   kaula_ws_deque_push_external() 任意线程调用，CAS 抢占槽位
 *   pop / steal 走 CAS 判定，与 Chase-Lev 论文一致
 *
 * 重要：push_external 与 push_owner/pop **不能并发使用**，它没有
 *       真正修好缺陷 #2。
 *
 *       push_owner / pop 改 bottom 用的是非 CAS 的裸 store，会把
 *       push_external 那次 CAS 的结果直接覆盖回去：
 *
 *           owner 读 b=5
 *           external CAS 5→6 成功，写 buffer[5] = taskE
 *           owner   store bottom = 6     ← 抹掉 external 刚占的槽位
 *
 *       于是要么 buffer[5] 被写两次（丢一个 Task），要么 bottom 被
 *       倒退（已入队的 Task 不可达），两者都无法自愈。
 *       实测：suspend 曲线 100% 死锁（FINDINGS.md F1）。
 *
 *       根治办法是回到 Chase-Lev 的原始约束 —— **bottom 只有 owner 写**。
 *       kaula_conc_worker.c 的 worker_post 已据此收紧：只有调用者
 *       本身就是 owner 才写本地 deque，外部一律走全局注入队列。
 *
 *       本接口保留给单线程 owner 场景与 test_ws_deque.c 使用；
 *       调用前必须确认 owner 不会同时 push/pop。不要把它接回
 *       worker_post —— 那正是 F1 的成因。
 *
 * 缺陷 #3：原实现在队列满时返回 false，而所有调用点都忽略了返回值，
 *         导致 Task 被静默丢弃、await 永久阻塞。
 *         修复：调用点必须检查返回值（见 scheduler.c）。
 */

#ifndef KAULA_CONC_WS_DEQUE_H
#define KAULA_CONC_WS_DEQUE_H

#include <stdint.h>
#include <stddef.h>
#include <string.h>
#include <stdatomic.h>

#include "kaula_conc_platform.h"

/* 容量必须为 2 的幂（I3：一切有界） */
#ifndef KAULA_WS_DEQUE_CAPACITY
    #define KAULA_WS_DEQUE_CAPACITY 4096
#endif
#define KAULA_WS_DEQUE_MASK (KAULA_WS_DEQUE_CAPACITY - 1)

#if (KAULA_WS_DEQUE_CAPACITY & (KAULA_WS_DEQUE_CAPACITY - 1)) != 0
    #error "KAULA_WS_DEQUE_CAPACITY 必须是 2 的幂"
#endif

/*
 * top 与 bottom 各占一条缓存行，buffer 另起一条。
 * 避免 owner 频繁写 bottom 时与 thief 读 top 互相失效缓存行。
 */
typedef struct KaulaWSDeque {
    _Atomic(int64_t)         top;
    char                      _pad0[KAULA_CACHE_LINE - sizeof(int64_t)];
    _Atomic(int64_t)         bottom;   /* 也做成 atomic：外部 push 需要 CAS */
    char                      _pad1[KAULA_CACHE_LINE - sizeof(int64_t)];
    _Atomic(struct KaulaTask*) buffer[KAULA_WS_DEQUE_CAPACITY];
} KaulaWSDeque;

static inline void kaula_ws_deque_init(KaulaWSDeque* q) {
    atomic_store_explicit(&q->top, 0, memory_order_relaxed);
    atomic_store_explicit(&q->bottom, 0, memory_order_relaxed);
    memset((void*)q->buffer, 0, sizeof(q->buffer));
}

/* ========== Owner 独占操作（最快路径，无 CAS） ========== */

/*
 * 从 bottom 端推入。
 * 前置条件：仅 deque 的 owner 线程调用。
 * 返回 1 = 成功，0 = 队列满（调用方必须处理背压，不得忽略）。
 */
static inline int kaula_ws_deque_push_owner(KaulaWSDeque* q, struct KaulaTask* task) {
    int64_t b = atomic_load_explicit(&q->bottom, memory_order_relaxed);
    int64_t t = atomic_load_explicit(&q->top, memory_order_acquire);

    if (b - t >= KAULA_WS_DEQUE_CAPACITY) {
        return 0; /* 满 */
    }

    atomic_store_explicit(&q->buffer[b & KAULA_WS_DEQUE_MASK], task, memory_order_relaxed);
    /* release：保证 task 的所有初始化对随后 steal 到它的 thief 可见 */
    atomic_store_explicit(&q->bottom, b + 1, memory_order_release);
    return 1;
}

/*
 * 从 bottom 端弹出（LIFO）。
 * 前置条件：仅 deque 的 owner 线程调用。
 * 返回 Task 指针，NULL 表示队列空。
 */
static inline struct KaulaTask* kaula_ws_deque_pop(KaulaWSDeque* q) {
    int64_t b = atomic_load_explicit(&q->bottom, memory_order_relaxed) - 1;
    atomic_store_explicit(&q->bottom, b, memory_order_relaxed);

    /*
     * 关键：seq_cst 栅栏，不可省略。
     *
     * 它禁止把下面的 top 读取重排到 bottom 写入之前，
     * 从而保证"本 owner 释放了这个位置"这一事实对 thief 可见，
     * 否则 thief 可能仍按旧的 top 抢走本 owner 已经看到的元素 ——
     * 表现为同一个 Task 被 pop 和 steal 各计一次（重复执行）。
     * Chase-Lev 论文 §4 要求此处用 seq_cst 栅栏而非更强的原子读。
     */
    atomic_thread_fence(memory_order_seq_cst);

    int64_t t = atomic_load_explicit(&q->top, memory_order_relaxed);

    struct KaulaTask* task = NULL;

    if (t <= b) {
        task = atomic_load_explicit(
            &q->buffer[b & KAULA_WS_DEQUE_MASK], memory_order_relaxed);

        if (t == b) {
            /* 最后一个元素：可能正被 thief 抢，用 CAS 裁决 */
            int64_t expected = t;
            if (!atomic_compare_exchange_strong_explicit(
                    &q->top, &expected, t + 1,
                    memory_order_seq_cst, memory_order_relaxed)) {
                task = NULL;   /* thief 赢了 */
            }
            atomic_store_explicit(&q->bottom, b + 1, memory_order_relaxed);
        }
    } else {
        /* 空：bottom 恢复到 top 位置，不能越过 top（否则覆盖未消费元素） */
        atomic_store_explicit(&q->bottom, t, memory_order_relaxed);
    }

    return task;
}

/* ========== 任意线程操作 ========== */

/*
 * 从 bottom 端推入（外部线程用）。
 *
 * 与 owner 路径的区别：用 CAS 循环在 bottom 上预留槽位，
 * 这样 IoDriver / Block Lane / await 线程可以并发投递而不会破坏
 * Chase-Lev 不变量。代价是多一次 CAS（远小于一次跨核缓存行失效）。
 *
 * 返回 1 = 成功，0 = 队列满（调用方必须处理背压）。
 */
static inline int kaula_ws_deque_push_external(KaulaWSDeque* q, struct KaulaTask* task) {
    int64_t b = atomic_load_explicit(&q->bottom, memory_order_relaxed);

    for (;;) {
        int64_t t = atomic_load_explicit(&q->top, memory_order_acquire);
        if (b - t >= KAULA_WS_DEQUE_CAPACITY) {
            return 0; /* 满 */
        }

        /* 先占槽位（发布点） */
        atomic_store_explicit(&q->buffer[b & KAULA_WS_DEQUE_MASK], task, memory_order_relaxed);

        int64_t expected = b;
        if (atomic_compare_exchange_weak_explicit(
                &q->bottom, &expected, b + 1,
                memory_order_acq_rel, memory_order_relaxed)) {
            return 1;
        }
        /* CAS 失败：expected 已被更新为当前 bottom，重试 */
        b = expected;
    }
}

/*
 * 从 top 端窃取（FIFO）。
 * 任意线程可调用。
 * 返回 Task 指针，NULL 表示队列空或竞争失败。
 */
static inline struct KaulaTask* kaula_ws_deque_steal(KaulaWSDeque* q) {
    int64_t t = atomic_load_explicit(&q->top, memory_order_acquire);

    /*
     * 关键：seq_cst 栅栏，不可省略。
     * 与 pop 中的栅栏配对：它保证"读到 bottom > t"发生在
     * "owner 对该元素的发布完成"之后被观察到。
     */
    atomic_thread_fence(memory_order_seq_cst);

    int64_t b = atomic_load_explicit(&q->bottom, memory_order_acquire);

    if (t >= b) {
        return NULL; /* 空 */
    }

    struct KaulaTask* task = atomic_load_explicit(
        &q->buffer[t & KAULA_WS_DEQUE_MASK], memory_order_acquire);

    int64_t expected = t;
    if (!atomic_compare_exchange_strong_explicit(
            &q->top, &expected, t + 1,
            memory_order_seq_cst, memory_order_relaxed)) {
        return NULL; /* CAS 失败：别的 thief 或 owner 先动了 */
    }
    return task;
}

/* ========== 查询 ========== */

/* 近似大小（用于统计） */
static inline int64_t kaula_ws_deque_size(const KaulaWSDeque* q) {
    int64_t t = atomic_load_explicit(&q->top, memory_order_relaxed);
    int64_t b = atomic_load_explicit(&q->bottom, memory_order_relaxed);
    int64_t size = b - t;
    return size > 0 ? size : 0;
}

/* 近似判空 */
static inline int kaula_ws_deque_empty(const KaulaWSDeque* q) {
    return kaula_ws_deque_size(q) <= 0;
}

#endif /* KAULA_CONC_WS_DEQUE_H */