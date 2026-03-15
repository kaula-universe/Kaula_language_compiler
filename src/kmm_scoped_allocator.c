#ifndef KMM_SCOPED_ALLOCATOR_IMPL_H
#define KMM_SCOPED_ALLOCATOR_IMPL_H

#include "kmm_scoped_allocator.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#ifdef _WIN32
#include <windows.h>
#ifndef CLOCK_MONOTONIC
#define CLOCK_MONOTONIC 0
#endif

// Windows 上定义 timespec 结构
struct timespec {
    long tv_sec;
    long tv_nsec;
};

static inline int clock_gettime(int clk_id, struct timespec* ts) {
    static LARGE_INTEGER frequency;
    static int initialized = 0;
    LARGE_INTEGER counter;
    if (!initialized) {
        QueryPerformanceFrequency(&frequency);
        initialized = 1;
    }
    QueryPerformanceCounter(&counter);
    ts->tv_sec = counter.QuadPart / frequency.QuadPart;
    ts->tv_nsec = ((counter.QuadPart % frequency.QuadPart) * 1000000000) / frequency.QuadPart;
    (void)clk_id;
    return 0;
}
#endif

// ==================== 内部工具函数 ====================
static inline size_t kmm_align_up(size_t size, size_t alignment) {
    return (size + alignment - 1) & ~(alignment - 1);
}

// ==================== 线程本地缓存 ====================
#if KMM_ENABLE_THREAD_CACHE
#ifdef _WIN32
__declspec(thread) kmm_thread_cache_t g_thread_cache = {0};
#else
__thread kmm_thread_cache_t g_thread_cache = {0};
#endif

static inline void kmm_thread_cache_init(kmm_context_t* ctx) {
    g_thread_cache.cache_size = 0;
    g_thread_cache.global_ctx = ctx;
}

static inline void* kmm_thread_cache_alloc(size_t size) {
    if (g_thread_cache.cache_size > 0) {
        return g_thread_cache.cache[--g_thread_cache.cache_size];
    }
    return NULL;
}

static inline void kmm_thread_cache_free(void* ptr) {
    if (g_thread_cache.cache_size < KMM_THREAD_CACHE_SIZE) {
        g_thread_cache.cache[g_thread_cache.cache_size++] = ptr;
    }
}

static inline void* kmm_thread_cache_alloc_batch(kmm_context_t* ctx, size_t size, size_t batch_size) {
    void* batch = kmm_alloc(ctx, size * batch_size, "<thread_cache>", 0);
    if (!batch) return NULL;
    
    // 填充线程缓存
    uint8_t* base = (uint8_t*)batch;
    for (size_t i = 1; i < batch_size && g_thread_cache.cache_size < KMM_THREAD_CACHE_SIZE; i++) {
        g_thread_cache.cache[g_thread_cache.cache_size++] = base + i * size;
    }
    
    return batch;
}
#endif

// SIMD 优化的内存清零（如果支持的话）
#ifdef __AVX2__
#include <immintrin.h>
static inline void fast_zero(void* ptr, size_t size) {
    __m256i zero = _mm256_setzero_si256();
    uint8_t* p = (uint8_t*)ptr;
    
    // 每次清理 32 bytes
    while (size >= 32) {
        _mm256_storeu_si256((__m256i*)p, zero);
        p += 32;
        size -= 32;
    }
    
    // 剩余部分用传统方法清理
    if (size > 0) {
        memset(p, 0, size);
    }
}
#elif defined(__SSE2__)
#include <emmintrin.h>
static inline void fast_zero(void* ptr, size_t size) {
    __m128i zero = _mm_setzero_si128();
    uint8_t* p = (uint8_t*)ptr;
    
    // 每次清理 16 bytes
    while (size >= 16) {
        _mm_storeu_si128((__m128i*)p, zero);
        p += 16;
        size -= 16;
    }
    
    // 剩余部分用传统方法清理
    if (size > 0) {
        memset(p, 0, size);
    }
}
#else
static inline void fast_zero(void* ptr, size_t size) {
    memset(ptr, 0, size);
}
#endif

