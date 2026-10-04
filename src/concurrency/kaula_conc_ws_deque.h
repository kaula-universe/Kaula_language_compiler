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

#ifdef __cplusplus
extern "C" {
#endif

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
     * 与 batch steal 的 fence(seq_cst) 配对，构成全局序仲裁点：
     * 若 owner 栅栏在全局序中先于 thief 栅栏，owner 已取的槽会反映在 thief 的
     * b2 读取里，thief 据此截断；反之 thief 栅栏先于 owner 栅栏，则 owner 随后
     * 读 top 必见 thief 已推进的 t+n，发现空、不取任何 <t+n 的槽。
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
     * P1-3：删除 seq_cst 栅栏。
     *
     * 原实现在 t 与 b 之间加 fence(seq_cst)。弱内存变体中不需要：
     * bottom 的 acquire 读已保证"看到 owner 释放位置"（与 pop 的
     * bottom store 构成顺序约束），最终裁决由 seq_cst CAS 完成。
     * 每个被窃取的元素省一次全序栅栏。
     */
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

/*
 * 批量窃取（DESIGN 8.1 第 1 条）：一次 CAS 把 top 前移 n 格，批量拿走
 * [t, t+n) 这 n 个最老的任务（FIFO），out[0..n) 按队列顺序填入。
 *
 * 两条正确性支柱（缺一不可，否则会双重取任务）：
 *   ① 永远在受害者队列里留 ≥1 项：n = min(max_n, size-1)。
 *      size = b - t。size < 2 时直接返回 0，调用方退回单任务 steal ——
 *      末元素由单偷的 seq_cst CAS 与 owner 仲裁。绝不能一次偷整队：
 *      owner 从 bottom 端 pop、thief 从 top 端 steal，只有"偷整队"时两端
 *      会撞上同一个槽。
 *   ② owner pop 对 bottom 的写必须是 release（见 kaula_ws_deque_pop），
 *      否则这里 acquire 读 bottom 可能拿到偏大的旧值、高估 size。
 *
 * 返回值：实际窃取数 n（1..max_n），0 = 空/竞争失败/不足两项（退回单偷）。
 * 调用方负责：改写这 n 个任务的 owner、统计、并把 out[1..n) 挪进自己队列。
 */
static inline int kaula_ws_deque_steal_batch(KaulaWSDeque* q,
                                             struct KaulaTask** out, int max_n) {
    if (max_n <= 0) return 0;

    int64_t t = atomic_load_explicit(&q->top, memory_order_acquire);
    int64_t b = atomic_load_explicit(&q->bottom, memory_order_acquire);
    int64_t s = b - t;
    if (s < 2) {
        return 0;   /* 不足两项：留给单任务 steal 仲裁末元素 */
    }

    int64_t n = s - 1;          /* 支柱①：永远留最后 1 项 */
    if (n > max_n) n = max_n;
    if (n <= 0) return 0;

    /*
     * ★关键：n 个槽的内容必须在 CAS（claim）**之前**投机读入 out。
     *
     * 经典单偷是「读 buffer[t] → CAS top」，读在 claim 之前。批量必须同构：
     * claim 一旦把 top 从 t 推进到 t+n，若 deque 原本接近满，owner 的下一次
     * push 几乎立刻就会写进这些刚释放的低物理槽（bottom 已在 +CAP 附近，
     * 例如 bottom=CAP 时 push 直接写 slot0=逻辑CAP）。若把读放在 CAS 之后，
     * thief 就会读到 owner 新写入的任务 → 双重取、真正的旧任务反成未消费。
     *
     * 投机读发生在 top=t 未变期间：此刻 owner 只能从 back（>=t+n，被 n=s-1
     * 排除在本范围外）pop/push，不会触碰 [t,t+n)，故读到的是当前内容。
     */
    for (int64_t i = 0; i < n; i++) {
        out[i] = atomic_load_explicit(
            &q->buffer[(t + i) & KAULA_WS_DEQUE_MASK], memory_order_acquire);
    }

    /* 一次 CAS 抢占 [t, t+n)；竞争失败（别的 thief/owner 动了 top）返回 0 重试。
     * 失败时 out 内容是投机垃圾，调用方按返回 0 丢弃。 */
    int64_t expected = t;
    if (!atomic_compare_exchange_strong_explicit(
            &q->top, &expected, t + n,
            memory_order_seq_cst, memory_order_relaxed)) {
        return 0;
    }

    /*
     * 与 owner pop 的 fence(seq_cst) 配对，构成全局序仲裁点。
     * fence 后只读一次 bottom：
     * - owner 栅栏在本 fence 之前：owner 已从 back 取走的槽（可能使 bottom 落入
     *   [t,t+n)）反映在 b2，只返回前 safe 个，其余投机读丢弃（归 owner 执行）。
     * - owner 栅栏在本 fence 之后：owner 随后读 top 必见 t+n，发现空、不取。
     */
    atomic_thread_fence(memory_order_seq_cst);
    int64_t b2 = atomic_load_explicit(&q->bottom, memory_order_acquire);
    int64_t safe = b2 - t;
    if (safe > n) safe = n;
    if (safe <= 0) return 0;

    return (int)safe;   /* out[0..safe) 是 CAS 前读入、经仲裁仍有效的任务 */
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


#ifdef __cplusplus
}
#endif
#endif /* KAULA_CONC_WS_DEQUE_H */