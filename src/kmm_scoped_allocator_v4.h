#ifndef KMM_SCOPED_ALLOCATOR_V4_H
#define KMM_SCOPED_ALLOCATOR_V4_H

#include <stdint.h>
#include <stddef.h>
#include <stdbool.h>
#include <string.h>

// 原子操作支持（轻量实时线程安全）
#if KMM_THREAD_SAFETY_LEVEL >= 1
#ifdef __STDC_NO_ATOMICS__
// C11 不支持原子操作，使用 GCC/Clang 内置函数
#define KMM_USE_ATOMICS 1
#define KMM_ATOMIC_TYPE unsigned long
#define KMM_ATOMIC_LOAD(var) __atomic_load_n(&(var), __ATOMIC_RELAXED)
#define KMM_ATOMIC_STORE(var, val) __atomic_store_n(&(var), (val), __ATOMIC_RELAXED)
#define KMM_ATOMIC_CAS(var, expected, desired) \
    __atomic_compare_exchange_n(&(var), &(expected), (desired), 1, __ATOMIC_ACQUIRE, __ATOMIC_RELAXED)
#define KMM_ATOMIC_FETCH_ADD(var, val) \
    __atomic_fetch_add(&(var), (val), __ATOMIC_RELAXED)
#else
// 使用 C11 标准原子操作
#define KMM_USE_ATOMICS 1
#include <stdatomic.h>
#define KMM_ATOMIC_TYPE size_t
#define KMM_ATOMIC_LOAD(var) atomic_load(&(var))
#define KMM_ATOMIC_STORE(var, val) atomic_store(&(var), (val))
#define KMM_ATOMIC_CAS(var, expected, desired) \
    atomic_compare_exchange_weak(&(var), &(expected), (desired))
#define KMM_ATOMIC_FETCH_ADD(var, val) \
    atomic_fetch_add(&(var), (val))
#endif
#else
// 单线程模式，无原子操作
#define KMM_USE_ATOMICS 0
#define KMM_ATOMIC_TYPE size_t
#define KMM_ATOMIC_LOAD(var) (var)
#define KMM_ATOMIC_STORE(var, val) ((var) = (val))
// CAS操作返回bool（0或1）
#define KMM_ATOMIC_CAS(var, expected, desired) \
    (((var) == (expected)) ? ((var) = (desired), 1) : ((expected) = (var), 0))
#define KMM_ATOMIC_FETCH_ADD(var, val) \
    (((var) += (val)) - (val))
#endif

// ==================== KMM 功能配置 ====================
// 线程安全级别控制
// 0 = 单线程(零开销,默认)
// 1 = 轻量实时(原子操作+TLS隔离,推荐)
// 2 = 完全线程安全(额外锁保护共享资源)
#ifndef KMM_THREAD_SAFETY_LEVEL
#define KMM_THREAD_SAFETY_LEVEL 1
#endif

#define KMM_ENABLE_ARENA 1
#define KMM_ENABLE_THREAD_CACHE (KMM_THREAD_SAFETY_LEVEL >= 1)
#define KMM_ENABLE_CLEANUP_STACK 1
#define KMM_ENABLE_UNION_DOMAIN 1

// ==================== 前向类型声明 ====================
typedef struct kmm_arena kmm_arena_t;
typedef struct kmm_thread_cache kmm_thread_cache_t;
typedef struct kmm_cleanup_node kmm_cleanup_node_t;
typedef struct kmm_union_node kmm_union_node_t;
typedef struct kmm_union_domain kmm_union_domain_t;

// ==================== 枚举类型定义 ====================
// Union Domain 状态枚举
typedef enum {
    KMM_DOMAIN_LOCAL = 0,
    KMM_DOMAIN_UNION = 1,
    KMM_DOMAIN_ESCAPED = 2
} kmm_domain_status_t;

// ==================== 常量定义 ====================
// 缓存行大小（用于对齐）
#ifndef KMM_CACHE_LINE_SIZE
#define KMM_CACHE_LINE_SIZE 64
#endif

