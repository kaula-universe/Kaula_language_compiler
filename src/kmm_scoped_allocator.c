#ifndef KMM_SCOPED_ALLOCATOR_IMPL_H
#define KMM_SCOPED_ALLOCATOR_IMPL_H

// 定义全局内存池（所有翻译单元共享）
// 线程安全：使用原子偏移量实现无锁分配（轻量实时）
// 确保 KMM_V4_DEFINE_GLOBALS 被定义，以便定义全局变量
#define KMM_V4_DEFINE_GLOBALS
#ifdef KMM_V4_DEFINE_GLOBALS
    #define KMM_V4_GLOBALS
    #include "kmm_scoped_allocator_v4.h"
    
    __attribute__((aligned(KMM_V4_ALIGNMENT)))
    uint8_t g_kmm_v4_pool[KMM_V4_POOL_SIZE];
    
#if KMM_THREAD_SAFETY_LEVEL >= 1
    KMM_ATOMIC_TYPE g_kmm_v4_offset = 0;  // 原子操作，无锁CAS
#else
    size_t g_kmm_v4_offset = 0;
#endif
    
    #ifdef KMM_V4_DEBUG
    size_t g_kmm_v4_peak = 0;
    size_t g_kmm_v4_alloc_count = 0;
    #endif
    
    // Kaula scope pointer (required by kmm_union_auto_alloc_fn)
    __thread kmm_context_t* g_kaula_scope = NULL;
    
#else
    #include "kmm_scoped_allocator_v4.h"
    
    // Kaula scope pointer (required by kmm_union_auto_alloc_fn)
    // Define if not already defined by the header
    #ifndef KMM_V4_GLOBALS
    __thread kmm_context_t* g_kaula_scope = NULL;
    #endif
#endif
#include <stdio.h>
#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
#include <stdlib.h>

#ifdef _WIN32
#include <windows.h>
#ifndef CLOCK_MONOTONIC
#define CLOCK_MONOTONIC 0
#endif

#ifndef _TIMESPEC_DEFINED
struct timespec {
    long tv_sec;
    long tv_nsec;
};
#endif

#ifndef _POSIX_TIMERS
static inline int clock_gettime(int clk_id, struct timespec* ts) {
    static LARGE_INTEGER frequency;
    static int initialized = 0;
    LARGE_INTEGER counter;
    if (!initialized) {
        QueryPerformanceFrequency(&frequency);
        initialized = 1;
    }
    QueryPerformanceCounter(&counter);
    ts->tv_sec = counter.QuadPart / frequency.QuadPart;
    ts->tv_nsec = ((counter.QuadPart % frequency.QuadPart) * 1000000000) / frequency.QuadPart;
    (void)clk_id;
    return 0;
}
#pragma warning(suppress: 4996)
#endif
#endif

// ==================== 智能配置（v4 自动化特性） ====================
// 自动检测是否需要启用高级功能
#ifndef KMM_ENABLE_ARENA
#define KMM_ENABLE_ARENA 1
#endif

#ifndef KMM_ENABLE_THREAD_CACHE
#define KMM_ENABLE_THREAD_CACHE 1
#endif

#ifndef KMM_ENABLE_SAFE_ALLOC
#define KMM_ENABLE_SAFE_ALLOC 1
#endif

#ifndef KMM_ENABLE_CLEANUP_STACK
#define KMM_ENABLE_CLEANUP_STACK 1
#endif

#ifndef KMM_ENABLE_UNION_DOMAIN
#define KMM_ENABLE_UNION_DOMAIN 1  // 联合域功能
#endif

// 联合域配置
#ifndef KMM_MAX_UNION_DEPTH
#define KMM_MAX_UNION_DEPTH 64
#endif

#ifndef KMM_MAX_DEPENDENCIES
#define KMM_MAX_DEPENDENCIES 32
#endif

#ifndef KMM_MAX_UNION_NODES
#define KMM_MAX_UNION_NODES 128
#endif

// 智能配置参数（根据平台自动调整）
// 注意：KMM_CACHE_LINE_SIZE 和 KMM_THREAD_CACHE_SIZE 已在 kmm_scoped_allocator_v4.h 中定义

// Arena 配置（v3 特色，v4 风格智能默认值）
#ifndef KMM_ARENA_TINY_MIN
#define KMM_ARENA_TINY_MIN        (64 * 1024)
#endif
#ifndef KMM_ARENA_TINY_MAX
#define KMM_ARENA_TINY_MAX        (256 * 1024)
#endif
#ifndef KMM_ARENA_SMALL_MIN
#define KMM_ARENA_SMALL_MIN       (512 * 1024)
#endif
#ifndef KMM_ARENA_SMALL_MAX
#define KMM_ARENA_SMALL_MAX       (4 * 1024 * 1024)
#endif
#ifndef KMM_ARENA_MEDIUM_MIN
#define KMM_ARENA_MEDIUM_MIN      (2 * 1024 * 1024)
#endif
#ifndef KMM_ARENA_MEDIUM_MAX
#define KMM_ARENA_MEDIUM_MAX      (16 * 1024 * 1024)
#endif

// 安全分配配置
#ifndef KMM_REDZONE_SIZE
#define KMM_REDZONE_SIZE          8
#endif
#ifndef KMM_REDZONE_PATTERN
#define KMM_REDZONE_PATTERN       0xCD
#endif
#ifndef KMM_CANARY_VALUE
#define KMM_CANARY_VALUE          0xDEADBEEFCAFEBABEULL
#endif

