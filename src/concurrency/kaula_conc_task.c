/*
 * Kaula 并发内核 · 任务生命周期
 *
 * 修复记录：
 * - 原 kaula_handle_await 是轮询 `while (!done_flag) sleep_us(10)`，
 *   空耗 CPU 且唤醒延迟不确定。现改为 park/unpark（事件语义），
 *   并在 Worker 上下文里协作执行其他任务。
 * - 原 kaula_task_recycle 从未被调用 —— Task 与帧全泄漏。
 *   现改为：结果被取走（或无需保留）后走 epoch 延迟回收（P5）。
 * - 原实现用裸 `KaulaHandle.task` 指针 + 立即释放的组合，
 *   一旦真的释放就是 UAF。现改为 epoch 保护（见 kaula_conc_epoch.h）。
 */

#include "kaula_conc_task.h"
#include "kaula_conc_worker.h"
#include "kaula_conc_scheduler.h"
#include "kaula_conc_epoch.h"
#include "kaula_conc_completion.h"
#include "kaula_conc_metrics.h"
#include "../kmm_scoped_allocator_v4.h"

/* 统一对外头不能被本模块包含（与本头互 include）——就地声明用到的两个符号 */
extern int kaula_runtime_ensure_init(void);

#include <stdlib.h>
#include <string.h>

/* ========== 分配 ========== */

KaulaTask* kaula_task_alloc(void) {
    /*
     * 从 KMM survivor 段分配（跨 scope 存活）。
     * 帧也来自同一段，故回收时能成对释放。
     */
    KaulaTask* task = (KaulaTask*)kmm_v4_alloc_global(sizeof(KaulaTask));
    if (task == NULL) return NULL;

    memset(task, 0, sizeof(*task));
    task->task_id = atomic_fetch_add_explicit(&g_conc_runtime.next_task_id, 1,
                                              memory_order_relaxed);
    atomic_store_explicit(&task->state, KAULA_TASK_CREATED, memory_order_relaxed);
    task->created_ns = kaula_p_now_ns();

    if (kaula_p_event_create(&task->done_event) != 0) {
        kmm_v4_survivor_free(task);
        return NULL;
    }
    atomic_store(&task->waiters, 0);
    return task;
}

void kaula_task_init(KaulaTask* task, KaulaResumeFn fn, void* frame, size_t frame_size) {
    task->resume_fn = fn;
    task->frame = frame;
    task->frame_size = frame_size;
    task->inner_frame = NULL;
    /*
     * 提交时刻在这里打点。
     *
     * 必须在「还没进队列」时取 —— 等 post 之后再取就把投递耗时排除在外，
     * 而那正是调度器该被计入的成本。
     */
    task->submit_ns = kaula_p_now_ns();
    task->complete_ns = 0;
}

/* ========== 完成 ========== */

void kaula_task_wake_waiters(KaulaTask* task) {
    if (atomic_load(&task->waiters) > 0) {
        kaula_p_event_signal(&task->done_event);
    }
}

/*
 * retire_frames 把任务持有的帧交给 epoch 延迟释放。
 *
 * 不能在 kaula_task_complete 里直接 free：await 可能正持有 handle
 * 并准备读 frame 里的 result 字段。epoch 保证所有活跃 Worker 离开后
 * 才真正释放，那时已经没人能读到它了。
 *
 * 两块帧（内核包装帧 + 用户帧）分别退休 —— epoch 只会 survivor_free
 * 它拿到的那个指针，没有析构钩子，所以得分两次挂。
 */
static void retire_frames(KaulaTask* task) {
    if (task->flags & KAULA_TASK_FLAG_FRAME_ON_HEAP) {
        if (task->inner_frame != NULL) {
            kaula_epoch_retire(task->inner_frame);
            task->inner_frame = NULL;
        }
        if (task->frame != NULL && task->frame_size > 0) {
            kaula_epoch_retire(task->frame);
            task->frame = NULL;
        }
    }
}

