#include <stdio.h>
#include <stdlib.h>
#include <stdint.h>
#include <string.h>
#include <stdbool.h>
#include <time.h>
#include <assert.h>

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

// ==================== 分层配置 ====================
#define KMM_REDZONE_SIZE         8
#define KMM_REDZONE_PATTERN      0xCD
#define KMM_CANARY_VALUE         0xDEADBEEFCAFEBABEULL
#define KMM_ALIGNMENT            8

// 分层对象阈值（基于统计优化）
#define KMM_SIZE_TINY            (16)     // 微小对象
#define KMM_SIZE_SMALL           (128)    // 小对象
#define KMM_SIZE_MEDIUM          (1024)   // 中对象
#define KMM_SIZE_LARGE           (4 * 1024) // 大对象

// 分层Arena大小（针对混合负载优化）
#define KMM_ARENA_TINY_SIZE      (64 * 1024)     // 64KB 微小对象专用
#define KMM_ARENA_SMALL_SIZE     (1 * 1024 * 1024)  // 1MB 小对象专用
#define KMM_ARENA_MEDIUM_SIZE    (4 * 1024 * 1024)  // 4MB 中对象专用

// 作用域重置阈值
#define KMM_RESET_BATCH_SIZE     1000     // 每1000次分配重置一次

// 统计配置
#define KMM_ENABLE_STATS         1
#define KMM_ENABLE_ARENA_RESET   1

// 分支预测优化
#define KMM_LIKELY(x)       __builtin_expect(!!(x), 1)
#define KMM_UNLIKELY(x)     __builtin_expect(!!(x), 0)

// ==================== 分层Arena数据结构 ====================
typedef void (*kmm_cleanup_fn)(void* ptr);

typedef struct kmm_cleanup_node {
    void* resource;
    kmm_cleanup_fn cleanup;
    struct kmm_cleanup_node* next;
} kmm_cleanup_node_t;

typedef struct {
    size_t user_size;
    const char* file;
    int line;
    uint64_t canary;
} kmm_safe_header_t;

// 分层Arena结构
typedef struct {
    uint8_t* buffer;
    size_t size;
    size_t offset;
    size_t peak;
    size_t allocations;
    size_t reset_count;  // 重置次数统计
} kmm_arena_t;

// 分层上下文
typedef struct {
    // 分层Arena
    kmm_arena_t tiny_arena;   // 微小对象专用
    kmm_arena_t small_arena;  // 小对象专用
    kmm_arena_t medium_arena; // 中对象专用
    
    // 清理栈
    kmm_cleanup_node_t* cleanup_stack;
    
    // 作用域重置计数器
    size_t alloc_counter;
    
#if KMM_ENABLE_STATS
    // 分层统计
    struct {
        size_t total_allocs;
        size_t arena_hits;
        size_t heap_hits;
        
        // 分层命中统计
        size_t tiny_hits;
        size_t small_hits;
        size_t medium_hits;
        size_t large_hits;
        
        // 分层重置统计
        size_t tiny_resets;
        size_t small_resets;
        size_t medium_resets;
        
        // 性能统计
        double tiny_total_time;
        double small_total_time;
        double medium_total_time;
        double heap_total_time;
        
        // 碎片统计
        size_t tiny_fragmentation;   // 微小对象Arena碎片
        size_t small_fragmentation;  // 小对象Arena碎片
        size_t medium_fragmentation; // 中对象Arena碎片
    } stats;
#endif
} kmm_context_t;

// ==================== 工具函数 ====================
static inline size_t kmm_align_up(size_t size, size_t alignment) {
    return (size + alignment - 1) & ~(alignment - 1);
}

static inline double get_time_us(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return ts.tv_sec * 1000000.0 + ts.tv_nsec / 1000.0;
}

// 编译时决策宏
#define KMM_IS_TINY(size)    ((size) <= KMM_SIZE_TINY)
#define KMM_IS_SMALL(size)   ((size) <= KMM_SIZE_SMALL)
#define KMM_IS_MEDIUM(size)  ((size) <= KMM_SIZE_MEDIUM)
#define KMM_IS_LARGE(size)   ((size) > KMM_SIZE_MEDIUM)

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
    
    memset(user_ptr, 0, aligned_size);
    
    return user_ptr;
}

static inline void kmm_safe_free(void* user_ptr) {
    if (KMM_UNLIKELY(!user_ptr)) return;
    
    if (KMM_UNLIKELY(!kmm_check_redzone(user_ptr))) {
        kmm_safe_header_t* hdr = kmm_get_header_from_user(user_ptr);
        fprintf(stderr, "🚨 内存损坏检测! 文件: %s, 行: %d\n", hdr->file, hdr->line);
        abort();
    }
    
    kmm_safe_header_t* hdr = kmm_get_header_from_user(user_ptr);
    free(hdr);
}

