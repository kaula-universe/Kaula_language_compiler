#include "memory.h"
#include "../../src/kmm_scoped_allocator.c"
#include <stdlib.h>
#include <string.h>

// g_kaula_scope is already defined in kmm_scoped_allocator.c (line 27)
// No need to define it here again

static kmm_context_t g_default_scope;

// ==================== KMM 作用域分配器实现 ====================

void kaula_scope_enter(void) {
    if (!g_kaula_scope) {
        kmm_init(&g_default_scope);
        g_kaula_scope = &g_default_scope;
    }
}

void kaula_scope_exit(void) {
    if (g_kaula_scope) {
        kmm_destroy(g_kaula_scope);
        g_kaula_scope = NULL;
    }
}

void* kaula_scope_alloc(size_t size) {
    if (!g_kaula_scope) {
        kaula_scope_enter();
    }
    return kmm_alloc(g_kaula_scope, size, "<kaula>", 0);
}

void kaula_scope_free(void* ptr) {
    kmm_free(ptr);
    // KMM V4 池内对象自动管理，无需手动释放
}

// ==================== KMM 包装函数供标准库使用 ====================
// 这些函数已在 kmm_scoped_allocator_v4.h 中定义为 static inline
// kmm_v4_malloc - 已存在
// kmm_v4_free - 已存在
// 只需要在这里实现 calloc 和 realloc

void* kmm_v4_calloc(size_t num, size_t size) {
    size_t total = num * size;
    void* ptr = kmm_v4_malloc(total);
    if (ptr) {
        kmm_v4_zero_auto(ptr, total);
    }
    return ptr;
}

void* kmm_v4_realloc(void* ptr, size_t new_size) {
    // KMM 池不支持 realloc，分配新内存并复制
    if (!ptr) return kmm_v4_malloc(new_size);
    void* new_ptr = kmm_v4_malloc(new_size);
    if (new_ptr && ptr) {
        // 无法知道旧内存块大小，保守拷贝
        memcpy(new_ptr, ptr, new_size);
    }
    // 池内对象不释放
    return new_ptr;
}

char* kmm_v4_strdup(const char* str) {
    if (!str) return NULL;
    size_t len = strlen(str);
    char* result = (char*)kmm_v4_malloc(len + 1);
    if (result) {
        memcpy(result, str, len + 1);
    }
    return result;
}

// ==================== 快速分配器实现 ====================

void* fast_alloc(size_t size) {
    return kmm_v4_alloc_auto(size);
}

void* fast_calloc(size_t num, size_t size) {
    size_t total = num * size;
    void* ptr = kmm_v4_malloc(total);
    if (ptr) {
        kmm_v4_zero_auto(ptr, total);
    }
    return ptr;
}

void fast_free(void* ptr) {
    kmm_v4_free(ptr);
}

// ==================== 标准分配器实现 ====================

void* std_malloc(size_t size) {
    return malloc(size);
}

void std_free(void* ptr) {
    free(ptr);
}
