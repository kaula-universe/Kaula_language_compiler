#include "kmm_scoped_allocator_v4.h"

#ifdef _WIN32
#include <windows.h>
#endif
#include <string.h>

// ==================== 按需提交的虚拟内存池 ====================
// MEM_RESERVE 预留大块虚拟地址空间（不占物理内存）
// 批量提交优化：每次提交 4MB，减少 VirtualAlloc 调用频率

#define BATCH_COMMIT_SIZE (4 * 1024 * 1024)  // 4MB 批量提交粒度

uint8_t* g_kmm_v4_pool = NULL;
size_t g_kmm_v4_pool_capacity = 0;

#if KMM_THREAD_SAFETY_LEVEL >= 1
KMM_ATOMIC_TYPE g_kmm_v4_offset = 0;
#else
size_t g_kmm_v4_offset = 0;
#endif

KMM_TLS kmm_scope_stack_t g_kmm_v4_scope_stack = { .offsets = {0}, .depth = 0 };

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

// ==================== 线程本地分配缓冲区 ====================
// 每个线程从全局池批量获取一块内存，后续分配在 TLS 内完成，无原子操作
// 只有当 TLS 缓冲区耗尽时才回退到全局 CAS
KMM_TLS kmm_tls_buffer_t g_kmm_v4_tls_buffer = { NULL, 0, 0 };

// ==================== Size-class 空闲链表 ====================
// 用于快速复用已释放的小对象，改善逐次分配释放场景
static kmm_free_list_t g_kmm_free_list = {
    .free_lists = {NULL},
    .free_counts = {0},
    .enabled = true
};

// 获取 size class 索引（返回 -1 表示不在 size class 范围内）
static inline int kmm_get_size_class(size_t size) {
    for (int i = 0; i < KMM_FREE_LIST_CLASSES; i++) {
        if (size <= KMM_FREE_LIST_SIZES[i]) {
            return i;
        }
    }
    return -1;
}

// 从空闲链表分配
static void* kmm_free_list_alloc(size_t size) {
    if (!g_kmm_free_list.enabled) return NULL;
    
    int class_idx = kmm_get_size_class(size);
    if (class_idx < 0) return NULL;
    
    if (g_kmm_free_list.free_lists[class_idx] == NULL) {
        return NULL;
    }
    
    // 从链表头取出一个节点
    kmm_free_node_t* node = g_kmm_free_list.free_lists[class_idx];
    g_kmm_free_list.free_lists[class_idx] = node->next;
    g_kmm_free_list.free_counts[class_idx]--;
    
    return (void*)node;
}

