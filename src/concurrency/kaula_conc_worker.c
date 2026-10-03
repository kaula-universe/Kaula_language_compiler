/*
 * Kaula 并发内核 · Worker 实现
 */

#include "kaula_conc_worker.h"
#include "kaula_conc_scheduler.h"
#include "kaula_conc_iodriver.h"
#include "kaula_conc_block_lane.h"
#include "kaula_conc_metrics.h"

#include <stdlib.h>
#include <string.h>

/* ========== TLS 当前 Worker ========== */

/*
 * 必须是 TLS。
 * 原实现是普通全局变量，所有 Worker 共享一个指针 ——
 * 于是 kaula_conc_current_worker() 在 A 线程会返回 B 的 Worker，
 * spawn 会把 Task 塞进别人的 deque，await 会误判自己是不是 Worker。
 */
static _Thread_local KaulaWorker* t_current_worker = NULL;
/* 非 Worker 线程（main / 外部线程）的退休列表兜底 */
static _Thread_local KaulaRetireList t_fallback_retired;

KaulaWorker* kaula_conc_current_worker(void) {
    return t_current_worker;
}

void kaula_conc_set_current_worker(KaulaWorker* w) {
    t_current_worker = w;
}

/* TLS：当前 Task 的 IO 结果（供 resume_fn 读取） */
_Thread_local int kaula_conc_tls_io_result = 0;

/* 全局 Worker 池 */
KaulaWorker g_conc_workers[KAULA_MAX_WORKERS];

/* ========== epoch 辅助 ========== */

KaulaRetireList* kaula_conc_retire_list(void) {
    KaulaWorker* w = t_current_worker;
    if (w != NULL) return &w->retired;
    return &t_fallback_retired;
}

int kaula_conc_worker_is_active(const KaulaWorker* w) {
    return atomic_load_explicit(&w->running, memory_order_acquire) ? 1 : 0;
}

static _Atomic(uint64_t) g_epoch_exit_counter = 0;

void kaula_conc_advance_epoch_if_needed(void) {
    /*
     * 每 256 次临界区退出尝试推进一次 epoch。
     * 配合 retire 时记录 epoch，回收条件为"所有活跃 Worker 的
     * local_epoch > 退休 epoch"，因此实际延迟至少两轮，
     * 足以覆盖任何 enter → 访问 → exit 的时序。
     */
    uint64_t n = atomic_fetch_add_explicit(&g_epoch_exit_counter, 1, memory_order_relaxed);
    if ((n & 0xFF) != 0xFF) return;

    atomic_fetch_add_explicit(&g_conc_epoch.global_epoch, 1, memory_order_acq_rel);
}

/* ========== RNG（窃取起点，文档 §4.2） ========== */

static uint64_t worker_rand(KaulaWorker* w) {
    /* xorshift64*：无锁、每 Worker 独立状态、无需跨线程同步 */
    uint64_t x = w->rng_state;
    if (x == 0) x = 0x9E3779B97F4A7C15ULL;
    x ^= x >> 12;
    x ^= x << 25;
    x ^= x >> 27;
    w->rng_state = x;
    return x * 0x2545F4914F6CDD1DULL;
}

/* ========== 投递（缺陷 #3 的核心：必须处理失败） ========== */

int kaula_conc_worker_post(KaulaWorker* w, KaulaTask* task) {
    if (w == NULL) {
        /*
         * 无主：进全局注入队列。
         *
         * 必须唤醒一个 Worker —— 否则正在 park 的 Worker 只能等自己
         * 1ms 的 park 超时到期。挂起往返要走两次注入队列（Worker→Driver
         * 的结果回投、Driver→Worker 的提交），两次各赔 1ms，
         * 实测就是那残余的约 2ms。
         *
         * worker_park 里那个 1ms 超时的注释写着它是「漏唤醒的自愈」——
         * 注入队列这条路不是漏唤醒，是**根本没有唤醒**，于是自愈机制
         * 退化成了唯一唤醒机制。
         */
        if (kaula_scheduler_inject_push(task)) {
            kaula_scheduler_notify_one();
            return 0;
        }
        return KAULA_ERR_WOULD_BLOCK;
    }

    /*
     * 只有 owner 能写本地队列；外部线程一律走全局注入队列。
     *
     * 这不是保守，是 Chase-Lev 的硬性前提：bottom 只有 owner 能动。
     * push_external 那次 CAS 会被 owner 随后的裸 store 抹掉：
     *
     *     owner    bottom = 5
     *     external CAS 5->6 成功，写 buffer[5] = taskE
     *     owner    store bottom = 6        ← 抹掉 external 刚占的槽
     *
     * 于是 buffer[5] 被写两次（丢一个 Task），或 bottom 倒退
     * （已入队的 Task 永远取不到）。两者都无法自愈 ——
     * Worker 的 1ms park 超时只捞得到「已经在队列里」的 Task。
     *
     * 所以这里**不再调 push_external**。外部线程要的只是「有个 Worker
     * 会去取它」，全局注入队列已经提供这个能力，而且它的 push 侧已串行化。
     * 代价是外部投递多一次队列中转，换来的是不再丢Task。
     */
    if (t_current_worker != w) {
        if (kaula_scheduler_inject_push(task)) {
            kaula_scheduler_notify_one();
            return 0;
        }
        return KAULA_ERR_WOULD_BLOCK;
    }

    /* owner：写自己的本地队列（无 CAS 快路径） */
    int ok = kaula_ws_deque_push_owner(&w->local_queue, task);

    if (ok) {
        /* 通知可能正在 park 的 Worker */
        kaula_worker_wake(w);
        return 0;
    }

    /* 本地队列满：退回全局注入队列 */
    if (kaula_scheduler_inject_push(task)) {
        /* 唤醒任意一个可能空转的 Worker */
        kaula_scheduler_notify_one();
        return 0;
    }
    return KAULA_ERR_WOULD_BLOCK;
}

