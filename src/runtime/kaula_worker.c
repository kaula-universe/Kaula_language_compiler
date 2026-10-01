/*
 * Kaula 并发内核 · Worker 实现
 */

#include "kaula_worker.h"
#include "kaula_task.h"
#include "kaula_scheduler.h"
#include "kaula_iodriver.h"
#include "kaula_block_lane.h"

#include <stdlib.h>
#include <string.h>

/* IoDriver */
extern KaulaIoDriver g_iodriver;

/* Block Lane */
extern KaulaBlockLane g_block_lane;

/* ========== TLS 当前 Worker ========== */

static KaulaWorker* g_current_worker = NULL;

KaulaWorker* kaula_current_worker(void) {
    return g_current_worker;
}

void kaula_set_current_worker(KaulaWorker* w) {
    g_current_worker = w;
}

/* ========== Worker 线程入口 ========== */

static void* kaula_worker_thread_entry(void* arg) {
    KaulaWorker* w = (KaulaWorker*)arg;
    kaula_set_current_worker(w);
    kaula_worker_loop(w);
    return NULL;
}

/* ========== Worker 主循环 ========== */

void kaula_worker_loop(KaulaWorker* w) {
    while (atomic_load_explicit(&w->running, memory_order_acquire)) {
        KaulaTask* task = NULL;

        /* 步骤 1：从本地队列取 */
        task = kaula_ws_deque_pop(&w->local_queue);

        /* 步骤 2：本地空则尝试 steal */
        if (task == NULL) {
            w->steal_attempts++;
            task = kaula_scheduler_steal(w);
            if (task != NULL) {
                w->steal_count++;
            }
        }

        /* 步骤 3：仍然空则短暂休眠，避免空转 */
        if (task == NULL) {
            /* 短暂休眠，让出 CPU */
            kaula_sleep_us(100);
            continue;
        }

        /* 步骤 4：执行 Task */
        kaula_worker_execute_task(w, task);
        w->tasks_executed++;
    }
}

/* ========== Worker 执行 Task ========== */

void kaula_worker_execute_task(KaulaWorker* w, KaulaTask* task) {
    /* 检查 Task 是否已完成（从 Block Lane 返回的） */
    if (atomic_load_explicit(&task->state, memory_order_acquire) == KAULA_TASK_DONE) {
        /* 已完成，直接跳过 */
        w->current_task = NULL;
        return;
    }

    /* 设置当前 Task */
    w->current_task = task;
    atomic_store_explicit(&task->state, KAULA_TASK_RUNNING, memory_order_release);

    /* 重置预算 */
    kaula_budget_reset(&w->budget, w->budget.quantum > 0 ? w->budget.quantum : KAULA_DEFAULT_BUDGET_QUANTUM);

    /* 如果是 IO 完成后恢复，设置 TLS io_result */
    if (task->io_result != 0 || atomic_load_explicit(&task->state, memory_order_acquire) == KAULA_TASK_RUNNING) {
        g_kaula_current_io_result = task->io_result;
    }

    /* 执行循环 */
    for (;;) {
        /* 预算检查 */
        if (kaula_budget_tick(&w->budget)) {
            /* 预算耗尽：如果 Task 还没完成，重新入队 */
            if (atomic_load_explicit(&task->state, memory_order_acquire) == KAULA_TASK_RUNNING) {
                kaula_ws_deque_push_tail(&w->local_queue, task);
                atomic_store_explicit(&task->state, KAULA_TASK_READY, memory_order_release);
            }
            break;
        }

        /* 调用 resume 函数 */
        task->resume_count++;
        KaulaResumeResult result = task->resume_fn(task->frame);

        switch (result.status) {
        case KAULA_RESUME_DONE:
            /* 完成 */
            kaula_task_complete(task, result.value);
            w->current_task = NULL;
            return;

        case KAULA_RESUME_BUDGET_EXHAUSTED:
            /* Task 主动让出：重新入队 */
            kaula_ws_deque_push_tail(&w->local_queue, task);
            atomic_store_explicit(&task->state, KAULA_TASK_READY, memory_order_release);
            w->current_task = NULL;
            return;

        case KAULA_RESUME_SUSPEND:
            /* IO 挂起：提交给 IoDriver */
            task->io_request = result.io_request;
            kaula_iodriver_submit(&g_iodriver, task, result.io_request);
            w->current_task = NULL;
            return;

        case KAULA_RESUME_BLOCKED:
            /* 阻塞：提交给 Block Lane */
            task->io_request = result.io_request;
            kaula_block_lane_submit(&g_block_lane, task);
            w->current_task = NULL;
            return;
        }
    }

    w->current_task = NULL;
}

/* ========== Worker 生命周期 ========== */

int kaula_worker_create(KaulaWorker* w, uint32_t worker_id) {
    memset(w, 0, sizeof(KaulaWorker));
    w->worker_id = worker_id;
    w->budget.quantum = KAULA_DEFAULT_BUDGET_QUANTUM;
    atomic_store_explicit(&w->running, 1, memory_order_release);

    kaula_ws_deque_init(&w->local_queue);

    /* 创建线程 */
    int err = pthread_create(&w->thread, NULL, kaula_worker_thread_entry, w);
    if (err != 0) {
        return err;
    }

    return 0;
}

void kaula_worker_join(KaulaWorker* w) {
    pthread_join(w->thread, NULL);
}

void kaula_worker_stop(KaulaWorker* w) {
    atomic_store_explicit(&w->running, 0, memory_order_release);
}