static inline double get_time_us(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return ts.tv_sec * 1000000.0 + ts.tv_nsec / 1000.0;
}

#define KMM_IS_TINY(size)    ((size) <= KMM_SIZE_TINY)
#define KMM_IS_SMALL(size)   ((size) <= KMM_SIZE_SMALL)
#define KMM_IS_MEDIUM(size)  ((size) <= KMM_SIZE_MEDIUM)
#define KMM_IS_LARGE(size)   ((size) > KMM_SIZE_MEDIUM)

#define KMM_LIKELY(x)       __builtin_expect(!!(x), 1)
#define KMM_UNLIKELY(x)     __builtin_expect(!!(x), 0)

// ==================== 安全堆分配器 ====================
static inline size_t kmm_safe_block_total_size(size_t user_size) {
    return sizeof(kmm_safe_header_t) + KMM_REDZONE_SIZE + user_size + KMM_REDZONE_SIZE;
}

static inline kmm_safe_header_t* kmm_get_header_from_user(void* user_ptr) {
    uint8_t* raw = (uint8_t*)user_ptr - KMM_REDZONE_SIZE - sizeof(kmm_safe_header_t);
    return (kmm_safe_header_t*)raw;
}

static inline bool kmm_check_redzone(void* user_ptr) {
    kmm_safe_header_t* hdr = kmm_get_header_from_user(user_ptr);
    
    if (KMM_UNLIKELY(hdr->canary != KMM_CANARY_VALUE)) {
        return false;
    }
    
    uint8_t* raw = (uint8_t*)hdr;
    uint8_t* front_redzone = raw + sizeof(kmm_safe_header_t);
    for (int i = 0; i < KMM_REDZONE_SIZE; i++) {
        if (KMM_UNLIKELY(front_redzone[i] != KMM_REDZONE_PATTERN)) {
            return false;
        }
    }
    
    uint8_t* user_mem = (uint8_t*)user_ptr;
    uint8_t* back_redzone = user_mem + hdr->user_size;
    for (int i = 0; i < KMM_REDZONE_SIZE; i++) {
        if (KMM_UNLIKELY(back_redzone[i] != KMM_REDZONE_PATTERN)) {
            return false;
        }
    }
    
    return true;
}

static inline void* kmm_safe_malloc(size_t size, const char* file, int line) {
    if (KMM_UNLIKELY(size == 0)) return NULL;
    
    size_t aligned_size = kmm_align_up(size, KMM_ALIGNMENT);
    size_t total = kmm_safe_block_total_size(aligned_size);
    
    uint8_t* raw = (uint8_t*)malloc(total);
    if (KMM_UNLIKELY(!raw)) return NULL;
    
    kmm_safe_header_t* hdr = (kmm_safe_header_t*)raw;
    hdr->user_size = aligned_size;
    hdr->file = file;
    hdr->line = line;
    hdr->canary = KMM_CANARY_VALUE;
    
    uint8_t* front_redzone = raw + sizeof(kmm_safe_header_t);
    memset(front_redzone, KMM_REDZONE_PATTERN, KMM_REDZONE_SIZE);
    
    uint8_t* user_ptr = front_redzone + KMM_REDZONE_SIZE;
    
    uint8_t* back_redzone = user_ptr + aligned_size;
    memset(back_redzone, KMM_REDZONE_PATTERN, KMM_REDZONE_SIZE);
    
    fast_zero(user_ptr, aligned_size);
    
    return user_ptr;
}

static inline void kmm_safe_free(void* user_ptr) {
    if (KMM_UNLIKELY(!user_ptr)) return;
    
    if (KMM_UNLIKELY(!kmm_check_redzone(user_ptr))) {
        kmm_safe_header_t* hdr = kmm_get_header_from_user(user_ptr);
        fprintf(stderr, "🚨 内存损坏检测！文件：%s, 行：%d\n", hdr->file, hdr->line);
        abort();
    }
    
    kmm_safe_header_t* hdr = kmm_get_header_from_user(user_ptr);
    free(hdr);
}

