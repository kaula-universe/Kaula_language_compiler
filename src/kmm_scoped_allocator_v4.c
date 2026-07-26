#include "kmm_scoped_allocator_v4.h"

#ifndef KMM_V4_STATIC_POOL
#ifdef _WIN32
#include <windows.h>
#else
#include <sys/mman.h>   // mmap, PROT_READ, MAP_PRIVATE 等
#include <unistd.h>      // sysconf
#endif
#endif
#include <string.h>

// ==================== 按需提交的虚拟内存池 ====================
// 修复 #25：reserve 大小使用 KMM_V4_POOL_SIZE 宏，受 SOR 估算影响
// 修复 #3：pool_ensure_init 使用 call_once / 双检锁保证线程安全

#ifdef KMM_V4_STATIC_POOL
// freestanding 模式：静态数组池，由链接脚本或编译器在 BSS 段分配
uint8_t g_kmm_v4_pool[KMM_V4_POOL_SIZE];
size_t g_kmm_v4_pool_capacity = KMM_V4_POOL_SIZE;
#else
uint8_t* g_kmm_v4_pool = NULL;
size_t g_kmm_v4_pool_capacity = 0;
#endif

#if KMM_THREAD_SAFETY_LEVEL >= 1
KMM_ATOMIC_TYPE g_kmm_v4_offset = 0;
#else
size_t g_kmm_v4_offset = 0;
#endif

KMM_TLS kmm_scope_stack_t g_kmm_v4_scope_stack = { .offsets = {0}, .depth = 0 };

// 修复 #1：per-thread heap 替代旧的 TLAB
KMM_TLS kmm_thread_heap_t g_kmm_v4_thread_heap = { NULL, 0, 0, 0 };

#ifdef KMM_V4_DEBUG
#if KMM_THREAD_SAFETY_LEVEL >= 1
KMM_ATOMIC_TYPE g_kmm_v4_peak = 0;
KMM_ATOMIC_TYPE g_kmm_v4_alloc_count = 0;
#else
size_t g_kmm_v4_peak = 0;
size_t g_kmm_v4_alloc_count = 0;
#endif
#endif

kmm_context_t g_kmm_ctx = { .is_initialized = false };

// 已提交的字节数（始终 >= g_kmm_v4_offset）
static size_t g_committed_bytes = 0;

// 修复 #3：线程安全的一次性初始化
#if KMM_THREAD_SAFETY_LEVEL >= 1
#if !defined(KMM_V4_STATIC_POOL)
// 使用原子标志做双检锁
static KMM_ATOMIC_TYPE g_pool_init_flag = 0;
#endif
#endif

// ==================== 平台虚拟内存 ====================

static void pool_ensure_init(void) {
#ifdef KMM_V4_STATIC_POOL
    // freestanding 模式：池已在 BSS 段静态分配，无需 OS 调用
    if (g_committed_bytes == 0) {
        g_committed_bytes = 0;
        g_kmm_v4_offset = 0;
    }
#else
    // 修复 #3：双检锁保证多线程下只初始化一次
    if (g_kmm_v4_pool != NULL) return;

#if KMM_THREAD_SAFETY_LEVEL >= 1
    // CAS 标志：0=未初始化, 1=初始化中, 2=已完成
    size_t expected = 0;
    if (!KMM_ATOMIC_CAS_STRONG(g_pool_init_flag, expected, 1)) {
        // 其他线程正在初始化，等待完成
        while (KMM_ATOMIC_LOAD(g_pool_init_flag) != 2) {
            // 自旋等待
        }
        return;
    }
#endif

    // 修复 #25：使用 KMM_V4_POOL_SIZE 宏，受 SOR 估算影响
    size_t reserve_size = KMM_V4_POOL_SIZE;
#ifdef _WIN32
    g_kmm_v4_pool = (uint8_t*)VirtualAlloc(NULL, reserve_size, MEM_RESERVE, PAGE_READWRITE);
#else
    void* p = mmap(NULL, reserve_size, PROT_READ|PROT_WRITE, MAP_PRIVATE|MAP_ANONYMOUS, -1, 0);
    g_kmm_v4_pool = (p == MAP_FAILED) ? NULL : (uint8_t*)p;
#endif
    if (g_kmm_v4_pool) {
        g_kmm_v4_pool_capacity = reserve_size;
        g_committed_bytes = 0;
    }

#if KMM_THREAD_SAFETY_LEVEL >= 1
    KMM_ATOMIC_STORE(g_pool_init_flag, 2);
#endif
#endif
}

