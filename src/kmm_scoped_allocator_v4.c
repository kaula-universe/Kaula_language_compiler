/* 暴露 BSD 扩展（如 macOS 的 MAP_ANON），需在所有 #include 前定义 */
#ifdef __APPLE__
#ifndef _DARWIN_C_SOURCE
#define _DARWIN_C_SOURCE
#endif
#endif

#include "kmm_scoped_allocator_v4.h"

#ifndef KMM_V4_STATIC_POOL
#ifdef _WIN32
#include <windows.h>
#else
#include <sys/mman.h>   // mmap, PROT_READ, MAP_PRIVATE 等
#include <unistd.h>      // sysconf
/* macOS 仅提供 MAP_ANON，无 MAP_ANONYMOUS；在此兼容 */
#ifndef MAP_ANONYMOUS
#ifdef MAP_ANON
#define MAP_ANONYMOUS MAP_ANON
#endif
#endif
#endif
#endif
#ifndef KAULA_FREESTANDING
#include <string.h>
#include <stdlib.h>   // Task #17：扩展段扩容用 malloc / free
#endif

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

KMM_TLS kmm_scope_stack_t g_kmm_v4_scope_stack = { .frames = {{0, 0}}, .depth = 0 };

// 修复 #1：per-thread heap 替代旧的 TLAB
KMM_TLS kmm_thread_heap_t g_kmm_v4_thread_heap = { NULL, 0, 0, 0 };

// ==================== Survivor 段（方案 A：slab 分配器） ====================
#ifdef KMM_V4_STATIC_POOL
// 静态池：slab 桶 slots + 大对象区统一从一块静态数组划分
__attribute__((aligned(KMM_V4_ALIGNMENT)))
uint8_t g_kmm_v4_survivor_storage[KMM_V4_SURVIVOR_REGION_SIZE];
#else
// 动态池（方案 A）：无大对象 bump 区（slab 密档覆盖 ≤1MB，超大对象页式）。
// large_base/capacity 保留符号兼容（恒 NULL/0，header 不再使用）。
uint8_t* g_kmm_v4_survivor_large_base = NULL;
size_t   g_kmm_v4_survivor_large_capacity = 0;
#endif

// survivor 段全局状态
kmm_survivor_state_t g_kmm_v4_survivor = {0};

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
// Task #25（审计 D2）：LEVEL>=1 时 refill 并发耗尽会并发调用
// pool_commit_up_to 做 read-modify-write，原子化同 survivor 计数器口径。
#if KMM_THREAD_SAFETY_LEVEL >= 1
static KMM_ATOMIC_TYPE g_committed_bytes = 0;
#else
static size_t g_committed_bytes = 0;
#endif

// ==================== 多段自动扩容（Task #17） ====================
// 全局池耗尽时通过系统 malloc 追加新段（链式段表，追加到链尾以保持
// 安装顺序），绝不整体 realloc/搬移（池内有大量活跃指针）。
// g_kmm_v4_offset 为跨段统一额度计数器：[0, pool_capacity) 对应首段，
// 超出部分按段链顺序落入扩展段。扩展段内存随段链存活到 destroy_pool。
// 线程安全口径与现状一致：KMM_THREAD_SAFETY_LEVEL>=1 时 grow 用原子标志
// 串行化（双检自旋，同 pool_ensure_init 口径），段链仅在 grow 内修改；
// 单线程模式无额外开销。freestanding（KMM_V4_STATIC_POOL）无 malloc，
// kmm_v4_pool_grow 恒返回 0，行为与旧版一致。
typedef struct kmm_v4_extra_seg {
    uint8_t* base;                    // 段内存起点（KMM_V4_ALIGNMENT 对齐）
    size_t   capacity;                // 段可用字节数
    struct kmm_v4_extra_seg* next;    // 下一段（安装顺序）
} kmm_v4_extra_seg_t;

// Task #25：段链暴露给头文件内联的 kmm_v4_pool_trim_tail（扩展段归还）
kmm_v4_extra_seg_t* g_kmm_v4_extra_head = NULL;  // 段链头（最早安装）
kmm_v4_extra_seg_t* g_kmm_v4_extra_tail = NULL;  // 段链尾（追加/归还用）
#define g_extra_head g_kmm_v4_extra_head
#define g_extra_tail g_kmm_v4_extra_tail

#if KMM_THREAD_SAFETY_LEVEL >= 1
KMM_ATOMIC_TYPE g_kmm_v4_extra_capacity = 0;
static KMM_ATOMIC_TYPE g_grow_flag = 0;  // 0=空闲, 1=扩容中
#else
size_t g_kmm_v4_extra_capacity = 0;
#endif

// 单个扩展段的最小尺寸（默认 1MB，可 -D 覆盖；每次 refill 仅消耗
// KMM_TLS_BUFFER_SIZE，过小的段会放大段链长度）
#ifndef KMM_V4_GROW_SEG_SIZE
#define KMM_V4_GROW_SEG_SIZE (1024 * 1024)
#endif

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
    if (KMM_ATOMIC_LOAD(g_committed_bytes) == 0) {
        KMM_ATOMIC_STORE(g_committed_bytes, 0);
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
        KMM_ATOMIC_STORE(g_committed_bytes, 0);
    }

