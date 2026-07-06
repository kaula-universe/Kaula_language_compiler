#include "kaula.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

// ============================================================================
// FastAllocator — 已废弃，重定向到 KMM V4
// 所有 fast_alloc/fast_calloc/fast_free 调用现在委托给 kmm_v4 分配器，
// 消除旧实现的互斥锁开销和独立池的内存浪费。
// ============================================================================

FastAllocator global_allocator = {0};

#ifdef __GNUC__
__attribute__((constructor))
#endif
void fast_allocator_init() {
    // 初始化 KMM V4 池（如果尚未初始化）
    // fast_alloc 的调用方无需感知底层变化
    kmm_v4_init_pool(0);
    global_allocator.base = g_kmm_v4_pool;
    global_allocator.offset = 0;
}

void* fast_alloc(size_t size) {
    if (size == 0) return NULL;
    return kmm_v4_malloc(size);
}

void* fast_calloc(size_t num, size_t size) {
    if (num == 0 || size == 0) return NULL;
    if (size > SIZE_MAX / num) {
        fprintf(stderr, "Error: Integer overflow in fast_calloc\n");
        return NULL;
    }
    return kmm_v4_calloc(num, size);
}

void fast_free(void* ptr) {
    // KMM V4 使用作用域批量回收，单个 free 是 no-op
    (void)ptr;
}