// ==================== Arena 管理 ====================
static inline int kmm_arena_init(kmm_arena_t* arena, size_t size) {
    arena->buffer = (uint8_t*)malloc(size);
    if (KMM_UNLIKELY(!arena->buffer)) return -1;
    
    arena->size = size;
    arena->offset = 0;
    arena->peak = 0;
    arena->allocations = 0;
    arena->reset_count = 0;
    
    return 0;
}

static inline void* kmm_arena_alloc(kmm_arena_t* arena, size_t size) {
    // 预取下一个可能访问的位置，减少缓存未命中
    __builtin_prefetch(arena->buffer + arena->offset + 64, 1, 3);
    
    size_t aligned_size = kmm_align_up(size, KMM_ALIGNMENT);
    size_t aligned_offset = kmm_align_up(arena->offset, KMM_ALIGNMENT);
    
    if (KMM_UNLIKELY(aligned_offset + aligned_size > arena->size)) {
        return NULL;
    }
    
    void* ptr = arena->buffer + aligned_offset;
    arena->offset = aligned_offset + aligned_size;
    arena->allocations++;
    
    if (arena->offset > arena->peak) {
        arena->peak = arena->offset;
    }
    
    return ptr;
}

static inline void* kmm_arena_alloc_tiny(kmm_arena_t* arena, size_t size) {
    // 预取优化
    __builtin_prefetch(arena->buffer + arena->offset + 64, 1, 3);
    
    size_t new_offset = arena->offset + size;
    
    if (KMM_LIKELY(new_offset <= arena->size)) {
        void* ptr = arena->buffer + arena->offset;
        arena->offset = new_offset;
        arena->allocations++;
        
        if (new_offset > arena->peak) {
            arena->peak = new_offset;
        }
        
        return ptr;
    }
    
    return NULL;
}

#if KMM_ENABLE_ARENA_RESET
static inline void kmm_arena_reset(kmm_arena_t* arena) {
    arena->offset = 0;
    arena->reset_count++;
}
#endif

// ==================== 清理栈管理 ====================
static inline int kmm_register_cleanup(kmm_context_t* ctx, void* ptr) {
    kmm_cleanup_node_t* node = (kmm_cleanup_node_t*)malloc(sizeof(kmm_cleanup_node_t));
    if (KMM_UNLIKELY(!node)) return -1;
    
    node->resource = ptr;
    node->cleanup = kmm_safe_free;
    node->next = ctx->cleanup_stack;
    ctx->cleanup_stack = node;
    
    return 0;
}

// ==================== 空闲列表管理 ====================
#if KMM_ENABLE_FREE_LIST
static inline void* kmm_free_list_alloc(kmm_context_t* ctx, size_t size) {
    kmm_free_block_t* block = ctx->free_list;
    kmm_free_block_t* prev = NULL;
    
    while (block) {
        if (block->size >= size) {
            // 找到合适的空闲块
            if (prev) {
                prev->next = block->next;
            } else {
                ctx->free_list = block->next;
            }
            
#if KMM_ENABLE_STATS
            ctx->stats.free_list_hits++;
#endif
            return block;
        }
        prev = block;
        block = block->next;
    }
    
    return NULL;
}

static inline void kmm_free_list_free(kmm_context_t* ctx, void* ptr, size_t size) {
    kmm_free_block_t* block = (kmm_free_block_t*)ptr;
    block->size = size;
    block->next = ctx->free_list;
    ctx->free_list = block;
}
#endif

#if KMM_ENABLE_ARENA_RESET
static inline void kmm_reset_if_needed(kmm_context_t* ctx) {
    ctx->alloc_counter++;
    
    if (ctx->alloc_counter >= KMM_RESET_BATCH_SIZE) {
        kmm_arena_reset(&ctx->tiny_arena);
        kmm_arena_reset(&ctx->small_arena);
        kmm_arena_reset(&ctx->medium_arena);
        ctx->alloc_counter = 0;
        
#if KMM_ENABLE_STATS
        ctx->stats.tiny_resets++;
        ctx->stats.small_resets++;
        ctx->stats.medium_resets++;
#endif
    }
}
#endif

