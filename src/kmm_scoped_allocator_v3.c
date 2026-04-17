#ifndef KMM_SCOPED_ALLOCATOR_V3_IMPL_H
#define KMM_SCOPED_ALLOCATOR_V3_IMPL_H

#include "kmm_scoped_allocator_v4.h"
#include <stdio.h>
#include <stdbool.h>
#include <stddef.h>

#ifdef _WIN32
#include <windows.h>
#ifndef CLOCK_MONOTONIC
#define CLOCK_MONOTONIC 0
#endif

#ifndef _TIMESPEC_DEFINED
struct timespec {
    long tv_sec;
    long tv_nsec;
};
#endif

#ifndef _POSIX_TIMERS
static inline int clock_gettime(int clk_id, struct timespec* ts) {
    static LARGE_INTEGER frequency;
    static int initialized = 0;
    LARGE_INTEGER counter;
    if (!initialized) {
        QueryPerformanceFrequency(&frequency);
        initialized = 1;
    }
    QueryPerformanceCounter(&counter);
    ts->tv_sec = counter.QuadPart / frequency.QuadPart;
    ts->tv_nsec = ((counter.QuadPart % frequency.QuadPart) * 1000000000) / frequency.QuadPart;
    (void)clk_id;
    return 0;
}
#endif
#endif

// ==================== 简化的 kmm_context_t（兼容 V3 API） ====================
typedef struct {
    size_t alloc_counter;
    size_t total_bytes;
    size_t peak_usage;
} kmm_context_t __attribute__((aligned(KMM_V4_CACHE_LINE_SIZE)));

// 全局上下文
static kmm_context_t g_kmm_ctx = {0};

// ==================== 公共 API 实现（简化版） ====================

int kmm_init(kmm_context_t* ctx) {
    if (!ctx) return -1;
    memset(ctx, 0, sizeof(kmm_context_t));
    return 0;
}

void kmm_destroy(kmm_context_t* ctx) {
    if (!ctx) return;
    memset(ctx, 0, sizeof(kmm_context_t));
}

void* kmm_alloc(kmm_context_t* ctx, size_t size, const char* file, int line) {
    (void)ctx;
    (void)file;
    (void)line;
    
    if (KMM_V4_UNLIKELY(size == 0)) return NULL;
    
    void* ptr = kmm_v4_malloc(size);
    
    #ifdef KMM_V4_STATS
    g_kmm_ctx.alloc_counter++;
    g_kmm_ctx.total_bytes += size;
    if (g_kmm_ctx.total_bytes > g_kmm_ctx.peak_usage) {
        g_kmm_ctx.peak_usage = g_kmm_ctx.total_bytes;
    }
    #endif
    
    return ptr;
}

void kmm_free(void* ptr) {
    kmm_v4_free(ptr);
}

void** kmm_alloc_batch(kmm_context_t* ctx, size_t size, size_t count, const char* file, int line) {
    void** ptrs = (void**)kmm_alloc(ctx, count * sizeof(void*), file, line);
    if (KMM_V4_UNLIKELY(!ptrs)) return NULL;
    
    uint8_t* base = (uint8_t*)kmm_alloc(ctx, size * count, file, line);
    if (KMM_V4_UNLIKELY(!base)) return NULL;
    
    for (size_t i = 0; i < count; i++) {
        ptrs[i] = base + i * size;
    }
    
    return ptrs;
}

void kmm_reset(void) {
    kmm_v4_reset();
    memset(&g_kmm_ctx, 0, sizeof(g_kmm_ctx));
}

// 性能统计（简化版）
void kmm_print_pool_stats(void) {
    printf("\n=== KMM V4 Pool Statistics ===\n");
    printf("Pool Size:      %zu bytes (%.2f MB)\n", 
           (size_t)KMM_V4_POOL_SIZE, KMM_V4_POOL_SIZE / (1024.0 * 1024.0));
    printf("Used:           %zu bytes (%.2f MB)\n", 
           kmm_v4_usage(), kmm_v4_usage() / (1024.0 * 1024.0));
    printf("Available:      %zu bytes (%.2f MB)\n", 
           kmm_v4_available(), kmm_v4_available() / (1024.0 * 1024.0));
    printf("Usage:          %.2f%%\n", 
           (kmm_v4_usage() * 100.0) / KMM_V4_POOL_SIZE);
    #ifdef KMM_V4_STATS
    printf("Alloc Count:    %zu\n", g_kmm_ctx.alloc_counter);
    printf("Total Bytes:    %zu bytes (%.2f MB)\n", 
           g_kmm_ctx.total_bytes, g_kmm_ctx.total_bytes / (1024.0 * 1024.0));
    printf("Peak Usage:     %zu bytes (%.2f MB)\n", 
           g_kmm_ctx.peak_usage, g_kmm_ctx.peak_usage / (1024.0 * 1024.0));
    #endif
}

// ==================== 便捷宏（兼容 V3 风格） ====================
#define KMM_V3_ALLOC(size)      kmm_v4_malloc(size)
#define KMM_V3_FREE(ptr)        kmm_v4_free(ptr)
#define KMM_V3_RESET()          kmm_v4_reset()

#define KMM_V3_ALLOC_BATCH(type, count) \
    ((type*)kmm_v4_malloc(sizeof(type) * (count)))

#define KMM_V3_ALLOC_ARRAY(type, count) \
    ((type*)kmm_v4_malloc(sizeof(type) * (count)))

#define KMM_V3_ALLOC_STRUCT(type) \
    ((type*)kmm_v4_malloc(sizeof(type)))

#endif // KMM_SCOPED_ALLOCATOR_V3_IMPL_H
