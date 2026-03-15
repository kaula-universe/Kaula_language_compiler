#ifndef STD_MEMORY_MEMORY_H
#define STD_MEMORY_MEMORY_H

#include "../base/types.h"
#include "../../src/kaula.h"
#include "../../src/kmm_scoped_allocator.h"

// ==================== ScopedAllocator 运行时 API ====================
// 线程局部存储：每个线程/协程一个作用域上下文
#ifdef _WIN32
__declspec(thread) extern kmm_context_t* g_kaula_scope;
#else
__thread extern kmm_context_t* g_kaula_scope;
#endif

// 作用域管理（编译器自动注入）
extern void kaula_scope_enter(void);
extern void kaula_scope_exit(void);

// 作用域分配（编译器生成的代码使用）
extern void* kaula_scope_alloc(size_t size);
extern void kaula_scope_free(void* ptr);

// ==================== 快速分配器 ====================
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