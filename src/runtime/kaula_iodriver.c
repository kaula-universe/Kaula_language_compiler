/*
 * Kaula 并发内核 · IoDriver 实现（epoll 后端）
 */

#include "kaula_iodriver.h"
#include "kaula_worker.h"
#include "kaula_scheduler.h"
#include "../kmm_scoped_allocator_v4.h"

#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <fcntl.h>
#include <sys/epoll.h>
#include <sys/eventfd.h>
#include <errno.h>

/* ========== 内部辅助 ========== */

/* 设置 fd 为非阻塞 */
static int set_nonblocking(int fd) {
    int flags = fcntl(fd, F_GETFL, 0);
    if (flags < 0) return -1;
    return fcntl(fd, F_SETFL, flags | O_NONBLOCK);
}

/* ========== IoDriver 线程入口 ========== */

static void* kaula_iodriver_thread_entry(void* arg) {
    KaulaIoDriver* drv = (KaulaIoDriver*)arg;

    struct epoll_event events[KAULA_IODRIVER_BATCH_SIZE];

    while (atomic_load_explicit(&drv->running, memory_order_acquire)) {
        /* 步骤 1：处理提交队列中的新 IO 请求 */
        KaulaIoSubmitEntry* entry;
        while ((entry = (KaulaIoSubmitEntry*)kaula_mpsc_queue_pop(&drv->submit_queue)) != NULL) {
            KaulaTask* task = entry->task;
            KaulaIoRequest* req = &entry->request;

            /* 设置 Task 状态为 SUSPENDED */
            atomic_store_explicit(&task->state, KAULA_TASK_SUSPENDED, memory_order_release);
            task->io_request = *req;

            /* 根据 IO 类型处理 */
            if (req->type == KAULA_IO_SLEEP) {
                /* 定时器：暂时简化处理，立即完成 */
                /* TODO: 实现真正的定时器 */
                task->io_result = 0;
                atomic_store_explicit(&task->state, KAULA_TASK_READY, memory_order_release);

                /* 推回原 Worker 的队列 */
                KaulaWorker* owner = atomic_load_explicit(&task->owner, memory_order_acquire);
                if (owner != NULL) {
                    kaula_ws_deque_push_tail(&owner->local_queue, task);
                }

                drv->total_completions++;
            } else if (req->type == KAULA_IO_READ || req->type == KAULA_IO_WRITE) {
                /* 文件描述符 IO：注册到 epoll */
                int fd = req->fd;

                /* 设置非阻塞 */
                set_nonblocking(fd);

                /* 注册到 epoll */
                struct epoll_event ev = {0};
                ev.events = (req->type == KAULA_IO_READ) ? EPOLLIN : EPOLLOUT;
                ev.events |= EPOLLET; /* 边缘触发 */
                ev.data.ptr = task;

                if (epoll_ctl(drv->epoll_fd, EPOLL_CTL_ADD, fd, &ev) < 0) {
                    /* 注册失败：可能是已注册或 fd 无效 */
                    if (errno == EEXIST) {
                        /* 已注册：修改事件 */
                        epoll_ctl(drv->epoll_fd, EPOLL_CTL_MOD, fd, &ev);
                    } else {
                        /* 其他错误：立即完成，返回错误 */
                        task->io_result = -errno;
                        atomic_store_explicit(&task->state, KAULA_TASK_READY, memory_order_release);

                        KaulaWorker* owner = atomic_load_explicit(&task->owner, memory_order_acquire);
                        if (owner != NULL) {
                            kaula_ws_deque_push_tail(&owner->local_queue, task);
                        }

                        drv->total_completions++;
                    }
                }
            }

            drv->total_submits++;
        }

        /* 步骤 2：epoll_wait（超时 1ms，兼顾新请求响应） */
        int nfds = epoll_wait(drv->epoll_fd, events, KAULA_IODRIVER_BATCH_SIZE, 1);
        if (nfds < 0) {
            if (errno == EINTR) continue;
            break;
        }

        /* 步骤 3：处理就绪事件 */
        for (int i = 0; i < nfds; i++) {
            struct epoll_event* ev = &events[i];

            /* 唤醒事件 */
            if (ev->data.fd == drv->wakeup_fd) {
                uint64_t val;
                read(drv->wakeup_fd, &val, sizeof(val));
                continue;
            }

            /* IO 事件 */
            KaulaTask* task = (KaulaTask*)ev->data.ptr;
            if (task == NULL) continue;

            /* 从 epoll 移除 */
            epoll_ctl(drv->epoll_fd, EPOLL_CTL_DEL, task->io_request.fd, NULL);

            /* 执行 IO 操作 */
            KaulaIoRequest* req = &task->io_request;
            int result = 0;

            if (req->type == KAULA_IO_READ) {
                result = read(req->fd, req->buf, req->buf_len);
                if (result < 0 && errno == EAGAIN) {
                    /* 非阻塞读无数据：重新注册 */
                    struct epoll_event new_ev = {0};
                    new_ev.events = EPOLLIN | EPOLLET;
                    new_ev.data.ptr = task;
                    epoll_ctl(drv->epoll_fd, EPOLL_CTL_ADD, req->fd, &new_ev);
                    continue;
                }
            } else if (req->type == KAULA_IO_WRITE) {
                result = write(req->fd, req->buf, req->buf_len);
                if (result < 0 && errno == EAGAIN) {
                    /* 非阻塞写缓冲区满：重新注册 */
                    struct epoll_event new_ev = {0};
                    new_ev.events = EPOLLOUT | EPOLLET;
                    new_ev.data.ptr = task;
                    epoll_ctl(drv->epoll_fd, EPOLL_CTL_ADD, req->fd, &new_ev);
                    continue;
                }
            }

            /* IO 完成 */
            task->io_result = result;
            atomic_store_explicit(&task->state, KAULA_TASK_READY, memory_order_release);

            /* 推回原 Worker 的队列 */
            KaulaWorker* owner = atomic_load_explicit(&task->owner, memory_order_acquire);
            if (owner != NULL) {
                kaula_ws_deque_push_tail(&owner->local_queue, task);
            }

            drv->total_completions++;
        }
    }

    return NULL;
}

