#include "kaula.h"
#include <stdio.h>
#include <stdlib.h>

FastAllocator global_allocator = {0};

#ifdef __GNUC__
__attribute__((constructor))
#endif
void fast_allocator_init() {
    #ifdef _WIN32
    global_allocator.base = (uint8_t*)_aligned_malloc(MEMORY_POOL_SIZE, 64);
    #else
    global_allocator.base = (uint8_t*)aligned_alloc(64, MEMORY_POOL_SIZE);
    #endif
    
    if (!global_allocator.base) {
        fprintf(stderr, "Error: Failed to allocate memory\n");
        exit(1);
    }
    
    global_allocator.offset = 0;
}

void* fast_alloc(size_t size) {
    if (!global_allocator.base) {
        fast_allocator_init();
    }
    
    if (size == 0) {
        return NULL;
    }
    
    size = (size + 63) & ~63;
    
    if (global_allocator.offset + size > MEMORY_POOL_SIZE) {
        fprintf(stderr, "Error: Memory pool exhausted (requested %zu bytes)\n", size);
        exit(1);
    }
    
    void* ptr = global_allocator.base + global_allocator.offset;
    global_allocator.offset += size;
    return ptr;
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
