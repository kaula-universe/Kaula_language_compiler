/* cross_arch.c — 交叉编译自检入口: 引用 freestanding 库的全部公共符号
 *
 * 目的: 让链接器把库的全部符号拉进目标文件, 再用 llvm-nm 检查
 *       "未定义符号" 是否只包含本文件显式 extern 的 OS 无关符号。
 * 每个架构下都应做到: 除了本文件自己声明的 O_* 桩以外没有任何未定义符号。
 */
#include "freestanding.h"

/* ---- OS 无关的输出桩: 交叉检查只要求"这些符号由本文件提供" ---- */
void fs_output_putchar(char c) { (void)c; }

/* ---- 引用每个公共函数, 保证不会被优化掉 ---- */
void cross_arch_refs(void) {
    /* base */
    volatile i8 a8; volatile i16 a16; volatile i32 a32; volatile i64 a64;
    volatile u8 b8; volatile u16 b16; volatile u32 b32; volatile u64 b64;
    volatile f32 f32v; volatile f64 f64v; volatile bool bv; volatile char cv;
    a8 = to_i8(1); a16 = to_i16(1); a32 = to_i32(1); a64 = to_i64(1);
    b8 = to_u8(1); b16 = to_u16(1); b32 = to_u32(1); b64 = to_u64(1);
    f32v = to_f32(1.0); f64v = to_f64(1.0); bv = to_bool(1); cv = to_char(65);
    a8 = compare_i8(1, 2); a16 = compare_i16(1, 2); a32 = compare_i32(1, 2); a64 = compare_i64(1, 2);
    b8 = compare_u8(1, 2); b16 = compare_u16(1, 2); b32 = compare_u32(1, 2); b64 = compare_u64(1, 2);
    f32v = compare_f32(1, 2); cv = compare_char('a', 'b');
    a8 = compare_f64(1, 2); cv = compare_bool(true, false);
    bv = is_integer(1); bv = is_unsigned(1); bv = is_float(1.0); bv = is_bool(true); bv = is_char('a');
    (void)a8; (void)a16; (void)a32; (void)a64; (void)b8; (void)b16; (void)b32; (void)b64;
    (void)f32v; (void)f64v; (void)bv; (void)cv;

    /* memory */
    {
        volatile u8 buf[64];
        memset((void *)buf, 0, sizeof(buf));
        memcpy((void *)buf, (const void *)buf, sizeof(buf));
        memmove((void *)buf, (const void *)buf, sizeof(buf));
        a32 = memcmp((const void *)buf, (const void *)buf, sizeof(buf));
        void *p = fs_alloc(64);
        p = fs_calloc(4, 16);
        fs_free(p);
        fs_alloc_reset();
        b64 = fs_alloc_usage();
        b64 = fs_alloc_available();
        b64 = fs_alloc_capacity();
        (void)p; (void)a32; (void)b64;
    }

    /* string */
    {
        char buf[64];
        memset(buf, 0, sizeof(buf));
        b64 = strlen("abc");
        a32 = strcmp("a", "b"); a32 = strncmp("a", "b", 1);
        a32 = strcasecmp("a", "b"); a32 = strncasecmp("a", "b", 1);
        strcpy(buf, "a"); strncpy(buf, "a", 1); strcat(buf, "b"); strncat(buf, "b", 1);
        (void)strchr("abc", 'a'); (void)strrchr("abc", 'a'); (void)strstr("abc", "b");
        b64 = strspn("abc", "abc"); b64 = strcspn("abc", "x");
        (void)memchr("abc", 'a', 3);
        bv = is_alpha('a'); bv = is_digit('1'); bv = is_alnum('a'); bv = is_space(' ');
        bv = is_upper('A'); bv = is_lower('a'); bv = is_xdigit('f');
        cv = to_upper('a'); cv = to_lower('A');
        fs_itoa(-1, buf); fs_utoa(1, buf); fs_itoa_hex(255, buf, false);
        a64 = fs_atoi("1"); b64 = fs_atou("1");
        (void)fs_strdup("abc");
        (void)b64; (void)a32; (void)bv; (void)cv;
    }

    /* math */
    f64v = math_abs(-1.0); f64v = math_fabs(-1.0); f64v = math_min(1, 2); f64v = math_max(1, 2);
    a32 = math_signbit(-1.0); bv = math_isnan(f64v); bv = math_isinf(f64v); bv = math_isfinite(f64v);
    f64v = math_floor(1.5); f64v = math_ceil(1.5); f64v = math_trunc(1.5);
    a64 = math_abs_i64(-1); a32 = math_abs_i32(-1);
    a64 = math_min_i64(1, 2); a64 = math_max_i64(1, 2);
    a32 = math_min_i32(1, 2); a32 = math_max_i32(1, 2);
    b64 = math_min_u64(1, 2); b64 = math_max_u64(1, 2);
    b32 = math_min_u32(1, 2); b32 = math_max_u32(1, 2);
    b32 = math_clz(1); b32 = math_ctz(1); b32 = math_popcount(1);
    bv = math_is_pow2(2); b64 = math_round_up(1, 8); b64 = math_round_down(1, 8);
    b64 = math_div_round_up(1, 2); b64 = math_gcd(4, 2);
    math_srand(1); a32 = math_rand(); f64v = math_randf(); f64v = math_rand_range(0, 1);
    f64v = math_deg_to_rad(90.0); f64v = math_rad_to_deg(1.0);
    (void)f64v; (void)a64; (void)a32; (void)b64; (void)b32; (void)bv;
}

/* 入口: 避免空 TU 被优化掉, 也给链接器一个锚点 */
void _start_c(void) {
    cross_arch_refs();
}
