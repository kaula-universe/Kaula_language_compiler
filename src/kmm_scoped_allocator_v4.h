#ifndef KMM_SCOPED_ALLOCATOR_V4_H
#define KMM_SCOPED_ALLOCATOR_V4_H

#include <stdint.h>
#include <stddef.h>
#include <stdbool.h>
#include <string.h>
#include <stdlib.h>

#ifndef KAULA_FREESTANDING
// hosted 模式下包含 stdio.h（用于 debug 输出）
#include <stdio.h>
#endif

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
#define KMM_ATOMIC_TYPE _Atomic size_t
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
#ifndef KMM_ENABLE_SAFE_ALLOC
#define KMM_ENABLE_SAFE_ALLOC 1
#endif

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

// 线程本地缓冲区大小（减少全局原子操作）
#ifndef KMM_TLS_BUFFER_SIZE
#define KMM_TLS_BUFFER_SIZE (256 * 1024)  // 256KB per thread，减少全局 CAS 频率
#endif

// 批量提交粒度（减少 VirtualAlloc 调用次数）
#ifndef KMM_BATCH_COMMIT_SIZE
#define KMM_BATCH_COMMIT_SIZE (4 * 1024 * 1024)  // 4MB 批量提交
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

// 作用域栈最大深度（支持嵌套作用域）
#ifndef KMM_V4_MAX_SCOPE_DEPTH
#define KMM_V4_MAX_SCOPE_DEPTH 64
#endif

// ==================== 空闲链表配置 ====================
// Size-class free list 用于快速复用已释放的小对象
// 改善逐次分配释放场景的性能

// Size class 数量（8, 16, 32, 64, 128, 256 字节）
#ifndef KMM_FREE_LIST_CLASSES
#define KMM_FREE_LIST_CLASSES 6
#endif

// 每个 size class 的最大空闲块数量
#ifndef KMM_FREE_LIST_MAX_PER_CLASS
#define KMM_FREE_LIST_MAX_PER_CLASS 1024
#endif

// Size class 大小定义
static const size_t KMM_FREE_LIST_SIZES[KMM_FREE_LIST_CLASSES] = {8, 16, 32, 64, 128, 256};

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

// 作用域栈结构（支持嵌套作用域，每层独立保存/恢复 offset + TLAB 状态）
// 解决嵌套作用域退出时回滚外层分配的问题
// TLAB 状态保存确保 scope_pop 后 TLS 快速路径仍然正确
typedef struct {
    size_t offsets[KMM_V4_MAX_SCOPE_DEPTH];
    uint8_t* tlab_buffers[KMM_V4_MAX_SCOPE_DEPTH];
    size_t tlab_remainings[KMM_V4_MAX_SCOPE_DEPTH];
    size_t depth;
} kmm_scope_stack_t;

// 空闲链表节点
typedef struct kmm_free_node {
    struct kmm_free_node* next;
} kmm_free_node_t;

// Size-class 空闲链表
typedef struct {
    kmm_free_node_t* free_lists[KMM_FREE_LIST_CLASSES];
    size_t free_counts[KMM_FREE_LIST_CLASSES];
    bool enabled;
} kmm_free_list_t;

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

// ==================== 编译期智能自动配置系统 ====================
// 本系统自动检测编译器、平台、硬件特性，并生成最优配置
// 所有配置均可通过 -D 参数手动覆盖

// ==================== 第 1 层：编译器特性检测 ====================

// GCC/Clang 检测
#if defined(__GNUC__) || defined(__clang__)
    #define KMM_V4_GCC_LIKE 1
    #define KMM_V4_HAS_BUILTIN(x) __builtin_expect(x, 1)
#else
    #define KMM_V4_GCC_LIKE 0
    #define KMM_V4_HAS_BUILTIN(x) (x)
#endif

// Clang 特定特性
#ifdef __clang__
    #define KMM_V4_COMPILER_CLANG 1
    #define KMM_V4_COMPILER_VERSION (__clang_major__ * 10000 + __clang_minor__ * 100 + __clang_patchlevel__)
    #define KMM_V4_COMPILER_NAME "Clang"
#else
    #define KMM_V4_COMPILER_CLANG 0
#endif

// GCC 特定特性
#if defined(__GNUC__) && !defined(__clang__)
    #define KMM_V4_COMPILER_GCC 1
    #ifndef KMM_V4_COMPILER_VERSION
        #define KMM_V4_COMPILER_VERSION (__GNUC__ * 10000 + __GNUC_MINOR__ * 100 + __GNUC_PATCHLEVEL__)
    #endif
    #define KMM_V4_COMPILER_NAME "GCC"
#else
    #define KMM_V4_COMPILER_GCC 0
#endif

// MSVC 检测
#ifdef _MSC_VER
    #define KMM_V4_COMPILER_MSVC 1
    #ifndef KMM_V4_COMPILER_VERSION
        #define KMM_V4_COMPILER_VERSION _MSC_VER
    #endif
    #ifndef KMM_V4_COMPILER_NAME
        #define KMM_V4_COMPILER_NAME "MSVC"
    #endif
#else
    #define KMM_V4_COMPILER_MSVC 0
#endif

// 默认编译器名称
#ifndef KMM_V4_COMPILER_NAME
    #define KMM_V4_COMPILER_NAME "Unknown"