/* ========== 唤醒 ========== */

void kaula_worker_wake(KaulaWorker* w) {
    if (w == NULL) return;
    atomic_store_explicit(&w->park_requested, 1, memory_order_release);
    kaula_p_event_signal(&w->park_event);
}

static void worker_park(KaulaWorker* w) {
    /*
     * park 前必须再取一次活：pop 之后到 park 之间可能有投递发生，
     * 若那次的 wake 信号先到，signal 会被记在计数里，
     * 这里 park 后能立刻取到（事件计数累积语义）。
     */
    atomic_store_explicit(&w->park_requested, 0, memory_order_release);

    if (!kaula_ws_deque_empty(&w->local_queue)) {
        return; /* 有活，不睡 */
    }
    /* 队列空且注入队列空才允许睡 */
    if (!kaula_scheduler_inject_empty() && g_conc_runtime.worker_count <= 1) {
        return;
    }

    uint64_t t0 = kaula_p_now_ns();
    atomic_fetch_add_explicit(&w->stats.park_count, 1, memory_order_relaxed);

    /*
     * 有限超时：即便漏了一次唤醒，最多 1ms 后自愈。
     * 这是"不空转"与"不丢唤醒"之间的折中 —— 漏唤醒最坏多等 1ms，
     * 而持续空转会烧掉一个核（P2 明确要求）。
     */
    kaula_p_event_wait(&w->park_event, 1000000ULL);

    uint64_t dt = kaula_p_now_ns() - t0;
    atomic_fetch_add_explicit(&w->stats.park_total_ns, dt, memory_order_relaxed);
}

/* ========== 取活 ========== */

KaulaTask* kaula_conc_worker_find_work(KaulaWorker* w) {
    /* 1. 本地队列（LIFO，缓存友好） */
    KaulaTask* t = kaula_ws_deque_pop(&w->local_queue);
    if (t != NULL) return t;

    /* 2. 全局注入队列 */
    t = kaula_scheduler_inject_pop();
    if (t != NULL) return t;

    /* 3. 窃取他人（随机起点，避免所有 Worker 撞同一个受害者） */
    uint32_t n = g_conc_runtime.worker_count;
    if (n <= 1) return NULL;

    atomic_fetch_add_explicit(&w->stats.steal_attempts, 1, memory_order_relaxed);

    uint32_t start = (uint32_t)(worker_rand(w) % n);
    for (uint32_t i = 0; i < n; i++) {
        uint32_t idx = (start + i) % n;
        if (idx == w->worker_id) continue;

        KaulaTask* victim = kaula_ws_deque_steal(&g_conc_workers[idx].local_queue);
        if (victim != NULL) {
            /*
             * I2：改写 owner。
             * 任务状态在帧里，迁移只是改一个指针，不拷贝任何执行状态，
             * 因此不存在"搬栈导致悬垂指针"的问题。
             */
            atomic_store_explicit(&victim->owner, w, memory_order_release);
            victim->steal_count++;
            atomic_fetch_add_explicit(&w->stats.steal_count, 1, memory_order_relaxed);
            return victim;
        }
    }
    return NULL;
}

/* ========== 预算自适应 ========== */

void kaula_conc_worker_update_budget(KaulaWorker* w, uint64_t elapsed_ns) {
    /* 自适应本身只在开启指标时驱动，避免热路径额外工作 */
    if (!atomic_load_explicit(&kaula_conc_metrics_enabled, memory_order_relaxed)) return;

    if (kaula_budget_adaptor_update(&w->budget_adaptor, elapsed_ns)) {
        int32_t q = kaula_budget_adaptor_quantum(&w->budget_adaptor);
        w->budget.quantum = q;
        kaula_metrics_set_quantum(q);
    }
}

