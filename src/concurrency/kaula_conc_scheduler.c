/*
 * Kaula 并发内核 · 调度器实现
 */

#include "kaula_conc_scheduler.h"
#include "kaula_conc_worker.h"
#include "kaula_conc_iodriver.h"
#include "kaula_conc_block_lane.h"
#include "kaula_conc_epoch.h"
#include "kaula_conc_metrics.h"
#include "../kmm_scoped_allocator_v4.h"

#include <stdlib.h>
#include <string.h>

/* ========== 全局运行时状态 ========== */

KaulaRuntimeState g_conc_runtime;

/* 全局注入队列 */
static KaulaInjectQueue g_inject_queue;

/*
 * push 侧串行化锁。
 *
 * push 的正确性依赖「先写 payload、再推进 tail」这个顺序：pop 要等
 * tail > head 才去读 buffer，靠的是 tail 上 release 建立的先写后读。
 * 但原实现把「读 tail → 写 buffer → 写 tail」这段裸露给了并发，两个
 * pusher 会读到同一个 tail：
 *
 *     A 读 tail=T → 写 buffer[T] = taskA
 *     B 读 tail=T → 写 buffer[T] = taskB    ← 覆盖掉 taskA
 *     A 写 tail=T+1
 *     B 写 tail=T+1                          ← 只前进 1
 *
 * taskA 永久丢失，而它的 await 再也等不到 —— 一次竞态丢一个任务，
 * 且完全无法自愈（worker 的 1ms park 超时只捞得到「已经在队列里」的任务）。
 * 实测：并发窗口下 suspend/mixed 曲线 80%+ 概率死锁，详见 FINDINGS.md F1。
 *
 * 为什么是 atomic_flag 而不是 kaula_p_mutex：
 *   - 静态初始化即可，不依赖 kaula_inject_queue_init 的调用时序；
 *   - 临界区只有「查满 + 写一格 + 写 tail」三条指令，自旋成本可忽略；
 *   - pusher 最多三路（main 的 spawn、IoDriver 的 resume、Worker 本地
 *     队列满时的兜底），临界区内不发生任何阻塞，持锁者绝不会在锁内
 *     等待别人，因此自旋不可能活锁。
 *
 * pop 侧保持原样不动：互斥只加在生产者一侧，读侧的 release/acquire
 * 配对关系没有变化，改动面越小越好。
 */
static atomic_flag g_inject_push_lock = ATOMIC_FLAG_INIT;

/*
 * 实现：多生产者 / 多消费者有界队列。
 * push 从 tail 进（整段串行化），pop 从 head 出（CAS 竞争，多线程可并行）。
 * 容量固定，因此满时返回 0 —— 绝不覆盖未消费的槽位。
 *
 * 注意 pop 的 CAS 只保护 head 的推进，**不能替代 push 侧的串行化**：
 * 两个 pusher 即使各自都「对」，写的也是同一个槽位。
 */

/* ========== 全局注入队列实现 ========== */

void kaula_inject_queue_init(void) {
    atomic_store_explicit(&g_inject_queue.head, 0, memory_order_relaxed);
    atomic_store_explicit(&g_inject_queue.tail, 0, memory_order_relaxed);
    memset((void*)g_inject_queue.buffer, 0, sizeof(g_inject_queue.buffer));
}

int kaula_inject_queue_push(KaulaTask* task) {
    /* 抢锁：acquire，与下面的 release 解锁配对 */
    while (atomic_flag_test_and_set_explicit(&g_inject_push_lock,
                                             memory_order_acquire)) {
        /* 临界区三条指令且不阻塞，持锁者必然在推进，自旋即可 */
    }

    uint64_t tail = atomic_load_explicit(&g_inject_queue.tail, memory_order_relaxed);
    uint64_t head = atomic_load_explicit(&g_inject_queue.head, memory_order_acquire);

    int ok = 1;
    if (tail - head >= KAULA_INJECT_QUEUE_CAPACITY) {
        ok = 0; /* 满：背压 */
    } else {
        /* 顺序关键：先写 payload，再以 release 推进 tail。
         * pop 读到 tail > head 之后才读 buffer，正是靠这次 release
         * 看见 payload。 */
        atomic_store_explicit(
            &g_inject_queue.buffer[tail & KAULA_INJECT_QUEUE_MASK],
            task, memory_order_relaxed);
        atomic_store_explicit(&g_inject_queue.tail, tail + 1, memory_order_release);
    }

    atomic_flag_clear_explicit(&g_inject_push_lock, memory_order_release);
    return ok;
}

