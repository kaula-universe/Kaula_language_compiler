#include "memory.h"
#include <stdlib.h>
#include <string.h>

// 使用src目录中的高性能内存分配器
// 这些函数在src/allocator.c中实现

// 内存池
static uint8_t* memory_pool = NULL;
static size_t memory_pool_size = 0;
static size_t memory_pool_used = 0;

// 标准内存分配函数
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