// 线程缓存大小
#ifndef KMM_THREAD_CACHE_SIZE
#define KMM_THREAD_CACHE_SIZE 1024
#endif

// 联合域配置
#ifndef KMM_MAX_UNION_NODES
#define KMM_MAX_UNION_NODES 128
#endif

#ifndef KMM_MAX_UNION_DEPTH
#define KMM_MAX_UNION_DEPTH 64
#endif

#ifndef KMM_MAX_DEPENDENCIES
#define KMM_MAX_DEPENDENCIES 32
#endif

// ==================== 结构体定义 ====================
// Arena 结构（用于分级内存管理）
struct kmm_arena {
    uint8_t* buffer;
    size_t capacity;
    size_t max_capacity;
    size_t allocations;
    size_t peak;
    size_t reset_count;
    size_t offset;
    bool is_initialized;
} __attribute__((aligned(KMM_CACHE_LINE_SIZE)));

// 清理节点
struct kmm_cleanup_node {
    void* resource;
    void (*cleanup)(void* ptr);
    struct kmm_cleanup_node* next;
};

// 线程缓存
struct kmm_thread_cache {
    void* cache[KMM_THREAD_CACHE_SIZE];
    size_t cache_size;
};

// Union Node 结构（用于联合域管理）
struct kmm_union_node {
    void* object;
    size_t object_size;
    kmm_domain_status_t status;
    size_t scope_depth;
    struct kmm_union_node* parent;
    struct kmm_union_node* next;
    struct kmm_union_node** dependencies;
    size_t dependency_count;
    bool is_root;
    bool is_elected;
    size_t temp_in_degree;
    bool temp_visited;
};

// Union Domain 结构
struct kmm_union_domain {
    struct kmm_union_node* root;
    struct kmm_union_node* current;
    size_t scope_depth;
    size_t node_count;
    size_t max_depth;
};

// ==================== 智能配置系统 ====================
// 自动检测编译器和平台
#if defined(__GNUC__) || defined(__clang__)
    #define KMM_V4_GCC_LIKE 1
    #define KMM_V4_HAS_BUILTIN(x) __builtin_expect(x, 1)
#else
    #define KMM_V4_GCC_LIKE 0
    #define KMM_V4_HAS_BUILTIN(x) (x)
#endif

#ifdef _WIN32
    #define KMM_V4_WINDOWS 1
    #if defined(__clang__) || defined(__GNUC__)
        #define KMM_TLS __thread
    #else
        #define KMM_TLS __declspec(thread)
    #endif
#else
    #define KMM_V4_WINDOWS 0
    #define KMM_TLS __thread
#endif

// 自动检测 SIMD 支持
#if defined(__AVX512F__)
    #define KMM_V4_SIMD_LEVEL 3  // AVX-512
#elif defined(__AVX2__)
    #define KMM_V4_SIMD_LEVEL 2  // AVX2
#elif defined(__SSE2__)
    #define KMM_V4_SIMD_LEVEL 1  // SSE2
#else
    #define KMM_V4_SIMD_LEVEL 0  // 无 SIMD
#endif

// 自动调整配置（智能默认值）
#ifndef KMM_V4_POOL_SIZE
    #if defined(__SIZEOF_POINTER__) && __SIZEOF_POINTER__ == 8
        #define KMM_V4_POOL_SIZE (8 * 1024 * 1024)  // 64 位系统：8MB
    #else
        #define KMM_V4_POOL_SIZE (2 * 1024 * 1024)  // 32 位系统：2MB
    #endif
#endif

#ifndef KMM_V4_ENABLE_FALLBACK
    #define KMM_V4_ENABLE_FALLBACK 0  // 默认严格模式
#endif

#ifndef KMM_V4_ALIGNMENT
    #define KMM_V4_ALIGNMENT 8  // 自动对齐
#endif

