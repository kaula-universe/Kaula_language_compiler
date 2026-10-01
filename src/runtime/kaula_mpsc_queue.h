/*
 * Kaula 并发内核 · MPSC 无锁队列
 *
 * Multi-Producer Single-Consumer 队列
 * 用于 Worker → IoDriver 的 IO 请求提交
 */

#ifndef KAULA_MPSC_QUEUE_H
#define KAULA_MPSC_QUEUE_H

#include <stdint.h>
#include <stddef.h>
#include <stdatomic.h>
#include <string.h>

/* 缓存行大小 */
#ifndef KAULA_CACHE_LINE
#define KAULA_CACHE_LINE 64
#endif

/* 队列容量：必须为 2 的幂 */
#define KAULA_MPSC_CAPACITY 8192
#define KAULA_MPSC_MASK (KAULA_MPSC_CAPACITY - 1)

/* 队列元素类型（void*） */
typedef void* KaulaMpscItem;

/*
 * MPSC 无锁队列
 *
 * 多个生产者（Worker）可以并发 push
 * 单个消费者（IoDriver）pop
 *
 * 实现：基于数组的环形缓冲区，使用 CAS 推进 tail
 */
typedef struct {
    /* 分离缓存行，避免 false sharing */
    _Atomic(uint64_t)     head;                              /* 消费者读写 */
    char                  _pad1[KAULA_CACHE_LINE - sizeof(uint64_t)];

    _Atomic(uint64_t)     tail;                              /* 生产者竞争写 */
    char                  _pad2[KAULA_CACHE_LINE - sizeof(uint64_t)];

    _Atomic(KaulaMpscItem) buffer[KAULA_MPSC_CAPACITY];     /* 环形缓冲区 */
} KaulaMpscQueue __attribute__((aligned(KAULA_CACHE_LINE)));

/* ========== 初始化 ========== */

static inline void kaula_mpsc_queue_init(KaulaMpscQueue* q) {
    atomic_store_explicit(&q->head, 0, memory_order_relaxed);
    atomic_store_explicit(&q->tail, 0, memory_order_relaxed);
    memset((void*)q->buffer, 0, sizeof(q->buffer));
}

/* ========== 生产者操作（多生产者并发安全） ========== */

/*
 * 推入一个元素
 *
 * 返回值：1 = 成功，0 = 队列满
 */
static inline int kaula_mpsc_queue_push(KaulaMpscQueue* q, KaulaMpscItem item) {
    uint64_t tail = atomic_load_explicit(&q->tail, memory_order_relaxed);

    /* 检查队列是否满 */
    uint64_t head = atomic_load_explicit(&q->head, memory_order_acquire);
    if (tail - head >= KAULA_MPSC_CAPACITY) {
        return 0; /* 满 */
    }

    /* CAS 推进 tail，预留一个槽位 */
    while (!atomic_compare_exchange_weak_explicit(
            &q->tail, &tail, tail + 1,
            memory_order_acq_rel, memory_order_relaxed)) {
        /* CAS 失败：其他生产者先推进了 tail */
        /* 重新检查是否满 */
        head = atomic_load_explicit(&q->head, memory_order_acquire);
        if (tail - head >= KAULA_MPSC_CAPACITY) {
            return 0; /* 满 */
        }
    }

    /* 写入元素到预留的槽位 */
    atomic_store_explicit(&q->buffer[tail & KAULA_MPSC_MASK], item, memory_order_release);

    return 1;
}

/* ========== 消费者操作（仅单消费者调用） ========== */

/*
 * 弹出一个元素
 *
 * 返回值：元素指针，或 NULL（队列空）
 * 注意：仅消费者线程调用
 */
static inline KaulaMpscItem kaula_mpsc_queue_pop(KaulaMpscQueue* q) {
    uint64_t head = atomic_load_explicit(&q->head, memory_order_relaxed);
    uint64_t tail = atomic_load_explicit(&q->tail, memory_order_acquire);

    if (head >= tail) {
        return NULL; /* 空 */
    }

    /* 读取元素 */
    KaulaMpscItem item = atomic_load_explicit(
        &q->buffer[head & KAULA_MPSC_MASK], memory_order_acquire);

    /* 等待生产者写入完成（元素可能还在写入中） */
    while (item == NULL) {
        /* 短暂等待 */
        item = atomic_load_explicit(
            &q->buffer[head & KAULA_MPSC_MASK], memory_order_acquire);
    }

    /* 推进 head */
    atomic_store_explicit(&q->head, head + 1, memory_order_release);

    return item;
}

/* ========== 查询 ========== */

/*
 * 获取队列大小（近似值）
 */
static inline uint64_t kaula_mpsc_queue_size(KaulaMpscQueue* q) {
    uint64_t head = atomic_load_explicit(&q->head, memory_order_relaxed);
    uint64_t tail = atomic_load_explicit(&q->tail, memory_order_relaxed);
    return tail > head ? tail - head : 0;
}

/*
 * 判断队列是否为空
 */
static inline int kaula_mpsc_queue_empty(KaulaMpscQueue* q) {
    return kaula_mpsc_queue_size(q) == 0;
}

#endif /* KAULA_MPSC_QUEUE_H */
