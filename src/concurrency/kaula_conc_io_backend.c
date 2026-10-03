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

#if KAULA_PLATFORM_WINDOWS
    /* _read / _write（CRT fd 的读写，不涉及 socket） */
    #include <io.h>
#endif

/*
 * 注意：kaula_p_io_* 基础原语（建句柄 / 销毁 / 非阻塞 / 唤醒）
 * 已在 kaula_conc_io_primitives.c 实现，本文件只负责
 * "注册 / 注销 / 等待 / 取事件"这四步。
 */

#define IO_READY_BATCH KAULA_IO_BATCH_SIZE

#if KAULA_P_IO_EPOLL || KAULA_P_IO_KQUEUE
/*
 * 就绪式后端共用的待处理批次。
 * 完成式（IOCP）有自己的一套 —— 那边要交 Task 而不只是 fd。
 */
typedef struct {
    int      fd;
    uint32_t events;
} IoReadyEvent;

static IoReadyEvent g_ready[IO_READY_BATCH];
static int          g_ready_count = 0;
static int          g_ready_pos = 0;
#endif

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

int kaula_iodriver_backend_add(KaulaIoDriver* drv, KaulaTask* task, uint32_t want) {
    int fd = task->io_request.fd;

    /* 非阻塞是 ET 的前提 */
    kaula_p_io_set_nonblocking(fd);

    struct epoll_event ev;
    memset(&ev, 0, sizeof(ev));
    ev.events = to_epoll(want);
    ev.data.fd = fd;      /* 只存 fd —— 修复缺陷 #6 */

    if (epoll_ctl(kaula_p_io_wait_epfd(drv->wait), EPOLL_CTL_ADD, fd, &ev) == 0) return 1;
    if (errno == EEXIST) {
        /* 已注册：合并关注事件，而不是覆盖 Task 指针 */
        return epoll_ctl(kaula_p_io_wait_epfd(drv->wait), EPOLL_CTL_MOD, fd, &ev) == 0;
    }
    return 0;
}

void kaula_iodriver_backend_del(KaulaIoDriver* drv, int fd) {
    epoll_ctl(kaula_p_io_wait_epfd(drv->wait), EPOLL_CTL_DEL, fd, NULL);
}

