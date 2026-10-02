/*
 * Kaula 并发内核 · IoDriver 实现
 */

#include "kaula_conc_iodriver.h"
#include "kaula_conc_io_backend.h"
#include "kaula_conc_platform.h"
#include "kaula_conc_scheduler.h"
#include "kaula_conc_worker.h"

#include <stdlib.h>
#include <string.h>
#include <errno.h>

#if !KAULA_PLATFORM_WINDOWS
    #include <unistd.h>
    #include <fcntl.h>
#endif

KaulaIoDriver g_conc_iodriver;

/* 提交队列元素：Task 指针 + 请求副本 */
typedef struct {
    KaulaTask*     task;
    KaulaIoRequest request;
} KaulaIoSubmitEntry;

/* ========== 挂起表 ========== */

static inline uint32_t io_bucket(int fd) {
    return (uint32_t)((uint32_t)fd) & (KAULA_IO_BUCKETS - 1);
}

/*
 * 登记挂起。
 *
 * 关键修复：原实现只支持"一个 fd 对应一个 Task"，第二个 Task 提交
 * 同 fd 会覆盖注册。这里改为 fd → 链表，支持同 fd 多个等待者，
 * 全部注册进 epoll（同 fd 只需注册一次，事件到来时唤醒整条链）。
 */
static int io_register(KaulaIoDriver* drv, KaulaTask* task,
                       int fd, uint32_t want) {
    if (drv->pending_count >= KAULA_IODRIVER_MAX_PENDING) {
        return KAULA_ERR_WOULD_BLOCK;
    }

#if KAULA_P_IO_IOCP
    /*
     * 完成式后端不走挂起表。
     *
     * 挂起表是 fd → Task 的映射，为"一个 fd 就绪、唤醒它上面所有等待者"
     * 这件事服务。IOCP 下每个等待者对应一次独立的投递，完成包直接带着
     * Task 回来（next_completion），压根不需要按 fd 反查。
     *
     * 硬塞进挂起表只会带来两个 bug：完成时 io_take_fd 按 fd 一次唤醒
     * 整条链，把还没投递的兄弟等待者也叫醒；以及 del 时无法区分
     * "这个 fd 没人等了"和"这个等待者被单独取消了"。
     */
    if (!kaula_iodriver_backend_add(drv, task, want)) {
        /* 投递失败。后端可能已把 errno 写进 task->io_result（如管道已关），
         * 没有的话给一个明确的失败码 —— 不能留 0，那会被读成"读到 0 字节"。 */
        if (task->io_result == 0) task->io_result = -EIO;
        return KAULA_ERR_IO;
    }
    atomic_fetch_add_explicit(&drv->pending_count, 1, memory_order_relaxed);
    return 0;
#else
    /* 首次为该 fd 注册系统事件 */
    uint32_t b = io_bucket(fd);
    int first = 1;
    for (KaulaIoPending* p = drv->pending[b]; p != NULL; p = p->next) {
        if (p->fd == fd) { first = 0; break; }
    }

    KaulaIoPending* node = (KaulaIoPending*)malloc(sizeof(KaulaIoPending));
    if (node == NULL) return KAULA_ERR_OOM;

    node->fd = fd;
    node->task = task;
    node->want = want;
    node->got = 0;
    node->next = drv->pending[b];
    drv->pending[b] = node;
    atomic_fetch_add_explicit(&drv->pending_count, 1, memory_order_relaxed);

    if (first && !kaula_iodriver_backend_add(drv, task, want)) {
        /* 系统注册失败：撤掉刚加的节点 */
        drv->pending[b] = node->next;
        atomic_fetch_sub_explicit(&drv->pending_count, 1, memory_order_relaxed);
        free(node);
        return KAULA_ERR_OOM;
    }
    return 0;
#endif
}

/* 桶内是否还有该 fd 的其他等待者 */
static int io_bucket_has_fd(KaulaIoDriver* drv, int fd) {
    uint32_t b = io_bucket(fd);
    for (KaulaIoPending* p = drv->pending[b]; p != NULL; p = p->next) {
        if (p->fd == fd) return 1;
    }
    return 0;
}