// ==================== 数据结构定义 ====================
// 注意：所有结构体定义已在 kmm_scoped_allocator_v4.h 中提供

// 安全分配头（新增）
typedef struct {
    size_t user_size;
    const char* file;
    int line;
    uint64_t canary;
} kmm_safe_header_t;

// ==================== KMM 上下文结构（使用头文件中的定义）====================
// kmm_context_t 已在 kmm_scoped_allocator_v4.h 中定义

// 全局上下文（用于统计）
kmm_context_t g_kmm_ctx = {0};

// ==================== 联合域全局变量（V3 特色，线程安全优化） ====================
// 轻量实时模式：联合域使用TLS隔离，每个线程独立管理
#if KMM_ENABLE_UNION_DOMAIN
#if KMM_THREAD_SAFETY_LEVEL >= 1
KMM_TLS kmm_union_domain_t g_union_domain = {0};
KMM_TLS kmm_union_node_t g_union_node_pool[KMM_MAX_UNION_NODES];
KMM_TLS size_t g_union_node_free_list = 0;
KMM_TLS bool g_union_pool_initialized = false;
#else
static kmm_union_domain_t g_union_domain = {0};
static kmm_union_node_t g_union_node_pool[KMM_MAX_UNION_NODES];
static size_t g_union_node_free_list = 0;
static bool g_union_pool_initialized = false;
#endif

// 拓扑排序缓冲区（TLS隔离）
KMM_TLS kmm_union_node_t* g_union_sort_buffer[1024];

// 前向声明
static inline kmm_union_node_t* kmm_union_node_alloc(void);
static inline void kmm_union_node_free(kmm_union_node_t* node);
static inline void kmm_union_auto_detect_dependencies(kmm_union_node_t* node);
static inline bool kmm_union_detect_cycle(kmm_union_node_t* node);
static inline void kmm_union_promote(kmm_union_node_t* node);
static inline void kmm_union_destroy(kmm_union_domain_t* domain);
#endif

// ==================== 公共 API 前向声明 ====================
int kmm_init(kmm_context_t* ctx);
void kmm_destroy(kmm_context_t* ctx);
void* kmm_alloc(kmm_context_t* ctx, size_t size, const char* file, int line);
void kmm_free(void* ptr);
void** kmm_alloc_batch(kmm_context_t* ctx, size_t size, size_t count, const char* file, int line);
void kmm_reset(kmm_context_t* ctx);
void kmm_print_pool_stats(void);

#if KMM_ENABLE_UNION_DOMAIN
void* kmm_union_elect(kmm_context_t* ctx, size_t size, const char* file, int line);
void kmm_union_set_dependencies(void* obj, void** deps, size_t count);
#endif

// ==================== 线程缓存实现（v4 自动化风格，完全TLS隔离） ====================
#if KMM_ENABLE_THREAD_CACHE
KMM_TLS kmm_thread_cache_t tls_kmm_thread_cache;

static inline void kmm_thread_cache_init(void) {
    if (KMM_V4_LIKELY(tls_kmm_thread_cache.cache_size == 0)) {
        memset(&tls_kmm_thread_cache, 0, sizeof(tls_kmm_thread_cache));
    }
}

static inline void* kmm_thread_cache_alloc(size_t size) {
    (void)size;
    // TLS完全隔离，无锁访问
    if (KMM_V4_LIKELY(tls_kmm_thread_cache.cache_size > 0)) {
        return tls_kmm_thread_cache.cache[--tls_kmm_thread_cache.cache_size];
    }
    return NULL;
}

static inline void kmm_thread_cache_free(void* ptr) {
    // TLS完全隔离，无锁访问
    if (KMM_V4_LIKELY(tls_kmm_thread_cache.cache_size < KMM_THREAD_CACHE_SIZE)) {
        tls_kmm_thread_cache.cache[tls_kmm_thread_cache.cache_size++] = ptr;
    }
}
#endif

// ==================== Arena 管理（v3 功能，v4 风格） ====================
#if KMM_ENABLE_ARENA

static inline int kmm_arena_ensure_initialized(kmm_arena_t* arena, size_t min_size, size_t max_size) {
    if (KMM_V4_LIKELY(arena->is_initialized)) {
        return 0;
    }
    
    size_t initial_capacity = (min_size + 4095) & ~4095;
    if (initial_capacity > max_size) {
        initial_capacity = max_size;
    }
    
    arena->buffer = (uint8_t*)kmm_v4_malloc(initial_capacity);
    if (!arena->buffer) return -1;
    
    arena->capacity = initial_capacity;
    arena->max_capacity = max_size;
    arena->offset = 0;
    arena->is_initialized = true;
    arena->allocations = 0;
    arena->peak = 0;
    arena->reset_count = 0;
    
    return 0;
}

static inline int kmm_arena_expand(kmm_arena_t* arena, size_t additional_size) {
    size_t new_capacity = arena->capacity * 2;
    
    while (new_capacity < arena->offset + additional_size) {
        new_capacity *= 2;
    }
    
    if (new_capacity > arena->max_capacity) {
        new_capacity = arena->max_capacity;
    }
    
    if (arena->offset + additional_size > new_capacity) {
        return -1;
    }
    
    uint8_t* new_buffer = (uint8_t*)kmm_v4_malloc(new_capacity);
    if (!new_buffer) return -1;
    
    if (arena->buffer && arena->offset > 0) {
        memcpy(new_buffer, arena->buffer, arena->offset);
    }
    
    if (arena->buffer) {
        kmm_v4_free(arena->buffer);
    }
    
    arena->buffer = new_buffer;
    arena->capacity = new_capacity;
    
    return 0;
}