#endif

// C11 原子操作支持检测
#ifndef KMM_V4_HAS_C11_ATOMICS
    #if defined(__STDC_VERSION__) && __STDC_VERSION__ >= 201112L && !defined(__STDC_NO_ATOMICS__)
        #define KMM_V4_HAS_C11_ATOMICS 1
    #elif defined(__GNUC__) || defined(__clang__)
        #define KMM_V4_HAS_C11_ATOMICS 1  // GCC/Clang 内置原子操作
    #else
        #define KMM_V4_HAS_C11_ATOMICS 0
    #endif
#endif

// 线程本地存储支持检测
#ifndef KMM_V4_HAS_TLS
    #if defined(__STDC_VERSION__) && __STDC_VERSION__ >= 201112L && !defined(__STDC_NO_THREADS__)
        #define KMM_V4_HAS_TLS 1
        #define KMM_V4_TLS _Thread_local
    #elif defined(__GNUC__) || defined(__clang__)
        #define KMM_V4_HAS_TLS 1
        #define KMM_V4_TLS __thread
    #elif defined(_MSC_VER)
        #define KMM_V4_HAS_TLS 1
        #define KMM_V4_TLS __declspec(thread)
    #else
        #define KMM_V4_HAS_TLS 0
        #define KMM_V4_TLS
    #endif
#endif

// 预取指令支持
#ifndef KMM_V4_HAS_PREFETCH
    #if KMM_V4_GCC_LIKE
        #define KMM_V4_HAS_PREFETCH 1
        #define KMM_V4_PREFETCH(ptr) __builtin_prefetch((ptr), 0, 3)
    #else
        #define KMM_V4_HAS_PREFETCH 0
        #define KMM_V4_PREFETCH(ptr) ((void)0)
    #endif
#endif

// 分支预测提示
#ifndef KMM_V4_HAS_BUILTIN_EXPECT
    #if KMM_V4_GCC_LIKE
        #define KMM_V4_HAS_BUILTIN_EXPECT 1
    #else
        #define KMM_V4_HAS_BUILTIN_EXPECT 0
    #endif
#endif

#if KMM_V4_HAS_BUILTIN_EXPECT
    #define KMM_V4_LIKELY(x)   __builtin_expect(!!(x), 1)
    #define KMM_V4_UNLIKELY(x) __builtin_expect(!!(x), 0)
#else
    #define KMM_V4_LIKELY(x)   (x)
    #define KMM_V4_UNLIKELY(x) (x)
#endif

// ==================== 第 2 层：操作系统检测 ====================

#ifdef _WIN32
    #define KMM_V4_OS_WINDOWS 1
    #define KMM_V4_OS_NAME "Windows"
#else
    #define KMM_V4_OS_WINDOWS 0
#endif

#ifdef __linux__
    #define KMM_V4_OS_LINUX 1
    #define KMM_V4_OS_NAME "Linux"
#else
    #define KMM_V4_OS_LINUX 0
#endif

#ifdef __APPLE__
    #define KMM_V4_OS_MACOS 1
    #define KMM_V4_OS_NAME "macOS"
#else
    #define KMM_V4_OS_MACOS 0
#endif

// TLS 宏（基于操作系统和编译器）
#ifndef KMM_TLS
    #if KMM_V4_OS_WINDOWS
        #define KMM_TLS __declspec(thread)
    #else
        #define KMM_TLS __thread
    #endif
#endif

// ==================== Scope 栈扁平化优化（深度 <= 2）====================
// 当嵌套深度 <= 2 时，使用独立变量代替数组索引，减少内存访问开销
// 这是单对象场景最常见的嵌套深度，优化后可显著提升性能
static KMM_TLS size_t g_kmm_v4_scope_offset_0 = 0;
static KMM_TLS uint8_t* g_kmm_v4_scope_tlab_buffer_0 = NULL;
static KMM_TLS size_t g_kmm_v4_scope_tlab_remaining_0 = 0;

static KMM_TLS size_t g_kmm_v4_scope_offset_1 = 0;
static KMM_TLS uint8_t* g_kmm_v4_scope_tlab_buffer_1 = NULL;
static KMM_TLS size_t g_kmm_v4_scope_tlab_remaining_1 = 0;

// ==================== 第 3 层：CPU 架构检测 ====================

// x86_64
#if defined(__x86_64__) || defined(_M_X64)
    #define KMM_V4_ARCH_X86_64 1
    #define KMM_V4_ARCH_NAME "x86_64"
#else
    #define KMM_V4_ARCH_X86_64 0
#endif

// ARM64
#if defined(__aarch64__) || defined(_M_ARM64)
    #define KMM_V4_ARCH_ARM64 1
    #define KMM_V4_ARCH_NAME "ARM64"
#else
    #define KMM_V4_ARCH_ARM64 0
#endif

// x86 (32-bit)
#if defined(__i386__) || defined(_M_IX86)
    #define KMM_V4_ARCH_X86 1
    #define KMM_V4_ARCH_NAME "x86"
#else
    #define KMM_V4_ARCH_X86 0
#endif

// ARM (32-bit)
#if defined(__arm__) || defined(_M_ARM)
    #define KMM_V4_ARCH_ARM 1
    #define KMM_V4_ARCH_NAME "ARM"
#else
    #define KMM_V4_ARCH_ARM 0
