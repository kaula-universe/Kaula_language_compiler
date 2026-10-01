/*
 * Kaula 并发内核 · Work-Stealing Deque
 * 基于 Chase-Lev 无锁双端队列
 *
 * owner 从 bottom 端 push/pop（LIFO）
 * thief 从 top 端 steal（FIFO）
 */

#ifndef KAULA_WS_DEQUE_H
#define KAULA_WS_DEQUE_H

/* 标准头文件必须在 platform.h 之前，因为 platform.h 使用了 getenv 等 */
#include <stdint.h>
#include <stddef.h>
#include <stdlib.h>
#include <stdatomic.h>
#include <string.h>

#include "../platform.h"

/* 缓存行大小（用于对齐，避免 false sharing） */
#ifndef KAULA_CACHE_LINE
#define KAULA_CACHE_LINE 64
#endif

/* 队列容量：必须为 2 的幂 */
#define KAULA_WS_DEQUE_CAPACITY 4096
#define KAULA_WS_DEQUE_MASK (KAULA_WS_DEQUE_CAPACITY - 1)

/* 前向声明 */
struct KaulaTask;

/*
 * Chase-Lev Work-Stealing Deque
 *
 * 内存布局：
 *   - top: 原子变量，thief 和 owner 都可能写
 *   - bottom: 仅 owner 写，thief 读（非原子近似值）
 *   - buffer: 环形缓冲区，存储 Task 指针
 *
 * 对齐：确保不同 deque 的热点字段不在同一缓存行
 */
typedef struct KaulaWSDeque {
    _Atomic(int64_t)          top;                                    /* 64 bytes */
    int64_t                   bottom;                                 /* 64 bytes */
    _Atomic(struct KaulaTask*) buffer[KAULA_WS_DEQUE_CAPACITY];      /* 32KB */
} KaulaWSDeque __attribute__((aligned(KAULA_CACHE_LINE)));

/* ========== 初始化 ========== */

static inline void kaula_ws_deque_init(KaulaWSDeque* q) {
    atomic_store_explicit(&q->top, 0, memory_order_relaxed);
    q->bottom = 0;
    memset((void*)q->buffer, 0, sizeof(q->buffer));
}

/* ========== Owner 操作（仅 Worker 自己调用） ========== */

/*
 * 从 bottom 端推入（本地提交 Task）
 *
 * 前置条件：仅 owner 线程调用
 * 返回值：true=成功，false=队列满
 */
static inline int kaula_ws_deque_push_tail(KaulaWSDeque* q, struct KaulaTask* task) {
    int64_t b = q->bottom;
    int64_t t = atomic_load_explicit(&q->top, memory_order_acquire);

    if (b - t >= KAULA_WS_DEQUE_CAPACITY) {
        return 0; /* 满：触发背压 */
    }

    atomic_store_explicit(
        &q->buffer[b & KAULA_WS_DEQUE_MASK],
        task,
        memory_order_relaxed
    );

    /* 内存屏障：确保 buffer 写入在 bottom 更新前可见 */
    atomic_thread_fence(memory_order_release);
    q->bottom = b + 1;

    return 1;
}

/*
 * 从 bottom 端弹出（本地取 Task，LIFO）
 *
 * 前置条件：仅 owner 线程调用
 * 返回值：Task 指针，或 NULL（队列空）
 */
static inline struct KaulaTask* kaula_ws_deque_pop(KaulaWSDeque* q) {
    int64_t b = q->bottom - 1;
    q->bottom = b;

    int64_t t = atomic_load_explicit(&q->top, memory_order_relaxed);

    if (t > b) {
        /* 空队列：修正 bottom */
        q->bottom = t;
        return NULL;
    }

    struct KaulaTask* task = atomic_load_explicit(
        &q->buffer[b & KAULA_WS_DEQUE_MASK],
        memory_order_relaxed
    );

    if (t < b) {
        /* 非边界情况：快速路径，无竞争 */
        return task;
    }

    /* t == b：最后一个元素，可能与 thief 竞争 */
    int64_t expected = t;
    if (!atomic_compare_exchange_strong_explicit(
            &q->top,
            &expected,
            t + 1,
            memory_order_acquire,
            memory_order_relaxed)) {
        /* CAS 失败：thief 抢走了 */
        q->bottom = t + 1;
        return NULL;
    }

    /* CAS 成功：拿到最后一个元素 */
    q->bottom = t + 1;
    return task;
}

/* ========== Thief 操作（从其他 Worker 偷 Task） ========== */

/*
 * 从 top 端窃取（FIFO）
 *
 * 前置条件：任何线程都可调用
 * 返回值：Task 指针，或 NULL（队列空或竞争失败）
 */
static inline struct KaulaTask* kaula_ws_deque_steal(KaulaWSDeque* q) {
    int64_t t = atomic_load_explicit(&q->top, memory_order_acquire);

    /* 读 bottom 的近似值（thief 不需要最新值） */
    int64_t b;
    memcpy(&b, &q->bottom, sizeof(b));
    atomic_thread_fence(memory_order_acquire);

    if (t >= b) {
        return NULL; /* 空 */
    }

    struct KaulaTask* task = atomic_load_explicit(
        &q->buffer[t & KAULA_WS_DEQUE_MASK],
        memory_order_acquire
    );

    int64_t expected = t;
    if (!atomic_compare_exchange_strong_explicit(
            &q->top,
            &expected,
            t + 1,
            memory_order_acquire,
            memory_order_relaxed)) {
        return NULL; /* CAS 失败：其他 thief 或 owner 先动了 */
    }

    return task;
}

/* ========== 查询 ========== */

/*
 * 获取队列大小（近似值，用于统计）
 */
static inline int64_t kaula_ws_deque_size(KaulaWSDeque* q) {
    int64_t t = atomic_load_explicit(&q->top, memory_order_relaxed);
    int64_t b;
    memcpy(&b, &q->bottom, sizeof(b));
    int64_t size = b - t;
    return size > 0 ? size : 0;
}

/*
 * 判断队列是否为空（近似值）
 */
static inline int kaula_ws_deque_empty(KaulaWSDeque* q) {
    return kaula_ws_deque_size(q) <= 0;
}

#endif /* KAULA_WS_DEQUE_H */