#ifndef KMM_V4_CACHE_LINE_SIZE
    #if defined(__x86_64__) || defined(_M_X64)
        #define KMM_V4_CACHE_LINE_SIZE 64  // x86-64
    #elif defined(__aarch64__)
        #define KMM_V4_CACHE_LINE_SIZE 128 // ARM64
    #else
        #define KMM_V4_CACHE_LINE_SIZE 64  // 默认
    #endif
#endif

// ==================== 编译期计算和类型推导 ====================
// 编译期常量检查
#define KMM_V4_CONSTEXPR static const

// 类型自动推导（C11 _Generic）
#define KMM_V4_TYPE_SIZE(x) _Generic((x), \
    int8_t: 1, int16_t: 2, int32_t: 4, int64_t: 8, \
    uint8_t: 1, uint16_t: 2, uint32_t: 4, uint64_t: 8, \
    float: 4, double: 8, long double: 16, \
    default: sizeof(x))

// 自动对齐计算
#define KMM_V4_ALIGN_UP(size, align) \
    (((size) + (align) - 1) & ~((align) - 1))

// 编译期检查对齐
#define KMM_V4_STATIC_ASSERT_ALIGN(ptr, align) \
    _Static_assert(((uintptr_t)(ptr) % (align)) == 0, "Alignment check failed")

// ==================== 智能内存池（自动化管理） ====================
// 内存池声明（实际定义在 .c 文件中）
extern uint8_t g_kmm_v4_pool[];

#if KMM_THREAD_SAFETY_LEVEL >= 1
extern KMM_ATOMIC_TYPE g_kmm_v4_offset;
#else
extern size_t g_kmm_v4_offset;
#endif

#ifdef KMM_V4_DEBUG
extern size_t g_kmm_v4_peak;
extern size_t g_kmm_v4_alloc_count;
#endif

// 自动预取（根据硬件能力）
#if KMM_V4_GCC_LIKE && defined(__GNUC__)
    #define KMM_V4_PREFETCH(ptr) __builtin_prefetch((ptr), 0, 3)
#else
    #define KMM_V4_PREFETCH(ptr) ((void)0)
#endif

// 分支预测优化（自动化）
#define KMM_V4_LIKELY(x)   __builtin_expect(!!(x), 1)
#define KMM_V4_UNLIKELY(x) __builtin_expect(!!(x), 0)

// ==================== 自动化分配策略 ====================
// 智能选择分配路径（轻量实时：无锁CAS原子操作）
static inline void* kmm_v4_alloc_auto(size_t size) {
    const size_t mask = KMM_V4_ALIGNMENT - 1;
    size_t aligned_size = (size + mask) & ~mask;
    
#if KMM_THREAD_SAFETY_LEVEL >= 1
    // 无锁CAS实现（轻量实时，保证实时性）
    size_t offset = KMM_ATOMIC_LOAD(g_kmm_v4_offset);
    size_t new_offset;
    do {
        new_offset = offset + aligned_size;
        if (KMM_V4_UNLIKELY(new_offset > KMM_V4_POOL_SIZE)) {
            #if KMM_V4_ENABLE_FALLBACK
                return malloc(size);
            #else
                return NULL;
            #endif
        }
    } while (KMM_V4_UNLIKELY(!KMM_ATOMIC_CAS(g_kmm_v4_offset, offset, new_offset)));
    
    #ifdef KMM_V4_DEBUG
    KMM_ATOMIC_FETCH_ADD(g_kmm_v4_alloc_count, 1);
    // 更新峰值（非严格原子，允许轻微误差）
    size_t peak = KMM_ATOMIC_LOAD(g_kmm_v4_peak);
    while (new_offset > peak) {
        if (KMM_ATOMIC_CAS(g_kmm_v4_peak, peak, new_offset)) break;
        peak = KMM_ATOMIC_LOAD(g_kmm_v4_peak);
    }
    #endif
    
    KMM_V4_PREFETCH(g_kmm_v4_pool + new_offset);
    return g_kmm_v4_pool + offset;
#else
    // 单线程快速路径（零开销）
    size_t offset = g_kmm_v4_offset;
    size_t new_offset = offset + aligned_size;
    
    if (KMM_V4_LIKELY(new_offset <= KMM_V4_POOL_SIZE)) {
        g_kmm_v4_offset = new_offset;
        
        #ifdef KMM_V4_DEBUG
        if (new_offset > g_kmm_v4_peak) g_kmm_v4_peak = new_offset;
        g_kmm_v4_alloc_count++;
        #endif
        
        KMM_V4_PREFETCH(g_kmm_v4_pool + new_offset);
        return g_kmm_v4_pool + offset;
    }
    
    // 慢速路径：自动回退
    #if KMM_V4_ENABLE_FALLBACK
        return malloc(size);
    #else
        return NULL;
    #endif
#endif
}