#endif

// 指针大小（自动检测 64位/32位）
#ifndef KMM_V4_POINTER_SIZE
    #if defined(__SIZEOF_POINTER__)
        #define KMM_V4_POINTER_SIZE __SIZEOF_POINTER__
    #elif KMM_V4_ARCH_X86_64 || KMM_V4_ARCH_ARM64
        #define KMM_V4_POINTER_SIZE 8
    #else
        #define KMM_V4_POINTER_SIZE 4
    #endif
#endif

// ==================== 第 4 层：SIMD 指令集检测 ====================

#ifndef KMM_V4_SIMD_LEVEL
    #if defined(__AVX512F__)
        #define KMM_V4_SIMD_LEVEL 3  // AVX-512
        #define KMM_V4_SIMD_NAME "AVX-512"
    #elif defined(__AVX2__)
        #define KMM_V4_SIMD_LEVEL 2  // AVX2
        #define KMM_V4_SIMD_NAME "AVX2"
    #elif defined(__SSE2__) || defined(_M_X64)
        #define KMM_V4_SIMD_LEVEL 1  // SSE2
        #define KMM_V4_SIMD_NAME "SSE2"
    #elif defined(__ARM_NEON)
        #define KMM_V4_SIMD_LEVEL 1  // ARM NEON
        #define KMM_V4_SIMD_NAME "NEON"
    #else
        #define KMM_V4_SIMD_LEVEL 0  // 无 SIMD
        #define KMM_V4_SIMD_NAME "None"
    #endif
#endif

// ==================== 第 5 层：缓存和内存层次检测 ====================

// 缓存行大小（基于架构）
#ifndef KMM_CACHE_LINE_SIZE
    #if KMM_V4_ARCH_X86_64 || KMM_V4_ARCH_X86
        #define KMM_CACHE_LINE_SIZE 64
    #elif KMM_V4_ARCH_ARM64
        #define KMM_CACHE_LINE_SIZE 128
    #elif KMM_V4_ARCH_ARM
        #define KMM_CACHE_LINE_SIZE 64
    #else
        #define KMM_CACHE_LINE_SIZE 64
    #endif
#endif

// L1 缓存大小估算（用于 TLAB 大小优化）
#ifndef KMM_V4_L1_CACHE_SIZE
    #if KMM_V4_ARCH_X86_64
        #define KMM_V4_L1_CACHE_SIZE (32 * 1024)   // 典型 32KB
    #elif KMM_V4_ARCH_ARM64
        #define KMM_V4_L1_CACHE_SIZE (64 * 1024)   // 典型 64KB
    #else
        #define KMM_V4_L1_CACHE_SIZE (32 * 1024)
    #endif
#endif

// 页面大小（基于操作系统）
#ifndef KMM_V4_PAGE_SIZE
    #if KMM_V4_OS_WINDOWS
        #define KMM_V4_PAGE_SIZE 4096
    #elif KMM_V4_OS_LINUX || KMM_V4_OS_MACOS
        #if KMM_V4_ARCH_ARM64
            #define KMM_V4_PAGE_SIZE 16384  // ARM64 常用 16KB 页面
        #else
            #define KMM_V4_PAGE_SIZE 4096
        #endif
    #else
        #define KMM_V4_PAGE_SIZE 4096
    #endif
#endif

// ==================== 第 6 层：性能模式自动选择 ====================

// 调试模式检测
#ifndef KMM_V4_DEBUG_MODE
    #if defined(DEBUG) || defined(_DEBUG) || defined(KMM_V4_DEBUG)
        #define KMM_V4_DEBUG_MODE 1
    #else
        #define KMM_V4_DEBUG_MODE 0
    #endif
#endif

// 优化级别检测
#ifndef KMM_V4_OPT_LEVEL
    #if defined(__OPTIMIZE__)
        #if defined(__OPTIMIZE_SIZE__)
            #define KMM_V4_OPT_LEVEL 1  // -Os
        #else
            #define KMM_V4_OPT_LEVEL 2  // -O2 or -O3
        #endif
    #else
        #define KMM_V4_OPT_LEVEL 0  // -O0
    #endif
#endif

// 自动选择线程安全级别
#ifndef KMM_THREAD_SAFETY_LEVEL
    #if KMM_V4_DEBUG_MODE
        #define KMM_THREAD_SAFETY_LEVEL 2  // 调试模式：完全线程安全
    #elif KMM_V4_OPT_LEVEL >= 2
        #define KMM_THREAD_SAFETY_LEVEL 1  // 优化模式：轻量实时
    #else
        #define KMM_THREAD_SAFETY_LEVEL 0  // 未优化：单线程
    #endif
#endif

// 自动选择 TLAB 大小（基于 L1 缓存）
#ifndef KMM_TLS_BUFFER_SIZE
    #if KMM_V4_OPT_LEVEL >= 2
        // 优化模式：使用较大的 TLAB（L1 缓存的 4 倍）
        #define KMM_TLS_BUFFER_SIZE (KMM_V4_L1_CACHE_SIZE * 4)
    #else
        // 调试模式：使用较小的 TLAB（L1 缓存大小）
        #define KMM_TLS_BUFFER_SIZE KMM_V4_L1_CACHE_SIZE
    #endif
#endif

