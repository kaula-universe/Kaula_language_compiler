// freestanding/include/math.h — 裸机模式的 <math.h> 替身（仅原型）
//
// 语义边界：C99 数学函数在裸机没有实现（freestanding.math 只提供
// math_floor/ceil/trunc/fabs/min/max/整数工具这些自实现版本），这里只声明，
// 让引用能编译过；链接期缺符号即是"裸机需要自备 libm"的如实反馈。
#ifndef KAULA_FS_COMPAT_MATH_H
#define KAULA_FS_COMPAT_MATH_H

#ifdef __cplusplus
extern "C" {
#endif

typedef float float_t;
typedef double double_t;

#define HUGE_VAL  (__builtin_huge_val())
#define HUGE_VALF (__builtin_huge_valf())
#define HUGE_VALL (__builtin_huge_vall())
#define INFINITY  (__builtin_inff())
#define NAN       (__builtin_nanf(""))

#define FP_NAN       0
#define FP_INFINITE  1
#define FP_ZERO      2
#define FP_SUBNORMAL 3
#define FP_NORMAL    4

#define MATH_ERRNO     1
#define MATH_ERREXCEPT 2
#define math_errhandling MATH_ERRNO

#define M_E        2.7182818284590452354
#define M_LOG2E    1.4426950408889634074
#define M_LOG10E   0.43429448190325182765
#define M_LN2      0.69314718055994530942
#define M_LN10     2.30258509299404568402
#define M_PI       3.14159265358979323846
#define M_PI_2     1.57079632679489661923
#define M_PI_4     0.78539816339744830962
#define M_1_PI     0.31830988618379067154
#define M_2_PI     0.63661977236758134308
#define M_2_SQRTPI 1.12837916709551257390
#define M_SQRT2    1.41421356237309504880
#define M_SQRT1_2  0.70710678118654752440

// 分类
int __isnan(double x);
int __isinf(double x);
int __finite(double x);
int __signbit(double x);
int __fpclassify(double x);
#define fpclassify(x) __fpclassify((double)(x))
#define isfinite(x)  __finite((double)(x))
#define isinf(x)     __isinf((double)(x))
#define isnan(x)     __isnan((double)(x))
#define isnormal(x)  (fpclassify(x) == FP_NORMAL)
#define signbit(x)   __signbit((double)(x))

// 舍入
double ceil(double x);
double floor(double x);
double nearbyint(double x);
double rint(double x);
long lrint(double x);
long long llrint(double x);
double round(double x);
long lround(double x);
long long llround(double x);
double trunc(double x);
double fmod(double x, double y);
double remainder(double x, double y);
double remquo(double x, double y, int* quo);
double copysign(double x, double y);
double nan(const char* tagp);
double nextafter(double x, double y);
double nexttoward(double x, long double y);
double fdim(double x, double y);
double fmax(double x, double y);
double fmin(double x, double y);
double fma(double x, double y, double z);

// 指数与对数
double exp(double x);
double exp2(double x);
double expm1(double x);
double frexp(double x, int* exp);
double ldexp(double x, int exp);
double log(double x);
double log10(double x);
double log1p(double x);
double log2(double x);
double logb(double x);
double scalbn(double x, int n);
double scalbln(double x, long n);

// 幂与根
double pow(double x, double y);
double sqrt(double x);
double cbrt(double x);
double hypot(double x, double y);

// 三角与双曲
double sin(double x);
double cos(double x);
double tan(double x);
double asin(double x);
double acos(double x);
double atan(double x);
double atan2(double y, double x);
double sinh(double x);
double cosh(double x);
double tanh(double x);
double asinh(double x);
double acosh(double x);
double atanh(double x);

// 误差与 gamma
double erf(double x);
double erfc(double x);
double lgamma(double x);
double tgamma(double x);

// float / long double 变体
float ceilf(float x);
float floorf(float x);
float roundf(float x);
float truncf(float x);
float fmodf(float x, float y);
float copysignf(float x, float y);
float powf(float x, float y);
float sqrtf(float x);
float cbrtf(float x);
float hypotf(float x, float y);
float sinf(float x);
float cosf(float x);
float tanf(float x);
float asinf(float x);
float acosf(float x);
float atanf(float x);
float atan2f(float y, float x);
float expf(float x);
float logf(float x);
float log10f(float x);
float log2f(float x);
float exp2f(float x);

long double ceill(long double x);
long double floorl(long double x);
long double roundl(long double x);
long double truncl(long double x);
long double fmodl(long double x, long double y);
long double copysignl(long double x, long double y);
long double powl(long double x, long double y);
long double sqrtl(long double x);
long double cosl(long double x);
long double sinl(long double x);
long double expl(long double x);
long double logl(long double x);

#ifdef __cplusplus
}
#endif

#endif /* KAULA_FS_COMPAT_MATH_H */
