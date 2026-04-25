#include "memory.h"
#include "../../src/kmm_scoped_allocator.c"
#include <stdlib.h>
#include <string.h>

// ==================== 全局作用域 ====================

#if defined(__clang__) || defined(__GNUC__)
__thread kmm_context_t* g_kaula_scope = NULL;
#elif defined(_WIN32)
__declspec(thread) kmm_context_t* g_kaula_scope = NULL;
#else
kmm_context_t* g_kaula_scope = NULL;
#endif

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

// ==================== 快速分配器实现 ====================

void* fast_alloc(size_t size) {
    return kmm_v4_malloc(size);
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