// ==================== 自动化 SIMD 清零 ====================
#if KMM_V4_SIMD_LEVEL >= 2
    #if defined(__AVX2__)
        #include <immintrin.h>
        static inline void kmm_v4_zero_auto(void* ptr, size_t size) {
            __m256i zero = _mm256_setzero_si256();
            uint8_t* p = (uint8_t*)ptr;
            while (size >= 32) {
                _mm256_storeu_si256((__m256i*)p, zero);
                p += 32;
                size -= 32;
            }
            if (size > 0) memset(p, 0, size);
        }
    #endif
#elif KMM_V4_SIMD_LEVEL >= 1
    #if defined(__SSE2__)
        #include <emmintrin.h>
        static inline void kmm_v4_zero_auto(void* ptr, size_t size) {
            __m128i zero = _mm_setzero_si128();
            uint8_t* p = (uint8_t*)ptr;
            while (size >= 16) {
                _mm_storeu_si128((__m128i*)p, zero);
                p += 16;
                size -= 16;
            }
            if (size > 0) memset(p, 0, size);
        }
    #endif
#else
    static inline void kmm_v4_zero_auto(void* ptr, size_t size) {
        memset(ptr, 0, size);
    }
#endif

// ==================== 智能宏系统（零成本抽象） ====================
// 类型安全分配宏（自动计算大小）
#define KMM_V4_ALLOC(type) \
    ((type*)kmm_v4_alloc_auto(sizeof(type)))

// 数组分配（自动计算元素大小和数量）
#define KMM_V4_ALLOC_ARRAY(type, count) \
    ((type*)kmm_v4_alloc_auto(sizeof(type) * (count)))

// 自动零初始化分配
#define KMM_V4_ALLOC_ZERO(type) \
    ({ type* p = KMM_V4_ALLOC(type); \
       if(p) kmm_v4_zero_auto(p, sizeof(type)); \
       p; })

// 自动批量分配（类型安全）
#define KMM_V4_ALLOC_BATCH(type, count) \
    ((type*)kmm_v4_alloc_auto(sizeof(type) * (count)))

