/*
 * Kaula 并发内核 · Epoch-Based Reclamation
 *
 * 内存安全的基石（设计说明书 P5、文档 §10.3）。
 *
 * 为什么需要：Task 完成时不能立即 free —— 可能还有别的线程持有
 * KaulaHandle 里的裸指针正在 await。立即 free = UAF。
 *
 * 算法：
 * - 全局 epoch 单调递增。
 * - 每个 Worker 记录自己"当前访问的 epoch"（local_epoch）。
 * - retire(p) 把 p 挂进当前 Worker 的退休列表。
 * - 只有当所有活跃 Worker 的 local_epoch 都 > p 被退休时的 epoch，
 *   才说明没有线程还可能持有 p，此时才真正 free。
 *
 * 进入临界区 = kaula_epoch_enter()；离开 = kaula_epoch_exit()。
 * 临界区覆盖"从队列取 Task"到"确定不再使用该 Task 指针"的全过程。
 */

#ifndef KAULA_CONC_EPOCH_H
#define KAULA_CONC_EPOCH_H

#include <stdint.h>
#include <stddef.h>
#include <stdatomic.h>

#include "kaula_conc_task.h"

#ifdef __cplusplus
extern "C" {
#endif

/* ========== 全局 epoch 域 ========== */

typedef struct {
    _Atomic(uint64_t) global_epoch;
} KaulaEpochDomain;

/*
 * 退休记录：指针 + 退休时所处的 epoch + 复用目标池。
 *
 * pool == NULL：epoch 安全后直接释放（默认）。
 * pool != NULL：epoch 安全后把 ptr 推进 *pool（P0-2 Task 复用池）。
 * 复用池由调用方保证单线程访问（per-Worker）。
 */
typedef struct {
    void*     ptr;
    uint64_t  epoch;
    void**    pool;
} KaulaRetired;

typedef struct {
    KaulaRetired  items[KAULA_EPOCH_MAX_RETIRED];
    uint32_t      count;
} KaulaRetireList;

extern KaulaEpochDomain g_conc_epoch;

/* 初始化 / 拆除 */
void kaula_epoch_init(void);
void kaula_epoch_shutdown(void);

/* 进入临界区：登记本 Worker 当前访问的 epoch */
void kaula_epoch_enter(void);

/*
 * 非 Worker 线程（main / 外部线程）的显式 pin。
 *
 * 必须包住"读 Task / 结果字段"的整个过程。
 * 不 pin 的话没人知道有人在读，reclaim 随时可能把脚下的内存释放掉。
 */
void kaula_epoch_pin(void);
void kaula_epoch_unpin(void);

/*
 * 退出临界区：把 local_epoch 推进到当前 global_epoch，
 * 并顺带尝试一次回收。
 */
void kaula_epoch_exit(void);

/* 延迟释放：把 ptr 挂进当前线程的退休列表 */
void kaula_epoch_retire(void* ptr);

/*
 * 延迟释放（带复用池）：epoch 判定安全后，把 ptr 推进 *pool 而非释放。
 * pool == NULL 等价于 kaula_epoch_retire。
 * *pool 的访问必须是单线程的（P0-2：per-Worker Task 复用池）。
 */
void kaula_epoch_retire_to(void* ptr, void** pool);

/*
 * 尝试回收：把所有"已安全"的退休对象真正释放。
 * 调用者必须已处于"当前 Worker 视角"，retire 列表是 per-Worker 的。
 *
 * 返回本次实际释放的对象个数。
 */
uint32_t kaula_epoch_try_reclaim(void);

/*
 * 单线程上下文（shutdown 时）强制回收全部退休对象。
 */
void kaula_epoch_reclaim_all(void);

/* 统计：当前累计退休数与已回收数 */
uint64_t kaula_epoch_retired_total(void);
uint64_t kaula_epoch_reclaimed_total(void);


#ifdef __cplusplus
}
#endif
#endif /* KAULA_CONC_EPOCH_H */