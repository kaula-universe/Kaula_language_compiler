/*
 * Kaula 并发内核（CONC）· Worker 实现
 */

#include "kaula_conc_worker.h"
#include "kaula_conc_scheduler.h"
#include "kaula_conc_iodriver.h"
/*
 * io_backend.h：给 Worker 自做的 do_io 拿 kaula_iodriver_do_io 声明。
 *
 * v1.2.1 把 do_io 下沉到 Worker（消除 iodriver 单线程串行税），
 * 但声明留在 io_backend.h 里，而这条 include 链没接上 ——
 * clang 报 implicit function declaration，整个运行时编不出来。
 * （隐式声明在 C99 起已被移除；就算编过，int 返回值也恰好对，
 *   属"能跑但不对"的沉默错误，必须显式包含而不是放宽告警。）
 */
#include "kaula_conc_io_backend.h"
#include "kaula_conc_block_lane.h"
#include "kaula_conc_metrics.h"
/*
 * KMM 作用域栈兜底：需要 kmm_v4_scope_depth_get/restore（见下方 guard）。
 * worker 原本不直接依赖分配器头，为单点兜底显式引入。
 */
#include "../kmm_scoped_allocator_v4.h"

#include <stdlib.h>
#include <string.h>
#include <stdio.h>

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
     *
     * P-opt：计数放 Worker 本地（独占缓存行、无原子、无跨线程争用）。
     * 旧实现用全局 g_epoch_exit_counter 每任务一次 relaxed RMW ——
     * 多 Worker 在同一个 cacheline 上乒乓，spawn/await 热路径
     * 每任务赔一次原子写。本地化后该原子写完全消失；
     * 代价是每个 Worker 各自每 256 次退出推进一次全局 epoch
     * （推进频率只升不降，回收只会更积极，属于安全方向）。
     */
    KaulaWorker* w = kaula_conc_current_worker();
    if (w != NULL) {
        if ((++w->epoch_exit_count & 0xFF) != 0xFF) return;
        atomic_fetch_add_explicit(&g_conc_epoch.global_epoch, 1, memory_order_acq_rel);
        return;
    }
    /* 非 Worker 线程（main 的 await 同步路径）：退化用全局计数 */
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
            /*
             * 无主投递（main 线程 / 外部生产者）：按 task_id 定片，
             * 按片 RR 唤醒该片的 owner —— 唤醒面铺开到整片，而不是
             * 恒定落在 worker[0]。这是 C 边 degrade（conc=100，投递密集、
             * Worker 基本都在忙）吞吐翻倍的关键：notify_one 只叫 w[0]，
             * 单核扫片封顶 ~250K；按片 RR 让同片 4 个 owner 轮流接活，
             * 4 核并行消费，实测 ~460K。
             */
            kaula_scheduler_notify_shard(kaula_scheduler_shard_of_task(task));
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
            /*
             * 有明确目标 w：直接唤醒 w，而不是按片 RR 挑一个「偏好该片」
             * 的兄弟 Worker。
             *
             * 任务已经进了注入队列（Chase-Lev 不允许外部线程写 bottom，
             * 见上方注释），但它「属于」w —— w 是投递方指定的执行者
             * （suspend 的挂起往返、驱动结果回投都带着明确 owner）。
             * 按片 RR 会把唤醒推给同片的另一个 Worker：那个 Worker 醒来
             * 先扫自己的片、再轮到别人的片，任务被它抢走执行，而真正的
             * w 还在睡 —— 一次挂起往返多出一次跨核迁移 + 一次多余的
             * 片扫描，实测 suspend/mixed 掉 15%。
             *
             * 直接 wake(w)：w 醒来扫片时优先看自己的偏好片，任务大概率
             * 与它同片（task_id 分配与 owner 相关），一次就取走。
             * 若 w 已撤离（running=0），下面的 notify_shard 兜底接手。
             */
            if (atomic_load_explicit(&w->running, memory_order_acquire)) {
                kaula_worker_wake(w);
            } else {
                kaula_scheduler_notify_shard(kaula_scheduler_shard_of_task(task));
            }
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