/* 摘除某个 Task 的挂起记录；返回 1 = 找到并摘除 */
static int io_unregister_task(KaulaIoDriver* drv, KaulaTask* task,
                              uint32_t* got_out) {
    uint32_t b = io_bucket(task->io_request.fd);
    KaulaIoPending** link = &drv->pending[b];

    while (*link != NULL) {
        KaulaIoPending* p = *link;
        if (p->task == task) {
            *link = p->next;
            if (got_out != NULL) *got_out = p->got;
            atomic_fetch_sub_explicit(&drv->pending_count, 1, memory_order_relaxed);

            /* 该 fd 链表空了就从系统摘掉，省 fd 资源 */
            if (!io_bucket_has_fd(drv, task->io_request.fd)) {
                kaula_iodriver_backend_del(drv, task->io_request.fd);
            }
            free(p);
            return 1;
        }
        link = &p->next;
    }
    return 0;
}

/* 取出某 fd 的全部等待者 */
static void io_take_fd(KaulaIoDriver* drv, int fd, uint32_t events,
                       KaulaTask** out, int max_out, int* n_out) {
    uint32_t b = io_bucket(fd);
    KaulaIoPending** link = &drv->pending[b];
    int n = 0;

    while (*link != NULL) {
        KaulaIoPending* p = *link;
        if (p->fd != fd) { link = &p->next; continue; }

        if (p->want & events) {
            /* 命中：整链摘下 */
            *link = p->next;
            atomic_fetch_sub_explicit(&drv->pending_count, 1, memory_order_relaxed);
            p->got = events;
            if (n < max_out) out[n++] = p->task;
            else free(p);   /* 超出本批容量：直接丢弃唤醒（不应发生） */
        } else {
            link = &p->next;
        }
    }
    *n_out = n;

    if (!io_bucket_has_fd(drv, fd)) {
        kaula_iodriver_backend_del(drv, fd);
    }
}

/* ========== 定时器 ========== */

static void io_timer_add(KaulaIoDriver* drv, KaulaTask* task, uint64_t delay_ns) {
    KaulaTimer* t = (KaulaTimer*)malloc(sizeof(KaulaTimer));
    if (t == NULL) return;

    uint64_t deadline = kaula_p_now_ns() + delay_ns;
    t->deadline_ns = deadline;
    t->task = task;
    t->next = NULL;

    /* 按 deadline 递增插入（链表足够短，通常只有几个） */
    KaulaTimer** link = &drv->timers;
    while (*link != NULL && (*link)->deadline_ns <= deadline) {
        link = &(*link)->next;
    }
    t->next = *link;
    *link = t;
}

/* 处理到期定时器；返回距离下一个到期的纳秒数（无定时器时返回 KAULA_P_WAIT_FOREVER） */
static uint64_t io_timers_fire(KaulaIoDriver* drv) {
    uint64_t now = kaula_p_now_ns();
    uint64_t next_delta = KAULA_P_WAIT_FOREVER;

    while (drv->timers != NULL && drv->timers->deadline_ns <= now) {
        KaulaTimer* t = drv->timers;
        drv->timers = t->next;

        KaulaTask* task = t->task;
        free(t);

        atomic_fetch_add_explicit(&drv->total_timer_fires, 1, memory_order_relaxed);

        /* 写回 TLS 供 resume_fn 读取，然后交回调度器 */
        task->io_result = 0;
        kaula_scheduler_resume_task(task);
    }

    if (drv->timers != NULL) {
        uint64_t d = drv->timers->deadline_ns - now;
        next_delta = d;
    }
    return next_delta;
}

/* ========== 主循环 ========== */

