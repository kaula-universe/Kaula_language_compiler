#include "memory.h"
#include <stdlib.h>
#include <string.h>
#include <stdio.h>

// ==================== ScopedAllocator 全局作用域 ====================
// 线程局部存储：每个线程/协程一个作用域上下文
#ifdef _WIN32
__declspec(thread) kmm_context_t* g_kaula_scope = NULL;
#else
__thread kmm_context_t* g_kaula_scope = NULL;
#endif

// 全局默认作用域
static kmm_context_t g_default_scope;
static int g_scope_initialized = 0;

/**
 * 进入新的作用域（函数调用时）
 * 编译器生成的代码将在每个函数入口处调用此函数
 */
void kaula_scope_enter(void) {
    // 如果还没有作用域，初始化默认作用域
    if (!g_kaula_scope) {
        if (kmm_init(&g_default_scope) == 0) {
            g_kaula_scope = &g_default_scope;
            g_scope_initialized = 1;
            #ifdef KMM_DEBUG
            printf("[KMM] 作用域初始化完成\n");
            #endif
        }
    }
}

/**
 * 退出作用域（函数返回时）
 * 编译器生成的代码将在每个函数出口处调用此函数
 */
void kaula_scope_exit(void) {
    if (g_kaula_scope) {
        #ifdef KMM_DEBUG
        printf("[KMM] 作用域清理开始\n");
        #endif
        
        // 销毁作用域，释放所有资源
        kmm_destroy(g_kaula_scope);
        g_kaula_scope = NULL;
    }
}

/**
 * Kaula 作用域分配函数
 * 这是编译器生成代码时调用的主要分配接口
 */
void* kaula_scope_alloc(size_t size) {
    if (!g_kaula_scope) {
        kaula_scope_enter();
    }
    
    void* ptr = kmm_alloc(g_kaula_scope, size, "<kaula>", 0);
    
    #ifdef KMM_DEBUG
    printf("[KMM] 分配 %zu bytes @ %p\n", size, ptr);
    #endif
    
    return ptr;
}

/**
 * Kaula 作用域释放函数
 * 注意：Arena 分配的对象不需要单独释放，会在作用域退出时批量释放
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
size_t memory_used() {
    if (global_allocator.base) {
        return global_allocator.offset;
    }
    return 0;
}

size_t memory_available() {
    if (global_allocator.base) {
        return MEMORY_POOL_SIZE - global_allocator.offset;
    }
    return 0;
}

size_t memory_total() {
    return MEMORY_POOL_SIZE;
}

// 内存安全检查
bool memory_is_valid(void* ptr) {
    if (global_allocator.base == NULL) {
        return false;
    }
    uint8_t* byte_ptr = (uint8_t*)ptr;
    return byte_ptr >= global_allocator.base && byte_ptr < global_allocator.base + MEMORY_POOL_SIZE;
}

bool memory_is_allocated(void* ptr) {
    if (!memory_is_valid(ptr)) {
        return false;
    }
    uint8_t* byte_ptr = (uint8_t*)ptr;
    return byte_ptr < global_allocator.base + global_allocator.offset;
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
