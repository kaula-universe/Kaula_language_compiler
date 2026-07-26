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

// ==================== 辅助宏（必须在配置信息输出之前定义） ====================
#define KMM_V4_STRINGIFY_IMPL(x) #x
#define KMM_V4_STRINGIFY(x) KMM_V4_STRINGIFY_IMPL(x)

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
    #ifndef KMM_V4_COMPILER_NAME
        #define KMM_V4_COMPILER_NAME "GCC"
    #endif
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

// TLS 宏（基于编译器优先，兼容 Clang on Windows）
// 修复：Clang 在 Windows 下将 __declspec(thread) 展开为 __attribute__((thread))，
// 但 Clang 不支持 thread 属性，需要使用 __thread
#ifndef KMM_TLS
    #if defined(__GNUC__) || defined(__clang__)
        #define KMM_TLS __thread
    #elif KMM_V4_OS_WINDOWS
        #define KMM_TLS __declspec(thread)
    #else
        #define KMM_TLS __thread
    #endif
#endif

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
    #if KMM_V4_ARCH_ARM64
        #define KMM_CACHE_LINE_SIZE 128
    #else
        #define KMM_CACHE_LINE_SIZE 64
    #endif
#endif

// L1 缓存大小估算（用于 TLAB 大小优化）
#ifndef KMM_V4_L1_CACHE_SIZE
    #if KMM_V4_ARCH_ARM64
        #define KMM_V4_L1_CACHE_SIZE (64 * 1024)   // 典型 64KB
    #else
        #define KMM_V4_L1_CACHE_SIZE (32 * 1024)   // 典型 32KB
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
// 注意：第 6/7 层是配置的唯一定义点。
// 前面不再硬编码默认值，用户可通过 -D 覆盖。

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

// 线程安全级别控制
// 0 = 单线程(零开销)
// 1 = 轻量实时(原子操作+per-thread heap,推荐)
// 2 = 完全线程安全(额外锁保护共享资源)
#ifndef KMM_THREAD_SAFETY_LEVEL
    #if KMM_V4_DEBUG_MODE
        #define KMM_THREAD_SAFETY_LEVEL 2  // 调试模式：完全线程安全
    #elif KMM_V4_OPT_LEVEL >= 2
        #define KMM_THREAD_SAFETY_LEVEL 1  // 优化模式：轻量实时
    #else
        #define KMM_THREAD_SAFETY_LEVEL 0  // 未优化：单线程
    #endif
#endif

// 原子操作支持（基于线程安全级别）
// 修复 #14：区分循环 CAS（weak）和非循环 CAS（strong）
#if KMM_THREAD_SAFETY_LEVEL >= 1
#ifdef __STDC_NO_ATOMICS__
// C11 不支持原子操作，使用 GCC/Clang 内置函数
#define KMM_USE_ATOMICS 1
#define KMM_ATOMIC_TYPE unsigned long
#define KMM_ATOMIC_LOAD(var) __atomic_load_n(&(var), __ATOMIC_RELAXED)
#define KMM_ATOMIC_STORE(var, val) __atomic_store_n(&(var), (val), __ATOMIC_RELAXED)
// 循环 CAS（允许伪失败，循环重试）
#define KMM_ATOMIC_CAS_WEAK(var, expected, desired) \
    __atomic_compare_exchange_n(&(var), &(expected), (desired), 1, __ATOMIC_ACQUIRE, __ATOMIC_RELAXED)
// 非循环 CAS（不允许伪失败，单次判断）
#define KMM_ATOMIC_CAS_STRONG(var, expected, desired) \
    __atomic_compare_exchange_n(&(var), &(expected), (desired), 0, __ATOMIC_ACQUIRE, __ATOMIC_RELAXED)
#define KMM_ATOMIC_FETCH_ADD(var, val) \
    __atomic_fetch_add(&(var), (val), __ATOMIC_RELAXED)
#else
// 使用 C11 标准原子操作
#define KMM_USE_ATOMICS 1
#include <stdatomic.h>
#define KMM_ATOMIC_TYPE _Atomic size_t
#define KMM_ATOMIC_LOAD(var) atomic_load(&(var))
#define KMM_ATOMIC_STORE(var, val) atomic_store(&(var), (val))
// 循环 CAS（weak，允许伪失败）
#define KMM_ATOMIC_CAS_WEAK(var, expected, desired) \
    atomic_compare_exchange_weak(&(var), &(expected), (desired))
// 非循环 CAS（strong，不允许伪失败）
#define KMM_ATOMIC_CAS_STRONG(var, expected, desired) \
    atomic_compare_exchange_strong(&(var), &(expected), (desired))
#define KMM_ATOMIC_FETCH_ADD(var, val) \
    atomic_fetch_add(&(var), (val))