static void iodriver_loop(void* arg) {
    KaulaIoDriver* drv = (KaulaIoDriver*)arg;

    while (atomic_load_explicit(&drv->running, memory_order_acquire)) {
        /* 步骤 1：处理新提交 */
        void* item;
        while ((item = kaula_mpsc_queue_pop(&drv->submit_queue)) != NULL) {
            KaulaIoSubmitEntry* e = (KaulaIoSubmitEntry*)item;
            KaulaTask* task = e->task;
            KaulaIoRequest req = e->request;

            atomic_store_explicit(&task->state, KAULA_TASK_SUSPENDED,
                                  memory_order_release);
            task->io_request = req;

            int rc = 0;
            int done = 0;
            uint32_t want = 0;

            if (req.type == KAULA_IO_SLEEP) {
                io_timer_add(drv, task, req.timeout_ns);
                done = 1;
            } else if (req.type == KAULA_IO_READ) {
                want = KAULA_IO_EV_READ;
            } else if (req.type == KAULA_IO_WRITE) {
                want = KAULA_IO_EV_WRITE;
            } else {
                /*
                 * 未知类型（CONNECT / ACCEPT / 未来的保留码）。
                 *
                 * 绝不能"默认按可读注册"：那会让一个无法执行的操作
                 * 挂在某个 fd 上，等就绪后由 do_io 拿 ENOSYS 交差 ——
                 * 症状是 await 拿到一个说不清来源的失败值，而不是
                 * 这里就该给出的明确 ENOSYS。
                 */
                task->io_result = -ENOSYS;
                kaula_scheduler_resume_task(task);
                done = 1;
            }

            if (!done) rc = io_register(drv, task, req.fd, want);

            if (rc != 0) {
                /*
                 * 挂起失败：立刻放回调度器，绝不把 Task 丢在半路。
                 *
                 * io_register 在投递失败时可能已经把 errno 写进
                 * io_result（后端知道得更具体）；只有它没写时才兜底。
                 * 无条件覆盖会把"管道已关闭"这种明确原因抹成 ENOMEM。
                 */
                if (task->io_result == 0) task->io_result = -EIO;
                kaula_scheduler_resume_task(task);
            }

            atomic_fetch_add_explicit(&drv->total_submits, 1, memory_order_relaxed);
            free(e);   /* 原实现在此泄漏 */
        }

        /* 步骤 2：等待 IO 事件，超时取"下一个定时器"的剩余时间 */
        uint64_t timer_delta = io_timers_fire(drv);
        uint64_t wait_ns = KAULA_IO_BATCH_WAIT_NS;
        if (timer_delta != KAULA_P_WAIT_FOREVER && timer_delta < wait_ns) {
            wait_ns = timer_delta;
        }

        kaula_iodriver_backend_begin(drv);

        int nready = kaula_iodriver_backend_wait(drv, wait_ns);

        if (nready < 0) {
            if (errno == EINTR) continue;
            break;
        }

        /*
         * 步骤 3：取事件。
         *
         * 两种事件模型在这里分岔，不是风格选择而是正确性要求：
         *
         *   完成式（IOCP）：完成包带来的是"已经做完"的事实与字节数。
         *       必须直接取结果交回 Task，绝不能再调 do_io ——
         *       那会把同一个 fd 再读一遍，拿到"没有更多数据"，
         *       于是第二次读的那批数据被静默丢掉。
         *
         *   就绪式（epoll / kqueue）：内核只说"可读了"，
         *       IO 还没做，由这里调 do_io 执行。
         *
         * next_completion 在就绪式后端恒返回 0，next_event 在完成式
         * 后端恒返回 0，所以两条分支各自空转，不需要额外的平台宏。
         */
        if (nready > 0) {
            /* 完成式：结果已就绪，直接交回 */
            KaulaTask* ctask;
            int cresult;
            while (kaula_iodriver_backend_next_completion(drv, &ctask, &cresult)) {
                atomic_fetch_sub_explicit(&drv->pending_count, 1, memory_order_relaxed);
                ctask->io_result = cresult;
                atomic_fetch_add_explicit(&drv->total_completions, 1,
                                          memory_order_relaxed);
                kaula_scheduler_resume_task(ctask);
            }

            /* 就绪式：唤醒等待者，由 do_io 执行 IO */
            KaulaTask* woken[KAULA_IO_BATCH_SIZE];
            for (int i = 0; i < nready; i++) {
                int fd;
                uint32_t events;
                if (!kaula_iodriver_backend_next_event(drv, &fd, &events)) break;
                if (fd == KAULA_P_IO_WAKE_FD) {
                    kaula_p_io_drain_wake(drv->wait);
                    continue;
                }

                int n = 0;
                io_take_fd(drv, fd, events, woken, KAULA_IO_BATCH_SIZE, &n);

                for (int j = 0; j < n; j++) {
                    KaulaTask* task = woken[j];
                    int r = kaula_iodriver_do_io(&task->io_request);
                    task->io_result = r;
                    atomic_fetch_add_explicit(&drv->total_completions, 1,
                                              memory_order_relaxed);
                    /* 交回原 owner 的队列（亲和性），失败则走注入队列 */
                    kaula_scheduler_resume_task(task);
                }
            }
        }

        /* 步骤 4：再扫一次定时器（IO 等待期间可能刚好到期） */
        io_timers_fire(drv);
    }
}

/* ========== 生命周期 ========== */

int kaula_iodriver_init(void) {
    KaulaIoDriver* drv = &g_conc_iodriver;
    memset(drv, 0, sizeof(*drv));

    kaula_mpsc_queue_init(&drv->submit_queue);

    if (kaula_p_io_wait_create(&drv->wait) != 0) {
        return -1;
    }

    atomic_store_explicit(&drv->running, 0, memory_order_release);
    return 0;
}