// 自动选择批量提交粒度（基于页面大小）
#ifndef KMM_BATCH_COMMIT_SIZE
    #if KMM_V4_OPT_LEVEL >= 2
        #define KMM_BATCH_COMMIT_SIZE (KMM_V4_PAGE_SIZE * 1024)  // 4MB
    #else
        #define KMM_BATCH_COMMIT_SIZE (KMM_V4_PAGE_SIZE * 256)   // 1MB
    #endif
#endif

// 自动选择内存池大小（基于指针大小和架构）
#ifndef KMM_V4_POOL_SIZE
    #if KMM_V4_POINTER_SIZE == 8
        #if KMM_V4_ARCH_X86_64 || KMM_V4_ARCH_ARM64
            #define KMM_V4_POOL_SIZE (256 * 1024 * 1024)  // 64位：256MB
        #else
            #define KMM_V4_POOL_SIZE (64 * 1024 * 1024)   // 64位其他：64MB
        #endif
    #else
        #define KMM_V4_POOL_SIZE (16 * 1024 * 1024)       // 32位：16MB
    #endif
#endif

// 自动选择对齐方式
#ifndef KMM_V4_ALIGNMENT
    #if KMM_V4_SIMD_LEVEL >= 3
        #define KMM_V4_ALIGNMENT 64  // AVX-512：64字节对齐
    #elif KMM_V4_SIMD_LEVEL >= 2
        #define KMM_V4_ALIGNMENT 32  // AVX2：32字节对齐
    #elif KMM_V4_SIMD_LEVEL >= 1
        #define KMM_V4_ALIGNMENT 16  // SSE/NEON：16字节对齐
    #else
        #define KMM_V4_ALIGNMENT 8   // 无 SIMD：8字节对齐
    #endif
#endif

// 自动选择回退策略
#ifndef KMM_V4_ENABLE_FALLBACK
    #if KMM_V4_DEBUG_MODE
        #define KMM_V4_ENABLE_FALLBACK 1  // 调试模式：允许回退到 malloc
    #else
        #define KMM_V4_ENABLE_FALLBACK 0  // 发布模式：严格模式
    #endif
#endif

// ==================== 第 7 层：功能开关自动配置 ====================

#ifndef KMM_ENABLE_ARENA
    #define KMM_ENABLE_ARENA 1
#endif

#ifndef KMM_ENABLE_THREAD_CACHE
    #define KMM_ENABLE_THREAD_CACHE (KMM_THREAD_SAFETY_LEVEL >= 1)
#endif

#ifndef KMM_ENABLE_CLEANUP_STACK
    #define KMM_ENABLE_CLEANUP_STACK 1
#endif

#ifndef KMM_ENABLE_UNION_DOMAIN
    #define KMM_ENABLE_UNION_DOMAIN 1
#endif

#ifndef KMM_ENABLE_SAFE_ALLOC
    #if KMM_V4_DEBUG_MODE
        #define KMM_ENABLE_SAFE_ALLOC 1  // 调试模式：启用安全分配
    #else
        #define KMM_ENABLE_SAFE_ALLOC 0  // 发布模式：禁用（减少开销）
    #endif
#endif

// ==================== 第 8 层：编译期验证 ====================

// 确保关键配置的有效性
_Static_assert(KMM_V4_POOL_SIZE > 0, "Pool size must be positive");
_Static_assert((KMM_V4_ALIGNMENT & (KMM_V4_ALIGNMENT - 1)) == 0, "Alignment must be power of 2");
_Static_assert(KMM_V4_ALIGNMENT >= 8, "Alignment must be at least 8 bytes");
_Static_assert(KMM_CACHE_LINE_SIZE >= 16, "Cache line size must be at least 16 bytes");
_Static_assert(KMM_V4_PAGE_SIZE >= 4096, "Page size must be at least 4KB");
_Static_assert(KMM_TLS_BUFFER_SIZE >= KMM_V4_PAGE_SIZE, "TLAB size must be at least one page");

// ==================== 第 9 层：配置信息输出（调试用）====================

#ifdef KMM_V4_PRINT_CONFIG
    #pragma message("KMM_V4 Configuration:")
    #pragma message("  Compiler: " KMM_V4_OS_NAME)
    #pragma message("  OS: " KMM_V4_OS_NAME)
    #pragma message("  Arch: " KMM_V4_ARCH_NAME)
    #pragma message("  SIMD: " KMM_V4_SIMD_NAME)
    #pragma message("  Pointer Size: " KMM_V4_STRINGIFY(KMM_V4_POINTER_SIZE) " bytes")
    #pragma message("  Cache Line: " KMM_V4_STRINGIFY(KMM_CACHE_LINE_SIZE) " bytes")
    #pragma message("  Page Size: " KMM_V4_STRINGIFY(KMM_V4_PAGE_SIZE) " bytes")
    #pragma message("  Pool Size: " KMM_V4_STRINGIFY(KMM_V4_POOL_SIZE) " bytes")
    #pragma message("  TLAB Size: " KMM_V4_STRINGIFY(KMM_TLS_BUFFER_SIZE) " bytes")
    #pragma message("  Alignment: " KMM_V4_STRINGIFY(KMM_V4_ALIGNMENT) " bytes")
    #pragma message("  Thread Safety: Level " KMM_V4_STRINGIFY(KMM_THREAD_SAFETY_LEVEL))
    #pragma message("  Debug Mode: " KMM_V4_STRINGIFY(KMM_V4_DEBUG_MODE))
    #pragma message("  Opt Level: " KMM_V4_STRINGIFY(KMM_V4_OPT_LEVEL))