static inline void* kmm_arena_alloc(kmm_arena_t* arena, size_t size, size_t min_capacity, size_t max_capacity) {
    (void)min_capacity;
    (void)max_capacity;
    
    size_t aligned_size = (size + 7) & ~7;
    size_t new_offset = arena->offset + aligned_size;
    
    if (KMM_V4_LIKELY(new_offset <= arena->capacity)) {
        void* ptr = arena->buffer + arena->offset;
        arena->offset = new_offset;
        arena->allocations++;
        arena->peak = (new_offset > arena->peak) ? new_offset : arena->peak;
        return ptr;
    }
    
    if (kmm_arena_expand(arena, aligned_size) != 0) {
        return NULL;
    }
    
    void* ptr = arena->buffer + arena->offset;
    arena->offset += aligned_size;
    arena->allocations++;
    arena->peak = (arena->offset > arena->peak) ? arena->offset : arena->peak;
    return ptr;
}

static inline void* kmm_arena_alloc_tiny(kmm_arena_t* arena, size_t size) {
    if (KMM_V4_LIKELY(arena->is_initialized)) {
        size_t new_offset = arena->offset + size;
        
        if (KMM_V4_LIKELY(new_offset <= arena->capacity)) {
            void* ptr = arena->buffer + arena->offset;
            arena->offset = new_offset;
            arena->allocations++;
            
            if (new_offset > arena->peak) {
                arena->peak = new_offset;
            }
            
            return ptr;
        }
    } else {
        if (kmm_arena_ensure_initialized(arena, KMM_ARENA_TINY_MIN, KMM_ARENA_TINY_MAX) != 0) {
            return NULL;
        }
    }
    
    if (kmm_arena_expand(arena, size) != 0) {
        return NULL;
    }
    
    void* ptr = arena->buffer + arena->offset;
    arena->offset += size;
    arena->allocations++;
    
    return ptr;
}

#endif

// ==================== 安全分配器（v3 特色，v4 风格） ====================
#if KMM_ENABLE_SAFE_ALLOC

static inline size_t kmm_safe_block_total_size(size_t user_size) {
    return sizeof(kmm_safe_header_t) + KMM_REDZONE_SIZE + user_size + KMM_REDZONE_SIZE;
}

static inline kmm_safe_header_t* kmm_get_header_from_user(void* user_ptr) {
    uint8_t* raw = (uint8_t*)user_ptr - KMM_REDZONE_SIZE - sizeof(kmm_safe_header_t);
    return (kmm_safe_header_t*)raw;
}

static inline bool kmm_check_redzone(void* user_ptr) {
    kmm_safe_header_t* hdr = kmm_get_header_from_user(user_ptr);
    
    if (KMM_V4_UNLIKELY(hdr->canary != KMM_CANARY_VALUE)) {
        return false;
    }
    
    uint8_t* raw = (uint8_t*)hdr;
    uint8_t* front_redzone = raw + sizeof(kmm_safe_header_t);
    for (int i = 0; i < KMM_REDZONE_SIZE; i++) {
        if (KMM_V4_UNLIKELY(front_redzone[i] != KMM_REDZONE_PATTERN)) {
            return false;
        }
    }
    
    uint8_t* user_mem = (uint8_t*)user_ptr;
    uint8_t* back_redzone = user_mem + hdr->user_size;
    for (int i = 0; i < KMM_REDZONE_SIZE; i++) {
        if (KMM_V4_UNLIKELY(back_redzone[i] != KMM_REDZONE_PATTERN)) {
            return false;
        }
    }
    
    return true;
}

static inline void* kmm_safe_malloc(size_t size, const char* file, int line) {
    if (KMM_V4_UNLIKELY(size == 0)) return NULL;
    
    size_t aligned_size = (size + 7) & ~7;
    size_t total = kmm_safe_block_total_size(aligned_size);
    
    uint8_t* raw = (uint8_t*)kmm_v4_malloc(total);
    if (KMM_V4_UNLIKELY(!raw)) return NULL;
    
    kmm_safe_header_t* hdr = (kmm_safe_header_t*)raw;
    hdr->user_size = aligned_size;
    hdr->file = file;
    hdr->line = line;
    hdr->canary = KMM_CANARY_VALUE;
    
    uint8_t* front_redzone = raw + sizeof(kmm_safe_header_t);
    memset(front_redzone, KMM_REDZONE_PATTERN, KMM_REDZONE_SIZE);
    
    uint8_t* user_ptr = front_redzone + KMM_REDZONE_SIZE;
    uint8_t* back_redzone = user_ptr + aligned_size;
    memset(back_redzone, KMM_REDZONE_PATTERN, KMM_REDZONE_SIZE);
    
    kmm_v4_zero_auto(user_ptr, aligned_size);
    
    return user_ptr;
}

static inline void kmm_safe_free(void* user_ptr) {
    if (KMM_V4_UNLIKELY(!user_ptr)) return;
    
    if (KMM_V4_UNLIKELY(!kmm_check_redzone(user_ptr))) {
        kmm_safe_header_t* hdr = kmm_get_header_from_user(user_ptr);
        fprintf(stderr, "Memory corruption detected! File: %s, Line: %d\n", hdr->file, hdr->line);
        return;
    }
    
    kmm_safe_header_t* hdr = kmm_get_header_from_user(user_ptr);
    kmm_v4_free(hdr);
}

#endif