#if KMM_THREAD_SAFETY_LEVEL >= 1
    KMM_ATOMIC_STORE(g_pool_init_flag, 2);
#endif
#endif
}

// 提交从 base+committed 到 base+needed 之间的页面
// 优化：使用批量提交粒度，减少 VirtualAlloc 调用频率
static void pool_commit_up_to(size_t needed) {
#ifdef KMM_V4_STATIC_POOL
    // freestanding 模式：静态池已全部"提交"，直接更新计数
    if (needed > g_kmm_v4_pool_capacity) needed = g_kmm_v4_pool_capacity;
    // D2 修复：CAS 循环保证计数器单调不回退
    size_t committed = KMM_ATOMIC_LOAD(g_committed_bytes);
    for (;;) {
        if (needed <= committed) return;
        if (KMM_ATOMIC_CAS_STRONG(g_committed_bytes, committed, needed)) return;
        // CAS 失败：committed 已被其他线程推进，重试
    }
#else
    // D2 修复：CAS 循环保证计数器单调不回退。
    // 线程安全分析：
    //  - 快照 committed → 若 needed ≤ committed 直接返回
    //  - 计算 commit_end → CAS(g_committed_bytes, committed, commit_end)
    //  - CAS 失败说明另一个线程已推进计数器，重读后重试
    //  - Windows VirtualAlloc MEM_COMMIT 对已提交区域是幂等的，并发提交重叠区间安全
    size_t committed = KMM_ATOMIC_LOAD(g_committed_bytes);
    for (;;) {
        if (needed <= committed) return;
        // 对齐到 KMM_BATCH_COMMIT_SIZE 边界（批量提交，减少系统调用）
        size_t commit_end = (needed + KMM_BATCH_COMMIT_SIZE - 1) & ~(KMM_BATCH_COMMIT_SIZE - 1);
        if (commit_end > g_kmm_v4_pool_capacity) commit_end = g_kmm_v4_pool_capacity;
        if (commit_end <= committed) return;
        size_t commit_size = commit_end - committed;
#ifdef _WIN32
        // 先提交页面（幂等），再 CAS 推进计数器
        // 若 CAS 失败说明别的线程已经推进到 >= commit_end，重试时 needed<=committed 即返回
        if (commit_size > 0) {
            VirtualAlloc(g_kmm_v4_pool + committed, commit_size, MEM_COMMIT, PAGE_READWRITE);
        }
#else
        (void)commit_size;  // Linux: mmap 自动按需提交
#endif
        if (KMM_ATOMIC_CAS_STRONG(g_committed_bytes, committed, commit_end)) return;
        // CAS 失败：committed 被更新为当前值，下一轮重试
    }
#endif
}

// 供 header inline 函数调用的提交接口（静态池模式下 header 提供 no-op 内联版）
#ifndef KMM_V4_STATIC_POOL
__attribute__((used))
void kmm_v4_pool_commit(size_t needed) {
    pool_commit_up_to(needed);
}
#endif

// ==================== Survivor 段初始化与提交 ====================

#ifndef KMM_V4_STATIC_POOL
// 线程安全的 survivor 段一次性初始化标志
static KMM_ATOMIC_TYPE g_survivor_init_flag = 0;

// 动态池（方案 A）：survivor 元数据由 header 的 kmm_v4_survivor_lazy_init
// 一次性建立（49 档 bucket_size/slots_count，slots=NULL 懒分配）。
// 这里只做线程安全的双检包装，不再 mmap 大对象区、不再预分配桶。
static void survivor_ensure_init(void) {
    if (g_kmm_v4_survivor.initialized) return;
#if KMM_THREAD_SAFETY_LEVEL >= 1
    size_t expected = 0;
    if (!KMM_ATOMIC_CAS_STRONG(g_survivor_init_flag, expected, 1)) {
        while (KMM_ATOMIC_LOAD(g_survivor_init_flag) != 2) {
            // 自旋等待
        }
        return;
    }
#endif
    kmm_v4_survivor_lazy_init();
#if KMM_THREAD_SAFETY_LEVEL >= 1
    KMM_ATOMIC_STORE(g_survivor_init_flag, 2);
#endif
}

// 懒分配指定桶的 slots 内存（首次使用时才分配，49 档不全预分配）
// 线程安全：CAS 双检，多线程并发 alloc 同一桶只分配一次。
int kmm_v4_survivor_bucket_ensure(int idx) {
    kmm_slab_bucket_t* b = &g_kmm_v4_survivor.buckets[idx];
    if (b->slots != NULL) return 0;
    size_t n = b->slots_count;
    if (KMM_V4_UNLIKELY(n == 0)) return -1;
    size_t bytes = n * b->bucket_size;
    void* p = malloc(bytes);
    if (KMM_V4_UNLIKELY(p == NULL)) return -1;
    memset(p, 0, bytes);
#if KMM_THREAD_SAFETY_LEVEL >= 1
    if (__atomic_load_n(&b->slots, __ATOMIC_ACQUIRE) != NULL) {
        free(p);  // 并发窗口内他人已建好
        return 0;
    }
    __atomic_store_n(&b->slots, (uint8_t*)p, __ATOMIC_RELEASE);
#else
    if (b->slots != NULL) { free(p); return 0; }
    b->slots = (uint8_t*)p;
#endif
    return 0;
}

