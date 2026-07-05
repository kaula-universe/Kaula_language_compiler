#include "memory.h"
#include <string.h>
#include <stdio.h>

// 所有核心函数由 kmm_scoped_allocator_v4.c 提供（VirtualAlloc 虚拟内存池）
// 此文件仅提供 std 模块的包装接口

void* std_malloc(size_t size) {
    return kmm_v4_malloc(size);
}

void std_free(void* ptr) {
    (void)ptr;
}

void* std_yeide(void* source, void* target) {
    if (source && target) {
        memcpy(target, source, sizeof(void*));
        memset(source, 0, sizeof(void*));
    }
    return target;
}

void* std_release(void* ptr) {
    return ptr;
}

// 直接 KMM V4 分配 — 作用域退出时自动释放
void* kmm_v4_alloc(size_t size) {
    return kmm_v4_malloc(size);
}
