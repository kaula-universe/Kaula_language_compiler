#include "../base/types.h"
#include <stdlib.h>
#include <string.h>
#include <stdio.h>

// 简单快速分配器 - 基于 Arena 的分配器
// 用于临时分配，不需要单独释放，一次性释放所有内存

#define FAST_ALLOC_POOL_SIZE (1024 * 1024)  // 1MB 初始池
#define FAST_ALLOC_MAX_POOLS 16

typedef struct FastAllocPool {
    uint8_t* buffer;
    size_t size;
    size_t used;
    struct FastAllocPool* next;
} FastAllocPool;

static FastAllocPool* g_fast_pools = NULL;
static FastAllocPool* g_current_pool = NULL;
static int g_fast_alloc_initialized = 0;

/**
 * 初始化快速分配器
 */
void fast_allocator_init() {
    if (g_fast_alloc_initialized) {
        return;
    }
    
    // 创建第一个内存池
    FastAllocPool* pool = (FastAllocPool*)malloc(sizeof(FastAllocPool));
    if (!pool) {
        fprintf(stderr, "Error: Failed to allocate fast allocator pool\n");
        return;
    }
    
    pool->buffer = (uint8_t*)malloc(FAST_ALLOC_POOL_SIZE);
    if (!pool->buffer) {
        fprintf(stderr, "Error: Failed to allocate buffer\n");
        free(pool);
        return;
    }
    
    pool->size = FAST_ALLOC_POOL_SIZE;
    pool->used = 0;
    pool->next = NULL;
    
    g_fast_pools = pool;
    g_current_pool = pool;
    g_fast_alloc_initialized = 1;
    
    #ifdef KMM_DEBUG
    printf("[FastAlloc] 初始化完成，池大小：%zu bytes\n", FAST_ALLOC_POOL_SIZE);
    #endif
}

/**
 * 快速分配函数
 * 从当前内存池分配内存，如果不够则创建新池
 */
void* fast_alloc(size_t size) {
    if (!g_fast_alloc_initialized) {
        fast_allocator_init();
    }
    
    // 对齐到 8 字节
    size = (size + 7) & ~7;
    
    // 检查当前池是否有足够空间
    if (g_current_pool->used + size > g_current_pool->size) {
        // 创建新池
        size_t new_size = FAST_ALLOC_POOL_SIZE;
        if (new_size < size * 2) {
            new_size = size * 2;  // 确保新池足够大
        }
        
        FastAllocPool* new_pool = (FastAllocPool*)malloc(sizeof(FastAllocPool));
        if (!new_pool) {
            fprintf(stderr, "Error: Failed to allocate new fast pool\n");
            return NULL;
        }
        
        new_pool->buffer = (uint8_t*)malloc(new_size);
        if (!new_pool->buffer) {
            fprintf(stderr, "Error: Failed to allocate new buffer\n");
            free(new_pool);
            return NULL;
        }
        
        new_pool->size = new_size;
        new_pool->used = 0;
        new_pool->next = NULL;
        
        // 链接到池链表
        g_current_pool->next = new_pool;
        g_current_pool = new_pool;
        
        #ifdef KMM_DEBUG
        printf("[FastAlloc] 创建新池，大小：%zu bytes\n", new_size);
        #endif
    }
    
    // 从当前池分配
    void* ptr = g_current_pool->buffer + g_current_pool->used;
    g_current_pool->used += size;
    
    #ifdef KMM_DEBUG
    printf("[FastAlloc] 分配 %zu bytes @ %p\n", size, ptr);
    #endif
    
    return ptr;
}

/**
 * 快速批量分配函数
 */
void* fast_calloc(size_t num, size_t size) {
    void* ptr = fast_alloc(num * size);
    if (ptr) {
        memset(ptr, 0, num * size);
    }
    return ptr;
}

/**
 * 释放所有快速分配的内存
 * 注意：这个函数会释放所有通过 fast_alloc 分配的内存
 */
void fast_free_all() {
    FastAllocPool* pool = g_fast_pools;
    while (pool) {
        FastAllocPool* next = pool->next;
        if (pool->buffer) {
            free(pool->buffer);
        }
        free(pool);
        pool = next;
    }
    
    g_fast_pools = NULL;
    g_current_pool = NULL;
    g_fast_alloc_initialized = 0;
    
    #ifdef KMM_DEBUG
    printf("[FastAlloc] 释放所有内存\n");
    #endif
}