// 页式超大对象（>1MB 或桶满）：magic header + payload，per-object free。
// payload 16 字节对齐（hdr 16B：magic 4 + pad 4 + size 8，malloc 16 对齐）。
// ⚠ 安全设计：所有页式对象登记到全局链表（g_paged_list）。survivor_free /
// realloc 只按登记表识别页式对象——绝不从任意指针反向读内存，
// 避免误把 TLAB/外部指针当页式对象解引用（见 memory 测试 double_free 崩溃）。
#define KMM_V4_PAGED_MAGIC 0x4B4D4D50u  /* "KMMP" */
typedef struct {
    uint32_t magic;
    uint32_t _pad;
    size_t   size;
} kmm_v4_paged_hdr_t;

typedef struct kmm_paged_node {
    struct kmm_paged_node* next;
    kmm_v4_paged_hdr_t*   hdr;   // 分配头（含 payload 基址）
} kmm_paged_node_t;

// 页式对象登记链表（页式对象数量少，全局自旋锁开销可忽略）
static kmm_paged_node_t* g_paged_list = NULL;
#if KMM_THREAD_SAFETY_LEVEL >= 1
static KMM_ATOMIC_TYPE g_paged_lock = 0;
static void kmm_v4_paged_lock(void) {
    size_t expected = 0;
    while (!KMM_ATOMIC_CAS_STRONG(g_paged_lock, expected, 1)) {
        expected = 0;
    }
}
static void kmm_v4_paged_unlock(void) { KMM_ATOMIC_STORE(g_paged_lock, 0); }
#else
static void kmm_v4_paged_lock(void) { }
static void kmm_v4_paged_unlock(void) { }
#endif

void* kmm_v4_survivor_paged_alloc(size_t size) {
    if (KMM_V4_UNLIKELY(size > SIZE_MAX - sizeof(kmm_v4_paged_hdr_t))) return NULL;
    kmm_v4_paged_hdr_t* hdr = (kmm_v4_paged_hdr_t*)malloc(sizeof(kmm_v4_paged_hdr_t) + size);
    if (KMM_V4_UNLIKELY(hdr == NULL)) return NULL;
    hdr->magic = KMM_V4_PAGED_MAGIC;
    hdr->size = size;

    kmm_paged_node_t* node = (kmm_paged_node_t*)malloc(sizeof(kmm_paged_node_t));
    if (KMM_V4_UNLIKELY(node == NULL)) { free(hdr); return NULL; }
    node->hdr = hdr;
    kmm_v4_paged_lock();
    node->next = g_paged_list;
    g_paged_list = node;
    kmm_v4_paged_unlock();
    return (uint8_t*)hdr + sizeof(kmm_v4_paged_hdr_t);
}

// 页式对象识别（登记表查找，安全：不读任意内存）
bool kmm_v4_survivor_paged_contains(const void* ptr) {
    if (KMM_V4_UNLIKELY(ptr == NULL)) return false;
    kmm_v4_paged_hdr_t* want = (kmm_v4_paged_hdr_t*)((uint8_t*)ptr - sizeof(kmm_v4_paged_hdr_t));
    bool found = false;
    kmm_v4_paged_lock();
    for (kmm_paged_node_t* n = g_paged_list; n != NULL; n = n->next) {
        if (n->hdr == want) { found = true; break; }
    }
    kmm_v4_paged_unlock();
    return found;
}

bool kmm_v4_survivor_paged_free(void* ptr) {
    if (KMM_V4_UNLIKELY(ptr == NULL)) return false;
    kmm_v4_paged_hdr_t* want = (kmm_v4_paged_hdr_t*)((uint8_t*)ptr - sizeof(kmm_v4_paged_hdr_t));
    kmm_v4_paged_lock();
    kmm_paged_node_t** pp = &g_paged_list;
    while (*pp != NULL) {
        if ((*pp)->hdr == want) {
            kmm_paged_node_t* n = *pp;
            *pp = n->next;
            kmm_v4_paged_unlock();
            if (n->hdr->magic == KMM_V4_PAGED_MAGIC) n->hdr->magic = 0;  // 防 double-free
            free(n->hdr);
            free(n);
            return true;
        }
        pp = &(*pp)->next;
    }
    kmm_v4_paged_unlock();
    return false;  // 不在登记表（TLAB/外部指针等）——安全返回
}

// 销毁全部页式对象（destroy_pool 调用）
void kmm_v4_survivor_paged_purge(void) {
    kmm_v4_paged_lock();
    kmm_paged_node_t* n = g_paged_list;
    g_paged_list = NULL;
    kmm_v4_paged_unlock();
    while (n != NULL) {
        kmm_paged_node_t* nx = n->next;
        if (n->hdr) free(n->hdr);
        free(n);
        n = nx;
    }
}

