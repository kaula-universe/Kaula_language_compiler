// freestanding/math/math.c — 数学模块实现
// 全部自包含：浮点位操作 + 编译器内建位运算 + xorshift64* 随机数
// 无 libm、无 libc 依赖；符号 FS_WEAK

#ifndef KAULA_FREESTANDING_MATH_C
#define KAULA_FREESTANDING_MATH_C

#include "math.h"
#include "../base/fs_common.h"
#include <stdint.h>
#include <stdbool.h>

// 双精度 IEEE-754 位视图（无严格别名问题）
typedef union {
    f64 d;
    uint64_t u;
} fs_f64_bits;

#define FS_F64_SIGN     0x8000000000000000ULL
#define FS_F64_EXP_MASK 0x7FF0000000000000ULL
#define FS_F64_MANT_MASK 0x000FFFFFFFFFFFFFULL

// ==================== 浮点基础 ====================

FS_WEAK f64 math_abs(f64 x) {
    fs_f64_bits b;
    b.d = x;
    b.u &= ~FS_F64_SIGN;
    return b.d;
}

FS_WEAK f64 math_fabs(f64 x) {
    return math_abs(x);
}

FS_WEAK f64 math_min(f64 x, f64 y) {
    if (math_isnan(x)) return y;
    if (math_isnan(y)) return x;
    return x < y ? x : y;
}

FS_WEAK f64 math_max(f64 x, f64 y) {
    if (math_isnan(x)) return y;
    if (math_isnan(y)) return x;
    return x > y ? x : y;
}

FS_WEAK int math_signbit(f64 x) {
    fs_f64_bits b;
    b.d = x;
    return (b.u & FS_F64_SIGN) != 0;
}

FS_WEAK bool math_isnan(f64 x) {
    fs_f64_bits b;
    b.d = x;
    return (b.u & FS_F64_EXP_MASK) == FS_F64_EXP_MASK && (b.u & FS_F64_MANT_MASK) != 0;
}

FS_WEAK bool math_isinf(f64 x) {
    fs_f64_bits b;
    b.d = x;
    return (b.u & FS_F64_EXP_MASK) == FS_F64_EXP_MASK && (b.u & FS_F64_MANT_MASK) == 0;
}

FS_WEAK bool math_isfinite(f64 x) {
    fs_f64_bits b;
    b.d = x;
    return (b.u & FS_F64_EXP_MASK) != FS_F64_EXP_MASK;
}

FS_WEAK f64 math_trunc(f64 x) {
    if (!math_isfinite(x) || x == 0) return x;
    if (x > 0) return (f64)(int64_t)x;
    return (f64)(int64_t)x; // 向零截断
}

FS_WEAK f64 math_floor(f64 x) {
    if (!math_isfinite(x) || x == 0) return x;
    f64 t = (f64)(int64_t)x;
    if (t > x) t -= 1.0; // 负数时 int64 截断偏大，回退 1
    return t;
}

FS_WEAK f64 math_ceil(f64 x) {
    if (!math_isfinite(x) || x == 0) return x;
    f64 t = (f64)(int64_t)x;
    if (t < x) t += 1.0;
    return t;
}

// ==================== 整数数学 ====================

FS_WEAK i64 math_abs_i64(i64 x) {
    return x < 0 ? -x : x;
}

FS_WEAK i32 math_abs_i32(i32 x) {
    return x < 0 ? -x : x;
}

FS_WEAK i64 math_min_i64(i64 x, i64 y) {
    return x < y ? x : y;
}

FS_WEAK i64 math_max_i64(i64 x, i64 y) {
    return x > y ? x : y;
}

FS_WEAK i32 math_min_i32(i32 x, i32 y) {
    return x < y ? x : y;
}

FS_WEAK i32 math_max_i32(i32 x, i32 y) {
    return x > y ? x : y;
}

FS_WEAK u64 math_min_u64(u64 x, u64 y) {
    return x < y ? x : y;
}

FS_WEAK u64 math_max_u64(u64 x, u64 y) {
    return x > y ? x : y;
}