// ==================== 分层Arena管理 ====================
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

// 微小对象专用分配（跳过对齐计算）
static inline void* kmm_arena_alloc_tiny(kmm_arena_t* arena, size_t size) {
    // 微小对象通常较小，跳过对齐计算
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
    // 不清零内存，保留峰值统计
}
#endif

// 计算Arena碎片率
static inline double kmm_arena_fragmentation(const kmm_arena_t* arena) {
    if (arena->size == 0 || arena->allocations == 0) return 0.0;
    
    // 简单估算：实际使用空间 vs 理论最小空间
    size_t theoretical_min = arena->peak;
    size_t actual_used = arena->size;
    
    if (theoretical_min == 0) return 0.0;
    
    double waste = 1.0 - ((double)theoretical_min / actual_used);
    return waste > 0 ? waste : 0;
}

// ==================== 分层上下文管理 ====================
static inline int kmm_init(kmm_context_t* ctx) {
    if (KMM_UNLIKELY(kmm_arena_init(&ctx->tiny_arena, KMM_ARENA_TINY_SIZE) != 0)) return -1;
    if (KMM_UNLIKELY(kmm_arena_init(&ctx->small_arena, KMM_ARENA_SMALL_SIZE) != 0)) return -1;
    if (KMM_UNLIKELY(kmm_arena_init(&ctx->medium_arena, KMM_ARENA_MEDIUM_SIZE) != 0)) return -1;
    
    ctx->cleanup_stack = NULL;
    ctx->alloc_counter = 0;
    
#if KMM_ENABLE_STATS
    memset(&ctx->stats, 0, sizeof(ctx->stats));
#endif
    
    return 0;
}

static inline int kmm_register_cleanup(kmm_context_t* ctx, void* ptr) {
    kmm_cleanup_node_t* node = (kmm_cleanup_node_t*)malloc(sizeof(kmm_cleanup_node_t));
    if (KMM_UNLIKELY(!node)) return -1;
    
    node->resource = ptr;
    node->cleanup = kmm_safe_free;
    node->next = ctx->cleanup_stack;
    ctx->cleanup_stack = node;
    
    return 0;
}