// ==================== 清理栈管理（v3 特色，v4 风格，TLS隔离） ====================
#if KMM_ENABLE_CLEANUP_STACK

#define KMM_MAX_CLEANUP_NODES 256
// TLS隔离：每个线程独立清理栈
KMM_TLS kmm_cleanup_node_t g_cleanup_node_pool[KMM_MAX_CLEANUP_NODES];
KMM_TLS size_t g_cleanup_node_free_list = 0;
KMM_TLS bool g_cleanup_initialized = false;

static inline void kmm_init_cleanup_pool(void) {
    if (!g_cleanup_initialized) {
        for (size_t i = 0; i < KMM_MAX_CLEANUP_NODES - 1; i++) {
            g_cleanup_node_pool[i].next = &g_cleanup_node_pool[i + 1];
        }
        g_cleanup_node_pool[KMM_MAX_CLEANUP_NODES - 1].next = NULL;
        g_cleanup_node_free_list = 0;
        g_cleanup_initialized = true;
    }
}

static inline kmm_cleanup_node_t* kmm_alloc_cleanup_node(void) {
    kmm_init_cleanup_pool();
    
    if (g_cleanup_node_free_list == 0) {
        return (kmm_cleanup_node_t*)kmm_v4_malloc(sizeof(kmm_cleanup_node_t));
    }
    
    kmm_cleanup_node_t* node = &g_cleanup_node_pool[g_cleanup_node_free_list - 1];
    g_cleanup_node_free_list--;
    return node;
}

static inline void kmm_free_cleanup_node(kmm_cleanup_node_t* node) {
    if (node >= g_cleanup_node_pool && 
        node < g_cleanup_node_pool + KMM_MAX_CLEANUP_NODES) {
        size_t index = node - g_cleanup_node_pool;
        node->next = (g_cleanup_node_free_list > 0) ? &g_cleanup_node_pool[g_cleanup_node_free_list - 1] : NULL;
        g_cleanup_node_free_list = index + 1;
    } else {
        kmm_v4_free(node);
    }
}

static inline int kmm_register_cleanup(kmm_context_t* ctx, void* ptr, void (*cleanup)(void*)) {
    kmm_cleanup_node_t* node = kmm_alloc_cleanup_node();
    if (KMM_V4_UNLIKELY(!node)) return -1;
    
    node->resource = ptr;
    node->cleanup = cleanup;
    node->next = ctx->cleanup_stack;
    ctx->cleanup_stack = node;
    
    return 0;
}

static inline void kmm_execute_cleanup(kmm_context_t* ctx) {
    kmm_cleanup_node_t* current = ctx->cleanup_stack;
    while (current) {
        if (current->cleanup && current->resource) {
            current->cleanup(current->resource);
        }
        kmm_cleanup_node_t* temp = current;
        current = current->next;
        kmm_free_cleanup_node(temp);
    }
    ctx->cleanup_stack = NULL;
}

#endif

// ==================== 联合域实现（V3 特色，V4 风格 - 完全自动化） ====================
#if KMM_ENABLE_UNION_DOMAIN

// 自动化联合域管理器（零运行时开销）
typedef struct {
    size_t saved_scope_depth;
    size_t saved_node_count;
    kmm_union_node_t* saved_current;
} kmm_union_auto_scope_t;

// 外部作用域指针（定义在 memory.c 中）
extern __thread kmm_context_t* g_kaula_scope;

// 前向声明
static inline void kmm_init_union_pool(void);
static inline void kmm_union_topological_sort(kmm_union_node_t** nodes, size_t count);

// 自动进入：保存现场 + 设置 TLS
static inline void kmm_union_auto_enter(kmm_union_auto_scope_t* scope) {
    kmm_init_union_pool();
    scope->saved_scope_depth = g_union_domain.scope_depth;
    scope->saved_node_count = g_union_domain.node_count;
    scope->saved_current = g_union_domain.current;
    g_union_domain.scope_depth++;
    g_union_domain.max_depth = (g_union_domain.scope_depth > g_union_domain.max_depth) ? 
                                g_union_domain.scope_depth : g_union_domain.max_depth;
}

// 自动退出：恢复现场 + 清理
static inline void kmm_union_auto_exit(kmm_union_auto_scope_t* scope) {
    (void)scope;
    if (g_union_domain.scope_depth > 0) {
        g_union_domain.scope_depth--;
        
        if (g_union_domain.scope_depth == 0) {
            kmm_union_destroy(&g_union_domain);
        }
    }
}

// 自动化分配 + 依赖检测（直接传递 scope 指针）
static inline void* kmm_union_auto_alloc_with_scope(kmm_union_auto_scope_t* scope, size_t size) {
    (void)scope;
    if (!g_kaula_scope || !g_kaula_scope->is_initialized) {
        return NULL;
    }
    
    void* obj = kmm_alloc(g_kaula_scope, size, "<union_auto>", 0);
    if (!obj) return NULL;
    
    kmm_union_node_t* node = kmm_union_node_alloc();
    if (!node) return obj;
    
    node->object = obj;
    node->object_size = size;
    node->status = KMM_DOMAIN_UNION;
    node->scope_depth = g_union_domain.scope_depth;
    node->parent = g_union_domain.current;
    node->next = NULL;
    node->dependencies = NULL;
    node->dependency_count = 0;
    node->is_root = (g_union_domain.scope_depth == 0);
    node->is_elected = true;
    
    if (g_union_domain.root == NULL) {
        g_union_domain.root = node;
    }
    
    g_union_domain.current = node;
    g_union_domain.node_count++;
    
    g_kaula_scope->union_rep = node;
    g_kaula_scope->domain = &g_union_domain;
    
    kmm_union_auto_detect_dependencies(node);
    return obj;
}

