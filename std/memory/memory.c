#include "memory.h"
#include <stdlib.h>
#include <string.h>

// ==================== 全局作用域 ====================

#ifdef _WIN32
__declspec(thread) kmm_context_t* g_kaula_scope = NULL;
#else
__thread kmm_context_t* g_kaula_scope = NULL;
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

// ==================== 联合域实现 ====================

void kaula_union_enter(void) {
    // 进入联合域作用域
    if (!g_kaula_scope) {
        kaula_scope_enter();
    }
#if KMM_ENABLE_UNION_DOMAIN
    g_union_domain.scope_depth++;
#endif
}

void kaula_union_exit(void) {
    // 退出联合域作用域，执行拓扑排序和清理
#if KMM_ENABLE_UNION_DOMAIN
    if (g_union_domain.scope_depth > 0) {
        g_union_domain.scope_depth--;
        
        // 执行联合域对象的拓扑排序和清理
        if (g_union_domain.scope_depth == 0) {
            kmm_union_destroy(&g_union_domain);
        }
    }
#endif
    
    // 如果没有外层作用域，清理全局作用域
    if (!g_kaula_scope || g_kaula_scope == &g_default_scope) {
        kaula_scope_exit();
    }
}

void* kaula_union_alloc(size_t size) {
    // 在联合域中分配对象
    if (!g_kaula_scope) {
        kaula_scope_enter();
    }
    
#if KMM_ENABLE_UNION_DOMAIN
    return kmm_alloc(g_kaula_scope, size, "<kaula_union>", 0);
#else
    return kaula_scope_alloc(size);
#endif
}

void* kaula_union_elect(size_t size) {
    // 选举联合域对象（自动管理生命周期）
    if (!g_kaula_scope) {
        kaula_scope_enter();
    }
    
#if KMM_ENABLE_UNION_DOMAIN
    return kmm_union_elect(g_kaula_scope, size, "<kaula_union_elect>", 0);
#else
    return kaula_scope_alloc(size);
#endif
}

void kaula_union_set_deps(void* obj, void** deps, size_t count) {
    // 设置联合域对象的依赖关系
#if KMM_ENABLE_UNION_DOMAIN
    kmm_union_set_dependencies(obj, deps, count);
#else
    (void)obj;
    (void)deps;
    (void)count;
#endif
}

void kaula_union_auto_detect(void* obj) {
    // 自动检测联合域对象的依赖关系
#if KMM_ENABLE_UNION_DOMAIN
    kmm_union_node_t* node = kmm_find_node_by_pointer(obj);
    if (node) {
        kmm_union_auto_detect_dependencies(node);
    }
#else
    (void)obj;
#endif
}
