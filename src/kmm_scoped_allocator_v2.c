#ifndef KMM_SCOPED_ALLOCATOR_V2_IMPL_H
#define KMM_SCOPED_ALLOCATOR_V2_IMPL_H

#include "kmm_scoped_allocator_v2.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#ifdef _WIN32
#include <windows.h>
#ifndef CLOCK_MONOTONIC
#define CLOCK_MONOTONIC 0
#endif

// 仅在未定义 timespec 时才定义
#ifndef _TIMESPEC_DEFINED
struct timespec {
    long tv_sec;
    long tv_nsec;
};
#endif

// 仅在未定义 clock_gettime 时才定义
#ifndef _POSIX_TIMERS
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
#endif

// ==================== 内部工具宏 ====================
#ifndef KMM_LIKELY
#define KMM_LIKELY(x) __builtin_expect(!!(x), 1)
#define KMM_UNLIKELY(x) __builtin_expect(!!(x), 0)
#endif

// ==================== 全局联合域实例 ====================
#if KMM_ENABLE_UNION_DOMAIN
kmm_union_domain_t g_union_domain = {0};
#endif

// ==================== 线程缓存实现 ====================
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

static inline void kmm_thread_cache_free(void* ptr, size_t size) {
    if (g_thread_cache.cache_size < KMM_THREAD_CACHE_SIZE) {
        g_thread_cache.cache[g_thread_cache.cache_size++] = ptr;
    }
}
#endif

// ==================== SIMD 清零优化 ====================
#ifdef __AVX2__
#include <immintrin.h>
static inline void fast_zero(void* ptr, size_t size) {
    __m256i zero = _mm256_setzero_si256();
    uint8_t* p = (uint8_t*)ptr;
    while (size >= 32) {
        _mm256_storeu_si256((__m256i*)p, zero);
        p += 32;
        size -= 32;
    }
    if (size > 0) memset(p, 0, size);
}
#elif defined(__SSE2__)
#include <emmintrin.h>
static inline void fast_zero(void* ptr, size_t size) {
    __m128i zero = _mm_setzero_si128();
    uint8_t* p = (uint8_t*)ptr;
    while (size >= 16) {
        _mm_storeu_si128((__m128i*)p, zero);
        p += 16;
        size -= 16;
    }
    if (size > 0) memset(p, 0, size);
}
#else
static inline void fast_zero(void* ptr, size_t size) {
    memset(ptr, 0, size);
}
#endif

// 计时器实现在外部提供（测试文件会覆盖）
#ifndef TEST_VERSION
static inline double get_time_us(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return ts.tv_sec * 1000000.0 + ts.tv_nsec / 1000.0;
}
#endif

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

// ==================== Arena 延迟初始化 ====================
static inline int kmm_arena_ensure_initialized(kmm_arena_t* arena, size_t min_size, size_t max_size) {
    if (KMM_LIKELY(arena->is_initialized)) {
        return 0;
    }
    
    size_t initial_capacity = kmm_align_up(min_size, 4096);
    if (initial_capacity > max_size) {
        initial_capacity = max_size;
    }
    
    arena->buffer = (uint8_t*)malloc(initial_capacity);
    if (!arena->buffer) return -1;
    
    arena->capacity = initial_capacity;
    arena->max_capacity = max_size;
    arena->offset = 0;
    arena->is_initialized = true;
    arena->allocations = 0;
    arena->peak = 0;
    arena->reset_count = 0;
    
    return 0;
}

// ==================== Arena 动态扩展 ====================
static inline int kmm_arena_expand(kmm_arena_t* arena, size_t additional_size) {
    size_t new_capacity = arena->capacity * KMM_ARENA_GROWTH_FACTOR;
    
    while (new_capacity < arena->offset + additional_size) {
        new_capacity *= KMM_ARENA_GROWTH_FACTOR;
    }
    
    if (new_capacity > arena->max_capacity) {
        new_capacity = arena->max_capacity;
    }
    
    if (arena->offset + additional_size > new_capacity) {
        return -1;
    }
    
    uint8_t* new_buffer = (uint8_t*)realloc(arena->buffer, new_capacity);
    if (!new_buffer) return -1;
    
    arena->buffer = new_buffer;
    arena->capacity = new_capacity;
    
    return 0;
}

