/*
 * Kaula 并发内核 · 阻塞车道实现
 */

#include "kaula_conc_block_lane.h"
#include "kaula_conc_iodriver.h"
#include "kaula_conc_scheduler.h"
#include "kaula_conc_worker.h"

#include <stdlib.h>
#include <string.h>

KaulaBlockLane g_conc_block_lane;

/* ========== 车道线程 ========== */

static void block_lane_thread(void* arg) {
    KaulaBlockLane* lane = (KaulaBlockLane*)arg;

    while (atomic_load_explicit(&lane->running, memory_order_acquire)) {
        KaulaTask* task = (KaulaTask*)kaula_mpsc_queue_pop(&lane->task_queue);

        if (task == NULL) {
            /* 空闲：park 到通知事件上，不轮询 */
            atomic_fetch_add_explicit(&lane->idle_count, 1, memory_order_acq_rel);

            /* 缩容判断：空闲线程数超过下限且线程总数 > min，则自行退出 */
            uint32_t alive = atomic_load_explicit(&lane->thread_count, memory_order_acquire);
            uint32_t idle = atomic_load_explicit(&lane->idle_count, memory_order_acquire);

            if (alive > KAULA_BLOCK_LANE_MIN_THREADS && idle > 1) {
                atomic_fetch_sub_explicit(&lane->thread_count, 1, memory_order_acq_rel);
                atomic_fetch_sub_explicit(&lane->idle_count, 1, memory_order_acq_rel);
                break;
            }

            /* 等待通知，超时 200ms 兜底复查 shrinking */
            kaula_p_event_wait(&lane->notify, 200000000ULL);

            atomic_fetch_sub_explicit(&lane->idle_count, 1, memory_order_acq_rel);
            continue;
        }

        atomic_store_explicit(&task->state, KAULA_TASK_BLOCKED, memory_order_release);

        /*
         * 执行阻塞工作。
         * 关键：resume_fn 可能返回 BLOCKED（任务内部又遇到阻塞），
         * 这时必须继续在车道上跑完 —— 它本来就不是可挂起的 IO。
         */
        for (;;) {
            KaulaResumeResult r = task->resume_fn(task->frame);

            if (r.status == KAULA_RESUME_DONE) {
                kaula_task_complete(task, r.value);
                break;
            }
            if (r.status == KAULA_RESUME_SUSPEND) {
                /*
                 * 阻塞车道里遇到 IO 挂起：交回 IoDriver，
                 * 完成后再回 Worker。车道线程不占用。
                 */
                task->io_request = r.io_request;
                atomic_store_explicit(&task->state, KAULA_TASK_SUSPENDED,
                                      memory_order_release);
                if (kaula_iodriver_submit(task) == 0) {
                    break;
                }
                /* 提交失败：就地完成这次 IO 再继续 */
                kaula_conc_tls_io_result = kaula_iodriver_blocking_wait(&r.io_request);
                continue;
            }
            /* BUDGET_EXHAUSTED / BLOCKED：车道内忽略预算，继续到 Done */
        }

        atomic_fetch_add_explicit(&lane->total_completions, 1, memory_order_relaxed);

        /* 任务终结：释放"在手"名额，伸缩判据随之回落 */
        atomic_fetch_sub_explicit(&lane->busy_count, 1, memory_order_acq_rel);
    }
}

/* 尝试新建一个车道线程（动态伸缩，文档 §7.2） */
static int block_lane_spawn_thread(KaulaBlockLane* lane) {
    uint32_t alive = atomic_load_explicit(&lane->thread_count, memory_order_acquire);
    if (alive >= lane->max_threads) return KAULA_ERR_WOULD_BLOCK;

    /* 抢一个名额 */
    if (!atomic_compare_exchange_strong_explicit(
            &lane->thread_count, &alive, alive + 1,
            memory_order_acq_rel, memory_order_acquire)) {
        /* CAS 失败：可能已有人加了线程，重试一次即可 */
        alive = atomic_load_explicit(&lane->thread_count, memory_order_acquire);
        if (alive >= lane->max_threads) return KAULA_ERR_WOULD_BLOCK;
    }

    kaula_p_thread_t t;
    if (kaula_p_thread_create(&t, block_lane_thread, lane) != 0) {
        atomic_fetch_sub_explicit(&lane->thread_count, 1, memory_order_acq_rel);
        return KAULA_ERR_OOM;
    }

    /* 新线程不参与 join（会自行退出），close 掉句柄避免泄漏 */
#if KAULA_PLATFORM_WINDOWS
    CloseHandle(t);
#else
    (void)t;
#endif

    atomic_fetch_add_explicit(&lane->total_spawned_threads, 1, memory_order_relaxed);
    return 0;
}