// ==================== 公共 API 实现 ====================

int kmm_init(kmm_context_t* ctx) {
    if (KMM_UNLIKELY(kmm_arena_init(&ctx->tiny_arena, KMM_ARENA_TINY_SIZE) != 0)) return -1;
    if (KMM_UNLIKELY(kmm_arena_init(&ctx->small_arena, KMM_ARENA_SMALL_SIZE) != 0)) return -1;
    if (KMM_UNLIKELY(kmm_arena_init(&ctx->medium_arena, KMM_ARENA_MEDIUM_SIZE) != 0)) return -1;
    
    ctx->cleanup_stack = NULL;
    ctx->alloc_counter = 0;
    ctx->free_list = NULL;
    
#if KMM_ENABLE_STATS
    memset(&ctx->stats, 0, sizeof(ctx->stats));
#endif
    
#if KMM_ENABLE_THREAD_CACHE
    kmm_thread_cache_init(ctx);
#endif
    
    return 0;
}

void kmm_destroy(kmm_context_t* ctx) {
    kmm_cleanup_node_t* current = ctx->cleanup_stack;
    while (current) {
        if (current->cleanup && current->resource) {
            current->cleanup(current->resource);
        }
        kmm_cleanup_node_t* temp = current;
        current = current->next;
        free(temp);
    }
    
#if KMM_ENABLE_FREE_LIST
    kmm_free_block_t* free_block = ctx->free_list;
    while (free_block) {
        kmm_free_block_t* temp = free_block;
        free_block = free_block->next;
        free(temp);
    }
    ctx->free_list = NULL;
#endif
    
    if (ctx->tiny_arena.buffer) free(ctx->tiny_arena.buffer);
    if (ctx->small_arena.buffer) free(ctx->small_arena.buffer);
    if (ctx->medium_arena.buffer) free(ctx->medium_arena.buffer);
    
    ctx->cleanup_stack = NULL;
}

