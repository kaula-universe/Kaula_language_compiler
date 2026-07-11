#include "cmath.h"
#include "../math/math.h"
#include <math.h>

Complex complex_create(f64 real, f64 imag) {
    Complex c;
    c.real = real;
    c.imag = imag;
    return c;
}

Complex complex_conjugate(const Complex* c) {
    return complex_create(c->real, -c->imag);
}

Complex complex_add(const Complex* a, const Complex* b) {
    return complex_create(a->real + b->real, a->imag + b->imag);
}

Complex complex_subtract(const Complex* a, const Complex* b) {
    return complex_create(a->real - b->real, a->imag - b->imag);
}

Complex complex_multiply(const Complex* a, const Complex* b) {
    return complex_create(
        a->real * b->real - a->imag * b->imag,
        a->real * b->imag + a->imag * b->real
    );
}

Complex complex_divide(const Complex* a, const Complex* b) {
    f64 denom = b->real * b->real + b->imag * b->imag;
    if (denom == 0.0) return complex_create(0.0, 0.0);
    return complex_create(
        (a->real * b->real + a->imag * b->imag) / denom,
        (a->imag * b->real - a->real * b->imag) / denom
    );
}

Complex complex_negate(const Complex* c) {
    return complex_create(-c->real, -c->imag);
}

f64 complex_abs(const Complex* c) {
    return sqrt(c->real * c->real + c->imag * c->imag);
}

f64 complex_arg(const Complex* c) {
    return atan2(c->imag, c->real);
}

f64 complex_real(const Complex* c) {
    return c->real;
}

f64 complex_imag(const Complex* c) {
    return c->imag;
}

Complex complex_exp(const Complex* c) {
    f64 exp_real = exp(c->real);
    return complex_create(exp_real * cos(c->imag), exp_real * sin(c->imag));
}

Complex complex_log(const Complex* c) {
    return complex_create(log(complex_abs(c)), complex_arg(c));
}

Complex complex_log10(const Complex* c) {
    Complex log_val = complex_log(c);
    Complex log10_val = complex_create(log(10.0), 0.0);
    return complex_divide(&log_val, &log10_val);
}

Complex complex_pow(const Complex* base, const Complex* exp) {
    Complex log_val = complex_log(base);
    Complex mul_val = complex_multiply(exp, &log_val);
    return complex_exp(&mul_val);
}

Complex complex_sqrt(const Complex* c) {
    f64 r = complex_abs(c);
    f64 theta = complex_arg(c);
    f64 sqrt_r = sqrt(r);
    return complex_create(sqrt_r * cos(theta / 2.0), sqrt_r * sin(theta / 2.0));
}

Complex complex_sin(const Complex* c) {
    return complex_create(
        sin(c->real) * cosh(c->imag),
        cos(c->real) * sinh(c->imag)
    );
}

Complex complex_cos(const Complex* c) {
    return complex_create(
        cos(c->real) * cosh(c->imag),
        -sin(c->real) * sinh(c->imag)
    );
}

Complex complex_tan(const Complex* c) {
    Complex sin_val = complex_sin(c);
    Complex cos_val = complex_cos(c);
    return complex_divide(&sin_val, &cos_val);
}

Complex complex_asin(const Complex* c) {
    Complex i = complex_create(0.0, 1.0);
    Complex one = complex_create(1.0, 0.0);
    Complex mul_val = complex_multiply(c, c);
    Complex sub_val = complex_subtract(&one, &mul_val);
    Complex sqrt_val = complex_sqrt(&sub_val);
    Complex mul_i_c = complex_multiply(&i, c);
    Complex add_val = complex_add(&mul_i_c, &sqrt_val);
    Complex log_val = complex_log(&add_val);
    return complex_multiply(&i, &log_val);
}

Complex complex_acos(const Complex* c) {
    Complex i = complex_create(0.0, 1.0);
    Complex one = complex_create(1.0, 0.0);
    Complex mul_val = complex_multiply(c, c);
    Complex sub_val = complex_subtract(&one, &mul_val);
    Complex sqrt_val = complex_sqrt(&sub_val);
    Complex mul_i_sqrt = complex_multiply(&i, &sqrt_val);
    Complex add_val = complex_add(c, &mul_i_sqrt);
    Complex log_val = complex_log(&add_val);
    return complex_multiply(&i, &log_val);
}

Complex complex_atan(const Complex* c) {
    Complex i = complex_create(0.0, 1.0);
    Complex one = complex_create(1.0, 0.0);
    Complex mul_i_c = complex_multiply(&i, c);
    Complex add_one_i_c = complex_add(&one, &mul_i_c);
    Complex sub_one_i_c = complex_subtract(&one, &mul_i_c);
    Complex div_val = complex_divide(&add_one_i_c, &sub_one_i_c);
    Complex log_val = complex_log(&div_val);
    Complex mul_i_log = complex_multiply(&i, &log_val);
    Complex two = complex_create(2.0, 0.0);
    return complex_divide(&mul_i_log, &two);
}

Complex complex_sinh(const Complex* c) {
    return complex_create(sinh(c->real) * cos(c->imag), cosh(c->real) * sin(c->imag));
}

Complex complex_cosh(const Complex* c) {
    return complex_create(cosh(c->real) * cos(c->imag), sinh(c->real) * sin(c->imag));
}

Complex complex_tanh(const Complex* c) {
    Complex sinh_val = complex_sinh(c);
    Complex cosh_val = complex_cosh(c);
    return complex_divide(&sinh_val, &cosh_val);
}

Complex complex_asinh(const Complex* c) {
    Complex one = complex_create(1.0, 0.0);
    Complex mul_val = complex_multiply(c, c);
    Complex add_val = complex_add(&mul_val, &one);
    Complex sqrt_val = complex_sqrt(&add_val);
    Complex add_c_sqrt = complex_add(c, &sqrt_val);
    return complex_log(&add_c_sqrt);
}

Complex complex_acosh(const Complex* c) {
    Complex one = complex_create(1.0, 0.0);
    Complex sub_val = complex_subtract(c, &one);
    Complex add_val = complex_add(c, &one);
    Complex mul_val = complex_multiply(&sub_val, &add_val);
    Complex sqrt_val = complex_sqrt(&mul_val);
    Complex add_c_sqrt = complex_add(c, &sqrt_val);
    return complex_log(&add_c_sqrt);
}

Complex complex_atanh(const Complex* c) {
    Complex one = complex_create(1.0, 0.0);
    Complex two = complex_create(2.0, 0.0);
    Complex add_val = complex_add(&one, c);
    Complex sub_val = complex_subtract(&one, c);
    Complex div_val = complex_divide(&add_val, &sub_val);
    Complex log_val = complex_log(&div_val);
    return complex_divide(&log_val, &two);
}

bool_t complex_equal(const Complex* a, const Complex* b) {
    return a->real == b->real && a->imag == b->imag;
}

bool_t complex_is_zero(const Complex* c) {
    return c->real == 0.0 && c->imag == 0.0;
}

bool_t complex_is_real(const Complex* c) {
    return c->imag == 0.0;
}
