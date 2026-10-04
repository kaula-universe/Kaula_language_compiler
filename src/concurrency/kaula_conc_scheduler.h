/*
 * Kaula 并发内核 · 调度器
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

/* ========== 全局注入队列（MPMC 有界） ========== */

typedef struct {
    _Atomic(uint64_t)   head;
    char                _pad0[KAULA_CACHE_LINE - sizeof(uint64_t)];
    _Atomic(uint64_t)   tail;
    char                _pad1[KAULA_CACHE_LINE - sizeof(uint64_t)];
    _Atomic(KaulaTask*) buffer[KAULA_INJECT_QUEUE_CAPACITY];
} KaulaInjectQueue;

void kaula_inject_queue_init(void);
int  kaula_inject_queue_push(KaulaTask* task);
/*
 * P2.1：批量注入。一次拿锁、连续写 n 个 payload、一次 release 推 tail。
 * 返回实际接纳数（≤n）。剩余空间不足时返回 < n，调用方重试到全部入队。
 */
int  kaula_inject_queue_push_batch(KaulaTask** tasks, int n);
KaulaTask* kaula_inject_queue_pop(void);
int  kaula_inject_queue_empty(void);
uint64_t kaula_inject_queue_size(void);

/* 简写（内核内部调用） */
int       kaula_scheduler_inject_push(KaulaTask* task);
KaulaTask* kaula_scheduler_inject_pop(void);
int       kaula_scheduler_inject_empty(void);

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
void kaula_scheduler_notify_all(void);

/* 把 Task 投递给它的 owner（IO / 阻塞完成后的回归路径） */
int kaula_scheduler_resume_task(KaulaTask* task);

/* 窃取：从其他 Worker 偷（供测试与内部调用） */
KaulaTask* kaula_scheduler_steal(KaulaWorker* w);

#endif /* KAULA_CONC_SCHEDULER_H */