// 将内存块归还到空闲链表
static void kmm_free_list_free(void* ptr, size_t size) {
    if (!g_kmm_free_list.enabled || ptr == NULL) return;
    
    int class_idx = kmm_get_size_class(size);
    if (class_idx < 0) return;
    
    // 检查是否超过每个 class 的最大空闲数量
    if (g_kmm_free_list.free_counts[class_idx] >= KMM_FREE_LIST_MAX_PER_CLASS) {
        return; // 直接丢弃，让 bump allocator 回收
    }
    
    // 插入到链表头
    kmm_free_node_t* node = (kmm_free_node_t*)ptr;
    node->next = g_kmm_free_list.free_lists[class_idx];
    g_kmm_free_list.free_lists[class_idx] = node;
    g_kmm_free_list.free_counts[class_idx]++;
}

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
// 优化：使用批量提交粒度（4MB），减少 VirtualAlloc 调用频率
static void pool_commit_up_to(size_t needed) {
    if (needed <= g_committed_bytes) return;
    // 对齐到 BATCH_COMMIT_SIZE 边界（批量提交，减少系统调用）
    size_t commit_end = (needed + BATCH_COMMIT_SIZE - 1) & ~(BATCH_COMMIT_SIZE - 1);
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

// ==================== TLAB 机制：线程本地分配缓冲区 ====================
// 从全局池批量获取一块内存到 TLS，后续分配无原子操作
// 只有当 TLS 缓冲区耗尽时才回退到全局 CAS

// 为当前线程填充 TLAB
// 返回新 TLAB 的起始地址，如果失败返回 NULL
static uint8_t* kmm_v4_tlab_refill(void) {
    pool_ensure_init();
    if (!g_kmm_v4_pool) return NULL;
    
    const size_t tlab_size = KMM_TLS_BUFFER_SIZE;
    
#if KMM_THREAD_SAFETY_LEVEL >= 1
    // 使用 CAS 从全局池获取一大块内存
    size_t offset = KMM_ATOMIC_LOAD(g_kmm_v4_offset);
    size_t new_offset;
    do {
        new_offset = offset + tlab_size;
        if (KMM_V4_UNLIKELY(new_offset > g_kmm_v4_pool_capacity)) {
            return NULL;  // 全局池耗尽
        }
    } while (KMM_V4_UNLIKELY(!KMM_ATOMIC_CAS(g_kmm_v4_offset, offset, new_offset)));
    
    pool_commit_up_to(new_offset);
    
    // 设置当前线程的 TLAB
    g_kmm_v4_tls_buffer.buffer = g_kmm_v4_pool + offset;
    g_kmm_v4_tls_buffer.remaining = tlab_size;
    g_kmm_v4_tls_buffer.total_allocated += tlab_size;
    
    return g_kmm_v4_tls_buffer.buffer;
#else
    // 单线程模式：直接分配
    size_t offset = g_kmm_v4_offset;
    size_t new_offset = offset + tlab_size;
    if (KMM_V4_UNLIKELY(new_offset > g_kmm_v4_pool_capacity)) {
        return NULL;
    }
    
    g_kmm_v4_offset = new_offset;
    pool_commit_up_to(new_offset);
    
    g_kmm_v4_tls_buffer.buffer = g_kmm_v4_pool + offset;
    g_kmm_v4_tls_buffer.remaining = tlab_size;
    g_kmm_v4_tls_buffer.total_allocated += tlab_size;
    
    return g_kmm_v4_tls_buffer.buffer;
#endif
}

// 失效当前线程的 TLAB（用于 scope_pop 等操作）
__attribute__((used))
void kmm_v4_tlab_invalidate(void) {
    g_kmm_v4_tls_buffer.buffer = NULL;
    g_kmm_v4_tls_buffer.remaining = 0;
}

// 前置声明
void* kmm_v4_malloc(size_t size);

// ==================== 核心分配 ====================

// 分配块 header（用于记录大小，支持 free 归还到空闲链表）
typedef struct {
    size_t size;  // 分配的大小（对齐后）
} kmm_alloc_header_t;

// 内部分配函数（不写 header，用于空闲链表和 TLAB）
static void* kmm_v4_alloc_internal(size_t aligned) {
    // 快速路径：尝试从 TLAB 分配（无原子操作）
    if (KMM_V4_LIKELY(aligned <= KMM_TLS_BUFFER_SIZE / 4)) {
        if (KMM_V4_LIKELY(g_kmm_v4_tls_buffer.remaining >= aligned)) {
            uint8_t* ptr = g_kmm_v4_tls_buffer.buffer;
            g_kmm_v4_tls_buffer.buffer += aligned;
            g_kmm_v4_tls_buffer.remaining -= aligned;
            return ptr;
        }
        
        // TLAB 耗尽，尝试填充
        if (KMM_V4_LIKELY(kmm_v4_tlab_refill() != NULL)) {
            if (KMM_V4_LIKELY(g_kmm_v4_tls_buffer.remaining >= aligned)) {
                uint8_t* ptr = g_kmm_v4_tls_buffer.buffer;
                g_kmm_v4_tls_buffer.buffer += aligned;
                g_kmm_v4_tls_buffer.remaining -= aligned;
                return ptr;
            }
        }
    }

    // 慢路径：直接从全局池分配（CAS 循环）
#if KMM_THREAD_SAFETY_LEVEL >= 1
    size_t off = KMM_ATOMIC_LOAD(g_kmm_v4_offset);
    size_t new_off;
    do {
        new_off = off + aligned;
        if (KMM_V4_UNLIKELY(new_off > g_kmm_v4_pool_capacity)) {
            #if KMM_V4_ENABLE_FALLBACK
                return malloc(aligned);
            #else
                return NULL;
            #endif
        }
    } while (KMM_V4_UNLIKELY(!KMM_ATOMIC_CAS(g_kmm_v4_offset, off, new_off)));
    
    pool_commit_up_to(new_off);
    return g_kmm_v4_pool + off;
#else
    size_t off = g_kmm_v4_offset;
    size_t new_off = off + aligned;
    if (KMM_V4_UNLIKELY(new_off > g_kmm_v4_pool_capacity)) {
        #if KMM_V4_ENABLE_FALLBACK
            return malloc(aligned);
        #else
            return NULL;
        #endif
    }
    
    g_kmm_v4_offset = new_off;
    pool_commit_up_to(new_off);
    return g_kmm_v4_pool + off;
#endif
}

// kmm_v4_malloc: 优化版本，使用空闲链表 + TLAB 快速路径
__attribute__((used))
void* kmm_v4_malloc(size_t size) {
    pool_ensure_init();
    if (!g_kmm_v4_pool) return NULL;

    const size_t mask = KMM_V4_ALIGNMENT - 1;
    size_t aligned = (size + mask) & ~mask;
    size_t total_size = aligned + sizeof(kmm_alloc_header_t);

    // 最快路径：尝试从空闲链表分配（零开销复用）
    void* free_ptr = kmm_free_list_alloc(total_size);
    if (KMM_V4_LIKELY(free_ptr != NULL)) {
        // 空闲链表返回的是 header 位置
        kmm_alloc_header_t* header = (kmm_alloc_header_t*)free_ptr;
        header->size = aligned;  // 更新大小
        return (void*)(header + 1);  // 返回 header 后的位置
    }

    // 分配包含 header 的总大小
    void* raw_ptr = kmm_v4_alloc_internal(total_size);
    if (!raw_ptr) return NULL;

    // 写入 header
    kmm_alloc_header_t* header = (kmm_alloc_header_t*)raw_ptr;
    header->size = aligned;

    // 返回 header 后的位置
    return (void*)(header + 1);
}

__attribute__((used))
void kmm_v4_free(void* ptr) {
    if (!ptr) return;
    
    // 读取 header 获取大小
    kmm_alloc_header_t* header = ((kmm_alloc_header_t*)ptr) - 1;
    size_t size = header->size;
    
    // 归还到空闲链表（归还整个块，包括 header）
    kmm_free_list_free(header, size + sizeof(kmm_alloc_header_t));
}

// ==================== 兼容接口 ====================

// kmm_v4_realloc: 受限的 realloc 实现
// bump allocator 无法高效实现完整的 realloc 语义
// 仅支持对最近分配的块进行原地扩展
// 在 TLAB 模式下，原地扩展需要刷新 TLAB 到全局 offset
__attribute__((used))
void* kmm_v4_realloc(void* ptr, size_t size) {
    if (!ptr) return kmm_v4_malloc(size);
    if (size == 0) {
        kmm_v4_free(ptr);
        return NULL;
    }
    
    size_t ptr_offset = (uint8_t*)ptr - g_kmm_v4_pool;
    
    // 在 TLAB 模式下，需要将 TLAB 的已使用部分刷新到全局 offset
    // 这样才能准确判断 ptr 是否是最近分配的块
    if (g_kmm_v4_tls_buffer.buffer != NULL) {
        // 计算 TLAB 中已使用的字节数
        size_t tlab_used = KMM_TLS_BUFFER_SIZE - g_kmm_v4_tls_buffer.remaining;
        if (tlab_used > 0) {
            // 将 TLAB 已使用部分刷新到全局 offset
            #if KMM_THREAD_SAFETY_LEVEL >= 1
            size_t offset = KMM_ATOMIC_LOAD(g_kmm_v4_offset);
            size_t new_offset = offset + tlab_used;
            if (new_offset <= g_kmm_v4_pool_capacity) {
                KMM_ATOMIC_STORE(g_kmm_v4_offset, new_offset);
                pool_commit_up_to(new_offset);
            }
            #else
            g_kmm_v4_offset += tlab_used;
            pool_commit_up_to(g_kmm_v4_offset);
            #endif
            // 失效 TLAB，强制下次分配时重新填充
            kmm_v4_tlab_invalidate();
        }
    }
    
    #if KMM_THREAD_SAFETY_LEVEL >= 1
    size_t current_offset = KMM_ATOMIC_LOAD(g_kmm_v4_offset);
    #else
    size_t current_offset = g_kmm_v4_offset;
    #endif
    
    const size_t mask = KMM_V4_ALIGNMENT - 1;
    size_t aligned_size = (size + mask) & ~mask;
    
    // 检查 ptr 是否是最近分配的块（ptr 末尾对齐后 <= current_offset）
    if (ptr_offset + aligned_size <= current_offset) {
        // ptr 不是最近分配的块，或者需要收缩
        // 分配新的块并复制
        void* p = kmm_v4_malloc(size);
        if (p) {
            // 保守策略：复制 min(size, current_offset - ptr_offset) 字节
            size_t max_copy = current_offset - ptr_offset;
            size_t copy_size = (size < max_copy) ? size : max_copy;
            memcpy(p, ptr, copy_size);
        }
        return p;
    }
    
    // ptr 可能是最近分配的块，尝试原地扩展
    size_t new_offset = ptr_offset + aligned_size;
    if (new_offset <= g_kmm_v4_pool_capacity) {
        #if KMM_THREAD_SAFETY_LEVEL >= 1
        // 使用 CAS 更新 offset
        size_t old_offset;
        do {
            old_offset = KMM_ATOMIC_LOAD(g_kmm_v4_offset);
            if (old_offset != current_offset) {
                // 其他线程已经修改了 offset，放弃原地扩展
                break;
            }
        } while (!KMM_ATOMIC_CAS(g_kmm_v4_offset, current_offset, new_offset));
        #else
        g_kmm_v4_offset = new_offset;
        #endif
        pool_commit_up_to(new_offset);
        return ptr;
    }
    
    // 无法原地扩展，分配新的块
    void* p = kmm_v4_malloc(size);
    if (p) {
        size_t max_copy = current_offset - ptr_offset;
        size_t copy_size = (size < max_copy) ? size : max_copy;
        memcpy(p, ptr, copy_size);
    }
    return p;
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
        // calloc 语义：清零全部请求的字节
        // kmm_v4_malloc 已经通过 pool_commit_up_to 提交了页面
        // 所以这里可以安全地清零全部 total 字节
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

void kmm_v4_init_pool(size_t initial_size) {
    pool_ensure_init();
    if (initial_size > 0) pool_commit_up_to(initial_size);
    g_kmm_v4_offset = 0;
    // 失效当前线程的 TLAB
    kmm_v4_tlab_invalidate();
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
        // 失效当前线程的 TLAB
        kmm_v4_tlab_invalidate();
    }
}