// 结构化分配（自动对齐和清零）
#define KMM_V4_ALLOC_STRUCT(name, ...) \
    ({ typedef struct { __VA_ARGS__ } name##_t; \
       name##_t* p = KMM_V4_ALLOC(name##_t); \
       if(p) kmm_v4_zero_auto(p, sizeof(name##_t)); \
       p; })

// ==================== 自动化生命周期管理 ====================
// RAII 风格资源管理（需要编译器扩展支持）
#ifdef __GNUC__
    #define KMM_V4_AUTOFREE __attribute__((cleanup(kmm_v4_autofree_fn)))
    
    static inline void kmm_v4_autofree_fn(void* ptr) {
        (void)ptr;  // 池内对象不释放
    }
#endif

// 作用域自动清理（更简洁的语法）
#define KMM_V4_SCOPE_START \
    for (size_t kmm_v4_scope_offset __attribute__((unused)) = g_kmm_v4_offset, \
         kmm_v4_scope_used = 0; \
         kmm_v4_scope_used == 0; \
         kmm_v4_scope_used = 1, g_kmm_v4_offset = kmm_v4_scope_offset)

#define KMM_V4_SCOPE_END

// ==================== 智能统计（零成本，编译期优化） ====================
#ifdef KMM_V4_STATS
typedef struct {
    size_t total_allocs;
    size_t total_bytes;
    size_t peak_usage;
    size_t alloc_count;
    size_t free_count;
} kmm_v4_stats_t;

static kmm_v4_stats_t g_kmm_v4_stats = {0};

#define KMM_V4_RECORD_ALLOC(size) \
    do { \
        g_kmm_v4_stats.total_allocs++; \
        g_kmm_v4_stats.total_bytes += (size); \
        if (g_kmm_v4_stats.total_bytes > g_kmm_v4_stats.peak_usage) \
            g_kmm_v4_stats.peak_usage = g_kmm_v4_stats.total_bytes; \
    } while(0)
#else
    #define KMM_V4_RECORD_ALLOC(size) ((void)0)
#endif

// ==================== 自动化 API ====================

// 缓存行大小（用于对齐）
#ifndef KMM_CACHE_LINE_SIZE
#define KMM_CACHE_LINE_SIZE 64
#endif

// 完整的 KMM 上下文结构
typedef struct kmm_context {
#if KMM_ENABLE_ARENA
    kmm_arena_t tiny_arena;
    kmm_arena_t small_arena;
    kmm_arena_t medium_arena;
#endif
#if KMM_ENABLE_THREAD_CACHE
    kmm_thread_cache_t* thread_cache;
#endif
#if KMM_ENABLE_CLEANUP_STACK
    kmm_cleanup_node_t* cleanup_stack;
#endif
#if KMM_ENABLE_UNION_DOMAIN
    kmm_union_node_t* union_rep;
    kmm_union_domain_t* domain;
#endif
    size_t alloc_counter;
    size_t total_bytes;
    size_t peak_usage;
    bool is_initialized;
} kmm_context_t __attribute__((aligned(KMM_CACHE_LINE_SIZE)));

// 全局上下文实例
extern kmm_context_t g_kmm_ctx;

static inline void* kmm_v4_malloc(size_t size) {
    void* ptr = kmm_v4_alloc_auto(size);
    KMM_V4_RECORD_ALLOC(size);
    return ptr;
}

static inline void kmm_v4_free(void* ptr) {
    // 池内对象不释放，自动管理
    (void)ptr;
}

static inline void kmm_v4_reset(void) {
#if KMM_THREAD_SAFETY_LEVEL >= 1
    KMM_ATOMIC_STORE(g_kmm_v4_offset, 0);
    #ifdef KMM_V4_STATS
    memset(&g_kmm_v4_stats, 0, sizeof(g_kmm_v4_stats));
    #endif
#else
    g_kmm_v4_offset = 0;
    #ifdef KMM_V4_STATS
    memset(&g_kmm_v4_stats, 0, sizeof(g_kmm_v4_stats));
    #endif
#endif
}

static inline size_t kmm_v4_usage(void) {
#if KMM_THREAD_SAFETY_LEVEL >= 1
    return KMM_ATOMIC_LOAD(g_kmm_v4_offset);
#else
    return g_kmm_v4_offset;
#endif
}

static inline size_t kmm_v4_available(void) {
#if KMM_THREAD_SAFETY_LEVEL >= 1
    return KMM_V4_POOL_SIZE - KMM_ATOMIC_LOAD(g_kmm_v4_offset);
#else
    return KMM_V4_POOL_SIZE - g_kmm_v4_offset;
#endif
}

#define KMM_V4_ALLOC_ARRAY(type, count) ((type*)kmm_v4_alloc_auto(sizeof(type) * (count)))

// ==================== 编译期检查 ====================
_Static_assert(KMM_V4_POOL_SIZE > 0, "Pool size must be positive");
_Static_assert((KMM_V4_ALIGNMENT & (KMM_V4_ALIGNMENT - 1)) == 0, "Alignment must be power of 2");

#endif // KMM_SCOPED_ALLOCATOR_V4_H