#endif

// 辅助宏：将值转换为字符串
#define KMM_V4_STRINGIFY_IMPL(x) #x
#define KMM_V4_STRINGIFY(x) KMM_V4_STRINGIFY_IMPL(x)

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
// 内存池声明（实际定义在 .c 文件中，由 VirtualAlloc 分配）
#ifdef KMM_V4_STATIC_POOL
extern uint8_t g_kmm_v4_pool[KMM_V4_POOL_SIZE];
static inline void kmm_v4_pool_commit(size_t needed) { (void)needed; }
#else
extern uint8_t* g_kmm_v4_pool;
extern void kmm_v4_pool_commit(size_t needed);
#endif
extern size_t g_kmm_v4_pool_capacity;

#if KMM_THREAD_SAFETY_LEVEL >= 1
extern KMM_ATOMIC_TYPE g_kmm_v4_offset;
#else
extern size_t g_kmm_v4_offset;
#endif

// 作用域栈（线程本地，支持嵌套作用域）
extern KMM_TLS kmm_scope_stack_t g_kmm_v4_scope_stack;

// ==================== 线程本地缓冲区（减少全局原子操作）====================
// 每个线程拥有独立的缓冲区，避免全局 CAS 竞争
typedef struct {
    uint8_t* buffer;           // 线程本地缓冲区指针
    size_t remaining;          // 剩余可用字节
    size_t total_allocated;    // 该线程已分配总量（用于统计）
} kmm_tls_buffer_t;

extern KMM_TLS kmm_tls_buffer_t g_kmm_v4_tls_buffer;

#ifdef KMM_V4_DEBUG
#if KMM_THREAD_SAFETY_LEVEL >= 1
extern KMM_ATOMIC_TYPE g_kmm_v4_peak;
extern KMM_ATOMIC_TYPE g_kmm_v4_alloc_count;
#else
extern size_t g_kmm_v4_peak;
extern size_t g_kmm_v4_alloc_count;
#endif
#endif

// 分支预测提示（已在智能配置系统中定义，此处不再重复）
// KMM_V4_LIKELY 和 KMM_V4_UNLIKELY 已在第 1 层定义

// TLAB 填充函数声明（实现在 .c 文件中）
// 必须在 kmm_v4_bump 和 kmm_v4_alloc_auto 之前声明
extern uint8_t* kmm_v4_tlab_refill(void);
extern void kmm_v4_tlab_invalidate(void);

// ==================== 批量分配 API（编译器生成，减少原子操作次数）====================

#ifndef KMM_V4_BUMP_IMPL
// kmm_v4_bump: 批量分配，单次原子加法分配 total_size 字节，返回起始地址
// 用于编译器将同一 scope 内的多次 malloc 合并为一次分配
// 优化：使用 TLAB 快速路径，减少全局 CAS
static inline void* kmm_v4_bump(size_t total_size) {
    const size_t mask = KMM_V4_ALIGNMENT - 1;
    size_t aligned_size = (total_size + mask) & ~mask;

    // 快速路径：从 TLAB 分配（无原子操作）
    if (KMM_V4_LIKELY(g_kmm_v4_tls_buffer.remaining >= aligned_size)) {
        uint8_t* ptr = g_kmm_v4_tls_buffer.buffer;
        g_kmm_v4_tls_buffer.buffer += aligned_size;
        g_kmm_v4_tls_buffer.remaining -= aligned_size;
        return ptr;
    }
    
    // TLAB 耗尽，尝试填充
    if (KMM_V4_LIKELY(kmm_v4_tlab_refill() != NULL)) {
        if (KMM_V4_LIKELY(g_kmm_v4_tls_buffer.remaining >= aligned_size)) {
            uint8_t* ptr = g_kmm_v4_tls_buffer.buffer;
            g_kmm_v4_tls_buffer.buffer += aligned_size;
            g_kmm_v4_tls_buffer.remaining -= aligned_size;
            return ptr;
        }
    }

    // 慢路径：直接从全局池分配（CAS 循环）
#if KMM_THREAD_SAFETY_LEVEL >= 1
    size_t offset = KMM_ATOMIC_LOAD(g_kmm_v4_offset);
    size_t new_offset;
    do {
        new_offset = offset + aligned_size;
        if (KMM_V4_UNLIKELY(new_offset > g_kmm_v4_pool_capacity)) {
            return NULL;
        }
    } while (KMM_V4_UNLIKELY(!KMM_ATOMIC_CAS(g_kmm_v4_offset, offset, new_offset)));

    kmm_v4_pool_commit(new_offset);
    return g_kmm_v4_pool + offset;
#else
    size_t offset = g_kmm_v4_offset;
    size_t new_offset = offset + aligned_size;

    if (KMM_V4_LIKELY(new_offset <= g_kmm_v4_pool_capacity)) {
        g_kmm_v4_offset = new_offset;
        kmm_v4_pool_commit(new_offset);
        return g_kmm_v4_pool + offset;
    }
    return NULL;
#endif
}
#endif

