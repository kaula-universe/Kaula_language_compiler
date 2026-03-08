#ifndef STD_MATH_MATH_H
#define STD_MATH_MATH_H

#include "../base/types.h"
#include <math.h>

// 数学常量
#define PI 3.14159265358979323846
#define E 2.71828182845904523536
#define LN2 0.69314718055994530942
#define LN10 2.30258509299404568402
#define LOG2E 1.44269504088896340736
#define LOG10E 0.43429448190325182765
#define SQRT2 1.41421356237309504880
#define SQRT1_2 0.70710678118654752440

// 基本算术函数
extern f64 math_abs(f64 x);
extern f64 math_floor(f64 x);
extern f64 math_ceil(f64 x);
extern f64 math_round(f64 x);
extern f64 math_trunc(f64 x);
extern f64 math_fmod(f64 x, f64 y);
extern f64 math_modf(f64 x, f64* int_part);

// 指数和对数函数
extern f64 math_exp(f64 x);
extern f64 math_exp2(f64 x);
extern f64 math_expm1(f64 x);
extern f64 math_log(f64 x);
extern f64 math_log2(f64 x);
extern f64 math_log10(f64 x);
extern f64 math_log1p(f64 x);

// 幂函数
extern f64 math_pow(f64 x, f64 y);
extern f64 math_sqrt(f64 x);
extern f64 math_cbrt(f64 x);
extern f64 math_hypot(f64 x, f64 y);

// 三角函数
extern f64 math_sin(f64 x);
extern f64 math_cos(f64 x);
extern f64 math_tan(f64 x);
extern f64 math_asin(f64 x);
extern f64 math_acos(f64 x);
extern f64 math_atan(f64 x);
extern f64 math_atan2(f64 y, f64 x);

// 双曲函数
extern f64 math_sinh(f64 x);
extern f64 math_cosh(f64 x);
extern f64 math_tanh(f64 x);
extern f64 math_asinh(f64 x);
extern f64 math_acosh(f64 x);
extern f64 math_atanh(f64 x);

// 特殊函数
extern f64 math_erf(f64 x);
extern f64 math_erfc(f64 x);
extern f64 math_gamma(f64 x);
extern f64 math_lgamma(f64 x);

// 比较函数
extern f64 math_max(f64 x, f64 y);
extern f64 math_min(f64 x, f64 y);
extern f64 math_fmax(f64 x, f64 y);
extern f64 math_fmin(f64 x, f64 y);
extern int math_signbit(f64 x);
extern bool math_isnan(f64 x);
extern bool math_isinf(f64 x);
extern bool math_isfinite(f64 x);

// 整数数学函数
extern i64 math_abs_i64(i64 x);
extern i32 math_abs_i32(i32 x);
extern i16 math_abs_i16(i16 x);
extern i8 math_abs_i8(i8 x);
extern i64 math_max_i64(i64 x, i64 y);
extern i64 math_min_i64(i64 x, i64 y);
extern i32 math_max_i32(i32 x, i32 y);
extern i32 math_min_i32(i32 x, i32 y);
extern u64 math_max_u64(u64 x, u64 y);
extern u64 math_min_u64(u64 x, u64 y);
extern u32 math_max_u32(u32 x, u32 y);
extern u32 math_min_u32(u32 x, u32 y);

// 随机数函数
extern void math_srand(unsigned int seed);
extern int math_rand();
extern f64 math_randf();
extern f64 math_rand_range(f64 min, f64 max);

// 角度转换
extern f64 math_deg_to_rad(f64 degrees);
extern f64 math_rad_to_deg(f64 radians);

#endif // STD_MATH_MATH_H