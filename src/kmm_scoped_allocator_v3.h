#ifndef KMM_SCOPED_ALLOCATOR_V3_H
#define KMM_SCOPED_ALLOCATOR_V3_H

#include <stdint.h>
#include <stddef.h>
#include <stdbool.h>
#include <string.h>

// ==================== 核心配置（可调整） ====================
#define KMM_V3_POOL_SIZE           (4 * 1024 * 1024)    // 4MB 静态池
#define KMM_V3_ENABLE_FALLBACK     0                     // 0=严格模式，1=允许回退
#define KMM_V3_ALIGNMENT           8
#define KMM_V3_CACHE_LINE_SIZE     64
#define KMM_V3_THREAD_CACHE_SIZE   256

// 兼容 V2 的配置
#define KMM_REDZONE_SIZE           8
#define KMM_REDZONE_PATTERN        0xCD
#define KMM_CANARY_VALUE           0xDEADBEEFCAFEBABEULL
#define KMM_ALIGNMENT              8
#define KMM_ARENA_TINY_MIN         (64 * 1024)
#define KMM_ARENA_TINY_MAX         (256 * 1024)
#define KMM_ARENA_SMALL_MIN        (512 * 1024)
#define KMM_ARENA_SMALL_MAX        (4 * 1024 * 1024)
#define KMM_ARENA_MEDIUM_MIN       (2 * 1024 * 1024)
#define KMM_ARENA_MEDIUM_MAX       (16 * 1024 * 1024)
#define KMM_ARENA_GROWTH_FACTOR    2
#define KMM_MAX_UNION_DEPTH        64
#define KMM_MAX_DEPENDENCIES       32
#define KMM_ENABLE_UNION_DOMAIN    1
#define KMM_ENABLE_THREAD_CACHE    1
#define KMM_THREAD_CACHE_SIZE      KMM_V3_THREAD_CACHE_SIZE

// 对象大小分类
#define KMM_V3_SIZE_TINY           64
#define KMM_V3_SIZE_SMALL          256
#define KMM_V3_SIZE_MEDIUM         2048

// 性能优化开关
#define KMM_V3_ENABLE_PREFETCH     1
#define KMM_V3_ENABLE_UNROLL       1

// ==================== 静态内存池（零依赖核心） ====================
#ifdef _MSC_VER
__declspec(align(KMM_V3_ALIGNMENT))
static uint8_t g_kmm_v3_pool[KMM_V3_POOL_SIZE];
static size_t g_kmm_v3_offset = 0;
#else
static uint8_t g_kmm_v3_pool[KMM_V3_POOL_SIZE] 
    __attribute__((aligned(KMM_V3_ALIGNMENT)));
static size_t g_kmm_v3_offset = 0;
#endif

// 预取优化
#if KMM_V3_ENABLE_PREFETCH && defined(__GNUC__)
#define KMM_V3_PREFETCH(ptr) __builtin_prefetch((ptr), 0, 3)
#else
#define KMM_V3_PREFETCH(ptr) ((void)0)
#endif

// 快速路径：无锁 bump allocator（单线程最优）
static inline void* kmm_v3_pool_alloc_fast(size_t size) {
    const size_t mask = KMM_V3_ALIGNMENT - 1;
    size_t aligned_size = (size + mask) & ~mask;
    
    size_t offset = g_kmm_v3_offset;
    size_t new_offset = offset + aligned_size;
    
    if (__builtin_expect(new_offset <= KMM_V3_POOL_SIZE, 1)) {
        g_kmm_v3_offset = new_offset;
        KMM_V3_PREFETCH(g_kmm_v3_pool + new_offset);
        return g_kmm_v3_pool + offset;
    }
    return NULL;
}

// 原子版本（多线程安全）
static inline void* kmm_v3_pool_alloc_atomic(size_t size) {
    const size_t mask = KMM_V3_ALIGNMENT - 1;
    size_t aligned_size = (size + mask) & ~mask;
    
    for (;;) {
        size_t offset = __atomic_load_n(&g_kmm_v3_offset, __ATOMIC_RELAXED);
        size_t new_offset = offset + aligned_size;
        
        if (new_offset > KMM_V3_POOL_SIZE) {
            return NULL;
        }
        
        if (__atomic_compare_exchange_n(
                &g_kmm_v3_offset, &offset, new_offset, 0,
                __ATOMIC_ACQ_REL, __ATOMIC_RELAXED)) {
            return g_kmm_v3_pool + offset;
        }
    }
}

// 批量分配（优化：一次分配多个对象）
static inline void* kmm_v3_pool_alloc_batch(size_t size, size_t count) {
    const size_t mask = KMM_V3_ALIGNMENT - 1;
    size_t total_size = ((size * count) + mask) & ~mask;
    
    size_t offset = g_kmm_v3_offset;
    size_t new_offset = offset + total_size;
    
    if (new_offset <= KMM_V3_POOL_SIZE) {
        g_kmm_v3_offset = new_offset;
        return g_kmm_v3_pool + offset;
    }
    return NULL;
}

