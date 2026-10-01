/*
 * Kaula 并发内核 · Block Lane 实现
 */

#include "kaula_block_lane.h"
#include "kaula_worker.h"
#include "../kmm_scoped_allocator_v4.h"

#include <stdlib.h>
#include <string.h>
#include <unistd.h>

/* ========== Block Lane 线程入口 ========== */

static void* block_lane_thread_entry(void* arg) {
    KaulaBlockLane* lane = (KaulaBlockLane*)arg;

    while (atomic_load_explicit(&lane->running, memory_order_acquire)) {
        /* 从队列取任务 */
        KaulaTask* task = (KaulaTask*)kaula_mpsc_queue_pop(&lane->task_queue);

        if (task == NULL) {
            /* 队列为空，短暂休眠 */
            usleep(100);
            continue;
        }

        /* 执行阻塞操作 */
        task->state = KAULA_TASK_BLOCKED;
        KaulaResumeResult result = task->resume_fn(task->frame);

        /* 完成后将 Task 送回 Worker */
        if (result.status == KAULA_RESUME_DONE) {
            task->result = result.value;
            atomic_store_explicit(&task->done_flag, 1, memory_order_release);
            task->state = KAULA_TASK_DONE;

            /* 推回原 Worker 的队列（用于回收） */
            KaulaWorker* owner = atomic_load_explicit(&task->owner, memory_order_acquire);
            if (owner != NULL) {
                kaula_ws_deque_push_tail(&owner->local_queue, task);
            }

            atomic_fetch_add(&lane->total_completions, 1);
        } else {
            /* 其他状态：不应该发生，但为了安全，完成任务 */
            task->result = result.value;
            atomic_store_explicit(&task->done_flag, 1, memory_order_release);
            task->state = KAULA_TASK_DONE;

            KaulaWorker* owner = atomic_load_explicit(&task->owner, memory_order_acquire);
            if (owner != NULL) {
                kaula_ws_deque_push_tail(&owner->local_queue, task);
            }
        }
    }

    return NULL;
}

/* ========== API 实现 ========== */

int kaula_block_lane_init(KaulaBlockLane* lane, uint32_t max_threads) {
    memset(lane, 0, sizeof(KaulaBlockLane));

    lane->max_threads = (max_threads > KAULA_BLOCK_LANE_MAX_THREADS)
                        ? KAULA_BLOCK_LANE_MAX_THREADS
                        : max_threads;

    kaula_mpsc_queue_init(&lane->task_queue);
    atomic_store_explicit(&lane->running, 0, memory_order_release);

    return 0;
}

int kaula_block_lane_start(KaulaBlockLane* lane) {
    atomic_store_explicit(&lane->running, 1, memory_order_release);

    /* 启动线程池（初始线程数 = min(2, max_threads)） */
    uint32_t initial_threads = (lane->max_threads < 2) ? lane->max_threads : 2;

    for (uint32_t i = 0; i < initial_threads; i++) {
        int err = pthread_create(&lane->threads[i], NULL, block_lane_thread_entry, lane);
        if (err != 0) {
            atomic_store_explicit(&lane->running, 0, memory_order_release);
            return err;
        }
        lane->thread_count++;
    }

    return 0;
}

void kaula_block_lane_stop(KaulaBlockLane* lane) {
    atomic_store_explicit(&lane->running, 0, memory_order_release);
}

void kaula_block_lane_join(KaulaBlockLane* lane) {
    for (uint32_t i = 0; i < lane->thread_count; i++) {
        pthread_join(lane->threads[i], NULL);
    }
}

void kaula_block_lane_destroy(KaulaBlockLane* lane) {
    /* 清理资源 */
}

void kaula_block_lane_submit(KaulaBlockLane* lane, KaulaTask* task) {
    /* 推入队列 */
    if (!kaula_mpsc_queue_push(&lane->task_queue, task)) {
        /* 队列满，直接在当前线程执行（降级） */
        task->state = KAULA_TASK_BLOCKED;
        KaulaResumeResult result = task->resume_fn(task->frame);

        if (result.status == KAULA_RESUME_DONE) {
            task->result = result.value;
            atomic_store_explicit(&task->done_flag, 1, memory_order_release);
            task->state = KAULA_TASK_DONE;
        }
        return;
    }

    atomic_fetch_add(&lane->total_submits, 1);
}