void* kmm_alloc(kmm_context_t* ctx, size_t size, const char* file, int line) {
#if KMM_ENABLE_STATS
    ctx->stats.total_allocs++;
    double start_time = get_time_us();
#endif
    
    void* ptr = NULL;
    
#if KMM_ENABLE_THREAD_CACHE
    // 1. 先尝试线程本地缓存（无锁，最快）
    ptr = kmm_thread_cache_alloc(size);
    if (KMM_LIKELY(ptr)) {
#if KMM_ENABLE_STATS
        ctx->stats.thread_cache_hits++;
        ctx->stats.arena_hits++;
        ctx->stats.tiny_total_time += (get_time_us() - start_time);
#endif
        goto done;
    }
#endif
    
#if KMM_ENABLE_FREE_LIST
    // 2. 尝试从空闲列表分配
    ptr = kmm_free_list_alloc(ctx, size);
    if (KMM_LIKELY(ptr)) {
#if KMM_ENABLE_STATS
        ctx->stats.free_list_hits++;
        ctx->stats.arena_hits++;
        ctx->stats.tiny_total_time += (get_time_us() - start_time);
#endif
        goto done;
    }
#endif
    
    // 3. 空闲列表没有，尝试 Arena 分配
    if (KMM_LIKELY(KMM_IS_TINY(size))) {
        ptr = kmm_arena_alloc_tiny(&ctx->tiny_arena, size);
        if (KMM_LIKELY(ptr)) {
#if KMM_ENABLE_STATS
            ctx->stats.tiny_hits++;
            ctx->stats.arena_hits++;
            ctx->stats.tiny_total_time += (get_time_us() - start_time);
#endif
            goto done;
        }
        
        ptr = kmm_arena_alloc(&ctx->small_arena, size);
        if (ptr) {
#if KMM_ENABLE_STATS
            ctx->stats.small_hits++;
            ctx->stats.arena_hits++;
            ctx->stats.small_total_time += (get_time_us() - start_time);
#endif
            goto done;
        }
    } 
    else if (KMM_LIKELY(KMM_IS_SMALL(size))) {
        ptr = kmm_arena_alloc(&ctx->small_arena, size);
        if (KMM_LIKELY(ptr)) {
#if KMM_ENABLE_STATS
            ctx->stats.small_hits++;
            ctx->stats.arena_hits++;
            ctx->stats.small_total_time += (get_time_us() - start_time);
#endif
            goto done;
        }
        
        ptr = kmm_arena_alloc(&ctx->medium_arena, size);
        if (ptr) {
#if KMM_ENABLE_STATS
            ctx->stats.medium_hits++;
            ctx->stats.arena_hits++;
            ctx->stats.medium_total_time += (get_time_us() - start_time);
#endif
            goto done;
        }
    } 
    else if (KMM_IS_MEDIUM(size)) {
        ptr = kmm_arena_alloc(&ctx->medium_arena, size);
        if (ptr) {
#if KMM_ENABLE_STATS
            ctx->stats.medium_hits++;
            ctx->stats.arena_hits++;
            ctx->stats.medium_total_time += (get_time_us() - start_time);
#endif
            goto done;
        }
    }
    
    // 3. Arena 都失败了，降级到堆
    double heap_start = get_time_us();
    
    ptr = kmm_safe_malloc(size, file, line);
    if (KMM_UNLIKELY(!ptr)) return NULL;
    
    if (KMM_UNLIKELY(kmm_register_cleanup(ctx, ptr) != 0)) {
        kmm_safe_free(ptr);
        return NULL;
    }
    
#if KMM_ENABLE_STATS
    ctx->stats.heap_hits++;
    ctx->stats.large_hits++;
    ctx->stats.heap_total_time += (get_time_us() - heap_start);
#endif

done:
#if KMM_ENABLE_ARENA_RESET
    kmm_reset_if_needed(ctx);
#endif
    
    return ptr;
}

void kmm_free(void* ptr) {
    if (KMM_UNLIKELY(!ptr)) return;
    
    // 检查是否是堆分配（通过检查红区）
    if (kmm_check_redzone(ptr)) {
        kmm_safe_header_t* hdr = kmm_get_header_from_user(ptr);
        free(hdr);
    }
    // Arena 分配的内存在作用域结束时自动释放，不需要单独 free
}

// ==================== 批量分配 API ====================
void** kmm_alloc_batch(kmm_context_t* ctx, size_t size, size_t count, const char* file, int line) {
#if KMM_ENABLE_STATS
    ctx->stats.total_allocs += count;
#endif
    
    // 分配指针数组
    void** ptrs = (void**)kmm_alloc(ctx, count * sizeof(void*), file, line);
    if (KMM_UNLIKELY(!ptrs)) return NULL;
    
    // 连续分配，提高缓存局部性
    uint8_t* base = (uint8_t*)kmm_alloc(ctx, size * count, file, line);
    if (KMM_UNLIKELY(!base)) return NULL;
    
    // 设置指针数组
    for (size_t i = 0; i < count; i++) {
        ptrs[i] = base + i * size;
    }
    
    return ptrs;
}