#endif
#else
// 单线程模式，无原子操作
#define KMM_USE_ATOMICS 0
#define KMM_ATOMIC_TYPE size_t
#define KMM_ATOMIC_LOAD(var) (var)
#define KMM_ATOMIC_STORE(var, val) ((var) = (val))
#define KMM_ATOMIC_CAS_WEAK(var, expected, desired) \
    (((var) == (expected)) ? ((var) = (desired), 1) : ((expected) = (var), 0))
#define KMM_ATOMIC_CAS_STRONG(var, expected, desired) \
    (((var) == (expected)) ? ((var) = (desired), 1) : ((expected) = (var), 0))
#define KMM_ATOMIC_FETCH_ADD(var, val) \
    (((var) += (val)) - (val))
#endif

// 兼容旧代码：KMM_ATOMIC_CAS 默认使用 weak（循环场景）
#define KMM_ATOMIC_CAS KMM_ATOMIC_CAS_WEAK

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
// 修复 #13：freestanding 模式下调低默认值
#ifndef KMM_V4_POOL_SIZE
    #if defined(KMM_V4_STATIC_POOL)
        // freestanding/bare-metal 模式：默认 16MB，避免过大 BSS 段
        #define KMM_V4_POOL_SIZE (16 * 1024 * 1024)
    #elif KMM_V4_POINTER_SIZE == 8
        #if KMM_V4_ARCH_X86_64 || KMM_V4_ARCH_ARM64
            #define KMM_V4_POOL_SIZE (256 * 1024 * 1024)  // 64位 hosted：256MB
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
    #define KMM_ENABLE_ARENA 0  // V4: arena 子系统已移除，V5 预留
#endif

#ifndef KMM_ENABLE_THREAD_CACHE
    #define KMM_ENABLE_THREAD_CACHE (KMM_THREAD_SAFETY_LEVEL >= 1)
#endif

// 线程缓存容量（仅用于遗留 kmm_thread_cache 结构体，V4 主路径使用 per-thread heap）
#ifndef KMM_THREAD_CACHE_SIZE
    #define KMM_THREAD_CACHE_SIZE 64
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

_Static_assert(KMM_V4_POOL_SIZE > 0, "Pool size must be positive");
_Static_assert((KMM_V4_ALIGNMENT & (KMM_V4_ALIGNMENT - 1)) == 0, "Alignment must be power of 2");
_Static_assert(KMM_V4_ALIGNMENT >= 8, "Alignment must be at least 8 bytes");
_Static_assert(KMM_CACHE_LINE_SIZE >= 16, "Cache line size must be at least 16 bytes");
_Static_assert(KMM_V4_PAGE_SIZE >= 4096, "Page size must be at least 4KB");
_Static_assert(KMM_TLS_BUFFER_SIZE >= KMM_V4_PAGE_SIZE, "TLAB size must be at least one page");

// ==================== 第 9 层：配置信息输出（调试用）====================

#ifdef KMM_V4_PRINT_CONFIG
    #pragma message("KMM_V4 Configuration:")
    #pragma message("  Compiler: " KMM_V4_COMPILER_NAME)
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

// ==================== 常量定义 ====================

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

// ==================== 前向类型声明 ====================
// 修复 #7：arena 子系统已移除，kmm_arena_t 仅保留为不透明占位（V5 预留）
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

// ==================== 结构体定义 ====================

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

// ==================== Per-Thread Heap 模型（修复 #1/#19） ====================
// 核心改动：scope_push/pop 只操作 per-thread heap offset，不碰全局 offset。
// 全局 offset 单调递增，仅在线程 heap refill 时 CAS 推进。
// 这样 scope 回退只影响当前线程，其他线程无感知。

// 作用域栈结构（支持嵌套作用域，每层独立保存/恢复 thread_heap offset）
typedef struct {
    size_t offsets[KMM_V4_MAX_SCOPE_DEPTH];
    size_t depth;
} kmm_scope_stack_t;

// Per-thread heap：每个线程从全局池批量获取一块内存，后续分配在 TLS 内完成
typedef struct {
    uint8_t* base;        // 当前 thread heap 起始地址（从全局池获取）
    size_t   offset;      // 当前 thread heap 内的分配偏移（scope_push/pop 只动这个）
    size_t   capacity;    // 当前 thread heap 容量
    size_t   total_allocated; // 该线程累计分配总量（用于统计）
} kmm_thread_heap_t;