/*
 * P2.1：批量注入。
 *
 * 一次拿锁 → 连续写 n 个 payload → 一次 release 推 tail。
 * 比逐个 push 少 n-1 次锁争抢 + n-1 次 tail store。
 *
 * 空间不足时只接纳 m ≤ n 个，返回 m；调用方用剩余 n-m 个重试。
 * Worker 持续排空队列，重试不会死循环。
 */
int kaula_inject_queue_push_batch(KaulaTask** tasks, int n) {
    if (n <= 0) return 0;

    while (atomic_flag_test_and_set_explicit(&g_inject_push_lock,
                                             memory_order_acquire)) {
    }

    uint64_t tail = atomic_load_explicit(&g_inject_queue.tail, memory_order_relaxed);
    uint64_t head = atomic_load_explicit(&g_inject_queue.head, memory_order_acquire);

    uint64_t avail = KAULA_INJECT_QUEUE_CAPACITY - (tail - head);
    int m = (int)avail;
    if (m > n) m = n;
    if (m < 0) m = 0;

    for (int i = 0; i < m; i++) {
        atomic_store_explicit(
            &g_inject_queue.buffer[(tail + i) & KAULA_INJECT_QUEUE_MASK],
            tasks[i], memory_order_relaxed);
    }
    if (m > 0) {
        atomic_store_explicit(&g_inject_queue.tail, tail + m, memory_order_release);
    }

    atomic_flag_clear_explicit(&g_inject_push_lock, memory_order_release);
    return m;
}

KaulaTask* kaula_inject_queue_pop(void) {
    /*
     * 多个 Worker 可能同时 pop。用 CAS 竞争 head：
     * 赢者取走该槽位的 Task，输者继续尝试下一个。
     */
    for (uint64_t attempt = 0; attempt < 8; attempt++) {
        uint64_t head = atomic_load_explicit(&g_inject_queue.head, memory_order_relaxed);
        uint64_t tail = atomic_load_explicit(&g_inject_queue.tail, memory_order_acquire);

        if (head >= tail) return NULL; /* 空 */

        KaulaTask* task = atomic_load_explicit(
            &g_inject_queue.buffer[head & KAULA_INJECT_QUEUE_MASK],
            memory_order_acquire);

        uint64_t expected = head;
        if (atomic_compare_exchange_strong_explicit(
                &g_inject_queue.head, &expected, head + 1,
                memory_order_acq_rel, memory_order_relaxed)) {
            /*
             * 修复：不再清空槽位。
             *
             * 原实现赢者 CAS 后以 relaxed store 清空槽位。该 relaxed
             * 写允许乱序/延迟执行，与 push 侧「环形 wrap 后写回同一槽位」
             * 交错时会抹掉刚 push 的新任务：
             *
             *   队列满 → push 写 buffer[head&MASK]=T2（tail 推进）
             *   pop 赢者的 NULL store 延迟执行 → T2 被抹成 NULL
             *   → 该任务从未被任何 Worker 执行（state=READY、
             *     start_ns=0、active 计数不减）→ await 永久挂起。
             *
             * 清空本身是多余的：head 单调推进，已被取走的槽位在环形
             * wrap 前必然已被新的 push 覆盖，pop 不会重读旧任务指针。
             * 去掉清空后，槽位数据残留无害（push 是锁内单写者，下次
             * 写同槽即覆盖）。
             */
            return task;
        }
        /* 输者：重试 */
    }
    return NULL;
}

int kaula_inject_queue_empty(void) {
    uint64_t h = atomic_load_explicit(&g_inject_queue.head, memory_order_relaxed);
    uint64_t t = atomic_load_explicit(&g_inject_queue.tail, memory_order_acquire);
    return t <= h;
}

uint64_t kaula_inject_queue_size(void) {
    uint64_t h = atomic_load_explicit(&g_inject_queue.head, memory_order_relaxed);
    uint64_t t = atomic_load_explicit(&g_inject_queue.tail, memory_order_acquire);
    return t > h ? t - h : 0;
}

/* 简写 */
int kaula_scheduler_inject_push(KaulaTask* task) { return kaula_inject_queue_push(task); }
KaulaTask* kaula_scheduler_inject_pop(void)      { return kaula_inject_queue_pop(); }
int kaula_scheduler_inject_empty(void)           { return kaula_inject_queue_empty(); }