void kaula_task_complete(KaulaTask* task, void* result) {
    task->result = result;
    task->error = KAULA_OK;

    /*
     * 完成时刻在这里打点，**必须早于置 DONE**。
     *
     * 晚一步就晚了：置 DONE 之后唤醒等待者，等待者可能立刻读
     * complete_ns，而那时它还是 0。放在 release 存之前，
     * 对任何看到 DONE 的读者都可见。
     */
    task->complete_ns = kaula_p_now_ns();

    /*
     * 先发布结果，再置状态 —— 保证看到 DONE 的线程一定看到 result。
     */
    atomic_store_explicit(&task->state, KAULA_TASK_DONE, memory_order_release);

    /*
     * 统计与活跃计数必须在唤醒等待者之前更新完。
     * 否则等待者被唤醒后立刻读 metrics，会看到一个"任务已完成但
     * 完成计数还没涨"的瞬时不一致状态。
     */
    atomic_fetch_sub_explicit(&g_conc_runtime.active_task_count, 1,
                              memory_order_acq_rel);
    kaula_conc_metrics_inc_completed();

    kaula_completion_push(task);

    /* 唤醒放最后：此刻任务的状态与统计都已就绪 */
    kaula_task_wake_waiters(task);

    /*
     * 延迟回收（P5）。
     * 不能立即 free：可能有线程正持有 KaulaHandle.task 指针在 await。
     * epoch 保证所有活跃 Worker 都离开后才会真正释放。
     */
    retire_frames(task);
    kaula_epoch_retire(task);
}

void kaula_task_fail(KaulaTask* task, KaulaError err) {
    task->result = NULL;
    task->error = (int)err;
    task->complete_ns = kaula_p_now_ns();   /* 失败路径同样打点 */

    atomic_store_explicit(&task->state, KAULA_TASK_DONE, memory_order_release);
    kaula_task_wake_waiters(task);

    atomic_fetch_sub_explicit(&g_conc_runtime.active_task_count, 1,
                              memory_order_acq_rel);
    kaula_conc_metrics_inc_failed();

    retire_frames(task);
    kaula_epoch_retire(task);
}

int kaula_task_cancel(KaulaTask* task) {
    /* #[hint(unintrusive)] 的任务不可取消 */
    if (task->flags & KAULA_TASK_FLAG_UNINTRUSIVE) return 0;
    if (atomic_load_explicit(&task->state, memory_order_acquire) == KAULA_TASK_DONE) {
        return 0;   /* 已完成，无需取消 */
    }
    atomic_store_explicit(&task->cancelled, 1, memory_order_release);

    /* 正在 park 等 IO 的任务需要被唤醒才能观察到取消 */
    int st = atomic_load_explicit(&task->state, memory_order_acquire);
    if (st == KAULA_TASK_SUSPENDED) {
        task->io_result = 0;
        kaula_scheduler_resume_task(task);
    }
    return 1;
}

/* ========== 等待 ========== */

static int task_is_done(const KaulaTask* task) {
    return atomic_load_explicit(&task->state, memory_order_acquire) == KAULA_TASK_DONE;
}

int kaula_task_await(KaulaTask* task, uint64_t timeout_ns) {
    /* NULL 无结果可等，视为"已完成"（返回 1），结果指针为 NULL */
    if (task == NULL) return 1;
    if (task_is_done(task)) return 1;

    KaulaWorker* w = kaula_conc_current_worker();

    if (w != NULL) {
        /*
         * Worker 上下文：绝不阻塞。
         * 协作执行其他任务直到目标完成 —— 这是 work-stealing 的
         * 另一种形态（帮别人干活），比 park 更有效。
         */
        atomic_fetch_add_explicit(&task->waiters, 1, memory_order_acq_rel);
        uint64_t deadline = (timeout_ns == KAULA_P_WAIT_FOREVER)
                          ? 0 : kaula_p_now_ns() + timeout_ns;

        /* 复查：同上，避免"注册前已完成"导致的漏唤醒 */
        if (task_is_done(task)) {
            atomic_fetch_sub_explicit(&task->waiters, 1, memory_order_acq_rel);
            return 1;
        }

        while (!task_is_done(task)) {
            KaulaTask* other = kaula_conc_worker_find_work(w);
            if (other != NULL) {
                kaula_worker_execute_task(w, other);
                continue;
            }
            /* 无事可做：短暂 park 而不是死等 */
            kaula_p_event_wait(&w->park_event, 1000000ULL);

            if (deadline != 0 && kaula_p_now_ns() >= deadline) break;
        }
        atomic_fetch_sub_explicit(&task->waiters, 1, memory_order_acq_rel);
        return task_is_done(task) ? 1 : 0;
    }

    /* 非 Worker 线程：park 到 done_event */
    atomic_fetch_add_explicit(&task->waiters, 1, memory_order_acq_rel);

    /*
     * 注册后必须复查一次状态。
     *
     * 否则存在这个窗口：
     *   本线程        上面的 task_is_done() 返回 false
     *   执行者线程    置 DONE，并看到 waiters == 0 → 不发信号
     *   本线程        waiters 加 1，然后 park → 永远等不到信号
     *
     * 这个死锁窗口很窄（几条指令），但 20 个定时器任务里
     * 偶尔撞上一次就足以让整个测试挂死 —— 实测 3 次里挂 2 次。
     */
    if (task_is_done(task)) {
        atomic_fetch_sub_explicit(&task->waiters, 1, memory_order_acq_rel);
        return 1;
    }

    int r;
    if (timeout_ns == KAULA_P_WAIT_FOREVER) {
        r = kaula_p_event_wait(&task->done_event, KAULA_P_WAIT_FOREVER);
    } else {
        /*
         * 分段等待：每次 50ms 后复查 running 状态，
         * 避免 shutdown 时永久挂住。
         */
        uint64_t deadline = kaula_p_now_ns() + timeout_ns;
        r = 0;
        while (kaula_p_now_ns() < deadline) {
            uint64_t remaining = deadline - kaula_p_now_ns();
            uint64_t slice = remaining > 50000000ULL ? 50000000ULL : remaining;
            if (kaula_p_event_wait(&task->done_event, slice)) { r = 1; break; }
            if (task_is_done(task)) { r = 1; break; }
            if (atomic_load(&g_conc_runtime.shutdown)) break;
        }
    }
    atomic_fetch_sub_explicit(&task->waiters, 1, memory_order_acq_rel);

    return task_is_done(task) ? 1 : (r ? 1 : 0);
}