// 重置内存池
static inline void kmm_v3_pool_reset(void) {
    __atomic_store_n(&g_kmm_v3_offset, 0, __ATOMIC_SEQ_CST);
}

// 获取使用率
static inline size_t kmm_v3_pool_usage(void) {
    return __atomic_load_n(&g_kmm_v3_offset, __ATOMIC_RELAXED);
}

// 获取剩余空间
static inline size_t kmm_v3_pool_available(void) {
    return KMM_V3_POOL_SIZE - kmm_v3_pool_usage();
}

// ==================== 回退分配器（可选） ====================
#if KMM_V3_ENABLE_FALLBACK
#include <stdlib.h>
#define KMM_V3_FALLBACK_ALLOC(size) malloc(size)
#define KMM_V3_FALLBACK_FREE(ptr)   free(ptr)
#else
#define KMM_V3_FALLBACK_ALLOC(size) NULL
#define KMM_V3_FALLBACK_FREE(ptr)   ((void)0)
#endif

// ==================== 统一分配接口 ====================
static inline void* kmm_v3_malloc(size_t size) {
    if (__builtin_expect(size == 0, 0)) return NULL;
    
    void* ptr = kmm_v3_pool_alloc_fast(size);
    if (__builtin_expect(ptr != NULL, 1)) {
        return ptr;
    }
    
    return KMM_V3_FALLBACK_ALLOC(size);
}

static inline void kmm_v3_free(void* ptr) {
    if (!ptr) return;
    
    // 检查是否在静态池中
    if ((uint8_t*)ptr >= g_kmm_v3_pool && 
        (uint8_t*)ptr < g_kmm_v3_pool + KMM_V3_POOL_SIZE) {
        return;  // 池内对象不单独释放
    }
    
    KMM_V3_FALLBACK_FREE(ptr);
}

static inline void* kmm_v3_realloc(void* ptr, size_t old_size, size_t new_size) {
    if (!ptr) return kmm_v3_malloc(new_size);
    if (new_size == 0) return NULL;
    
    // 检查是否在静态池中
    if ((uint8_t*)ptr >= g_kmm_v3_pool && 
        (uint8_t*)ptr < g_kmm_v3_pool + KMM_V3_POOL_SIZE) {
        void* new_ptr = kmm_v3_malloc(new_size);
        if (new_ptr) {
            memcpy(new_ptr, ptr, old_size < new_size ? old_size : new_size);
        }
        return new_ptr;
    }
    
    // 回退路径
    #if KMM_V3_ENABLE_FALLBACK
        return realloc(ptr, new_size);
    #else
        return NULL;
    #endif
}

// ==================== 线程缓存（无锁实现） ====================
#ifdef _MSC_VER
__declspec(thread)
#else
__thread
#endif
static void* g_kmm_v3_thread_cache[KMM_V3_THREAD_CACHE_SIZE];

#ifdef _MSC_VER
__declspec(thread)
#else
__thread
#endif
static size_t g_kmm_v3_thread_cache_size = 0;

static inline void* kmm_v3_thread_cache_alloc(size_t size) {
    if (g_kmm_v3_thread_cache_size > 0) {
        return g_kmm_v3_thread_cache[--g_kmm_v3_thread_cache_size];
    }
    return NULL;
}

static inline void kmm_v3_thread_cache_free(void* ptr) {
    if (g_kmm_v3_thread_cache_size < KMM_V3_THREAD_CACHE_SIZE) {
        g_kmm_v3_thread_cache[g_kmm_v3_thread_cache_size++] = ptr;
    } else {
        kmm_v3_free(ptr);
    }
}

// ==================== SIMD 内存清零（最优性能） ====================
#if defined(__AVX512F__)
#include <immintrin.h>
static inline void kmm_v3_zero(void* ptr, size_t size) {
    __m512i zero = _mm512_setzero_si512();
    uint8_t* p = (uint8_t*)ptr;
    while (size >= 64) {
        _mm512_storeu_si512((__m512i*)p, zero);
        p += 64;
        size -= 64;
    }
    if (size > 0) memset(p, 0, size);
}
#elif defined(__AVX2__)
#include <immintrin.h>
static inline void kmm_v3_zero(void* ptr, size_t size) {
    __m256i zero = _mm256_setzero_si256();
    uint8_t* p = (uint8_t*)ptr;
    while (size >= 32) {
        _mm256_storeu_si256((__m256i*)p, zero);
        p += 32;
        size -= 32;
    }
    if (size > 0) memset(p, 0, size);
}
#elif defined(__SSE2__)
#include <emmintrin.h>
static inline void kmm_v3_zero(void* ptr, size_t size) {
    __m128i zero = _mm_setzero_si128();
    uint8_t* p = (uint8_t*)ptr;
    while (size >= 16) {
        _mm_storeu_si128((__m128i*)p, zero);
        p += 16;
        size -= 16;
    }
    if (size > 0) memset(p, 0, size);
}
#else
static inline void kmm_v3_zero(void* ptr, size_t size) {
    memset(ptr, 0, size);
}
#endif