/* ========== spawn ========== */

static KaulaHandle spawn_failed(KaulaError err) {
    KaulaHandle h = kaula_handle_null();
    h.task_id = (uint64_t)err;   /* 错误码放在 task_id 里，task 为 NULL */
    return h;
}

static KaulaHandle kaula_spawn_to_flags(KaulaWorker* owner, KaulaResumeFn fn,
                                        KaulaResumeFn enter_fn,
                                        void* frame, size_t frame_size, uint32_t flags);

KaulaHandle kaula_spawn_to(KaulaWorker* owner, KaulaResumeFn fn, void* frame, size_t frame_size) {
    return kaula_spawn_to_flags(owner, fn, NULL, frame, frame_size, 0);
}

KaulaHandle kaula_spawn_to_flags(KaulaWorker* owner, KaulaResumeFn fn,
                                 KaulaResumeFn enter_fn,
                                 void* frame, size_t frame_size, uint32_t flags) {
    if (atomic_load_explicit(&g_conc_runtime.shutdown, memory_order_acquire)) {
        return spawn_failed(KAULA_ERR_SHUTDOWN);
    }

    /*
     * I3：全局任务数硬上限。
     * 递归 conc fn + spawn 无界增长时，这里返回 WouldBlock 而非 OOM。
     */
    uint64_t active = atomic_load_explicit(&g_conc_runtime.active_task_count,
                                           memory_order_acquire);
    if (active >= g_conc_runtime.max_tasks) {
        return spawn_failed(KAULA_ERR_WOULD_BLOCK);
    }

    KaulaTask* task = kaula_task_alloc();
    if (task == NULL) {
        return spawn_failed(KAULA_ERR_OOM);
    }
    kaula_task_init(task, fn, enter_fn, frame, frame_size);
    /*
     * Frame 由调用方（生成代码）堆分配，随任务退休释放。
     *
     * 不置这个标志的话，帧在正常完成路径上永远不会被回收 ——
     * kaula_task_recycle 只服务"spawn 被拒"那条路。
     * 每 spawn 泄一个 Frame，泄漏量与调用次数成正比。
     */
    task->flags |= KAULA_TASK_FLAG_FRAME_ON_HEAP;
    /* 调用方 flags（如 HINT_BLOCKING）在投递前设置，无竞争 */
    task->flags |= flags;
    atomic_store_explicit(&task->owner, owner, memory_order_relaxed);

    atomic_fetch_add_explicit(&g_conc_runtime.active_task_count, 1, memory_order_relaxed);
    kaula_conc_metrics_inc_spawned();

    /*
     * 置 READY 必须在投递**之前**。
     *
     * 反过来（先投递、后置 READY）会抹掉完成：
     *   投递方   post() 把 task 放进队列 —— 此刻它已经对 Worker 可见
     *   Worker   pop → resume_fn → kaula_task_complete 写 state=DONE
     *   投递方   atomic_store(state, READY) —— 把 DONE 覆盖回 READY
     *
     * 完成被抹掉之后没有任何补救：kaula_task_wake_waiters 是在完成那一刻
     * 发的信号，那时还没有等待者（waiters==0），所以不发；等 await 注册
     * 上去时看到的是 READY，不是 DONE，于是 park 到 done_event 上再也醒不来。
     *
     * 窗口只有 post 与 store 之间的几条指令，但 Worker 热的时候
     * resume_fn 只要几十纳秒就能跑完 —— 实测 10 万次 spawn 里命中约 100 次
     * （0.1% 量级），表现为 spawn 之后的第一个 await 永久阻塞：
     * 压测随机挂死、test_task 卡在 test_await_spawned、test_phase4 随机挂。
     */
    atomic_store_explicit(&task->state, KAULA_TASK_READY, memory_order_release);

    /* 投递：内部已实现三级背压 */
    if (kaula_conc_worker_post(owner, task) != 0) {
        /*
         * 全部队列都满。
         * 文档 §4.3 策略 3：原地执行，降级为顺序调用 ——
         * 语义唯一（I1）不退化：用户看不出区别，只是失去并行性。
         *
         * 注意循环里必须处理所有 status：
         * 原实现在这里写成 if DONE break，其他状态继续无限循环 ——
         * 遇到 SUSPEND 就是无休眠死循环（缺陷 #4）。
         */
        for (;;) {
            KaulaResumeResult r = fn(frame);

            if (r.status == KAULA_RESUME_DONE) {
                kaula_task_complete(task, r.value);
                break;
            }
            if (r.status == KAULA_RESUME_SUSPEND) {
                /*
                 * 顺序路径：就地阻塞等待 IO 完成，不经过 IoDriver。
                 * 这是文档 §9.4 coercion shim 的语义。
                 */
                kaula_conc_tls_io_result = kaula_iodriver_blocking_wait(&r.io_request);
                continue;
            }
            if (r.status == KAULA_RESUME_BLOCKED) {
                /* 顺序路径：直接执行阻塞操作，不经过 Block Lane */
                kaula_conc_tls_io_result = 0;
                continue;
            }
            /* BUDGET_EXHAUSTED：顺序路径下继续原地跑完 */
        }
        return kaula_handle_from_task(task);
    }

    return kaula_handle_from_task(task);
}

