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
    // D3 修复：size + alignment 溢出检查
    if (size == 0 || alignment == 0) return NULL;
    if (size > SIZE_MAX - alignment) return NULL;
    // alignment 必须是 2 的幂（调用方契约），用对齐公式安全地计算偏移
    if ((alignment & (alignment - 1)) != 0) return NULL;
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
 * 幸存段（slab / 页式）分配
 * ============================================================================ */

/* 头文件在本 TU 只看到 kmm 头的 extern 声明（KMM_V4_EXTERNAL_DECLS 早于
 * kmm 头生效），因此这里导出可链接符号：委托 kmm 头的实现。生成的 C TU 若
 * 已展开 kmm 头，则由 memory.h 的 static inline 版本接管（热路径内联）。 */
void* kmm_v4_global_alloc(size_t size) {
    return kmm_v4_alloc_global(size);
}

bool kmm_v4_global_free(void* ptr) {
    return kmm_v4_survivor_free(ptr);
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
    // P1：与头文件 inline 版口径一致——主池 offset **加上** survivor 段用量。
    // 旧实现只返回 offset，漏掉 slab 桶与大对象区，导致 P1 诊断时
    // "峰值后是否回落"被低估（survivor 段的滞留完全不可见）。
#if KMM_THREAD_SAFETY_LEVEL >= 1
    return KMM_ATOMIC_LOAD(g_kmm_v4_offset) + kmm_v4_survivor_usage();
#else
    return g_kmm_v4_offset + kmm_v4_survivor_usage();
#endif
}

size_t kmm_v4_available(void) {
    // P1：与头文件 inline 版口径一致（多段感知）。旧实现用
    // KMM_V4_POOL_SIZE - offset，一旦 offset 越过首段容量（扩展段生效）
    // 就会 size_t 下溢，返回一个天文数字的"可用量"。
#if KMM_THREAD_SAFETY_LEVEL >= 1
    size_t _off = KMM_ATOMIC_LOAD(g_kmm_v4_offset);
    size_t _primary_left = (_off < g_kmm_v4_pool_capacity) ? (g_kmm_v4_pool_capacity - _off) : 0;
    size_t _extra_used = (_off > g_kmm_v4_pool_capacity) ? (_off - g_kmm_v4_pool_capacity) : 0;
    size_t _extra_cap = KMM_ATOMIC_LOAD(g_kmm_v4_extra_capacity);
    size_t _extra_left = (_extra_cap > _extra_used) ? (_extra_cap - _extra_used) : 0;
    return _primary_left + _extra_left + kmm_v4_survivor_available();
#else
    size_t _primary_left = (g_kmm_v4_offset < g_kmm_v4_pool_capacity) ? (g_kmm_v4_pool_capacity - g_kmm_v4_offset) : 0;
    size_t _extra_used = (g_kmm_v4_offset > g_kmm_v4_pool_capacity) ? (g_kmm_v4_offset - g_kmm_v4_pool_capacity) : 0;
    size_t _extra_left = (g_kmm_v4_extra_capacity > _extra_used) ? (g_kmm_v4_extra_capacity - _extra_used) : 0;
    return _primary_left + _extra_left + kmm_v4_survivor_available();
#endif
}

size_t kmm_v4_capacity(void) {
    // 多段扩容后真实容量 = 首段 + 已安装扩展段；宏 KMM_V4_POOL_SIZE 只是首段。
    return g_kmm_v4_pool_capacity + g_kmm_v4_extra_capacity;
}

void kmm_v4_reset(void) {
    // P1 · 方案 3：转调单一真相源 kmm_v4_reset_full()，补齐原先漏掉的
    // survivor 段清空 / thread heap 失效 / 扩展段 trim_tail。旧实现只写
    // offset=0，用户显式调用后扩展段与 survivor 仍滞留 → RSS 不回落。
    kmm_v4_reset_full();
}

// 注意：kmm_v4_idle_reclaim 的实现在 src/kmm_scoped_allocator_v4.c 中
// （无 KMM_V4_*_IMPL 宏，热路径零改动）。此处只经 memory.h 声明导出给
// Kaula 代码，不可再定义同名函数——会导致 multiple definition 或自递归。

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
