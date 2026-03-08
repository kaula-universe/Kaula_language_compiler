#include "memory_pool.h"
#include "memory.h"
#include <stdio.h>
#include <string.h>

// 预定义的内存块大小
static const size_t BLOCK_SIZES[] = {
    16,    // 16字节
    32,    // 32字节
    64,    // 64字节
    128,   // 128字节
    256,   // 256字节
    512,   // 512字节
    1024,  // 1KB
    2048,  // 2KB
    4096,  // 4KB
    8192   // 8KB
};

#define POOL_COUNT (sizeof(BLOCK_SIZES) / sizeof(BLOCK_SIZES[0]))

// 全局内存池管理器
static MemoryPoolManager g_pool_manager = {0};

// 找到适合大小的内存池
static MemoryPool* find_suitable_pool(size_t size) {
    for (size_t i = 0; i < POOL_COUNT; i++) {
        if (BLOCK_SIZES[i] >= size) {
            return g_pool_manager.pools[i];
        }
    }
    return NULL; // 超出最大块大小，使用标准分配
}

// 初始化单个内存池
static void init_memory_pool(MemoryPool* pool, size_t block_size) {
    pool->block_size = block_size;
    pool->free_list = NULL;
    pool->used_list = NULL;
    pool->total_blocks = 0;
    pool->free_blocks = 0;
    pool->used_blocks = 0;
}

// 扩展内存池
static void expand_memory_pool(MemoryPool* pool, size_t count) {
    const size_t BLOCK_OVERHEAD = sizeof(MemoryBlock);
    const size_t total_size = (pool->block_size + BLOCK_OVERHEAD) * count;
    
    uint8_t* memory = (uint8_t*)fast_alloc(total_size);
    if (!memory) {
        fprintf(stderr, "Error: Failed to expand memory pool\n");
        return;
    }
    
    for (size_t i = 0; i < count; i++) {
        MemoryBlock* block = (MemoryBlock*)(memory + i * (pool->block_size + BLOCK_OVERHEAD));
        block->size = pool->block_size;
        block->is_free = true;
        block->next = pool->free_list;
        block->prev = NULL;
        
        if (pool->free_list) {
            pool->free_list->prev = block;
        }
        
        pool->free_list = block;
        pool->total_blocks++;
        pool->free_blocks++;
    }
    
    g_pool_manager.total_memory += total_size;
}

// 初始化内存池管理器
void memory_pool_manager_init() {
    g_pool_manager.total_memory = 0;
    g_pool_manager.used_memory = 0;
    
    for (size_t i = 0; i < POOL_COUNT; i++) {
        g_pool_manager.pools[i] = (MemoryPool*)fast_alloc(sizeof(MemoryPool));
        if (g_pool_manager.pools[i]) {
            init_memory_pool(g_pool_manager.pools[i], BLOCK_SIZES[i]);
            // 初始分配100个块
            expand_memory_pool(g_pool_manager.pools[i], 100);
        }
    }
    g_pool_manager.pool_count = POOL_COUNT;
}

// 销毁内存池管理器
void memory_pool_manager_destroy() {
    // 内存池使用的是fast_alloc分配的内存，不需要单独释放
    // 当fast_alloc的内存池被销毁时会自动释放
    memset(&g_pool_manager, 0, sizeof(g_pool_manager));
}

// 分配内存
void* memory_pool_manager_alloc(size_t size) {
    // 找到适合的内存池
    MemoryPool* pool = find_suitable_pool(size);
    if (!pool) {
        // 超出最大块大小，使用标准分配
        void* ptr = fast_alloc(size);
        if (ptr) {
            g_pool_manager.used_memory += size;
        }
        return ptr;
    }
    
    // 如果没有空闲块，扩展内存池
    if (!pool->free_list) {
        expand_memory_pool(pool, 50);
    }
    
    // 从空闲列表中取出一个块
    MemoryBlock* block = pool->free_list;
    if (block) {
        // 从空闲列表移除
        pool->free_list = block->next;
        if (pool->free_list) {
            pool->free_list->prev = NULL;
        }
        
        // 添加到使用列表
        block->next = pool->used_list;
        block->prev = NULL;
        if (pool->used_list) {
            pool->used_list->prev = block;
        }
        pool->used_list = block;
        
        block->is_free = false;
        pool->free_blocks--;
        pool->used_blocks++;
        
        g_pool_manager.used_memory += block->size;
        
        // 返回块的实际内存区域（跳过MemoryBlock头）
        return (void*)((uint8_t*)block + sizeof(MemoryBlock));
    }
    
    return NULL;
}

