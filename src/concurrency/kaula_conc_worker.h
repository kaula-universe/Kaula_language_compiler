/*
 * Kaula 并发内核（CONC）· Worker
 *
 * Worker 是执行 Task 的 OS 线程（文档 §2）。
 *
 * 对应设计说明书 I5：隔离对象是 Worker，不是任务。
 * 判定"卡住"时换掉卡住的 Worker，任务原地不动 ——
 * 任务的状态在帧里，与执行它的线程无关，迁移零成本、无悬垂指针。
 *
 * 修复记录：
 * - 原 kaula_current_worker() 是普通全局变量而非 TLS，
 *   所有 Worker 共享一个指针，spawn 的"我是不是 Worker"判断会错。
 *   现改为 _Thread_local。
 * - 原主循环队列空时 kaula_sleep_us(100) 空转。
 *   现改为 park 到可唤醒事件上（P2：万并发空转 CPU ≈ 0）。
 */

#ifndef KAULA_CONC_WORKER_H
#define KAULA_CONC_WORKER_H

#include <stdint.h>
#include <stddef.h>
#include <stdatomic.h>

#include "kaula_conc_platform.h"
#include "kaula_conc_task.h"
#include "kaula_conc_ws_deque.h"
#include "kaula_conc_budget.h"
#include "kaula_conc_epoch.h"