/* ========== 回收 ========== */

void kaula_task_recycle(KaulaTask* task) {
    /*
     * 显式回收：仅在没有等待者时安全。
     * 正常路径下任务完成后已由 kaula_task_complete 交给 epoch，
     * 此函数用于"创建后立刻放弃"的路径（如 spawn 被拒后的清理）。
     */
    if (task == NULL) return;

    if (task->frame != NULL && task->frame_size > 0 &&
        (task->flags & KAULA_TASK_FLAG_FRAME_ON_HEAP)) {
        kmm_v4_survivor_free(task->frame);
        task->frame = NULL;
    }
    if (kaula_p_event_is_valid(&task->done_event)) {
        kaula_p_event_destroy(&task->done_event);
        kaula_p_event_invalidate(&task->done_event);
    }
    kmm_v4_survivor_free(task);
}

/* ========== Handle ========== */

/*
 * 句柄持有 pin：安全期的定义是「从 spawn 到 await 读完」。
 *
 * 只在 await 里 pin 是不够的 —— 那是马后炮：
 *   main: spawn 返回 ──── 某个 Worker: 完成 → retire → 立刻 reclaim ──── main: pin, await
 * 这个窗口里帧已经被释放了。醒来 memcpy 读到的是已回收内存
 * （表现为一个像指针的垃圾数），而它往往不崩。
 *
 * 所以 pin 在句柄产生的那一刻拿，由 kaula_handle_release 归还。
 *
 * 归还点有两个，都是「句柄不再可能被读」的时刻：
 *   - await_into 读完（声明位置，编译器走这条）
 *   - 句柄离开作用域（编译器在作用域出口发射 kaula_handle_release）
 *
 * 计数放在 Task 上而不是句柄结构里，是为了让 release 幂等：
 * 编译器若在 await_into 之后再发一次 release（作用域出口那一发），
 * 第二次会发现计数已经归零并补回去，不会把 epoch pin 多放一次 ——
 * 多放一次就是提前回收，那是悬垂，比泄漏危险得多。
 *
 * 漏发 release 仍然是可能的（那是编译器侧的保守退化）：
 * pin 不归还 → 那块内存留到 shutdown。可观测的泄漏，不是悬垂。
 */
KaulaHandle kaula_handle_from_task(KaulaTask* task) {
    KaulaHandle h;
    h.task_id = task->task_id;
    h.task = task;
    atomic_fetch_add_explicit(&task->pin_refs, 1, memory_order_acq_rel);
    kaula_epoch_pin();
    return h;
}

/*
 * kaula_handle_release 归还句柄持有的那一个 pin。
 *
 * 幂等：多调一次只是什么都不做，不会把 pin 放成负数。
 * 放在 Task 上做 CAS 递减，最后一个持有者才真正 unpin。
 */
