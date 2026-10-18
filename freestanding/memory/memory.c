// freestanding/memory/memory.c — 内存模块实现
//
// memset/memcpy/memmove/memcmp 与 libc 同名：
//   - 裸机模式（KAULA_FREESTANDING）: 定义本实现，供 LLVM builtin lower 引用
//   - 托管模式: 不定义（否则弱符号会从静态库中被提取，遮蔽 libc 强符号实现）
// fs_alloc 系列函数无 libc 冲突，始终定义。
//
// 字节拷贝循环使用 volatile 指针访问，防止 LLVM 的 loop idiom 识别
// 将其改写为 memcpy 自调用（会导致无限递归 + 栈溢出）。
//
// 本文件带包含保护，可安全地被 kaula_freestanding_runtime.c 与独立编译两种方式使用。

#ifndef KAULA_FREESTANDING_MEMORY_C
#define KAULA_FREESTANDING_MEMORY_C

#include "memory.h"
#include "../base/fs_common.h"
#include <stdint.h>
#include <stddef.h>

// ==================== 字长定义 ====================
// 64 位: uint64_t, 每次处理 8 字节
// 32 位: uint32_t, 每次处理 4 字节

#if defined(__LP64__) || defined(_WIN64) || defined(__x86_64__) || \
    defined(__aarch64__) || (defined(__riscv) && __riscv_xlen == 64)
    typedef uint64_t kr_word;
    #define KR_WORD_SIZE    8
    #define KR_WORD_ALIGN   8
    #define KR_WORD_M       0x0101010101010101ULL   // 低字节为 1
    #define KR_WORD_H       0x8080808080808080ULL   // 高位标记位
    #define KR_WORD_L       0x7F7F7F7F7F7F7F7FULL   // 去除高位
#else
    typedef uint32_t kr_word;
    #define KR_WORD_SIZE    4
    #define KR_WORD_ALIGN   4
    #define KR_WORD_M       0x01010101U
    #define KR_WORD_H       0x80808080U
    #define KR_WORD_L       0x7F7F7F7FU
#endif

// ==================== libc 同名函数（仅裸机模式定义） ====================
// 托管模式下不定义这些符号：弱符号会从静态库中被提取并遮蔽 libc 强符号实现
#ifdef KAULA_FREESTANDING

// ==================== haszero / hasbyte ====================
// 经典空字节检测算法（MIT HAKMEM / Numpy / musl 均使用）
// 原理：对于 word v，若某字节为 0，则 (v - 0x01..01) 的借位会传播，
//       使得该字节的高位变为 1，再 AND ~v & 0x80..80 即可判定。

static inline kr_word kr_haszero(kr_word v) {
    return (v - KR_WORD_M) & ~v & KR_WORD_H;
}

// ==================== 逐字复制（dst/src 已对齐） ====================
static inline void kr_wordcopy(kr_word* restrict dst, const kr_word* restrict src, size_t words) {
    size_t i = 0;
    // 8 路展开 — 减少分支，流水线友好
    size_t unrolled = words & ~(size_t)7;
    for (; i < unrolled; i += 8) {
        dst[i] = src[i]; dst[i+1] = src[i+1];
        dst[i+2] = src[i+2]; dst[i+3] = src[i+3];
        dst[i+4] = src[i+4]; dst[i+5] = src[i+5];
        dst[i+6] = src[i+6]; dst[i+7] = src[i+7];
    }
    for (; i < words; i++) {
        dst[i] = src[i];
    }
}

// 逐字设置
static inline void kr_wordset(kr_word* dst, kr_word pattern, size_t words) {
    size_t i = 0;
    size_t unrolled = words & ~(size_t)7;
    for (; i < unrolled; i += 8) {
        dst[i] = pattern; dst[i+1] = pattern;
        dst[i+2] = pattern; dst[i+3] = pattern;
        dst[i+4] = pattern; dst[i+5] = pattern;
        dst[i+6] = pattern; dst[i+7] = pattern;
    }
    for (; i < words; i++) {
        dst[i] = pattern;
    }
}

