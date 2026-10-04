/*
 * Kaula 并发内核（CONC，Continuation-Oriented Native Co-runtime，
 * 面向续延的原生协同运行时，以下简称并发内核/CONC）
 *
 * 统一对外头文件。用户代码与编译器生成的 C 代码只需包含此文件。
 *
 * 用法：
 *     KaulaRuntimeConfig cfg = {0};
 *     cfg.worker_count = 4;
 *     kaula_runtime_init(&cfg);
 *     KaulaHandle h = kaula_spawn(my_resume, frame, sizeof(MyFrame));
 *     void* r = kaula_handle_await(h);
 *     kaula_runtime_shutdown();
 */

#ifndef KAULA_CONC_H
#define KAULA_CONC_H

#include <stdint.h>
#include <stddef.h>
#include <stdbool.h>
#include <stdatomic.h>

/* KMM V4：Task 与帧从它的 survivor 段分配 */
#include "../kmm_scoped_allocator_v4.h"

/* 组件 */
#include "kaula_conc_platform.h"
#include "kaula_conc_task.h"
#include "kaula_conc_ws_deque.h"
#include "kaula_conc_mpsc_queue.h"
#include "kaula_conc_budget.h"
#include "kaula_conc_epoch.h"
#include "kaula_conc_completion.h"
#include "kaula_conc_worker.h"
#include "kaula_conc_scheduler.h"
#include "kaula_conc_iodriver.h"
#include "kaula_conc_block_lane.h"
#include "kaula_conc_evict.h"
#include "kaula_conc_metrics.h"

#ifdef __cplusplus
extern "C" {
#endif

/* ========== 生命周期 ========== */

/* config 可为 NULL（走默认值）。幂等：重复调用安全。 */
extern int  kaula_runtime_init(const KaulaRuntimeConfig* config);
extern void kaula_runtime_shutdown(void);

/* 首次 spawn 时自动初始化（编译器生成的代码会调用） */
extern int  kaula_runtime_ensure_init(void);
extern int  kaula_runtime_is_initialized(void);

/* ========== 查询 ========== */

extern uint32_t kaula_conc_cpu_count(void);
extern uint32_t kaula_conc_worker_count(void);
extern uint64_t kaula_conc_active_tasks(void);
extern int      kaula_conc_is_shutdown(void);

/* ========== 可观测性 ========== */

extern void kaula_metrics_snapshot(KaulaMetrics* out);
extern void kaula_metrics_dump(void);
extern void kaula_metrics_enable(int on);

/* ========== 编译器 lowering 需要的稳定 ABI ========== */

/*
 * 编译器为每个 conc fn 生成：
 *   1. Frame 结构体：含 pc（程序计数）+ 跨 suspend 存活的变量
 *   2. resume 函数：KaulaResumeResult (*)(void* frame)
 *
 * 该签名是对外契约，编译器与运行时必须一致（文档 §9.1）。
 * 顺序调用 conc fn 时编译器额外生成 coercion shim，
 * 内部用 kaula_iodriver_blocking_wait() 就地等待，不经过调度器。
 */


#ifdef __cplusplus
}
#endif
#endif /* KAULA_CONC_H */