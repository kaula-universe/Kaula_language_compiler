#ifndef KMM_SCOPED_ALLOCATOR_V2_H
#define KMM_SCOPED_ALLOCATOR_V2_H

#include <stdint.h>
#include <stddef.h>
#include <stdbool.h>
#include <stdatomic.h>

// 包含快速分配器
#include "../std/memory/fast_alloc.h"

// ==================== 核心配置宏 ====================
#define KMM_REDZONE_SIZE         8
#define KMM_REDZONE_PATTERN      0xCD
#define KMM_CANARY_VALUE         0xDEADBEEFCAFEBABEULL
#define KMM_ALIGNMENT            8
#define KMM_CACHE_LINE_SIZE      64

// ==================== 分层对象阈值 ====================
#define KMM_SIZE_TINY            (16)
#define KMM_SIZE_SMALL           (128)
#define KMM_SIZE_MEDIUM          (1024)
#define KMM_SIZE_LARGE           (4 * 1024)

// ==================== 延迟初始化 + 动态扩展配置 ====================
// 初始容量（0 = 延迟分配）
#define KMM_ARENA_TINY_INITIAL     (0)
#define KMM_ARENA_SMALL_INITIAL    (0)
#define KMM_ARENA_MEDIUM_INITIAL   (0)

// 最小容量（首次分配时的初始大小）
#define KMM_ARENA_TINY_MIN         (4 * 1024)
#define KMM_ARENA_SMALL_MIN        (64 * 1024)
#define KMM_ARENA_MEDIUM_MIN       (256 * 1024)

// 最大容量（防止无限扩张）
#define KMM_ARENA_TINY_MAX         (128 * 1024)
#define KMM_ARENA_SMALL_MAX        (2 * 1024 * 1024)
#define KMM_ARENA_MEDIUM_MAX       (8 * 1024 * 1024)

// 扩展策略：倍增因子
#define KMM_ARENA_GROWTH_FACTOR    2

// ==================== 联合域配置 ====================
#define KMM_MAX_UNION_DEPTH        64
#define KMM_MAX_DEPENDENCIES       32

// ==================== 特性开关 ====================
#define KMM_ENABLE_STATS           1
#define KMM_ENABLE_ARENA_RESET     1
#define KMM_ENABLE_FREE_LIST       0  // 禁用空闲列表，简化设计
#define KMM_ENABLE_THREAD_CACHE    1
#define KMM_ENABLE_UNION_DOMAIN    1   // 启用联合域

// ==================== 前向声明 ====================
typedef struct kmm_union_node kmm_union_node_t;
typedef struct kmm_union_domain kmm_union_domain_t;

// ==================== 联合域数据结构 ====================
typedef enum {
    KMM_DOMAIN_LOCAL = 0,
    KMM_DOMAIN_UNION = 1,
    KMM_DOMAIN_ESCAPED = 2
} kmm_domain_status_t;

struct kmm_union_node {
    void* object;
    size_t object_size;
    kmm_domain_status_t status;
    size_t scope_depth;
    kmm_union_node_t* parent;
    kmm_union_node_t* next;
    kmm_union_node_t** dependencies;
    size_t dependency_count;
    bool is_root;
    bool is_elected;
    size_t temp_in_degree;
    bool temp_visited;
};

struct kmm_union_domain {
    kmm_union_node_t* root;
    kmm_union_node_t* current;
    size_t scope_depth;
    size_t node_count;
    size_t max_depth;
};

// ==================== Arena 数据结构（支持延迟初始化） ====================
typedef struct {
    uint8_t* buffer;
    size_t offset;
    size_t capacity;
    size_t max_capacity;
    size_t allocations;
    size_t peak;
    size_t reset_count;
    bool is_initialized;
} kmm_arena_t __attribute__((aligned(KMM_CACHE_LINE_SIZE)));

// ==================== 安全头（堆对象） ====================
typedef struct {
    size_t user_size;
    const char* file;
    int line;
    uint64_t canary;
} kmm_safe_header_t;

// ==================== 清理节点 ====================
typedef void (*kmm_cleanup_fn)(void* ptr);

typedef struct kmm_cleanup_node {
    void* resource;
    kmm_cleanup_fn cleanup;
    struct kmm_cleanup_node* next;
} kmm_cleanup_node_t;