// ==================== 内存屏障和分支预测 ====================
#define KMM_V3_LIKELY(x)       __builtin_expect(!!(x), 1)
#define KMM_V3_UNLIKELY(x)     __builtin_expect(!!(x), 0)
#define KMM_V3_BARRIER()       __atomic_thread_fence(__ATOMIC_SEQ_CST)

// ==================== 工具宏 ====================
#define KMM_V3_IS_TINY(size)    ((size) <= KMM_V3_SIZE_TINY)
#define KMM_V3_IS_SMALL(size)   ((size) <= KMM_V3_SIZE_SMALL)
#define KMM_V3_IS_MEDIUM(size)  ((size) <= KMM_V3_SIZE_MEDIUM)

#define KMM_V3_ALLOC(size)      kmm_v3_malloc(size)
#define KMM_V3_FREE(ptr)        kmm_v3_free(ptr)
#define KMM_V3_RESET()          kmm_v3_pool_reset()

// ==================== V2 兼容数据结构 ====================
typedef struct {
    size_t user_size;
    const char* file;
    int line;
    uint64_t canary;
} kmm_safe_header_t;

typedef struct {
    uint8_t* buffer;
    size_t offset;
    size_t capacity;
    size_t max_capacity;
    size_t allocations;
    size_t peak;
    size_t reset_count;
    bool is_initialized;
} kmm_arena_t __attribute__((aligned(KMM_V3_CACHE_LINE_SIZE)));

typedef void (*kmm_cleanup_fn)(void* ptr);

typedef struct kmm_cleanup_node {
    void* resource;
    kmm_cleanup_fn cleanup;
    struct kmm_cleanup_node* next;
} kmm_cleanup_node_t;

typedef struct kmm_union_node kmm_union_node_t;
typedef struct kmm_union_domain kmm_union_domain_t;

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
} kmm_context_t __attribute__((aligned(KMM_V3_CACHE_LINE_SIZE)));

#if KMM_ENABLE_THREAD_CACHE
typedef struct {
    void* cache[KMM_V3_THREAD_CACHE_SIZE];
    size_t cache_size;
    kmm_context_t* global_ctx;
} kmm_thread_cache_t;

#ifdef _WIN32
__declspec(thread) extern kmm_thread_cache_t g_thread_cache;
#else
__thread extern kmm_thread_cache_t g_thread_cache;
#endif
#endif

#if KMM_ENABLE_UNION_DOMAIN
extern kmm_union_domain_t g_union_domain;
#endif

// ==================== 便捷宏（批量分配） ====================
#define KMM_V3_ALLOC_BATCH(type, count) \
    ((type*)kmm_v3_pool_alloc_batch(sizeof(type), count))

#define KMM_V3_ALLOC_ARRAY(type, count) \
    ((type*)kmm_v3_malloc(sizeof(type) * (count)))

#define KMM_V3_ALLOC_STRUCT(type) \
    ((type*)kmm_v3_malloc(sizeof(type)))

#define KMM_V3_ALLOC_ZERO(size) \
    ({ void* p = kmm_v3_malloc(size); if(p) kmm_v3_zero(p, size); p; })

// ==================== KMM_IS_* 宏（兼容 V2） ====================
#define KMM_IS_TINY(size)    ((size) <= KMM_SIZE_TINY)
#define KMM_IS_SMALL(size)   ((size) <= KMM_SIZE_SMALL)
#define KMM_IS_MEDIUM(size)  ((size) <= KMM_SIZE_MEDIUM)
#define KMM_SIZE_TINY        KMM_V3_SIZE_TINY
#define KMM_SIZE_SMALL       KMM_V3_SIZE_SMALL
#define KMM_SIZE_MEDIUM      KMM_V3_SIZE_MEDIUM

// ==================== API 声明 ====================
int kmm_init(kmm_context_t* ctx);
void kmm_destroy(kmm_context_t* ctx);
void* kmm_alloc(kmm_context_t* ctx, size_t size, const char* file, int line);
void kmm_free(void* ptr);
void** kmm_alloc_batch(kmm_context_t* ctx, size_t size, size_t count, const char* file, int line);
void kmm_print_pool_stats(void);

#if KMM_ENABLE_UNION_DOMAIN
void* kmm_union_elect(kmm_context_t* ctx, size_t size, const char* file, int line);
void kmm_union_set_dependencies(void* obj, void** deps, size_t count);
void kmm_union_destroy(kmm_union_domain_t* domain);
bool kmm_union_detect_cycle(kmm_union_node_t* node);
#endif

#endif // KMM_SCOPED_ALLOCATOR_V3_H
