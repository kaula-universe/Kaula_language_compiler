#ifndef KMM_SCOPED_ALLOCATOR_H
#define KMM_SCOPED_ALLOCATOR_H

#include <stdint.h>
#include <stddef.h>
#include <stdbool.h>

// ==================== 配置宏 ====================
#define KMM_REDZONE_SIZE         8
#define KMM_REDZONE_PATTERN      0xCD
#define KMM_CANARY_VALUE         0xDEADBEEFCAFEBABEULL
#define KMM_ALIGNMENT            8

// 分层对象阈值
#define KMM_SIZE_TINY            (16)
#define KMM_SIZE_SMALL           (128)
#define KMM_SIZE_MEDIUM          (1024)
#define KMM_SIZE_LARGE           (4 * 1024)

// 分层 Arena 大小
#define KMM_ARENA_TINY_SIZE      (64 * 1024)
#define KMM_ARENA_SMALL_SIZE     (1 * 1024 * 1024)
#define KMM_ARENA_MEDIUM_SIZE    (4 * 1024 * 1024)

// 作用域重置阈值
#define KMM_RESET_BATCH_SIZE     1000

// 特性开关
#define KMM_ENABLE_STATS         1
#define KMM_ENABLE_ARENA_RESET   1
#define KMM_ENABLE_FREE_LIST     1
#define KMM_ENABLE_SIZE_CLASSES  1
#define KMM_ENABLE_THREAD_CACHE  1

// 线程缓存配置
#define KMM_THREAD_CACHE_SIZE    1024
#define KMM_BATCH_ALLOC_SIZE     64

// ==================== 数据结构 ====================
typedef void (*kmm_cleanup_fn)(void* ptr);

typedef struct kmm_cleanup_node {
    void* resource;
    kmm_cleanup_fn cleanup;
    struct kmm_cleanup_node* next;
} kmm_cleanup_node_t;

typedef struct {
    size_t user_size;
    const char* file;
    int line;
    uint64_t canary;
} kmm_safe_header_t;

typedef struct {
    uint8_t* buffer __attribute__((aligned(64)));
    size_t offset __attribute__((aligned(64)));
    size_t size;
    size_t peak;
    size_t allocations;
    size_t reset_count;
} kmm_arena_t;

typedef struct kmm_free_block {
    size_t size;
    struct kmm_free_block* next;
} kmm_free_block_t;

typedef struct {
    kmm_arena_t tiny_arena;
    kmm_arena_t small_arena;
    kmm_arena_t medium_arena;
    
    kmm_free_block_t* free_list;
    
    kmm_cleanup_node_t* cleanup_stack;
    size_t alloc_counter;
    
#if KMM_ENABLE_STATS
    struct {
        size_t total_allocs;
        size_t arena_hits;
        size_t heap_hits;
        size_t tiny_hits;
        size_t small_hits;
        size_t medium_hits;
        size_t large_hits;
        size_t tiny_resets;
        size_t small_resets;
        size_t medium_resets;
        size_t free_list_hits;
        size_t thread_cache_hits;
        double tiny_total_time;
        double small_total_time;
        double medium_total_time;
        double heap_total_time;
    } stats;
#endif
} kmm_context_t;

#if KMM_ENABLE_THREAD_CACHE
typedef struct {
    void* cache[KMM_THREAD_CACHE_SIZE];
    size_t cache_size;
    kmm_context_t* global_ctx;
} kmm_thread_cache_t;
#endif

#if KMM_ENABLE_THREAD_CACHE
#ifdef _WIN32
__declspec(thread) extern kmm_thread_cache_t g_thread_cache;
#else
__thread extern kmm_thread_cache_t g_thread_cache;
#endif
#endif

// ==================== API 函数 ====================
// 生命周期管理
int kmm_init(kmm_context_t* ctx);
void kmm_destroy(kmm_context_t* ctx);

// 作用域分配（核心 API）
void* kmm_alloc(kmm_context_t* ctx, size_t size, const char* file, int line);
void kmm_free(void* ptr);

// 批量分配 API（Level 3 优化）
void** kmm_alloc_batch(kmm_context_t* ctx, size_t size, size_t count, const char* file, int line);

// 便捷宏（供 Kaula 代码生成使用）
#define KMM_ALLOC(ctx, size) kmm_alloc(ctx, size, __FILE__, __LINE__)
#define KMM_FREE(ptr) kmm_free(ptr)

// 统计信息（可选）
#if KMM_ENABLE_STATS
void kmm_print_stats(kmm_context_t* ctx);
#endif

#endif // KMM_SCOPED_ALLOCATOR_H