void kaula_handle_release(KaulaHandle h) {
    if (h.task == NULL) return;
    int prev = atomic_fetch_sub_explicit(&h.task->pin_refs, 1, memory_order_acq_rel);
    if (prev == 1) {
        kaula_epoch_unpin();
    } else if (prev <= 0) {
        /* 释放次数多于句柄数：补回去，别让计数变负（那会让下一次 release 少放一个 pin） */
        atomic_fetch_add_explicit(&h.task->pin_refs, 1, memory_order_acq_rel);
    }
}

/* 测试可观测：当前还欠多少个 epoch pin（未归还的句柄数） */
int kaula_handle_pin_debt(KaulaHandle h) {
    if (h.task == NULL) return 0;
    int n = atomic_load_explicit(&h.task->pin_refs, memory_order_acquire);
    return n > 0 ? n : 0;
}

KaulaHandle kaula_handle_null(void) {
    KaulaHandle h = {0, NULL};
    return h;
}

int kaula_handle_is_null(KaulaHandle h) {
    return h.task == NULL;
}

int kaula_handle_error(KaulaHandle h) {
    if (h.task != NULL) return h.task->error;
    /* task == NULL 时错误码放在 task_id 里 */
    return (int)h.task_id;
}

/*
 * await 的结果约定：内核返回的是「结果所在内存的地址」，不是结果本身。
 *
 * 那个地址指向 Task 的 Frame 里的一块字段。Frame 在任务完成时就随任务
 * 一起 retire 了，所以地址的有效期只到"epoch 判定没人再读"为止。
 *
 * 因此 await 必须 pin 住整个「等 + 读」过程，而且读出来的内容必须
 * 在 pin 期间拷走 —— 返回裸指针等于把一个随时会悬垂的地址交给调用者。
 * main 这类非 Worker 线程没有 epoch 槽位，不 pin 就等于告诉回收器
 * 「没人会读，随便收」：醒来读到的可能是已释放内存，
 * 或者更糟 —— 一个已回收并复用的 Frame 里残留的旧值。
 */

/* 核心：等完成后把结果拷进调用者的缓冲区 */
/*
 * 上一次 await 的服务时间。
 *
 * 定义在 task.c 而不是 worker.c（那里有 kaula_conc_tls_io_result），
 * 因为它属于 await 路径 —— 与它记的量同生命周期。
 */
_Thread_local uint64_t kaula_conc_tls_last_latency_ns = 0;

/* Task 的服务时间；打点缺失或时钟倒流时返回 0 —— 不编造数值 */
uint64_t kaula_task_latency_ns(const KaulaTask* task) {
    if (task == NULL) return 0;
    uint64_t c = task->complete_ns;
    uint64_t s = task->submit_ns;
    return (c > s) ? (c - s) : 0;
}

static int await_copy_out(KaulaHandle h, void* dst, size_t dst_size, uint64_t timeout_ns) {
    if (h.task == NULL || dst == NULL) return 0;

    /*
     * 句柄在 kaula_handle_from_task 里已经 pin 过，
     * 安全期从 spawn 就开始 —— 这里不能只靠本次 pin。
     */
    int ok = kaula_task_await(h.task, timeout_ns);
    if (ok) {
        const void* src = h.task->result;
        if (src != NULL && dst_size > 0) {
            memcpy(dst, src, dst_size);
        }
        /*
         * 把本次 await 的服务时间留在 TLS 里，供调用方随后取用。
         *
         * 走 TLS 而不给 await 加出参：codegen 只生成
         * `kaula_handle_await_into(h, &x, sizeof(x))` 这一种形态，
         * 改出参会牵动所有 await 的生成与 ABI。一个无参访问器就够
         * 基准与诊断用了。
         */
        kaula_conc_tls_last_latency_ns = kaula_task_latency_ns(h.task);
    } else {
        kaula_conc_tls_last_latency_ns = 0;
    }

    /* 读完了，归还句柄持有的那个 pin */
    kaula_handle_release(h);
    return ok;
}

/*
 * kaula_conc_last_latency_ns —— 上一次 await 的服务时间（纳秒）。
 *
 * 与「谁先取到结果」无关，所以延迟统计该用它：并发窗口里按调用方
 * 掐表量到的是排队 + 顺序取回，与服务时间能差两个数量级。
 *
 * 编译器生成的 await 形态不带出参，基准与诊断代码经
 *   extern fn kaula_conc_last_latency_ns() -> u64
 * 取值（extern 形参里放不了 Handle，无参就没这个问题）。
 */
uint64_t kaula_conc_last_latency_ns(void) {
    return kaula_conc_tls_last_latency_ns;
}

