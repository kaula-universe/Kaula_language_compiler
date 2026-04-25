#ifndef STD_MEMORY_MEMORY_H
#define STD_MEMORY_MEMORY_H

#include "../base/types.h"
#include "../../src/kmm_scoped_allocator_v4.h"

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

#if defined(__clang__) || defined(__GNUC__)
__thread extern kmm_context_t* g_kaula_scope;
#elif defined(_WIN32)
__declspec(thread) extern kmm_context_t* g_kaula_scope;
#else
extern kmm_context_t* g_kaula_scope;
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

// ==================== 联合域分配器（KMM V3 特色 - 全自动） ====================
// 自动管理：无需手动 enter/exit，使用宏即可
// 使用方式：
//   KMEM_UNION_SCOPE_START();
//       MyType* obj = KMEM_UNION_ELECT(MyType);
//       obj->field = value;
//   KMEM_UNION_SCOPE_END();  // 自动结束

#if KMM_ENABLE_UNION_DOMAIN
// 内部函数包装（由 kmm_scoped_allocator.c 实现，使用全局 scope 存储）
extern void kmm_union_auto_enter_fn(void);
extern void kmm_union_auto_exit_fn(void);
extern void* kmm_union_auto_alloc_fn(size_t size);

// 自动作用域宏（用户直接使用，基于全局 scope 存储）
// 使用 for 循环 RAII 模式：作用域结束时自动清理
#define KMEM_UNION_SCOPE_START() \
    for (int _kmm_u_done = 0; \
         !_kmm_u_done; \
         _kmm_u_done = 1, kmm_union_auto_exit_fn()) \
    if ((kmm_union_auto_enter_fn(), 1))

// 自动分配（类型安全 + 零初始化）
#define KMEM_UNION_ALLOC(type) \
    ((type*)kmm_union_auto_alloc_fn(sizeof(type)))

#define KMEM_UNION_ALLOC_ZERO(type) \
    ({ type* p = KMEM_UNION_ALLOC(type); \
       if(p) kmm_v4_zero_auto(p, sizeof(type)); \
       p; })

#define KMEM_UNION_ALLOC_ARRAY(type, count) \
    ((type*)kmm_union_auto_alloc_fn(sizeof(type) * (count)))

#define KMEM_UNION_ELECT(type) KMEM_UNION_ALLOC(type)
#define KMEM_UNION_SCOPE_END()   // for 循环自动结束
#else
// 联合域未启用时，退化为普通分配
#define KMEM_UNION_SCOPE_START()
#define KMEM_UNION_SCOPE_END()
#define KMEM_UNION_ALLOC(type) KMEM_ALLOC(type)
#define KMEM_UNION_ALLOC_ZERO(type) KMEM_ALLOC(type)
#define KMEM_UNION_ALLOC_ARRAY(type, count) KMEM_ALLOC_ARRAY(type, count)
#define KMEM_UNION_ELECT(type) KMEM_ALLOC(type)
#endif

// ==================== 便捷宏（KMM V4 风格） ====================
#define KMEM_ALLOC(size) kaula_scope_alloc(size)
#define KMEM_FREE(ptr) kaula_scope_free(ptr)
#define KMEM_ALLOC_TYPE(type) ((type*)kaula_scope_alloc(sizeof(type)))
#define KMEM_ALLOC_ARRAY(type, count) ((type*)kaula_scope_alloc(sizeof(type) * (count)))

// ==================== 作用域管理宏 ====================
#define KMEM_SCOPE_START() kaula_scope_enter()
#define KMEM_SCOPE_END() kaula_scope_exit()

#endif // STD_MEMORY_MEMORY_H
