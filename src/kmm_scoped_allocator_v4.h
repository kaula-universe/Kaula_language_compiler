#ifndef KMM_SCOPED_ALLOCATOR_V4_H
#define KMM_SCOPED_ALLOCATOR_V4_H

#include <stdint.h>
#include <stddef.h>
#include <stdbool.h>

// SIZE_MAX 兜底：freestanding 或旧标准下可能未定义
#ifndef SIZE_MAX
#define SIZE_MAX ((size_t)-1)
#endif

#ifndef KAULA_FREESTANDING
#include <string.h>
#include <stdlib.h>
#else
// freestanding/bare-metal：libc 头不存在，声明 kaula_freestanding_runtime.c 提供的函数
void* memset(void* s, int c, size_t n);
void* memcpy(void* dst, const void* src, size_t n);
void* memmove(void* dst, const void* src, size_t n);
int   memcmp(const void* a, const void* b, size_t n);
size_t strlen(const char* s);
// freestanding 无 stderr；debug 输出降级为空操作
#define fprintf(...) (0)
#endif

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
    #if defined(KMM_V4_STATIC_POOL) && defined(KAULA_FREESTANDING)
        // freestanding/bare-metal：无 TLS 运行时支持（FS/TPIDR 未初始化），
        // 单线程模型下线程局部变量退化为普通全局变量。
        // 多核内核可自行设置 TLS 并用 -DKMM_TLS=__thread 覆盖。
        #define KMM_TLS
    #elif defined(__GNUC__) || defined(__clang__)
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

// Survivor 段大小（方案 C：可回卷幸存段，独立于主池）
// 用于 kmm_v4_promote / kmm_v4_alloc_global 分配的对象，
// 支持 checkpoint/rewind 批量回收，避免单调耗尽。
#ifndef KMM_V4_SURVIVOR_REGION_SIZE
    #if defined(KMM_V4_STATIC_POOL)
        // freestanding：从静态池中划出 1/8 作为 survivor 段（默认 2MB）
        #define KMM_V4_SURVIVOR_REGION_SIZE (KMM_V4_POOL_SIZE / 8)
    #else
        // hosted：独立 mmap 预留，可设较大（默认 16MB）
        #define KMM_V4_SURVIVOR_REGION_SIZE (16 * 1024 * 1024)
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
// D4 修复：FALLBACK 会把 libc malloc 指针混进 KMM 返回值，
// 而 kmm_v4_free 是 no-op、survivor_free 只收 KMM 区域指针，
// 导致该内存永久泄漏，且所有权语义被破坏。
// 因此所有模式下默认关闭 FALLBACK：池耗尽即返回 NULL，
// 返回的所有指针都在 KMM 管理区域（pool/extra/survivor），所有权语义一致。
// 用户如需 libc，直接用 std_malloc；如需在 realloc 中兼容外部指针，
// 可手动 -D KMM_V4_ENABLE_FALLBACK=1 开启（仅限 realloc 外部指针兼容分支生效，
// alloc_global 仍不做 malloc fallback，仍保证 KMM 返回指针归属可追踪）。
#ifndef KMM_V4_ENABLE_FALLBACK
    #define KMM_V4_ENABLE_FALLBACK 0  // 所有模式默认关闭：严格池，所有权语义一致
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

