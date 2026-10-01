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

/* ========== 全局注入队列实现 ========== */

/*
 * 实现：单生产者槽位用 CAS 抢占 head。
 * push 从 tail 进（CAS 竞争），pop 从 head 出（多线程可竞争）。
 * 容量固定，因此满时返回 0 —— 绝不覆盖未消费的槽位。
 */

void kaula_inject_queue_init(void) {
    atomic_store_explicit(&g_inject_queue.head, 0, memory_order_relaxed);
    atomic_store_explicit(&g_inject_queue.tail, 0, memory_order_relaxed);
    memset((void*)g_inject_queue.buffer, 0, sizeof(g_inject_queue.buffer));
}

int kaula_inject_queue_push(KaulaTask* task) {
    uint64_t tail = atomic_load_explicit(&g_inject_queue.tail, memory_order_relaxed);
    uint64_t head = atomic_load_explicit(&g_inject_queue.head, memory_order_acquire);

    if (tail - head >= KAULA_INJECT_QUEUE_CAPACITY) {
        return 0; /* 满：背压 */
    }

    atomic_store_explicit(
        &g_inject_queue.buffer[tail & KAULA_INJECT_QUEUE_MASK],
        task, memory_order_relaxed);
    atomic_store_explicit(&g_inject_queue.tail, tail + 1, memory_order_release);
    return 1;
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
            /* 赢者：清空槽位防止悬垂引用被误读 */
            atomic_store_explicit(
                &g_inject_queue.buffer[head & KAULA_INJECT_QUEUE_MASK],
                NULL, memory_order_relaxed);
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

KaulaHandle kaula_spawn_to(KaulaWorker* owner, KaulaResumeFn fn, void* frame, size_t frame_size) {
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
    kaula_task_init(task, fn, frame, frame_size);
    atomic_store_explicit(&task->owner, owner, memory_order_relaxed);

    atomic_fetch_add_explicit(&g_conc_runtime.active_task_count, 1, memory_order_relaxed);
    kaula_conc_metrics_inc_spawned();

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

    atomic_store_explicit(&task->state, KAULA_TASK_READY, memory_order_release);
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

    return (KaulaResumeResult){ .status = KAULA_RESUME_DONE, .value = call(user_frame) };
}

/* 单一形态的包装帧：{ 函数指针, 用户帧 } */
typedef struct {
    void* fn;
    void* user_frame;
} KaulaSimpleFrame;

KaulaHandle kaula_spawn_simple(void* (*fn)(void* frame),
                                void* frame, size_t frame_size) {
    (void)frame_size;

    if (atomic_load_explicit(&g_conc_runtime.shutdown, memory_order_acquire)) {
        return spawn_failed(KAULA_ERR_SHUTDOWN);
    }

    KaulaTask* task = kaula_task_alloc();
    if (task == NULL) {
        return spawn_failed(KAULA_ERR_OOM);
    }

    /* 包装帧：前两个槽是 { fn, user_frame }，其余是 conc fn 的结果槽 */
    KaulaSimpleFrame* wrapper = (KaulaSimpleFrame*)kmm_v4_alloc_global(sizeof(KaulaSimpleFrame));
    if (wrapper == NULL) {
        kaula_task_recycle(task);
        return spawn_failed(KAULA_ERR_OOM);
    }
    wrapper->fn = (void*)fn;
    wrapper->user_frame = frame;

    kaula_task_init(task, simple_trampoline, wrapper, sizeof(KaulaSimpleFrame));
    atomic_store_explicit(&task->owner, NULL, memory_order_relaxed);

    atomic_fetch_add_explicit(&g_conc_runtime.active_task_count, 1, memory_order_relaxed);

    if (kaula_conc_worker_post(NULL, task) != 0) {
        /* 全部队列都满：就地执行，保证 spawn 语义不退化 */
        void* r = fn(frame);
        kaula_task_complete(task, r);
        kmm_v4_survivor_free(wrapper);
        return kaula_handle_from_task(task);
    }

    atomic_store_explicit(&task->state, KAULA_TASK_READY, memory_order_release);
    return kaula_handle_from_task(task);
}

KaulaHandle kaula_spawn(KaulaResumeFn resume_fn, void* frame, size_t frame_size) {
    KaulaWorker* w = kaula_conc_current_worker();

    if (w == NULL) {
        /*
         * 非 Worker 线程（main / 外部线程）发起 spawn。
         * 不能建 Worker 归属，走"owner=NULL + 注入队列"，
         * 由任一 Worker 取走执行。若一个 Worker 都没有，则原地执行。
         */
        if (g_conc_runtime.worker_count == 0) {
            return kaula_spawn_to(NULL, resume_fn, frame, frame_size);
        }
        return kaula_spawn_to(NULL, resume_fn, frame, frame_size);
    }
    return kaula_spawn_to(w, resume_fn, frame, frame_size);
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