/* ========== 本地短定时器（P-D3：Task 内嵌） ========== */

/*
 * 登记短 sleep 到本 Worker 本地定时器。调用者必须是 owner 线程
 * （execute 的 SUSPEND 分支，此时任务正由本 Worker 执行）。
 * 任务本身已在池中（生命周期由任务池管理），登记不可能失败——
 * 直接把 deadline 写进 Task 并头插链表，省 KaulaLocalTimer 分配
 * 与一次指针跳。链接复用 task->resume_next（见 task.h 注释：
 * 定时器链与恢复链不共存，共享字段安全）。
 */
static void kaula_local_timer_add(KaulaWorker* w, KaulaTask* task,
                                  uint64_t delay_ns) {
    uint64_t deadline = kaula_p_now_ns() + delay_ns;
    task->local_deadline_ns = deadline;
    task->resume_next = w->local_timers;
    w->local_timers = task;
    if (deadline < w->local_timers_min_deadline) {
        w->local_timers_min_deadline = deadline;
    }
    /* 跨线程镜像：本 Worker 现在「为定时器而睡」 */
    atomic_store_explicit(&w->has_local_timer, 1, memory_order_release);
}

/*
 * 到期检查（owner 线程调度循环每轮调用，对标 Go checkTimers）。
 * 到期任务 READY 后入恢复优先链（P-D2 FIFO，不被新任务插队）；
 * 未到期任务留在定时器链（keep 重建，仍用 resume_next 链接）。
 * keep 链与恢复链共享 resume_next 字段，但每个任务同一时刻只属于
 * 其中一条链（到期任务先摘除再入恢复链），无交叉写。
 */
static void kaula_worker_check_local_timers(KaulaWorker* w) {
    if (w->local_timers == NULL) return;
    uint64_t now = kaula_p_now_ns();
    if (w->local_timers_min_deadline > now) return; /* 最短短路 */

    KaulaTask* keep = NULL;
    KaulaTask* keep_tail = NULL;
    uint64_t new_min = UINT64_MAX;
    KaulaTask* cur = w->local_timers;
    while (cur != NULL) {
        KaulaTask* next = cur->resume_next;
        if (cur->local_deadline_ns <= now) {
            cur->local_deadline_ns = 0;   /* 摘除：不在定时器链上 */
            cur->io_result = 0;
            atomic_store_explicit(&cur->state, KAULA_TASK_READY,
                                  memory_order_release);
            /* 恢复优先（P-D2）：入 owner 专用 FIFO 链，不被新任务插队 */
            cur->resume_next = NULL;
            if (w->resume_tail != NULL) {
                w->resume_tail->resume_next = cur;
            } else {
                w->resume_head = cur;
            }
            w->resume_tail = cur;
        } else {
            cur->resume_next = NULL;
            if (keep == NULL) {
                keep = cur;
                keep_tail = cur;
            } else {
                keep_tail->resume_next = cur;
                keep_tail = cur;
            }
            if (cur->local_deadline_ns < new_min) new_min = cur->local_deadline_ns;
        }
        cur = next;
    }
    w->local_timers = keep;
    w->local_timers_min_deadline = new_min; /* keep 空时为 UINT64_MAX */
    /* keep 空 → 不再为定时器而睡，清镜像（notify_shard 可重新唤醒本 Worker） */
    atomic_store_explicit(&w->has_local_timer, keep != NULL, memory_order_release);
}

/* ========== KMM 作用域栈兜底 ========== */

