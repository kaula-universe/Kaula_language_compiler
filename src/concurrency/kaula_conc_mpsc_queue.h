/*
 * Kaula 并发内核 · MPSC 有界队列
 *
 * 用途：Worker → IoDriver 的 IO 请求提交、Worker → Block Lane 的阻塞任务。
 * 多生产者（各 Worker），单消费者（IoDriver / Block Lane 主循环）。
 *
 * 关键修复（对应缺陷 #5）：
 * 原实现在 pop 里对槽位做无界自旋：
 *
 *     while (item == NULL) { item = re-read-same-slot; }
 *
 * 而 push 是"先 CAS 推进 tail，再写槽位"两步。两者之间存在窗口，
 * 生产者若在窗口内被抢占（或线程被取消），消费者就永久自旋，
 * 连带 shutdown 也退不出来。
 *
 * 修复：每个槽位带独立的 sequence 号（Vyukov 有界 MPMC 队列）。
 * 生产者写数据 + 发布 sequence；消费者只在 sequence 就绪时才消费，
 * 读不到就返回 NULL，绝不等待。因此 pop 恒为 O(1) 且不会卡死。
 */

#ifndef KAULA_CONC_MPSC_QUEUE_H
#define KAULA_CONC_MPSC_QUEUE_H

#include <stdint.h>
#include <stddef.h>
#include <string.h>
#include <stdatomic.h>

#include "kaula_conc_platform.h"

/* 容量必须为 2 的幂（I3） */
#ifndef KAULA_MPSC_CAPACITY
    #define KAULA_MPSC_CAPACITY 131072
#endif
#define KAULA_MPSC_MASK (KAULA_MPSC_CAPACITY - 1)

#if (KAULA_MPSC_CAPACITY & (KAULA_MPSC_CAPACITY - 1)) != 0
    #error "KAULA_MPSC_CAPACITY 必须是 2 的幂"
#endif

typedef struct {
    _Atomic(void*) value;
    /* sequence：槽位状态机，值为"该位置期望的入队序号" */
    _Atomic(uint64_t) seq;
} kaula_mpsc_cell_t;

typedef struct {
    /* head：消费者专用 */
    _Atomic(uint64_t) head;
    char                _pad0[KAULA_CACHE_LINE - sizeof(uint64_t)];

    /* tail：生产者通过 CAS 竞争 */
    _Atomic(uint64_t) tail;
    char                _pad1[KAULA_CACHE_LINE - sizeof(uint64_t)];

    kaula_mpsc_cell_t    cells[KAULA_MPSC_CAPACITY];
} kaula_mpsc_queue_t;

static inline void kaula_mpsc_queue_init(kaula_mpsc_queue_t* q) {
    atomic_store_explicit(&q->head, 0, memory_order_relaxed);
    atomic_store_explicit(&q->tail, 0, memory_order_relaxed);
    for (uint64_t i = 0; i < KAULA_MPSC_CAPACITY; i++) {
        atomic_store_explicit(&q->cells[i].value, NULL, memory_order_relaxed);
        atomic_store_explicit(&q->cells[i].seq, i, memory_order_relaxed);
    }
}

/*
 * 推入元素。
 * 返回 1 = 成功，0 = 队列满（调用方必须处理背压，不得忽略）。
 */
static inline int kaula_mpsc_queue_push(kaula_mpsc_queue_t* q, void* item) {
    kaula_mpsc_cell_t* cell;
    uint64_t pos = atomic_load_explicit(&q->tail, memory_order_relaxed);

    for (;;) {
        cell = &q->cells[pos & KAULA_MPSC_MASK];
        /* 读该槽位的 sequence：等于 pos 说明空着可写 */
        uint64_t seq = atomic_load_explicit(&cell->seq, memory_order_acquire);

        int64_t diff = (int64_t)(seq - pos);
        if (diff == 0) {
            /* 槽位就绪，尝试占位 */
            if (atomic_compare_exchange_weak_explicit(
                    &q->tail, &pos, pos + 1,
                    memory_order_relaxed, memory_order_relaxed)) {
                break; /* 占位成功 */
            }
            /* CAS 失败，pos 已被更新为新的 tail，重试 */
        } else if (diff < 0) {
            return 0; /* 队列满 */
        } else {
            /* 被别的生产者抢走了这个位置，重读 tail */
            pos = atomic_load_explicit(&q->tail, memory_order_relaxed);
        }
    }

    /* 槽位已归我：写值 + 发布 sequence */
    atomic_store_explicit(&cell->value, item, memory_order_relaxed);
    atomic_store_explicit(&cell->seq, pos + 1, memory_order_release);
    return 1;
}

/*
 * 弹出元素（仅单消费者调用）。
 * 返回元素指针，NULL 表示队列空。
 *
 * 关键：绝不等待。槽位尚未发布就返回 NULL。
 */
static inline void* kaula_mpsc_queue_pop(kaula_mpsc_queue_t* q) {
    uint64_t head = atomic_load_explicit(&q->head, memory_order_relaxed);
    kaula_mpsc_cell_t* cell = &q->cells[head & KAULA_MPSC_MASK];

    uint64_t seq = atomic_load_explicit(&cell->seq, memory_order_acquire);
    if ((int64_t)(seq - (head + 1)) != 0) {
        /* 槽位未就绪 = 队列空 */
        return NULL;
    }

    void* item = atomic_load_explicit(&cell->value, memory_order_relaxed);

    /* 释放槽位：sequence 前进一整圈，供下一个生产者复用 */
    atomic_store_explicit(&cell->seq, head + KAULA_MPSC_CAPACITY, memory_order_release);
    atomic_store_explicit(&q->head, head + 1, memory_order_relaxed);
    return item;
}

/* 近似大小（用于统计） */
static inline uint64_t kaula_mpsc_queue_size(const kaula_mpsc_queue_t* q) {
    uint64_t h = atomic_load_explicit(&q->head, memory_order_relaxed);
    uint64_t t = atomic_load_explicit(&q->tail, memory_order_relaxed);
    return t > h ? t - h : 0;
}

static inline int kaula_mpsc_queue_empty(const kaula_mpsc_queue_t* q) {
    return kaula_mpsc_queue_size(q) == 0;
}

#endif /* KAULA_CONC_MPSC_QUEUE_H */