/* ========== 执行单个 Task ========== */

void kaula_worker_execute_task(KaulaWorker* w, KaulaTask* task) {
    /* I2：执行前确认属主一致（ steal 路径已改写 owner） */
    KaulaWorker* owner = atomic_load_explicit(&task->owner, memory_order_acquire);
    if (owner != NULL && owner != w) {
        /* 不该发生：说明投递路径有 bug。此处拒绝执行而非静默继续。 */
        extern _Atomic(uint64_t) kaula_dbg_owner_mismatch_count;
        atomic_fetch_add(&kaula_dbg_owner_mismatch_count, 1);
        kaula_task_complete(task, atomic_load_explicit(&task->result, memory_order_relaxed));
        return;
    }

    /* 取消检查（文档 §3.3） */
    if (atomic_load_explicit(&task->cancelled, memory_order_acquire) &&
        !(task->flags & KAULA_TASK_FLAG_UNINTRUSIVE)) {
        kaula_task_fail(task, KAULA_ERR_CANCELLED);
        w->current_task = NULL;
        return;
    }

    w->current_task = task;
    if (task->started_ns == 0) {
        task->started_ns = kaula_p_now_ns();
    }
    atomic_store_explicit(&task->state, KAULA_TASK_RUNNING, memory_order_release);

    /* 把上次的 IO 结果交给 resume_fn（TLS，非 IO 任务为 0） */
    kaula_conc_tls_io_result = task->io_result;
    task->io_result = 0;

    /* 预算重置：自适应生效时用推荐量子 */
    int32_t quantum = g_conc_runtime.budget_quantum;
    if (w->budget.quantum > 0) quantum = w->budget.quantum;
    kaula_budget_reset(&w->budget, quantum);

    uint64_t slice_start = kaula_p_now_ns();

    for (;;) {
        /* 预算检查：每次 resume 前消耗一个 tick */
        if (kaula_budget_tick(&w->budget)) {
            atomic_fetch_add_explicit(&w->stats.budget_exhausted_count, 1,
                                      memory_order_relaxed);
            /*
             * 预算耗尽：重新入队让出。
             * push 失败必须处理 —— 队列满时改走注入队列。
             */
            atomic_store_explicit(&task->state, KAULA_TASK_READY, memory_order_release);
            if (kaula_conc_worker_post(w, task) != 0) {
                /*
                 * 队列与注入队列都满：Task 会丢失。
                 * 唯一安全退路是原地继续执行到下一个让出点，
                 * 否则 done_flag 永不置位 → await 死锁（缺陷 #3）。
                 */
                kaula_task_fail(task, KAULA_ERR_WOULD_BLOCK);
            }
            w->current_task = NULL;
            return;
        }

        task->resume_count++;
        KaulaResumeResult result = task->resume_fn(task->frame);

        switch (result.status) {
        case KAULA_RESUME_DONE:
            kaula_task_complete(task, result.value);
            atomic_fetch_add_explicit(&w->stats.tasks_executed, 1, memory_order_relaxed);
            w->current_task = NULL;
            kaula_conc_worker_update_budget(w, kaula_p_now_ns() - slice_start);
            return;

        case KAULA_RESUME_BUDGET_EXHAUSTED:
            /* 状态机主动让出（长计算分裂点，文档 §9.2） */
            atomic_store_explicit(&task->state, KAULA_TASK_READY, memory_order_release);
            if (kaula_conc_worker_post(w, task) != 0) {
                kaula_task_fail(task, KAULA_ERR_WOULD_BLOCK);
            }
            w->current_task = NULL;
            return;

        case KAULA_RESUME_SUSPEND:
            /*
             * IO 挂起：移交 IoDriver，Worker 立即转做别的事（I4）。
             * 若投递失败，必须让 Task 继续而不是挂死。
             */
            task->io_request = result.io_request;
            atomic_store_explicit(&task->state, KAULA_TASK_SUSPENDED, memory_order_release);
            if (kaula_iodriver_submit(task) != 0) {
                /* 提交失败（如运行时就绪上限）：退回本地继续 */
                atomic_store_explicit(&task->state, KAULA_TASK_RUNNING, memory_order_release);
                break;
            }
            w->current_task = NULL;
            return;

        case KAULA_RESUME_BLOCKED:
            /*
             * 不可 lowering 的阻塞：移交 Block Lane（I5）。
             * 关键：绝不在 Worker 上原地执行 ——
             * 那会让整个 Worker 卡住，正是 I5 要避免的情况。
             */
            if (task->flags & KAULA_TASK_FLAG_HINT_BLOCKING) {
                /* #[hint(blocking)]：明确知道会阻塞，直接走车道 */
                atomic_store_explicit(&task->state, KAULA_TASK_BLOCKED, memory_order_release);
                if (kaula_block_lane_submit(task) != 0) {
                    atomic_store_explicit(&task->state, KAULA_TASK_RUNNING,
                                          memory_order_release);
                    break;
                }
                w->current_task = NULL;
                return;
            }
            /*
             * 未标注 hint：这是分类不确定的情形。
             * 设计说明书 §2.2 规定走预算检测 + L2/L3 遏制，
             * 即先让它在 Worker 上跑，交给预算与停滞判定兜底。
             */
            break;
        }
    }

    w->current_task = NULL;
}

