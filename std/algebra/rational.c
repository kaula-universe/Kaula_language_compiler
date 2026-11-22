#include "rational.h"
#include "../memory/memory.h"
#include <stdio.h>
#include <string.h>

static Rational rational_make(const BigInt* num, const BigInt* den) {
    Rational r;
    r.num = bigint_copy(num);
    r.den = bigint_copy(den);
    r.normalized = false;
    return r;
}

Rational rational_create(const BigInt* num, const BigInt* den) {
    if (bigint_is_zero(den)) {
        return rational_from_i64(0);
    }
    Rational r = rational_make(num, den);
    if (bigint_is_negative(&r.den)) {
        r.num.negative = !r.num.negative;
        r.den.negative = false;
    }
    if (bigint_is_zero(&r.num)) {
        bigint_destroy(&r.den);
        r.den = bigint_from_i64(1);
    }
    return r;
}

Rational rational_create_i64(i64 num, i64 den) {
    BigInt bn = bigint_from_i64(num);
    BigInt bd = bigint_from_i64(den);
    Rational r = rational_create(&bn, &bd);
    bigint_destroy(&bn);
    bigint_destroy(&bd);
    return r;
}

Rational rational_from_i64(i64 value) {
    return rational_create_i64(value, 1);
}

Rational rational_zero(void) {
    return rational_from_i64(0);
}

Rational rational_one(void) {
    return rational_from_i64(1);
}

Rational rational_copy(const Rational* r) {
    return rational_make(&r->num, &r->den);
}

void rational_destroy(Rational* r) {
    if (!r) return;
    bigint_destroy(&r->num);
    bigint_destroy(&r->den);
}

void rational_normalize(Rational* r) {
    if (r->normalized) return;
    if (bigint_is_zero(&r->num)) {
        bigint_destroy(&r->den);
        r->den = bigint_from_i64(1);
        r->normalized = true;
        return;
    }
    BigInt a = bigint_abs(&r->num);
    BigInt g = bigint_gcd(&a, &r->den);
    if (bigint_compare(&g, &a) != 0 || bigint_compare(&g, &r->den) != 0) {
        BigInt n = bigint_divide(&r->num, &g);
        BigInt d = bigint_divide(&r->den, &g);
        bigint_destroy(&r->num);
        bigint_destroy(&r->den);
        r->num = n;
        r->den = d;
    }
    bigint_destroy(&a);
    bigint_destroy(&g);
    r->normalized = true;
}

bool_t rational_is_normalized(const Rational* r) {
    return r->normalized;
}

bool_t rational_is_zero(const Rational* r) {
    return bigint_is_zero(&r->num);
}

bool_t rational_is_negative(const Rational* r) {
    return bigint_is_negative(&r->num);
}

bool_t rational_is_integer(const Rational* r) {
    if (r->normalized) {
        return r->den.count == 1 && r->den.limbs[0] == 1;
    }
    BigInt q, rem;
    bigint_divmod(&r->num, &r->den, &q, &rem);
    bool_t ok = bigint_is_zero(&rem);
    bigint_destroy(&q);
    bigint_destroy(&rem);
    return ok;
}

int rational_compare(const Rational* a, const Rational* b) {
    BigInt lhs = bigint_multiply(&a->num, &b->den);
    BigInt rhs = bigint_multiply(&b->num, &a->den);
    int c = bigint_compare(&lhs, &rhs);
    bigint_destroy(&lhs);
    bigint_destroy(&rhs);
    return c;
}

bool_t rational_equal(const Rational* a, const Rational* b) {
    return rational_compare(a, b) == 0;
}

Rational rational_negate(const Rational* r) {
    Rational out = rational_copy(r);
    if (!bigint_is_zero(&out.num)) out.num.negative = !out.num.negative;
    return out;
}

Rational rational_abs(const Rational* r) {
    Rational out = rational_copy(r);
    out.num.negative = false;
    return out;
}

