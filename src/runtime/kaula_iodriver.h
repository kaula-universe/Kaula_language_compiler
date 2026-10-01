/*
 * Kaula 并发内核 · IoDriver
 *
 * 异步 IO 驱动，基于 epoll（Linux）/ kqueue（macOS）
 * 负责处理 Task 的 IO 请求，完成后唤醒 Task
 */

#ifndef KAULA_IODRIVER_H
#define KAULA_IODRIVER_H

#include <stdint.h>
#include <stdatomic.h>
#include <pthread.h>

#include "kaula_task.h"
#include "kaula_mpsc_queue.h"

/* IoDriver 最大挂起请求数 */
#define KAULA_IODRIVER_MAX_PENDING 65536

/* epoll 批量处理大小 */
#define KAULA_IODRIVER_BATCH_SIZE 256

/* 挂起的 IO 请求（Worker 提交给 IoDriver） */
typedef struct {
    KaulaTask*      task;           /* 发起请求的 Task */
    KaulaIoRequest  request;        /* IO 请求详情 */
} KaulaIoSubmitEntry;

/*
 * IoDriver 结构体
 *
 * 独立线程，负责：
 * 1. 从提交队列获取 IO 请求
 * 2. 注册到 epoll/kqueue
 * 3. 等待 IO 事件
 * 4. IO 完成后唤醒 Task
 */
typedef struct {
    /* ---- 线程 ---- */
    pthread_t           thread;

    /* ---- 提交队列（Worker → IoDriver） ---- */
    KaulaMpscQueue      submit_queue;

    /* ---- epoll fd ---- */
    int                 epoll_fd;

    /* ---- 唤醒 fd（eventfd） ---- */
    int                 wakeup_fd;

    /* ---- 生命周期 ---- */
    _Atomic(int)        running;

    /* ---- 统计 ---- */
    uint64_t            total_submits;
    uint64_t            total_completions;
} KaulaIoDriver;

/* ========== API ========== */

/* 初始化 IoDriver */
extern int kaula_iodriver_init(KaulaIoDriver* drv);

/* 启动 IoDriver 线程 */
extern int kaula_iodriver_start(KaulaIoDriver* drv);

/* 停止 IoDriver */
extern void kaula_iodriver_stop(KaulaIoDriver* drv);

/* 等待 IoDriver 线程退出 */
extern void kaula_iodriver_join(KaulaIoDriver* drv);

/* 销毁 IoDriver */
extern void kaula_iodriver_destroy(KaulaIoDriver* drv);

/* 提交 IO 请求（Worker 调用） */
extern void kaula_iodriver_submit(KaulaIoDriver* drv, KaulaTask* task, KaulaIoRequest request);

#endif /* KAULA_IODRIVER_H */
