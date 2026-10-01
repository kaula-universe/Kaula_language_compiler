/*
 * Kaula 并发内核 · IO 复用后端实现
 *
 * 与 kaula_conc_io_backend.c 的接口一一对应。
 * 三种后端共享同一套事件位语义（KAULA_IO_EV_*）。
 */

#include "kaula_conc_io_backend.h"
#include "kaula_conc_iodriver.h"

#include <stdlib.h>
#include <string.h>
#include <errno.h>

/*
 * 注意：kaula_p_io_* 基础原语（建句柄 / 销毁 / 非阻塞 / 唤醒）
 * 已在 kaula_conc_io_primitives.c 实现，本文件只负责
 * "注册 / 注销 / 等待 / 取事件"这四步。
 */

/* 待处理事件批次（跨后端共用） */
typedef struct {
    int      fd;
    uint32_t events;
} IoReadyEvent;

#define IO_READY_BATCH KAULA_IO_BATCH_SIZE

/* 每驱动一批就绪事件 */
static IoReadyEvent g_ready[IO_READY_BATCH];
static int          g_ready_count = 0;
static int          g_ready_pos = 0;

/* ================================================================
 * Linux: epoll（边缘触发）
 * ================================================================ */
#if KAULA_P_IO_EPOLL

static uint32_t to_epoll(uint32_t want) {
    uint32_t e = 0;
    if (want & KAULA_IO_EV_READ)  e |= EPOLLIN;
    if (want & KAULA_IO_EV_WRITE) e |= EPOLLOUT;
    /* ET 是 P2「万并发空转 CPU ≈ 0」的关键：只在状态跃迁时唤醒 */
    e |= EPOLLET | EPOLLERR | EPOLLHUP;
    return e;
}

int kaula_iodriver_backend_add(KaulaIoDriver* drv, int fd, uint32_t want) {
    /* 非阻塞是 ET 的前提 */
    kaula_p_io_set_nonblocking(fd);

    struct epoll_event ev;
    memset(&ev, 0, sizeof(ev));
    ev.events = to_epoll(want);
    ev.data.fd = fd;      /* 只存 fd —— 修复缺陷 #6 */

    if (epoll_ctl(drv->wait, EPOLL_CTL_ADD, fd, &ev) == 0) return 1;
    if (errno == EEXIST) {
        /* 已注册：合并关注事件，而不是覆盖 Task 指针 */
        return epoll_ctl(drv->wait, EPOLL_CTL_MOD, fd, &ev) == 0;
    }
    return 0;
}

void kaula_iodriver_backend_del(KaulaIoDriver* drv, int fd) {
    epoll_ctl(drv->wait, EPOLL_CTL_DEL, fd, NULL);
}

int kaula_iodriver_backend_wait(KaulaIoDriver* drv, uint64_t timeout_ns) {
    struct epoll_event evs[IO_READY_BATCH];
    int ms = (timeout_ns == KAULA_P_WAIT_FOREVER)
           ? -1
           : (int)((timeout_ns + 999999) / 1000000);

    int n = epoll_wait(drv->wait, evs, IO_READY_BATCH, ms);
    if (n < 0) return -1;

    g_ready_count = 0;
    g_ready_pos = 0;
    for (int i = 0; i < n; i++) {
        uint32_t e = 0;
        if (evs[i].events & (EPOLLIN | EPOLLHUP | EPOLLERR)) e |= KAULA_IO_EV_READ;
        if (evs[i].events & (EPOLLOUT | EPOLLERR))          e |= KAULA_IO_EV_WRITE;
        g_ready[g_ready_count].fd = evs[i].data.fd;
        g_ready[g_ready_count].events = e;
        g_ready_count++;
    }
    return g_ready_count;
}

int kaula_iodriver_backend_next_event(KaulaIoDriver* drv, int* fd, uint32_t* events) {
    (void)drv;
    if (g_ready_pos >= g_ready_count) return 0;
    *fd = g_ready[g_ready_pos].fd;
    *events = g_ready[g_ready_pos].events;
    g_ready_pos++;
    return 1;
}