// 旧版本（使用 TLS，已废弃）

// ==================== 联合域自动化宏（零成本，用户无需手动管理） ====================
// 简化宏：用户声明一个 scope 变量，宏自动管理生命周期
// 用法：
//   kmm_union_auto_scope_t _union_scope;
//   KMM_UNION_SCOPE_START(&_union_scope);
//       Node* n = KMM_UNION_ALLOC(Node, &_union_scope);
//   KMM_UNION_SCOPE_END();

#define KMM_UNION_SCOPE_START(scope_ptr) \
    for (int _kmm_u_done = 0; \
         !_kmm_u_done; \
         _kmm_u_done = 1, kmm_union_auto_exit(scope_ptr)) \
    if ((kmm_union_auto_enter(scope_ptr), 1))

#define KMM_UNION_SCOPE_END() // for 循环自动结束

// 自动分配（需传递 scope 指针）
#define KMM_UNION_ALLOC(type, scope_ptr) \
    ((type*)kmm_union_auto_alloc_with_scope(scope_ptr, sizeof(type)))

#define KMM_UNION_ALLOC_ARRAY(type, count, scope_ptr) \
    ((type*)kmm_union_auto_alloc_with_scope(scope_ptr, sizeof(type) * (count)))

#define KMM_UNION_ALLOC_ZERO(type, scope_ptr) \
    ({ type* p = KMM_UNION_ALLOC(type, scope_ptr); \
       if(p) kmm_v4_zero_auto(p, sizeof(type)); \
       p; })

// ==================== 导出给 memory.h 使用的自动化函数 ====================
// 使用 TLS 存储 scope，供宏使用
// 注意：Windows/Clang 上 TLS 有限，使用全局变量

static kmm_union_auto_scope_t g_union_scope_storage;

void kmm_union_auto_enter_fn(void) {
    kmm_union_auto_enter(&g_union_scope_storage);
}

void kmm_union_auto_exit_fn(void) {
    kmm_union_auto_exit(&g_union_scope_storage);
}

void* kmm_union_auto_alloc_fn(size_t size) {
    return kmm_union_auto_alloc_with_scope(&g_union_scope_storage, size);
}

static inline void kmm_init_union_pool(void) {
    if (!g_union_pool_initialized) {
        for (size_t i = 0; i < KMM_MAX_UNION_NODES - 1; i++) {
            g_union_node_pool[i].next = &g_union_node_pool[i + 1];
        }
        g_union_node_pool[KMM_MAX_UNION_NODES - 1].next = NULL;
        g_union_node_free_list = 0;
        g_union_pool_initialized = true;
    }
}

static inline kmm_union_node_t* kmm_union_node_alloc(void) {
    kmm_init_union_pool();
    
    if (g_union_node_free_list >= KMM_MAX_UNION_NODES) {
        return (kmm_union_node_t*)kmm_v4_malloc(sizeof(kmm_union_node_t));
    }
    
    kmm_union_node_t* node = &g_union_node_pool[g_union_node_free_list];
    g_union_node_free_list = (node->next) ? (size_t)(node->next - g_union_node_pool) : KMM_MAX_UNION_NODES;
    return node;
}

static inline void kmm_union_node_free(kmm_union_node_t* node) {
    if (node >= g_union_node_pool && 
        node < g_union_node_pool + KMM_MAX_UNION_NODES) {
        size_t index = (size_t)(node - g_union_node_pool);
        node->next = (g_union_node_free_list < KMM_MAX_UNION_NODES) ? &g_union_node_pool[g_union_node_free_list] : NULL;
        g_union_node_free_list = index;
    } else {
        kmm_v4_free(node);
    }
}

static inline bool kmm_union_has_dependency(kmm_union_node_t* node, kmm_union_node_t* target) {
    for (size_t i = 0; i < node->dependency_count; i++) {
        if (node->dependencies[i] == target) {
            return true;
        }
    }
    return false;
}

static inline kmm_union_node_t* kmm_find_node_by_pointer(void* ptr) {
    kmm_union_node_t* current = g_union_domain.root;
    while (current) {
        if (current->object == ptr) {
            return current;
        }
        current = current->next;
    }
    return NULL;
}

static inline void kmm_union_auto_detect_dependencies(kmm_union_node_t* node) {
    if (!node || !node->object) return;
    
    void** ptr = (void**)node->object;
    size_t word_count = node->object_size / sizeof(void*);
    
    for (size_t i = 0; i < word_count; i++) {
        void* potential_ptr = ptr[i];
        if (potential_ptr && (uintptr_t)potential_ptr > 0x1000) {
            kmm_union_node_t* target = kmm_find_node_by_pointer(potential_ptr);
            if (target && target != node && !kmm_union_has_dependency(node, target)) {
                if (!node->dependencies) {
                    node->dependencies = (kmm_union_node_t**)kmm_v4_malloc(sizeof(kmm_union_node_t*) * KMM_MAX_DEPENDENCIES);
                    if (!node->dependencies) return;
                }
                if (node->dependency_count < KMM_MAX_DEPENDENCIES) {
                    node->dependencies[node->dependency_count++] = target;
                }
            }
        }
    }
}

