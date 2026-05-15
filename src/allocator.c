#include "kaula.h"
#include <stdio.h>
#include <stdlib.h>

FastAllocator global_allocator = {0};

#ifdef _WIN32
#include <windows.h>
static SRWLOCK g_allocator_lock = SRWLOCK_INIT;
#else
#include <pthread.h>
static pthread_mutex_t g_allocator_lock = PTHREAD_MUTEX_INITIALIZER;
#endif

#ifdef __GNUC__
__attribute__((constructor))
#endif
void fast_allocator_init() {
#ifdef _WIN32
    AcquireSRWLockExclusive(&g_allocator_lock);
    if (global_allocator.base) {
        ReleaseSRWLockExclusive(&g_allocator_lock);
        return;
    }
    global_allocator.base = (uint8_t*)_aligned_malloc(MEMORY_POOL_SIZE, 64);
    if (!global_allocator.base) {
        fprintf(stderr, "Error: Failed to allocate memory\n");
        exit(1);
    }
    global_allocator.offset = 0;
    ReleaseSRWLockExclusive(&g_allocator_lock);
#else
    pthread_mutex_lock(&g_allocator_lock);
    if (global_allocator.base) {
        pthread_mutex_unlock(&g_allocator_lock);
        return;
    }
    global_allocator.base = (uint8_t*)aligned_alloc(64, MEMORY_POOL_SIZE);
    if (!global_allocator.base) {
        fprintf(stderr, "Error: Failed to allocate memory\n");
        exit(1);
    }
    global_allocator.offset = 0;
    pthread_mutex_unlock(&g_allocator_lock);
#endif
}

void* fast_alloc(size_t size) {
    if (!global_allocator.base) {
        fast_allocator_init();
    }

    if (size == 0) {
        return NULL;
    }

    size = (size + 63) & ~63;

#ifdef _WIN32
    AcquireSRWLockExclusive(&g_allocator_lock);
    if (global_allocator.offset + size > MEMORY_POOL_SIZE) {
        ReleaseSRWLockExclusive(&g_allocator_lock);
        fprintf(stderr, "Error: Memory pool exhausted (requested %zu bytes)\n", size);
        exit(1);
    }
    void* ptr = global_allocator.base + global_allocator.offset;
    global_allocator.offset += size;
    ReleaseSRWLockExclusive(&g_allocator_lock);
    return ptr;
#else
    pthread_mutex_lock(&g_allocator_lock);
    if (global_allocator.offset + size > MEMORY_POOL_SIZE) {
        pthread_mutex_unlock(&g_allocator_lock);
        fprintf(stderr, "Error: Memory pool exhausted (requested %zu bytes)\n", size);
        exit(1);
    }
    void* ptr = global_allocator.base + global_allocator.offset;
    global_allocator.offset += size;
    pthread_mutex_unlock(&g_allocator_lock);
    return ptr;
#endif
}

void* fast_calloc(size_t num, size_t size) {
    if (num == 0 || size == 0) {
        return NULL;
    }
    
    if (size > SIZE_MAX / num) {
        fprintf(stderr, "Error: Integer overflow in fast_calloc\n");
        return NULL;
    }
    
    size_t total = num * size;
    void* ptr = fast_alloc(total);
    if (ptr) {
        memset(ptr, 0, total);
    }
    return ptr;
}

void fast_free(void* ptr) {

    if (!ptr) return;
    
    uint8_t* ptr_u8 = (uint8_t*)ptr;
    if (ptr_u8 >= global_allocator.base && ptr_u8 < global_allocator.base + MEMORY_POOL_SIZE) {
        return;
    }
    
    #ifdef _WIN32
    _aligned_free(ptr);
    #else
    free(ptr);
    #endif
}
