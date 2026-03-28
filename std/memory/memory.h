#ifndef STD_MEMORY_MEMORY_H
#define STD_MEMORY_MEMORY_H

#include "../base/types.h"
#include "../../src/kmm_scoped_allocator_v2.h"

// ==================== KMM V2 ScopedAllocator 运行时 API ====================
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
extern void fast_allocator_init(void);
extern void* fast_alloc(size_t size);
extern void* fast_calloc(size_t num, size_t size);

// 标准内存分配函数
extern void* std_malloc(size_t size);
extern void std_free(void* ptr);

#endif // STD_MEMORY_MEMORY_H
