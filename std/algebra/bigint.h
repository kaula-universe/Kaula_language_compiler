#ifndef STD_ALGEBRA_BIGINT_H
#define STD_ALGEBRA_BIGINT_H

#include "../base/types.h"

typedef struct BigInt {
    u32* limbs;
    size_t count;
    bool_t negative;
} BigInt;

BigInt bigint_from_i64(i64 value);
BigInt bigint_from_u64(u64 value);
BigInt bigint_from_string(const char* str);
BigInt bigint_copy(const BigInt* x);
void bigint_destroy(BigInt* x);

bool_t bigint_is_zero(const BigInt* x);
bool_t bigint_is_negative(const BigInt* x);
bool_t bigint_is_odd(const BigInt* x);
int bigint_sign(const BigInt* x);
size_t bigint_bit_length(const BigInt* x);
i64 bigint_to_i64(const BigInt* x);

int bigint_compare(const BigInt* a, const BigInt* b);

BigInt bigint_abs(const BigInt* x);
BigInt bigint_negate(const BigInt* x);

BigInt bigint_add(const BigInt* a, const BigInt* b);
BigInt bigint_subtract(const BigInt* a, const BigInt* b);
BigInt bigint_multiply(const BigInt* a, const BigInt* b);
BigInt bigint_divide(const BigInt* a, const BigInt* b);
BigInt bigint_mod(const BigInt* a, const BigInt* b);
void bigint_divmod(const BigInt* a, const BigInt* b, BigInt* q, BigInt* r);

BigInt bigint_gcd(const BigInt* a, const BigInt* b);
BigInt bigint_pow(const BigInt* base, u64 exp);
BigInt bigint_pow_mod(const BigInt* base, u64 exp, const BigInt* mod);

char* bigint_to_string(const BigInt* x);

#endif // STD_ALGEBRA_BIGINT_H