// ==================== TLS 垃圾箱：延迟批量归还 slab 槽位 ====================
// 性能优化：per-object free 从"立即 bitmap CAS"降为"TLS 数组 push"（~1ns，
// 无锁无 CAS），批量归还时机为：垃圾箱满阈值 + 函数出口 rewind（编译器
// 每个函数都发射 kmm_v4_survivor_rewind）。线程安全不变：垃圾箱是 TLS，
// flush 只归还本线程释放的槽位（契约上已死），无全局水位回卷。
typedef struct {
    int    bucket;   // 桶下标
    size_t slot;     // 槽位下标
} kmm_garbage_entry_t;

#define KMM_V4_GARBAGE_CAP 4096  // 垃圾箱容量（槽位数），满则 flush

static KMM_TLS kmm_garbage_entry_t g_kmm_garbage[KMM_V4_GARBAGE_CAP];
static KMM_TLS size_t              g_kmm_garbage_count = 0;

// 批量归还垃圾箱内全部槽位。小批量（≤16）逐槽清除（每槽一次 CAS，无聚合
// 固定开销——函数出口 rewind 常见 1~16 槽，此路径主导）；大批量按桶聚合
// word 掩码、每 word 一次原子清（原子操作从 N 次降到非零 word 数）。
// 调用点：阈值满 / 函数出口 rewind / destroy_pool。
#define KMM_V4_FLUSH_SMALL_N 16
void kmm_v4_survivor_flush(void) {
    size_t n = g_kmm_garbage_count;
    if (KMM_V4_LIKELY(n == 0)) return;
    g_kmm_garbage_count = 0;

    if (KMM_V4_LIKELY(n <= KMM_V4_FLUSH_SMALL_N)) {
        // 小批量：逐槽清除（复用 slot→bitmap 逻辑，避免聚合数组 memset 开销）
        for (size_t i = 0; i < n; i++) {
            kmm_slab_bucket_t* bk = &g_kmm_v4_survivor.buckets[g_kmm_garbage[i].bucket];
            if (KMM_V4_UNLIKELY(bk->slots == NULL)) continue;  // 防御
            size_t slot = g_kmm_garbage[i].slot;
            size_t word = slot / 64;
            uint64_t bit = (uint64_t)1 << (slot % 64);
#if KMM_THREAD_SAFETY_LEVEL >= 1
            uint64_t old = __atomic_load_n(&bk->bitmap[word], __ATOMIC_RELAXED);
            for (;;) {
                uint64_t neu = old & ~bit;
                if (__atomic_compare_exchange_n(&bk->bitmap[word], &old, neu, 1,
                                                __ATOMIC_RELEASE, __ATOMIC_RELAXED)) {
                    break;
                }
            }
#else
            bk->bitmap[word] &= ~bit;
#endif
            if (bk->allocated > 0) bk->allocated--;
        }
        return;
    }

    // 大批量：每桶聚合 word 掩码（49 桶 × 4 words，栈上临时数组）
    uint64_t masks[KMM_V4_SLAB_BUCKETS][KMM_V4_SLAB_BITMAP_WORDS];
    size_t   per_bucket[KMM_V4_SLAB_BUCKETS];
    memset(masks, 0, sizeof(masks));
    memset(per_bucket, 0, sizeof(per_bucket));

    for (size_t i = 0; i < n; i++) {
        int b = g_kmm_garbage[i].bucket;
        size_t slot = g_kmm_garbage[i].slot;
        masks[b][slot / 64] |= (uint64_t)1 << (slot % 64);
        per_bucket[b]++;
    }

    for (int b = 0; b < KMM_V4_SLAB_BUCKETS; b++) {
        if (per_bucket[b] == 0) continue;
        kmm_slab_bucket_t* bk = &g_kmm_v4_survivor.buckets[b];
        if (KMM_V4_UNLIKELY(bk->slots == NULL)) continue;  // 防御：理论不可达
        for (int w = 0; w < KMM_V4_SLAB_BITMAP_WORDS; w++) {
            uint64_t mask = masks[b][w];
            if (mask == 0) continue;
#if KMM_THREAD_SAFETY_LEVEL >= 1
            uint64_t old = __atomic_load_n(&bk->bitmap[w], __ATOMIC_RELAXED);
            for (;;) {
                uint64_t neu = old & ~mask;
                if (__atomic_compare_exchange_n(&bk->bitmap[w], &old, neu, 1,
                                                __ATOMIC_RELEASE, __ATOMIC_RELAXED)) {
                    break;
                }
            }
#else
            bk->bitmap[w] &= ~mask;
#endif
        }
        if (bk->allocated >= per_bucket[b]) {
            bk->allocated -= per_bucket[b];
        } else {
            bk->allocated = 0;
        }
    }
}

