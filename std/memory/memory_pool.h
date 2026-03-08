#ifndef STD_MEMORY_MEMORY_POOL_H
#define STD_MEMORY_MEMORY_POOL_H

#include "../base/types.h"

// 内存块结构
typedef struct MemoryBlock {
    size_t size;              // 块大小
    bool is_free;             // 是否空闲
    struct MemoryBlock* next; // 下一个块
    struct MemoryBlock* prev; // 前一个块
} MemoryBlock;

// 内存池结构
typedef struct MemoryPool {
    size_t block_size;        // 块大小
    MemoryBlock* free_list;   // 空闲块列表
    MemoryBlock* used_list;   // 使用中块列表
    size_t total_blocks;      // 总块数
    size_t free_blocks;       // 空闲块数
    size_t used_blocks;       // 使用中块数
} MemoryPool;

// 分级内存池管理器
typedef struct MemoryPoolManager {
    MemoryPool* pools[10];    // 不同大小的内存池
    size_t pool_count;        // 内存池数量
    size_t total_memory;      // 总内存
    size_t used_memory;       // 使用中内存
} MemoryPoolManager;

// 初始化内存池管理器
extern void memory_pool_manager_init();

// 销毁内存池管理器
extern void memory_pool_manager_destroy();

// 分配内存
extern void* memory_pool_manager_alloc(size_t size);

// 释放内存
extern void memory_pool_manager_free(void* ptr);

// 获取内存使用情况
extern size_t memory_pool_manager_used();

extern size_t memory_pool_manager_total();

// 内存池统计信息
extern void memory_pool_manager_stats();

#endif // STD_MEMORY_MEMORY_POOL_H