// ==================== 统计信息 ====================
#if KMM_ENABLE_STATS
void kmm_print_stats(kmm_context_t* ctx) {
    printf("\n📊 ScopedAllocator 统计:\n");
    printf("  总分配次数：%zu\n", ctx->stats.total_allocs);
    printf("  Arena 总命中：%zu (%.1f%%)\n", ctx->stats.arena_hits,
           ctx->stats.total_allocs > 0 ? (ctx->stats.arena_hits * 100.0) / ctx->stats.total_allocs : 0);
    printf("  堆命中：%zu (%.1f%%)\n", ctx->stats.heap_hits,
           ctx->stats.total_allocs > 0 ? (ctx->stats.heap_hits * 100.0) / ctx->stats.total_allocs : 0);
    
#if KMM_ENABLE_THREAD_CACHE || KMM_ENABLE_FREE_LIST
    printf("\n  优化命中详情:\n");
#if KMM_ENABLE_THREAD_CACHE
    printf("    线程缓存命中：%zu (%.1f%%)\n", ctx->stats.thread_cache_hits,
           ctx->stats.total_allocs > 0 ? (ctx->stats.thread_cache_hits * 100.0) / ctx->stats.total_allocs : 0);
#endif
#if KMM_ENABLE_FREE_LIST
    printf("    空闲列表命中：%zu (%.1f%%)\n", ctx->stats.free_list_hits,
           ctx->stats.total_allocs > 0 ? (ctx->stats.free_list_hits * 100.0) / ctx->stats.total_allocs : 0);
#endif
#endif
    
    printf("\n  分层命中详情:\n");
    printf("    微小对象 (Tiny): %zu (%.1f%%)\n", ctx->stats.tiny_hits,
           ctx->stats.total_allocs > 0 ? (ctx->stats.tiny_hits * 100.0) / ctx->stats.total_allocs : 0);
    printf("    小对象 (Small): %zu (%.1f%%)\n", ctx->stats.small_hits,
           ctx->stats.total_allocs > 0 ? (ctx->stats.small_hits * 100.0) / ctx->stats.total_allocs : 0);
    printf("    中对象 (Medium): %zu (%.1f%%)\n", ctx->stats.medium_hits,
           ctx->stats.total_allocs > 0 ? (ctx->stats.medium_hits * 100.0) / ctx->stats.total_allocs : 0);
    printf("    大对象 (Large): %zu (%.1f%%)\n", ctx->stats.large_hits,
           ctx->stats.total_allocs > 0 ? (ctx->stats.large_hits * 100.0) / ctx->stats.total_allocs : 0);
    
    printf("\n  Arena 使用情况:\n");
    printf("    微小 Arena: %zu/%zu bytes (%.1f%%) [%zu 次分配，%zu 次重置]\n", 
           ctx->tiny_arena.offset, ctx->tiny_arena.size,
           (ctx->tiny_arena.offset * 100.0) / ctx->tiny_arena.size,
           ctx->tiny_arena.allocations, ctx->tiny_arena.reset_count);
    printf("    小 Arena: %zu/%zu bytes (%.1f%%) [%zu 次分配，%zu 次重置]\n", 
           ctx->small_arena.offset, ctx->small_arena.size,
           (ctx->small_arena.offset * 100.0) / ctx->small_arena.size,
           ctx->small_arena.allocations, ctx->small_arena.reset_count);
    printf("    中 Arena: %zu/%zu bytes (%.1f%%) [%zu 次分配，%zu 次重置]\n", 
           ctx->medium_arena.offset, ctx->medium_arena.size,
           (ctx->medium_arena.offset * 100.0) / ctx->medium_arena.size,
           ctx->medium_arena.allocations, ctx->medium_arena.reset_count);
    
    if (ctx->stats.heap_hits > 0) {
        printf("\n  性能统计:\n");
        printf("    微小对象平均时间：%.3f μs\n",
               ctx->stats.tiny_hits > 0 ? ctx->stats.tiny_total_time / ctx->stats.tiny_hits : 0);
        printf("    小对象平均时间：%.3f μs\n",
               ctx->stats.small_hits > 0 ? ctx->stats.small_total_time / ctx->stats.small_hits : 0);
        printf("    中对象平均时间：%.3f μs\n",
               ctx->stats.medium_hits > 0 ? ctx->stats.medium_total_time / ctx->stats.medium_hits : 0);
        printf("    堆平均时间：%.3f μs\n",
               ctx->stats.heap_hits > 0 ? ctx->stats.heap_total_time / ctx->stats.heap_hits : 0);
    }
}
#endif

#endif // KMM_SCOPED_ALLOCATOR_IMPL_H