// ==================== memset ====================
FS_WEAK void* memset(void* dst, int c, size_t n) {
    unsigned char* d = (unsigned char*)dst;
    unsigned char* volatile dv = d;

    // 小对象快速路径（volatile 防止改写为 memset 自调用）
    if (FS_UNLIKELY(n < KR_WORD_SIZE * 2)) {
        for (size_t i = 0; i < n; i++) {
            dv[i] = (unsigned char)c;
        }
        return dst;
    }

    // 构造全填充 word：例如 c=0x42 → 0x4242424242424242
    kr_word pattern = 0;
    {
        unsigned char* p = (unsigned char*)&pattern;
        for (size_t i = 0; i < KR_WORD_SIZE; i++) {
            p[i] = (unsigned char)c;
        }
    }

    // 头部字节直到对齐
    size_t head = (-((uintptr_t)d)) & (KR_WORD_ALIGN - 1);
    if (head) {
        for (size_t i = 0; i < head; i++) {
            dv[i] = (unsigned char)c;
        }
        d += head;
        dv += head;
        n -= head;
    }

    // 逐字填充
    kr_wordset((kr_word*)d, pattern, n / KR_WORD_SIZE);

    // 尾部字节
    size_t tail = n & (KR_WORD_ALIGN - 1);
    if (tail) {
        unsigned char* volatile t = d + (n & ~(KR_WORD_ALIGN - 1));
        unsigned char* s = (unsigned char*)&pattern;
        for (size_t i = 0; i < tail; i++) {
            t[i] = s[i];
        }
    }

    return dst;
}

// ==================== memcpy ====================
FS_WEAK void* memcpy(void* restrict dst, const void* restrict src, size_t n) {
    unsigned char* d = (unsigned char*)dst;
    const unsigned char* s = (const unsigned char*)src;
    unsigned char* volatile dv = d;
    const unsigned char* volatile sv = s;

    // 小对象快速路径
    if (FS_UNLIKELY(n < KR_WORD_SIZE * 2)) {
        for (size_t i = 0; i < n; i++) {
            dv[i] = sv[i];
        }
        return dst;
    }

    // 头部字节直到 dst 对齐
    size_t head = (-((uintptr_t)d)) & (KR_WORD_ALIGN - 1);
    if (head) {
        for (size_t i = 0; i < head; i++) {
            dv[i] = sv[i];
        }
        d += head;
        s += head;
        dv += head;
        sv += head;
        n -= head;
    }

    // 逐字复制
    kr_wordcopy((kr_word*)d, (const kr_word*)s, n / KR_WORD_SIZE);

    // 尾部字节
    size_t tail = n & (KR_WORD_ALIGN - 1);
    if (tail) {
        unsigned char* volatile td = d + (n & ~(KR_WORD_ALIGN - 1));
        const unsigned char* volatile ts = s + (n & ~(KR_WORD_ALIGN - 1));
        for (size_t i = 0; i < tail; i++) {
            td[i] = ts[i];
        }
    }

    return dst;
}

// ==================== memmove ====================
FS_WEAK void* memmove(void* dst, const void* src, size_t n) {
    unsigned char* d = (unsigned char*)dst;
    const unsigned char* s = (const unsigned char*)src;
    unsigned char* volatile dv = d;
    const unsigned char* volatile sv = s;

    // 无重叠或正向安全：走 memcpy 路径
    if (FS_LIKELY(d <= s || d >= s + n)) {
        if (FS_UNLIKELY(n < KR_WORD_SIZE * 2)) {
            for (size_t i = 0; i < n; i++) {
                dv[i] = sv[i];
            }
            return dst;
        }

        size_t head = (-((uintptr_t)d)) & (KR_WORD_ALIGN - 1);
        if (head) {
            for (size_t i = 0; i < head; i++) {
                dv[i] = sv[i];
            }
            d += head;
            s += head;
            dv += head;
            sv += head;
            n -= head;
        }

        kr_wordcopy((kr_word*)d, (const kr_word*)s, n / KR_WORD_SIZE);

        size_t tail = n & (KR_WORD_ALIGN - 1);
        if (tail) {
            unsigned char* volatile td = d + (n & ~(KR_WORD_ALIGN - 1));
            const unsigned char* volatile ts = s + (n & ~(KR_WORD_ALIGN - 1));
            for (size_t i = 0; i < tail; i++) {
                td[i] = ts[i];
            }
        }
    } else {
        // 重叠且 dst > src：从尾部开始反向逐字节复制
        // （overlap 场景不常见，保持简单正确；volatile 防自调用）
        const unsigned char* volatile ts = s + n;
        unsigned char* volatile td = d + n;
        while (n--) {
            *--td = *--ts;
        }
    }

    return dst;
}