int kaula_iodriver_start(void) {
    KaulaIoDriver* drv = &g_conc_iodriver;
    atomic_store_explicit(&drv->running, 1, memory_order_release);
    if (kaula_p_thread_create(&drv->thread, iodriver_loop, drv) != 0) {
        atomic_store_explicit(&drv->running, 0, memory_order_release);
        return -1;
    }
    return 0;
}

void kaula_iodriver_stop(void) {
    KaulaIoDriver* drv = &g_conc_iodriver;
    atomic_store_explicit(&drv->running, 0, memory_order_release);
    /* 唤醒让它退出等待 */
    kaula_p_io_wake(drv->wait);
}

void kaula_iodriver_join(void) {
    kaula_p_thread_join(g_conc_iodriver.thread);
}

void kaula_iodriver_destroy(void) {
    KaulaIoDriver* drv = &g_conc_iodriver;

    /*
     * 清空挂起表与定时器：把残留 Task 标失败，避免 await 死锁。
     *
     * 完成式后端（IOCP）没有挂起表，在途操作由后端自己持有。
     * 端口即将被关闭，那些操作会以 ERROR_ABORTED 完成并排进端口 ——
     * 但没人再去收包了。所以这里必须先让后端把在途记录清掉，
     * 否则每个未完成的 overlapped 操作泄漏一条记录。
     */
    kaula_iodriver_backend_abort_all(drv);

    for (uint32_t b = 0; b < KAULA_IO_BUCKETS; b++) {
        KaulaIoPending* p = drv->pending[b];
        while (p != NULL) {
            KaulaIoPending* next = p->next;
            if (p->task != NULL) {
                p->task->io_result = -ECANCELED;
                atomic_store_explicit(&p->task->state, KAULA_TASK_READY,
                                      memory_order_release);
            }
            free(p);
            p = next;
        }
        drv->pending[b] = NULL;
    }
    atomic_store(&drv->pending_count, 0);

    while (drv->timers != NULL) {
        KaulaTimer* t = drv->timers;
        drv->timers = t->next;
        if (t->task != NULL) {
            t->task->io_result = -ECANCELED;
            atomic_store_explicit(&t->task->state, KAULA_TASK_READY,
                                  memory_order_release);
        }
        free(t);
    }

    kaula_p_io_wait_destroy(drv->wait);
    drv->wait = 0;
}

/* ========== 提交 ========== */

int kaula_iodriver_submit(KaulaTask* task) {
    KaulaIoDriver* drv = &g_conc_iodriver;

    /* I3：挂起数硬上限 */
    uint32_t pending = atomic_load_explicit(&drv->pending_count, memory_order_relaxed);
    if (pending >= KAULA_IODRIVER_MAX_PENDING) {
        atomic_fetch_add_explicit(&drv->total_would_block, 1, memory_order_relaxed);
        return KAULA_ERR_WOULD_BLOCK;
    }

    /* 条目走 malloc：生命周期与提交严格配对，避免泄漏 */
    KaulaIoSubmitEntry* e = (KaulaIoSubmitEntry*)malloc(sizeof(KaulaIoSubmitEntry));
    if (e == NULL) {
        return KAULA_ERR_OOM;
    }
    e->task = task;
    e->request = task->io_request;

    if (!kaula_mpsc_queue_push(&drv->submit_queue, e)) {
        free(e);
        atomic_fetch_add_explicit(&drv->total_would_block, 1, memory_order_relaxed);
        return KAULA_ERR_WOULD_BLOCK;
    }

    kaula_p_io_wake(drv->wait);
    return 0;
}

/* ========== 顺序路径的阻塞 IO（文档 §9.4） ========== */

int kaula_iodriver_blocking_wait(const KaulaIoRequest* req) {
    if (req->type == KAULA_IO_SLEEP) {
        kaula_p_sleep_ns(req->timeout_ns);
        return 0;
    }
    return kaula_iodriver_do_io(req);
}

/* ========== 诊断 ========== */

uint32_t kaula_iodriver_pending_count(void) {
    return atomic_load(&g_conc_iodriver.pending_count);
}

void kaula_iodriver_fire_timer(KaulaTask* task, uint64_t delay_ns) {
    KaulaIoDriver* drv = &g_conc_iodriver;
    task->io_request.type = KAULA_IO_SLEEP;
    task->io_request.timeout_ns = delay_ns;
    atomic_store_explicit(&task->state, KAULA_TASK_SUSPENDED, memory_order_release);

    io_timer_add(drv, task, delay_ns);
    /* 驱动线程可能正睡在长等待上，唤它来算下一次到期 */
    kaula_p_io_wake(drv->wait);
}