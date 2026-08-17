#ifndef STD_ALGEBRA_POLY_H
#define STD_ALGEBRA_POLY_H

#include "../base/types.h"
#include "rational.h"

typedef struct Poly {
    Rational* coeff;
    size_t count;
    bool_t trimmed;
} Poly;

Poly poly_create(size_t count);
Poly poly_constant(const Rational* c);
Poly poly_from_rationals(const Rational* coeffs, size_t count);
Poly poly_from_i64s(const i64* coeffs, size_t count);
Poly poly_copy(const Poly* p);
void poly_destroy(Poly* p);

void poly_get_coefficient(const Poly* p, size_t index, Rational* out);
void poly_set_coefficient(Poly* p, size_t index, const Rational* c);

size_t poly_degree(Poly* p);
size_t poly_count(const Poly* p);
bool_t poly_is_zero(const Poly* p);
bool_t poly_is_constant(const Poly* p);

Poly poly_add(const Poly* a, const Poly* b);
Poly poly_subtract(const Poly* a, const Poly* b);
Poly poly_multiply(const Poly* a, const Poly* b);
Poly poly_scale(const Poly* p, const Rational* s);
Poly poly_negate(const Poly* p);

void poly_divmod(const Poly* a, const Poly* b, Poly* q, Poly* r);

Rational poly_evaluate(const Poly* p, const Rational* x);
Poly poly_derivative(const Poly* p);
Poly poly_integral(const Poly* p);

bool_t poly_equal(const Poly* a, const Poly* b);

#endif // STD_ALGEBRA_POLY_H