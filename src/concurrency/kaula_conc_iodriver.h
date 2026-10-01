/*
 * Kaula 并发内核 · IoDriver（可插桩 IO 驱动）
 *
 * I4：可插桩 IO 不占线程 —— 提交后 Worker 立即转做别的事。
 *
 * 后端按平台选择（文档附录 B）：
 *   Linux epoll(ET) / macOS kqueue / Windows IOCP
 *
 * 修复记录：
 * - 原实现把 Task 指针塞进 epoll 的 ev.data.ptr，而唤醒 fd 又用
 *   ev.data.fd，同一字段被两种语义混用；且同一 fd 的第二个 Task 会走
 *   EPOLL_CTL_MOD 覆盖掉第一个注册，导致第一个 Task 永久挂起。
 *   现改为 ev.data.fd 只存 fd，Task 通过 pending 表按 fd 查找。
 * - 原实现 KAULA_IO_SLEEP 直接返回"立即完成"，定时器是空壳。
 *   现接入真实定时器堆。
 * - 原实现没有 pending 计数上限，I3 在 IO 侧失守。
 *   现加 KAULA_IODRIVER_MAX_PENDING 硬上限，超出返回 WouldBlock。
 * - 提交条目从 KMM 分配但从不释放，现改为随提交完成即释放。
 */

#ifndef KAULA_CONC_IODRIVER_H
#define KAULA_CONC_IODRIVER_H

#include <stdint.h>
#include <stdatomic.h>

#include "kaula_conc_task.h"
#include "kaula_conc_mpsc_queue.h"

/* 前向声明：后端头文件需要它 */
typedef struct KaulaIoDriver KaulaIoDriver;

/* ========== 挂起表（文档 §6.2） ========== */

/*
 * fd → Task 的映射。
 * 哈希桶固定大小（I3），链表节点内嵌。
 */
#define KAULA_IO_BUCKETS 1024

typedef struct KaulaIoPending {
    int                  fd;
    KaulaTask*           task;
    uint32_t             want;     /* 关心的事件（EPOLLIN/EPOLLOUT 等） */
    uint32_t             got;      /* 已就绪的事件 */
    struct KaulaIoPending* next;   /* 桶内链表 */
} KaulaIoPending;

/* ========== 定时器 ========== */

typedef struct KaulaTimer {
    uint64_t             deadline_ns;
    KaulaTask*           task;
    struct KaulaTimer*   next;
} KaulaTimer;

/* ========== IoDriver 主结构 ========== */

struct KaulaIoDriver {
    kaula_p_thread_t  thread;
    _Atomic(int)      running;

    /* 后端句柄：epoll fd / kqueue fd / IOCP handle */
    kaula_p_io_wait_t wait;

    /* Worker → IoDriver 提交队列 */
    kaula_mpsc_queue_t submit_queue;

    /* fd → Task 挂起表（仅 IoDriver 线程写；计数供 submit 侧读，故为原子） */
    KaulaIoPending*   pending[KAULA_IO_BUCKETS];
    _Atomic(uint32_t) pending_count;

    /* 定时器链表（按 deadline 递增插入） */
    KaulaTimer*       timers;

    /* 统计 */
    _Atomic(uint64_t) total_submits;
    _Atomic(uint64_t) total_completions;
    _Atomic(uint64_t) total_timer_fires;
    _Atomic(uint64_t) total_would_block;
};

extern KaulaIoDriver g_conc_iodriver;

/* 生命周期 */
int  kaula_iodriver_init(void);
int  kaula_iodriver_start(void);
void kaula_iodriver_stop(void);
void kaula_iodriver_join(void);
void kaula_iodriver_destroy(void);

/*
 * 提交一个 IO 请求（Worker 调用）。
 * 返回 0 成功（Task 已移交 IoDriver），非 0 表示失败
 * （pending 超上限 / 提交队列满），调用方须让 Task 继续在 Worker 上跑。
 */
int kaula_iodriver_submit(KaulaTask* task);

/*
 * 顺序路径的阻塞式 IO 等待（文档 §9.4 coercion shim）。
 * 直接在当前线程完成 IO，不经过 IoDriver。
 * 返回字节数或 -errno。
 */
int kaula_iodriver_blocking_wait(const KaulaIoRequest* req);

/* 挂起表当前占用（测试与诊断用） */
uint32_t kaula_iodriver_pending_count(void);

/* 定时器测试钩子：直接触发一个 Task 的挂起定时器到期 */
void kaula_iodriver_fire_timer(KaulaTask* task, uint64_t delay_ns);

#endif /* KAULA_CONC_IODRIVER_H */