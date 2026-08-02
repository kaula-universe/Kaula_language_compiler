#ifndef KAULA_FREESTANDING_MATH_H
#define KAULA_FREESTANDING_MATH_H

// freestanding/math/math.h — 数学模块
// 与 std.math 同级，但无 <math.h>/libm 依赖：浮点运算全部基于 IEEE-754 位操作，
// 位运算辅助函数使用编译器内建（裸机同样可用）
//
// 函数命名与 std.math 一致（math_abs/math_min/...），
// 仅实现裸机常用子集；符号 FS_WEAK，托管模式下 std.math/libm 强符号优先

#include <stdint.h>
#include <stdbool.h>
#include "../base/types.h"

// 数学常量
#define FS_PI 3.14159265358979323846
#define FS_E 2.71828182845904523536

/* ============================================================================
 * 浮点基础（无 libm，位操作实现）
 * ============================================================================ */

/**
 * math_abs - 浮点绝对值（清除符号位）
 */
extern f64 math_abs(f64 x);

/**
 * math_fabs - 同 math_abs
 */
extern f64 math_fabs(f64 x);

/**
 * math_min - 取较小值
 */
extern f64 math_min(f64 x, f64 y);

/**
 * math_max - 取较大值
 */
extern f64 math_max(f64 x, f64 y);

/**
 * math_signbit - 返回符号位（1 为负）
 */
extern int math_signbit(f64 x);

/**
 * math_isnan - 是否为 NaN
 */
extern bool math_isnan(f64 x);

/**
 * math_isinf - 是否为无穷
 */
extern bool math_isinf(f64 x);

/**
 * math_isfinite - 是否为有限数
 */
extern bool math_isfinite(f64 x);

/**
 * math_floor - 向下取整
 */
extern f64 math_floor(f64 x);

/**
 * math_ceil - 向上取整
 */
extern f64 math_ceil(f64 x);

/**
 * math_trunc - 截断取整
 */
extern f64 math_trunc(f64 x);

/* ============================================================================
 * 整数数学（与 std.math 同名）
 * ============================================================================ */

extern i64 math_abs_i64(i64 x);
extern i32 math_abs_i32(i32 x);
extern i64 math_min_i64(i64 x, i64 y);
extern i64 math_max_i64(i64 x, i64 y);
extern i32 math_min_i32(i32 x, i32 y);
extern i32 math_max_i32(i32 x, i32 y);
extern u64 math_min_u64(u64 x, u64 y);
extern u64 math_max_u64(u64 x, u64 y);
extern u32 math_min_u32(u32 x, u32 y);
extern u32 math_max_u32(u32 x, u32 y);

/* ============================================================================
 * 位运算辅助（裸机常用，编译器内建，无库依赖）
 * ============================================================================ */

/**
 * math_clz - 前导零个数（x==0 返回 64）
 */
extern u32 math_clz(u64 x);

/**
 * math_ctz - 尾随零个数（x==0 返回 64）
 */
extern u32 math_ctz(u64 x);

/**
 * math_popcount - 置位个数
 */
extern u32 math_popcount(u64 x);

/**
 * math_is_pow2 - 是否为 2 的幂（0 返回 false）
 */
extern bool math_is_pow2(u64 x);

/**
 * math_round_up - 向上对齐到 alignment（2 的幂）
 */
extern u64 math_round_up(u64 x, u64 alignment);

/**
 * math_round_down - 向下对齐到 alignment（2 的幂）
 */
extern u64 math_round_down(u64 x, u64 alignment);

/**
 * math_div_round_up - 向上取整除法
 */
extern u64 math_div_round_up(u64 x, u64 y);

/**
 * math_gcd - 最大公约数
 */
extern u64 math_gcd(u64 a, u64 b);

/* ============================================================================
 * 随机数（xorshift64*，无 libc 依赖，裸机可用）
 * ============================================================================ */

/**
 * math_srand - 设置随机数种子
 */
extern void math_srand(unsigned int seed);

/**
 * math_rand - 返回 [0, 2147483647] 的伪随机数
 */
extern int math_rand(void);

/**
 * math_randf - 返回 [0.0, 1.0) 的伪随机浮点数
 */
extern f64 math_randf(void);

/**
 * math_rand_range - 返回 [min, max] 的伪随机浮点数
 */
extern f64 math_rand_range(f64 min, f64 max);

/* ============================================================================
 * 角度转换
 * ============================================================================ */

extern f64 math_deg_to_rad(f64 degrees);
extern f64 math_rad_to_deg(f64 radians);

#endif /* KAULA_FREESTANDING_MATH_H */