#ifdef __cplusplus
extern "C" {
#endif

/* 每个 Worker 的统计（文档 §12.1） */
typedef struct {
    _Atomic(uint64_t) tasks_executed;
    _Atomic(uint64_t) steal_count;
    _Atomic(uint64_t) steal_attempts;
    _Atomic(uint64_t) park_count;
    _Atomic(uint64_t) park_total_ns;
    _Atomic(uint64_t) budget_exhausted_count;
    _Atomic(uint64_t) io_completions;
    _Atomic(uint64_t) block_completions;
    _Atomic(uint64_t) completions_drained;
} KaulaWorkerStats;

/*
 * ---- 本地短定时器（P-D：短 sleep 不跨线程，对标 Go checkTimers） ----
 *
 * 短 sleep（<= KAULA_LOCAL_TIMER_MAX_NS）不提交 IoDriver，登记到执行
 * 它的 Worker 自己的定时器链表；调度循环每轮检查到期（check），到期
 * 任务 READY 后直接推回本地队列（owner 写，无锁）。消除每次挂起的
 * 跨线程往返（MPSC 提交 + iodriver 处理 + 批量注入 + notify 唤醒），
 * 这是 suspend 落后 Go 的核心结构性差距。
 *
 * 链表仅 owner 线程访问（add 在 execute 的 SUSPEND 分支；check 在本
 * Worker 调度循环；转移在退出路径），无并发，无需原子。P-D3 起节点
 * 直接内嵌进 KaulaTask（local_deadline_ns + 复用 resume_next 链接），
 * 不再分配 KaulaLocalTimer，省一次指针跳与池管理。
 */
/* 短 sleep 阈值：<= 此值走本地定时器；更长仍提交 IoDriver（低频跨线程可摊薄） */
#define KAULA_LOCAL_TIMER_MAX_NS 1000000ULL /* 1ms */

typedef struct KaulaWorker {
    /* ---- 身份 ---- */
    kaula_p_thread_t  thread;
    uint32_t          worker_id;

    /* ---- 本地队列 ---- */
    KaulaWSDeque      local_queue;

    /* ---- 预算 ---- */
    KaulaBudget       budget;
    KaulaBudgetAdaptor budget_adaptor;

    /* ---- 当前执行 ---- */
    KaulaTask*        current_task;
    uint64_t          current_task_start_ns;

    /* ---- park / 唤醒 ---- */
    kaula_p_event     park_event;   /* Worker 睡在这里 */
    _Atomic(int)      park_requested;
    _Atomic(int)      parked;

    /* ---- 生命周期 ---- */
    _Atomic(int)      running;
    _Atomic(int)      evicted;      /* I5：被标记撤离 */

    /* ---- 随机数（窃取起点，文档 §4.2） ---- */
    uint64_t          rng_state;

    /* ---- epoch ---- */
    _Atomic(uint64_t) local_epoch;
    uint64_t          epoch_exit_count;  /* 临界区退出计数（本 Worker 独占，无原子） */
    KaulaRetireList   retired;

    /*
     * ---- Task 复用池（P0-2） ----
     *
     * owner 独占的单链表（push/pop 都只由本 Worker 在自己上下文执行，
     * 无锁、零竞争）。epoch 判定 Task 安全后，把 Task 推进这个池
     * 而不是 free；spawn 时优先从这里取，取空才退到 KMM 全局分配。
     * done_event 跨复用保留（epoch 期间从未被 destroy）。
     */
    KaulaTask*        task_pool;
    /* task_pool 当前长度（owner 独占维护）。归还时据此判定高水位收缩。 */
    uint32_t          task_pool_len;

    /* ---- 本地短定时器（P-D3：Task 内嵌，owner 独占无锁） ---- */
    KaulaTask*        local_timers;              /* 链接复用 task->resume_next */
    uint64_t          local_timers_min_deadline; /* UINT64_MAX = 空 */
    _Atomic(int)      has_local_timer;

    /*
     * ---- 恢复优先链（P-D2） ----
     *
     * 本地定时器到期任务先入此 FIFO 链（owner 独占），find_work 优先
     * 取它 —— 对标 Go 的 runnext：恢复的任务绝不被新 spawn 的任务用
     * LIFO 压住。基准是"完成一个补 spawn 一个"，若无此链，新任务
     * push head 会把恢复任务一直压住，恢复波次处理被摊慢。
     */
    KaulaTask*        resume_head;
    KaulaTask*        resume_tail;

    /* ---- 统计（独占缓存行） ---- */
    KaulaWorkerStats  stats;
    char              _pad_stats[KAULA_CACHE_LINE];
} KaulaWorker;

/* ========== TLS ========== */

KaulaWorker* kaula_conc_current_worker(void);
void         kaula_conc_set_current_worker(KaulaWorker* w);

/* ========== 全局 Worker 池 ========== */

extern KaulaWorker g_conc_workers[KAULA_MAX_WORKERS];

/* ========== 主循环 ========== */

void kaula_worker_loop(KaulaWorker* w);
void kaula_worker_execute_task(KaulaWorker* w, KaulaTask* task);

/* ========== 生命周期 ========== */

int  kaula_worker_create(KaulaWorker* w, uint32_t worker_id);
int  kaula_worker_prepare(KaulaWorker* w, uint32_t worker_id);
int  kaula_worker_launch(KaulaWorker* w);
void kaula_worker_join(KaulaWorker* w);
void kaula_worker_stop(KaulaWorker* w);

/* 唤醒一个可能正在 park 的 Worker */
void kaula_worker_wake(KaulaWorker* w);

/* ========== 内部工具（供 epoch / scheduler 使用） ========== */

/* Worker 是否处于活跃状态（仍在运行、且曾进入过 epoch 临界区） */
int kaula_conc_worker_is_active(const KaulaWorker* w);

/* 取当前 Worker 的退休列表；非 Worker 线程返回主线程的兜底列表 */
KaulaRetireList* kaula_conc_retire_list(void);

/*
 * 尝试推进全局 epoch。
 *
 * 策略：距上次推进经过足够多的临界区退出，且所有活跃 Worker 都已
 * 推进过 local_epoch，才把 global_epoch 加一。
 * 这样保证"被 retire 的对象至少要等两轮才可能被释放"，
 * 覆盖任何线程的 enter → 访问 → exit 时序。
 */
void kaula_conc_advance_epoch_if_needed(void);

/* 预算自适应（文档 §5.3），由 Worker 在退出临界区时驱动 */
void kaula_conc_worker_update_budget(KaulaWorker* w, uint64_t elapsed_ns);

/* 尝试窃取一个 Task（local → steal → inject），返回 NULL 表示无可用 */
KaulaTask* kaula_conc_worker_find_work(KaulaWorker* w);

/*
 * 把 Task 投递到 Worker。
 *
 * 返回 0 成功，非 0 表示队列满（调用方必须回退，不得静默丢弃 ——
 * 静默丢弃会让 await 永久阻塞，见缺陷 #3）。
 *
 * w == NULL 时投递到全局注入队列。
 */
int kaula_conc_worker_post(KaulaWorker* w, KaulaTask* task);


#ifdef __cplusplus
}
#endif
#endif /* KAULA_CONC_WORKER_H */