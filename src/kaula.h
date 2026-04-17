#ifndef KAULA_H
#define KAULA_H

#include <stdint.h>
#include <stdbool.h>
#include <stddef.h>

// ==================== 跨平台支持 ====================
#include "platform.h"

// 平台检测
#if defined(_WIN32) || defined(_WIN64)
    #define KAULA_PLATFORM_WINDOWS 1
    #define KAULA_PLATFORM_UNIX 0
    #define KAULA_PLATFORM_LINUX 0
    #define KAULA_PLATFORM_MACOS 0
#elif defined(__linux__)
    #define KAULA_PLATFORM_WINDOWS 0
    #define KAULA_PLATFORM_UNIX 1
    #define KAULA_PLATFORM_LINUX 1
    #define KAULA_PLATFORM_MACOS 0
#elif defined(__APPLE__) && defined(__MACH__)
    #define KAULA_PLATFORM_WINDOWS 0
    #define KAULA_PLATFORM_UNIX 1
    #define KAULA_PLATFORM_LINUX 0
    #define KAULA_PLATFORM_MACOS 1
#else
    #error "Unsupported platform"
#endif

// 编译器检测
#if defined(__GNUC__) || defined(__clang__)
    #define KAULA_COMPILER_GCC_LIKE 1
    #define KAULA_LIKELY(x) __builtin_expect(!!(x), 1)
    #define KAULA_UNLIKELY(x) __builtin_expect(!!(x), 0)
    #define KAULA_INLINE inline __attribute__((always_inline))
    #define KAULA_NORETURN __attribute__((noreturn))
#elif defined(_MSC_VER)
    #define KAULA_COMPILER_GCC_LIKE 0
    #define KAULA_LIKELY(x) (x)
    #define KAULA_UNLIKELY(x) (x)
    #define KAULA_INLINE __forceinline
    #define KAULA_NORETURN __declspec(noreturn)
#else
    #define KAULA_COMPILER_GCC_LIKE 0
    #define KAULA_LIKELY(x) (x)
    #define KAULA_UNLIKELY(x) (x)
    #define KAULA_INLINE inline
    #define KAULA_NORETURN
#endif

// 线程局部存储 (TLS)
#if KAULA_PLATFORM_WINDOWS
    #define KAULA_TLS __declspec(thread)
#elif KAULA_PLATFORM_UNIX
    #define KAULA_TLS __thread
#else
    #define KAULA_TLS
#endif

// 强制对齐
#if KAULA_COMPILER_GCC_LIKE
    #define KAULA_ALIGNED(x) __attribute__((aligned(x)))
#else
    #define KAULA_ALIGNED(x) __declspec(align(x))
#endif

// 导入/导出符号
#if KAULA_PLATFORM_WINDOWS
    #if defined(_MSC_VER)
        #define KAULA_EXPORT __declspec(dllexport)
        #define KAULA_IMPORT __declspec(dllimport)
    #else
        #define KAULA_EXPORT __attribute__((dllexport))
        #define KAULA_IMPORT __attribute__((dllimport))
    #endif
#else
    #define KAULA_EXPORT __attribute__((visibility("default")))
    #define KAULA_IMPORT
#endif

// ==================== Configuration Constants ====================
#define VO_CACHE_SIZE 2048
#define QUEUE_CAPACITY 100000
#define SPENDABLE_CAPACITY 2048
#define HIGH_PRIORITY 0
#define MEDIUM_PRIORITY 1
#define LOW_PRIORITY 2
#define PRIORITY_LEVELS 3
#define MAX_RECURSION_DEPTH 8
#define MEMORY_POOL_SIZE (256 * 1024 * 1024)

// ==================== KMM 内存管理 ====================
#include "kmm_scoped_allocator_v4.h"

// ==================== VO 模块 ====================
typedef struct VOModule VOModule;

VOModule* vo_create(size_t cache_size);
void vo_destroy(VOModule* vo);
void vo_data_load(VOModule* vo, int index, void* data);
void vo_code_load(VOModule* vo, int index, void (*code)(void*));
void vo_associate(VOModule* vo, int data_index, int code_index);
void* vo_access(VOModule* vo, void* key);

// ==================== Spend/Call 模块 ====================
typedef struct Spendable Spendable;

Spendable* spendable_create(size_t size);
void spendable_destroy(Spendable* sp);
void spendable_add(Spendable* sp, void* component);
void* spendable_call(Spendable* sp);

// ==================== Priority Queue 模块 ====================
typedef struct PriorityQueue PriorityQueue;

PriorityQueue* priority_queue_create(size_t capacity);
void priority_queue_destroy(PriorityQueue* pq);
void priority_queue_add(PriorityQueue* pq, int priority, void (*func)(void*), void* arg);
void* priority_queue_execute_next(PriorityQueue* pq);
int priority_queue_batch_add(PriorityQueue* pq, int priority, void (*func)(void*), void** args, int count);
int priority_queue_batch_execute(PriorityQueue* pq, int max_tasks);

// ==================== Prefix System 模块 ====================
typedef struct PrefixSystem PrefixSystem;

PrefixSystem* prefix_system_create(void);
void prefix_system_destroy(PrefixSystem* ps);
void prefix_enter(PrefixSystem* ps, const char* name);
void prefix_leave(PrefixSystem* ps);

// ==================== Tree System 模块 ====================
typedef struct Tree Tree;
typedef struct TreeNode TreeNode;

Tree* tree_create(void);
void tree_destroy(Tree* tree);
void tree_set_root(Tree* tree, void* value);
void tree_add_node(Tree* tree, const char* parent_name, const char* node_name, void* value);
void* tree_get_node(Tree* tree, const char* node_name);

// ==================== Time 模块 ====================
typedef struct TimeModule TimeModule;

TimeModule* time_create(void);
void time_destroy(TimeModule* tm);
double time_now(TimeModule* tm);
void time_sleep(TimeModule* tm, double seconds);

// ==================== Fast Allocator ====================
typedef struct FastAllocator {
    uint8_t* base;
    size_t offset;
} FastAllocator;

extern FastAllocator global_allocator;

void fast_allocator_init();
void* fast_alloc(size_t size);
void* fast_calloc(size_t num, size_t size);
void fast_free(void* ptr);

#endif // KAULA_H