FS_WEAK u32 math_min_u32(u32 x, u32 y) {
    return x < y ? x : y;
}

FS_WEAK u32 math_max_u32(u32 x, u32 y) {
    return x > y ? x : y;
}

// ==================== 位运算辅助 ====================

FS_WEAK u32 math_clz(u64 x) {
#if defined(__GNUC__) || defined(__clang__)
    if (x == 0) return 64;
    return (u32)__builtin_clzll(x);
#else
    u32 n = 64;
    u64 y;
    y = x >> 32; if (y != 0) { n -= 32; x = y; }
    y = x >> 16; if (y != 0) { n -= 16; x = y; }
    y = x >> 8;  if (y != 0) { n -= 8;  x = y; }
    y = x >> 4;  if (y != 0) { n -= 4;  x = y; }
    y = x >> 2;  if (y != 0) { n -= 2;  x = y; }
    y = x >> 1;  if (y != 0) { n -= 1;  x = y; }
    if (x == 0) return 64;
    return n - 1;
#endif
}

FS_WEAK u32 math_ctz(u64 x) {
#if defined(__GNUC__) || defined(__clang__)
    if (x == 0) return 64;
    return (u32)__builtin_ctzll(x);
#else
    if (x == 0) return 64;
    u32 n = 0;
    while ((x & 1ULL) == 0) {
        x >>= 1;
        n++;
    }
    return n;
#endif
}

FS_WEAK u32 math_popcount(u64 x) {
#if defined(__GNUC__) || defined(__clang__)
    return (u32)__builtin_popcountll(x);
#else
    // 分治位计数
    x = x - ((x >> 1) & 0x5555555555555555ULL);
    x = (x & 0x3333333333333333ULL) + ((x >> 2) & 0x3333333333333333ULL);
    x = (x + (x >> 4)) & 0x0F0F0F0F0F0F0F0FULL;
    return (u32)((x * 0x0101010101010101ULL) >> 56);
#endif
}

FS_WEAK bool math_is_pow2(u64 x) {
    return x != 0 && (x & (x - 1)) == 0;
}

FS_WEAK u64 math_round_up(u64 x, u64 alignment) {
    if (alignment == 0) return x;
    return (x + alignment - 1) & ~(alignment - 1);
}

FS_WEAK u64 math_round_down(u64 x, u64 alignment) {
    if (alignment == 0) return x;
    return x & ~(alignment - 1);
}

FS_WEAK u64 math_div_round_up(u64 x, u64 y) {
    if (y == 0) return 0;
    return (x + y - 1) / y;
}

FS_WEAK u64 math_gcd(u64 a, u64 b) {
    while (b != 0) {
        u64 t = a % b;
        a = b;
        b = t;
    }
    return a;
}

// ==================== 随机数（xorshift64*） ====================

static u64 g_fs_rand_state = 0x9E3779B97F4A7C15ULL;

FS_WEAK void math_srand(unsigned int seed) {
    g_fs_rand_state = seed;
    // 避免全零状态
    if (g_fs_rand_state == 0) {
        g_fs_rand_state = 0x9E3779B97F4A7C15ULL;
    }
}

FS_WEAK int math_rand(void) {
    u64 x = g_fs_rand_state;
    x ^= x >> 12;
    x ^= x << 25;
    x ^= x >> 27;
    g_fs_rand_state = x;
    return (int)((x * 0x2545F4914F6CDD1DULL) >> 33) & 0x7FFFFFFF;
}

FS_WEAK f64 math_randf(void) {
    return (f64)math_rand() / 2147483648.0; // [0, 1)
}

FS_WEAK f64 math_rand_range(f64 min, f64 max) {
    return min + (max - min) * math_randf();
}

// ==================== 角度转换 ====================

FS_WEAK f64 math_deg_to_rad(f64 degrees) {
    return degrees * (FS_PI / 180.0);
}

FS_WEAK f64 math_rad_to_deg(f64 radians) {
    return radians * (180.0 / FS_PI);
}

#endif /* KAULA_FREESTANDING_MATH_C */