static inline bool kmm_union_detect_cycle(kmm_union_node_t* node) {
    if (node->scope_depth == 0) {
        return false;
    }
    
    kmm_union_node_t* current = node->parent;
    size_t depth = 0;
    
    while (current) {
        depth++;
        
        if (depth > KMM_MAX_UNION_DEPTH) {
            return true;
        }
        
        if (kmm_union_has_dependency(node, current)) {
            return true;
        }
        
        current = current->parent;
    }
    
    return false;
}

static inline void kmm_union_promote(kmm_union_node_t* node) {
    if (!node || !node->parent) {
        return;
    }
    
    node->scope_depth = node->parent->scope_depth;
    node->status = KMM_DOMAIN_ESCAPED;
    
    if (node->parent->scope_depth > 0) {
        kmm_union_promote(node->parent);
    }
}

static inline void kmm_union_topological_sort(kmm_union_node_t** nodes, size_t count) {
    if (count <= 1) return;
    if (count > 1024) count = 1024;
    
    for (size_t i = 0; i < count; i++) {
        nodes[i]->temp_in_degree = nodes[i]->dependency_count;
        nodes[i]->temp_visited = false;
    }
    
    kmm_union_node_t** queue = g_union_sort_buffer;
    size_t queue_front = 0;
    size_t queue_back = 0;
    
    for (size_t i = 0; i < count; i++) {
        if (nodes[i]->temp_in_degree == 0) {
            queue[queue_back++] = nodes[i];
        }
    }
    
    size_t sorted_count = 0;
    
    while (queue_front < queue_back) {
        kmm_union_node_t* current = queue[queue_front++];
        nodes[sorted_count++] = current;
        
        for (size_t i = 0; i < count; i++) {
            if (nodes[i]->temp_visited) continue;
            
            for (size_t j = 0; j < nodes[i]->dependency_count; j++) {
                if (nodes[i]->dependencies[j] == current) {
                    nodes[i]->temp_in_degree--;
                    if (nodes[i]->temp_in_degree == 0) {
                        nodes[i]->temp_visited = true;
                        queue[queue_back++] = nodes[i];
                    }
                    break;
                }
            }
        }
    }
}

static inline void kmm_union_destroy(kmm_union_domain_t* domain) {
    if (!domain->root) return;
    
    kmm_union_node_t** nodes = g_union_sort_buffer;
    size_t count = 0;
    
    kmm_union_node_t* current = domain->root;
    while (current && count < 1024) {
        nodes[count++] = current;
        current = current->next;
    }
    
    kmm_union_topological_sort(nodes, count);
    
    for (size_t i = count; i > 0; i--) {
        kmm_union_node_t* node = nodes[i - 1];
        
        if (node->dependencies) {
            kmm_v4_free(node->dependencies);
            node->dependencies = NULL;
            node->dependency_count = 0;
        }
        
        kmm_union_node_free(node);
    }
    
    domain->root = NULL;
    domain->current = NULL;
    domain->node_count = 0;
    domain->scope_depth = 0;
}

// 公开 API：联合域选举
void* kmm_union_elect(kmm_context_t* ctx, size_t size, const char* file, int line) {
    void* obj = kmm_alloc(ctx, size, file, line);
    if (!obj) return NULL;
    
    kmm_union_node_t* node = kmm_union_node_alloc();
    if (!node) return obj;
    
    node->object = obj;
    node->object_size = size;
    node->status = KMM_DOMAIN_UNION;
    node->scope_depth = g_union_domain.scope_depth;
    node->parent = g_union_domain.current;
    node->next = NULL;
    node->dependencies = NULL;
    node->dependency_count = 0;
    node->is_root = (g_union_domain.scope_depth == 0);
    node->is_elected = true;
    
    if (g_union_domain.root == NULL) {
        g_union_domain.root = node;
    }
    
    g_union_domain.current = node;
    g_union_domain.node_count++;
    
    ctx->union_rep = node;
    ctx->domain = &g_union_domain;
    
    kmm_union_auto_detect_dependencies(node);
    
    if (kmm_union_detect_cycle(node)) {
        node->status = KMM_DOMAIN_LOCAL;
        node->is_elected = false;
        return obj;
    }
    
    return obj;
}

// 公开 API：设置依赖关系
void kmm_union_set_dependencies(void* obj, void** deps, size_t count) {
    if (!obj || !deps || count == 0) return;
    
    kmm_union_node_t* node = NULL;
    kmm_union_node_t* current = g_union_domain.root;
    
    while (current) {
        if (current->object == obj) {
            node = current;
            break;
        }
        current = current->next;
    }
    
    if (!node) return;
    
    if (count > KMM_MAX_DEPENDENCIES) {
        count = KMM_MAX_DEPENDENCIES;
    }
    
    node->dependencies = (kmm_union_node_t**)kmm_v4_malloc(sizeof(kmm_union_node_t*) * count);
    node->dependency_count = count;
    
    for (size_t i = 0; i < count; i++) {
        current = g_union_domain.root;
        while (current) {
            if (current->object == deps[i]) {
                node->dependencies[i] = current;
                break;
            }
            current = current->next;
        }
    }
}

#endif // KMM_ENABLE_UNION_DOMAIN

// ==================== 公共 API 实现 ====================

