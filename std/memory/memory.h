#pragma once
#include <stdint.h>
#include <stdbool.h>
#include <stdlib.h>

// KMM V4 pool size (used by compiler to pass via -D flag)
#ifndef KMM_V4_POOL_SIZE
#define KMM_V4_POOL_SIZE (256 * 1024 * 1024)  // 256MB virtual address space
#endif

#define KMM_V4_ALIGNMENT 8
#define KMM_V4_MAX_SCOPE_DEPTH 64

// 核心分配函数（由 kmm_scoped_allocator_v4.c 实现，使用 VirtualAlloc）
void* kmm_v4_alloc_auto(size_t size);
void* kmm_v4_malloc(size_t size);
void  kmm_v4_free(void* ptr);
void* kmm_v4_calloc(size_t count, size_t size);
void* kmm_v4_realloc(void* ptr, size_t size);
void* kmm_v4_strdup(const char* s);

// 作用域管理
void kmm_v4_scope_push(void);
void kmm_v4_scope_pop(void);

// 初始化/销毁
void kmm_v4_init_pool(size_t initial_size);
void kmm_v4_destroy_pool(void);
size_t kmm_v4_usage(void);
size_t kmm_v4_capacity(void);

// Std wrappers
void* std_malloc(size_t size);
void  std_free(void* ptr);
void* std_release(void* ptr);
void* std_yeide(void* source, void* target);

// 直接 KMM V4 分配 — 作用域退出时自动释放，无需手动 free
void* kmm_v4_alloc(size_t size);