#ifndef KMM_V4_OFFSET_SAVE_IMPL
// kmm_v4_offset_save: 保存当前 offset（用于 scope 优化）
static inline size_t kmm_v4_offset_save(void) {
#if KMM_THREAD_SAFETY_LEVEL >= 1
    return KMM_ATOMIC_LOAD(g_kmm_v4_offset);
#else
    return g_kmm_v4_offset;
#endif
}
#endif

#ifndef KMM_V4_OFFSET_RESTORE_IMPL
// kmm_v4_offset_restore: 直接恢复 offset（跳过 scope 栈，用于简单 scope 模式）
static inline void kmm_v4_offset_restore(size_t saved) {
#if KMM_THREAD_SAFETY_LEVEL >= 1
    KMM_ATOMIC_STORE(g_kmm_v4_offset, saved);
#else
    g_kmm_v4_offset = saved;
#endif
}
#endif

// ==================== 自动化分配策略 ====================
// 智能选择分配路径（TLAB 快速路径 + 全局 CAS 慢路径）
// TLAB: Thread Local Allocation Buffer，每个线程从全局池批量获取 256KB
// 快速路径：从 TLS 缓冲区分配，无原子操作，无锁
// 慢路径：TLAB 耗尽时，通过 CAS 从全局池获取新的 TLAB

// TLAB 填充函数声明（实现在 .c 文件中）
// 必须在 kmm_v4_alloc_auto 之前声明
extern uint8_t* kmm_v4_tlab_refill(void);
extern void kmm_v4_tlab_invalidate(void);