/* ================================================================
 * macOS: kqueue
 * ================================================================ */
#elif KAULA_P_IO_KQUEUE

int kaula_iodriver_backend_add(KaulaIoDriver* drv, int fd, uint32_t want) {
    kaula_p_io_set_nonblocking(fd);

    struct kevent changes[2];
    int n = 0;

    if (want & KAULA_IO_EV_READ) {
        EV_SET(&changes[n], fd, EVFILT_READ, EV_ADD | EV_ENABLE, 0, 0, NULL); n++;
    }
    if (want & KAULA_IO_EV_WRITE) {
        EV_SET(&changes[n], fd, EVFILT_WRITE, EV_ADD | EV_ENABLE, 0, 0, NULL); n++;
    }
    if (n == 0) return 0;

    /* udata 用 fd 编码，保持与 epoll 侧一致的判别语义 */
    for (int i = 0; i < n; i++) changes[i].udata = (void*)(intptr_t)fd;

    return kevent(drv->wait, changes, n, NULL, 0, NULL) >= 0;
}

void kaula_iodriver_backend_del(KaulaIoDriver* drv, int fd) {
    struct kevent changes[2];
    EV_SET(&changes[0], fd, EVFILT_READ,  EV_DELETE, 0, 0, NULL);
    EV_SET(&changes[1], fd, EVFILT_WRITE, EV_DELETE, 0, 0, NULL);
    kevent(drv->wait, changes, 2, NULL, 0, NULL);
}

int kaula_iodriver_backend_wait(KaulaIoDriver* drv, uint64_t timeout_ns) {
    struct kevent evs[IO_READY_BATCH];
    struct timespec ts;
    struct timespec* tsp = NULL;

    if (timeout_ns != KAULA_P_WAIT_FOREVER) {
        ts.tv_sec  = (time_t)(timeout_ns / 1000000000ULL);
        ts.tv_nsec = (long)(timeout_ns % 1000000000ULL);
        tsp = &ts;
    }

    int n = kevent(drv->wait, NULL, 0, evs, IO_READY_BATCH, tsp);
    if (n < 0) return -1;

    g_ready_count = 0;
    g_ready_pos = 0;
    for (int i = 0; i < n; i++) {
        /* 唤醒通道：udata 存哨兵 */
        int fd = (int)(intptr_t)evs[i].udata;
        uint32_t e = 0;
        if (evs[i].filter == EVFILT_READ)  e |= KAULA_IO_EV_READ;
        if (evs[i].filter == EVFILT_WRITE) e |= KAULA_IO_EV_WRITE;
        g_ready[g_ready_count].fd = fd;
        g_ready[g_ready_count].events = e;
        g_ready_count++;
    }
    return g_ready_count;
}

int kaula_iodriver_backend_next_event(KaulaIoDriver* drv, int* fd, uint32_t* events) {
    (void)drv;
    if (g_ready_pos >= g_ready_count) return 0;
    *fd = g_ready[g_ready_pos].fd;
    *events = g_ready[g_ready_pos].events;
    g_ready_pos++;
    return 1;
}

/* ================================================================
 * Windows: IOCP
 *
 * 说明：IOCP 是完成式（completion）模型，不是就绪式。
 * 本实现走"投递 overlapped 请求 → 等完成包"的路径：
 *   add()   把 fd 加入待投递列表
 *   wait()  投递 read/write 并等完成
 *   next_event() 取完成包
 * ================================================================ */
#elif KAULA_P_IO_IOCP

/* IOCP 侧的挂起记录 */
typedef struct {
    OVERLAPPED ov;
    int         fd;
    int         is_write;
    uint32_t    want;
} IocpOp;

static IocpOp       g_iocp_ops[IO_READY_BATCH];
static int          g_iocp_op_count = 0;