int kaula_iodriver_backend_wait(KaulaIoDriver* drv, uint64_t timeout_ns) {
    struct epoll_event evs[IO_READY_BATCH];
    int ms = (timeout_ns == KAULA_P_WAIT_FOREVER)
           ? -1
           : (int)((timeout_ns + 999999) / 1000000);

    int n = epoll_wait(kaula_p_io_wait_epfd(drv->wait), evs, IO_READY_BATCH, ms);
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

/*
 * 下面三个是驱动的统一调用面（kaula_conc_iodriver.c 无条件调用它们，
 * 不做平台分支）。就绪式后端需要给出与完成式后端语义一致的空实现 ——
 * 少了任何一个，Linux 上就是 undefined symbol 链接失败。
 */

/* 就绪式后端没有「完成结果」：IO 还没做，结果要等 do_io 跑完才知道。 */
int kaula_iodriver_backend_next_completion(KaulaIoDriver* drv,
                                           KaulaTask** out_task, int* out_result) {
    (void)drv; (void)out_task; (void)out_result;
    return 0;
}

/* 每轮事件循环开始时清空就绪批次。 */
void kaula_iodriver_backend_begin(KaulaIoDriver* drv) {
    (void)drv;
    g_ready_count = 0;
    g_ready_pos = 0;
}

/* 就绪式后端的在途操作由驱动的挂起表持有，这里无事可做。 */
void kaula_iodriver_backend_abort_all(KaulaIoDriver* drv) {
    (void)drv;
}

/* ================================================================
 * macOS: kqueue
 * ================================================================ */
#elif KAULA_P_IO_KQUEUE

int kaula_iodriver_backend_add(KaulaIoDriver* drv, KaulaTask* task, uint32_t want) {
    int fd = task->io_request.fd;
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

    return kevent(kaula_p_io_wait_epfd(drv->wait), changes, n, NULL, 0, NULL) >= 0;
}

void kaula_iodriver_backend_del(KaulaIoDriver* drv, int fd) {
    struct kevent changes[2];
    EV_SET(&changes[0], fd, EVFILT_READ,  EV_DELETE, 0, 0, NULL);
    EV_SET(&changes[1], fd, EVFILT_WRITE, EV_DELETE, 0, 0, NULL);
    kevent(kaula_p_io_wait_epfd(drv->wait), changes, 2, NULL, 0, NULL);
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

    int n = kevent(kaula_p_io_wait_epfd(drv->wait), NULL, 0, evs, IO_READY_BATCH, tsp);
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

/* 就绪式后端没有"完成结果"：IO 还没做，结果要等 do_io 跑完才知道。 */
int kaula_iodriver_backend_next_completion(KaulaIoDriver* drv,
                                           KaulaTask** out_task, int* out_result) {
    (void)drv; (void)out_task; (void)out_result;
    return 0;
}

void kaula_iodriver_backend_begin(KaulaIoDriver* drv) {
    (void)drv;
    g_ready_count = 0;
    g_ready_pos = 0;
}

/* 就绪式后端的在途操作由驱动的挂起表持有，这里无事可做。 */
void kaula_iodriver_backend_abort_all(KaulaIoDriver* drv) {
    (void)drv;
}

/* ================================================================
 * Windows: IOCP
 *
 * 说明：IOCP 是完成式（completion）模型，不是就绪式。
 * 本实现走"投递 overlapped 请求 → 等完成包"的路径：
 *   add()             关联端口 + 投递 ReadFile/WriteFile
 *   wait()            收完成包，攒成一批
 *   next_completion() 取出 Task 与结果
 *
 * 关键：完成包带来的是"已经做完"的事实与字节数，所以完成路径
 * 不再调 do_io —— 那会把同一个 fd 再读一遍，静默丢数据。
 * ================================================================ */
#elif KAULA_P_IO_IOCP

/*
 * 一个在途的 overlapped 操作。
 *
 * 必须独立分配：OVERLAPPED 的生命周期要跨过 wait() 返回、
 * 直到完成包抵达。放在栈上或固定数组里都会被下一次 wait 覆写，
 * 症状是完成包认不出自己的记录、事件被静默丢弃。
 */
typedef struct IocpOp {
    OVERLAPPED    ov;
    HANDLE        handle;
    KaulaTask*    task;
    int           fd;
    int           is_write;
    int           slot;           /* 在途表下标，也是 completionKey 的高位 */
    _Atomic(int)  cancelled;      /* 被 del 取消：完成包照收，但不上交 Task */
} IocpOp;

/*
 * 在途操作表。固定容量 + 线性查找。
 *
 * 并发挂起数已被 KAULA_IODRIVER_MAX_PENDING 限住；而这张表只在
 * IoDriver 线程上被读写（add/del/wait 都由驱动线程调用），所以不需要锁。
 * 加锁只会把一个单线程数据结构复杂化，换不来任何正确性。
 */
#define IOCP_MAX_OPS KAULA_IODRIVER_MAX_PENDING
static IocpOp* g_iocp_ops[IOCP_MAX_OPS];

/* 完成包批次 */
typedef struct {
    KaulaTask* task;
    int        result;
} IocpDone;

static IocpDone g_iocp_done[IO_READY_BATCH];
static int      g_iocp_done_count = 0;
static int      g_iocp_done_pos = 0;

void kaula_iodriver_backend_begin(KaulaIoDriver* drv) {
    (void)drv;
    g_iocp_done_count = 0;
    g_iocp_done_pos = 0;
}

/* 登记一个在途操作，返回槽位下标；满了返回 -1 */
static int iocp_track(IocpOp* op) {
    for (int i = 0; i < IOCP_MAX_OPS; i++) {
        if (g_iocp_ops[i] == NULL) {
            g_iocp_ops[i] = op;
            op->slot = i;
            return i;
        }
    }
    return -1;
}

static void iocp_untrack(int slot) {
    if (slot >= 0 && slot < IOCP_MAX_OPS) g_iocp_ops[slot] = NULL;
}

/*
 * 把句柄关联到本驱动独占的 completion port。
 *
 * 一个句柄只能关联一次；之后再对同一个句柄调 CreateIoCompletionPort
 * 会返回 ERROR_INVALID_PARAMETER —— 那正是"已经关联过"，不是失败。
 * 不忽略它的话，同一个 fd 上的第二个等待者会被误判成注册失败。
 */
static void iocp_associate(KaulaIoDriver* drv, HANDLE h) {
    CreateIoCompletionPort(h, drv->wait, 0, 0);
}

int kaula_iodriver_backend_add(KaulaIoDriver* drv, KaulaTask* task, uint32_t want) {
    const KaulaIoRequest* req = &task->io_request;

    /*
     * fd → HANDLE 必须走 _get_osfhandle，不能直接强转。
     *
     * KaulaIoRequest.fd 是 int，装不下 64 位 HANDLE —— 直接
     * (HANDLE)(intptr_t)fd 会把高位截掉，拿到一个看似合法的野指针，
     * 症状是 CreateIoCompletionPort 失败或更糟：关联到错误的句柄上。
     *
     * 走 CRT fd 也让三处保持同一个口径：Kaula 层给的 fd 在 Windows
     * 上是 CRT fd（fopen/_pipe 的产物），do_io 的 _read/_write 要的
     * 也是 CRT fd，_get_osfhandle 是二者之间唯一的正规转换。
     */
    intptr_t osh = _get_osfhandle(req->fd);
    if (osh == -1) return 0;
    HANDLE h = (HANDLE)osh;
    if (h == INVALID_HANDLE_VALUE) return 0;

    IocpOp* op = (IocpOp*)calloc(1, sizeof(IocpOp));
    if (op == NULL) return 0;

    int slot = iocp_track(op);
    if (slot < 0) {
        free(op);
        return 0;
    }

    op->handle   = h;
    op->task     = task;
    op->fd       = req->fd;
    op->is_write = (want & KAULA_IO_EV_WRITE) ? 1 : 0;

    iocp_associate(drv, h);

    /*
     * 完成包靠 lpOverlapped 认回记录，不靠 completionKey。
     *
     * completionKey 是在 CreateIoCompletionPort 关联时定的，一个句柄
     * 只能关联一次 —— 也就是说它是"每句柄一个值"，装不下"每次操作一个
     * 值"。想在完成包里带上操作身份，唯一正确的位置就是 OVERLAPPED
     * 指针本身。
     *
     * 唤醒包与完成包也不会混淆：唤醒包投递时 lpOverlapped 传 NULL，
     * 所以 pov==NULL 就足以区分，不需要占用 key 的取值空间。
     */
    BOOL ok;
    DWORD transferred = 0;
    if (op->is_write) {
        ok = WriteFile(h, req->buf, (DWORD)req->buf_len, &transferred, &op->ov);
    } else {
        ok = ReadFile(h, req->buf, (DWORD)req->buf_len, &transferred, &op->ov);
    }

    if (ok) {
        /*
         * 立即完成（缓冲区里已有数据、或写往内存设备）。
         *
         * 完成包仍会排进端口 —— 句柄关联了端口之后，Windows 保证
         * 每次 overlapped 操作完成时都恰好有一个包。所以这里不能
         * 就地交付，否则那个包会把同一条记录再交一次。
         * 记录留在表里，等 wait() 统一收。
         */
        return 1;
    }

    DWORD err = GetLastError();
    if (err == ERROR_IO_PENDING) {
        return 1;   /* 正常路径：等完成包 */
    }

    /*
     * 投递就失败（管道已关闭、句柄无效、缓冲区不可写……）。
     * 完成包不会有，所以现在就把错误写进 Task 并清掉记录。
     *
     * 返回 0 让驱动立刻把 Task 交回调度器 —— 它会读到 io_result。
     */
    iocp_untrack(slot);
    free(op);
    task->io_result = -(int)err;
    return 0;
}

void kaula_iodriver_backend_del(KaulaIoDriver* drv, int fd) {
    (void)drv;
    /*
     * 取消该 fd 上所有在途操作。
     *
     * CancelIoEx 让操作以 ERROR_OPERATION_ABORTED 完成，而完成包
     * 照样会来 —— 这是 overlapped 的契约，不是可选项。所以记录
     * 不能在这里 free，得等 wait() 收到那个包再释放。用 cancelled
     * 标记让 wait() 认出来：回收记录，但不上交 Task。
     *
     * 早期实现这里是空函数，理由是"IOCP 下生命周期由句柄持有"。
     * 那只对"再没人等这个 fd"成立；对"某个等待者被取消"是错的 ——
     * 那个 Task 会永久留在内核里，await 死锁，且不报错。
     */
    for (int i = 0; i < IOCP_MAX_OPS; i++) {
        IocpOp* op = g_iocp_ops[i];
        if (op == NULL || op->fd != fd) continue;
        atomic_store_explicit(&op->cancelled, 1, memory_order_relaxed);
        CancelIoEx(op->handle, &op->ov);
    }
}

/*
 * 关停：放弃所有在途操作并释放记录。
 *
 * 这里必须直接 free 而不是只 CancelIoEx —— 端口马上要关，之后再没有
 * 人收包，那些记录永远等不到完成包，只能靠这里回收。
 *
 * 相应地不能把 Task 交回调度器：关停流程已经在做这件事（统一标
 * ECANCELED），这里再交一次就是重复 resume。
 *
 * 驱动线程已 join，所以这里访问在途表不需要同步。
 */
void kaula_iodriver_backend_abort_all(KaulaIoDriver* drv) {
    (void)drv;
    for (int i = 0; i < IOCP_MAX_OPS; i++) {
        IocpOp* op = g_iocp_ops[i];
        if (op == NULL) continue;
        g_iocp_ops[i] = NULL;
        CancelIoEx(op->handle, &op->ov);
        free(op);
    }
    g_iocp_done_count = 0;
    g_iocp_done_pos = 0;
}

int kaula_iodriver_backend_wait(KaulaIoDriver* drv, uint64_t timeout_ns) {
    /*
     * 超时换算：向下取整，亚毫秒一律折成 0（= 立即返回重查定时器堆）。
     *
     * 向上取整是错的，而且错得很贵：200us 会被抬成 1ms，
     * 而 Windows 默认时钟粒度 15.625ms，这一个 1ms 实际阻塞 ~15.6ms。
     * 折成 0 之后驱动立刻回到循环重查 deadline，亚毫秒定时按 µs 级精度触发。
     *
     * 不用取整方向做权衡：向上取整让「本来能立刻触发的定时」多等一个时钟��。
     * 代价只是亚毫秒场景下多几次空转（0 超时的 wait 是纯用户态返回，很快）。
     */
    DWORD ms = (timeout_ns == KAULA_P_WAIT_FOREVER)
             ? INFINITE
             : (DWORD)(timeout_ns / 1000000);

    g_iocp_done_count = 0;
    g_iocp_done_pos = 0;

    /*
     * 收一批：第一包按真实超时等，之后一律 0 超时把已排队的包抽干。
     *
     * 只等一个包会让吞吐被包数拖住：每包一次系统调用往返，而一次
     * 投递往往能攒下好几个完成。
     */
    for (int round = 0; round < IO_READY_BATCH; round++) {
        DWORD bytes = 0;
        ULONG_PTR key = 0;
        OVERLAPPED* pov = NULL;

        BOOL ok = GetQueuedCompletionStatus(drv->wait, &bytes, &key, &pov,
                                            (round == 0) ? ms : 0);
        if (pov == NULL) {
            if (!ok) break;      /* 超时（或端口已关闭） */
            continue;            /* 唤醒包：忽略，继续抽干 */
        }

        /*
         * 完成包：OVERLAPPED 就在 IocpOp 的首位（结构内第一个成员），
         * 所以指针能直接转回去。
         *
         * 还要核对它仍在自己登记的槽位上：abort_all 会在关停时清表，
         * 那时若仍有包到达（端口未完全关闭），转出来的指针已是 freed，
         * 读它就是 UAF。槽位不匹配就丢弃这个包。
         */
        IocpOp* op = (IocpOp*)pov;
        int slot = op->slot;
        if (slot < 0 || slot >= IOCP_MAX_OPS) continue;
        if (g_iocp_ops[slot] != op) continue;   /* 已被回收：丢弃 */

        iocp_untrack(slot);

        if (atomic_load_explicit(&op->cancelled, memory_order_relaxed)) {
            /* 被 del 取消：只回收记录，不上交 Task（它已经走了） */
            free(op);
            continue;
        }

        /*
         * ok==FALSE 时 bytes 无意义，错误在 GetLastError。
         * 自己取消的 ERROR_OPERATION_ABORTED 已在上一步拦掉，
         * 到这里的 FALSE 都是真正的 IO 失败。
         */
        int result = ok ? (int)bytes : -(int)GetLastError();

        g_iocp_done[g_iocp_done_count].task = op->task;
        g_iocp_done[g_iocp_done_count].result = result;
        g_iocp_done_count++;
        free(op);

        if (g_iocp_done_count >= IO_READY_BATCH) break;
    }

    return g_iocp_done_count;
}

int kaula_iodriver_backend_next_event(KaulaIoDriver* drv, int* fd, uint32_t* events) {
    /*
     * 完成式后端不走就绪路径：IO 投递时就做完了，没有"就绪"这回事。
     * 返回 0，驱动循环自然跳过 do_io 分支。
     */
    (void)drv; (void)fd; (void)events;
    return 0;
}

int kaula_iodriver_backend_next_completion(KaulaIoDriver* drv,
                                           KaulaTask** out_task, int* out_result) {
    (void)drv;
    if (g_iocp_done_pos >= g_iocp_done_count) return 0;
    *out_task = g_iocp_done[g_iocp_done_pos].task;
    *out_result = g_iocp_done[g_iocp_done_pos].result;
    g_iocp_done_pos++;
    return 1;
}

#endif /* backend 分派结束 */

/* ================================================================
 * 实际 IO 执行（三平台共用）
 * ================================================================ */

/*
 * 一律用 read/write，不用 recv/send。
 *
 * 为什么：recv/send 是 socket 专属接口，用它们做实际 IO 会把整个内核
 * 绑死在 socket 语义上 —— 头文件要 winsock2.h（必须排在 windows.h 之前），
 * 链接要 ws2_32，普通文件与 pipe 的 fd 一进来就 ENOTSOCK。而 read/write
 * 覆盖得更广：POSIX 上 socket 就是 fd，所以网络照样能走；文件与 pipe
 * 本来就该支持。内核的 IO 复用层（epoll/kqueue）本来就对任意 fd 有效，
 * 执行环节用 recv/send 才是那个多余的约束。
 *
 * 唯一的代价在 Windows：SOCKET 不是 CRT fd，_read/_write 对它无效。
 * 但 Windows 侧的 IO 后端是 IOCP（完成式），它需要真正的 overlapped
 * 投递，那条路径当前尚未实现（见 docs/core-runtime.md 第 3 章），
 * 也就是说 Windows 上这条 IO 路径本来就不通。
 */
int kaula_iodriver_do_io(const KaulaIoRequest* req) {
    if (req->type == KAULA_IO_SLEEP) {
        return 0;   /* 定时器已在驱动线程处理 */
    }

#if KAULA_PLATFORM_WINDOWS
    switch (req->type) {
    case KAULA_IO_READ:
        return _read(req->fd, req->buf, (unsigned int)req->buf_len);
    case KAULA_IO_WRITE:
        return _write(req->fd, req->buf, (unsigned int)req->buf_len);
    default:
        return -ENOSYS;
    }
#else
    switch (req->type) {
    case KAULA_IO_READ:
        return (int)read(req->fd, req->buf, req->buf_len);
    case KAULA_IO_WRITE:
        return (int)write(req->fd, req->buf, req->buf_len);
    default:
        return -ENOSYS;
    }
#endif
}