static inline void* kmm_v4_alloc_auto(size_t size) {
    const size_t mask = KMM_V4_ALIGNMENT - 1;
    size_t aligned_size = (size + mask) & ~mask;
    
    // 快速路径：从 TLAB 分配（无原子操作，无锁）
    // 只有当分配大小 <= TLAB 大小的 1/4 时才使用 TLAB
    // 大对象直接走全局 CAS，避免浪费 TLAB 空间
    if (KMM_V4_LIKELY(aligned_size <= KMM_TLS_BUFFER_SIZE / 4)) {
        if (KMM_V4_LIKELY(g_kmm_v4_tls_buffer.remaining >= aligned_size)) {
            uint8_t* ptr = g_kmm_v4_tls_buffer.buffer;
            g_kmm_v4_tls_buffer.buffer += aligned_size;
            g_kmm_v4_tls_buffer.remaining -= aligned_size;
            
            #ifdef KMM_V4_DEBUG
            KMM_ATOMIC_FETCH_ADD(g_kmm_v4_alloc_count, 1);
            #endif
            
            return ptr;
        }
        
        // TLAB 耗尽，尝试填充
        if (KMM_V4_LIKELY(kmm_v4_tlab_refill() != NULL)) {
            if (KMM_V4_LIKELY(g_kmm_v4_tls_buffer.remaining >= aligned_size)) {
                uint8_t* ptr = g_kmm_v4_tls_buffer.buffer;
                g_kmm_v4_tls_buffer.buffer += aligned_size;
                g_kmm_v4_tls_buffer.remaining -= aligned_size;
                
                #ifdef KMM_V4_DEBUG
                KMM_ATOMIC_FETCH_ADD(g_kmm_v4_alloc_count, 1);
                #endif
                
                return ptr;
            }
        }
    }
    
    // 慢路径：直接从全局池分配（CAS 循环）
    // 或者 TLAB 填充失败，回退到全局分配
#if KMM_THREAD_SAFETY_LEVEL >= 1
    size_t offset = KMM_ATOMIC_LOAD(g_kmm_v4_offset);
    size_t new_offset;
    do {
        new_offset = offset + aligned_size;
        if (KMM_V4_UNLIKELY(new_offset > g_kmm_v4_pool_capacity)) {
            #if KMM_V4_ENABLE_FALLBACK
                return malloc(size);
            #else
                return NULL;
            #endif
        }
    } while (KMM_V4_UNLIKELY(!KMM_ATOMIC_CAS(g_kmm_v4_offset, offset, new_offset)));
    
    // 按需提交页面
    kmm_v4_pool_commit(new_offset);
    
    #ifdef KMM_V4_DEBUG
    KMM_ATOMIC_FETCH_ADD(g_kmm_v4_alloc_count, 1);
    size_t peak = KMM_ATOMIC_LOAD(g_kmm_v4_peak);
    while (new_offset > peak) {
        if (KMM_ATOMIC_CAS(g_kmm_v4_peak, peak, new_offset)) break;
        peak = KMM_ATOMIC_LOAD(g_kmm_v4_peak);
    }
    #endif
    
    KMM_V4_PREFETCH(g_kmm_v4_pool + new_offset);
    return g_kmm_v4_pool + offset;
#else
    size_t offset = g_kmm_v4_offset;
    size_t new_offset = offset + aligned_size;
    
    if (KMM_V4_LIKELY(new_offset <= g_kmm_v4_pool_capacity)) {
        g_kmm_v4_offset = new_offset;
        
        // 按需提交页面
        kmm_v4_pool_commit(new_offset);
        
        #ifdef KMM_V4_DEBUG
        if (new_offset > g_kmm_v4_peak) g_kmm_v4_peak = new_offset;
        g_kmm_v4_alloc_count++;
        #endif
        
        KMM_V4_PREFETCH(g_kmm_v4_pool + new_offset);
        return g_kmm_v4_pool + offset;
    }
    
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
#ifndef KMM_V4_ALLOC
#define KMM_V4_ALLOC(type) \
    ((type*)kmm_v4_alloc_auto(sizeof(type)))
#endif

// 数组分配（自动计算元素大小和数量）
#ifndef KMM_V4_ALLOC_ARRAY
#define KMM_V4_ALLOC_ARRAY(type, count) \
    ((type*)kmm_v4_alloc_auto(sizeof(type) * (count)))
#endif

// 自动零初始化分配
#ifndef KMM_V4_ALLOC_ZERO
#define KMM_V4_ALLOC_ZERO(type) \
    ({ type* p = KMM_V4_ALLOC(type); \
       if(p) kmm_v4_zero_auto(p, sizeof(type)); \
       p; })
#endif

// 自动批量分配（类型安全）
#define KMM_V4_ALLOC_BATCH(type, count) \
    ((type*)kmm_v4_alloc_auto(sizeof(type) * (count)))

// 结构化分配（自动对齐和清零）
#define KMM_V4_ALLOC_STRUCT(name, ...) \
    ({ typedef struct { __VA_ARGS__ } name##_t; \
       name##_t* p = KMM_V4_ALLOC(name##_t); \
       if(p) kmm_v4_zero_auto(p, sizeof(name##_t)); \
       p; })

// 作用域栈操作函数（嵌套作用域支持）
// kmm_v4_scope_push: 进入作用域，保存当前 offset + TLAB 状态到作用域栈
// kmm_v4_scope_pop: 退出作用域，恢复到本层开始时的 offset + TLAB 状态
// 注意：定义必须放在头文件中，因为 static inline 函数具有内部链接，
// 跨编译单元不可见。将定义放在 .c 文件中会导致链接错误。
//
// 优化：Scope 栈扁平化（深度 <= 2）
// 当嵌套深度 <= 2 时，使用独立 TLS变量代替数组索引，减少内存访问开销
// 这是单对象场景最常见的嵌套深度，优化后可显著提升性能
static inline void kmm_v4_scope_push(void) {
    kmm_scope_stack_t* stack = &g_kmm_v4_scope_stack;
    size_t depth = stack->depth;
    
    // 快速路径：深度 <= 2 时使用扁平化变量（零数组索引开销）
    if (KMM_V4_LIKELY(depth <= 1)) {
        #if KMM_THREAD_SAFETY_LEVEL >= 1
        size_t current_offset = KMM_ATOMIC_LOAD(g_kmm_v4_offset);
        #else
        size_t current_offset = g_kmm_v4_offset;
        #endif
        
        if (depth == 0) {
            g_kmm_v4_scope_offset_0 = current_offset;
            g_kmm_v4_scope_tlab_buffer_0 = g_kmm_v4_tls_buffer.buffer;
            g_kmm_v4_scope_tlab_remaining_0 = g_kmm_v4_tls_buffer.remaining;
        } else { // depth == 1
            g_kmm_v4_scope_offset_1 = current_offset;
            g_kmm_v4_scope_tlab_buffer_1 = g_kmm_v4_tls_buffer.buffer;
            g_kmm_v4_scope_tlab_remaining_1 = g_kmm_v4_tls_buffer.remaining;
        }
        stack->depth++;
        return;
    }
    
    // 慢路径：深度 > 2 时使用数组索引
    if (KMM_V4_UNLIKELY(depth >= KMM_V4_MAX_SCOPE_DEPTH)) {
        #ifdef KMM_V4_DEBUG
        fprintf(stderr, "KMM ERROR: Scope stack overflow (max depth: %d)\n", KMM_V4_MAX_SCOPE_DEPTH);
        #endif
        return;
    }
    size_t idx = depth;
    stack->depth++;
    #if KMM_THREAD_SAFETY_LEVEL >= 1
    stack->offsets[idx] = KMM_ATOMIC_LOAD(g_kmm_v4_offset);
    #else
    stack->offsets[idx] = g_kmm_v4_offset;
    #endif
    // 保存 TLAB 状态
    stack->tlab_buffers[idx] = g_kmm_v4_tls_buffer.buffer;
    stack->tlab_remainings[idx] = g_kmm_v4_tls_buffer.remaining;
}

static inline void kmm_v4_scope_pop(void) {
    kmm_scope_stack_t* stack = &g_kmm_v4_scope_stack;
    if (KMM_V4_UNLIKELY(stack->depth == 0)) {
        #ifdef KMM_V4_DEBUG
        fprintf(stderr, "KMM ERROR: Scope stack underflow\n");
        #endif
        return;
    }
    
    size_t new_depth = stack->depth - 1;
    stack->depth = new_depth;
    
    // 快速路径：深度 <= 2 时使用扁平化变量（零数组索引开销）
    if (KMM_V4_LIKELY(new_depth <= 1)) {
        size_t saved_offset;
        uint8_t* saved_tlab_buffer;
        size_t saved_tlab_remaining;
        
        if (new_depth == 0) {
            saved_offset = g_kmm_v4_scope_offset_0;
            saved_tlab_buffer = g_kmm_v4_scope_tlab_buffer_0;
            saved_tlab_remaining = g_kmm_v4_scope_tlab_remaining_0;
        } else { // new_depth == 1
            saved_offset = g_kmm_v4_scope_offset_1;
            saved_tlab_buffer = g_kmm_v4_scope_tlab_buffer_1;
            saved_tlab_remaining = g_kmm_v4_scope_tlab_remaining_1;
        }
        
        #if KMM_THREAD_SAFETY_LEVEL >= 1
        KMM_ATOMIC_STORE(g_kmm_v4_offset, saved_offset);
        #else
        g_kmm_v4_offset = saved_offset;
        #endif
        // 恢复 TLAB 状态（确保 scope_pop 后 TLS 快速路径仍然正确）
        g_kmm_v4_tls_buffer.buffer = saved_tlab_buffer;
        g_kmm_v4_tls_buffer.remaining = saved_tlab_remaining;
        return;
    }
    
    // 慢路径：深度 > 2 时使用数组索引
    size_t idx = new_depth;
    size_t saved_offset = stack->offsets[idx];
    #if KMM_THREAD_SAFETY_LEVEL >= 1
    KMM_ATOMIC_STORE(g_kmm_v4_offset, saved_offset);
    #else
    g_kmm_v4_offset = saved_offset;
    #endif
    // 恢复 TLAB 状态（确保 scope_pop 后 TLS 快速路径仍然正确）
    g_kmm_v4_tls_buffer.buffer = stack->tlab_buffers[idx];
    g_kmm_v4_tls_buffer.remaining = stack->tlab_remainings[idx];
}

// 作用域自动清理（支持嵌套，每层独立管理）
// 使用 do-while 结构确保在进入块前执行 push，退出块时执行 pop
// 解决嵌套作用域退出时回滚外层分配的问题
#define KMM_V4_SCOPE_START \
    kmm_v4_scope_push(); \
    do

#define KMM_V4_SCOPE_END \
    while (0); \
    kmm_v4_scope_pop()

// 别名：兼容 stdlib 中的命名
#define kmm_v4_scope_enter kmm_v4_scope_push
#define kmm_v4_scope_exit kmm_v4_scope_pop

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

// KMM_CACHE_LINE_SIZE 已在常量定义区定义，此处不再重复

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

// ==================== 编译期检查 ====================
// kmm_v4_malloc / kmm_v4_free / kmm_v4_calloc 由 .c 文件提供（非 static，支持跨 TU 链接）

#ifndef KMM_V4_RESET_IMPL
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
    // 失效当前线程的 TLAB，强制下次分配时重新填充
    kmm_v4_tlab_invalidate();
}
#endif

#ifndef KMM_V4_USAGE_IMPL
static inline size_t kmm_v4_usage(void) {
#if KMM_THREAD_SAFETY_LEVEL >= 1
    return KMM_ATOMIC_LOAD(g_kmm_v4_offset);
#else
    return g_kmm_v4_offset;
#endif
}
#endif

#ifndef KMM_V4_AVAILABLE_IMPL
static inline size_t kmm_v4_available(void) {
#if KMM_THREAD_SAFETY_LEVEL >= 1
    return KMM_V4_POOL_SIZE - KMM_ATOMIC_LOAD(g_kmm_v4_offset);
#else
    return KMM_V4_POOL_SIZE - g_kmm_v4_offset;
#endif
}
#endif

// KMM_V4_ALLOC_ARRAY 已在智能宏系统区定义，此处不再重复

// ==================== 兼容 API: malloc/calloc/free/strdup ====================
// 为旧代码提供兼容接口，底层基于 bump allocator
// 动态内存池模式：实现在 kmm_scoped_allocator_v4.c 中
// 静态内存池模式：使用内联版本

#ifdef KMM_V4_STATIC_POOL
static inline void* kmm_v4_malloc(size_t size) {
    return kmm_v4_alloc_auto(size);
}

static inline void* kmm_v4_calloc(size_t num, size_t size) {
    size_t total = num * size;
    void* p = kmm_v4_alloc_auto(total);
    if (p) kmm_v4_zero_auto(p, total);
    return p;
}

static inline void kmm_v4_free(void* ptr) {
    (void)ptr;
}

static inline void* kmm_v4_strdup(const char* s) {
    if (!s) return NULL;
    size_t len = strlen(s) + 1;
    void* p = kmm_v4_alloc_auto(len);
    if (p) memcpy(p, s, len);
    return p;
}

static inline void kmm_v4_init_pool(size_t reserved) {
    (void)reserved;
    KMM_ATOMIC_STORE(g_kmm_v4_offset, 0);
}

static inline void kmm_v4_destroy_pool(void) {
    KMM_ATOMIC_STORE(g_kmm_v4_offset, 0);
}
#else
void* kmm_v4_malloc(size_t size);
void* kmm_v4_calloc(size_t num, size_t size);
void  kmm_v4_free(void* ptr);
void* kmm_v4_strdup(const char* s);
void  kmm_v4_init_pool(size_t reserved);
void  kmm_v4_destroy_pool(void);
#endif

// ==================== 编译期检查 ====================
_Static_assert(KMM_V4_POOL_SIZE > 0, "Pool size must be positive");
_Static_assert((KMM_V4_ALIGNMENT & (KMM_V4_ALIGNMENT - 1)) == 0, "Alignment must be power of 2");

#endif // KMM_SCOPED_ALLOCATOR_V4_H