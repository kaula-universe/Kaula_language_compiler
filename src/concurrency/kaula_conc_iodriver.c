/*
 * Kaula 并发内核（CONC）· IoDriver 实现
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
__attribute__((unused))
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

/* ========== 定时器对象池（P2.2：suspend/mixed 落后 Go 的主因之一） ==========
 *
 * io_timer_add 每 200us 级 sleep 都 malloc/free 一个 KaulaTimer，高并发
 * sleep（conc=10000、total=20000）下 20000 次系统堆分配/释放 + 堆锁争用
 * 压在 iodriver 单线程上串行。改为无锁原子栈池化：fire 归还、add 复用。
 * 池上限防空闲膨胀（并发 sleep 稳态下池大小 ≈ 峰值在途定时器数）。
 *
 * 池只在 iodriver 线程被访问（io_timer_add 仅 iodriver_loop 调用；
 * kaula_iodriver_fire_timer 是当前无调用点的导出诊断 API，若未来被外部
 * 线程使用，必须先走 submit_queue，不能让外部线程直接碰池）。
 */
#define KAULA_TIMER_POOL_MAX 4096
static _Atomic(KaulaTimer*) g_timer_pool_head;
static _Atomic(int)         g_timer_pool_count;

static KaulaTimer* timer_pool_pop(void) {
    KaulaTimer* h = atomic_load_explicit(&g_timer_pool_head, memory_order_relaxed);
    while (h != NULL) {
        KaulaTimer* n = h->next;
        if (atomic_compare_exchange_weak_explicit(&g_timer_pool_head, &h, n,
                                                  memory_order_acquire,
                                                  memory_order_relaxed)) {
            atomic_fetch_sub_explicit(&g_timer_pool_count, 1, memory_order_relaxed);
            return h;
        }
        /* CAS 失败：h 已被更新为实际 head，继续 */
    }
    return NULL;
}

static void timer_pool_push(KaulaTimer* t) {
    if (atomic_load_explicit(&g_timer_pool_count, memory_order_relaxed) >=
        KAULA_TIMER_POOL_MAX) {
        free(t);
        return;
    }
    KaulaTimer* h = atomic_load_explicit(&g_timer_pool_head, memory_order_relaxed);
    for (;;) {
        t->next = h;
        if (atomic_compare_exchange_weak_explicit(&g_timer_pool_head, &h, t,
                                                  memory_order_release,
                                                  memory_order_relaxed)) {
            atomic_fetch_add_explicit(&g_timer_pool_count, 1, memory_order_relaxed);
            return;
        }
    }
}

static void io_timer_add(KaulaIoDriver* drv, KaulaTask* task, uint64_t delay_ns) {
    KaulaTimer* t = timer_pool_pop();
    if (t == NULL) {
        t = (KaulaTimer*)malloc(sizeof(KaulaTimer));
        if (t == NULL) {
            /* 定时器分配失败：不能静默丢弃——否则挂起等待定时器的任务永远不会
             * 被唤醒（await 永久挂起）。立即把任务标错并恢复到 READY。 */
            task->io_result = -ENOMEM;
            kaula_scheduler_resume_task(task);
            return;
        }
    }

    uint64_t deadline = kaula_p_now_ns() + delay_ns;
    t->deadline_ns = deadline;
    t->task = task;
    t->next = drv->timers;

    /*
     * 头插 O(1)，链表无序。
     *
     * 原实现按 deadline 递增有序插入 —— 全量并发压测暴露：1 万个同
     * deadline 的 sleep 挂起时，每个插入都要扫到链表尾部，O(n²) 把
     * 驱动线程卡在插入上（[D] mixed 场景 spawn 阶段 3.8s 的根因）。
     *
     * 无序链表 + 最小 deadline 短路（见 io_timers_fire）：
     *   插入 O(1)；绝大多数 wake 时 min > now，fire 直接短路 O(1)；
     *   到期那一刻扫描一次 O(n) 批量 fire。并发 sleep 场景总成本 O(n)。
     * 分散 deadline 会退化为多次 O(n) 扫描 —— sleep 语义天然集中
     * （同一批挂起的时间窗相近），可接受；极端分散时 IO 数量本就
     * 被 MAX_PENDING 限住，单次扫描有界。
     */
    drv->timers = t;
    if (t->deadline_ns < drv->timers_min_deadline) {
        drv->timers_min_deadline = t->deadline_ns;
    }
}

/*
 * P2.1：批量注入 flush —— 把收集到的任务批量推入注入队列。
 * 队列空间不足时 sched_yield 重试到全部入队（Worker 持续排空，不丢任务）。
 */
static void kaula_inject_batch_flush(KaulaTask** tasks, int n) {
    int off = 0;
    while (off < n) {
        int accepted = kaula_inject_queue_push_batch(tasks + off, n - off);
        if (accepted <= 0) {
            sched_yield();
            continue;
        }
        off += accepted;
    }
}