// ==================== 作用域上下文 ====================
typedef struct {
    kmm_arena_t tiny_arena;
    kmm_arena_t small_arena;
    kmm_arena_t medium_arena;
    
    kmm_cleanup_node_t* cleanup_stack;
    size_t alloc_counter;
    
#if KMM_ENABLE_UNION_DOMAIN
    kmm_union_node_t* union_rep;
    kmm_union_domain_t* domain;
#endif
    
#if KMM_ENABLE_STATS
    struct {
        size_t total_allocs;
        size_t arena_hits;
        size_t heap_hits;
        size_t tiny_hits;
        size_t small_hits;
        size_t medium_hits;
        size_t large_hits;
        size_t union_elections;
        size_t tiny_resets;
        size_t small_resets;
        size_t medium_resets;
        double tiny_total_time;
        double small_total_time;
        double medium_total_time;
        double heap_total_time;
    } stats;
#endif
} kmm_context_t __attribute__((aligned(KMM_CACHE_LINE_SIZE)));

// ==================== 线程缓存 ====================
#if KMM_ENABLE_THREAD_CACHE
#define KMM_THREAD_CACHE_SIZE    256

typedef struct {
    void* cache[KMM_THREAD_CACHE_SIZE];
    size_t cache_size;
    kmm_context_t* global_ctx;
} kmm_thread_cache_t;

#ifdef _WIN32
__declspec(thread) extern kmm_thread_cache_t g_thread_cache;
#else
__thread extern kmm_thread_cache_t g_thread_cache;
#endif
#endif

// ==================== 全局联合域 ====================
#if KMM_ENABLE_UNION_DOMAIN
extern kmm_union_domain_t g_union_domain;
#endif

// ==================== 分支预测优化 ====================
#define KMM_LIKELY(x)       __builtin_expect(!!(x), 1)
#define KMM_UNLIKELY(x)     __builtin_expect(!!(x), 0)

// ==================== 工具宏 ====================
#define KMM_IS_TINY(size)    ((size) <= KMM_SIZE_TINY)
#define KMM_IS_SMALL(size)   ((size) <= KMM_SIZE_SMALL)
#define KMM_IS_MEDIUM(size)  ((size) <= KMM_SIZE_MEDIUM)
#define KMM_IS_LARGE(size)   ((size) > KMM_SIZE_MEDIUM)

// ==================== API 函数 ====================

// 生命周期管理
int kmm_init(kmm_context_t* ctx);
void kmm_destroy(kmm_context_t* ctx);

// 作用域分配（核心 API）
void* kmm_alloc(kmm_context_t* ctx, size_t size, const char* file, int line);
void kmm_free(void* ptr);

// 联合域 API
#if KMM_ENABLE_UNION_DOMAIN
void* kmm_union_elect(kmm_context_t* ctx, size_t size, const char* file, int line);
void kmm_union_set_dependencies(void* obj, void** deps, size_t count);
void kmm_union_destroy(kmm_union_domain_t* domain);
bool kmm_union_detect_cycle(kmm_union_node_t* node);
#endif

// 批量分配 API
void** kmm_alloc_batch(kmm_context_t* ctx, size_t size, size_t count, const char* file, int line);

// 便捷宏
#define KMM_ALLOC(ctx, size) kmm_alloc(ctx, size, __FILE__, __LINE__)
#define KMM_FREE(ptr) kmm_free(ptr)

#if KMM_ENABLE_UNION_DOMAIN
#define KMM_UNION_ELECT(ctx, size) kmm_union_elect(ctx, size, __FILE__, __LINE__)
#define KMM_UNION_DEPS(obj, ...) \
    do { \
        void* deps[] = { __VA_ARGS__ }; \
        kmm_union_set_dependencies(obj, deps, sizeof(deps)/sizeof(deps[0])); \
    } while(0)
#endif

// 统计信息
#if KMM_ENABLE_STATS
void kmm_print_stats(kmm_context_t* ctx);
void kmm_print_union_stats(kmm_union_domain_t* domain);
#endif

// ==================== 内联工具函数 ====================

static inline size_t kmm_align_up(size_t size, size_t alignment) {
    return (size + alignment - 1) & ~(alignment - 1);
}

static inline size_t kmm_align_down(size_t size, size_t alignment) {
    return size & ~(alignment - 1);
}

#endif // KMM_SCOPED_ALLOCATOR_V2_H