/*
 * 一个 task 的两次 resume 之间，TLS 作用域栈深度必须回到任务进入时的值：
 *
 *   - 正常路径：resume 内 push/pop 配对，返回时深度 == 进入深度，本函数
 *     是零开销空操作（restore 只在更深时裁剪，相等直接返回 0）。
 *   - 缺陷路径：`conc fn` 里从块级 KMM 区间（KMM_V4_SCOPE_START）内部
 *     提前 return，return 绕过了 KMM_V4_SCOPE_END 的弹栈。每执行一次就在
 *     本线程 TLS 定长栈上永久泄漏一帧，累计到 KMM_V4_MAX_SCOPE_DEPTH
 *     （默认 1024）后 abort —— 连接关闭潮汐下必现。
 *
 * 真修复在 codegen 侧（generateConcReturn 已补齐 DONE 出口的弹栈）。
 * 这里做单点兜底：把深度拉回本任务进入时的值，任何非配平的出口都被
 * runtime 收口，正确性不再依赖「生成器处处小心」。
 *
 * 记录策略：基线 `task->kmm_entry_depth` 在**首次进入任务体之前**采集
 * （见 execute_task 调用点），之后跨挂起/重入沿用。这样即使某次 resume
 * 泄漏了帧，也在该次出口被拉回基线，不会跨任务扩散。
 *
 * 为什么在 SUSPEND 出口也要恢复：挂起语义要求「挂起后如同未执行」，
 * 定长栈若在挂起期间残留深层帧，会挤占其它任务的深度额度（同一 Worker
 * 上多个任务共享 TLS 栈），同样可能撑爆。
 *
 * 调用点：owner 线程（执行 resume_fn 的同一个 Worker）——与 KMM 的
 * TLS 亲和性一致，无需额外同步。
 */
static void kaula_kmm_scope_guard_task(KaulaWorker* w, KaulaTask* task,
                                       KaulaResumeStatus status) {
    (void)w;
    switch (status) {
    case KAULA_RESUME_DONE:
    case KAULA_RESUME_SUSPEND:
    case KAULA_RESUME_BLOCKED:
        break;
    default:
        /* BUDGET_EXHAUSTED 等重入路径不改变作用域归属（未离开任务体）。 */
        return;
    }
    /* 基线在进入任务体前由 execute_task 写入；这里只做收敛。 */
    kmm_v4_scope_depth_restore((size_t)task->kmm_entry_depth);
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
     * 置 parked=1 必须紧贴 event_wait 之前，且要保证「置位 → wait」之间
     * 落入的 wake 不会丢：event 是计数语义（signal 累积），后到的 wait
     * 会立即返回，因此即便 wake 在置位后、wait 前发生也不丢。
     * 醒来后（含 wait 超时）立刻清零，保证 parked=1 只在真正阻塞时为真。
     */
    atomic_store_explicit(&w->parked, 1, memory_order_release);

    /*
     * busy-wait 低延迟阶段：park 前先轮询 N 次。
     * 有活立即返回（省去 event_wait 的唤醒延迟，~1-10us）。
     * 代价：空转时 CPU 100%。适合低延迟服务（对标 Redis/DPDK）。
     */
    if (g_conc_runtime.busy_wait_spins > 0) {
        for (uint32_t i = 0; i < g_conc_runtime.busy_wait_spins; i++) {
            if (!kaula_ws_deque_empty(&w->local_queue)) return;
            if (w->resume_head != NULL) return;
            if (!kaula_scheduler_inject_empty()) return;
            kaula_p_yield();
        }
    }

    /*
     * 有限超时：即便漏了一次唤醒，最多 1ms 后自愈。
     * 这是"不空转"与"不丢唤醒"之间的折中 —— 漏唤醒最坏多等 1ms，
     * 而持续空转会烧掉一个核（P2 明确要求）。
     *
     * P-D：本地短定时器未到期时，park 不能超过最近到期时刻，
     * 否则短 sleep 任务会因 Worker 长睡而错过 deadline。
     * 超时取 min(1ms, 最近到期剩余)；已到期（正常不会到这）立即醒。
     */
    uint64_t park_ns = 1000000ULL;
    if (w->local_timers_min_deadline != UINT64_MAX) {
        uint64_t now = kaula_p_now_ns();
        if (w->local_timers_min_deadline <= now) {
            park_ns = 0;
        } else {
            uint64_t rem = w->local_timers_min_deadline - now;
            if (rem < park_ns) park_ns = rem;
        }
    }
    kaula_p_event_wait(&w->park_event, park_ns);
    atomic_store_explicit(&w->parked, 0, memory_order_release);

    uint64_t dt = kaula_p_now_ns() - t0;
    atomic_fetch_add_explicit(&w->stats.park_total_ns, dt, memory_order_relaxed);
}

