#include "memory.h"
#include "memory_pool.h"
#include "../../src/kmm_scoped_allocator_v2.c"  // 包含内联函数实现
#include <stdlib.h>
#include <string.h>
#include <stdio.h>

// ==================== KMM V2 全局作用域 ====================
#ifdef _WIN32
__declspec(thread) kmm_context_t* g_kaula_scope = NULL;
#else
__thread kmm_context_t* g_kaula_scope = NULL;
#endif

// 全局默认作用域
static kmm_context_t g_default_scope;
static int g_scope_initialized = 0;

// 内存池全局变量
static uint8_t* memory_pool = NULL;
static size_t memory_pool_size = 0;
static size_t memory_pool_used = 0;

/**
 * 进入新的作用域（函数调用时）- KMM V2 版本
 * 支持联合域和延迟初始化
 */
void kaula_scope_enter(void) {
    if (!g_kaula_scope) {
        if (kmm_init(&g_default_scope) == 0) {
            g_kaula_scope = &g_default_scope;
            g_scope_initialized = 1;
            #ifdef KMM_DEBUG
            printf("[KMM V2] 作用域初始化完成\n");
            #endif
        }
    } else {
        // 嵌套作用域：增加联合域深度
        #if KMM_ENABLE_UNION_DOMAIN
        g_union_domain.scope_depth++;
        #endif
    }
}

/**
 * 退出作用域（函数返回时）- KMM V2 版本
 * 处理联合域代表对象的生命周期提升
 */
void kaula_scope_exit(void) {
    if (g_kaula_scope) {
        #ifdef KMM_DEBUG
        printf("[KMM V2] 作用域清理开始\n");
        #endif
        
        // 处理联合域代表对象
        #if KMM_ENABLE_UNION_DOMAIN
        if (g_kaula_scope->union_rep) {
            kmm_union_promote(g_kaula_scope->union_rep);
        }
        
        if (g_union_domain.scope_depth > 0) {
            g_union_domain.scope_depth--;
        }
        #endif
        
        kmm_destroy(g_kaula_scope);
        g_kaula_scope = NULL;
    }
}

/**
 * Kaula 作用域分配函数 - KMM V2 版本
 * 支持联合域选举
 */
void* kaula_scope_alloc(size_t size) {
    if (!g_kaula_scope) {
        kaula_scope_enter();
    }
    
    void* ptr = kmm_alloc(g_kaula_scope, size, "<kaula>", 0);
    
    #ifdef KMM_DEBUG
    printf("[KMM V2] 分配 %zu bytes @ %p\n", size, ptr);
    #endif
    
    return ptr;
}

/**
 * Kaula 作用域释放函数
 */
void kaula_scope_free(void* ptr) {
    if (ptr) {
        kmm_free(ptr);
    }
}

// ==================== 标准内存分配函数 ====================
void* std_malloc(size_t size) {
    return malloc(size);
}

void* std_calloc(size_t num, size_t size) {
    return calloc(num, size);
}

void* std_realloc(void* ptr, size_t size) {
    return realloc(ptr, size);
}

void std_free(void* ptr) {
    free(ptr);
}

// 内存使用统计
size_t memory_used(void) {
    return memory_pool_used;
}

size_t memory_available(void) {
    return memory_pool_size - memory_pool_used;
}

size_t memory_total(void) {
    return memory_pool_size;
}

// 内存安全检查
bool memory_is_valid(void* ptr) {
    if (memory_pool == NULL) {
        return false;
    }
    uint8_t* byte_ptr = (uint8_t*)ptr;
    return byte_ptr >= memory_pool && byte_ptr < memory_pool + memory_pool_size;
}

bool memory_is_allocated(void* ptr) {
    if (!memory_is_valid(ptr)) {
        return false;
    }
    // 简单检查：如果指针在已使用范围内则认为已分配
    uint8_t* byte_ptr = (uint8_t*)ptr;
    return (size_t)(byte_ptr - memory_pool) < memory_pool_used;
}

// 内存操作函数
void memory_copy(void* dest, const void* src, size_t size) {
    memcpy(dest, src, size);
}

void memory_move(void* dest, const void* src, size_t size) {
    memmove(dest, src, size);
}

void memory_set(void* ptr, int value, size_t size) {
    memset(ptr, value, size);
}

int memory_compare(const void* ptr1, const void* ptr2, size_t size) {
    return memcmp(ptr1, ptr2, size);
}

// 内存对齐函数
void* memory_align(size_t alignment, size_t size) {
    void* ptr = NULL;
    #ifdef _WIN32
    ptr = _aligned_malloc(size, alignment);
    #else
    int err = posix_memalign(&ptr, alignment, size);
    if (err != 0) {
        return NULL;
    }
    #endif
    return ptr;
}

void memory_align_free(void* ptr) {
    #ifdef _WIN32
    _aligned_free(ptr);
    #else
    free(ptr);
    #endif
}

// 内存池管理
void memory_pool_init(size_t size) {
    if (memory_pool) {
        free(memory_pool);
    }
    memory_pool = (uint8_t*)malloc(size);
    memory_pool_size = size;
    memory_pool_used = 0;
}

void memory_pool_destroy() {
    if (memory_pool) {
        free(memory_pool);
        memory_pool = NULL;
        memory_pool_size = 0;
        memory_pool_used = 0;
    }
}

void* memory_pool_alloc(size_t size) {
    if (!memory_pool) {
        memory_pool_init(MEMORY_POOL_SIZE);
    }
    
    if (memory_pool_used + size > memory_pool_size) {
        return NULL; // 内存池已满
    }
    
    void* ptr = &memory_pool[memory_pool_used];
    memory_pool_used += size;
    return ptr;
}

void memory_pool_free(void* ptr) {
    // 内存池不支持单独释放，只能重置整个内存池
    (void)ptr; // 避免未使用参数警告
}

// 内存调试
void memory_dump_usage() {
    printf("Memory usage:\n");
    printf("  Total: %zu bytes\n", memory_total());
    printf("  Used: %zu bytes\n", memory_used());
    printf("  Available: %zu bytes\n", memory_available());
    printf("  Usage: %.2f%%\n", (float)memory_used() / memory_total() * 100);
}

void memory_check_leaks() {
    printf("Memory leak check: %zu bytes used\n", memory_used());
    if (memory_used() > 0) {
        printf("Warning: Possible memory leaks detected\n");
    } else {
        printf("No memory leaks detected\n");
    }
}