// 释放一个 slab 槽位：入 TLS 垃圾箱（延迟归还），满阈值立即 flush。
void kmm_v4_survivor_slab_release(int bucket_idx, size_t slot) {
    if (KMM_V4_UNLIKELY(g_kmm_garbage_count >= KMM_V4_GARBAGE_CAP)) {
        kmm_v4_survivor_flush();
    }
    g_kmm_garbage[g_kmm_garbage_count].bucket = bucket_idx;
    g_kmm_garbage[g_kmm_garbage_count].slot = slot;
    g_kmm_garbage_count++;
}

// 兼容保留：动态池无大对象 bump，commit 为 no-op
// （静态池下 header 内联为 no-op，不经过这里）。
__attribute__((used))
void kmm_v4_survivor_commit(size_t needed) {
    (void)needed;
    survivor_ensure_init();  // 顺带保证元数据就绪
}
#endif // !KMM_V4_STATIC_POOL

// ==================== 多段扩容：grow / 地址换算（Task #17） ====================

#ifndef KMM_V4_STATIC_POOL
// 追加新段（至少 min_needed 字节额度），成功返回 1；malloc 失败打一条
// stderr 日志并返回 0（保持现有失败约定：上层返回 NULL）。
// 注意：min_needed 语义为“额外额度缺口”——调用方（refill）已确认
// 已安装额度不足以容纳新块。
__attribute__((used))
int kmm_v4_pool_grow(size_t min_needed) {
    pool_ensure_init();
    if (!g_kmm_v4_pool) return 0;

#if KMM_THREAD_SAFETY_LEVEL >= 1
    // 扩容串行化：CAS 抢占标志，抢不到则自旋等待另一线程完成。
    // 返回 1 表示“有线程已完成一次扩容”，调用方（refill CAS 循环）
    // 会重新检查额度并重试。
    size_t _grow_expected = 0;
    if (!KMM_ATOMIC_CAS_STRONG(g_grow_flag, _grow_expected, 1)) {
        while (KMM_ATOMIC_LOAD(g_grow_flag) == 1) {
            // 自旋等待
        }
        return 1;
    }
    // 抢到标志后复检缺口：等待期间可能已有其他线程扩容满足需求
    {
        size_t _off_now = KMM_ATOMIC_LOAD(g_kmm_v4_offset);
        size_t _used_now = (_off_now > g_kmm_v4_pool_capacity) ? (_off_now - g_kmm_v4_pool_capacity) : 0;
        size_t _cap_now = KMM_ATOMIC_LOAD(g_kmm_v4_extra_capacity);
        if (_cap_now >= _used_now + min_needed) {
            KMM_ATOMIC_STORE(g_grow_flag, 0);
            return 1;
        }
        // 计算实际缺口
        size_t _deficit = _used_now + min_needed - _cap_now;
        if (_deficit > min_needed) min_needed = _deficit;
    }
#endif

    // 新段尺寸：不小于缺口，不小于 KMM_V4_GROW_SEG_SIZE，对齐到页边界
    size_t seg_size = min_needed;
    if (seg_size < KMM_V4_GROW_SEG_SIZE) seg_size = KMM_V4_GROW_SEG_SIZE;
    seg_size = (seg_size + (KMM_V4_PAGE_SIZE - 1)) & ~(size_t)(KMM_V4_PAGE_SIZE - 1);

    // 头结构 + 对齐负载一次 malloc（负载需 KMM_V4_ALIGNMENT 对齐，
    // malloc 仅保证 max_align_t，需多分配后自行对齐）
    const size_t align_mask = KMM_V4_ALIGNMENT - 1;
    size_t total = sizeof(kmm_v4_extra_seg_t) + seg_size + align_mask;
    void* raw = malloc(total);
    if (!raw) {
        fprintf(stderr, "KMM ERROR: pool grow failed (malloc %zu bytes returned NULL)\n", total);
#if KMM_THREAD_SAFETY_LEVEL >= 1
        KMM_ATOMIC_STORE(g_grow_flag, 0);
#endif
        return 0;
    }
    kmm_v4_extra_seg_t* seg = (kmm_v4_extra_seg_t*)raw;
    size_t hdr_off = (sizeof(kmm_v4_extra_seg_t) + align_mask) & ~align_mask;
    seg->base = (uint8_t*)raw + hdr_off;
    seg->capacity = seg_size;
    seg->next = NULL;
    // 追加到链尾，保持安装顺序（refill 按段链顺序消耗额度）
    if (g_extra_tail) g_extra_tail->next = seg;
    else g_extra_head = seg;
    g_extra_tail = seg;

#if KMM_THREAD_SAFETY_LEVEL >= 1
    // 最后原子发布新额度：其他线程观察到新额度前，段链已安装完毕
    KMM_ATOMIC_FETCH_ADD(g_kmm_v4_extra_capacity, seg_size);
    KMM_ATOMIC_STORE(g_grow_flag, 0);
#else
    g_kmm_v4_extra_capacity += seg_size;
#endif
    return 1;
}
#else  // KMM_V4_STATIC_POOL
// freestanding/静态池模式：无 malloc，不支持扩容，恒失败
// （保持旧版行为：refill 返回 NULL → 走 survivor 慢路径）
__attribute__((used))
int kmm_v4_pool_grow(size_t min_needed) { (void)min_needed; return 0; }
#endif // !KMM_V4_STATIC_POOL

