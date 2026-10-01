/*
 * Kaula 并发内核 · 调度器实现
 */

#include "kaula_scheduler.h"
#include "kaula_worker.h"
#include "kaula_task.h"
#include "kaula_iodriver.h"
#include "../kmm_scoped_allocator_v4.h"

#include <stdlib.h>
#include <string.h>

/* ========== 全局状态 ========== */

/* Worker 数组 */
extern KaulaWorker g_workers[];
extern uint32_t g_worker_count;

/* 全局注入队列 */
extern KaulaInjectQueue g_inject_queue;

/* 全局 Task ID 计数器 */
extern _Atomic(uint64_t) g_next_task_id;

/* IoDriver */
extern KaulaIoDriver g_iodriver;

/* TLS: 当前 Task 的 IO 结果 */
__thread int g_kaula_current_io_result = 0;

/* ========== Task 实现 ========== */

KaulaTask* kaula_task_alloc(void) {
    /* 使用 KMM V4 survivor 段分配（跨 scope 存活） */
    KaulaTask* task = (KaulaTask*)kmm_v4_alloc_global(sizeof(KaulaTask));
    if (task == NULL) return NULL;

    memset(task, 0, sizeof(KaulaTask));
    task->task_id = atomic_fetch_add(&g_next_task_id, 1);
    atomic_store_explicit(&task->state, KAULA_TASK_CREATED, memory_order_relaxed);
    task->created_ns = kaula_get_ticks();

    return task;
}

void kaula_task_init(KaulaTask* task, KaulaResumeFn resume_fn, void* frame, size_t frame_size) {
    task->resume_fn = resume_fn;
    task->frame = frame;
    task->frame_size = frame_size;
}

void kaula_task_complete(KaulaTask* task, void* result) {
    task->result = result;
    atomic_store_explicit(&task->done_flag, 1, memory_order_release);
    atomic_store_explicit(&task->state, KAULA_TASK_DONE, memory_order_release);
}

void kaula_task_recycle(KaulaTask* task) {
    /* 帧回收 */
    if (task->frame != NULL && task->frame_size > 0) {
        kmm_v4_survivor_free(task->frame);
    }
    /* Task 结构本身回收到 KMM */
    kmm_v4_survivor_free(task);
}

/* ========== Handle 实现 ========== */

void* kaula_handle_await(KaulaHandle h) {
    if (h.task == NULL) return NULL;

    /* 轮询等待完成 */
    while (!atomic_load_explicit(&h.task->done_flag, memory_order_acquire)) {
        /* 如果当前线程是 Worker，尝试帮忙执行任务（work-stealing） */
        KaulaWorker* w = kaula_current_worker();
        if (w != NULL) {
            KaulaTask* task = kaula_ws_deque_pop(&w->local_queue);
            if (task == NULL) {
                task = kaula_scheduler_steal(w);
            }
            if (task != NULL) {
                kaula_worker_execute_task(w, task);
                continue;
            }
        }
        /* 短暂休眠，避免空转 */
        kaula_sleep_us(10);
    }

    return h.task->result;
}

int kaula_handle_poll(KaulaHandle h) {
    if (h.task == NULL) return 0;
    return atomic_load_explicit(&h.task->done_flag, memory_order_acquire);
}

/* ========== 全局注入队列 ========== */

void kaula_inject_queue_init(KaulaInjectQueue* q) {
    atomic_store_explicit(&q->head, 0, memory_order_relaxed);
    atomic_store_explicit(&q->tail, 0, memory_order_relaxed);
    memset((void*)q->buffer, 0, sizeof(q->buffer));
}