// ==================== Arena 分配（超快速路径） ====================
static inline void* kmm_arena_alloc(kmm_arena_t* arena, size_t size, size_t min_capacity, size_t max_capacity) {
    size_t aligned_size = kmm_align_up(size, KMM_ALIGNMENT);
    size_t new_offset = arena->offset + aligned_size;
    
    // 超快速路径：无检查，直接分配
    if (KMM_LIKELY(new_offset <= arena->capacity)) {
        void* ptr = arena->buffer + arena->offset;
        arena->offset = new_offset;
        arena->allocations++;
        arena->peak = (new_offset > arena->peak) ? new_offset : arena->peak;
        return ptr;
    }
    
    // 慢速路径：扩展
    if (kmm_arena_expand(arena, aligned_size) != 0) {
        return NULL;
    }
    
    void* ptr = arena->buffer + arena->offset;
    arena->offset += aligned_size;
    arena->allocations++;
    arena->peak = (arena->offset > arena->peak) ? arena->offset : arena->peak;
    return ptr;
}

// ==================== Arena 分配（Tiny 优化） ====================
static inline void* kmm_arena_alloc_tiny(kmm_arena_t* arena, size_t size) {
    if (KMM_LIKELY(arena->is_initialized)) {
        size_t new_offset = arena->offset + size;
        
        if (KMM_LIKELY(new_offset <= arena->capacity)) {
            void* ptr = arena->buffer + arena->offset;
            arena->offset = new_offset;
            arena->allocations++;
            
            if (new_offset > arena->peak) {
                arena->peak = new_offset;
            }
            
            return ptr;
        }
    } else {
        if (kmm_arena_ensure_initialized(arena, KMM_ARENA_TINY_MIN, KMM_ARENA_TINY_MAX) != 0) {
            return NULL;
        }
    }
    
    if (kmm_arena_expand(arena, size) != 0) {
        return NULL;
    }
    
    void* ptr = arena->buffer + arena->offset;
    arena->offset += size;
    arena->allocations++;
    
    return ptr;
}

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

// ==================== 联合域实现 ====================
#if KMM_ENABLE_UNION_DOMAIN

static inline kmm_union_node_t* kmm_union_node_alloc(void) {
    return (kmm_union_node_t*)malloc(sizeof(kmm_union_node_t));
}

static inline bool kmm_union_has_dependency(kmm_union_node_t* node, kmm_union_node_t* target) {
    for (size_t i = 0; i < node->dependency_count; i++) {
        if (node->dependencies[i] == target) {
            return true;
        }
    }
    return false;
}

static inline kmm_union_node_t* kmm_find_node_by_pointer(void* ptr) {
    kmm_union_node_t* current = g_union_domain.root;
    while (current) {
        if (current->object == ptr) {
            return current;
        }
        current = current->next;
    }
    return NULL;
}

static inline void kmm_union_auto_detect_dependencies(kmm_union_node_t* node) {
    if (!node->dependencies) {
        return;
    }
    
    void** ptr = (void**)node->object;
    size_t word_count = node->object_size / sizeof(void*);
    
    for (size_t i = 0; i < word_count; i++) {
        void* potential_ptr = ptr[i];
        if (potential_ptr) {
            kmm_union_node_t* target = kmm_find_node_by_pointer(potential_ptr);
            if (target && target != node && !kmm_union_has_dependency(node, target)) {
                if (node->dependency_count < KMM_MAX_DEPENDENCIES) {
                    node->dependencies[node->dependency_count++] = target;
                }
            }
        }
    }
}

bool kmm_union_detect_cycle(kmm_union_node_t* node) {
    if (node->scope_depth == 0) {
        return false;
    }
    
    kmm_union_node_t* current = node->parent;
    size_t depth = 0;
    
    while (current) {
        depth++;
        
        if (depth > KMM_MAX_UNION_DEPTH) {
            return true;
        }
        
        if (kmm_union_has_dependency(node, current)) {
            return true;
        }
        
        current = current->parent;
    }
    
    return false;
}

