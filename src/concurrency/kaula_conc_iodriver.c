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

    if (first && !kaula_iodriver_backend_add(drv, fd, want)) {
        /* 系统注册失败：撤掉刚加的节点 */
        drv->pending[b] = node->next;
        atomic_fetch_sub_explicit(&drv->pending_count, 1, memory_order_relaxed);
        free(node);
        return KAULA_ERR_OOM;
    }
    return 0;
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
            if (req.type == KAULA_IO_SLEEP) {
                io_timer_add(drv, task, req.timeout_ns);
            } else {
                uint32_t want = (req.type == KAULA_IO_READ)  ? KAULA_IO_EV_READ
                             : (req.type == KAULA_IO_WRITE) ? KAULA_IO_EV_WRITE
                             : KAULA_IO_EV_READ;
                rc = io_register(drv, task, req.fd, want);
            }

            if (rc != 0) {
                /* 挂起失败：立刻放回调度器，绝不把 Task 丢在半路 */
                task->io_result = -ENOMEM;
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

        int nready = kaula_iodriver_backend_wait(drv, wait_ns);

        if (nready < 0) {
            if (errno == EINTR) continue;
            break;
        }

        /* 步骤 3：处理就绪事件 */
        if (nready > 0) {
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

    /* 清空挂起表与定时器：把残留 Task 标失败，避免 await 死锁 */
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