/*
 * Kaula 并发内核 · 运行时初始化/关闭
 */

#include "kaula_runtime_public.h"

#include <stdlib.h>
#include <string.h>

/* ========== 全局状态 ========== */

/* Worker 数组 */
KaulaWorker g_workers[KAULA_MAX_WORKERS];
uint32_t g_worker_count = 0;

/* 全局注入队列 */
KaulaInjectQueue g_inject_queue;

/* 全局 Task ID 计数器 */
_Atomic(uint64_t) g_next_task_id = 1;

/* 运行时状态 */
static int g_runtime_initialized = 0;

/* ========== 初始化 ========== */

int kaula_runtime_init(const KaulaRuntimeConfig* config) {
    if (g_runtime_initialized) {
        return 0; /* 已初始化 */
    }

    /* 确定 Worker 数量 */
    uint32_t n_workers = config->worker_count;
    if (n_workers == 0) {
        n_workers = kaula_get_cpu_count();
        if (n_workers == 0) n_workers = 2; /* 兜底 */
    }
    if (n_workers > KAULA_MAX_WORKERS) {
        n_workers = KAULA_MAX_WORKERS;
    }
    g_worker_count = n_workers;

    /* 初始化 KMM V4 */
    kmm_v4_init_pool(0);

    /* 初始化全局注入队列 */
    kaula_inject_queue_init(&g_inject_queue);

    /* 创建 Worker 线程 */
    for (uint32_t i = 0; i < n_workers; i++) {
        int err = kaula_worker_create(&g_workers[i], i);
        if (err != 0) {
            /* 创建失败：停止已创建的 Worker */
            for (uint32_t j = 0; j < i; j++) {
                kaula_worker_stop(&g_workers[j]);
                kaula_worker_join(&g_workers[j]);
            }
            return err;
        }
    }

    g_runtime_initialized = 1;
    return 0;
}

/* ========== 关闭 ========== */

void kaula_runtime_shutdown(void) {
    if (!g_runtime_initialized) {
        return;
    }

    /* 停止所有 Worker */
    for (uint32_t i = 0; i < g_worker_count; i++) {
        kaula_worker_stop(&g_workers[i]);
    }

    /* 等待所有 Worker 退出 */
    for (uint32_t i = 0; i < g_worker_count; i++) {
        kaula_worker_join(&g_workers[i]);
    }

    /* 销毁 KMM V4 */
    kmm_v4_destroy_pool();

    g_runtime_initialized = 0;
}
