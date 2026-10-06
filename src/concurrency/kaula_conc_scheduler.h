/*
 * Kaula 并发内核（CONC）· 调度器
 *
 * 职责（文档 §4）：
 * - spawn：把 Task 提交到当前 Worker 的本地队列
 * - 全局注入队列：接收本地队列满时的溢出，以及外部线程的提交
 * - 背压：一切有界（I3），满了返回 WouldBlock 而非丢弃
 */

#ifndef KAULA_CONC_SCHEDULER_H
#define KAULA_CONC_SCHEDULER_H

#include <stdint.h>
#include <stdatomic.h>

#include "kaula_conc_task.h"

#ifdef __cplusplus
extern "C" {
#endif

/* ========== 全局注入队列（MPMC 有界，分片） ========== */

typedef struct {
    _Atomic(uint64_t)   head;
    char                _pad0[KAULA_CACHE_LINE - sizeof(uint64_t)];
    _Atomic(uint64_t)   tail;
    char                _pad1[KAULA_CACHE_LINE - sizeof(uint64_t)];
    _Atomic(KaulaTask*) buffer[KAULA_INJECT_SHARD_CAPACITY];
} KaulaInjectShard;

typedef struct {
    /*
     * 分片后的注入队列。每一片都是独立的 MPSC/MPMC 有界队列，
     * head/tail 与 push 锁全部按片私有 —— 这是把 32 核上的单一
     * 全局争用点拆成 8 份的全部手段（见 kaula_conc_task.h 常量处的
     * 长注释）。lock 与 shard 分开数组存放，避免同一缓存行上
     * 8 把锁被不同核反复弹跳（false sharing）。
     */
    KaulaInjectShard shard[KAULA_INJECT_SHARDS];
    /* 每片一把 push 串行化锁，彼此独立 */
    atomic_flag      push_lock[KAULA_INJECT_SHARDS];
} KaulaInjectQueue;

void kaula_inject_queue_init(void);

int  kaula_inject_queue_push(KaulaTask* task);
/*
 * P2.1：批量注入。一次拿锁、连续写 n 个 payload、一次 release 推 tail。
 * 返回实际接纳数（≤n）。剩余空间不足时返回 < n，调用方重试到全部入队。
 */
int  kaula_inject_queue_push_batch(KaulaTask** tasks, int n);
KaulaTask* kaula_inject_queue_pop(void);
/*
 * 批量弹出：一次 CAS 推进 head m 个，把连续的 m 个 Task 写进 out[]。
 * 返回实际弹出数（≤max）。空则返回 0。
 *
 * 为什么需要：外部线程（main）投递全部走全局注入队列，Worker 若每次只
 * pop 一个 Task，那么每执行一个 Task 就要回来重新竞争 head，且本地队列
 * 恒空 → 窃取恒失败 → 频繁 park（每次 ~260µs condvar 往返）。批量弹出
 * 让一次 CAS 换来 m 个 Task，其中 m-1 个回填本地队列，后续取活走无 CAS
 * 的本地 LIFO 快路径，把全局队列的竞争摊薄到 m 分之一。
 *
 * 注意：out[i] 在 head 推进后即可见（CAS 的 acq_rel 语义），
 * 但因为 head 是单调推进，必须**先**把槽位指针读进 out，**再** CAS
 * 推进 head；否则 CAS 成功后可能有新 push 覆盖槽位。
 */
int  kaula_inject_queue_pop_batch(KaulaTask** out, int max);
int  kaula_inject_queue_empty(void);
uint64_t kaula_inject_queue_size(void);

/*
 * 指定分片的取活/排空（Worker 用）：
 *   prefer = 优先尝试的片（通常是 worker_id & MASK），
 *   该片空则轮转扫其余片。
 * 返回 NULL 表示所有片都空。
 */
KaulaTask* kaula_inject_queue_pop_pref(uint32_t prefer);
/*
 * 只排空指定片（不做全片扫描）。Worker 先扫自己的片、再扫别人的片，
 * 使「本片有货」时不必付全扫代价。
 */
int kaula_inject_queue_pop_batch_shard(uint32_t shard, KaulaTask** out, int max);

/* 简写（内核内部调用） */
int       kaula_scheduler_inject_push(KaulaTask* task);
KaulaTask* kaula_scheduler_inject_pop(void);
int       kaula_scheduler_inject_pop_batch(KaulaTask** out, int max);
int       kaula_scheduler_inject_pop_batch_shard(uint32_t shard, KaulaTask** out, int max);
int       kaula_scheduler_inject_empty(void);
int       kaula_scheduler_inject_empty_shard(uint32_t shard);

/* ========== spawn（文档 §4.3） ========== */

/*
 * 提交一个 Task。
 *
 * 成功返回有效 Handle；失败返回带错误码的空 Handle，
 * 用 kaula_handle_error() 读取原因（KAULA_ERR_WOULD_BLOCK / OOM / SHUTDOWN）。
 *
 * 三级背压：
 * 1. 本地队列有空位 → 入队
 * 2. 本地满 → 全局注入队列
 * 3. 都满且超过全局任务硬上限 → 回收并返回 WouldBlock
 */
KaulaHandle kaula_spawn(KaulaResumeFn resume_fn, void* frame, size_t frame_size);

/*
 * kaula_spawn_flags：与 kaula_spawn 相同，但允许调用方在投递**之前**
 * 设置任务标志（如 KAULA_TASK_FLAG_HINT_BLOCKING）。
 *
 * 不要在 kaula_spawn 返回后直接写 h.task->flags：
 * 任务可能已被 Worker pop 并执行，此时写 flags 与 Worker 读 flags
 * 是数据竞争（实测导致 10% 概率 segfault / 车道计数错乱）。
 * 本函数在 task 对 Worker 可见之前完成 flags 设置，无竞争。
 */
KaulaHandle kaula_spawn_flags(KaulaResumeFn resume_fn, void* frame,
                              size_t frame_size, uint32_t flags);

/*
 * kaula_spawn_enter：带首跑直通入口的提交（守卫形态专用）。
 *
 * 守卫形态的 resume 首跑要过 switch(pc) + sub 守卫判断；这些在首跑时
 * 必然命中（spawn 时 sub 槽清零），是纯浪费。enter_fn 是编译期生成的
 * 去守卫入口：无条件执行入口段到第一个挂起点，此后恢复一律走 resume_fn。
 * enter_fn == NULL 时等价于 kaula_spawn。
 */
KaulaHandle kaula_spawn_enter(KaulaResumeFn resume_fn, KaulaResumeFn enter_fn,
                              void* frame, size_t frame_size);
KaulaHandle kaula_spawn_enter_flags(KaulaResumeFn resume_fn, KaulaResumeFn enter_fn,
                                    void* frame, size_t frame_size, uint32_t flags);

/*
 * kaula_spawn_simple 提交一个"无挂起"的 conc fn。
 *
 * 单一形态的 conc fn 没有状态机：它的 coercion shim 就是普通 C 函数。
 * 但 spawn 仍必须把它交给内核并行执行（否则 spawn 失去并发语义）。
 * 这里用一个 trampoline 把普通函数包装成 Task：
 * Worker 调它 → 调 shim → 一次性得到结果 → complete。
 */
KaulaHandle kaula_spawn_simple(void* (*fn)(void* frame),
                                void* frame, size_t frame_size);

/* 显式指定 owner 的提交（供 IoDriver / Block Lane 完成路径复用） */
KaulaHandle kaula_spawn_to(KaulaWorker* owner, KaulaResumeFn fn, void* frame, size_t frame_size);

/* 唤醒任意一个可能 park 的 Worker（用于唤醒所有 worker 之外的场景） */
void kaula_scheduler_notify_one(void);
/*
 * 分片感知唤醒：唤醒「偏好 shard 这一片」的 Worker（worker_id & MASK == shard）。
 * 投递方按 task_id 低位选片，调用本函数让 owner 片对应的 Worker 来取，
 * 避免统一叫醒 worker[0] 造成的搬运不均与额外 park。
 */
void kaula_scheduler_notify_shard(uint32_t shard);
/*
 * 任务落在哪一片：与 kaula_inject_queue_push 的选片规则一致（task_id & MASK）。
 */
uint32_t kaula_scheduler_shard_of_task(KaulaTask* task);
void kaula_scheduler_notify_all(void);
/* 唤醒至多 n 个正在运行的 Worker（按 id 顺序；n > worker_count 时等同 all） */
void kaula_scheduler_notify_n(uint32_t n);

/* 把 Task 投递给它的 owner（IO / 阻塞完成后的回归路径） */
int kaula_scheduler_resume_task(KaulaTask* task);

/* 窃取：从其他 Worker 偷（供测试与内部调用） */
KaulaTask* kaula_scheduler_steal(KaulaWorker* w);


#ifdef __cplusplus
}
#endif
#endif /* KAULA_CONC_SCHEDULER_H */