/* 处理到期定时器；返回距离下一个到期的纳秒数（无定时器时返回 KAULA_P_WAIT_FOREVER） */
static uint64_t io_timers_fire(KaulaIoDriver* drv) {
    if (drv->timers == NULL) {
        drv->timers_min_deadline = KAULA_P_WAIT_FOREVER;
        return KAULA_P_WAIT_FOREVER;
    }

    uint64_t now = kaula_p_now_ns();

    /* 短路：最近到期时间还没到，本次 wake 无需扫描（O(1)） */
    if (drv->timers_min_deadline > now) {
        return drv->timers_min_deadline - now;
    }

    /*
     * 至少一个到期：整链扫描一次，把到期者全部 fire，
     * 未到期者保留并重建链表，同时重算最小 deadline。
     *
     * P2.2：到期任务**批量收集**，扫描结束后一次批量注入 + 一次唤醒。
     * 原实现逐个 kaula_scheduler_resume_task：外部线程（iodriver）不是
     * owner，Chase-Lev 约束下每个任务都要逐个注入全局队列 + 逐个
     * notify（pthread_mutex + cond_signal），conc=10000 时 20000 个
     * 到期任务 = 20000 次锁竞争与唤醒，是 suspend/mixed 落后 Go 的
     * 直接原因。此改动与 IO 完成路径（P2.1 done_batch 批量注入）对齐。
     */
    KaulaTimer* keep = NULL;
    KaulaTimer* keep_tail = NULL;
    uint64_t new_min = KAULA_P_WAIT_FOREVER;

    KaulaTask* done_batch[KAULA_IO_BATCH_SIZE * 2];
    int done_n = 0;

    KaulaTimer* cur = drv->timers;
    while (cur != NULL) {
        KaulaTimer* next = cur->next;
        if (cur->deadline_ns <= now) {
            KaulaTask* task = cur->task;
            timer_pool_push(cur);       /* P2.2：归还定时器池，替代 free */
            atomic_fetch_add_explicit(&drv->total_timer_fires, 1,
                                      memory_order_relaxed);
            /* 写回 TLS 供 resume_fn 读取，然后交回调度器 */
            task->io_result = 0;
            if (done_n < (int)(sizeof(done_batch) / sizeof(done_batch[0]))) {
                done_batch[done_n++] = task;
            } else {
                /* 批次满：先排空（sched_yield 重试，不丢任务），
                 * 并立即唤醒 Worker 并行消费——不能等整波扫描完再唤醒，
                 * 否则 fire 串行处理大波到期任务期间 Worker 全部闲置 */
                kaula_inject_batch_flush(done_batch, done_n);
                kaula_scheduler_notify_all();
                done_n = 0;
                done_batch[done_n++] = task;
            }
        } else {
            cur->next = NULL;
            if (keep == NULL) {
                keep = cur;
                keep_tail = cur;
            } else {
                keep_tail->next = cur;
                keep_tail = cur;
            }
            if (cur->deadline_ns < new_min) new_min = cur->deadline_ns;
        }
        cur = next;
    }

    drv->timers = keep;
    drv->timers_min_deadline = new_min;

    /* P2.2：批量注入剩余到期任务；有注入就唤醒所有 Worker 并行消化
     * （替代原逐任务 notify_one；任务量大，多 Worker 同时 pop 注入队列） */
    if (done_n > 0) {
        kaula_inject_batch_flush(done_batch, done_n);
        kaula_scheduler_notify_all();
    }

    if (keep != NULL) {
        uint64_t d = new_min - now;
        return d > 0 ? d : 0;
    }
    return KAULA_P_WAIT_FOREVER;
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
            /* P2.1：批量收集完成任务，一次批量注入 */
            KaulaTask* done_batch[KAULA_IO_BATCH_SIZE * 2];
            int done_n = 0;

            /* 完成式：结果已就绪，直接交回 */
            KaulaTask* ctask;
            int cresult;
            while (kaula_iodriver_backend_next_completion(drv, &ctask, &cresult)) {
                atomic_fetch_sub_explicit(&drv->pending_count, 1, memory_order_relaxed);
                ctask->io_result = cresult;
                atomic_fetch_add_explicit(&drv->total_completions, 1,
                                          memory_order_relaxed);
                atomic_store_explicit(&ctask->state, KAULA_TASK_READY,
                                      memory_order_release);
                if (done_n < KAULA_IO_BATCH_SIZE * 2) {
                    done_batch[done_n++] = ctask;
                } else {
                    /* 批次已满：先排空 */
                    kaula_inject_batch_flush(done_batch, done_n);
                    done_n = 0;
                }
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
                    atomic_store_explicit(&task->state, KAULA_TASK_READY,
                                          memory_order_release);
                    if (done_n < KAULA_IO_BATCH_SIZE * 2) {
                        done_batch[done_n++] = task;
                    } else {
                        kaula_inject_batch_flush(done_batch, done_n);
                        done_n = 0;
                    }
                }
            }

            /* 批量注入剩余任务 */
            if (done_n > 0) {
                kaula_inject_batch_flush(done_batch, done_n);
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

    /*
     * 必须先标成无效：memset 之后 wait 是 {0,0}，而 0 是**合法的 fd**
     * （stdin）。POSIX 上句柄是含两个 fd 的结构体，若 create 失败就直接
     * 走到 destroy，那两个 close(0) 会把 stdin 关掉。
     * 零值因此不是「无效」，得显式置 -1。
     */
    kaula_p_io_wait_invalidate(&drv->wait);
    drv->timers_min_deadline = KAULA_P_WAIT_FOREVER;

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
    /* POSIX 上句柄是含两个 fd 的结构体，不能 = 0（只能整体失效） */
    kaula_p_io_wait_invalidate(&drv->wait);
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