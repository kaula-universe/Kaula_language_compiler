#include "kmm_scoped_allocator_v4.h"

#ifdef _WIN32
#include <windows.h>
#endif
#include <string.h>

// ==================== 按需提交的虚拟内存池 ====================
// MEM_RESERVE 预留大块虚拟地址空间（不占物理内存）
// 每次分配跨过 64KB 边界时，提交新页面

#define PAGE_COMMIT_SIZE (64 * 1024)  // 64KB 提交粒度

uint8_t* g_kmm_v4_pool = NULL;
size_t g_kmm_v4_pool_capacity = 0;

#if KMM_THREAD_SAFETY_LEVEL >= 1
KMM_ATOMIC_TYPE g_kmm_v4_offset = 0;
#else
size_t g_kmm_v4_offset = 0;
#endif

KMM_TLS kmm_scope_stack_t g_kmm_v4_scope_stack = { .offsets = {0}, .depth = 0 };

#ifdef KMM_V4_DEBUG
size_t g_kmm_v4_peak = 0;
size_t g_kmm_v4_alloc_count = 0;
#endif

kmm_context_t g_kmm_ctx = { .is_initialized = false };

// 已提交的字节数（始终 >= g_kmm_v4_offset）
static size_t g_committed_bytes = 0;

// ==================== 平台虚拟内存 ====================

static void pool_ensure_init(void) {
    if (g_kmm_v4_pool) return;
    size_t reserve_size = 256 * 1024 * (size_t)1024; // 256MB 虚拟地址
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
}

// 提交从 base+committed 到 base+needed 之间的页面
static void pool_commit_up_to(size_t needed) {
    if (needed <= g_committed_bytes) return;
    // 对齐到 PAGE_COMMIT_SIZE 边界
    size_t commit_end = (needed + PAGE_COMMIT_SIZE - 1) & ~(PAGE_COMMIT_SIZE - 1);
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
}

// 供 header inline 函数调用的提交接口
__attribute__((used))
void kmm_v4_pool_commit(size_t needed) {
    pool_commit_up_to(needed);
}

// 前置声明
void* kmm_v4_malloc(size_t size);

// ==================== 核心分配 ====================

__attribute__((used))
void* kmm_v4_malloc(size_t size) {
    pool_ensure_init();
    if (!g_kmm_v4_pool) return NULL;

    const size_t mask = KMM_V4_ALIGNMENT - 1;
    size_t aligned = (size + mask) & ~mask;

    size_t off = KMM_ATOMIC_LOAD(g_kmm_v4_offset);
    size_t new_off = off + aligned;
    if (new_off > g_kmm_v4_pool_capacity) return NULL;

    pool_commit_up_to(new_off);
    KMM_ATOMIC_STORE(g_kmm_v4_offset, new_off);
    return g_kmm_v4_pool + off;
}

__attribute__((used))
void kmm_v4_free(void* ptr) {
    (void)ptr;
}

// ==================== 兼容接口 ====================

void* kmm_v4_realloc(void* ptr, size_t size) {
    if (!ptr) return kmm_v4_malloc(size);
    if (size == 0) return NULL;
    void* p = kmm_v4_malloc(size);
    if (p) memcpy(p, ptr, size);
    return p;
}

__attribute__((used))
void* kmm_v4_calloc(size_t count, size_t size) {
    size_t total = count * size;
    void* p = kmm_v4_malloc(total);
    if (p) memset(p, 0, total);
    return p;
}

void* kmm_v4_strdup(const char* s) {
    if (!s) return NULL;
    size_t len = strlen(s) + 1;
    void* p = kmm_v4_malloc(len);
    if (p) memcpy(p, s, len);
    return p;
}

void kmm_v4_init_pool(size_t initial_size) {
    pool_ensure_init();
    if (initial_size > 0) pool_commit_up_to(initial_size);
    g_kmm_v4_offset = 0;
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
    }
}