/* ========== 取活 ========== */

KaulaTask* kaula_conc_worker_find_work(KaulaWorker* w) {
    /* 0. 恢复优先（P-D2）：到期任务先于一切新任务（对标 Go runnext） */
    KaulaTask* t = w->resume_head;
    if (t != NULL) {
        w->resume_head = t->resume_next;
        if (w->resume_head == NULL) w->resume_tail = NULL;
        t->resume_next = NULL;
        return t;
    }

    /* 1. 本地队列（LIFO，缓存友好） */
    t = kaula_ws_deque_pop(&w->local_queue);
    if (t != NULL) return t;

    /* 2. 全局注入队列（分片，批量优先本片）
     *
     * 外部线程（main）投递一律走注入队列（见 kaula_conc_worker_post 的
     * F1 说明），注入队列因此是主要任务来源。注入队列已按 task_id 分成
     * KAULA_INJECT_SHARDS 片（见 kaula_conc_task.h 常量说明）。
     *
     * 先扫「自己那一片」（prefer = worker_id）：投递方按 task_id 轮转选片，
     * 每个 Worker 大概率在自己的片上拿到活，锁与 head 竞争都只落在本片，
     * 全局竞争被拆成 1/N。本片空了再轮转扫其余片，保证不让任务滞留。
     *
     * 每片批量取（一次 CAS 换一批）：out[0] 立即执行，out[1..m) 回填
     * 本地队列，后续取活走第 1 步的无 CAS LIFO 快路径，把 head CAS
     * 竞争再摊薄一个数量级。
     *
     * 关键：回填本地队列的每个 Task 都必须改写 owner = w。
     * owner 是「SUSPEND 后 resume 该投给谁」的依据（kaula_scheduler_resume_task
     * 读它决定 post 到哪个 Worker）。批量取出的 Task 仍带旧 owner（甚至
     * NULL），不改写就会出现：IoDriver 唤醒时投给了别人的队列，而持有者
     * 本地队列里那份仍在等 —— 表现为 sleep 任务 resume 一次后永不完成。
     * steal 路径同样改写 owner，这里是同一约束。
     */
    {
        KaulaTask* drained[32];
        uint32_t want = (uint32_t)(w->worker_id) & KAULA_INJECT_SHARD_MASK;

        int got = kaula_scheduler_inject_pop_batch_shard(want, drained, 32);
        if (got == 0) {
            /* 本片空：轮转扫其余片 */
            for (uint32_t k = 1; k < KAULA_INJECT_SHARDS && got == 0; k++) {
                uint32_t si = (want + k) & KAULA_INJECT_SHARD_MASK;
                got = kaula_scheduler_inject_pop_batch_shard(si, drained, 32);
            }
        }

        if (got > 0) {
            /* 全部改写 owner = w（见上方「关键」注释） */
            for (int j = 0; j < got; j++) {
                atomic_store_explicit(&drained[j]->owner, w, memory_order_release);
            }
            for (int j = 1; j < got; j++) {
                /* 本地队列刚被 pop 空、容量 4096、批量 ≤32，正常不会满；
                 * 万一满则退回注入队列，绝不丢任务。 */
                if (!kaula_ws_deque_push_owner(&w->local_queue, drained[j])) {
                    if (!kaula_scheduler_inject_push(drained[j])) {
                        kaula_scheduler_notify_one();
                    }
                }
            }
            return drained[0];
        }

        /*
         * 没取到 —— 但**队列可能非空**。
         *
         * inject_shard_pop_batch 的 CAS 在竞争窗口里会失败；旧实现在
         * 重试耗尽后返回 0（=「空」），Worker 据此走向 park，而任务其实
         * 还在队列里。这是「永久漏唤醒」的确切来源：连接任务的第一条
         * 请求没人处理，撞满读超时（实测 4096 连接 / 4 Worker 下均匀
         * 分布 1/4 连接中招）。
         *
         * inject_shard_pop_batch 已修（重试耗尽退化为单条 CAS，不谎报
         * 空）；这里再加一道复核：若整个注入队列确实非空，说明是竞争
         * 抢空而非真空 —— 自旋收紧一次、重来一轮，绝不 park。
         *
         * 加一次上限（4 次）防止病态情况占死 Worker：真到了这一步，
         * 让出 Worker 由 park 的 1ms 自愈兜底，仍不会永久卡死。
         */
        for (int spin = 0; spin < 4; spin++) {
            if (kaula_scheduler_inject_empty()) {
                break; /* 真空：安心往下走（窃取 / park） */
            }
            got = kaula_scheduler_inject_pop_batch_shard(want, drained, 32);
            if (got == 0) {
                for (uint32_t k = 1; k < KAULA_INJECT_SHARDS && got == 0; k++) {
                    uint32_t si = (want + k) & KAULA_INJECT_SHARD_MASK;
                    got = kaula_scheduler_inject_pop_batch_shard(si, drained, 32);
                }
            }
            if (got > 0) {
                for (int j = 0; j < got; j++) {
                    atomic_store_explicit(&drained[j]->owner, w, memory_order_release);
                }
                for (int j = 1; j < got; j++) {
                    if (!kaula_ws_deque_push_owner(&w->local_queue, drained[j])) {
                        if (!kaula_scheduler_inject_push(drained[j])) {
                            kaula_scheduler_notify_one();
                        }
                    }
                }
                return drained[0];
            }
            kaula_p_yield();
        }
    }

    /* 3. 窃取他人（随机起点，避免所有 Worker 撞同一个受害者） */
    uint32_t n = g_conc_runtime.worker_count;
    if (n <= 1) return NULL;

    atomic_fetch_add_explicit(&w->stats.steal_attempts, 1, memory_order_relaxed);

    /* 批量窃取批量大小（init 时由 KAULA_STEAL_BATCH 读入缓存）。
     * 栈数组上限 16：实测 k=1/4/8，超出则截断，避免栈溢出。 */
    int batch_max = (int)g_conc_runtime.steal_batch_max;
    if (batch_max > 16) batch_max = 16;

    uint32_t start = (uint32_t)(worker_rand(w) % n);
    for (uint32_t i = 0; i < n; i++) {
        uint32_t idx = (start + i) % n;
        if (idx == w->worker_id) continue;

        KaulaWSDeque* victim_q = &g_conc_workers[idx].local_queue;

        /* 先尝试批量偷：一次 CAS 拿 n 个最老任务。 */
        if (batch_max > 0) {
            KaulaTask* grabbed[16];
            int got = kaula_ws_deque_steal_batch(victim_q, grabbed, batch_max);
            if (got > 0) {
                /* 全部 n 个改写 owner=w；out[0] 立即返回，out[1..n) 回填本地队列 */
                for (int j = 0; j < got; j++) {
                    atomic_store_explicit(&grabbed[j]->owner, w, memory_order_release);
                    grabbed[j]->steal_count++;
                }
                atomic_fetch_add_explicit(&w->stats.steal_count, got,
                                          memory_order_relaxed);
                for (int j = 1; j < got; j++) {
                    /* 本地队列刚被 pop 空、容量 4096、n≤8，正常不会满；
                     * 万一满则退回全局注入队列，绝不丢任务。 */
                    if (!kaula_ws_deque_push_owner(&w->local_queue, grabbed[j])) {
                        if (!kaula_scheduler_inject_push(grabbed[j])) {
                            /* 注入也满：兜底唤醒，由 worker 排空后自举 */
                            kaula_scheduler_notify_one();
                        }
                    }
                }
                return grabbed[0];
            }
            /* got==0：空/不足两项/CAS 竞争失败，退回单任务 steal 仲裁末元素 */
        }

        KaulaTask* victim = kaula_ws_deque_steal(victim_q);
        if (victim != NULL) {
            /*
             * I2：改写 owner。
             * 任务状态在帧里，迁移只是改一个指针，不拷贝任何执行状态，
             * 因此不存在"搬栈导致悬垂指针"的问题。
             */
            victim->steal_count++;
            atomic_store_explicit(&victim->owner, w, memory_order_release);
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

    /*
     * do_io 下沉（v1.2.1）：就绪式后端事件就绪后 iodriver 不代做 IO，
     * 由 Worker 在恢复任务前自做非阻塞 read/write——消除 iodriver 单
     * 线程 do_io 串行税，4 Worker 并行执行 syscall。
     * 必须在 TLS 复制（下方 404）之前执行，否则 kl 恢复点读到旧 TLS=0
     * 误判 EOF 关闭连接。EAGAIN（假就绪）经 submit 重挂（其 READ fast
     * path 会先再读一次，仍 EAGAIN 才真正挂回 epoll）。
     */
    if (task->io_do_in_worker) {
        task->io_do_in_worker = 0;
        int r = kaula_iodriver_do_io(&task->io_request);
        if (r < 0 && (errno == EAGAIN || errno == EWOULDBLOCK)) {
            if (kaula_iodriver_submit(task) != 0) {
                kaula_task_fail(task, KAULA_ERR_WOULD_BLOCK);
            }
            w->current_task = NULL;
            return;
        }
        task->io_result = r;
    }

    /* 把上次的 IO 结果交给 resume_fn（TLS，非 IO 任务为 0） */
    kaula_conc_tls_io_result = task->io_result;
    task->io_result = 0;

    /* 预算重置：自适应生效时用推荐量子 */
    int32_t quantum = g_conc_runtime.budget_quantum;
    if (w->budget.quantum > 0) quantum = w->budget.quantum;
    kaula_budget_reset(&w->budget, quantum);

    /*
     * P-D3：slice 计时只在实际需要时取 —— metrics 关闭（基准/生产默认）
     * 时 update_budget 直接返回，clock_gettime 每任务两次纯属浪费
     * （实测 ~60ns/任务，10k 并发下可占 8%）。开启时才计时。
     */
    int metrics_on = atomic_load_explicit(&kaula_conc_metrics_enabled,
                                          memory_order_relaxed);
    uint64_t slice_start = metrics_on ? kaula_p_now_ns() : 0;

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

        /*
         * 首跑直通（B）：resume_count==0 且提供 enter_fn 时走 enter ——
         * 无 switch(pc) 无守卫判断，无条件执行入口段到第一个挂起点。
         * 挂起/让出后 resume_count>0，一律走 resume_fn 恢复。
         */
        KaulaResumeFn fn = task->resume_fn;
        if (task->enter_fn != NULL && task->resume_count == 0) {
            fn = task->enter_fn;
        }
        task->resume_count++;

        /*
         * KMM 作用域栈兜底 · 记录基线（必须在 fn 之前）。
         *
         * 基线是「本任务未占用任何作用域」时的栈深度（即调用者上下文
         * 的深度，通常为 0）。必须在进入任务体之前采，否则任务体若
         * 泄漏帧，采到的就是「已污染」的假基线，兜底失效。
         * 只在首次执行记录：SUSPEND 后重入时深度已被上一次兜底拉回
         * 基线，重新采会采到同一个值，没有意义且多一次 TLS 读。
         */
        if (task->kmm_entry_depth == (uint32_t)-1) {
            task->kmm_entry_depth = (uint32_t)kmm_v4_scope_depth_get();
        }

        KaulaResumeResult result = fn(task->frame);

        /*
         * KMM 作用域栈兜底 · 出口收敛（防 codegen 疏漏）。
         *
         * 一个 task 的 resume 可能从块级 KMM 区间（KMM_V4_SCOPE_START）
         * 内部提前 return，而 return 绕过 KMM_V4_SCOPE_END 的弹栈 ——
         * 每执行一次就在本线程 TLS 定长栈上永久泄漏一帧，累计
         * KMM_V4_MAX_SCOPE_DEPTH（默认 1024）后 abort。
         *
         * 真修复在 codegen 侧（stmtgen 的 generateConcReturn 已补齐弹栈）。
         * 这里做单点兜底：任何离开任务体的出口都把深度拉回基线，
         * 正确性不再依赖「生成器处处小心」。对配平正确的路径是零开销
         * 空操作（kmm_v4_scope_depth_restore 仅在更深时裁剪）。
         */
        kaula_kmm_scope_guard_task(w, task, result.status);

        switch (result.status) {
        case KAULA_RESUME_DONE:
            kaula_task_complete(task, result.value);
            atomic_fetch_add_explicit(&w->stats.tasks_executed, 1, memory_order_relaxed);
            w->current_task = NULL;
            if (metrics_on) {
                kaula_conc_worker_update_budget(w, kaula_p_now_ns() - slice_start);
            }
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
             * IO 挂起：短 sleep 走本 Worker 本地定时器（P-D，不跨线程）；
             * 其余移交 IoDriver，Worker 立即转做别的事（I4）。
             * 若投递失败，必须让 Task 继续而不是挂死。
             */
            task->io_request = result.io_request;
            if (task->io_request.type == KAULA_IO_SLEEP &&
                task->io_request.timeout_ns <= KAULA_LOCAL_TIMER_MAX_NS) {
                atomic_store_explicit(&task->state, KAULA_TASK_SUSPENDED,
                                      memory_order_release);
                kaula_local_timer_add(w, task, task->io_request.timeout_ns);
                w->current_task = NULL;
                return;
            }
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

        /* P-D：本地短定时器到期检查（对标 Go checkTimers） */
        kaula_worker_check_local_timers(w);

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

    /* P-D2：恢复优先链剩余任务转注入队列（否则随 Worker 消失 → await 死锁） */
    KaulaTask* rt = w->resume_head;
    w->resume_head = NULL;
    w->resume_tail = NULL;
    while (rt != NULL) {
        KaulaTask* nx = rt->resume_next;
        rt->resume_next = NULL;
        if (!kaula_scheduler_inject_push(rt)) {
            kaula_task_fail(rt, KAULA_ERR_WOULD_BLOCK);
        }
        rt = nx;
    }

    /*
     * P-D3：本地短定时器未到期任务转移给 IoDriver（保持剩余 deadline，
     * sleep 语义不变），否则随 Worker 消失 → await 死锁。
     * 节点内嵌在 Task 里，只需摘链并清 deadline 即可。
     */
    KaulaTask* lt = w->local_timers;
    w->local_timers = NULL;
    w->local_timers_min_deadline = UINT64_MAX;
    uint64_t now_ns = kaula_p_now_ns();
    while (lt != NULL) {
        KaulaTask* nx = lt->resume_next;
        uint64_t rem = lt->local_deadline_ns > now_ns
                     ? lt->local_deadline_ns - now_ns : 0;
        lt->local_deadline_ns = 0;
        lt->resume_next = NULL;
        lt->io_request.type = KAULA_IO_SLEEP;
        lt->io_request.timeout_ns = rem;
        if (kaula_iodriver_submit(lt) != 0) {
            kaula_task_fail(lt, KAULA_ERR_WOULD_BLOCK);
        }
        lt = nx;
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
    atomic_store_explicit(&w->parked, 0, memory_order_relaxed);
    atomic_store_explicit(&w->has_local_timer, 0, memory_order_relaxed);
    atomic_store_explicit(&w->evicted, 0, memory_order_relaxed);
    atomic_store_explicit(&w->local_epoch, 0, memory_order_relaxed);

    kaula_ws_deque_init(&w->local_queue);
    /* P-D：空定时器状态为 UINT64_MAX（memset 0 会让 park 误判为已到期） */
    w->local_timers_min_deadline = UINT64_MAX;
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