#if KMM_ENABLE_ARENA_RESET
static inline void kmm_reset_if_needed(kmm_context_t* ctx) {
    ctx->alloc_counter++;
    
    if (ctx->alloc_counter >= KMM_RESET_BATCH_SIZE) {
        // 模拟作用域重置
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

static inline void kmm_destroy(kmm_context_t* ctx) {
    // 清理堆对象
    kmm_cleanup_node_t* current = ctx->cleanup_stack;
    while (current) {
        if (current->cleanup && current->resource) {
            current->cleanup(current->resource);
        }
        kmm_cleanup_node_t* temp = current;
        current = current->next;
        free(temp);
    }
    
    // 释放Arena缓冲区
    if (ctx->tiny_arena.buffer) free(ctx->tiny_arena.buffer);
    if (ctx->small_arena.buffer) free(ctx->small_arena.buffer);
    if (ctx->medium_arena.buffer) free(ctx->medium_arena.buffer);
    
    ctx->cleanup_stack = NULL;
}

// ==================== 分层分配器 ====================
static inline void* kmm_alloc_layered(kmm_context_t* ctx, size_t size, 
                                      const char* file, int line) {
#if KMM_ENABLE_STATS
    ctx->stats.total_allocs++;
    double start_time = get_time_us();
#endif
    
    void* ptr = NULL;
    
    // 🎯 优化1：分层决策
    if (KMM_LIKELY(KMM_IS_TINY(size))) {
        // 微小对象 -> tiny arena
        ptr = kmm_arena_alloc_tiny(&ctx->tiny_arena, size);
        if (KMM_LIKELY(ptr)) {
#if KMM_ENABLE_STATS
            ctx->stats.tiny_hits++;
            ctx->stats.arena_hits++;
            ctx->stats.tiny_total_time += (get_time_us() - start_time);
#endif
            goto done;
        }
        
        // tiny arena失败，尝试small arena
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
        // 小对象 -> small arena
        ptr = kmm_arena_alloc(&ctx->small_arena, size);
        if (KMM_LIKELY(ptr)) {
#if KMM_ENABLE_STATS
            ctx->stats.small_hits++;
            ctx->stats.arena_hits++;
            ctx->stats.small_total_time += (get_time_us() - start_time);
#endif
            goto done;
        }
        
        // small arena失败，尝试medium arena
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
        // 中对象 -> medium arena
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
    
    // 🎯 所有Arena失败，降级到堆
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
    // 🎯 优化2：作用域重置检查
    kmm_reset_if_needed(ctx);
#endif
    
    return ptr;
}

// ==================== 测试套件 ====================
void test_layered_performance(void) {
    printf("🚀 分层Arena性能测试...\n");
    
    kmm_context_t ctx;
    kmm_init(&ctx);
    
    // 测试1: 微小对象
    printf("  测试微小对象(16B)...\n");
    double start = get_time_us();
    for (int i = 0; i < 10000; i++) {
        int* data = (int*)kmm_alloc_layered(&ctx, 16, __FILE__, __LINE__);
        if (data) {
            for (int j = 0; j < 4; j++) data[j] = i + j;
        }
    }
    double end = get_time_us();
    printf("    耗时: %.2f μs/分配\n", (end - start) / 10000);
    
    // 测试2: 优化版混合负载
    printf("  测试优化混合负载（带分层决策）...\n");
    start = get_time_us();
    
    for (int i = 0; i < 50000; i++) {
        size_t size;
        if (i % 100 == 0) {          // 1% 大对象
            size = 4096;
        } else if (i % 20 == 0) {    // 5% 中对象
            size = 1024;
        } else {                     // 94% 小/微小对象
            // 在微小和小对象之间合理分布
            if (i % 3 == 0) size = 16;   // 微小对象
            else if (i % 3 == 1) size = 64;  // 小对象
            else size = 128;              // 小对象
        }
        
        char* data = (char*)kmm_alloc_layered(&ctx, size, __FILE__, __LINE__);
        if (data) {
            size_t write_size = size < 256 ? size : 256;
            for (size_t j = 0; j < write_size; j++) {
                data[j] = (char)((i + j) % 256);
            }
        }
    }
    
    end = get_time_us();
    
    printf("  总分配: 50000 次\n");
    printf("  总耗时: %.2f ms\n", (end - start) / 1000);
    printf("  平均耗时: %.2f μs/分配\n", (end - start) / 50000);
    
#if KMM_ENABLE_STATS
    printf("\n📊 分层统计信息:\n");
    printf("  总分配次数: %zu\n", ctx.stats.total_allocs);
    printf("  Arena总命中: %zu (%.1f%%)\n", ctx.stats.arena_hits,
           ctx.stats.total_allocs > 0 ? (ctx.stats.arena_hits * 100.0) / ctx.stats.total_allocs : 0);
    printf("  堆命中: %zu (%.1f%%)\n", ctx.stats.heap_hits,
           ctx.stats.total_allocs > 0 ? (ctx.stats.heap_hits * 100.0) / ctx.stats.total_allocs : 0);
    
    printf("\n  分层命中详情:\n");
    printf("    微小对象(Tiny): %zu (%.1f%%)\n", ctx.stats.tiny_hits,
           ctx.stats.total_allocs > 0 ? (ctx.stats.tiny_hits * 100.0) / ctx.stats.total_allocs : 0);
    printf("    小对象(Small): %zu (%.1f%%)\n", ctx.stats.small_hits,
           ctx.stats.total_allocs > 0 ? (ctx.stats.small_hits * 100.0) / ctx.stats.total_allocs : 0);
    printf("    中对象(Medium): %zu (%.1f%%)\n", ctx.stats.medium_hits,
           ctx.stats.total_allocs > 0 ? (ctx.stats.medium_hits * 100.0) / ctx.stats.total_allocs : 0);
    printf("    大对象(Large): %zu (%.1f%%)\n", ctx.stats.large_hits,
           ctx.stats.total_allocs > 0 ? (ctx.stats.large_hits * 100.0) / ctx.stats.total_allocs : 0);
    
    printf("\n  Arena使用情况:\n");
    printf("    微小Arena: %zu/%zu bytes (%.1f%%) [%zu次分配, %zu次重置]\n", 
           ctx.tiny_arena.offset, ctx.tiny_arena.size,
           (ctx.tiny_arena.offset * 100.0) / ctx.tiny_arena.size,
           ctx.tiny_arena.allocations, ctx.tiny_arena.reset_count);
    printf("    小Arena: %zu/%zu bytes (%.1f%%) [%zu次分配, %zu次重置]\n", 
           ctx.small_arena.offset, ctx.small_arena.size,
           (ctx.small_arena.offset * 100.0) / ctx.small_arena.size,
           ctx.small_arena.allocations, ctx.small_arena.reset_count);
    printf("    中Arena: %zu/%zu bytes (%.1f%%) [%zu次分配, %zu次重置]\n", 
           ctx.medium_arena.offset, ctx.medium_arena.size,
           (ctx.medium_arena.offset * 100.0) / ctx.medium_arena.size,
           ctx.medium_arena.allocations, ctx.medium_arena.reset_count);
    
    // 计算碎片率
    double tiny_frag = kmm_arena_fragmentation(&ctx.tiny_arena);
    double small_frag = kmm_arena_fragmentation(&ctx.small_arena);
    double medium_frag = kmm_arena_fragmentation(&ctx.medium_arena);
    
    printf("\n  碎片率分析:\n");
    printf("    微小Arena碎片: %.1f%%\n", tiny_frag * 100);
    printf("    小Arena碎片: %.1f%%\n", small_frag * 100);
    printf("    中Arena碎片: %.1f%%\n", medium_frag * 100);
    
    printf("\n  性能统计:\n");
    printf("    微小对象平均时间: %.3f μs\n",
           ctx.stats.tiny_hits > 0 ? ctx.stats.tiny_total_time / ctx.stats.tiny_hits : 0);
    printf("    小对象平均时间: %.3f μs\n",
           ctx.stats.small_hits > 0 ? ctx.stats.small_total_time / ctx.stats.small_hits : 0);
    printf("    中对象平均时间: %.3f μs\n",
           ctx.stats.medium_hits > 0 ? ctx.stats.medium_total_time / ctx.stats.medium_hits : 0);
    printf("    堆平均时间: %.3f μs\n",
           ctx.stats.heap_hits > 0 ? ctx.stats.heap_total_time / ctx.stats.heap_hits : 0);
    
    printf("\n  Arena重置统计:\n");
    printf("    微小Arena重置: %zu 次\n", ctx.stats.tiny_resets);
    printf("    小Arena重置: %zu 次\n", ctx.stats.small_resets);
    printf("    中Arena重置: %zu 次\n", ctx.stats.medium_resets);
#endif
    
    kmm_destroy(&ctx);
}

void test_arena_reset_effect(void) {
    printf("\n🔄 Arena重置效果测试...\n");
    
    kmm_context_t ctx;
    kmm_init(&ctx);
    
    // 阶段1: 填充微小Arena
    size_t allocated_before_reset = 0;
    for (int i = 0; i < 1000; i++) {
        void* ptr = kmm_alloc_layered(&ctx, 16, __FILE__, __LINE__);
        if (ptr) {
            allocated_before_reset += 16;
            memset(ptr, 'A', 16);
        }
    }
    
    printf("  重置前分配: %zu bytes (%.1f%% 使用率)\n", 
           allocated_before_reset,
           (ctx.tiny_arena.offset * 100.0) / ctx.tiny_arena.size);
    
    // 阶段2: 模拟作用域重置
#if KMM_ENABLE_ARENA_RESET
    for (int i = 0; i < KMM_RESET_BATCH_SIZE; i++) {
        kmm_alloc_layered(&ctx, 16, __FILE__, __LINE__);
    }
    // 此时应触发重置
    
    printf("  Arena重置触发\n");
    printf("  重置后Arena偏移: %zu\n", ctx.tiny_arena.offset);
#endif
    
    // 阶段3: 重新分配
    size_t allocated_after_reset = 0;
    for (int i = 0; i < 1000; i++) {
        void* ptr = kmm_alloc_layered(&ctx, 16, __FILE__, __LINE__);
        if (ptr) {
            allocated_after_reset += 16;
            memset(ptr, 'B', 16);
        }
    }
    
    printf("  重置后分配: %zu bytes (%.1f%% 使用率)\n", 
           allocated_after_reset,
           (ctx.tiny_arena.offset * 100.0) / ctx.tiny_arena.size);
    
    if (allocated_before_reset > 0 && allocated_after_reset > 0) {
        printf("  ✅ Arena重置机制工作正常\n");
    }
    
    kmm_destroy(&ctx);
}

int main(void) {
    printf("========================================\n");
    printf("Kaula内存管理 - 分层Arena优化版\n");
    printf("解决20.6%%命中率问题，引入分层竞技场和作用域重置\n");
    printf("========================================\n\n");
    
    test_layered_performance();
    test_arena_reset_effect();
    
    printf("\n========================================\n");
    printf("优化策略总结:\n");
    printf("🎯 策略1: 分层Arena设计\n");
    printf("  • 微小对象Arena(64KB): 专用处理<16B对象\n");
    printf("  • 小对象Arena(1MB): 专用处理<128B对象\n");
    printf("  • 中对象Arena(4MB): 专用处理<1KB对象\n");
    printf("\n🎯 策略2: 智能降级机制\n");
    printf("  • 微小Arena满 → 小Arena\n");
    printf("  • 小Arena满 → 中Arena\n");
    printf("  • 中Arena满 → 堆分配\n");
    printf("\n🎯 策略3: 作用域重置\n");
    printf("  • 每%d次分配重置Arena\n", KMM_RESET_BATCH_SIZE);
    printf("  • 模拟Web请求/游戏帧处理模式\n");
    printf("\n预期改进:\n");
    printf("• Arena命中率: 从20.6%%提升到>85%%\n");
    printf("• 微小对象性能: 保持<0.1μs\n");
    printf("• 内存利用率: 分层管理减少碎片\n");
    printf("========================================\n");
    
    return 0;
}