// 释放内存
void memory_pool_manager_free(void* ptr) {
    if (!ptr) return;
    
    // 检查是否是大内存块（通过地址范围判断）
    uint8_t* ptr_byte = (uint8_t*)ptr;
    bool is_large_block = true;
    
    // 检查是否在任何内存池的范围内
    for (size_t i = 0; i < POOL_COUNT; i++) {
        MemoryPool* pool = g_pool_manager.pools[i];
        if (pool) {
            // 遍历使用中的块，检查ptr是否在其中
            MemoryBlock* block = pool->used_list;
            while (block) {
                uint8_t* block_start = (uint8_t*)block + sizeof(MemoryBlock);
                uint8_t* block_end = block_start + block->size;
                if (ptr_byte >= block_start && ptr_byte < block_end) {
                    is_large_block = false;
                    break;
                }
                block = block->next;
            }
            if (!is_large_block) break;
        }
    }
    
    if (is_large_block) {
        // 大内存块，直接释放
        fast_free(ptr);
        // 注意：大内存块的大小无法直接获取，这里不更新used_memory
        // 实际应用中应该在分配时记录大小
        return;
    }
    
    // 计算MemoryBlock的地址
    MemoryBlock* block = (MemoryBlock*)((uint8_t*)ptr - sizeof(MemoryBlock));
    
    // 找到对应的内存池
    MemoryPool* pool = find_suitable_pool(block->size);
    if (!pool) {
        // 超出最大块大小，使用标准释放
        fast_free(ptr);
        return;
    }
    
    // 从使用列表移除
    if (block->prev) {
        block->prev->next = block->next;
    } else {
        pool->used_list = block->next;
    }
    
    if (block->next) {
        block->next->prev = block->prev;
    }
    
    // 添加到空闲列表
    block->next = pool->free_list;
    block->prev = NULL;
    if (pool->free_list) {
        pool->free_list->prev = block;
    }
    pool->free_list = block;
    
    block->is_free = true;
    if (pool->used_blocks > 0) {
        pool->used_blocks--;
    }
    pool->free_blocks++;
    
    g_pool_manager.used_memory -= block->size;
}

// 获取内存使用情况
size_t memory_pool_manager_used() {
    return g_pool_manager.used_memory;
}

size_t memory_pool_manager_total() {
    return g_pool_manager.total_memory;
}

// 内存池统计信息
void memory_pool_manager_stats() {
    printf("Memory Pool Manager Stats:\n");
    printf("Total memory: %zu bytes\n", g_pool_manager.total_memory);
    printf("Used memory: %zu bytes\n", g_pool_manager.used_memory);
    printf("Usage: %.2f%%\n", g_pool_manager.total_memory > 0 ? 
           (float)g_pool_manager.used_memory / g_pool_manager.total_memory * 100 : 0);
    
    for (size_t i = 0; i < POOL_COUNT; i++) {
        MemoryPool* pool = g_pool_manager.pools[i];
        if (pool) {
            printf("Pool %zu (block size: %zu bytes):\n", i, pool->block_size);
            printf("  Total blocks: %zu\n", pool->total_blocks);
            printf("  Free blocks: %zu\n", pool->free_blocks);
            printf("  Used blocks: %zu\n", pool->used_blocks);
            printf("  Usage: %.2f%%\n", pool->total_blocks > 0 ? 
                   (float)pool->used_blocks / pool->total_blocks * 100 : 0);
        }
    }
}