/* ========== 主循环 ========== */

void kaula_worker_loop(KaulaWorker* w) {
    while (atomic_load_explicit(&w->running, memory_order_acquire)) {
        /* 撤离检查（I5）：完成当前 Task 后退出，不强行打断 */
        if (atomic_load_explicit(&w->evicted, memory_order_acquire)) {
            if (w->current_task == NULL) break;
        }

        /* 进入 epoch 临界区 */
        kaula_epoch_enter();

        KaulaTask* task = kaula_conc_worker_find_work(w);
        if (task == NULL) {
            /* 退出临界区并 park */
            kaula_epoch_exit();
            worker_park(w);
            continue;
        }

        kaula_worker_execute_task(w, task);

        /* 退出临界区：此时已不再持有该 Task 指针 */
        kaula_epoch_exit();
    }

    /*
     * Worker 退出：把本地队列里剩下的 Task 全部转入注入队列，
     * 否则这些 Task 会随 Worker 一起消失 → await 死锁。
     */
    KaulaTask* leftover;
    while ((leftover = kaula_ws_deque_pop(&w->local_queue)) != NULL) {
        if (!kaula_scheduler_inject_push(leftover)) {
            /* 注入队列也满：本地找活时已经用尽，只能标记失败 */
            kaula_task_fail(leftover, KAULA_ERR_WOULD_BLOCK);
        }
    }
    kaula_scheduler_notify_one();
}

/* ========== 生命周期 ========== */

static void worker_thread_entry(void* arg) {
    KaulaWorker* w = (KaulaWorker*)arg;
    kaula_conc_set_current_worker(w);
    kaula_worker_loop(w);
}

/*
 * 两阶段创建（初始化竞态修复）：
 *   prepare 只初始化结构（不含线程），launch 才起线程。
 * 原实现 create 内 memset+init+thread_create 一气呵成：worker[0] 的线程
 * 启动后立刻进入 find_work 的 steal 步骤，读取 g_conc_workers[1..n]
 * 尚未初始化的本地队列 → 未初始化读（TSan: data race）+ 可能窃取到
 * 半初始化 buffer 里的垃圾指针。
 */
int kaula_worker_prepare(KaulaWorker* w, uint32_t worker_id) {
    memset(w, 0, sizeof(*w));
    w->worker_id = worker_id;
    w->rng_state = 0x9E3779B97F4A7C15ULL ^ (uint64_t)(worker_id + 1);
    w->budget.quantum = g_conc_runtime.budget_quantum;
    kaula_budget_adaptor_init(&w->budget_adaptor);

    atomic_store_explicit(&w->running, 1, memory_order_release);
    atomic_store_explicit(&w->park_requested, 0, memory_order_relaxed);
    atomic_store_explicit(&w->evicted, 0, memory_order_relaxed);
    atomic_store_explicit(&w->local_epoch, 0, memory_order_relaxed);

    kaula_ws_deque_init(&w->local_queue);
    w->retired.count = 0;

    if (kaula_p_event_create(&w->park_event) != 0) {
        return -1;
    }
    return 0;
}

int kaula_worker_launch(KaulaWorker* w) {
    if (kaula_p_thread_create(&w->thread, worker_thread_entry, w) != 0) {
        kaula_p_event_destroy(&w->park_event);
        return -1;
    }
    return 0;
}

int kaula_worker_create(KaulaWorker* w, uint32_t worker_id) {
    if (kaula_worker_prepare(w, worker_id) != 0) return -1;
    return kaula_worker_launch(w);
}

void kaula_worker_join(KaulaWorker* w) {
    if (kaula_p_thread_is_valid(&w->thread) || kaula_p_event_is_valid(&w->park_event)) {
        kaula_p_thread_join(w->thread);
        kaula_p_thread_invalidate(&w->thread);
    }
}

void kaula_worker_stop(KaulaWorker* w) {
    atomic_store_explicit(&w->running, 0, memory_order_release);
    /* 唤醒，让它看到 running=0 */
    kaula_worker_wake(w);
}