int kaula_inject_queue_push(KaulaInjectQueue* q, KaulaTask* task) {
    uint64_t tail = atomic_load_explicit(&q->tail, memory_order_relaxed);
    uint64_t head = atomic_load_explicit(&q->head, memory_order_acquire);

    if (tail - head >= KAULA_INJECT_QUEUE_CAPACITY) {
        return 0; /* 满 */
    }

    atomic_store_explicit(&q->buffer[tail & KAULA_INJECT_QUEUE_MASK], task, memory_order_relaxed);
    atomic_store_explicit(&q->tail, tail + 1, memory_order_release);

    return 1;
}

KaulaTask* kaula_inject_queue_pop(KaulaInjectQueue* q) {
    uint64_t head = atomic_load_explicit(&q->head, memory_order_relaxed);
    uint64_t tail = atomic_load_explicit(&q->tail, memory_order_acquire);

    if (head >= tail) {
        return NULL; /* 空 */
    }

    KaulaTask* task = atomic_load_explicit(&q->buffer[head & KAULA_INJECT_QUEUE_MASK], memory_order_relaxed);
    atomic_store_explicit(&q->head, head + 1, memory_order_release);

    return task;
}

/* ========== Spawn ========== */

KaulaHandle kaula_spawn(KaulaResumeFn resume_fn, void* frame, size_t frame_size) {
    KaulaWorker* w = kaula_current_worker();

    /* 如果当前线程不是 Worker，直接同步执行（降级为顺序调用） */
    if (w == NULL) {
        KaulaTask* task = kaula_task_alloc();
        if (task == NULL) {
            return kaula_handle_null();
        }
        kaula_task_init(task, resume_fn, frame, frame_size);

        /* 直接执行（循环直到完成） */
        void* result = NULL;
        for (;;) {
            KaulaResumeResult r = resume_fn(frame);
            if (r.status == KAULA_RESUME_DONE) {
                result = r.value;
                break;
            }
            /* BUDGET_EXHAUSTED / SUSPEND / BLOCKED：继续执行 */
        }
        kaula_task_complete(task, result);
        return kaula_handle_from_task(task);
    }

    /* 分配 Task */
    KaulaTask* task = kaula_task_alloc();
    if (task == NULL) {
        return kaula_handle_null();
    }

    kaula_task_init(task, resume_fn, frame, frame_size);

    /* 设置 owner */
    atomic_store_explicit(&task->owner, w, memory_order_relaxed);

    /* 尝试推入本地队列 */
    if (kaula_ws_deque_push_tail(&w->local_queue, task)) {
        atomic_store_explicit(&task->state, KAULA_TASK_READY, memory_order_release);
        return kaula_handle_from_task(task);
    }

    /* 本地队列满：推入全局注入队列 */
    if (kaula_inject_queue_push(&g_inject_queue, task)) {
        atomic_store_explicit(&task->state, KAULA_TASK_READY, memory_order_release);
        return kaula_handle_from_task(task);
    }

    /* 全部失败：回收 Task，返回空 handle */
    kaula_task_recycle(task);
    return kaula_handle_null();
}

/* ========== Steal ========== */

KaulaTask* kaula_scheduler_steal(KaulaWorker* w) {
    if (g_worker_count <= 1) {
        /* 只有一个 Worker：尝试从全局注入队列取 */
        return kaula_inject_queue_pop(&g_inject_queue);
    }

    /* 随机起点，避免所有 Worker 都偷同一个 */
    uint32_t start = w->worker_id;
    uint32_t n = g_worker_count;

    for (uint32_t i = 0; i < n; i++) {
        uint32_t idx = (start + i) % n;
        if (idx == w->worker_id) continue;

        KaulaWorker* victim = &g_workers[idx];
        KaulaTask* task = kaula_ws_deque_steal(&victim->local_queue);
        if (task != NULL) {
            /* 窃取成功：更新 owner */
            atomic_store_explicit(&task->owner, w, memory_order_relaxed);
            task->steal_count++;
            return task;
        }
    }

    /* 所有 Worker 都空：尝试全局注入队列 */
    return kaula_inject_queue_pop(&g_inject_queue);
}