// ==================== memcmp ====================
FS_WEAK int memcmp(const void* a, const void* b, size_t n) {
    const unsigned char* p = (const unsigned char*)a;
    const unsigned char* q = (const unsigned char*)b;

    // 小对象：直接逐字节
    if (FS_UNLIKELY(n < KR_WORD_SIZE * 2)) {
        for (size_t i = 0; i < n; i++) {
            if (p[i] != q[i]) return (int)p[i] - (int)q[i];
        }
        return 0;
    }

    // 对齐前缀
    size_t head = (-((uintptr_t)p)) & (KR_WORD_ALIGN - 1);
    if (head && head <= n) {
        for (size_t i = 0; i < head; i++) {
            if (p[i] != q[i]) return (int)p[i] - (int)q[i];
        }
        p += head;
        q += head;
        n -= head;
    }

    // 逐字比较
    const kr_word* pw = (const kr_word*)p;
    const kr_word* qw = (const kr_word*)q;
    size_t words = n / KR_WORD_SIZE;
    for (size_t i = 0; i < words; i++) {
        if (pw[i] != qw[i]) {
            // 定位差异字节
            const unsigned char* wp = (const unsigned char*)&pw[i];
            const unsigned char* wq = (const unsigned char*)&qw[i];
            for (size_t j = 0; j < KR_WORD_SIZE; j++) {
                if (wp[j] != wq[j]) return (int)wp[j] - (int)wq[j];
            }
        }
    }

    // 尾部字节
    p = (const unsigned char*)(pw + words);
    q = (const unsigned char*)(qw + words);
    n &= (KR_WORD_ALIGN - 1);
    for (size_t i = 0; i < n; i++) {
        if (p[i] != q[i]) return (int)p[i] - (int)q[i];
    }

    return 0;
}

#endif /* KAULA_FREESTANDING */

// ==================== fs_alloc 静态池 bump 分配器 ====================
// 无 malloc/free、无系统调用；BSS 静态池 + 单调偏移，裸机/OS 均可用

static uint8_t g_fs_pool[FS_ALLOC_POOL_SIZE] __attribute__((aligned(8)));
static size_t g_fs_offset = 0;

FS_WEAK void* fs_alloc(size_t size) {
    // 对齐到 8 字节
    size = (size + 7) & ~(size_t)7;
    if (size == 0) size = 8;

    if (g_fs_offset + size > sizeof(g_fs_pool)) {
        return NULL; // 池满
    }
    void* ptr = &g_fs_pool[g_fs_offset];
    g_fs_offset += size;
    return ptr;
}

FS_WEAK void* fs_calloc(size_t count, size_t size) {
    size_t total = count * size;
    if (count != 0 && total / count != size) {
        return NULL; // 溢出
    }
    void* ptr = fs_alloc(total);
    if (ptr) {
        memset(ptr, 0, total);
    }
    return ptr;
}

FS_WEAK void fs_free(void* ptr) {
    (void)ptr; // bump 分配器：空操作，内存通过 fs_alloc_reset 批量回收
}

FS_WEAK void fs_alloc_reset(void) {
    g_fs_offset = 0;
}

FS_WEAK size_t fs_alloc_usage(void) {
    return g_fs_offset;
}

FS_WEAK size_t fs_alloc_available(void) {
    return sizeof(g_fs_pool) - g_fs_offset;
}

FS_WEAK size_t fs_alloc_capacity(void) {
    return sizeof(g_fs_pool);
}

#endif /* KAULA_FREESTANDING_MEMORY_C */