Rational rational_inverse(const Rational* r) {
    if (bigint_is_zero(&r->num)) return rational_zero();
    Rational out = rational_make(&r->den, &r->num);
    if (bigint_is_negative(&out.den)) {
        out.num.negative = !out.num.negative;
        out.den.negative = false;
    }
    return out;
}

Rational rational_add(const Rational* a, const Rational* b) {
    BigInt ad = bigint_multiply(&a->num, &b->den);
    BigInt bc = bigint_multiply(&b->num, &a->den);
    BigInt n = bigint_add(&ad, &bc);
    BigInt d = bigint_multiply(&a->den, &b->den);
    bigint_destroy(&ad);
    bigint_destroy(&bc);
    Rational r = rational_make(&n, &d);
    bigint_destroy(&n);
    bigint_destroy(&d);
    return r;
}

Rational rational_subtract(const Rational* a, const Rational* b) {
    Rational nb = rational_negate(b);
    Rational r = rational_add(a, &nb);
    rational_destroy(&nb);
    return r;
}

Rational rational_multiply(const Rational* a, const Rational* b) {
    BigInt n = bigint_multiply(&a->num, &b->num);
    BigInt d = bigint_multiply(&a->den, &b->den);
    Rational r = rational_make(&n, &d);
    bigint_destroy(&n);
    bigint_destroy(&d);
    return r;
}

Rational rational_divide(const Rational* a, const Rational* b) {
    if (bigint_is_zero(&b->num)) return rational_zero();
    BigInt n = bigint_multiply(&a->num, &b->den);
    BigInt d = bigint_multiply(&a->den, &b->num);
    Rational r = rational_make(&n, &d);
    bigint_destroy(&n);
    bigint_destroy(&d);
    if (bigint_is_negative(&r.den)) {
        r.num.negative = !r.num.negative;
        r.den.negative = false;
    }
    rational_normalize(&r);
    return r;
}

Rational rational_pow(const Rational* r, i64 exp) {
    if (exp == 0) return rational_one();
    if (bigint_is_zero(&r->num)) return rational_zero();
    bool_t neg_exp = exp < 0;
    u64 e = neg_exp ? (u64)(-(exp + 1)) + 1 : (u64)exp;
    BigInt n = bigint_pow(&r->num, e);
    BigInt d = bigint_pow(&r->den, e);
    if (neg_exp) {
        BigInt t = n;
        n = d;
        d = t;
    }
    Rational out = rational_make(&n, &d);
    bigint_destroy(&n);
    bigint_destroy(&d);
    if (bigint_is_negative(&out.den)) {
        out.num.negative = !out.num.negative;
        out.den.negative = false;
    }
    rational_normalize(&out);
    return out;
}

BigInt rational_numerator(const Rational* r) {
    return bigint_copy(&r->num);
}

BigInt rational_denominator(const Rational* r) {
    return bigint_copy(&r->den);
}

f64 rational_to_double(const Rational* r) {
    f64 n = 0.0;
    for (size_t i = r->num.count; i-- > 0;) {
        n = n * 4294967296.0 + (f64)r->num.limbs[i];
    }
    f64 d = 0.0;
    for (size_t i = r->den.count; i-- > 0;) {
        d = d * 4294967296.0 + (f64)r->den.limbs[i];
    }
    f64 v = n / d;
    return r->num.negative ? -v : v;
}

char* rational_to_string(const Rational* r) {
    char* num_str = bigint_to_string(&r->num);
    if (!num_str) return NULL;
    if (r->den.count == 1 && r->den.limbs[0] == 1) {
        return num_str;
    }
    char* den_str = bigint_to_string(&r->den);
    if (!den_str) {
        kmm_v4_free(num_str);
        return NULL;
    }
    size_t len = strlen(num_str) + strlen(den_str) + 2;
    char* buf = (char*)kmm_v4_calloc(len, 1);
    if (!buf) {
        kmm_v4_free(num_str);
        kmm_v4_free(den_str);
        return NULL;
    }
    sprintf(buf, "%s/%s", num_str, den_str);
    kmm_v4_free(num_str);
    kmm_v4_free(den_str);
    return buf;
}