int kmm_init(kmm_context_t* ctx) {
    if (!ctx) return -1;
    memset(ctx, 0, sizeof(kmm_context_t));
    
#if KMM_ENABLE_ARENA
    ctx->tiny_arena.is_initialized = false;
    ctx->tiny_arena.max_capacity = KMM_ARENA_TINY_MAX;
    
    ctx->small_arena.is_initialized = false;
    ctx->small_arena.max_capacity = KMM_ARENA_SMALL_MAX;
    
    ctx->medium_arena.is_initialized = false;
    ctx->medium_arena.max_capacity = KMM_ARENA_MEDIUM_MAX;
#endif

#if KMM_ENABLE_THREAD_CACHE
    kmm_thread_cache_init();
    ctx->thread_cache = &tls_kmm_thread_cache;
#endif

#if KMM_ENABLE_CLEANUP_STACK
    kmm_init_cleanup_pool();
    ctx->cleanup_stack = NULL;
#endif

#if KMM_ENABLE_UNION_DOMAIN
    ctx->union_rep = NULL;
    ctx->domain = &g_union_domain;
    g_union_domain.root = NULL;
    g_union_domain.current = NULL;
    g_union_domain.scope_depth = 0;
    g_union_domain.node_count = 0;
#endif

    ctx->alloc_counter = 0;
    ctx->total_bytes = 0;
    ctx->peak_usage = 0;
    ctx->is_initialized = true;
    
    return 0;
}

void kmm_destroy(kmm_context_t* ctx) {
    if (!ctx || !ctx->is_initialized) return;
    
#if KMM_ENABLE_CLEANUP_STACK
    kmm_execute_cleanup(ctx);
#endif

#if KMM_ENABLE_ARENA
    if (ctx->tiny_arena.buffer) kmm_v4_free(ctx->tiny_arena.buffer);
    if (ctx->small_arena.buffer) kmm_v4_free(ctx->small_arena.buffer);
    if (ctx->medium_arena.buffer) kmm_v4_free(ctx->medium_arena.buffer);
#endif

#if KMM_ENABLE_UNION_DOMAIN
    if (ctx->union_rep) {
        kmm_union_promote(ctx->union_rep);
    }
    if (g_union_domain.scope_depth > 0) {
        g_union_domain.scope_depth--;
    }
    kmm_union_destroy(&g_union_domain);
#endif

    memset(ctx, 0, sizeof(kmm_context_t));
}

void* kmm_alloc(kmm_context_t* ctx, size_t size, const char* file, int line) {
    if (KMM_V4_UNLIKELY(!ctx || !ctx->is_initialized)) return NULL;
    if (KMM_V4_UNLIKELY(size == 0)) return NULL;
    
    void* ptr = NULL;
    
#if KMM_ENABLE_THREAD_CACHE
    ptr = kmm_thread_cache_alloc(size);
    if (KMM_V4_LIKELY(ptr)) {
        return ptr;
    }
#endif
    
#if KMM_ENABLE_ARENA
    if (KMM_V4_LIKELY(size <= 64)) {
        ptr = kmm_arena_alloc_tiny(&ctx->tiny_arena, size);
        if (KMM_V4_LIKELY(ptr)) {
            return ptr;
        }
        
        ptr = kmm_arena_alloc(&ctx->small_arena, size, KMM_ARENA_SMALL_MIN, KMM_ARENA_SMALL_MAX);
        if (ptr) {
            return ptr;
        }
    } 
    else if (KMM_V4_LIKELY(size <= 256)) {
        ptr = kmm_arena_alloc(&ctx->small_arena, size, KMM_ARENA_SMALL_MIN, KMM_ARENA_SMALL_MAX);
        if (KMM_V4_LIKELY(ptr)) {
            return ptr;
        }
        
        ptr = kmm_arena_alloc(&ctx->medium_arena, size, KMM_ARENA_MEDIUM_MIN, KMM_ARENA_MEDIUM_MAX);
        if (ptr) {
            return ptr;
        }
    } 
    else if (size <= 2048) {
        ptr = kmm_arena_alloc(&ctx->medium_arena, size, KMM_ARENA_MEDIUM_MIN, KMM_ARENA_MEDIUM_MAX);
        if (ptr) {
            return ptr;
        }
    }
#endif
    
#if KMM_ENABLE_SAFE_ALLOC
    ptr = kmm_safe_malloc(size, file, line);
    if (KMM_V4_UNLIKELY(!ptr)) return NULL;
    
#if KMM_ENABLE_CLEANUP_STACK
    if (KMM_V4_UNLIKELY(kmm_register_cleanup(ctx, ptr, kmm_safe_free) != 0)) {
        kmm_safe_free(ptr);
        return NULL;
    }
#endif
#else
    ptr = kmm_v4_malloc(size);
#endif
    
    ctx->alloc_counter++;
    ctx->total_bytes += size;
    if (ctx->total_bytes > ctx->peak_usage) {
        ctx->peak_usage = ctx->total_bytes;
    }
    
    return ptr;
}

void kmm_free(void* ptr) {
    if (KMM_V4_UNLIKELY(!ptr)) return;
    
#if KMM_ENABLE_THREAD_CACHE
    kmm_thread_cache_free(ptr);
#elif KMM_ENABLE_SAFE_ALLOC
    if (kmm_check_redzone(ptr)) {
        kmm_safe_header_t* hdr = kmm_get_header_from_user(ptr);
        kmm_v4_free(hdr);
    }
#else
    kmm_v4_free(ptr);
#endif
}

void** kmm_alloc_batch(kmm_context_t* ctx, size_t size, size_t count, const char* file, int line) {
    void** ptrs = (void**)kmm_alloc(ctx, count * sizeof(void*), file, line);
    if (KMM_V4_UNLIKELY(!ptrs)) return NULL;
    
    uint8_t* base = (uint8_t*)kmm_alloc(ctx, size * count, file, line);
    if (KMM_V4_UNLIKELY(!base)) {
        kmm_free(ptrs);
        return NULL;
    }
    
    for (size_t i = 0; i < count; i++) {
        ptrs[i] = base + i * size;
    }
    
    return ptrs;
}