void* kmm_union_elect(kmm_context_t* ctx, size_t size, const char* file, int line) {
#if KMM_ENABLE_STATS
    ctx->stats.union_elections++;
#endif
    
    void* obj = kmm_alloc(ctx, size, file, line);
    if (!obj) return NULL;
    
    kmm_union_node_t* node = kmm_union_node_alloc();
    node->object = obj;
    node->object_size = size;
    node->status = KMM_DOMAIN_UNION;
    node->scope_depth = g_union_domain.scope_depth;
    node->parent = g_union_domain.current;
    node->next = NULL;
    node->dependencies = NULL;
    node->dependency_count = 0;
    node->is_root = (g_union_domain.scope_depth == 0);
    node->is_elected = true;
    
    if (g_union_domain.root == NULL) {
        g_union_domain.root = node;
    }
    
    g_union_domain.current = node;
    g_union_domain.node_count++;
    
    ctx->union_rep = node;
    ctx->domain = &g_union_domain;
    
    kmm_union_auto_detect_dependencies(node);
    
    if (kmm_union_detect_cycle(node)) {
        node->status = KMM_DOMAIN_LOCAL;
        node->is_elected = false;
        return obj;
    }
    
    return obj;
}

void kmm_union_set_dependencies(void* obj, void** deps, size_t count) {
    if (!obj || !deps || count == 0) return;
    
    kmm_union_node_t* node = NULL;
    kmm_union_node_t* current = g_union_domain.root;
    
    while (current) {
        if (current->object == obj) {
            node = current;
            break;
        }
        current = current->next;
    }
    
    if (!node) return;
    
    if (count > KMM_MAX_DEPENDENCIES) {
        count = KMM_MAX_DEPENDENCIES;
    }
    
    node->dependencies = (kmm_union_node_t**)malloc(sizeof(kmm_union_node_t*) * count);
    node->dependency_count = count;
    
    for (size_t i = 0; i < count; i++) {
        current = g_union_domain.root;
        while (current) {
            if (current->object == deps[i]) {
                node->dependencies[i] = current;
                break;
            }
            current = current->next;
        }
    }
}

static inline void kmm_union_promote(kmm_union_node_t* node) {
    if (!node || !node->parent) {
        return;
    }
    
    node->scope_depth = node->parent->scope_depth;
    node->status = KMM_DOMAIN_ESCAPED;
    
    if (node->parent->scope_depth > 0) {
        kmm_union_promote(node->parent);
    }
}

static inline bool kmm_union_has_active_dependencies(kmm_union_node_t* node) {
    if (!node->dependencies || node->dependency_count == 0) {
        return false;
    }
    
    for (size_t i = 0; i < node->dependency_count; i++) {
        if (node->dependencies[i]->status != KMM_DOMAIN_LOCAL) {
            return true;
        }
    }
    return false;
}

static inline void kmm_union_topological_sort(kmm_union_node_t** nodes, size_t count) {
    if (count <= 1) return;
    
    for (size_t i = 0; i < count; i++) {
        nodes[i]->temp_in_degree = nodes[i]->dependency_count;
        nodes[i]->temp_visited = false;
    }
    
    kmm_union_node_t** queue = (kmm_union_node_t**)malloc(sizeof(kmm_union_node_t*) * count);
    if (!queue) return;
    
    size_t queue_front = 0;
    size_t queue_back = 0;
    
    for (size_t i = 0; i < count; i++) {
        if (nodes[i]->temp_in_degree == 0) {
            queue[queue_back++] = nodes[i];
        }
    }
    
    size_t sorted_count = 0;
    
    while (queue_front < queue_back) {
        kmm_union_node_t* current = queue[queue_front++];
        nodes[sorted_count++] = current;
        
        for (size_t i = 0; i < count; i++) {
            if (nodes[i]->temp_visited) continue;
            
            for (size_t j = 0; j < nodes[i]->dependency_count; j++) {
                if (nodes[i]->dependencies[j] == current) {
                    nodes[i]->temp_in_degree--;
                    if (nodes[i]->temp_in_degree == 0) {
                        nodes[i]->temp_visited = true;
                        queue[queue_back++] = nodes[i];
                    }
                    break;
                }
            }
        }
    }
    
    free(queue);
}