// 提交从 base+committed 到 base+needed 之间的页面
// 优化：使用批量提交粒度，减少 VirtualAlloc 调用频率
static void pool_commit_up_to(size_t needed) {
    if (needed <= g_committed_bytes) return;
#ifdef KMM_V4_STATIC_POOL
    // freestanding 模式：静态池已全部"提交"，直接更新计数
    if (needed > g_kmm_v4_pool_capacity) needed = g_kmm_v4_pool_capacity;
    g_committed_bytes = needed;
#else
    // 对齐到 KMM_BATCH_COMMIT_SIZE 边界（批量提交，减少系统调用）
    size_t commit_end = (needed + KMM_BATCH_COMMIT_SIZE - 1) & ~(KMM_BATCH_COMMIT_SIZE - 1);
    if (commit_end > g_kmm_v4_pool_capacity) commit_end = g_kmm_v4_pool_capacity;
    size_t commit_size = commit_end - g_committed_bytes;
    if (commit_size == 0) return;
#ifdef _WIN32
    void* result = VirtualAlloc(g_kmm_v4_pool + g_committed_bytes, commit_size,
                                 MEM_COMMIT, PAGE_READWRITE);
    if (result) g_committed_bytes = commit_end;
#else
    // Linux: mmap pages are automatically committed on first access
    g_committed_bytes = commit_end;
#endif
#endif
}

// 供 header inline 函数调用的提交接口
__attribute__((used))
void kmm_v4_pool_commit(size_t needed) {
    pool_commit_up_to(needed);
}

// ==================== Per-Thread Heap 机制（修复 #1） ====================
// 每个线程从全局池批量获取一块内存，后续分配在 TLS 内完成，无原子操作
// 只有当 thread heap 耗尽时才回退到全局 CAS

// 为当前线程填充 thread heap
// min_needed: 最小需要的字节数（用于大对象直接分配）
// 返回 thread heap 的起始地址，如果失败返回 NULL
uint8_t* kmm_v4_thread_heap_refill(size_t min_needed) {
    pool_ensure_init();
    if (!g_kmm_v4_pool) return NULL;

    // thread heap 大小：至少 KMM_TLS_BUFFER_SIZE，或 min_needed（对齐后）
    const size_t mask = KMM_V4_ALIGNMENT - 1;
    size_t aligned_min = (min_needed + mask) & ~mask;
    size_t heap_size = KMM_TLS_BUFFER_SIZE;
    if (aligned_min > heap_size) {
        // 大对象：直接从全局池分配，不放入 thread heap
        // 这种情况下调用者应走全局 CAS 慢路径
        return NULL;
    }

#if KMM_THREAD_SAFETY_LEVEL >= 1
    // 使用 CAS 从全局池获取一大块内存
    size_t offset = KMM_ATOMIC_LOAD(g_kmm_v4_offset);
    size_t new_offset;
    do {
        new_offset = offset + heap_size;
        if (KMM_V4_UNLIKELY(new_offset > g_kmm_v4_pool_capacity)) {
            return NULL;  // 全局池耗尽
        }
    } while (KMM_V4_UNLIKELY(!KMM_ATOMIC_CAS_WEAK(g_kmm_v4_offset, offset, new_offset)));

    pool_commit_up_to(new_offset);

    // 设置当前线程的 thread heap
    g_kmm_v4_thread_heap.base = g_kmm_v4_pool + offset;
    g_kmm_v4_thread_heap.offset = 0;
    g_kmm_v4_thread_heap.capacity = heap_size;
    g_kmm_v4_thread_heap.total_allocated += heap_size;

    return g_kmm_v4_thread_heap.base;
#else
    // 单线程模式：直接分配
    size_t offset = g_kmm_v4_offset;
    size_t new_offset = offset + heap_size;
    if (KMM_V4_UNLIKELY(new_offset > g_kmm_v4_pool_capacity)) {
        return NULL;
    }

    g_kmm_v4_offset = new_offset;
    pool_commit_up_to(new_offset);

    g_kmm_v4_thread_heap.base = g_kmm_v4_pool + offset;
    g_kmm_v4_thread_heap.offset = 0;
    g_kmm_v4_thread_heap.capacity = heap_size;
    g_kmm_v4_thread_heap.total_allocated += heap_size;

    return g_kmm_v4_thread_heap.base;
#endif
}