/*
 * 单一形态 conc fn 的 trampoline。
 *
 * shim 是普通 C 函数（void*(void*)），一次调用即得结果，
 * 不会有挂起。Task 的 frame 就是 shim 的调用上下文。
 */
typedef void* (*KaulaSimpleFn)(void* frame);

static KaulaResumeResult simple_trampoline(void* frame_ptr) {
    /*
     * frame 的前两个槽是 { 函数指针, 用户帧 }。
     * 真正的调用在提交时就被包进了这个小结构。
     */
    void** slots = (void**)frame_ptr;
    KaulaSimpleFn call = (KaulaSimpleFn)slots[0];
    void* user_frame = slots[1];

    /*
     * 结果是「指向 Frame 里 result 字段的指针」，不是值本身。
     *
     * 所以这里**不能**释放 user_frame：await 还没读。
     * 释放由 retire_frames 在 epoch 回收期完成。
     */
    return (KaulaResumeResult){ .status = KAULA_RESUME_DONE, .value = call(user_frame) };
}

/* 单一形态的包装帧：{ 函数指针, 用户帧 } */
typedef struct {
    void* fn;
    void* user_frame;
} KaulaSimpleFrame;

/*
 * 懒初始化：编译器生成的代码不要求用户显式 init。
 *
 * 没有这一步的症状很隐蔽：spawn 把任务丢进注入队列，
 * 却没有任何 Worker 来取（worker_count == 0），
 * await 就永久阻塞 —— 程序挂死且一行输出都没有。
 */
static void conc_ensure_init(void) {
    /* 统一对外头不能被本模块包含（与本头互 include）——就地声明 */
    extern int kaula_runtime_is_initialized(void);
    extern int kaula_runtime_ensure_init(void);
    if (!kaula_runtime_is_initialized()) {
        kaula_runtime_ensure_init();
    }
}

static KaulaHandle kaula_spawn_to_flags(KaulaWorker* owner, KaulaResumeFn fn,
                                        KaulaResumeFn enter_fn,
                                        void* frame, size_t frame_size, uint32_t flags);

KaulaHandle kaula_spawn(KaulaResumeFn resume_fn, void* frame, size_t frame_size) {
    conc_ensure_init();
    return kaula_spawn_flags(resume_fn, frame, frame_size, 0);
}

KaulaHandle kaula_spawn_flags(KaulaResumeFn resume_fn, void* frame,
                              size_t frame_size, uint32_t flags) {
    conc_ensure_init();
    if (atomic_load_explicit(&g_conc_runtime.shutdown, memory_order_acquire)) {
        return spawn_failed(KAULA_ERR_SHUTDOWN);
    }

    KaulaWorker* w = kaula_conc_current_worker();
    return kaula_spawn_to_flags(w, resume_fn, NULL, frame, frame_size, flags);
}

KaulaHandle kaula_spawn_enter(KaulaResumeFn resume_fn, KaulaResumeFn enter_fn,
                              void* frame, size_t frame_size) {
    return kaula_spawn_enter_flags(resume_fn, enter_fn, frame, frame_size, 0);
}

