#ifndef STD_MEMORY_MEMORY_H
#define STD_MEMORY_MEMORY_H

#include "../base/types.h"
#include "../../src/kmm_scoped_allocator_v4.h"
#include "../../src/kmm_scoped_allocator.c"

/**
 * @file memory.h
 * @brief Kaula 简化内存管理 - 基于 KMM Enhanced V4
 * 
 * 功能：
 * - KMM 作用域分配器（自动内存管理）
 * - 快速分配器（基于 KMM 静态池）
 * - 标准分配器包装
 */

// ==================== KMM 作用域分配器 ====================

#ifdef _WIN32
__declspec(thread) extern kmm_context_t* g_kaula_scope;
#else
__thread extern kmm_context_t* g_kaula_scope;
#endif

extern void kaula_scope_enter(void);
extern void kaula_scope_exit(void);
extern void* kaula_scope_alloc(size_t size);
extern void kaula_scope_free(void* ptr);

// ==================== 快速分配器 ====================

extern void* fast_alloc(size_t size);
extern void* fast_calloc(size_t num, size_t size);
extern void fast_free(void* ptr);

// ==================== 标准分配器 ====================

extern void* std_malloc(size_t size);
extern void std_free(void* ptr);

// ==================== 便捷宏 ====================

#define KMEM_ALLOC(size) kaula_scope_alloc(size)
#define KMEM_FREE(ptr) kaula_scope_free(ptr)
#define KMEM_ALLOC_TYPE(type) ((type*)kaula_scope_alloc(sizeof(type)))
#define KMEM_ALLOC_ARRAY(type, count) ((type*)kaula_scope_alloc(sizeof(type) * (count)))

#endif // STD_MEMORY_MEMORY_H