// ==================== 智能内存池（自动化管理） ====================
// 内存池声明（实际定义在 .c 文件中）
#ifdef KMM_V4_STATIC_POOL
extern uint8_t g_kmm_v4_pool[KMM_V4_POOL_SIZE];
static inline void kmm_v4_pool_commit(size_t needed) { (void)needed; }
#else
extern uint8_t* g_kmm_v4_pool;
extern void kmm_v4_pool_commit(size_t needed);
#endif
extern size_t g_kmm_v4_pool_capacity;

// 全局 offset（单调递增，仅 CAS 推进，永不回退）
#if KMM_THREAD_SAFETY_LEVEL >= 1
extern KMM_ATOMIC_TYPE g_kmm_v4_offset;
#else
extern size_t g_kmm_v4_offset;
#endif

// 作用域栈（线程本地，支持嵌套作用域）
extern KMM_TLS kmm_scope_stack_t g_kmm_v4_scope_stack;

// Per-thread heap（线程本地）
extern KMM_TLS kmm_thread_heap_t g_kmm_v4_thread_heap;

#ifdef KMM_V4_DEBUG
#if KMM_THREAD_SAFETY_LEVEL >= 1
extern KMM_ATOMIC_TYPE g_kmm_v4_peak;
extern KMM_ATOMIC_TYPE g_kmm_v4_alloc_count;
#else
extern size_t g_kmm_v4_peak;
extern size_t g_kmm_v4_alloc_count;
#endif
#endif

// Thread heap refill 函数声明（实现在 .c 文件中）
extern uint8_t* kmm_v4_thread_heap_refill(size_t min_needed);
extern void kmm_v4_thread_heap_invalidate(void);

// ==================== 批量分配 API ====================

// kmm_v4_bump: 批量分配，从 per-thread heap 推进 offset
// 用于编译器将同一 scope 内的多次 malloc 合并为一次分配
#ifndef KMM_V4_BUMP_IMPL
static inline void* kmm_v4_bump(size_t total_size) {
    const size_t mask = KMM_V4_ALIGNMENT - 1;
    size_t aligned_size = (total_size + mask) & ~mask;

    // 快速路径：从 per-thread heap 分配（无原子操作）
    if (KMM_V4_LIKELY(g_kmm_v4_thread_heap.offset + aligned_size <= g_kmm_v4_thread_heap.capacity)) {
        uint8_t* ptr = g_kmm_v4_thread_heap.base + g_kmm_v4_thread_heap.offset;
        g_kmm_v4_thread_heap.offset += aligned_size;
        return ptr;
    }

    // thread heap 耗尽，尝试 refill
    if (KMM_V4_LIKELY(kmm_v4_thread_heap_refill(aligned_size) != NULL)) {
        if (KMM_V4_LIKELY(g_kmm_v4_thread_heap.offset + aligned_size <= g_kmm_v4_thread_heap.capacity)) {
            uint8_t* ptr = g_kmm_v4_thread_heap.base + g_kmm_v4_thread_heap.offset;
            g_kmm_v4_thread_heap.offset += aligned_size;
            return ptr;
        }
    }

    return NULL;
}
#endif

// kmm_v4_offset_save: 保存当前 thread heap offset（用于 scope 优化）
// 修复 #19：只保存 per-thread heap offset，不碰全局 offset
#ifndef KMM_V4_OFFSET_SAVE_IMPL
static inline size_t kmm_v4_offset_save(void) {
    return g_kmm_v4_thread_heap.offset;
}
#endif

// kmm_v4_offset_restore: 恢复 thread heap offset（scope 回退）
// 修复 #19：只恢复 per-thread heap offset，不影响其他线程
#ifndef KMM_V4_OFFSET_RESTORE_IMPL
static inline void kmm_v4_offset_restore(size_t saved) {
    g_kmm_v4_thread_heap.offset = saved;
}
#endif

// ==================== 自动化分配策略 ====================
// 智能选择分配路径（per-thread heap 快速路径 + 全局 CAS 慢路径）
// 修复 #6：所有分配路径统一不加 header，kmm_v4_free 为 no-op