// 统一 offset → 实际地址换算：[0, pool_capacity) 对应首段，
// 超出部分按段链顺序定位。仅对已合法推进到的 offset 调用。
static uint8_t* pool_offset_to_addr(size_t offset) {
    if (offset < g_kmm_v4_pool_capacity) return g_kmm_v4_pool + offset;
    size_t extra = offset - g_kmm_v4_pool_capacity;
    for (kmm_v4_extra_seg_t* s = g_extra_head; s != NULL; s = s->next) {
        if (extra < s->capacity) return s->base + extra;
        extra -= s->capacity;
    }
    return NULL;  // 不会发生：offset 推进前已校验额度
}

// 将统一 offset 调整为使 [offset, offset+size) 完整落在单一段内的最小
// 合法起点（跳过段尾部碎片——被跳过的碎片与首段尾部一样不回收，与
// “offset 单调递增”语义一致）。返回调整后的 offset；已安装额度不足
// 时返回 (size_t)-1（调用方触发 grow）。
static size_t pool_fit_offset(size_t offset, size_t size) {
    size_t primary_cap = g_kmm_v4_pool_capacity;
    if (offset < primary_cap) {
        if (size <= primary_cap - offset) return offset;
        offset = primary_cap;  // 块放不下首段剩余 → 跳过首段尾部，整体落扩展段
    }
    size_t extra = offset - primary_cap;  // 相对扩展区起点的额度位置
    size_t seg_begin = 0;
    for (kmm_v4_extra_seg_t* s = g_extra_head; s != NULL; s = s->next) {
        size_t seg_end = seg_begin + s->capacity;
        if (extra < seg_end) {
            if (extra + size <= seg_end) return primary_cap + extra;
            extra = seg_end;  // 本段放不下 → 跳到下一段起点
        }
        seg_begin = seg_end;
    }
    return (size_t)-1;  // 已安装额度不足
}