KaulaHandle kaula_spawn_enter_flags(KaulaResumeFn resume_fn, KaulaResumeFn enter_fn,
                                    void* frame, size_t frame_size, uint32_t flags) {
    conc_ensure_init();
    if (atomic_load_explicit(&g_conc_runtime.shutdown, memory_order_acquire)) {
        return spawn_failed(KAULA_ERR_SHUTDOWN);
    }

    KaulaWorker* w = kaula_conc_current_worker();
    return kaula_spawn_to_flags(w, resume_fn, enter_fn, frame, frame_size, flags);
}

KaulaHandle kaula_spawn_simple(void* (*fn)(void* frame),
                                void* frame, size_t frame_size) {
    conc_ensure_init();
    if (atomic_load_explicit(&g_conc_runtime.shutdown, memory_order_acquire)) {
        return spawn_failed(KAULA_ERR_SHUTDOWN);
    }

    KaulaTask* task = kaula_task_alloc();
    if (task == NULL) {
        return spawn_failed(KAULA_ERR_OOM);
    }

    /* 包装帧：前两个槽是 { fn, user_frame }，供 trampoline 取用 */
    KaulaSimpleFrame* wrapper = (KaulaSimpleFrame*)kmm_v4_alloc_global(sizeof(KaulaSimpleFrame));
    if (wrapper == NULL) {
        kaula_task_recycle(task);
        return spawn_failed(KAULA_ERR_OOM);
    }
    wrapper->fn = (void*)fn;
    wrapper->user_frame = frame;

    kaula_task_init(task, simple_trampoline, NULL, wrapper, sizeof(KaulaSimpleFrame));
    /*
     * 用户帧挂在 inner_frame 上，随任务退休一起释放。
     *
     * 结果以 &f->result 的形式返回，await 会读它 —— 所以不能在这里
     * 或 trampoline 里释放，必须等没人能读之后（epoch）。
     */
    task->inner_frame = frame;
    task->flags |= KAULA_TASK_FLAG_FRAME_ON_HEAP;
    atomic_store_explicit(&task->owner, NULL, memory_order_relaxed);

    atomic_fetch_add_explicit(&g_conc_runtime.active_task_count, 1, memory_order_relaxed);

    /* 与 kaula_spawn_to 同理：READY 必须在投递之前置，否则会覆盖 DONE */
    atomic_store_explicit(&task->state, KAULA_TASK_READY, memory_order_release);

    if (kaula_conc_worker_post(NULL, task) != 0) {
        /* 全部队列都满：就地执行，保证 spawn 语义不退化 */
        void* r = fn(frame);
        kaula_task_complete(task, r);
        /*
         * wrapper 不在这里 free —— complete 已经把它交给 epoch 了。
         * 手动 free 会与 epoch 的回收撞车（double free），
         * 而 epoch 的回收时机不可预测，所以它未必当场炸。
         */
        return kaula_handle_from_task(task);
    }

    return kaula_handle_from_task(task);
}

/* ========== 唤醒 ========== */

void kaula_scheduler_notify_one(void) {
    uint32_t n = g_conc_runtime.worker_count;
    for (uint32_t i = 0; i < n; i++) {
        if (atomic_load_explicit(&g_conc_workers[i].running, memory_order_acquire)) {
            kaula_worker_wake(&g_conc_workers[i]);
            return;
        }
    }
}

void kaula_scheduler_notify_all(void) {
    uint32_t n = g_conc_runtime.worker_count;
    for (uint32_t i = 0; i < n; i++) {
        kaula_worker_wake(&g_conc_workers[i]);
    }
}

/* ========== Task 回归（IO / 阻塞完成后） ========== */

int kaula_scheduler_resume_task(KaulaTask* task) {
    atomic_store_explicit(&task->state, KAULA_TASK_READY, memory_order_release);
    KaulaWorker* owner = atomic_load_explicit(&task->owner, memory_order_acquire);
    return kaula_conc_worker_post(owner, task);
}

/* ========== 窃取 ========== */

KaulaTask* kaula_scheduler_steal(KaulaWorker* w) {
    uint32_t n = g_conc_runtime.worker_count;
    if (n <= 1) return NULL;

    for (uint32_t i = 0; i < n; i++) {
        uint32_t idx = (w->worker_id + 1 + i) % n;
        if (idx == w->worker_id) continue;

        KaulaTask* t = kaula_ws_deque_steal(&g_conc_workers[idx].local_queue);
        if (t != NULL) {
            atomic_store_explicit(&t->owner, w, memory_order_release);
            t->steal_count++;
            return t;
        }
    }
    return NULL;
}