/*
 * Kaula 并发内核（CONC）· IoDriver（可插桩 IO 驱动）
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

#ifdef __cplusplus
extern "C" {
#endif

/* 前向声明：后端头文件需要它 */
typedef struct KaulaIoDriver KaulaIoDriver;
typedef struct KaulaIoShard  KaulaIoShard;

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
    struct KaulaTimer*   next;   /* 溢出链用；堆化后正常路径不用 */
} KaulaTimer;

/* ========== IoDriver 主结构（sharded epoll：v1.2） ========== */

/*
 * 就绪事件（后端批次元素，epoll/kqueue 每 shard 一份）。
 * 原为 io_backend.c 文件级静态 g_ready —— 多 shard 线程共享会互相覆盖，
 * 移入 shard 内（见 io_backend.c）。
 */
typedef struct {
    int      fd;
    uint32_t events;
} IoReadyEvent;

#define KAULA_IO_MAX_SHARDS 64

/*
 * 定时器最小堆容量（每 shard 一份）。
 *
 * 为什么从"无序链表 + 全链扫描"改成二叉最小堆：
 *
 *   旧实现 io_timers_fire 每当 timers_min_deadline 到期，就**整链扫描
 *   一次 O(n)** 挑出到期者。60k/s × 200µs sleep 场景下单 shard 在途
 *   定时器稳态上万，每 ~200µs 一次 O(n) 全扫 = 每轮几十万次访问，
 *   全部串行压在该 shard 线程上 —— 实测单 shard 定速 60k/s 的 p99
 *   被顶到 ~10-15ms（p999 45-320ms），远高于 Go 的 4-5ms。
 *
 *   最小堆把「取到期」变成 O(k log n)（k=本次到期数），插入 O(log n)
 *   取代 O(1) 头插——插入略变贵，但消除了每轮 O(n) 全扫这个
 *   O(每秒轮数 × n) 的主导项。sleep 天然集中到期，k 通常远小于 n。
 *
 * 容量取 65536：与 KAULA_IODRIVER_MAX_PENDING 同量级（挂起上限）。
 * 堆满时退化到 sh->timer_overflow 线性链（不丢任务、不崩），
 * 且溢出链只在极端突发下短暂存在。
 *
 * 存储：堆是「指针数组」（KaulaTimer*），KaulaTimer 对象仍走无锁池，
 * 因此每个 shard 只多花 65536×8 = 512KB 指针槽（.bss，按需触达）。
 */
#define KAULA_TIMER_HEAP_CAP 65536

/*
 * 一个 shard = 一个 iodriver 线程 + 独立 epoll/kqueue 句柄 + 独立
 * 提交队列/挂起表/定时器/就绪批次。同 fd 恒归同一 shard（哈希固定），
 * 每个 shard 的挂起表/epoll_ctl/timers 只被该 shard 线程碰，跨 shard
 * 无共享可变结构（timer 池为无锁原子栈，跨 shard 共享安全）。
 */
struct KaulaIoShard {
    kaula_p_thread_t  thread;
    _Atomic(int)      running;

    /* 后端句柄：epoll fd / kqueue fd / IOCP handle（每 shard 一份） */
    kaula_p_io_wait_t wait;

    /* Worker → 该 shard 的提交队列（mpsc） */
    kaula_mpsc_queue_t submit_queue;

    /* fd → Task 挂起表（仅本 shard 线程写） */
    KaulaIoPending*   pending[KAULA_IO_BUCKETS];

    /*
     * 定时器最小堆（按 deadline_ns 排序，仅本 shard 线程读写）。
     * timers_min_deadline 是堆顶缓存，供 fire 的 O(1) 短路。
     */
    KaulaTimer*       timer_heap[KAULA_TIMER_HEAP_CAP];
    uint32_t          timer_heap_n;
    uint64_t          timers_min_deadline;

    /* 堆满时的线性溢出链（正常路径恒空） */
    KaulaTimer*       timer_overflow;

    /* 就绪批次（epoll/kqueue；IOCP 保持文件级静态，强制 1 shard 安全） */
    IoReadyEvent      ready[KAULA_IO_BATCH_SIZE];
    int               ready_count;
    int               ready_pos;
};

struct KaulaIoDriver {
    uint32_t          shard_count;                 /* 1..KAULA_IO_MAX_SHARDS */
    KaulaIoShard      shards[KAULA_IO_MAX_SHARDS];

    /* 挂起总数（driver 级单一全局原子：各 shard 线程增减；submit 侧读上限） */
    _Atomic(uint32_t) pending_count;

    /* 统计（metrics.c 读取，保留 driver 级） */
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


#ifdef __cplusplus
}
#endif
#endif /* KAULA_CONC_IODRIVER_H */