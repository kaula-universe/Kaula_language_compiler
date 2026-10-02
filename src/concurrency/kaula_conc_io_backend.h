/*
 * Kaula 并发内核 · IO 复用后端
 *
 * 把 epoll / kqueue / IOCP 三种后端收敛成 5 个操作：
 *   add(task, want)               注册关注并投递（就绪式只注册，完成式顺带投递）
 *   del(fd)                       注销 / 取消在途操作
 *   wait(timeout_ns)              等待，返回事件数量
 *   next_event(&fd, &events)      取出一个就绪事件（就绪式）
 *   next_completion(&task, &res)  取出一个已完成结果（完成式）
 *
 * 两种事件模型的分野（这是后端接口为什么要分成两支）：
 *
 *   就绪式（epoll / kqueue）：内核只说"这个 fd 现在可读了"，
 *       真正的 IO 由驱动循环在唤醒之后调 do_io 执行。
 *       next_event 给出 fd 与事件位。
 *
 *   完成式（IOCP）：IO 在投递的那一刻就由系统执行了，
 *       完成包带来的是"已经做完"的事实与字节数。
 *       所以完成路径绝不能再调 do_io —— 那会把同一个 fd 再读一遍，
 *       第二次读拿到的是"没有更多数据"，结果是静默丢数据。
 *       next_completion 直接给出 Task 与结果。
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

/* 事件位（跨后端统一） */
#define KAULA_IO_EV_READ   0x1
#define KAULA_IO_EV_WRITE  0x2
#define KAULA_IO_EV_ERROR  0x4

/* 唤醒哨兵 fd：不可能与真实 fd 相同（真实 fd 总 >= 0 且通常 > 2） */
#define KAULA_P_IO_WAKE_FD (-1)

/*
 * 等待上限定义在 kaula_conc_task.h（KAULA_IO_BATCH_WAIT_NS），
 * 此处不重复定义。
 */

/*
 * 注册一个等待者并开始这次 IO。
 *
 * want 里的 KAULA_IO_EV_READ / WRITE 决定关注方向。
 * fd 从 task->io_request.fd 取 —— 完成式后端需要连 buf 与 buf_len
 * 一起投递，所以接口必须收 Task 而不是裸 fd。
 *
 * 返回 1 = 已接手，0 = 拒绝（驱动应立刻把 Task 交回调度器）。
 */
int  kaula_iodriver_backend_add(KaulaIoDriver* drv, KaulaTask* task, uint32_t want);
void kaula_iodriver_backend_del(KaulaIoDriver* drv, int fd);

/*
 * 等待事件。
 * timeout_ns = KAULA_P_WAIT_FOREVER 表示无限等待。
 * 返回就绪事件数，< 0 表示出错（errno 已设置）。
 */
int kaula_iodriver_backend_wait(KaulaIoDriver* drv, uint64_t timeout_ns);

/* 取出一个就绪事件；返回 0 表示批次已空。就绪式后端专用。 */
int kaula_iodriver_backend_next_event(KaulaIoDriver* drv, int* fd, uint32_t* events);

/*
 * 取出一个已完成的结果；返回 0 表示批次已空。完成式后端专用。
 *
 * out_result 是字节数，或负的 -errno。数据已经在 task 的缓冲区里，
 * 调用方直接交回调度器，不要再执行一次 IO。
 */
int kaula_iodriver_backend_next_completion(KaulaIoDriver* drv,
                                           KaulaTask** out_task, int* out_result);

/* 批次开始前清空后端内部状态（完成式后端要在此归还未投递的记录） */
void kaula_iodriver_backend_begin(KaulaIoDriver* drv);

/*
 * 放弃所有在途操作（关停时调用）。
 *
 * 完成式后端必须实现：那些操作在端口关闭后仍会完成并排包，而此时
 * 已无人收取，记录就泄漏了。就绪式后端是空实现 —— 那边由驱动自己
 * 管挂起表。
 */
void kaula_iodriver_backend_abort_all(KaulaIoDriver* drv);

/*
 * 真正执行一次 IO。
 * 返回字节数，负值为 -errno。
 * EAGAIN 表示"暂时不可用"，调用方应重新挂起。
 *
 * 语义是 fd：READ/WRITE 走 read/write，普通文件与 pipe 都能用；
 * POSIX 上 socket 也是 fd，所以网络不需要另一条路径。
 * CONNECT / ACCEPT 内核不实现，返回 -ENOSYS。
 */
int kaula_iodriver_do_io(const KaulaIoRequest* req);

#endif /* KAULA_CONC_IO_BACKEND_H */