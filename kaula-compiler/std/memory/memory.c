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

/* ============================================================================
 * KMM V4 核心分配函数
 * ============================================================================ */

void* kmm_v4_alloc(size_t size) {
    return kmm_v4_alloc_auto(size);
}

void* kmm_v4_calloc(size_t count, size_t size) {
    size_t total = count * size;
    void* ptr = kmm_v4_alloc_auto(total);
    if (ptr != NULL) {
        kmm_v4_zero_auto(ptr, total);
    }
    return ptr;
}

void* kmm_v4_realloc(void* ptr, size_t new_size) {
    /* Bump allocator 不支持真正的 realloc */
    /* 如果指针是池内最后分配的，可以原地扩展 */
    if (ptr == NULL) {
        return kmm_v4_alloc_auto(new_size);
    }

    /* 分配新内存并复制 */
    void* new_ptr = kmm_v4_alloc_auto(new_size);
    if (new_ptr != NULL && ptr != NULL) {
        /* 无法知道原大小，复制 new_size 字节 */
        memcpy(new_ptr, ptr, new_size);
    }
    return new_ptr;
}

void kmm_v4_free(void* ptr) {
    /* Bump allocator: 空操作，内存通过作用域退出批量回收 */
    (void)ptr;
}

void* kmm_v4_malloc(size_t size) {
    /* kmm_v4_malloc 是 kmm_v4_alloc 的别名，提供 API 兼容性 */
    return kmm_v4_alloc_auto(size);
}

/* ============================================================================
 * 对齐分配
 * ============================================================================ */

void* kmm_v4_alloc_aligned(size_t size, size_t alignment) {
    /* 通过 kmm_v4_alloc_auto 分配，它已经是 8 字节对齐 */
    /* 对于更大的对齐要求，需要填充 */
    if (alignment <= KMM_V4_ALIGNMENT) {
        return kmm_v4_alloc_auto(size);
    }

    /* 分配额外空间用于对齐 */
    size_t total = size + alignment;
    void* raw = kmm_v4_alloc_auto(total);
    if (raw == NULL) {
        return NULL;
    }

    /* 对齐指针 */
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

void* kmm_v4_bump(size_t total_size) {
    return kmm_v4_bump_inline(total_size);
}

size_t kmm_v4_offset_save(void) {
    return kmm_v4_offset_save_inline();
}

void kmm_v4_offset_restore(size_t saved) {
    kmm_v4_offset_restore_inline(saved);
}

/* ============================================================================
 * 状态查询
 * ============================================================================ */

/* 注意：kmm_v4_usage、kmm_v4_available、kmm_v4_reset 已在 kmm_scoped_allocator_v4.h 中定义为 static inline，
 * 但通过定义 KMM_V4_*_IMPL 宏禁用了 inline 版本，此处提供非 inline 版本用于跨 TU 链接。 */

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

void* std_yeide(void* src, void* dst) {
    /* 编译器在释放模式下展开为: dst = src; src = NULL; */
    /* 这里提供符号以支持链接 */
    if (src != NULL) {
        memcpy(dst, src, sizeof(void*));
        memset(src, 0, sizeof(void*));
    }
    return dst;
}

void* std_release(void* src) {
    /* 编译器在释放模式下展开为零开销操作 */
    /* 这里提供符号以支持链接 */
    return src;
}

/* ============================================================================
 * 快速分配器（兼容旧 API）
 * 这些函数在 src/allocator.c 中已有实现，此处不重复定义
 * fast_alloc, fast_calloc, fast_free 由 src/allocator.c 提供
 * ============================================================================ */

/* 声明外部函数（由 src/allocator.c 提供） */
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