/* ========== 生命周期 ========== */

int kaula_block_lane_init(uint32_t max_threads) {
    KaulaBlockLane* lane = &g_conc_block_lane;
    memset(lane, 0, sizeof(*lane));

    if (max_threads == 0) max_threads = KAULA_BLOCK_LANE_MAX_THREADS;
    if (max_threads > KAULA_BLOCK_LANE_MAX_THREADS) {
        max_threads = KAULA_BLOCK_LANE_MAX_THREADS;
    }
    if (max_threads < KAULA_BLOCK_LANE_MIN_THREADS) {
        max_threads = KAULA_BLOCK_LANE_MIN_THREADS;
    }
    lane->max_threads = max_threads;

    kaula_mpsc_queue_init(&lane->task_queue);
    atomic_store_explicit(&lane->thread_count, 0, memory_order_relaxed);
    atomic_store_explicit(&lane->idle_count, 0, memory_order_relaxed);
    atomic_store_explicit(&lane->running, 0, memory_order_release);
    atomic_store_explicit(&lane->stopping, 0, memory_order_release);
    return 0;
}

int kaula_block_lane_start(void) {
    KaulaBlockLane* lane = &g_conc_block_lane;

    if (kaula_p_event_create(&lane->notify) != 0) {
        return -1;
    }
    atomic_store_explicit(&lane->running, 1, memory_order_release);
    atomic_store_explicit(&lane->stopping, 0, memory_order_release);

    /* 预热最小线程数 */
    for (uint32_t i = 0; i < KAULA_BLOCK_LANE_MIN_THREADS; i++) {
        if (block_lane_spawn_thread(lane) != 0) break;
    }
    return 0;
}

void kaula_block_lane_stop(void) {
    KaulaBlockLane* lane = &g_conc_block_lane;
    atomic_store_explicit(&lane->running, 0, memory_order_release);
    atomic_store_explicit(&lane->stopping, 1, memory_order_release);
    /* 唤醒所有 park 的车道线程 */
    for (uint32_t i = 0; i < KAULA_BLOCK_LANE_MAX_THREADS; i++) {
        kaula_p_event_signal(&lane->notify);
    }
}

void kaula_block_lane_join(void) {
    /*
     * 车道线程会自行退出（park 超时后复查 running）。
     * 此处只等一小段时间让它们看到标志并退出，
     * 不做严格 join —— 它们不持有需要同步的资源。
     */
    kaula_p_sleep_ns(30000000ULL);  /* 30ms */
}

void kaula_block_lane_destroy(void) {
    KaulaBlockLane* lane = &g_conc_block_lane;
    if (lane->notify != 0) {
        kaula_p_event_destroy(&lane->notify);
        lane->notify = 0;
    }
}

/* ========== 提交 ========== */

int kaula_block_lane_submit(KaulaTask* task) {
    KaulaBlockLane* lane = &g_conc_block_lane;

    if (!atomic_load_explicit(&lane->running, memory_order_acquire)) {
        return KAULA_ERR_SHUTDOWN;
    }

    if (!kaula_mpsc_queue_push(&lane->task_queue, task)) {
        /* 队列满：返回失败，绝不在 Worker 上原地阻塞（那会破坏 I5） */
        atomic_fetch_add_explicit(&lane->total_would_block, 1, memory_order_relaxed);
        return KAULA_ERR_WOULD_BLOCK;
    }

    atomic_fetch_add_explicit(&lane->total_submits, 1, memory_order_relaxed);

    /*
     * 动态伸缩（文档 §7.2 的 0..max_threads）。
     *
     * 判据是"在手任务数 ≥ 线程数"。
     *
     * 不能用 idle_count：那是在线程 park 前后才递减的，
     * 提交路径读到的常是陈旧值（线程还没醒），永远判成"不忙"。
     * 也不能只看队列深度：Worker 取任务很快，
     * 提交第 2、3 个任务时队列可能已经空了，
     * 于是 4 个并发阻塞只长出 2 条线程，实测串行成 125ms。
     *
     * busy_count 在提交时加一、任务终结时减一，
     * 精确表示"有多少阻塞工作正在占用车道线程"。
     */
    uint32_t busy = atomic_fetch_add_explicit(&lane->busy_count, 1,
                                              memory_order_acq_rel) + 1;
    uint32_t alive = atomic_load_explicit(&lane->thread_count, memory_order_acquire);
    while (busy > alive && block_lane_spawn_thread(lane) == 0) {
        alive = atomic_load_explicit(&lane->thread_count, memory_order_acquire);
    }

    kaula_p_event_signal(&lane->notify);
    return 0;
}

uint32_t kaula_block_lane_thread_count(void) {
    return atomic_load(&g_conc_block_lane.thread_count);
}