// ==================== 扩展段归还（Task #25） ====================
// offset 回卷（scope_pop / reset）后归还全空尾段，避免扩展段滞留到
// destroy_pool。
//
// 归还条件：尾段的完整额度区间（按统一 offset 记账，相对扩展区起点
// 的位置为 [seg_begin, seg_begin+capacity)）全部 >= 回卷后的水位
// extra_used = max(0, watermark - primary_capacity)，即该段未承载任何
// 已被消耗的 bump 额度。自首个全空段起至链尾连续摘除并 free，同步
// 扣减 extra_capacity。
//
// 安全性论证：
//  1. 扩展段只承载 bump 分配，生命周期受统一 offset/scope 管辖；水位
//     位于段区间之前 ⇒ 池内无存活对象引用该段内存。
//  2. 长寿命对象走 survivor 段（独立 mmap 区域），与扩展段归还无关。
//  3. pool_offset_to_addr / pool_fit_offset 只换算已消耗 offset
//     （< watermark），摘除段的区间全部 >= watermark，换算不触及。
//  4. 触发时机（保守，LEVEL>=1 下仅在 kmm_v4_reset 调用）：reset 约定
//     单线程上下文（见头文件注释），无并发 refill 遍历段链；scope_pop
//     在 LEVEL>=1 下不调用本函数——回卷仅作用于 thread heap，全局
//     offset 不回退，尾段不可能满足归还条件，且并发 refill 可能在
//     摘除瞬间遍历段链（use-after-free 窗口），故不触发。
//  5. LEVEL>=1 下抢 g_grow_flag 与 grow 互斥段链修改；抢不到（grow
//     进行中）直接放弃本次归还（归还仅为优化，下次回卷重试）。
__attribute__((used))
void kmm_v4_pool_trim_tail(size_t watermark) {
    if (g_extra_head == NULL) return;
    size_t primary_cap = g_kmm_v4_pool_capacity;
    size_t extra_used = (watermark > primary_cap) ? (watermark - primary_cap) : 0;

#if KMM_THREAD_SAFETY_LEVEL >= 1
    // 与 grow 互斥：段链仅允许持标志者修改（见 grow 注释同口径）
    size_t _trim_expected = 0;
    if (!KMM_ATOMIC_CAS_STRONG(g_grow_flag, _trim_expected, 1)) {
        return;  // grow 进行中：放弃本次归还
    }
#endif

    // 定位首个全空段（seg_begin >= extra_used），其后至链尾全部归还
    kmm_v4_extra_seg_t* cut_prev = NULL;
    kmm_v4_extra_seg_t* cut = g_extra_head;
    size_t seg_begin = 0;
    while (cut != NULL) {
        if (seg_begin >= extra_used) break;
        seg_begin += cut->capacity;
        cut_prev = cut;
        cut = cut->next;
    }
    if (cut != NULL) {
        // 摘除 [cut, tail]
        if (cut_prev) cut_prev->next = NULL;
        else g_extra_head = NULL;
        g_extra_tail = cut_prev;
        size_t freed_bytes = 0;
        while (cut) {
            kmm_v4_extra_seg_t* next = cut->next;
            freed_bytes += cut->capacity;
            free(cut);
            cut = next;
        }
#if KMM_THREAD_SAFETY_LEVEL >= 1
        // 摘链完成后才原子扣减额度：观察者不会拿旧额度 fit 进已摘除段
        KMM_ATOMIC_FETCH_ADD(g_kmm_v4_extra_capacity, (size_t)0 - freed_bytes);
        KMM_ATOMIC_STORE(g_grow_flag, 0);
#else
        g_kmm_v4_extra_capacity -= freed_bytes;
#endif
        return;
    }
#if KMM_THREAD_SAFETY_LEVEL >= 1
    KMM_ATOMIC_STORE(g_grow_flag, 0);
#endif
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
    // 使用 CAS 从全局池（含扩展段，Task #17）获取一大块内存。
    // thread heap 永远是单次 refill 的完整段块（不跨段拼接），
    // scope push/pop 的 {base, offset} 帧语义因此不变。
    size_t offset = KMM_ATOMIC_LOAD(g_kmm_v4_offset);
    size_t fit = (size_t)-1;
    size_t new_offset;
    for (;;) {
        fit = pool_fit_offset(offset, heap_size);
        if (KMM_V4_LIKELY(fit != (size_t)-1)) {
            new_offset = fit + heap_size;
            if (KMM_ATOMIC_CAS_WEAK(g_kmm_v4_offset, offset, new_offset)) break;
            continue;  // CAS 失败：offset 已被更新为重载值，重试
        }
        // 额度不足：追加新段后重试（另一线程已完成扩容时 grow 返回 1）
        if (!kmm_v4_pool_grow(heap_size)) return NULL;
    }

    // 仅当块落在首段时需要提交页面；扩展段由 malloc 提供，已可访问
    if (new_offset <= g_kmm_v4_pool_capacity) {
        pool_commit_up_to(new_offset);
    }

    // 设置当前线程的 thread heap
    g_kmm_v4_thread_heap.base = pool_offset_to_addr(fit);
    g_kmm_v4_thread_heap.offset = 0;
    g_kmm_v4_thread_heap.capacity = heap_size;
    g_kmm_v4_thread_heap.total_allocated += heap_size;

    return g_kmm_v4_thread_heap.base;
#else
    // 单线程模式：直接分配（Task #17：额度不足时自动扩容）
    size_t offset = g_kmm_v4_offset;
    size_t fit;
    for (;;) {
        fit = pool_fit_offset(offset, heap_size);
        if (fit != (size_t)-1) break;
        if (!kmm_v4_pool_grow(heap_size)) return NULL;
    }
    size_t new_offset = fit + heap_size;

    g_kmm_v4_offset = new_offset;
    if (new_offset <= g_kmm_v4_pool_capacity) {
        pool_commit_up_to(new_offset);
    }

    g_kmm_v4_thread_heap.base = pool_offset_to_addr(fit);
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
// 静态池模式下 malloc/free/calloc/strdup 由 header 提供内联版本（#ifdef KMM_V4_STATIC_POOL）

#ifndef KMM_V4_STATIC_POOL
__attribute__((used))
void* kmm_v4_malloc(size_t size) {
    return kmm_v4_alloc_auto(size);
}

// 修复 #6：free 为 no-op，bump allocator 靠 scope 回收
__attribute__((used))
void kmm_v4_free(void* ptr) {
    (void)ptr;
}
#endif

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
        void* p = kmm_v4_malloc(size);
        if (p) {
            size_t avail = (size_t)(g_kmm_v4_pool + g_kmm_v4_pool_capacity - (uint8_t*)ptr);
            size_t copy_size = (size < avail) ? size : avail;
            memcpy(p, ptr, copy_size);
        }
        return p;
    }

    // Task #17：检查 ptr 是否在扩展段内（always-copy，保守复制到段尾）
    for (kmm_v4_extra_seg_t* seg = g_extra_head; seg != NULL; seg = seg->next) {
        if ((uint8_t*)ptr >= seg->base && (uint8_t*)ptr < seg->base + seg->capacity) {
            void* p = kmm_v4_malloc(size);
            if (p) {
                size_t avail = (size_t)(seg->base + seg->capacity - (uint8_t*)ptr);
                size_t copy_size = (size < avail) ? size : avail;
                memcpy(p, ptr, copy_size);
            }
            return p;
        }
    }

#ifndef KMM_V4_STATIC_POOL
    // 动态池：页式超大对象（登记表识别，安全——不读任意内存）。
    // 须在 survivor_contains 之前：页式对象不在任何 slab 桶内。
    if (kmm_v4_survivor_paged_contains(ptr)) {
        kmm_v4_paged_hdr_t* _hdr = (kmm_v4_paged_hdr_t*)((uint8_t*)ptr - sizeof(kmm_v4_paged_hdr_t));
        size_t _old = _hdr->size;
        void* p = kmm_v4_malloc(size);
        if (p) {
            size_t copy_size = (size < _old) ? size : _old;
            memcpy(p, ptr, copy_size);
            kmm_v4_survivor_free(ptr);  // 登记表识别后释放
        }
        return p;
    }