void kmm_union_destroy(kmm_union_domain_t* domain) {
    if (!domain->root) return;
    
    kmm_union_node_t** nodes = (kmm_union_node_t**)malloc(sizeof(kmm_union_node_t*) * domain->node_count);
    if (!nodes) return;
    
    size_t count = 0;
    
    kmm_union_node_t* current = domain->root;
    while (current) {
        nodes[count++] = current;
        current = current->next;
    }
    
    kmm_union_topological_sort(nodes, count);
    
    for (size_t i = count; i > 0; i--) {
        kmm_union_node_t* node = nodes[i - 1];
        
        if (node->dependencies) {
            free(node->dependencies);
            node->dependencies = NULL;
            node->dependency_count = 0;
        }
        
        free(node);
    }
    
    free(nodes);
    
    domain->root = NULL;
    domain->current = NULL;
    domain->node_count = 0;
    domain->scope_depth = 0;
}
#endif

// ==================== 公共 API 实现 ====================

int kmm_init(kmm_context_t* ctx) {
    memset(ctx, 0, sizeof(kmm_context_t));
    
    ctx->tiny_arena.is_initialized = false;
    ctx->tiny_arena.max_capacity = KMM_ARENA_TINY_MAX;
    
    ctx->small_arena.is_initialized = false;
    ctx->small_arena.max_capacity = KMM_ARENA_SMALL_MAX;
    
    ctx->medium_arena.is_initialized = false;
    ctx->medium_arena.max_capacity = KMM_ARENA_MEDIUM_MAX;
    
    ctx->cleanup_stack = NULL;
    ctx->alloc_counter = 0;
    
#if KMM_ENABLE_UNION_DOMAIN
    ctx->union_rep = NULL;
    ctx->domain = &g_union_domain;
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
    
    ctx->cleanup_stack = NULL;
    
    if (ctx->tiny_arena.buffer) free(ctx->tiny_arena.buffer);
    if (ctx->small_arena.buffer) free(ctx->small_arena.buffer);
    if (ctx->medium_arena.buffer) free(ctx->medium_arena.buffer);
    
    ctx->tiny_arena.buffer = NULL;
    ctx->small_arena.buffer = NULL;
    ctx->medium_arena.buffer = NULL;
}

void* kmm_alloc(kmm_context_t* ctx, size_t size, const char* file, int line) {
    void* ptr = NULL;
    
    // 超快速路径 1: 线程缓存（最快）
#if KMM_ENABLE_THREAD_CACHE
    ptr = kmm_thread_cache_alloc(size);
    if (KMM_LIKELY(ptr)) {
        return ptr;
    }
#endif
    
    // 超快速路径 2: Arena 分配
    if (KMM_LIKELY(KMM_IS_TINY(size))) {
        ptr = kmm_arena_alloc_tiny(&ctx->tiny_arena, size);
        if (KMM_LIKELY(ptr)) {
            return ptr;
        }
        
        ptr = kmm_arena_alloc(&ctx->small_arena, size, KMM_ARENA_SMALL_MIN, KMM_ARENA_SMALL_MAX);
        if (ptr) {
            return ptr;
        }
    } 
    else if (KMM_LIKELY(KMM_IS_SMALL(size))) {
        ptr = kmm_arena_alloc(&ctx->small_arena, size, KMM_ARENA_SMALL_MIN, KMM_ARENA_SMALL_MAX);
        if (KMM_LIKELY(ptr)) {
            return ptr;
        }
        
        ptr = kmm_arena_alloc(&ctx->medium_arena, size, KMM_ARENA_MEDIUM_MIN, KMM_ARENA_MEDIUM_MAX);
        if (ptr) {
#if KMM_ENABLE_STATS
            ctx->stats.medium_hits++;
            ctx->stats.arena_hits++;
            ctx->stats.medium_total_time += (get_time_us() - start_time);
#endif
            return ptr;
        }
    } 
    else if (KMM_IS_MEDIUM(size)) {
        ptr = kmm_arena_alloc(&ctx->medium_arena, size, KMM_ARENA_MEDIUM_MIN, KMM_ARENA_MEDIUM_MAX);
        if (ptr) {
#if KMM_ENABLE_STATS
            ctx->stats.medium_hits++;
            ctx->stats.arena_hits++;
            ctx->stats.medium_total_time += (get_time_us() - start_time);
#endif
            return ptr;
        }
    }
    
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
    
    return ptr;
}