/* 待投递的 fd（add 时记下，wait 时真正发 IO） */
typedef struct {
    int      fd;
    uint32_t want;
} IocpPendingAdd;

static IocpPendingAdd g_iocp_adds[IO_READY_BATCH];
static int            g_iocp_add_count = 0;

int kaula_iodriver_backend_add(KaulaIoDriver* drv, int fd, uint32_t want) {
    (void)drv;
    if (g_iocp_add_count >= IO_READY_BATCH) return 0;
    g_iocp_adds[g_iocp_add_count].fd = fd;
    g_iocp_adds[g_iocp_add_count].want = want;
    g_iocp_add_count++;
    return 1;
}

void kaula_iodriver_backend_del(KaulaIoDriver* drv, int fd) {
    (void)drv; (void)fd;
    /*
     * IOCP 下 fd 的生命周期由 socket 持有，
     * 这里无需从内核注销；未投递的 add 项跳过。
     */
}

int kaula_iodriver_backend_wait(KaulaIoDriver* drv, uint64_t timeout_ns) {
    /* 先把 add 队列里的 fd 投递为 overlapped 读 */
    g_iocp_op_count = 0;
    for (int i = 0; i < g_iocp_add_count; i++) {
        if (g_iocp_op_count >= IO_READY_BATCH) break;
        IocpOp* op = &g_iocp_ops[g_iocp_op_count++];
        memset(&op->ov, 0, sizeof(op->ov));
        op->fd = g_iocp_adds[i].fd;
        op->is_write = (g_iocp_adds[i].want & KAULA_IO_EV_WRITE) ? 1 : 0;
        op->want = g_iocp_adds[i].want;
    }
    g_iocp_add_count = 0;

    DWORD ms = (timeout_ns == KAULA_P_WAIT_FOREVER)
             ? INFINITE
             : (DWORD)((timeout_ns + 999999) / 1000000);

    DWORD bytes = 0;
    ULONG_PTR key = 0;
    OVERLAPPED* pov = NULL;

    BOOL ok = GetQueuedCompletionStatus(drv->wait, &bytes, &key, &pov, ms);
    if (!ok) {
        if (pov == NULL) return 0;   /* 超时 */
        return -1;
    }
    if (key == KAULA_P_WAKE_TAG) {
        return 1;   /* 唤醒包：上层识别哨兵后跳过 */
    }

    /* 完成包：找出对应 op */
    IocpOp* op = NULL;
    for (int i = 0; i < g_iocp_op_count; i++) {
        if (&g_iocp_ops[i].ov == pov) { op = &g_iocp_ops[i]; break; }
    }

    g_ready_count = 0;
    g_ready_pos = 0;
    if (op != NULL) {
        g_ready[0].fd = op->fd;
        g_ready[0].events = op->is_write ? KAULA_IO_EV_WRITE : KAULA_IO_EV_READ;
        g_ready_count = 1;
    }
    return g_ready_count;
}

int kaula_iodriver_backend_next_event(KaulaIoDriver* drv, int* fd, uint32_t* events) {
    (void)drv;
    if (g_ready_pos >= g_ready_count) return 0;
    *fd = g_ready[g_ready_pos].fd;
    *events = g_ready[g_ready_pos].events;
    g_ready_pos++;
    return 1;
}

#endif /* backend 分派结束 */

/* ================================================================
 * 实际 IO 执行（三平台共用）
 * ================================================================ */

int kaula_iodriver_do_io(const KaulaIoRequest* req) {
    if (req->type == KAULA_IO_SLEEP) {
        return 0;   /* 定时器已在驱动线程处理 */
    }

    switch (req->type) {
    case KAULA_IO_READ:
    case KAULA_IO_RECV:
        return (int)recv(req->fd, (char*)req->buf, (int)req->buf_len, 0);
    case KAULA_IO_WRITE:
    case KAULA_IO_SEND:
        return (int)send(req->fd, (const char*)req->buf, (int)req->buf_len, 0);
    default:
        return -ENOSYS;
    }
}