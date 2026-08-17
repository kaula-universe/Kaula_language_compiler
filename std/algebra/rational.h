#ifndef STD_ALGEBRA_RATIONAL_H
#define STD_ALGEBRA_RATIONAL_H

#include "../base/types.h"
#include "bigint.h"

typedef struct Rational {
    BigInt num;
    BigInt den;
    bool_t normalized;
} Rational;

Rational rational_create(const BigInt* num, const BigInt* den);
Rational rational_create_i64(i64 num, i64 den);
Rational rational_from_i64(i64 value);
Rational rational_zero(void);
Rational rational_one(void);
Rational rational_copy(const Rational* r);
void rational_destroy(Rational* r);

void rational_normalize(Rational* r);
bool_t rational_is_normalized(const Rational* r);

bool_t rational_is_zero(const Rational* r);
bool_t rational_is_negative(const Rational* r);
bool_t rational_is_integer(const Rational* r);

int rational_compare(const Rational* a, const Rational* b);
bool_t rational_equal(const Rational* a, const Rational* b);

Rational rational_negate(const Rational* r);
Rational rational_abs(const Rational* r);
Rational rational_inverse(const Rational* r);

Rational rational_add(const Rational* a, const Rational* b);
Rational rational_subtract(const Rational* a, const Rational* b);
Rational rational_multiply(const Rational* a, const Rational* b);
Rational rational_divide(const Rational* a, const Rational* b);
Rational rational_pow(const Rational* r, i64 exp);

BigInt rational_numerator(const Rational* r);
BigInt rational_denominator(const Rational* r);

f64 rational_to_double(const Rational* r);
char* rational_to_string(const Rational* r);

#endif // STD_ALGEBRA_RATIONAL_H