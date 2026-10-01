/*
 * Kaula 并发内核 · 统一对外头文件
 *
 * 用户代码和测试只需包含此头文件。
 */

#ifndef KAULA_RUNTIME_PUBLIC_H
#define KAULA_RUNTIME_PUBLIC_H

#include <stdint.h>
#include <stddef.h>
#include <stdbool.h>
#include <stdatomic.h>
#include <pthread.h>
#include <unistd.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

/* 平台检测（提供 kaula_get_ticks, kaula_sleep_us 等） */
/* 注意：platform.h 依赖 stdlib.h 已包含 */
#include "../platform.h"

/* KMM V4 内存管理 */
#include "../kmm_scoped_allocator_v4.h"

/* 运行时组件 */
#include "kaula_task.h"
#include "kaula_ws_deque.h"
#include "kaula_budget.h"
#include "kaula_worker.h"
#include "kaula_scheduler.h"

/* ========== 运行时配置 ========== */

typedef struct {
    uint32_t    worker_count;       /* 0 = 自动检测 CPU 数 */
    int32_t     budget_quantum;     /* 0 = 默认 256 */
} KaulaRuntimeConfig;

/* ========== 运行时 API ========== */

/* 初始化运行时 */
extern int kaula_runtime_init(const KaulaRuntimeConfig* config);

/* 关闭运行时 */
extern void kaula_runtime_shutdown(void);

/* ========== 辅助函数 ========== */

/* 获取 CPU 核心数 */
static inline uint32_t kaula_get_cpu_count(void) {
    long n = sysconf(_SC_NPROCESSORS_ONLN);
    return n > 0 ? (uint32_t)n : 2;
}

#endif /* KAULA_RUNTIME_PUBLIC_H */
