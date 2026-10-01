/*
 * Kaula 并发内核 · IO 复用后端
 *
 * 把 epoll / kqueue / IOCP 三种后端收敛成 4 个操作：
 *   add(fd, want)              注册关注
 *   del(fd)                    注销
 *   wait(timeout_ns)           等待，返回就绪数量
 *   next_event(&fd, &events)   取出一个就绪事件
 *
 * 关键约定（修复缺陷 #6）：
 *   ev.data.fd 只承载"文件描述符"语义。
 *   唤醒通道用独立哨兵值 KAULA_P_WAKE_FD 标识，
 *   因此"读出 data.fd 与哨兵比较"是可靠的判别，
 *   不存在 Task 指针被当作整数比较的隐患。
 */

#ifndef KAULA_CONC_IO_BACKEND_H
#define KAULA_CONC_IO_BACKEND_H

#include <stdint.h>

#include "kaula_conc_platform.h"
#include "kaula_conc_task.h"
#include "kaula_conc_iodriver.h"

/*
 * Windows 需要 winsock2.h 才有 recv/send 声明。
 * 必须在 windows.h 之前包含，故此处显式引入。
 */
#if KAULA_PLATFORM_WINDOWS
    #include <winsock2.h>
#endif

/* 事件位（跨后端统一） */
#define KAULA_IO_EV_READ   0x1
#define KAULA_IO_EV_WRITE  0x2
#define KAULA_IO_EV_ERROR  0x4

/* 唤醒哨兵 fd：不可能与真实 fd 相同（真实 fd 总 >= 0 且通常 > 2） */
#define KAULA_P_IO_WAKE_FD (-1)

/*
 * 等待上限定义在 kaula_conc_task.h（KAULA_IO_BATCH_WAIT_NS），
 * 此处不重复定义。

/* 注册 / 注销（内部用） */
int  kaula_iodriver_backend_add(KaulaIoDriver* drv, int fd, uint32_t want);
void kaula_iodriver_backend_del(KaulaIoDriver* drv, int fd);

/*
 * 等待事件。
 * timeout_ns = KAULA_P_WAIT_FOREVER 表示无限等待。
 * 返回就绪事件数，< 0 表示出错（errno 已设置）。
 */
int kaula_iodriver_backend_wait(KaulaIoDriver* drv, uint64_t timeout_ns);

/* 取出一个就绪事件；返回 0 表示批次已空 */
int kaula_iodriver_backend_next_event(KaulaIoDriver* drv, int* fd, uint32_t* events);

/*
 * 真正执行一次 IO。
 * 返回字节数，负值为 -errno。
 * EAGAIN 表示"暂时不可用"，调用方应重新挂起。
 */
int kaula_iodriver_do_io(const KaulaIoRequest* req);

#endif /* KAULA_CONC_IO_BACKEND_H */