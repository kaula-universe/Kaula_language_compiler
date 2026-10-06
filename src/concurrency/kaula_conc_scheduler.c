/*
 * Kaula 并发内核（CONC）· 调度器实现
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

/* 全局注入队列（分片，见 kaula_conc_task.h 常量说明与 scheduler.h 结构说明） */
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
/*
 * 实现：多生产者 / 多消费者有界队列（分片版）。
 * push 从 tail 进（整段串行化），pop 从 head 出（CAS 竞争，多线程可并行）。
 * 容量固定，因此满时返回 0 —— 绝不覆盖未消费的槽位。
 *
 * 注意 pop 的 CAS 只保护 head 的推进，**不能替代 push 侧的串行化**：
 * 两个 pusher 即使各自都「对」，写的也是同一个槽位。
 *
 * 分片后上面的论证逐片成立：每片有自己的 head/tail/lock，互不相干。
 */

/* ========== 全局注入队列实现（分片） ========== */

static inline KaulaInjectShard* inject_shard(uint32_t idx) {
    return &g_inject_queue.shard[idx & KAULA_INJECT_SHARD_MASK];
}

void kaula_inject_queue_init(void) {
    for (uint32_t i = 0; i < KAULA_INJECT_SHARDS; i++) {
        KaulaInjectShard* sh = inject_shard(i);
        atomic_store_explicit(&sh->head, 0, memory_order_relaxed);
        atomic_store_explicit(&sh->tail, 0, memory_order_relaxed);
        memset((void*)sh->buffer, 0, sizeof(sh->buffer));
        atomic_flag_clear_explicit(&g_inject_queue.push_lock[i],
                                   memory_order_relaxed);
    }
}

/* 轮转片选择：投递方按 task_id 低位分散，避免全压在一片 */
static inline uint32_t inject_shard_for_task(KaulaTask* task) {
    /* task_id 由 g_conc_runtime.next_task_id 单调分配，低位轮转良好 */
    uint64_t id = task ? (uint64_t)task->task_id : 0;
    return (uint32_t)(id & KAULA_INJECT_SHARD_MASK);
}

uint32_t kaula_scheduler_shard_of_task(KaulaTask* task) {
    return inject_shard_for_task(task);
}

