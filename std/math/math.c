#include "math.h"
#include <stdlib.h>
#include <time.h>

// 基本算术函数
f64 math_abs(f64 x) {
    return fabs(x);
}

f64 math_floor(f64 x) {
    return floor(x);
}

f64 math_ceil(f64 x) {
    return ceil(x);
}

f64 math_round(f64 x) {
    return round(x);
}

f64 math_trunc(f64 x) {
    return trunc(x);
}

f64 math_fmod(f64 x, f64 y) {
    return fmod(x, y);
}

f64 math_modf(f64 x, f64* int_part) {
    return modf(x, int_part);
}

// 指数和对数函数
f64 math_exp(f64 x) {
    return exp(x);
}

f64 math_exp2(f64 x) {
    return exp2(x);
}

f64 math_expm1(f64 x) {
    return expm1(x);
}

f64 math_log(f64 x) {
    return log(x);
}

f64 math_log2(f64 x) {
    return log2(x);
}

f64 math_log10(f64 x) {
    return log10(x);
}

f64 math_log1p(f64 x) {
    return log1p(x);
}

// 幂函数
f64 math_pow(f64 x, f64 y) {
    return pow(x, y);
}

f64 math_sqrt(f64 x) {
    return sqrt(x);
}

f64 math_cbrt(f64 x) {
    return cbrt(x);
}

f64 math_hypot(f64 x, f64 y) {
    return hypot(x, y);
}

// 三角函数
f64 math_sin(f64 x) {
    return sin(x);
}

f64 math_cos(f64 x) {
    return cos(x);
}

f64 math_tan(f64 x) {
    return tan(x);
}

f64 math_asin(f64 x) {
    return asin(x);
}

f64 math_acos(f64 x) {
    return acos(x);
}

f64 math_atan(f64 x) {
    return atan(x);
}

f64 math_atan2(f64 y, f64 x) {
    return atan2(y, x);
}

// 双曲函数
f64 math_sinh(f64 x) {
    return sinh(x);
}

f64 math_cosh(f64 x) {
    return cosh(x);
}

f64 math_tanh(f64 x) {
    return tanh(x);
}

f64 math_asinh(f64 x) {
    return asinh(x);
}

f64 math_acosh(f64 x) {
    return acosh(x);
}

f64 math_atanh(f64 x) {
    return atanh(x);
}

// 特殊函数
f64 math_erf(f64 x) {
    return erf(x);
}

f64 math_erfc(f64 x) {
    return erfc(x);
}

f64 math_gamma(f64 x) {
    return tgamma(x);
}

f64 math_lgamma(f64 x) {
    return lgamma(x);
}

// 比较函数
f64 math_max(f64 x, f64 y) {
    return fmax(x, y);
}

f64 math_min(f64 x, f64 y) {
    return fmin(x, y);
}

f64 math_fmax(f64 x, f64 y) {
    return fmax(x, y);
}

f64 math_fmin(f64 x, f64 y) {
    return fmin(x, y);
}

int math_signbit(f64 x) {
    return signbit(x);
}

bool math_isnan(f64 x) {
    return isnan(x);
}

bool math_isinf(f64 x) {
    return isinf(x);
}

bool math_isfinite(f64 x) {
    return isfinite(x);
}

// 整数数学函数
i64 math_abs_i64(i64 x) {
    return x < 0 ? -x : x;
}

i32 math_abs_i32(i32 x) {
    return x < 0 ? -x : x;
}

i16 math_abs_i16(i16 x) {
    return x < 0 ? -x : x;
}

i8 math_abs_i8(i8 x) {
    return x < 0 ? -x : x;
}

i64 math_max_i64(i64 x, i64 y) {
    return x > y ? x : y;
}

i64 math_min_i64(i64 x, i64 y) {
    return x < y ? x : y;
}

i32 math_max_i32(i32 x, i32 y) {
    return x > y ? x : y;
}

i32 math_min_i32(i32 x, i32 y) {
    return x < y ? x : y;
}

u64 math_max_u64(u64 x, u64 y) {
    return x > y ? x : y;
}

u64 math_min_u64(u64 x, u64 y) {
    return x < y ? x : y;
}

u32 math_max_u32(u32 x, u32 y) {
    return x > y ? x : y;
}

u32 math_min_u32(u32 x, u32 y) {
    return x < y ? x : y;
}

// 随机数函数
void math_srand(unsigned int seed) {
    srand(seed);
}

int math_rand() {
    return rand();
}

f64 math_randf() {
    return (f64)rand() / RAND_MAX;
}

f64 math_rand_range(f64 min, f64 max) {
    return min + (max - min) * math_randf();
}

// 角度转换
f64 math_deg_to_rad(f64 degrees) {
    return degrees * PI / 180.0;
}

f64 math_rad_to_deg(f64 radians) {
    return radians * 180.0 / PI;
}