/* ========== API 实现 ========== */

int kaula_iodriver_init(KaulaIoDriver* drv) {
    memset(drv, 0, sizeof(KaulaIoDriver));

    /* 初始化提交队列 */
    kaula_mpsc_queue_init(&drv->submit_queue);

    /* 创建 epoll fd */
    drv->epoll_fd = epoll_create1(0);
    if (drv->epoll_fd < 0) {
        return -1;
    }

    /* 创建 eventfd 用于唤醒 */
    drv->wakeup_fd = eventfd(0, EFD_NONBLOCK);
    if (drv->wakeup_fd < 0) {
        close(drv->epoll_fd);
        return -1;
    }

    /* 注册 wakeup_fd 到 epoll */
    struct epoll_event ev = {0};
    ev.events = EPOLLIN;
    ev.data.fd = drv->wakeup_fd;
    if (epoll_ctl(drv->epoll_fd, EPOLL_CTL_ADD, drv->wakeup_fd, &ev) < 0) {
        close(drv->wakeup_fd);
        close(drv->epoll_fd);
        return -1;
    }

    atomic_store_explicit(&drv->running, 0, memory_order_release);

    return 0;
}

int kaula_iodriver_start(KaulaIoDriver* drv) {
    atomic_store_explicit(&drv->running, 1, memory_order_release);

    int err = pthread_create(&drv->thread, NULL, kaula_iodriver_thread_entry, drv);
    if (err != 0) {
        atomic_store_explicit(&drv->running, 0, memory_order_release);
        return err;
    }

    return 0;
}

void kaula_iodriver_stop(KaulaIoDriver* drv) {
    atomic_store_explicit(&drv->running, 0, memory_order_release);

    /* 唤醒 IoDriver 线程 */
    uint64_t val = 1;
    write(drv->wakeup_fd, &val, sizeof(val));
}

void kaula_iodriver_join(KaulaIoDriver* drv) {
    pthread_join(drv->thread, NULL);
}

void kaula_iodriver_destroy(KaulaIoDriver* drv) {
    close(drv->wakeup_fd);
    close(drv->epoll_fd);
}

void kaula_iodriver_submit(KaulaIoDriver* drv, KaulaTask* task, KaulaIoRequest request) {
    /* 分配提交条目 */
    KaulaIoSubmitEntry* entry = (KaulaIoSubmitEntry*)kmm_v4_alloc_global(sizeof(KaulaIoSubmitEntry));
    if (entry == NULL) {
        /* 分配失败：立即完成，返回错误 */
        task->io_result = -1;
        atomic_store_explicit(&task->state, KAULA_TASK_READY, memory_order_release);

        KaulaWorker* owner = atomic_load_explicit(&task->owner, memory_order_acquire);
        if (owner != NULL) {
            kaula_ws_deque_push_tail(&owner->local_queue, task);
        }
        return;
    }

    entry->task = task;
    entry->request = request;

    /* 推入提交队列 */
    if (!kaula_mpsc_queue_push(&drv->submit_queue, entry)) {
        /* 队列满：立即完成，返回错误 */
        task->io_result = -1;
        atomic_store_explicit(&task->state, KAULA_TASK_READY, memory_order_release);

        KaulaWorker* owner = atomic_load_explicit(&task->owner, memory_order_acquire);
        if (owner != NULL) {
            kaula_ws_deque_push_tail(&owner->local_queue, task);
        }

        kmm_v4_survivor_free(entry);
        return;
    }

    /* 唤醒 IoDriver 线程 */
    uint64_t val = 1;
    write(drv->wakeup_fd, &val, sizeof(val));
}