void kmm_reset(kmm_context_t* ctx) {
    if (!ctx || !ctx->is_initialized) return;
    
#if KMM_ENABLE_CLEANUP_STACK
    kmm_execute_cleanup(ctx);
#endif

#if KMM_ENABLE_ARENA
    if (ctx->tiny_arena.buffer) {
        kmm_v4_free(ctx->tiny_arena.buffer);
        ctx->tiny_arena.buffer = NULL;
        ctx->tiny_arena.offset = 0;
        ctx->tiny_arena.capacity = 0;
        ctx->tiny_arena.is_initialized = false;
    }
    if (ctx->small_arena.buffer) {
        kmm_v4_free(ctx->small_arena.buffer);
        ctx->small_arena.buffer = NULL;
        ctx->small_arena.offset = 0;
        ctx->small_arena.capacity = 0;
        ctx->small_arena.is_initialized = false;
    }
    if (ctx->medium_arena.buffer) {
        kmm_v4_free(ctx->medium_arena.buffer);
        ctx->medium_arena.buffer = NULL;
        ctx->medium_arena.offset = 0;
        ctx->medium_arena.capacity = 0;
        ctx->medium_arena.is_initialized = false;
    }
#endif

    kmm_v4_reset();
    
    ctx->alloc_counter = 0;
    ctx->total_bytes = 0;
    ctx->peak_usage = 0;
}

void kmm_print_pool_stats(void) {
    printf("\n=== KMM V4 Enhanced Statistics ===\n");
    printf("Pool Size:      %zu bytes (%.2f MB)\n", 
           (size_t)KMM_V4_POOL_SIZE, KMM_V4_POOL_SIZE / (1024.0 * 1024.0));
    printf("Used:           %zu bytes (%.2f MB)\n", 
           kmm_v4_usage(), kmm_v4_usage() / (1024.0 * 1024.0));
    printf("Available:      %zu bytes (%.2f MB)\n", 
           kmm_v4_available(), kmm_v4_available() / (1024.0 * 1024.0));
    printf("Usage:          %.2f%%\n", 
           (kmm_v4_usage() * 100.0) / KMM_V4_POOL_SIZE);
    printf("\n--- Allocation Stats ---\n");
    printf("Alloc Count:    %zu\n", g_kmm_ctx.alloc_counter);
    printf("Total Bytes:    %zu bytes (%.2f MB)\n", 
           g_kmm_ctx.total_bytes, g_kmm_ctx.total_bytes / (1024.0 * 1024.0));
    printf("Peak Usage:     %zu bytes (%.2f MB)\n", 
           g_kmm_ctx.peak_usage, g_kmm_ctx.peak_usage / (1024.0 * 1024.0));
    
#if KMM_ENABLE_ARENA
    printf("\n--- Arena Stats ---\n");
    printf("Tiny Arena:     %zu/%zu bytes (%.1f%%)\n", 
           g_kmm_ctx.tiny_arena.offset, g_kmm_ctx.tiny_arena.capacity,
           g_kmm_ctx.tiny_arena.capacity > 0 ? 
               (g_kmm_ctx.tiny_arena.offset * 100.0 / g_kmm_ctx.tiny_arena.capacity) : 0);
    printf("Small Arena:    %zu/%zu bytes (%.1f%%)\n",
           g_kmm_ctx.small_arena.offset, g_kmm_ctx.small_arena.capacity,
           g_kmm_ctx.small_arena.capacity > 0 ?
               (g_kmm_ctx.small_arena.offset * 100.0 / g_kmm_ctx.small_arena.capacity) : 0);
    printf("Medium Arena:   %zu/%zu bytes (%.1f%%)\n",
           g_kmm_ctx.medium_arena.offset, g_kmm_ctx.medium_arena.capacity,
           g_kmm_ctx.medium_arena.capacity > 0 ?
               (g_kmm_ctx.medium_arena.offset * 100.0 / g_kmm_ctx.medium_arena.capacity) : 0);
#endif

#if KMM_ENABLE_THREAD_CACHE
    printf("\n--- Thread Cache ---\n");
    printf("Cache Size:     %zu objects\n", tls_kmm_thread_cache.cache_size);
#endif
}

// ==================== 便捷宏（兼容 v3 风格） ====================
#define KMM_V3_ALLOC(size)              kmm_alloc(ctx, size, __FILE__, __LINE__)
#define KMM_V3_FREE(ptr)                kmm_free(ptr)
#define KMM_V3_RESET()                  kmm_reset(ctx)

#define KMM_V3_ALLOC_BATCH(type, count) \
    ((type*)kmm_alloc(ctx, sizeof(type) * (count), __FILE__, __LINE__))

#define KMM_V3_ALLOC_ARRAY(type, count) \
    ((type*)kmm_alloc(ctx, sizeof(type) * (count), __FILE__, __LINE__))

#define KMM_V3_ALLOC_STRUCT(type) \
    ((type*)kmm_alloc(ctx, sizeof(type), __FILE__, __LINE__))

#define KMM_V3_ALLOC_ZERO(type) \
    ({ type* p = KMM_V3_ALLOC_STRUCT(type); \
       if(p) kmm_v4_zero_auto(p, sizeof(type)); \
       p; })

#endif // KMM_SCOPED_ALLOCATOR_IMPL_H