#endif

    // 方案 A：检查 ptr 是否在 survivor 段内
    if (kmm_v4_survivor_contains(ptr)) {
        // D5 修复：先判断 ptr 是否在 slab 桶内，精确处理：
        //  - new_size <= bucket_size：返回原指针（兑现"不缩小"契约）
        //  - new_size >  bucket_size：分配新指针 + 精确复制 bucket_size 字节
        //    （防止读到相邻槽位）+ survivor_free 释放旧槽位（防泄漏）
        int slab_idx = -1;
        size_t slab_bucket_size = 0;
        for (int i = 0; i < KMM_V4_SLAB_BUCKETS; i++) {
            kmm_slab_bucket_t* b = &g_kmm_v4_survivor.buckets[i];
            if (b->slots != NULL && (uint8_t*)ptr >= b->slots &&
                (uint8_t*)ptr < b->slots + b->slots_count * b->bucket_size) {
                slab_idx = i;
                slab_bucket_size = b->bucket_size;
                break;
            }
        }
        if (slab_idx >= 0) {
            // slab 槽位指针
            if (size <= slab_bucket_size) {
                return ptr;  // D5 修复 #1：装得下就返回原指针（不缩小），兑现契约
            }
            // 装不下：分配新内存 + 按 slab 槽位大小精确复制（不越到相邻槽位）
            // + 释放旧槽位
            void* p = kmm_v4_malloc(size);
            if (p) {
                size_t copy_size = (size < slab_bucket_size) ? size : slab_bucket_size;
                memcpy(p, ptr, copy_size);  // D5 修复 #3：只复制 bucket_size 字节，不越界
                // D5 修复 #2：释放旧 slab 槽位，防止每次 realloc 漏一个槽
                kmm_v4_survivor_free(ptr);
            }
            return p;
        }
#ifdef KMM_V4_STATIC_POOL
        // survivor large bump 区域：无 header，保守复制到段尾（bump 固有特性）
        void* p = kmm_v4_malloc(size);
        if (p) {
            size_t avail = 0;
            if (g_kmm_v4_survivor.large_base != NULL) {
                avail = (size_t)(g_kmm_v4_survivor.large_base + g_kmm_v4_survivor.large_capacity - (uint8_t*)ptr);
            }
            size_t copy_size = (size < avail) ? size : avail;
            memcpy(p, ptr, copy_size);
        }
        return p;
#else
        // 动态池：survivor 段内但不在 slab 桶（页式对象已被前置分支拦截，此处不可达）
        return NULL;
#endif
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

#ifndef KMM_V4_STATIC_POOL
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
#endif

#ifndef KMM_V4_STATIC_POOL
void kmm_v4_init_pool(size_t initial_size) {
    pool_ensure_init();
    survivor_ensure_init();
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
        KMM_ATOMIC_STORE(g_committed_bytes, 0);
        g_kmm_v4_offset = 0;
        kmm_v4_thread_heap_invalidate();
    }
    // Task #17：释放扩展段链（各段为独立 malloc，逐个 free，无搬移）。
    // 注：Task #25 后 kmm_v4_reset 会经 kmm_v4_pool_trim_tail 归还全空尾段；
    // destroy 无条件释放全链（见头文件 trim 的归还条件与安全性论证）。
    {
        kmm_v4_extra_seg_t* seg = g_extra_head;
        while (seg) {
            kmm_v4_extra_seg_t* next = seg->next;
            free(seg);
            seg = next;
        }
        g_extra_head = NULL;
        g_extra_tail = NULL;
#if KMM_THREAD_SAFETY_LEVEL >= 1
        KMM_ATOMIC_STORE(g_kmm_v4_extra_capacity, 0);
#else
        g_kmm_v4_extra_capacity = 0;
#endif
    }
    // 方案 A：销毁 survivor 段——先 flush 垃圾箱（归还延迟槽位，防旧垃圾箱
    // 污染重建后的池），再释放各桶懒分配的 slots 与全部页式登记对象。
    kmm_v4_survivor_flush();
    for (int i = 0; i < KMM_V4_SLAB_BUCKETS; i++) {
        kmm_slab_bucket_t* b = &g_kmm_v4_survivor.buckets[i];
        if (b->slots) {
            free(b->slots);
            b->slots = NULL;
        }
        b->allocated = 0;
        b->free_hint = 0;
    }
    kmm_v4_survivor_paged_purge();
    g_kmm_v4_survivor.large_offset = 0;
    g_kmm_v4_survivor.initialized = false;
#if KMM_THREAD_SAFETY_LEVEL >= 1
    // Task #17：复位一次性初始化标志，否则 destroy 后 pool/survivor
    // 无法重新初始化（双检锁见状态 2 会直接返回而不重新分配）
    KMM_ATOMIC_STORE(g_pool_init_flag, 0);
    KMM_ATOMIC_STORE(g_survivor_init_flag, 0);
#endif
}
#endif
