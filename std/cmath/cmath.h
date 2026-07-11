#ifndef STD_CMATH_CMATH_H
#define STD_CMATH_CMATH_H

#include "../base/types.h"

typedef struct Complex {
    f64 real;
    f64 imag;
} Complex;

Complex complex_create(f64 real, f64 imag);
Complex complex_conjugate(const Complex* c);
Complex complex_add(const Complex* a, const Complex* b);
Complex complex_subtract(const Complex* a, const Complex* b);
Complex complex_multiply(const Complex* a, const Complex* b);
Complex complex_divide(const Complex* a, const Complex* b);
Complex complex_negate(const Complex* c);

f64 complex_abs(const Complex* c);
f64 complex_arg(const Complex* c);
f64 complex_real(const Complex* c);
f64 complex_imag(const Complex* c);

Complex complex_exp(const Complex* c);
Complex complex_log(const Complex* c);
Complex complex_log10(const Complex* c);
Complex complex_pow(const Complex* base, const Complex* exp);

Complex complex_sqrt(const Complex* c);
Complex complex_sin(const Complex* c);
Complex complex_cos(const Complex* c);
Complex complex_tan(const Complex* c);
Complex complex_asin(const Complex* c);
Complex complex_acos(const Complex* c);
Complex complex_atan(const Complex* c);

Complex complex_sinh(const Complex* c);
Complex complex_cosh(const Complex* c);
Complex complex_tanh(const Complex* c);
Complex complex_asinh(const Complex* c);
Complex complex_acosh(const Complex* c);
Complex complex_atanh(const Complex* c);

bool_t complex_equal(const Complex* a, const Complex* b);
bool_t complex_is_zero(const Complex* c);
bool_t complex_is_real(const Complex* c);

#endif