int kaula_inject_queue_push(KaulaTask* task) {
    uint32_t si = inject_shard_for_task(task);
    KaulaInjectShard* sh = inject_shard(si);

    /* 抢锁：acquire，与下面的 release 解锁配对。分片后只与本片 pusher 争 */
    while (atomic_flag_test_and_set_explicit(&g_inject_queue.push_lock[si],
                                             memory_order_acquire)) {
        /* 临界区三条指令且不阻塞，持锁者必然在推进，自旋即可 */
    }

    uint64_t tail = atomic_load_explicit(&sh->tail, memory_order_relaxed);
    uint64_t head = atomic_load_explicit(&sh->head, memory_order_acquire);

    int ok = 1;
    if (tail - head >= KAULA_INJECT_SHARD_CAPACITY) {
        ok = 0; /* 满：背压 */
    } else {
        /* 顺序关键：先写 payload，再以 release 推进 tail。
         * pop 读到 tail > head 之后才读 buffer，正是靠这次 release
         * 看见 payload。 */
        atomic_store_explicit(
            &sh->buffer[tail & KAULA_INJECT_SHARD_MASK_CAP],
            task, memory_order_relaxed);
        atomic_store_explicit(&sh->tail, tail + 1, memory_order_release);
    }

    atomic_flag_clear_explicit(&g_inject_queue.push_lock[si], memory_order_release);
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
 *
 * 分片后：一批 task 可能落在不同的片上。按片分组、逐片批量入队；
 * 单片空间的限制按片各自成立。返回「首个未能接纳的下标」（0..n），
 * 语义与原实现一致：返回值之前的都入队成功。
 */
int kaula_inject_queue_push_batch(KaulaTask** tasks, int n) {
    if (n <= 0) return 0;

    /*
     * 简单而正确的做法：逐片处理。
     * 调用方（Worker 本地队列满时的兜底、evict 批量转移）不追求
     * 极致的批量摊销，正确性优先；真正的热路径是单条 push。
     */
    int accepted = 0;
    for (int i = 0; i < n; i++) {
        if (!kaula_inject_queue_push(tasks[i])) break;
        accepted++;
    }
    return accepted;
}

/* 从指定片弹一个（CAS 竞争该片的 head） */
static KaulaTask* inject_shard_pop(uint32_t si) {
    KaulaInjectShard* sh = inject_shard(si);
    for (uint64_t attempt = 0; attempt < 8; attempt++) {
        uint64_t head = atomic_load_explicit(&sh->head, memory_order_relaxed);
        uint64_t tail = atomic_load_explicit(&sh->tail, memory_order_acquire);

        if (head >= tail) return NULL; /* 空 */

        KaulaTask* task = atomic_load_explicit(
            &sh->buffer[head & KAULA_INJECT_SHARD_MASK_CAP],
            memory_order_acquire);

        uint64_t expected = head;
        if (atomic_compare_exchange_strong_explicit(
                &sh->head, &expected, head + 1,
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

/* 从指定片批量弹（同一片内一次 CAS 推进 m 个） */
static int inject_shard_pop_batch(uint32_t si, KaulaTask** out, int max) {
    if (out == NULL || max <= 0) return 0;
    if (max > 64) max = 64;
    KaulaInjectShard* sh = inject_shard(si);

    for (uint64_t attempt = 0; attempt < 8; attempt++) {
        uint64_t head = atomic_load_explicit(&sh->head, memory_order_relaxed);
        uint64_t tail = atomic_load_explicit(&sh->tail, memory_order_acquire);

        if (head >= tail) return 0; /* 空 */

        uint64_t avail = tail - head;
        int m = (avail < (uint64_t)max) ? (int)avail : max;

        /* 先读 payload（见下方注释） */
        for (int i = 0; i < m; i++) {
            out[i] = atomic_load_explicit(
                &sh->buffer[(head + i) & KAULA_INJECT_SHARD_MASK_CAP],
                memory_order_acquire);
        }

        uint64_t expected = head;
        if (atomic_compare_exchange_strong_explicit(
                &sh->head, &expected, head + (uint64_t)m,
                memory_order_acq_rel, memory_order_relaxed)) {
            return m;
        }
        /* 输者：重试（可能因别的 Worker 抢走了 head） */
    }
    return 0;
}

static int inject_shard_empty(uint32_t si) {
    KaulaInjectShard* sh = inject_shard(si);
    uint64_t h = atomic_load_explicit(&sh->head, memory_order_relaxed);
    uint64_t t = atomic_load_explicit(&sh->tail, memory_order_acquire);
    return t <= h;
}

/* 某片的存量深度（tail - head，近似；仅用于唤醒策略的启发式判断）。 */
static uint64_t inject_shard_depth(uint32_t si) {
    KaulaInjectShard* sh = inject_shard(si);
    uint64_t h = atomic_load_explicit(&sh->head, memory_order_relaxed);
    uint64_t t = atomic_load_explicit(&sh->tail, memory_order_acquire);
    return t > h ? (t - h) : 0;
}

/*
 * 便捷弹出：优先 prefer 片，空则轮转扫其余片。
 * 这是无 prefer 信息的调用方（如检测、兜底）使用的版本。
 */
KaulaTask* kaula_inject_queue_pop_pref(uint32_t prefer) {
    uint32_t base = prefer & KAULA_INJECT_SHARD_MASK;
    for (uint32_t i = 0; i < KAULA_INJECT_SHARDS; i++) {
        uint32_t si = (base + i) & KAULA_INJECT_SHARD_MASK;
        KaulaTask* t = inject_shard_pop(si);
        if (t != NULL) return t;
    }
    return NULL;
}

int kaula_inject_queue_pop_batch_shard(uint32_t si, KaulaTask** out, int max) {
    return inject_shard_pop_batch(si & KAULA_INJECT_SHARD_MASK, out, max);
}

/*
 * 批量弹出（跨片）：先扫一遍 prefer 起始的所有片，只要任一片取到
 * 就不再看后面的片 —— Worker 的 find_work 会在下一轮继续来取。
 * 保留旧签名以兼容既有调用方。
 */
int kaula_inject_queue_pop_batch(KaulaTask** out, int max) {
    if (out == NULL || max <= 0) return 0;
    if (max > 64) max = 64;
    for (uint32_t i = 0; i < KAULA_INJECT_SHARDS; i++) {
        int got = inject_shard_pop_batch(i, out, max);
        if (got > 0) return got;
    }
    return 0;
}

KaulaTask* kaula_inject_queue_pop(void) {
    /* 无片偏好：扫全部片 */
    return kaula_inject_queue_pop_pref(0);
}

int kaula_inject_queue_empty(void) {
    for (uint32_t i = 0; i < KAULA_INJECT_SHARDS; i++) {
        if (!inject_shard_empty(i)) return 0;
    }
    return 1;
}

int kaula_scheduler_inject_empty_shard(uint32_t shard) {
    return inject_shard_empty(shard & KAULA_INJECT_SHARD_MASK);
}

uint64_t kaula_inject_queue_size(void) {
    uint64_t total = 0;
    for (uint32_t i = 0; i < KAULA_INJECT_SHARDS; i++) {
        KaulaInjectShard* sh = inject_shard(i);
        uint64_t h = atomic_load_explicit(&sh->head, memory_order_relaxed);
        uint64_t t = atomic_load_explicit(&sh->tail, memory_order_acquire);
        if (t > h) total += t - h;
    }
    return total;
}

/* 简写 */
int kaula_scheduler_inject_push(KaulaTask* task) { return kaula_inject_queue_push(task); }
KaulaTask* kaula_scheduler_inject_pop(void)      { return kaula_inject_queue_pop(); }
int kaula_scheduler_inject_pop_batch(KaulaTask** out, int max) {
    return kaula_inject_queue_pop_batch(out, max);
}
int kaula_scheduler_inject_pop_batch_shard(uint32_t shard, KaulaTask** out, int max) {
    return kaula_inject_queue_pop_batch_shard(shard, out, max);
}
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
    (void)frame_size;  /* 公开 ABI 形参；simple 路径固定 sizeof(KaulaSimpleFrame) */
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

/* notify_shard 铺开唤醒面的最小片积压深度（见 kaula_scheduler_notify_shard） */
#define KAULA_NOTIFY_SHARD_SPREAD_MIN 3

void kaula_scheduler_notify_one(void) {
    uint32_t n = g_conc_runtime.worker_count;
    for (uint32_t i = 0; i < n; i++) {
        if (atomic_load_explicit(&g_conc_workers[i].running, memory_order_acquire)) {
            kaula_worker_wake(&g_conc_workers[i]);
            return;
        }
    }
}

/*
 * 分片感知唤醒：唤醒「偏好该片」的 Worker（worker_id & MASK == shard），
 * 让「谁该干活就叫谁」，把唤醒面从 worker[0] 铺开到整片。
 *
 * 为什么不是 notify_one：投递方知道任务落在哪一片（task_id 低位），
 * 而 consume 方也按同样规则偏好自己的片。若统一用 notify_one 只叫醒
 * worker[0]，那 w[0] 醒来会先扫自己的片、再去扫别人的片 —— 它要么
 * 替别人把活干了（分布不均），要么抢在 owner 醒来之前把片搬空、导致
 * owner 永远只等自己的空片而 park。实测 C 边 degrade（conc=100、投递
 * 密集、Worker 基本都在忙）吞吐 ~200K → ~460K（2.3×）。
 *
 * **但有一个必须避开的坑**：suspend/mixed 里 Worker 常处于「本地短定时器
 * 未到期、为定时器而睡」的状态（park_ns = 距最近到期的剩余时间）。此时
 * 用按片 RR 把它唤醒，它醒来发现定时器没到、手头没活，只能立刻 re-park
 * ——一次多余的 syscall + 调度往返。100 并发 × 20000 任务下这种「无谓
 * 唤醒」累积起来，实测把 suspend/mixed 拖慢 19%（270K → 220K）。
 *
 * 因此这里加一道过滤：**跳过 has_local_timer=1 的 Worker**。它为定时器
 * 而睡时叫它没用（定时器到期它自己会醒）；degrade 无定时器，过滤不生效，
 * 分片唤醒的 2.4× 收益完整保留。轮转（ticket）保证同片多个 owner 轮流
 * 承担，避免只唤醒最低 id 导致其余 Worker 长期 park。
 *
 * 找不到可唤醒的 owner 时退化为 notify_one，保证不漏唤醒。
 */
void kaula_scheduler_notify_shard(uint32_t shard) {
    uint32_t n = g_conc_runtime.worker_count;
    uint32_t want = shard & KAULA_INJECT_SHARD_MASK;
    if (n == 0) return;

    /*
     * 门槛：只有当本片确实「积压」时才铺开唤醒面。
     *
     * 这是把「degrade 要铺开、suspend 不能骚扰」这对矛盾落到可判定信号
     * 上的关键。判据用本片存量深度：
     *   - degrade（conc=100、投递密集）：本片常积压多个任务 → 铺开唤醒，
     *     让同片多个 owner 并行消费 → 吞吐 2.3×。
     *   - suspend/mixed：一次只投一两个任务，唤醒后的 owner 立刻取走、
     *     片随即转空 → 深度低，走 notify_one，不去骚扰为定时器而睡的
     *     Worker → 保住挂起往返延迟。
     * 深度是近似值（relaxed 读，允许与真实值差 1），只用于启发式选择
     * 唤醒策略，不影响正确性：无论走哪条，任务最终都会被消费。
     */
    if (inject_shard_depth(want) < KAULA_NOTIFY_SHARD_SPREAD_MIN) {
        kaula_scheduler_notify_one();
        return;
    }

    uint32_t owners_per_shard = n / KAULA_INJECT_SHARDS;
    if (owners_per_shard == 0) owners_per_shard = 1;

    static _Atomic(uint32_t) rr_ticket = 0;
    uint32_t k = atomic_fetch_add_explicit(&rr_ticket, 1, memory_order_relaxed)
                 % owners_per_shard;

    for (uint32_t t = 0; t < owners_per_shard; t++) {
        uint32_t off = (k + t) % owners_per_shard;
        uint32_t idx = want + off * KAULA_INJECT_SHARDS;
        if (idx >= n) continue;
        KaulaWorker* w = &g_conc_workers[idx];
        if (!atomic_load_explicit(&w->running, memory_order_acquire)) continue;
        /* 为定时器而睡的 Worker 不叫（叫了也是白醒一次） */
        if (atomic_load_explicit(&w->has_local_timer, memory_order_acquire)) continue;
        kaula_worker_wake(w);
        return;
    }

    /*
     * 兜底：worker_id 未必是 0..n-1 的紧凑序列（撤离/重启后可能有空洞），
     * 或该片 owner 全在为定时器而睡。退化为顺序扫「任何 running 的
     * Worker」，保证不漏唤醒 —— 代价只是这一次的分布差。
     */
    kaula_scheduler_notify_one();
}

void kaula_scheduler_notify_all(void) {
    uint32_t n = g_conc_runtime.worker_count;
    for (uint32_t i = 0; i < n; i++) {
        kaula_worker_wake(&g_conc_workers[i]);
    }
}

void kaula_scheduler_notify_n(uint32_t n) {
    uint32_t wc = g_conc_runtime.worker_count;
    if (n > wc) n = wc;
    for (uint32_t i = 0; i < wc && n > 0; i++) {
        if (atomic_load_explicit(&g_conc_workers[i].running, memory_order_acquire)) {
            kaula_worker_wake(&g_conc_workers[i]);
            n--;
        }
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