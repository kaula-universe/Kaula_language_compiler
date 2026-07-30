// Kaula Freestanding Runtime
// 裸机模式下的最小 C 运行时
// 提供 LLVM 在大小未知时降级调用的内存操作函数
// 这些函数在 hosted 模式下由 libc 提供，freestanding 模式下必须自行实现
//
// 优化策略：
//   1. 逐字（word-at-a-time）批量操作 — 64 位平台每次处理 8 字节
//   2. 循环展开（8-way unrolling）— 减少分支开销
//   3. 空字节检测（haszero trick）— strlen 快速终止
//   4. 对齐快速路径 — 对齐地址走字操作，不对齐走字节前缀

#include <stdint.h>
#include <stddef.h>

// ==================== 编译器辅助 ====================

#if defined(__GNUC__) || defined(__clang__)
    #define KR_LIKELY(x)   __builtin_expect(!!(x), 1)
    #define KR_UNLIKELY(x) __builtin_expect(!!(x), 0)
    #define KR_USED        __attribute__((used))
#else
    #define KR_LIKELY(x)   (x)
    #define KR_UNLIKELY(x) (x)
    #define KR_USED
#endif

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

// ==================== haszero / hasbyte ====================
// 经典空字节检测算法（MIT HAKMEM / Numpy / musl 均使用）
// 原理：对于 word v，若某字节为 0，则 (v - 0x01..01) 的借位会传播，
//       使得该字节的高位变为 1，再 AND ~v & 0x80..80 即可判定。
// 用途：strlen 逐字扫描时 O(1) 检测 8 字节中是否有 \0

static inline kr_word kr_haszero(kr_word v) {
    return (v - KR_WORD_M) & ~v & KR_WORD_H;
}

// ==================== 内部工具 ====================

// 逐字复制（dst/src 已对齐）
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
KR_USED
void* memset(void* dst, int c, size_t n) {
    unsigned char* d = (unsigned char*)dst;

    // 小对象快速路径
    if (KR_UNLIKELY(n < KR_WORD_SIZE * 2)) {
        for (size_t i = 0; i < n; i++) {
            d[i] = (unsigned char)c;
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
            d[i] = (unsigned char)c;
        }
        d += head;
        n -= head;
    }

    // 逐字填充
    kr_wordset((kr_word*)d, pattern, n / KR_WORD_SIZE);

    // 尾部字节
    size_t tail = n & (KR_WORD_ALIGN - 1);
    if (tail) {
        unsigned char* t = d + (n & ~(KR_WORD_ALIGN - 1));
        unsigned char* s = (unsigned char*)&pattern;
        for (size_t i = 0; i < tail; i++) {
            t[i] = s[i];
        }
    }

    return dst;
}

// ==================== memcpy ====================
KR_USED
void* memcpy(void* restrict dst, const void* restrict src, size_t n) {
    unsigned char* d = (unsigned char*)dst;
    const unsigned char* s = (const unsigned char*)src;

    // 小对象快速路径
    if (KR_UNLIKELY(n < KR_WORD_SIZE * 2)) {
        for (size_t i = 0; i < n; i++) {
            d[i] = s[i];
        }
        return dst;
    }

    // 头部字节直到 dst 对齐
    size_t head = (-((uintptr_t)d)) & (KR_WORD_ALIGN - 1);
    if (head) {
        for (size_t i = 0; i < head; i++) {
            d[i] = s[i];
        }
        d += head;
        s += head;
        n -= head;
    }

    // 逐字复制
    kr_wordcopy((kr_word*)d, (const kr_word*)s, n / KR_WORD_SIZE);

    // 尾部字节
    size_t tail = n & (KR_WORD_ALIGN - 1);
    if (tail) {
        unsigned char* td = d + (n & ~(KR_WORD_ALIGN - 1));
        const unsigned char* ts = s + (n & ~(KR_WORD_ALIGN - 1));
        for (size_t i = 0; i < tail; i++) {
            td[i] = ts[i];
        }
    }

    return dst;
}

// ==================== memmove ====================
KR_USED
void* memmove(void* dst, const void* src, size_t n) {
    unsigned char* d = (unsigned char*)dst;
    const unsigned char* s = (const unsigned char*)src;

    // 无重叠或正向安全：走 memcpy 路径
    if (KR_LIKELY(d <= s || d >= s + n)) {
        if (KR_UNLIKELY(n < KR_WORD_SIZE * 2)) {
            for (size_t i = 0; i < n; i++) {
                d[i] = s[i];
            }
            return dst;
        }

        size_t head = (-((uintptr_t)d)) & (KR_WORD_ALIGN - 1);
        if (head) {
            for (size_t i = 0; i < head; i++) {
                d[i] = s[i];
            }
            d += head;
            s += head;
            n -= head;
        }

        kr_wordcopy((kr_word*)d, (const kr_word*)s, n / KR_WORD_SIZE);

        size_t tail = n & (KR_WORD_ALIGN - 1);
        if (tail) {
            unsigned char* td = d + (n & ~(KR_WORD_ALIGN - 1));
            const unsigned char* ts = s + (n & ~(KR_WORD_ALIGN - 1));
            for (size_t i = 0; i < tail; i++) {
                td[i] = ts[i];
            }
        }
    } else {
        // 重叠且 dst > src：从尾部开始反向逐字节复制
        // （overlap 场景不常见，保持简单正确）
        const unsigned char* ts = s + n;
        unsigned char* td = d + n;
        while (n--) {
            *--td = *--ts;
        }
    }

    return dst;
}

// ==================== memcmp ====================
KR_USED
int memcmp(const void* a, const void* b, size_t n) {
    const unsigned char* p = (const unsigned char*)a;
    const unsigned char* q = (const unsigned char*)b;

    // 小对象：直接逐字节
    if (KR_UNLIKELY(n < KR_WORD_SIZE * 2)) {
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

// ==================== strlen ====================
KR_USED
size_t strlen(const char* s) {
    const unsigned char* p = (const unsigned char*)s;

    // 对齐到 word 边界（前缀逐字节扫描）
    size_t head = (-((uintptr_t)p)) & (KR_WORD_ALIGN - 1);
    for (size_t i = 0; i < head; i++) {
        if (p[i] == 0) return i;
    }
    p += head;

    // 逐字扫描，利用 haszero 在 8 字节中 O(1) 检测 \0
    const kr_word* wp = (const kr_word*)p;
    for (;;) {
        if (kr_haszero(*wp)) {
            // 命中：在该 word 内逐字节定位精确位置
            const unsigned char* bp = (const unsigned char*)wp;
            for (size_t i = 0; i < KR_WORD_SIZE; i++) {
                if (bp[i] == 0) {
                    // bp 已包含 head 偏移，直接算相对 s 的距离
                    return (size_t)(bp + i - (const unsigned char*)s);
                }
            }
        }
        wp++;
    }
}
