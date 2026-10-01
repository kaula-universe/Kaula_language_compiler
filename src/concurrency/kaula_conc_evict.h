/*
 * Kaula 并发内核 · Worker 隔离（撤离）
 *
 * 设计说明书 I5：隔离对象是 Worker，不是任务。
 *
 *   "判定'卡住'时换掉卡住的 Worker，任务原地不动 ——
 *    从根本上避免'搬栈悬垂指针'。"
 *
 * 落地方式分两层：
 *
 * L1 预算（毫秒级，零成本）：每个 resume 消耗一个 tick，
 *    量子耗尽 Task 主动让出。这解决了绝大多数"长计算"，
 *    不需要任何监控线程。
 *
 * L2 停滞判定（兜底）：如果一个 resume 调用本身不返回
 *    （死循环、不可插桩的 FFI、无 suspend 的大块计算），
 *    预算也救不了 —— 因为控制权根本不返回。
 *    此时由撤离监控线程检测"某 Worker 连续执行超过阈值"，
 *    换一个 Worker 顶上（Worker 池扩容），旧的等它自然结束。
 *
 * 注意 L2 不打断旧 Worker：强杀线程在 C 里不合法（会泄漏资源、
 * 破坏不变量）。真正的隔离来自"扩容 + 任务不粘在 Worker 上"：
 * 旧 Worker 卡住时，新 Worker 从队列取走剩余任务继续干活，
 * 系统整体仍然有进展。这就是 I5 的实际含义。
 */

#ifndef KAULA_CONC_EVICT_H
#define KAULA_CONC_EVICT_H

#include <stdint.h>
#include <stdatomic.h>

#include "kaula_conc_task.h"

/* 停滞阈值：单次执行超过该时长视为疑似卡住 */
#ifndef KAULA_DEFAULT_STALL_NS
    #define KAULA_DEFAULT_STALL_NS (10 * 1000000ULL)
#endif

/* 扩容上限：撤离时最多新增多少个 Worker */
#ifndef KAULA_EVICT_MAX_REPLACEMENTS
    #define KAULA_EVICT_MAX_REPLACEMENTS 8
#endif

/* 生命周期 */
int  kaula_evict_init(void);
int  kaula_evict_start(void);
void kaula_evict_stop(void);
void kaula_evict_join(void);
void kaula_evict_destroy(void);

/* 扫描一次所有 Worker（监控线程周期调用；测试可直接调用） */
void kaula_evict_scan(void);

/* 统计 */
uint64_t kaula_evict_total(void);

#endif /* KAULA_CONC_EVICT_H */