// 作用域栈结构（支持嵌套作用域，每层独立保存/恢复 thread_heap {base, offset}）
// 修复 Bug A：只保存 offset 会在 TLAB refill 后破坏回卷——refill 会重置
// base/offset/capacity（kmm_scoped_allocator_v4.c:174-176），scope_pop 把旧
// offset 恢复到新 TLAB 上会导致后续分配从新 TLAB 中间开始，覆盖存活对象。
// 因此每帧保存完整 {base, offset}，恢复时若 base 变了就失效重填。
typedef struct {
    struct {
        uint8_t* base;    // push 时的 thread_heap.base
        size_t   offset;  // push 时的 thread_heap.offset
    } frames[KMM_V4_MAX_SCOPE_DEPTH];
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
//
// 多段自动扩容（Task #17）：
//   KMM_V4_POOL_SIZE 仅决定首段大小（编译期静态估算继续生效）。
//   hosted 模式下首段耗尽时，thread heap refill 触发 kmm_v4_pool_grow：
//   用系统 malloc 追加新段（链式段表 g_kmm_v4_extra_segs），绝不整体
//   realloc/搬移（池内有大量活跃指针）。g_kmm_v4_offset 升级为跨段统一
//   额度计数器：[0, primary_capacity) 对应首段，超出部分按段链顺序落
//   入扩展段。扩容失败（malloc 返回 NULL）保持现有失败约定（返回 NULL）
//   并打一条 stderr 日志。freestanding（KAULA_FREESTANDING）无 malloc，
//   不扩容，行为与旧版一致。
//   跨段 scope 回卷策略：thread heap 永远是单次 refill 的完整段块（不会
//   跨段拼接），scope push/pop 仅操作 thread heap 内部，{base, offset}
//   帧语义不变；refill 换段导致 base 变化时按既有 Bug A 修复路径失效
//   重填——因此无需段级回卷标记。取舍：回卷不会把已消耗的扩展段额度
//   归还全局计数器（与首段一致），扩展段内存随段链存活到 destroy_pool，
//   换取实现简单与指针稳定。扩展段已消耗额度直接由 g_kmm_v4_offset 派生
//   （max(0, offset - 首段容量)），不单独维护计数器。
#ifdef KMM_V4_STATIC_POOL
extern uint8_t g_kmm_v4_pool[KMM_V4_POOL_SIZE];
static inline void kmm_v4_pool_commit(size_t needed) { (void)needed; }
#else
extern uint8_t* g_kmm_v4_pool;
extern void kmm_v4_pool_commit(size_t needed);
#endif
extern size_t g_kmm_v4_pool_capacity;

// 扩展段额度统计（Task #17）：已安装扩展段的总额度（字节）。
// 已消耗部分 = max(0, g_kmm_v4_offset - g_kmm_v4_pool_capacity)，由统一
// offset 直接派生，不单独维护计数器。
#if KMM_THREAD_SAFETY_LEVEL >= 1
extern KMM_ATOMIC_TYPE g_kmm_v4_extra_capacity;
#else
extern size_t g_kmm_v4_extra_capacity;
#endif

// 池耗尽时追加新段（至少 min_needed 字节），成功返回 1；
// 不可扩容（freestanding）或 malloc 失败返回 0（实现见 .c）
extern int kmm_v4_pool_grow(size_t min_needed);

// Task #25：offset 回卷后归还全空尾段（实现见 .c，归还条件与安全性
// 论证见实现处注释）。watermark = 回卷后的全局 offset；完整额度区间
// 全部 >= watermark 的尾段被连续摘除并 free。静态池无扩展段，no-op。
#ifdef KMM_V4_STATIC_POOL
static inline void kmm_v4_pool_trim_tail(size_t watermark) { (void)watermark; }
#else
extern void kmm_v4_pool_trim_tail(size_t watermark);
#endif

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

// ==================== Survivor 段（方案 A：slab 分配器 + per-object free） ====================
// 修复设计层面问题：survivor 段原为全局池的 bump 子区域，单调增长无回收。
// 方案 A：slab 分配器，按 size class 分桶 + bitmap，支持 per-object free。
//
// - kmm_v4_alloc_global / kmm_v4_promote 从 slab 桶分配（小对象）或大对象区（≥2KB）
// - kmm_v4_survivor_free(ptr) 标记槽位空闲（per-object 回收）
// - kmm_v4_survivor_checkpoint() / rewind(cp) 批量回卷（兜底，code gen 未发射 free 时）
//
// SOR 用法（编译器自动发射）：
//   变量死亡点 → kmm_v4_survivor_free(ptr)（per-object 精确回收）
//   函数入口 checkpoint → 函数出口 rewind（兜底批量回收，仅对不向外 promote 的函数）
//
// 非 SOR 用法（手写 C）：
//   void* p = kmm_v4_alloc_global(64);
//   ... 使用 p ...
//   kmm_v4_survivor_free(p);  // 精确回收
//
// 线程安全：分配/释放走原子 CAS（线程安全）；rewind 非线程安全——
// 调用方需确保被回卷的 survivor 对象无其他线程引用。

// Slab 配置
#define KMM_V4_SLAB_MIN_SIZE     8      // 最小 size class
#define KMM_V4_SLAB_MAX_SIZE     2048   // 最大 slab 桶 size（超过走大对象 bump）
#define KMM_V4_SLAB_BUCKETS      9      // 桶数：8/16/32/64/128/256/512/1024/2048
#define KMM_V4_SLAB_SLOTS        256    // 每桶槽位数
#define KMM_V4_SLAB_BITMAP_WORDS ((KMM_V4_SLAB_SLOTS + 63) / 64)  // bitmap 字数

// Slab 桶：固定大小槽位数组 + bitmap
typedef struct {
    uint8_t* slots;                       // 槽位内存（KMM_V4_SLAB_SLOTS * bucket_size）
    uint64_t bitmap[KMM_V4_SLAB_BITMAP_WORDS]; // 占用位图（1=已分配，用 __atomic 内建操作）
    size_t free_hint;                     // 下一个可能空闲的槽位（原子操作用 __atomic 内建）
    size_t bucket_size;                   // 本桶的 size class
    size_t allocated;                     // 已分配槽位数（统计用）
} kmm_slab_bucket_t;

// Survivor 段状态
typedef struct {
    kmm_slab_bucket_t buckets[KMM_V4_SLAB_BUCKETS];  // slab 桶数组
    // 大对象区（≥2KB）走 bump，仅靠 rewind 回收
    uint8_t* large_base;
    size_t   large_capacity;
#if KMM_THREAD_SAFETY_LEVEL >= 1
    KMM_ATOMIC_TYPE large_offset;
#else
    size_t   large_offset;
#endif
    bool     initialized;
} kmm_survivor_state_t;

// Survivor checkpoint（不透明令牌）
typedef struct {
    size_t _large_offset;  // checkpoint 时的大对象区 offset（slab 桶 per-slot 回收，不回卷）
} kmm_survivor_checkpoint_t;

// Survivor 段全局状态（定义在 .c 中）
extern kmm_survivor_state_t g_kmm_v4_survivor;

// 大对象区内存基址（slab 桶内存从 .c 中按需分配）
#ifdef KMM_V4_STATIC_POOL
// 静态池：slab 桶 slots 内存 + 大对象区,统一从一块静态数组中划分
extern uint8_t g_kmm_v4_survivor_storage[KMM_V4_SURVIVOR_REGION_SIZE];
static inline void kmm_v4_survivor_commit(size_t needed) { (void)needed; }
#else
// 动态池：大对象区独立 mmap，slab 桶 slots 在 survivor_ensure_init 中分配
extern uint8_t* g_kmm_v4_survivor_large_base;
extern size_t   g_kmm_v4_survivor_large_capacity;
extern void kmm_v4_survivor_commit(size_t needed);
#endif

// ==================== 批量分配 API ====================

// kmm_v4_bump: 批量分配，从 per-thread heap 推进 offset
// 用于编译器将同一 scope 内的多次 malloc 合并为一次分配
#ifndef KMM_V4_BUMP_IMPL
static inline void* kmm_v4_bump(size_t total_size) {
    const size_t mask = KMM_V4_ALIGNMENT - 1;
    // D3 修复：对齐前检查 total_size + mask 溢出
    if (KMM_V4_UNLIKELY(total_size == 0 || total_size > SIZE_MAX - mask)) return NULL;
    size_t aligned_size = (total_size + mask) & ~mask;

    // 快速路径：从 per-thread heap 分配（无原子操作）
    if (KMM_V4_LIKELY(aligned_size <= g_kmm_v4_thread_heap.capacity - g_kmm_v4_thread_heap.offset)) {
        uint8_t* ptr = g_kmm_v4_thread_heap.base + g_kmm_v4_thread_heap.offset;
        g_kmm_v4_thread_heap.offset += aligned_size;
        return ptr;
    }

    // thread heap 耗尽，尝试 refill
    if (KMM_V4_LIKELY(kmm_v4_thread_heap_refill(aligned_size) != NULL)) {
        if (KMM_V4_LIKELY(aligned_size <= g_kmm_v4_thread_heap.capacity - g_kmm_v4_thread_heap.offset)) {
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

// ==================== 全局幸存段分配 + 提升（survivor）API ====================
// 修复 #25：SOR 跨作用域所有权提升。
// 方案 A：slab 分配器，per-object free。
// kmm_v4_promote：把对象从当前作用域段"提升"到幸存段（分配 + 拷贝）。
// 用于 SOR 在 yield/extract/return 边界实现真正的跨作用域所有权转移。

// 内部：选 bucket index（size→bucket），返回桶下标，size 超限返回 -1
static inline int kmm_v4_slab_bucket_index(size_t size) {
    if (size == 0 || size > KMM_V4_SLAB_MAX_SIZE) return -1;
    int idx = 0;
    size_t cls = KMM_V4_SLAB_MIN_SIZE;
    while (cls < size) { cls <<= 1; idx++; }
    return idx;  // 0..8 对应 8..2048
}

// 内部：从指定桶分配一个槽位，返回槽位指针，满返回 NULL
static inline void* kmm_v4_slab_alloc_from_bucket(kmm_slab_bucket_t* bucket) {
#if KMM_THREAD_SAFETY_LEVEL >= 1
    size_t hint = __atomic_load_n(&bucket->free_hint, __ATOMIC_RELAXED);
#else
    size_t hint = 0;
#endif
    for (int attempt = 0; attempt < KMM_V4_SLAB_SLOTS; attempt++) {
        size_t slot = (hint + attempt) % KMM_V4_SLAB_SLOTS;
        size_t word = slot / 64;
        uint64_t bit = (uint64_t)1 << (slot % 64);
#if KMM_THREAD_SAFETY_LEVEL >= 1
        // CAS 设置占用位（bitmap 是普通 uint64_t 数组，用 __atomic 内建）
        uint64_t old = __atomic_load_n(&bucket->bitmap[word], __ATOMIC_RELAXED);
        if (old & bit) continue;  // 已占用
        uint64_t neu = old | bit;
        if (__atomic_compare_exchange_n(&bucket->bitmap[word], &old, neu, 1,
                                        __ATOMIC_ACQUIRE, __ATOMIC_RELAXED)) {
            __atomic_store_n(&bucket->free_hint, (slot + 1) % KMM_V4_SLAB_SLOTS, __ATOMIC_RELAXED);
            bucket->allocated++;
            return bucket->slots + slot * bucket->bucket_size;
        }
        // CAS 失败，重试当前 slot
        attempt--;
#else
        if (bucket->bitmap[word] & bit) continue;
        bucket->bitmap[word] |= bit;
        bucket->allocated++;
        return bucket->slots + slot * bucket->bucket_size;
#endif
    }
    return NULL;  // 桶满
}

// 内部：释放指定桶中的槽位
static inline bool kmm_v4_slab_free_from_bucket(kmm_slab_bucket_t* bucket, void* ptr) {
    if (bucket->slots == NULL) return false;
    size_t offset = (size_t)((uint8_t*)ptr - bucket->slots);
    if (offset >= KMM_V4_SLAB_SLOTS * bucket->bucket_size) return false;
    if (offset % bucket->bucket_size != 0) return false;
    size_t slot = offset / bucket->bucket_size;
    size_t word = slot / 64;
    uint64_t bit = (uint64_t)1 << (slot % 64);
#if KMM_THREAD_SAFETY_LEVEL >= 1
    uint64_t old = __atomic_load_n(&bucket->bitmap[word], __ATOMIC_RELAXED);
    if (!(old & bit)) return false;  // 本来就空闲
    uint64_t neu = old & ~bit;
    while (!__atomic_compare_exchange_n(&bucket->bitmap[word], &old, neu, 1,
                                         __ATOMIC_RELEASE, __ATOMIC_RELAXED)) {
        if (!(old & bit)) return false;
        neu = old & ~bit;
    }
#else
    if (!(bucket->bitmap[word] & bit)) return false;
    bucket->bitmap[word] &= ~bit;
#endif
    if (bucket->allocated > 0) bucket->allocated--;
    return true;
}

// 内部：判断 ptr 是否在指定桶内
static inline bool kmm_v4_slab_bucket_contains(kmm_slab_bucket_t* bucket, const void* ptr) {
    return bucket->slots != NULL &&
           (const uint8_t*)ptr >= bucket->slots &&
           (const uint8_t*)ptr <  bucket->slots + KMM_V4_SLAB_SLOTS * bucket->bucket_size;
}

// ---- Survivor lazy init ----
// kmm_v4_alloc_global 首次调用时触发，确保 slab 桶 slots 和大对象区已就绪。
// 静态池：从 g_kmm_v4_survivor_storage 划分 slab 桶 + 大对象区。
// 动态池：委托 kmm_v4_survivor_commit → survivor_ensure_init（mmap + 原子双检锁）。
static inline void kmm_v4_survivor_lazy_init(void) {
    if (g_kmm_v4_survivor.initialized) return;
#ifdef KMM_V4_STATIC_POOL
    static const size_t _bucket_sizes[KMM_V4_SLAB_BUCKETS] = {8,16,32,64,128,256,512,1024,2048};
    size_t _slab_total = 0;
    for (int i = 0; i < KMM_V4_SLAB_BUCKETS; i++)
        _slab_total += _bucket_sizes[i] * KMM_V4_SLAB_SLOTS;
    size_t _off = 0;
    for (int i = 0; i < KMM_V4_SLAB_BUCKETS; i++) {
        g_kmm_v4_survivor.buckets[i].bucket_size = _bucket_sizes[i];
        g_kmm_v4_survivor.buckets[i].slots = g_kmm_v4_survivor_storage + _off;
        _off += _bucket_sizes[i] * KMM_V4_SLAB_SLOTS;
        memset(g_kmm_v4_survivor.buckets[i].bitmap, 0,
               sizeof(g_kmm_v4_survivor.buckets[i].bitmap));
        g_kmm_v4_survivor.buckets[i].free_hint = 0;
        g_kmm_v4_survivor.buckets[i].allocated = 0;
    }
    g_kmm_v4_survivor.large_base = g_kmm_v4_survivor_storage + _off;
    g_kmm_v4_survivor.large_capacity = KMM_V4_SURVIVOR_REGION_SIZE - _slab_total;
    g_kmm_v4_survivor.large_offset = 0;
    g_kmm_v4_survivor.initialized = true;
#else
    kmm_v4_survivor_commit(0);
#endif
}

#ifndef KMM_V4_ALLOC_GLOBAL_IMPL
static inline void* kmm_v4_alloc_global(size_t size) {
    if (KMM_V4_UNLIKELY(size == 0)) return NULL;
    // D3 修复：对齐前检查 size + mask 溢出（与 alloc_auto/bump 统一口径）
    const size_t mask = KMM_V4_ALIGNMENT - 1;
    if (KMM_V4_UNLIKELY(size > SIZE_MAX - mask)) return NULL;

    // 首次调用时懒初始化 survivor 段（slab 桶 slots + 大对象区）
    if (KMM_V4_UNLIKELY(!g_kmm_v4_survivor.initialized)) {
        kmm_v4_survivor_lazy_init();
    }

    // 小对象走 slab 桶
    int idx = kmm_v4_slab_bucket_index(size);
    if (idx >= 0) {
        void* p = kmm_v4_slab_alloc_from_bucket(&g_kmm_v4_survivor.buckets[idx]);
        if (p) {
            #ifdef KMM_V4_DEBUG
            #if KMM_THREAD_SAFETY_LEVEL >= 1
            KMM_ATOMIC_FETCH_ADD(g_kmm_v4_alloc_count, 1);
            #else
            g_kmm_v4_alloc_count++;
            #endif
            #endif
            return p;
        }
        // 桶满，fallthrough 到大对象区
    }

    // 大对象走 bump（仅靠 rewind 回收）
    size_t aligned_size = (size + mask) & ~mask;
#if KMM_THREAD_SAFETY_LEVEL >= 1
    size_t offset = KMM_ATOMIC_LOAD(g_kmm_v4_survivor.large_offset);
    size_t new_offset;
    do {
        // D3 修复：offset + aligned_size 溢出检查
        if (KMM_V4_UNLIKELY(aligned_size > SIZE_MAX - offset)) {
            return NULL;
        }
        new_offset = offset + aligned_size;
        // D4 修复：无论 FALLBACK 是否开启，survivor 耗尽都返回 NULL。
        // 严禁把 libc malloc 指针混入 KMM 返回值——否则 kmm_v4_free (no-op)
        // 和 survivor_free (只收 survivor 段) 都无法回收，永久泄漏。
        if (KMM_V4_UNLIKELY(new_offset > g_kmm_v4_survivor.large_capacity)) {
            return NULL;
        }
    } while (KMM_V4_UNLIKELY(!KMM_ATOMIC_CAS_WEAK(g_kmm_v4_survivor.large_offset, offset, new_offset)));
    kmm_v4_survivor_commit(new_offset);
    #ifdef KMM_V4_DEBUG
    KMM_ATOMIC_FETCH_ADD(g_kmm_v4_alloc_count, 1);
    #endif
    KMM_V4_PREFETCH(g_kmm_v4_survivor.large_base + new_offset);
    return g_kmm_v4_survivor.large_base + offset;
#else
    size_t offset = g_kmm_v4_survivor.large_offset;
    // D3 修复：offset + aligned_size 溢出检查
    if (KMM_V4_UNLIKELY(aligned_size > SIZE_MAX - offset)) return NULL;
    size_t new_offset = offset + aligned_size;
    if (KMM_V4_LIKELY(new_offset <= g_kmm_v4_survivor.large_capacity)) {
        g_kmm_v4_survivor.large_offset = new_offset;
        kmm_v4_survivor_commit(new_offset);
        #ifdef KMM_V4_DEBUG
        g_kmm_v4_alloc_count++;
        #endif
        KMM_V4_PREFETCH(g_kmm_v4_survivor.large_base + new_offset);
        return g_kmm_v4_survivor.large_base + offset;
    }
    // D4 修复：耗尽返回 NULL，不混入 libc malloc
    return NULL;
#endif
}
#endif

// ---- Survivor per-object free ----
// 释放 survivor 段分配的对象。ptr 必须是 kmm_v4_alloc_global/promote 返回的指针。
// 返回 true 表示成功释放，false 表示 ptr 不在 survivor 段或已释放。
static inline bool kmm_v4_survivor_free(void* ptr) {
    if (ptr == NULL) return false;
    // 扫描所有 slab 桶
    for (int i = 0; i < KMM_V4_SLAB_BUCKETS; i++) {
        if (kmm_v4_slab_free_from_bucket(&g_kmm_v4_survivor.buckets[i], ptr)) {
            return true;
        }
    }
    // 大对象区不支持 per-object free（bump allocator 无 free）
    return false;
}

// ---- Survivor checkpoint/rewind API（兜底批量回收） ----
static inline kmm_survivor_checkpoint_t kmm_v4_survivor_checkpoint(void) {
    kmm_survivor_checkpoint_t cp;
#if KMM_THREAD_SAFETY_LEVEL >= 1
    cp._large_offset = KMM_ATOMIC_LOAD(g_kmm_v4_survivor.large_offset);
#else
    cp._large_offset = g_kmm_v4_survivor.large_offset;
#endif
    return cp;
}

// 回卷 survivor 段大对象区到 checkpoint，批量释放之后的分配。
// ⚠ 非线程安全：调用方需确保被回卷的对象无其他线程引用。
// ⚠ slab 桶不回卷——靠 per-object free 回收。
static inline void kmm_v4_survivor_rewind(kmm_survivor_checkpoint_t cp) {
#if KMM_THREAD_SAFETY_LEVEL >= 1
    KMM_ATOMIC_STORE(g_kmm_v4_survivor.large_offset, cp._large_offset);
#else
    g_kmm_v4_survivor.large_offset = cp._large_offset;
#endif
}

// survivor 段已用字节数（slab 桶已分配槽位 + 大对象区 offset）
static inline size_t kmm_v4_survivor_usage(void) {
    size_t slab_used = 0;
    for (int i = 0; i < KMM_V4_SLAB_BUCKETS; i++) {
        slab_used += g_kmm_v4_survivor.buckets[i].allocated * g_kmm_v4_survivor.buckets[i].bucket_size;
    }
#if KMM_THREAD_SAFETY_LEVEL >= 1
    return slab_used + KMM_ATOMIC_LOAD(g_kmm_v4_survivor.large_offset);
#else
    return slab_used + g_kmm_v4_survivor.large_offset;
#endif
}

// survivor 段可用字节数（粗略估算）
static inline size_t kmm_v4_survivor_available(void) {
    size_t slab_total = 0;
    for (int i = 0; i < KMM_V4_SLAB_BUCKETS; i++) {
        slab_total += KMM_V4_SLAB_SLOTS * g_kmm_v4_survivor.buckets[i].bucket_size;
    }
    return slab_total + g_kmm_v4_survivor.large_capacity - kmm_v4_survivor_usage();
}

// 判断指针是否在 survivor 段内（供 realloc/free 使用）
static inline bool kmm_v4_survivor_contains(const void* ptr) {
    // slab 桶
    for (int i = 0; i < KMM_V4_SLAB_BUCKETS; i++) {
        if (kmm_v4_slab_bucket_contains(&g_kmm_v4_survivor.buckets[i], ptr)) return true;
    }
    // 大对象区（统一用 g_kmm_v4_survivor.large_base，兼容静态/动态池）
    return g_kmm_v4_survivor.large_base != NULL &&
           (const uint8_t*)ptr >= g_kmm_v4_survivor.large_base &&
           (const uint8_t*)ptr <  g_kmm_v4_survivor.large_base + g_kmm_v4_survivor.large_capacity;
}

// kmm_v4_promote: 将 ptr 指向的 size 字节对象提升到全局幸存段，返回新指针。
// 旧槽位保持原样，随后由所属作用域的 scope_pop 正常回收（无害）。
//
// ⚠ 限制（Bug 2）：本函数是浅拷贝——仅复制 size 字节的裸内存。
// 若对象内部含指针字段，且这些指针指向当前作用域段内的其他对象，
// 提升后这些内部指针仍指向旧作用域段，scope_pop 回卷后即成悬垂指针。
// 对聚合对象（含指针字段）请改用 kmm_v4_promote_deep，由回调负责把
// 内部指针逐个重新提升并改写为幸存段新地址。
static inline void* kmm_v4_promote(void* ptr, size_t size) {
    if (KMM_V4_UNLIKELY(ptr == NULL)) return NULL;
    void* dst = kmm_v4_alloc_global(size);
    if (KMM_V4_LIKELY(dst != NULL && size > 0)) {
        memcpy(dst, ptr, size);
    }
    return dst;
}

// kmm_v4_promote_deep: 深拷贝提升。先浅拷贝 size 字节到幸存段，再调用 cb
// 让调用方遍历对象内部的指针字段，对每个指向作用域段的内部指针调用
// kmm_v4_promote_deep（递归）并把字段改写为新地址。cb 接收提升后的新对象
// 地址，返回 0 表示成功，非 0 表示失败（此时释放已分配的幸存段内存）。
typedef int (*kmm_v4_promote_cb_t)(void* promoted_obj);
static inline void* kmm_v4_promote_deep(void* ptr, size_t size, kmm_v4_promote_cb_t cb) {
    if (KMM_V4_UNLIKELY(ptr == NULL)) return NULL;
    void* dst = kmm_v4_alloc_global(size);
    if (KMM_V4_UNLIKELY(dst == NULL)) return NULL;
    if (size > 0) memcpy(dst, ptr, size);
    if (cb && cb(dst) != 0) {
        // D12 顺手修复：回调失败时必须回收已分配的 dst。
        // 原注释称"无法回收"是错误的——slab 分配器支持 per-object free，
        // survivor_free 可精确释放 dst；即使 dst 落在 large bump 区，
        // survivor_free 返回 false 也没有副作用（bump 区靠 rewind 兜底回收）。
        (void)kmm_v4_survivor_free(dst);
        return NULL;
    }
    return dst;
}

// kmm_v4_scope_promote_p: 原地提升（编译器友好版，配合 sizeof(*var) 使用）。
// 将 *ptr 指向的内容拷贝到幸存段，并把 *ptr 替换为新指针。
//
// ⚠ 限制（Bug 2）：同 kmm_v4_promote，浅拷贝，对含指针字段的聚合对象不安全。
// 编译器 codegen 当前调用此函数做 SOR yield/return 边界提升——仅对扁平对象
// 或内部指针已单独提升的对象安全。聚合对象需由前端生成逐字段提升代码。
static inline void kmm_v4_scope_promote_p(void** ptr, size_t size) {
    if (KMM_V4_UNLIKELY(ptr == NULL || *ptr == NULL)) return;
    void* dst = kmm_v4_alloc_global(size);
    if (KMM_V4_UNLIKELY(dst == NULL)) return;  // D1 修复：分配失败时保留原指针
    if (size > 0) memcpy(dst, *ptr, size);
    *ptr = dst;  // 仅在成功时替换
}

// kmm_v4_scope_promote_p_deep: 原地深拷贝提升。提升后调用 cb 让调用方
// 改写对象内部的指针字段。失败时 *ptr 保持原值（不替换为半成品）。
static inline int kmm_v4_scope_promote_p_deep(void** ptr, size_t size, kmm_v4_promote_cb_t cb) {
    if (KMM_V4_UNLIKELY(ptr == NULL || *ptr == NULL)) return 0;
    void* dst = kmm_v4_alloc_global(size);
    if (KMM_V4_UNLIKELY(dst == NULL)) return -1;
    if (size > 0) memcpy(dst, *ptr, size);
    if (cb && cb(dst) != 0) {
        return -1;  // 失败：不替换 *ptr
    }
    *ptr = dst;
    return 0;
}

// ==================== 自动化分配策略 ====================
// 智能选择分配路径（per-thread heap 快速路径 + 全局 CAS 慢路径）
// 修复 #6：所有分配路径统一不加 header，kmm_v4_free 为 no-op

static inline void* kmm_v4_alloc_auto(size_t size) {
    const size_t mask = KMM_V4_ALIGNMENT - 1;
    // D3 修复：对齐前检查 size + mask 溢出
    if (KMM_V4_UNLIKELY(size == 0 || size > SIZE_MAX - mask)) return NULL;
    size_t aligned_size = (size + mask) & ~mask;

    // 快速路径：从 per-thread heap 分配（无原子操作，无锁）
    if (KMM_V4_LIKELY(aligned_size <= g_kmm_v4_thread_heap.capacity - g_kmm_v4_thread_heap.offset)) {
        uint8_t* ptr = g_kmm_v4_thread_heap.base + g_kmm_v4_thread_heap.offset;
        g_kmm_v4_thread_heap.offset += aligned_size;

        #ifdef KMM_V4_DEBUG
        KMM_ATOMIC_FETCH_ADD(g_kmm_v4_alloc_count, 1);
        #endif

        return ptr;
    }

    // thread heap 耗尽，尝试 refill
    if (KMM_V4_LIKELY(kmm_v4_thread_heap_refill(aligned_size) != NULL)) {
        if (KMM_V4_LIKELY(aligned_size <= g_kmm_v4_thread_heap.capacity - g_kmm_v4_thread_heap.offset)) {
            uint8_t* ptr = g_kmm_v4_thread_heap.base + g_kmm_v4_thread_heap.offset;
            g_kmm_v4_thread_heap.offset += aligned_size;

            #ifdef KMM_V4_DEBUG
            KMM_ATOMIC_FETCH_ADD(g_kmm_v4_alloc_count, 1);
            #endif

            return ptr;
        }
    }

    // 慢路径：直接从全局池分配（幸存段，scope_pop 不回卷）
    // 修复 #25：提取为 kmm_v4_alloc_global 复用
    return kmm_v4_alloc_global(aligned_size);
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
    #elif defined(__ARM_NEON)
        #include <arm_neon.h>
        static inline void kmm_v4_zero_auto(void* ptr, size_t size) {
            uint8x16_t zero = vdupq_n_u8(0);
            uint8_t* p = (uint8_t*)ptr;
            while (size >= 16) {
                vst1q_u8(p, zero);
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
        // D6 修复：深度超限直接 abort。64 层嵌套深度在任何合理代码下都足够，
        // 超限一定是程序逻辑 bug（push/pop 不成对或递归失控）。
        // 静默 return 会导致后续对应的 pop 把 depth 减到一个错误的低值，
        // 弹出不匹配的帧，回卷 thread heap 到错误位置——悬垂/错误回收，
        // 后果比立即终止严重得多。
        fprintf(stderr, "KMM FATAL: Scope stack overflow (max depth: %d), aborting\n",
                KMM_V4_MAX_SCOPE_DEPTH);
#ifndef KAULA_FREESTANDING
        abort();
#else
        for (;;) { /* 死循环，避免在 freestanding 无 abort 时继续执行 */ }
#endif
    }
    // 修复 Bug A：保存完整 {base, offset}，而非仅 offset。
    // 若作用域内发生 thread_heap_refill，base 会变，旧 offset 对新 TLAB 无意义。
    stack->frames[stack->depth].base   = g_kmm_v4_thread_heap.base;
    stack->frames[stack->depth].offset = g_kmm_v4_thread_heap.offset;
    stack->depth++;
}

static inline void kmm_v4_scope_pop(void) {
    kmm_scope_stack_t* stack = &g_kmm_v4_scope_stack;
    if (KMM_V4_UNLIKELY(stack->depth == 0)) {
        // D6 修复：下溢同样 abort。push/pop 不成对是程序逻辑 bug，
        // 静默 return 后后续 pop 会继续在 depth=0 上误判并弹出 frames[-1]，
        // 读越界后回卷 thread heap，严重 UB。
        fprintf(stderr, "KMM FATAL: Scope stack underflow (pop without push), aborting\n");
#ifndef KAULA_FREESTANDING
        abort();
#else
        for (;;) { }
#endif
    }

    stack->depth--;
    uint8_t* saved_base   = stack->frames[stack->depth].base;
    size_t   saved_offset = stack->frames[stack->depth].offset;
    // 修复 Bug A：若 push 后发生过 refill，当前 base != saved_base，
    // 旧 offset 指向的是已废弃的旧 TLAB，直接恢复会覆盖新 TLAB 中的存活对象。
    // 此时失效当前 thread heap，强制下次分配时重新 refill（保守但安全）。
    if (g_kmm_v4_thread_heap.base == saved_base) {
        g_kmm_v4_thread_heap.offset = saved_offset;
    } else {
        kmm_v4_thread_heap_invalidate();
    }
    // Task #25：回卷后检查段链尾部，归还全空扩展段。
    // 仅 LEVEL 0 触发：回卷只作用于 thread heap，全局 offset 不回退，
    // 尾段几乎不可能满足归还条件，检查开销为一次空链判空；LEVEL>=1
    // 下并发 refill 可能在摘除瞬间遍历段链（use-after-free 窗口），
    // 故不在此触发，归还由 kmm_v4_reset（单线程上下文约定）承担。
#if KMM_THREAD_SAFETY_LEVEL == 0
    kmm_v4_pool_trim_tail(g_kmm_v4_offset);
#endif
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
// 方案 A：同时重置 survivor 段（清空所有 slab 桶 bitmap + 大对象区 offset）
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
    // 方案 A：清空 survivor slab 桶 + 大对象区
    for (int i = 0; i < KMM_V4_SLAB_BUCKETS; i++) {
        memset(g_kmm_v4_survivor.buckets[i].bitmap, 0, sizeof(g_kmm_v4_survivor.buckets[i].bitmap));
        g_kmm_v4_survivor.buckets[i].allocated = 0;
#if KMM_THREAD_SAFETY_LEVEL >= 1
        __atomic_store_n(&g_kmm_v4_survivor.buckets[i].free_hint, 0, __ATOMIC_RELAXED);
#else
        g_kmm_v4_survivor.buckets[i].free_hint = 0;
#endif
    }
#if KMM_THREAD_SAFETY_LEVEL >= 1
    KMM_ATOMIC_STORE(g_kmm_v4_survivor.large_offset, 0);
#else
    g_kmm_v4_survivor.large_offset = 0;
#endif
    // 失效当前线程的 thread heap，强制下次分配时重新填充
    kmm_v4_thread_heap_invalidate();
    // Task #25：全局 offset 已归零，所有扩展段额度全部越过水位（全空）：
    // 归还整条扩展段链，避免内存滞留到 destroy_pool。reset 约定单线程
    // 上下文（见上方注释），无并发 refill 遍历段链，归还安全；后续分配
    // 从首段重新开始，额度不足时 refill 会重新触发 grow。
    kmm_v4_pool_trim_tail(0);
}
#endif

#if !defined(KMM_V4_USAGE_IMPL) && !defined(KMM_V4_EXTERNAL_DECLS)
// 修复 #12：usage 返回全局 offset（实际已分配的字节）
// 方案 A：包含主池 TLAB + survivor 段（slab 桶 + 大对象区）
static inline size_t kmm_v4_usage(void) {
#if KMM_THREAD_SAFETY_LEVEL >= 1
    return KMM_ATOMIC_LOAD(g_kmm_v4_offset) + kmm_v4_survivor_usage();
#else
    return g_kmm_v4_offset + kmm_v4_survivor_usage();
#endif
}
#endif

#if !defined(KMM_V4_AVAILABLE_IMPL) && !defined(KMM_V4_EXTERNAL_DECLS)
// 修复 #12：available 用 g_kmm_v4_pool_capacity 而非宏 KMM_V4_POOL_SIZE
// 方案 A：主池剩余 + survivor 段剩余
// Task #17：多段扩容后 g_kmm_v4_offset 为跨段统一额度，可能超过首段容量：
// 首段剩余截断到 0；扩展段已消耗 = max(0, offset - 首段容量)，由 offset 派生。
static inline size_t kmm_v4_available(void) {
#if KMM_THREAD_SAFETY_LEVEL >= 1
    size_t _off = KMM_ATOMIC_LOAD(g_kmm_v4_offset);
    size_t _primary_left = (_off < g_kmm_v4_pool_capacity) ? (g_kmm_v4_pool_capacity - _off) : 0;
    size_t _extra_used = (_off > g_kmm_v4_pool_capacity) ? (_off - g_kmm_v4_pool_capacity) : 0;
    size_t _extra_cap = KMM_ATOMIC_LOAD(g_kmm_v4_extra_capacity);
    size_t _extra_left = (_extra_cap > _extra_used) ? (_extra_cap - _extra_used) : 0;
    return _primary_left + _extra_left + kmm_v4_survivor_available();
#else
    size_t _primary_left = (g_kmm_v4_offset < g_kmm_v4_pool_capacity) ? (g_kmm_v4_pool_capacity - g_kmm_v4_offset) : 0;
    size_t _extra_used = (g_kmm_v4_offset > g_kmm_v4_pool_capacity) ? (g_kmm_v4_offset - g_kmm_v4_pool_capacity) : 0;
    size_t _extra_left = (g_kmm_v4_extra_capacity > _extra_used) ? (g_kmm_v4_extra_capacity - _extra_used) : 0;
    return _primary_left + _extra_left + kmm_v4_survivor_available();
#endif
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
    // 修复 Bug 3：补乘法溢出检查，与 kmm_scoped_allocator_v4.c:267 的动态池版对齐。
    // 之前直接 num * size，当 num/size 很大时静默回绕成一个小的 total，
    // 随后分配出过小的缓冲区并被零填充——调用方以为拿到了 num*size 字节，实为堆溢出。
    if (num == 0 || size == 0) return NULL;
    if (size > SIZE_MAX / num) {
        return NULL;  // 溢出
    }
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
    // 方案 A：初始化 survivor slab 桶
    static const size_t bucket_sizes[KMM_V4_SLAB_BUCKETS] = {8,16,32,64,128,256,512,1024,2048};
    size_t slab_total = 0;
    for (int i = 0; i < KMM_V4_SLAB_BUCKETS; i++) {
        size_t bsize = bucket_sizes[i];
        g_kmm_v4_survivor.buckets[i].bucket_size = bsize;
        g_kmm_v4_survivor.buckets[i].allocated = 0;
        g_kmm_v4_survivor.buckets[i].slots = g_kmm_v4_survivor_storage + slab_total;
        slab_total += bsize * KMM_V4_SLAB_SLOTS;
        memset(g_kmm_v4_survivor.buckets[i].bitmap, 0, sizeof(g_kmm_v4_survivor.buckets[i].bitmap));
#if KMM_THREAD_SAFETY_LEVEL >= 1
        __atomic_store_n(&g_kmm_v4_survivor.buckets[i].free_hint, 0, __ATOMIC_RELAXED);
#else
        g_kmm_v4_survivor.buckets[i].free_hint = 0;
#endif
    }
    // 大对象区：slab 桶之后的剩余空间
    g_kmm_v4_survivor.large_base = g_kmm_v4_survivor_storage + slab_total;
    g_kmm_v4_survivor.large_capacity = KMM_V4_SURVIVOR_REGION_SIZE - slab_total;
    KMM_ATOMIC_STORE(g_kmm_v4_survivor.large_offset, 0);
    g_kmm_v4_survivor.initialized = true;
}

static inline void kmm_v4_destroy_pool(void) {
    KMM_ATOMIC_STORE(g_kmm_v4_offset, 0);
    KMM_ATOMIC_STORE(g_kmm_v4_survivor.large_offset, 0);
    g_kmm_v4_survivor.initialized = false;
}
#else
void* kmm_v4_malloc(size_t size);
void* kmm_v4_calloc(size_t num, size_t size);
void  kmm_v4_free(void* ptr);
void* kmm_v4_realloc(void* ptr, size_t size);
void* kmm_v4_strdup(const char* s);
void  kmm_v4_init_pool(size_t reserved);
void  kmm_v4_destroy_pool(void);
#endif

// ==================== 编译期检查 ====================
_Static_assert(KMM_V4_POOL_SIZE > 0, "Pool size must be positive");
_Static_assert((KMM_V4_ALIGNMENT & (KMM_V4_ALIGNMENT - 1)) == 0, "Alignment must be power of 2");

#endif // KMM_SCOPED_ALLOCATOR_V4_H
