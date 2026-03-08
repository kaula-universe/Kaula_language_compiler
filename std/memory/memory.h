#ifndef STD_MEMORY_MEMORY_H
#define STD_MEMORY_MEMORY_H

#include "../base/types.h"
#include "../../src/kaula.h"

// 内存分配器结构
// 使用src目录中的高性能内存分配器
extern void fast_allocator_init();
extern void* fast_alloc(size_t size);
extern void* fast_calloc(size_t num, size_t size);

// 标准内存分配函数
extern void* std_malloc(size_t size);
extern void* std_calloc(size_t num, size_t size);
extern void* std_realloc(void* ptr, size_t size);
extern void std_free(void* ptr);

// 内存使用统计
extern size_t memory_used();
extern size_t memory_available();
extern size_t memory_total();

// 内存安全检查
extern bool memory_is_valid(void* ptr);
extern bool memory_is_allocated(void* ptr);

// 内存操作函数
extern void memory_copy(void* dest, const void* src, size_t size);
extern void memory_move(void* dest, const void* src, size_t size);
extern void memory_set(void* ptr, int value, size_t size);
extern int memory_compare(const void* ptr1, const void* ptr2, size_t size);

// 内存对齐函数
extern void* memory_align(size_t alignment, size_t size);
extern void memory_align_free(void* ptr);

// 内存池管理
extern void memory_pool_init(size_t size);
extern void memory_pool_destroy();
extern void* memory_pool_alloc(size_t size);
extern void memory_pool_free(void* ptr);

// 分级内存池管理器
extern void memory_pool_manager_init();
extern void memory_pool_manager_destroy();
extern void* memory_pool_manager_alloc(size_t size);
extern void memory_pool_manager_free(void* ptr);
extern size_t memory_pool_manager_used();
extern size_t memory_pool_manager_total();
extern void memory_pool_manager_stats();

// 内存调试
extern void memory_dump_usage();
extern void memory_check_leaks();

#endif // STD_MEMORY_MEMORY_H