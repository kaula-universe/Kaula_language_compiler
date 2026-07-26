/**
 * std/memory/memory.c - Kaula 标准库内存模块实现
 *
 * KMM V4 分配器的高级接口实现
 * 所有函数都映射到 kmm_scoped_allocator_v4.h 中的实现
 */

#include "memory.h"

/* 禁用头文件中的 static inline 版本，使用 .c 中的非 inline 版本 */
#define KMM_V4_RESET_IMPL
#define KMM_V4_USAGE_IMPL
#define KMM_V4_AVAILABLE_IMPL
#define KMM_V4_BUMP_IMPL
#define KMM_V4_OFFSET_SAVE_IMPL
#define KMM_V4_OFFSET_RESTORE_IMPL

#include "../../src/kmm_scoped_allocator_v4.h"
#include "../../src/kaula.h"
#include <stdlib.h>
#include <string.h>

/* 取消 scope_enter/exit 的宏定义，以便提供函数实现 */
#undef kmm_v4_scope_enter
#undef kmm_v4_scope_exit

/* ============================================================================
 * KMM V4 核心分配函数
 * ============================================================================ */

void* kmm_v4_alloc(size_t size) {
    return kmm_v4_alloc_auto(size);
}

/* 修复：kmm_v4_calloc/realloc/free/malloc/strdup 的实现已在
   kmm_scoped_allocator_v4.c 中提供，此处不再重复定义以避免链接时
   multiple definition 错误（Linux GCC 严格检查）。
   memory.h 中的 extern 声明会引用 kmm_scoped_allocator_v4.c 中的实现。 */

/* ============================================================================
 * 对齐分配
 * ============================================================================ */

void* kmm_v4_alloc_aligned(size_t size, size_t alignment) {
    if (alignment <= KMM_V4_ALIGNMENT) {
        return kmm_v4_alloc_auto(size);
    }
    size_t total = size + alignment;
    void* raw = kmm_v4_alloc_auto(total);
    if (raw == NULL) {
        return NULL;
    }
    uintptr_t addr = (uintptr_t)raw;
    uintptr_t aligned_addr = (addr + alignment - 1) & ~((uintptr_t)alignment - 1);
    return (void*)aligned_addr;
}

/* ============================================================================
 * 作用域管理
 * ============================================================================ */

void kmm_v4_scope_enter(void) {
    kmm_v4_scope_push();
}

void kmm_v4_scope_exit(void) {
    kmm_v4_scope_pop();
}

/* ============================================================================
 * 批量分配 API（编译器生成，减少原子操作次数）
 * ============================================================================ */

/* 修复：V4 使用 per-thread heap 替代旧 V3 的 tls_buffer，
   kmm_v4_bump 直接委托给 kmm_v4_alloc_auto（header 中的 inline 函数），
   per-thread heap 已实现单次 CAS 批量获取的效果 */
void* kmm_v4_bump(size_t total_size) {
    return kmm_v4_alloc_auto(total_size);
}

size_t kmm_v4_offset_save(void) {
#if KMM_THREAD_SAFETY_LEVEL >= 1
    return KMM_ATOMIC_LOAD(g_kmm_v4_offset);
#else
    return g_kmm_v4_offset;
#endif
}

void kmm_v4_offset_restore(size_t saved) {
#if KMM_THREAD_SAFETY_LEVEL >= 1
    KMM_ATOMIC_STORE(g_kmm_v4_offset, saved);
#else
    g_kmm_v4_offset = saved;
#endif
}

/* ============================================================================
 * 状态查询
 * ============================================================================ */

size_t kmm_v4_usage(void) {
#if KMM_THREAD_SAFETY_LEVEL >= 1
    return KMM_ATOMIC_LOAD(g_kmm_v4_offset);
#else
    return g_kmm_v4_offset;
#endif
}

size_t kmm_v4_available(void) {
#if KMM_THREAD_SAFETY_LEVEL >= 1
    return KMM_V4_POOL_SIZE - KMM_ATOMIC_LOAD(g_kmm_v4_offset);
#else
    return KMM_V4_POOL_SIZE - g_kmm_v4_offset;
#endif
}

size_t kmm_v4_capacity(void) {
    return KMM_V4_POOL_SIZE;
}

void kmm_v4_reset(void) {
#if KMM_THREAD_SAFETY_LEVEL >= 1
    KMM_ATOMIC_STORE(g_kmm_v4_offset, 0);
#else
    g_kmm_v4_offset = 0;
#endif
}

/* ============================================================================
 * 与 std_malloc 的对比 API
 * ============================================================================ */

void* std_malloc(size_t size) {
    return malloc(size);
}

void std_free(void* ptr) {
    free(ptr);
}

/* ============================================================================
 * SOR 所有权操作（编译器展开，这里提供链接符号）
 * ============================================================================ */

void* std_yield(void* src, void* dst) {
    if (src != NULL) {
        memcpy(dst, src, sizeof(void*));
        memset(src, 0, sizeof(void*));
    }
    return dst;
}

void* std_release(void* src) {
    return src;
}

/* ============================================================================
 * 快速分配器（兼容旧 API）
 * ============================================================================ */

extern void* fast_alloc(size_t size);
extern void* fast_calloc(size_t num, size_t size);
extern void fast_free(void* ptr);

/* ============================================================================
 * 作用域分配（兼容旧 API）
 * ============================================================================ */

void kaula_scope_enter(void) {
    kmm_v4_scope_push();
}

void kaula_scope_exit(void) {
    kmm_v4_scope_pop();
}

void* kaula_scope_alloc(size_t size) {
    return kmm_v4_alloc_auto(size);
}

void kaula_scope_free(void* ptr) {
    (void)ptr;
}
