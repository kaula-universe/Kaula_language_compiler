/*
 * Kaula 并发内核 · IO 复用基础原语（跨平台）
 *
 * 与 kaula_conc_io_backend.c 的区别：
 * 本文件提供的是"与事件模型无关"的最小集合 —— 建句柄、销毁、非阻塞、唤醒。
 * epoll / kqueue / IOCP 的注册与事件提取在 kaula_conc_io_backend.c。
 *
 * 分离的理由：测试（test_platform.c）只需要这一层就能验证唤醒语义，
 * 不必把三平台事件循环一起拖进来。
 */

#include "kaula_conc_platform.h"

#include <string.h>
#include <errno.h>

#if KAULA_PLATFORM_WINDOWS
    #include <winsock2.h>
#else
    #include <unistd.h>
    #include <fcntl.h>
#endif

/* ================================================================
 * epoll 后端（Linux）
 * ================================================================ */
#if KAULA_P_IO_EPOLL

int kaula_p_io_wait_create(kaula_p_io_wait_t* out) {
    int ep = epoll_create1(EPOLL_CLOEXEC);
    if (ep < 0) return -1;

    /*
     * 唤醒通道：独立 eventfd。
     * data.fd 存 KAULA_P_WAKE_TAG(-1) —— 与真实 fd（>= 0）不冲突，
     * 因此上层读出 data.fd 后与哨兵比较即可可靠区分唤醒与 IO 就绪。
     * 这正是修复缺陷 #6 的关键：不再把 Task 指针塞进 data 字段。
     */
    int efd = eventfd(0, EFD_NONBLOCK | EFD_CLOEXEC);
    if (efd < 0) { close(ep); return -1; }

    struct epoll_event ev;
    memset(&ev, 0, sizeof(ev));
    ev.events = EPOLLIN;
    ev.data.fd = KAULA_P_WAKE_TAG;
    if (epoll_ctl(ep, EPOLL_CTL_ADD, efd, &ev) < 0) {
        close(efd);
        close(ep);
        return -1;
    }

    *out = ep;
    return 0;
}

void kaula_p_io_wait_destroy(kaula_p_io_wait_t w) {
    if (w >= 0) close(w);
}

int kaula_p_io_set_nonblocking(int fd) {
    int flags = fcntl(fd, F_GETFL, 0);
    if (flags < 0) return -1;
    return fcntl(fd, F_SETFL, flags | O_NONBLOCK);
}

int kaula_p_io_wake(kaula_p_io_wait_t w) {
    uint64_t one = 1;
    ssize_t n = write(w, &one, sizeof(one));
    return (n == (ssize_t)sizeof(one)) ? 0 : -1;
}

void kaula_p_io_drain_wake(kaula_p_io_wait_t w) {
    uint64_t val;
    while (read(w, &val, sizeof(val)) > 0) { /* 抽干计数 */ }
}

/* ================================================================
 * kqueue 后端（macOS）
 * ================================================================ */
#elif KAULA_P_IO_KQUEUE

int kaula_p_io_wait_create(kaula_p_io_wait_t* out) {
    int kq = kqueue();
    if (kq < 0) return -1;

    /* EVFILT_USER 作唤醒通道：无需额外 fd */
    struct kevent ch;
    EV_SET(&ch, KAULA_P_WAKE_TAG, EVFILT_USER, EV_ADD | EV_CLEAR, 0, 0, NULL);
    if (kevent(kq, &ch, 1, NULL, 0, NULL) < 0) {
        close(kq);
        return -1;
    }
    *out = kq;
    return 0;
}

void kaula_p_io_wait_destroy(kaula_p_io_wait_t w) {
    if (w >= 0) close(w);
}

int kaula_p_io_set_nonblocking(int fd) {
    int flags = fcntl(fd, F_GETFL, 0);
    if (flags < 0) return -1;
    return fcntl(fd, F_SETFL, flags | O_NONBLOCK);
}

int kaula_p_io_wake(kaula_p_io_wait_t w) {
    struct kevent ch;
    EV_SET(&ch, KAULA_P_WAKE_TAG, EVFILT_USER, 0, NOTE_TRIGGER, 0, NULL);
    return (kevent(w, &ch, 1, NULL, 0, NULL) < 0) ? -1 : 0;
}

void kaula_p_io_drain_wake(kaula_p_io_wait_t w) {
    /* EVFILT_USER 是一次性的，无需抽干 */
    (void)w;
}

/* ================================================================
 * IOCP 后端（Windows）
 * ================================================================ */
#elif KAULA_P_IO_IOCP

int kaula_p_io_wait_create(kaula_p_io_wait_t* out) {
    HANDLE h = CreateIoCompletionPort(INVALID_HANDLE_VALUE, NULL, 0, 0);
    if (h == NULL) return -1;
    *out = h;
    return 0;
}

void kaula_p_io_wait_destroy(kaula_p_io_wait_t w) {
    if (w) CloseHandle(w);
}

int kaula_p_io_set_nonblocking(int fd) {
#if defined(SOCKET)
    u_long mode = 1;
    return (ioctlsocket((SOCKET)fd, FIONBIO, &mode) == 0) ? 0 : -1;
#else
    (void)fd;
    return 0;
#endif
}

int kaula_p_io_wake(kaula_p_io_wait_t w) {
    /*
     * completionKey 用 WAKE_TAG(0)，真实 fd 不可能为 0
     * （0 是 stdin，且我们不会把 stdin 注册进内核），
     * 因此上层凭 key==0 即可识别唤醒包。
     */
    return PostQueuedCompletionStatus(w, 0, KAULA_P_WAKE_TAG, NULL) ? 0 : -1;
}

void kaula_p_io_drain_wake(kaula_p_io_wait_t w) {
    /* 一次 PostQueuedCompletionStatus 对应一次唤醒，无需抽干 */
    (void)w;
}

#endif /* 后端分派结束 */