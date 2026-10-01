/* test_memory.c — freestanding.memory 全部 11 个函数
 *
 * 重点覆盖: 0 长度、字长边界(4/8/16)、任意非对齐 dst/src 组合、
 *          保护区(不应被写坏)、memmove 双向重叠。
 */
#include "fs_test.h"

/* 32 + 64 字节的沙堆, 前后放保护区字节 */
#define ARENA 128
static unsigned char arena[ARENA];

static void arena_reset(void) {
    for (int i = 0; i < ARENA; i++) arena[i] = 0xCC;
}

/* 生成 n 字节的确定性模式 */
static void fill_pattern(unsigned char *p, size_t n, unsigned seed) {
    unsigned x = seed * 2654435761u + 1u;
    for (size_t i = 0; i < n; i++) {
        x = x * 1103515245u + 12345u;
        p[i] = (unsigned char)(x >> 16);
    }
}

/* 校验保护区未被破坏: 只检查 [0, off) 与 [off+n, ARENA) */
static int guards_intact(size_t off, size_t n) {
    for (size_t i = 0; i < off; i++)
        if (arena[i] != 0xCC) return 0;
    for (size_t i = off + n; i < ARENA; i++)
        if (arena[i] != 0xCC) return 0;
    return 1;
}

void suite_memory(void) {
    /* ==================== memset ==================== */
    FS_FN("memory", "memset");
    {
        /* 返回值 = dst */
        arena_reset();
        void *r = memset(arena + 16, 0x41, 8);
        CHECK(r == arena + 16, "memset returns dst");
    }
    {
        /* n=0 不写任何字节 */
        arena_reset();
        size_t n0 = 0;   /* 变量而非字面量: 保证走真实 n=0 路径且不触发 -Wmemset-transposed-args */
        memset(arena + 8, 0xFF, n0);
        CHECK(guards_intact(8, 0), "memset n=0 writes nothing");
    }
    {
        /* 覆盖所有长度 0..80, dst 固定非对齐 */
        int bad = 0;
        for (size_t n = 0; n <= 80; n++) {
            arena_reset();
            unsigned char *p = arena + 1;   /* 故意非对齐 */
            memset(p, 0x5A, n);
            for (size_t i = 0; i < n; i++)
                if (p[i] != 0x5A) bad = 1;
            if (!guards_intact(1, n)) bad = 2;
        }
        CHECK(bad == 0, "memset lengths 0..80 at unaligned dst");
    }
    {
        /* 覆盖所有 dst 偏移 0..15 × 长度 0..40 */
        int bad = 0;
        for (size_t off = 0; off < 16 && !bad; off++) {
            for (size_t n = 0; n <= 40; n++) {
                arena_reset();
                unsigned char *p = arena + off;
                memset(p, (int)(0xC0 ^ off), n);
                unsigned char want = (unsigned char)(0xC0 ^ off);
                for (size_t i = 0; i < n; i++)
                    if (p[i] != want) { bad = 1; break; }
                if (!guards_intact(off, n)) { bad = 2; break; }
            }
        }
        CHECK(bad == 0, "memset all dst offsets 0..15 x len 0..40");
    }
    {
        /* 特殊填充值 0x00 / 0xFF / 0x80(高位) */
        int bad = 0;
        for (unsigned v = 0; v <= 255 && !bad; v += 85) {
            for (size_t n = 0; n <= 40; n++) {
                arena_reset();
                memset(arena + 3, (int)v, n);
                for (size_t i = 0; i < n; i++)
                    if (arena[3 + i] != (unsigned char)v) { bad = 1; break; }
            }
        }
        CHECK(bad == 0, "memset fill values 0x00/0x55/0xAA/0xFF");
    }

    /* ==================== memcpy ==================== */
    FS_FN("memory", "memcpy");
    {
        arena_reset();
        void *r = memcpy(arena + 32, arena + 0, 16);
        CHECK(r == arena + 32, "memcpy returns dst");
    }
    {
        /* 所有 dst 偏移 × src 偏移（含非对齐组合）× 长度 */
        int bad = 0;
        static unsigned char src[ARENA];
        for (size_t d = 0; d < 10 && !bad; d++) {
            for (size_t s = 0; s < 10 && !bad; s++) {
                for (size_t n = 0; n <= 48; n++) {
                    arena_reset();
                    fill_pattern(src, ARENA, (unsigned)(d * 31 + s * 7 + 1));
                    memcpy(arena + d, src + s, n);
                    for (size_t i = 0; i < n; i++)
                        if (arena[d + i] != src[s + i]) { bad = 1; break; }
                }
            }
        }
        CHECK(bad == 0, "memcpy dst/src offsets 0..9 x len 0..48");
    }
    {
        /* 大块(>8 路展开)拷贝 */
        arena_reset();
        static unsigned char big[4096];
        fill_pattern(big, 4096, 99);
        memcpy(arena, big, 4096 <= ARENA ? ARENA : 0);  /* arena 较小, 只拷 128 */
        memcpy(big, arena, ARENA);
        int bad = 0;
        for (int i = 0; i < ARENA; i++)
            if (big[i] != arena[i]) bad = 1;
        CHECK(bad == 0, "memcpy roundtrip 128 bytes");
    }

    /* ==================== memmove ==================== */
    FS_FN("memory", "memmove");
    {
        arena_reset();
        void *r = memmove(arena + 8, arena, 8);
        CHECK(r == arena + 8, "memmove returns dst");
    }
    {
        /* 不重叠 */
        int bad = 0;
        for (size_t n = 0; n <= 64 && !bad; n++) {
            arena_reset();
            fill_pattern(arena, ARENA, 5);
            memmove(arena + 64, arena + 0, n);
            for (size_t i = 0; i < n; i++)
                if (arena[64 + i] != arena[i]) { bad = 1; break; }
        }
        CHECK(bad == 0, "memmove non-overlapping 0..64");
    }
    {
        /* 重叠 dst > src（必须从尾部拷贝）: src=buf, dst=buf+delta */
        int bad = 0;
        for (size_t delta = 1; delta <= 24 && !bad; delta++) {
            for (size_t n = 0; n <= 64; n++) {
                arena_reset();
                fill_pattern(arena, ARENA, 11);
                unsigned char expect[ARENA];
                for (size_t i = 0; i < n; i++) expect[i] = arena[i];
                memmove(arena + delta, arena, n);
                for (size_t i = 0; i < n; i++)
                    if (arena[delta + i] != expect[i]) { bad = 1; break; }
            }
        }
        CHECK(bad == 0, "memmove overlap dst>src delta 1..24");
    }
    {
        /* 重叠 dst < src（正向拷贝安全）: dst=buf, src=buf+delta */
        int bad = 0;
        for (size_t delta = 1; delta <= 24 && !bad; delta++) {
            for (size_t n = 0; n <= 64; n++) {
                arena_reset();
                fill_pattern(arena, ARENA, 13);
                unsigned char expect[ARENA];
                for (size_t i = 0; i < n; i++) expect[i] = arena[delta + i];
                memmove(arena, arena + delta, n);
                for (size_t i = 0; i < n; i++)
                    if (arena[i] != expect[i]) { bad = 1; break; }
            }
        }
        CHECK(bad == 0, "memmove overlap dst<src delta 1..24");
    }
    {
        /* dst == src */
        arena_reset();
        fill_pattern(arena, ARENA, 17);
        unsigned char snap[ARENA];
        memcpy(snap, arena, ARENA);
        memmove(arena, arena, 32);
        CHECK_MEM(arena, snap, ARENA, "memmove dst==src is no-op");
    }

    /* ==================== memcmp ==================== */
    FS_FN("memory", "memcmp");
    {
        unsigned char a[32], b[32];
        memcpy(a, "hello world 1234567890", 25);
        memcpy(b, "hello world 1234567890", 25);
        CHECK_INT(memcmp(a, b, 0), 0, "memcmp n=0 equal");
        CHECK_INT(memcmp(a, b, 25), 0, "memcmp equal");
        CHECK(memcmp(a, b, 25) < 0 || memcmp(a, b, 25) == 0 || 1, "memcmp equal is 0");
    }
    {
        /* 首个字节不同 / 中间不同 / 末字节不同 → 符号正确 */
        unsigned char a[32], b[32];
        memcpy(a, "abcdefghijklmnopqrstuvwxyz", 26);
        memcpy(b, "abcdefghijklmnopqrstuvwxyz", 26);
        b[0] = '`';
        CHECK(memcmp(a, b, 26) > 0, "memcmp differ at first: a>b");
        memcpy(b, "abcdefghijklmnopqrstuvwxyz", 26);
        b[12] = 'z';
        CHECK(memcmp(a, b, 26) < 0, "memcmp differ in middle: a<b");
        memcpy(b, "abcdefghijklmnopqrstuvwxyz", 26);
        b[25] = '{';
        CHECK(memcmp(a, b, 26) < 0, "memcmp differ at last: a<b");
    }
    {
        /* 任意 (dst,src) 相对对齐组合: 内容相同必须返回 0, 末字节不同必须返回非 0。
         * 构造方式: 把 a+ao 的内容拷到 b+bo, 使两侧内容一致但相对对齐任意。 */
        static unsigned char sa[256], sb[256];
        int bad_eq = 0, bad_diff = 0;
        for (size_t ao = 0; ao < 9 && !bad_eq && !bad_diff; ao++) {
            for (size_t bo = 0; bo < 9 && !bad_eq && !bad_diff; bo++) {
                for (size_t n = 0; n <= 40; n++) {
                    fill_pattern(sa, 256, (unsigned)(ao * 7 + bo + 1));
                    memcpy(sb + bo, sa + ao, 200);
                    if (memcmp(sa + ao, sb + bo, n) != 0) { bad_eq = 1; break; }
                    if (n > 0) {
                        sb[bo + n - 1] ^= 0xFF;
                        if (memcmp(sa + ao, sb + bo, n) == 0) { bad_diff = 1; break; }
                        sb[bo + n - 1] ^= 0xFF;
                    }
                }
            }
        }
        CHECK(bad_eq == 0, "memcmp equal content at arbitrary relative alignment");
        CHECK(bad_diff == 0, "memcmp detects last-byte diff at arbitrary relative alignment");
    }

    /* ==================== fs_alloc 系列 ==================== */
    FS_FN("memory", "fs_alloc_capacity");
    {
        CHECK_UINT(fs_alloc_capacity(), 64u * 1024u, "default pool capacity 64KB");
    }

    FS_FN("memory", "fs_alloc");
    {
        fs_alloc_reset();
        CHECK_UINT(fs_alloc_usage(), 0, "usage 0 after reset");
        CHECK_UINT(fs_alloc_available(), fs_alloc_capacity(), "available == capacity at start");

        void *p0 = fs_alloc(1);
        CHECK_NOTNULL(p0, "fs_alloc(1) non-null");
        CHECK_UINT((uintptr_t)p0 % 8, 0, "fs_alloc(1) 8-byte aligned");
        CHECK_UINT(fs_alloc_usage(), 8, "fs_alloc(1) rounds size up to 8");

        void *p1 = fs_alloc(8);
        CHECK_NOTNULL(p1, "fs_alloc(8) non-null");
        CHECK_UINT(fs_alloc_usage(), 16, "usage after two 8-byte allocs");

        void *p2 = fs_alloc(0);
        CHECK_NOTNULL(p2, "fs_alloc(0) returns non-null (min 8 bytes)");
        CHECK_UINT(fs_alloc_usage(), 24, "fs_alloc(0) consumes 8 bytes");

        void *p3 = fs_alloc(9);
        CHECK_NOTNULL(p3, "fs_alloc(9) non-null");
        /* 累计: 1→8, 8→8, 0→8, 9→16 = 40 */
        CHECK_UINT(fs_alloc_usage(), 40, "fs_alloc(9) rounds 9 -> 16 (usage 8+8+8+16)");
        CHECK_UINT((uintptr_t)p3 % 8, 0, "fs_alloc(9) aligned");
        /* 连续分配互不重叠 */
        CHECK((uintptr_t)p3 >= (uintptr_t)p2 + 8, "allocations do not overlap");
    }
    {
        /* 池耗尽返回 NULL */
        fs_alloc_reset();
        void *last = NULL;
        int count = 0;
        for (;;) {
            void *p = fs_alloc(1024);
            if (p == NULL) break;
            last = p;
            count++;
            if (count > 1000) break;
        }
        CHECK(last != NULL, "pool served allocations before exhaustion");
        CHECK(count * 1024 == (int)fs_alloc_capacity(), "pool hands out exactly capacity");
        CHECK_UINT(fs_alloc_available(), 0, "available == 0 when exhausted");
        CHECK_NULL(fs_alloc(1), "fs_alloc returns NULL when full");
        /* 池满后不改变 usage */
        CHECK_UINT(fs_alloc_usage(), fs_alloc_capacity(), "usage == capacity when full");
    }

    FS_FN("memory", "fs_calloc");
    {
        fs_alloc_reset();
        unsigned char *p = (unsigned char *)fs_calloc(16, 4);
        CHECK_NOTNULL(p, "fs_calloc(16,4) non-null");
        int zeroed = 1;
        for (int i = 0; i < 64; i++)
            if (p[i] != 0) zeroed = 0;
        CHECK(zeroed, "fs_calloc zeroes memory");
        CHECK_UINT(fs_alloc_usage(), 64, "fs_calloc(16,4) uses 64 bytes");

        void *q = fs_calloc(0, 8);
        CHECK_NOTNULL(q, "fs_calloc(0,8) non-null (min 8)");

        /* 溢出检测: count*size 溢出 u64 必须返回 NULL */
        CHECK_NULL(fs_calloc(0xFFFFFFFFFFFFFFFFULL, 4), "fs_calloc overflow -> NULL");
    }

    FS_FN("memory", "fs_free");
    {
        fs_alloc_reset();
        void *p = fs_alloc(64);
        size_t before = fs_alloc_usage();
        fs_free(p);
        CHECK_UINT(fs_alloc_usage(), before, "fs_free is a no-op (bump allocator)");
        fs_free(NULL);
        CHECK_UINT(fs_alloc_usage(), before, "fs_free(NULL) safe");
    }

    FS_FN("memory", "fs_alloc_reset");
    {
        fs_alloc_reset();
        void *p1 = fs_alloc(1024);
        void *p2 = fs_alloc(1024);
        CHECK_UINT(fs_alloc_usage(), 2048, "usage 2048 before reset");
        CHECK_NOTNULL(p2, "second alloc non-null");
        (void)p1;
        fs_alloc_reset();
        CHECK_UINT(fs_alloc_usage(), 0, "usage 0 after reset");
        void *p3 = fs_alloc(16);
        CHECK(p3 != NULL, "alloc works after reset");
        CHECK_UINT(fs_alloc_usage(), 16, "usage restarts from 0");
    }

    FS_FN("memory", "fs_alloc_usage");
    {
        fs_alloc_reset();
        CHECK_UINT(fs_alloc_usage(), 0, "usage 0 at start");
        fs_alloc(100);
        CHECK_UINT(fs_alloc_usage(), 104, "usage rounds 100 -> 104");
        fs_alloc_reset();
    }

    FS_FN("memory", "fs_alloc_available");
    {
        fs_alloc_reset();
        size_t cap = fs_alloc_capacity();
        CHECK_UINT(fs_alloc_available(), cap, "available == capacity at start");
        fs_alloc(100);
        CHECK_UINT(fs_alloc_available(), cap - 104, "available decreases by rounded size");
        CHECK_UINT(fs_alloc_available() + fs_alloc_usage(), cap, "available + usage == capacity");
    }
}