// 失效当前线程的 thread heap（用于 reset 等操作）
__attribute__((used))
void kmm_v4_thread_heap_invalidate(void) {
    g_kmm_v4_thread_heap.base = NULL;
    g_kmm_v4_thread_heap.offset = 0;
    g_kmm_v4_thread_heap.capacity = 0;
}

// ==================== 核心分配 ====================
// 修复 #6：统一不加 header，kmm_v4_free 为 no-op
// 所有分配路径（malloc/calloc/alloc_auto/bump/strdup）行为一致

__attribute__((used))
void* kmm_v4_malloc(size_t size) {
    return kmm_v4_alloc_auto(size);
}

// 修复 #6：free 为 no-op，bump allocator 靠 scope 回收
__attribute__((used))
void kmm_v4_free(void* ptr) {
    (void)ptr;
}

// 修复 #2：realloc 简化为 always-copy + 区间检查
// 修复 #8：检查 ptr 是否在 pool 内
__attribute__((used))
void* kmm_v4_realloc(void* ptr, size_t size) {
    if (!ptr) return kmm_v4_malloc(size);
    if (size == 0) {
        kmm_v4_free(ptr);
        return NULL;
    }

    // 修复 #8：检查 ptr 是否在 pool 区间内
    if (g_kmm_v4_pool != NULL &&
        (uint8_t*)ptr >= g_kmm_v4_pool &&
        (uint8_t*)ptr < g_kmm_v4_pool + g_kmm_v4_pool_capacity) {
        // pool 内指针：always-copy（无法得知 old size，保守复制 size 字节）
        // 注意：由于无 header，无法得知 old size，复制 min(size, 可用空间) 字节
        void* p = kmm_v4_malloc(size);
        if (p) {
            // 保守复制：只复制到 pool 末尾的字节数，避免越界读
            size_t avail = (size_t)(g_kmm_v4_pool + g_kmm_v4_pool_capacity - (uint8_t*)ptr);
            size_t copy_size = (size < avail) ? size : avail;
            memcpy(p, ptr, copy_size);
        }
        return p;
    }

    // 来自 libc fallback 的指针：走 libc realloc
    #if KMM_V4_ENABLE_FALLBACK
        return realloc(ptr, size);
    #else
        // 严格模式：无法处理外部指针，返回新分配
        void* p = kmm_v4_malloc(size);
        if (p) {
            // 无法得知 old size，不复制（调用方需自行处理）
        }
        return p;
    #endif
}

__attribute__((used))
void* kmm_v4_calloc(size_t count, size_t size) {
    // 检查溢出
    if (count == 0 || size == 0) return NULL;
    if (size > SIZE_MAX / count) {
        return NULL;  // 溢出
    }

    size_t total = count * size;
    void* p = kmm_v4_malloc(total);
    if (p) {
        memset(p, 0, total);
    }
    return p;
}

void* kmm_v4_strdup(const char* s) {
    if (!s) return NULL;
    size_t len = strlen(s) + 1;
    void* p = kmm_v4_malloc(len);
    if (p) memcpy(p, s, len);
    return p;
}

#ifndef KMM_V4_STATIC_POOL
void kmm_v4_init_pool(size_t initial_size) {
    pool_ensure_init();
    if (initial_size > 0) pool_commit_up_to(initial_size);
    g_kmm_v4_offset = 0;
    kmm_v4_thread_heap_invalidate();
}

void kmm_v4_destroy_pool(void) {
    if (g_kmm_v4_pool) {
#ifdef _WIN32
        VirtualFree(g_kmm_v4_pool, 0, MEM_RELEASE);
#else
        munmap(g_kmm_v4_pool, g_kmm_v4_pool_capacity);
#endif
        g_kmm_v4_pool = NULL;
        g_kmm_v4_pool_capacity = 0;
        g_committed_bytes = 0;
        g_kmm_v4_offset = 0;
        kmm_v4_thread_heap_invalidate();
    }
}
#endif