void kmm_free(void* ptr) {
    if (KMM_UNLIKELY(!ptr)) return;
    
    if (kmm_check_redzone(ptr)) {
        kmm_safe_header_t* hdr = kmm_get_header_from_user(ptr);
        free(hdr);
    }
}

void** kmm_alloc_batch(kmm_context_t* ctx, size_t size, size_t count, const char* file, int line) {
#if KMM_ENABLE_STATS
    ctx->stats.total_allocs += count;
#endif
    
    void** ptrs = (void**)kmm_alloc(ctx, count * sizeof(void*), file, line);
    if (KMM_UNLIKELY(!ptrs)) return NULL;
    
    uint8_t* base = (uint8_t*)kmm_alloc(ctx, size * count, file, line);
    if (KMM_UNLIKELY(!base)) return NULL;
    
    for (size_t i = 0; i < count; i++) {
        ptrs[i] = base + i * size;
    }
    
    return ptrs;
}

#if KMM_ENABLE_STATS
void kmm_print_stats(kmm_context_t* ctx) {
    printf("\n📊 KMM V2 统计:\n");
    printf("  总分配次数：%zu\n", ctx->stats.total_allocs);
    printf("  Arena 总命中：%zu (%.1f%%)\n", ctx->stats.arena_hits,
           ctx->stats.total_allocs > 0 ? (ctx->stats.arena_hits * 100.0) / ctx->stats.total_allocs : 0);
    printf("  堆命中：%zu (%.1f%%)\n", ctx->stats.heap_hits,
           ctx->stats.total_allocs > 0 ? (ctx->stats.heap_hits * 100.0) / ctx->stats.total_allocs : 0);
    
#if KMM_ENABLE_UNION_DOMAIN
    printf("  联合域选举：%zu 次\n", ctx->stats.union_elections);
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
    printf("    微小 Arena: %zu/%zu bytes (%.1f%%) [%zu 次分配]\n", 
           ctx->tiny_arena.offset, ctx->tiny_arena.capacity,
           ctx->tiny_arena.is_initialized ? (ctx->tiny_arena.offset * 100.0) / ctx->tiny_arena.capacity : 0,
           ctx->tiny_arena.allocations);
    printf("    小 Arena: %zu/%zu bytes (%.1f%%) [%zu 次分配]\n", 
           ctx->small_arena.offset, ctx->small_arena.capacity,
           ctx->small_arena.is_initialized ? (ctx->small_arena.offset * 100.0) / ctx->small_arena.capacity : 0,
           ctx->small_arena.allocations);
    printf("    中 Arena: %zu/%zu bytes (%.1f%%) [%zu 次分配]\n", 
           ctx->medium_arena.offset, ctx->medium_arena.capacity,
           ctx->medium_arena.is_initialized ? (ctx->medium_arena.offset * 100.0) / ctx->medium_arena.capacity : 0,
           ctx->medium_arena.allocations);
    
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

#if KMM_ENABLE_UNION_DOMAIN
void kmm_print_union_stats(kmm_union_domain_t* domain) {
    printf("\n🔗 联合域统计:\n");
    printf("  总节点数：%zu\n", domain->node_count);
    printf("  最大深度：%zu\n", domain->max_depth);
    printf("  当前深度：%zu\n", domain->scope_depth);
}
#endif
#endif

#endif // KMM_SCOPED_ALLOCATOR_V2_IMPL_H