/*
 * await_into：把结果直接写进调用者的变量。
 *
 * 这是编译器唯一应当使用的 await 入口 —— 调用点知道结果的类型与大小，
 * 由它提供 dst，拷贝就发生在一个大小明确、类型正确、栈上的位置。
 * 不需要内核猜类型，也不需要分配临时缓冲（那会漏）。
 */
void kaula_handle_await_into(KaulaHandle h, void* dst, size_t dst_size) {
    kaula_handle_await_into_timed(h, dst, dst_size, NULL);
}

/*
 * await_into_timed：await 的同时给出「内核测得的服务时间」。
 *
 * out_latency_ns = complete_ns - submit_ns，即任务从被创建到真正做完
 * 花了多久。这个量与「调用方什么时候轮到去取结果」无关 ——
 * 并发窗口里那两者能差两个数量级，所以延迟统计必须用这个。
 *
 * out_latency_ns 可为 NULL（不关心延迟）。
 */
void kaula_handle_await_into_timed(KaulaHandle h, void* dst, size_t dst_size,
                                   uint64_t* out_latency_ns) {
    /*
     * 任务若是外部（IoDriver / 阻塞车道）线程直接置的 DONE，
     * 唤醒事件可能还没建好。走 ensure_init 会顺带补建。
     */
    kaula_runtime_ensure_init();

    if (out_latency_ns != NULL) *out_latency_ns = 0;

    if (h.task == NULL) {
        if (dst != NULL && dst_size > 0) memset(dst, 0, dst_size);
        return;
    }

    if (!await_copy_out(h, dst, dst_size, KAULA_P_WAIT_FOREVER)) {
        memset(dst, 0, dst_size);
        return;
    }

    if (out_latency_ns != NULL) {
        /*
         * 只能取 await_copy_out 留下的 TLS 值，**不能回头读 h.task**。
         *
         * await_copy_out 在拷贝完结果后就 kaula_handle_release(h) 了 ——
         * 那是句柄持有的最后一个引用，Task 可能已经被回收。
         * 此时再读 h.task->complete_ns 就是 use-after-free。
         *
         * 这个坑的表现很有欺骗性：不立刻崩，而是安静地返回上一个
         * 任务的旧值，或者在别处崩（本机实测是 0xC0000005）。
         *
         * TLS 值是在 release **之前**、pin 仍然有效时取的，所以它安全。
         */
        *out_latency_ns = kaula_conc_tls_last_latency_ns;
    }
}

int kaula_handle_try_await_into(KaulaHandle h, void* dst, size_t dst_size, uint64_t timeout_ns) {
    if (h.task == NULL) {
        if (dst != NULL && dst_size > 0) memset(dst, 0, dst_size);
        return 0;
    }
    return await_copy_out(h, dst, dst_size, timeout_ns);
}

/*
 * await 返回「结果所在内存的地址」。
 *
 * 与 await_into 的区别只在于 pin 的去留：
 *   await_into  读完立刻归还句柄持有的 pin —— 推荐，声明处用它
 *   await       不归还：返回的是「结果的地址」，调用方接着要解引用它，
 *               此刻 pin 还得留着。归还点交给 kaula_handle_release ——
 *               编译器在句柄离开作用域时会发一次。
 *
 * 所以表达式位置的 await（`h1.await + h2.await`）不再把那块内存
 * 永久留到 shutdown：它在作用域出口就还掉了 pin。
 *
 * 但地址本身的生命周期仍然是「到 epoch 判定没人再读为止」，所以
 * 表达式位置仍然不该出现在需要长期持有结果的场景 —— 声明位置的
 * await_into 才是那条安全路径。
 */
void* kaula_handle_await(KaulaHandle h) {
    if (h.task == NULL) return NULL;
    kaula_runtime_ensure_init();
    if (!kaula_task_await(h.task, KAULA_P_WAIT_FOREVER)) return NULL;
    return h.task->result;
}

void* kaula_handle_try_await(KaulaHandle h, uint64_t timeout_ns) {
    if (h.task == NULL) return NULL;
    if (!kaula_task_await(h.task, timeout_ns)) return NULL;
    return h.task->result;
}

int kaula_handle_poll(KaulaHandle h) {
    if (h.task == NULL) return 0;
    return atomic_load_explicit(&h.task->state, memory_order_acquire) == KAULA_TASK_DONE;
}

void kaula_handle_cancel(KaulaHandle h) {
    if (h.task == NULL) return;
    kaula_task_cancel(h.task);
}