static inline void* kmm_v4_alloc_auto(size_t size) {
    const size_t mask = KMM_V4_ALIGNMENT - 1;
    size_t aligned_size = (size + mask) & ~mask;

    // 快速路径：从 per-thread heap 分配（无原子操作，无锁）
    if (KMM_V4_LIKELY(g_kmm_v4_thread_heap.offset + aligned_size <= g_kmm_v4_thread_heap.capacity)) {
        uint8_t* ptr = g_kmm_v4_thread_heap.base + g_kmm_v4_thread_heap.offset;
        g_kmm_v4_thread_heap.offset += aligned_size;

        #ifdef KMM_V4_DEBUG
        KMM_ATOMIC_FETCH_ADD(g_kmm_v4_alloc_count, 1);
        #endif

        return ptr;
    }

    // thread heap 耗尽，尝试 refill
    if (KMM_V4_LIKELY(kmm_v4_thread_heap_refill(aligned_size) != NULL)) {
        if (KMM_V4_LIKELY(g_kmm_v4_thread_heap.offset + aligned_size <= g_kmm_v4_thread_heap.capacity)) {
            uint8_t* ptr = g_kmm_v4_thread_heap.base + g_kmm_v4_thread_heap.offset;
            g_kmm_v4_thread_heap.offset += aligned_size;

            #ifdef KMM_V4_DEBUG
            KMM_ATOMIC_FETCH_ADD(g_kmm_v4_alloc_count, 1);
            #endif

            return ptr;
        }
    }

    // 慢路径：直接从全局池分配（CAS 循环）
    // 仅当对象太大无法放入 thread heap 时走此路径
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
    } while (KMM_V4_UNLIKELY(!KMM_ATOMIC_CAS_WEAK(g_kmm_v4_offset, offset, new_offset)));

    kmm_v4_pool_commit(new_offset);

    #ifdef KMM_V4_DEBUG
    KMM_ATOMIC_FETCH_ADD(g_kmm_v4_alloc_count, 1);
    size_t peak = KMM_ATOMIC_LOAD(g_kmm_v4_peak);
    while (new_offset > peak) {
        if (KMM_ATOMIC_CAS_STRONG(g_kmm_v4_peak, peak, new_offset)) break;
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

// ==================== 作用域栈操作（嵌套作用域支持） ====================
// 修复 #1/#19：scope_push/pop 只操作 per-thread heap offset，不碰全局 offset。
// 多线程安全：每个线程有独立的 g_kmm_v4_thread_heap，scope 回退互不影响。

static inline void kmm_v4_scope_push(void) {
    kmm_scope_stack_t* stack = &g_kmm_v4_scope_stack;

    if (KMM_V4_UNLIKELY(stack->depth >= KMM_V4_MAX_SCOPE_DEPTH)) {
        #ifdef KMM_V4_DEBUG
        fprintf(stderr, "KMM ERROR: Scope stack overflow (max depth: %d)\n", KMM_V4_MAX_SCOPE_DEPTH);
        #endif
        return;
    }
    // 保存当前 thread heap offset（TLS 变量，无原子操作）
    stack->offsets[stack->depth] = g_kmm_v4_thread_heap.offset;
    stack->depth++;
}

static inline void kmm_v4_scope_pop(void) {
    kmm_scope_stack_t* stack = &g_kmm_v4_scope_stack;
    if (KMM_V4_UNLIKELY(stack->depth == 0)) {
        #ifdef KMM_V4_DEBUG
        fprintf(stderr, "KMM ERROR: Scope stack underflow\n");
        #endif
        return;
    }

    stack->depth--;
    // 恢复 thread heap offset（TLS 变量，无原子操作，不影响其他线程）
    g_kmm_v4_thread_heap.offset = stack->offsets[stack->depth];
}

// 作用域自动清理（支持嵌套，每层独立管理）
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

// KMM 上下文结构（简化版，arena 字段已移除）
typedef struct kmm_context {
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

// ==================== 重置与查询 ====================
// 当 memory.h 已提供 extern 声明时，跳过 static inline 定义，避免 static/extern 冲突

#if !defined(KMM_V4_RESET_IMPL) && !defined(KMM_V4_EXTERNAL_DECLS)
// 修复 #5：reset 只在单线程上下文调用，重置全局 offset + 失效所有 thread heap
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
    // 失效当前线程的 thread heap，强制下次分配时重新填充
    kmm_v4_thread_heap_invalidate();
}
#endif

#if !defined(KMM_V4_USAGE_IMPL) && !defined(KMM_V4_EXTERNAL_DECLS)
// 修复 #12：usage 返回全局 offset（实际已分配的字节）
static inline size_t kmm_v4_usage(void) {
#if KMM_THREAD_SAFETY_LEVEL >= 1
    return KMM_ATOMIC_LOAD(g_kmm_v4_offset);
#else
    return g_kmm_v4_offset;
#endif
}
#endif

#if !defined(KMM_V4_AVAILABLE_IMPL) && !defined(KMM_V4_EXTERNAL_DECLS)
// 修复 #12：available 用 g_kmm_v4_pool_capacity 而非宏 KMM_V4_POOL_SIZE
static inline size_t kmm_v4_available(void) {
    return g_kmm_v4_pool_capacity - kmm_v4_usage();
}
#endif

// ==================== 兼容 API: malloc/calloc/free/strdup ====================
// 修复 #6：统一不加 header，kmm_v4_free 为 no-op（靠 scope 回收）
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

// 修复 #6：free 为 no-op，bump allocator 靠 scope 回收
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
