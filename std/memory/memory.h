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

// ==================== 联合域分配器（KMM V3 特色） ====================

extern void kaula_union_enter(void);
extern void kaula_union_exit(void);
extern void* kaula_union_alloc(size_t size);
extern void* kaula_union_elect(size_t size);
extern void kaula_union_set_deps(void* obj, void** deps, size_t count);
extern void kaula_union_auto_detect(void* obj);

// ==================== 便捷宏（KMM V4 风格） ====================

#define KMEM_ALLOC(size) kaula_scope_alloc(size)
#define KMEM_FREE(ptr) kaula_scope_free(ptr)
#define KMEM_ALLOC_TYPE(type) ((type*)kaula_scope_alloc(sizeof(type)))
#define KMEM_ALLOC_ARRAY(type, count) ((type*)kaula_scope_alloc(sizeof(type) * (count)))

// ==================== 联合域便捷宏 ====================

#define KMEM_UNION_ENTER() kaula_union_enter()
#define KMEM_UNION_EXIT() kaula_union_exit()
#define KMEM_UNION_ALLOC(size) kaula_union_alloc(size)
#define KMEM_UNION_ALLOC_TYPE(type) ((type*)kaula_union_alloc(sizeof(type)))
#define KMEM_UNION_ALLOC_ARRAY(type, count) ((type*)kaula_union_alloc(sizeof(type) * (count)))
#define KMEM_UNION_ELECT(type) ((type*)kaula_union_elect(sizeof(type)))
#define KMEM_UNION_SET_DEPS(obj, deps, count) kaula_union_set_deps(obj, deps, count)
#define KMEM_UNION_AUTO_DETECT(obj) kaula_union_auto_detect(obj)

// ==================== 作用域管理宏 ====================

#define KMEM_SCOPE_START() kaula_scope_enter()
#define KMEM_SCOPE_END() kaula_scope_exit()

#define KMEM_UNION_SCOPE_START() kaula_union_enter()
#define KMEM_UNION_SCOPE_END() kaula_union_exit()

#endif // STD_MEMORY_MEMORY_H
