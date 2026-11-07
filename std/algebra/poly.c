#include "poly.h"
#include "../memory/memory.h"
#include <string.h>

Poly poly_create(size_t count) {
    Poly p;
    p.count = count > 0 ? count : 1;
    p.coeff = (Rational*)kmm_v4_calloc(p.count, sizeof(Rational));
    if (p.coeff) {
        for (size_t i = 0; i < p.count; i++) {
            p.coeff[i] = rational_zero();
        }
    }
    p.trimmed = true;
    return p;
}

Poly poly_constant(const Rational* c) {
    Poly p = poly_create(1);
    if (p.coeff) {
        rational_destroy(&p.coeff[0]);
        p.coeff[0] = rational_copy(c);
    }
    p.trimmed = true;
    return p;
}

Poly poly_from_rationals(const Rational* coeffs, size_t count) {
    Poly p = poly_create(count);
    if (p.coeff) {
        for (size_t i = 0; i < count; i++) {
            rational_destroy(&p.coeff[i]);
            p.coeff[i] = rational_copy(&coeffs[i]);
        }
    }
    p.trimmed = false;
    return p;
}

Poly poly_from_i64s(const i64* coeffs, size_t count) {
    Poly p = poly_create(count);
    if (p.coeff) {
        for (size_t i = 0; i < count; i++) {
            rational_destroy(&p.coeff[i]);
            p.coeff[i] = rational_from_i64(coeffs[i]);
        }
    }
    p.trimmed = false;
    return p;
}

Poly poly_copy(const Poly* src) {
    Poly p = poly_create(src->count);
    if (p.coeff && src->coeff) {
        for (size_t i = 0; i < src->count; i++) {
            rational_destroy(&p.coeff[i]);
            p.coeff[i] = rational_copy(&src->coeff[i]);
        }
    }
    p.trimmed = src->trimmed;
    return p;
}

void poly_destroy(Poly* p) {
    if (!p) return;
    if (p->coeff) {
        for (size_t i = 0; i < p->count; i++) {
            rational_destroy(&p->coeff[i]);
        }
        kmm_v4_free(p->coeff);
        p->coeff = NULL;
    }
    p->count = 0;
    p->trimmed = true;
}

void poly_get_coefficient(const Poly* p, size_t index, Rational* out) {
    if (index >= p->count) {
        *out = rational_zero();
        return;
    }
    *out = rational_copy(&p->coeff[index]);
}

void poly_set_coefficient(Poly* p, size_t index, const Rational* c) {
    if (index >= p->count) {
        size_t new_count = index + 1;
        Rational* new_coeff = (Rational*)kmm_v4_calloc(new_count, sizeof(Rational));
        if (!new_coeff) return;
        for (size_t i = 0; i < new_count; i++) {
            new_coeff[i] = rational_zero();
        }
        if (p->coeff) {
            for (size_t i = 0; i < p->count; i++) {
                new_coeff[i] = p->coeff[i];
            }
            kmm_v4_free(p->coeff);
        }
        p->coeff = new_coeff;
        p->count = new_count;
        p->trimmed = false;
    }
    if (p->coeff) {
        rational_destroy(&p->coeff[index]);
        p->coeff[index] = rational_copy(c);
        p->trimmed = false;
    }
}

static void poly_trim(Poly* p) {
    if (!p->coeff) return;
    while (p->count > 1 && rational_is_zero(&p->coeff[p->count - 1])) {
        rational_destroy(&p->coeff[p->count - 1]);
        p->count--;
    }
    p->trimmed = true;
}

size_t poly_degree(Poly* p) {
    if (!p->trimmed) poly_trim(p);
    return p->count - 1;
}

size_t poly_count(const Poly* p) {
    return p->count;
}

bool_t poly_is_zero(const Poly* p) {
    for (size_t i = 0; i < p->count; i++) {
        if (!rational_is_zero(&p->coeff[i])) return false;
    }
    return true;
}

bool_t poly_is_constant(const Poly* p) {
    for (size_t i = 1; i < p->count; i++) {
        if (!rational_is_zero(&p->coeff[i])) return false;
    }
    return true;
}

static size_t poly_max_count(size_t a, size_t b) {
    return a > b ? a : b;
}

Poly poly_add(const Poly* a, const Poly* b) {
    size_t n = poly_max_count(a->count, b->count);
    Poly r = poly_create(n);
    if (!r.coeff) return r;
    for (size_t i = 0; i < n; i++) {
        Rational ca = rational_zero();
        Rational cb = rational_zero();
        if (i < a->count) {
            rational_destroy(&ca);
            ca = rational_copy(&a->coeff[i]);
        }
        if (i < b->count) {
            rational_destroy(&cb);
            cb = rational_copy(&b->coeff[i]);
        }
        Rational s = rational_add(&ca, &cb);
        rational_destroy(&ca);
        rational_destroy(&cb);
        rational_destroy(&r.coeff[i]);
        r.coeff[i] = s;
    }
    r.trimmed = false;
    return r;
}

Poly poly_subtract(const Poly* a, const Poly* b) {
    size_t n = poly_max_count(a->count, b->count);
    Poly r = poly_create(n);
    if (!r.coeff) return r;
    for (size_t i = 0; i < n; i++) {
        Rational ca = rational_zero();
        Rational cb = rational_zero();
        if (i < a->count) {
            rational_destroy(&ca);
            ca = rational_copy(&a->coeff[i]);
        }
        if (i < b->count) {
            rational_destroy(&cb);
            cb = rational_copy(&b->coeff[i]);
        }
        Rational s = rational_subtract(&ca, &cb);
        rational_destroy(&ca);
        rational_destroy(&cb);
        rational_destroy(&r.coeff[i]);
        r.coeff[i] = s;
    }
    r.trimmed = false;
    return r;
}

Poly poly_multiply(const Poly* a, const Poly* b) {
    if (poly_is_zero(a) || poly_is_zero(b)) {
        return poly_create(1);
    }
    size_t n = a->count + b->count - 1;
    Poly r = poly_create(n);
    if (!r.coeff) return r;
    for (size_t i = 0; i < a->count; i++) {
        if (rational_is_zero(&a->coeff[i])) continue;
        for (size_t j = 0; j < b->count; j++) {
            if (rational_is_zero(&b->coeff[j])) continue;
            Rational prod = rational_multiply(&a->coeff[i], &b->coeff[j]);
            Rational sum = rational_add(&r.coeff[i + j], &prod);
            rational_destroy(&prod);
            rational_destroy(&r.coeff[i + j]);
            r.coeff[i + j] = sum;
        }
    }
    for (size_t i = 0; i < n; i++) {
        rational_normalize(&r.coeff[i]);
    }
    r.trimmed = false;
    return r;
}

Poly poly_scale(const Poly* p, const Rational* s) {
    Poly r = poly_copy(p);
    if (!r.coeff) return r;
    for (size_t i = 0; i < r.count; i++) {
        Rational prod = rational_multiply(&r.coeff[i], s);
        rational_destroy(&r.coeff[i]);
        r.coeff[i] = prod;
    }
    r.trimmed = false;
    return r;
}

Poly poly_negate(const Poly* p) {
    Poly r = poly_copy(p);
    if (!r.coeff) return r;
    for (size_t i = 0; i < r.count; i++) {
        Rational neg = rational_negate(&r.coeff[i]);
        rational_destroy(&r.coeff[i]);
        r.coeff[i] = neg;
    }
    return r;
}

void poly_divmod(const Poly* a, const Poly* b, Poly* q_out, Poly* r_out) {
    Poly da = poly_copy(a);
    Poly db = poly_copy(b);
    poly_degree(&da);
    poly_degree(&db);
    if (db.count == 1 && rational_is_zero(&db.coeff[0])) {
        *q_out = poly_create(1);
        *r_out = poly_create(1);
        poly_destroy(&da);
        poly_destroy(&db);
        return;
    }
    Poly q = poly_create(1);
    if (da.count >= db.count) {
        size_t q_count = da.count - db.count + 1;
        poly_destroy(&q);
        q = poly_create(q_count);
        size_t r_count = da.count;
        Poly rem = poly_create(r_count);
        for (size_t i = 0; i < r_count; i++) {
            rational_destroy(&rem.coeff[i]);
            rem.coeff[i] = rational_copy(&da.coeff[i]);
        }
        while (rem.count >= db.count && !poly_is_zero(&rem)) {
            size_t shift = rem.count - db.count;
            Rational lead = rational_divide(&rem.coeff[rem.count - 1], &db.coeff[db.count - 1]);
            rational_destroy(&q.coeff[shift]);
            q.coeff[shift] = lead;
            for (size_t i = 0; i < db.count; i++) {
                if (rational_is_zero(&db.coeff[i])) continue;
                Rational prod = rational_multiply(&db.coeff[i], &lead);
                Rational sub = rational_subtract(&rem.coeff[shift + i], &prod);
                rational_destroy(&prod);
                rational_destroy(&rem.coeff[shift + i]);
                rem.coeff[shift + i] = sub;
            }
            poly_trim(&rem);
        }
        poly_trim(&rem);
        *r_out = rem;
        poly_destroy(&da);
    } else {
        *r_out = da;
    }
    poly_trim(&q);
    *q_out = q;
    poly_destroy(&db);
}

Rational poly_evaluate(const Poly* p, const Rational* x) {
    Rational acc = rational_zero();
    for (size_t i = p->count; i-- > 0;) {
        Rational prod = rational_multiply(&acc, x);
        Rational sum = rational_add(&prod, &p->coeff[i]);
        rational_destroy(&acc);
        rational_destroy(&prod);
        acc = sum;
    }
    rational_normalize(&acc);
    return acc;
}

Poly poly_derivative(const Poly* p) {
    if (p->count <= 1) {
        return poly_create(1);
    }
    Poly r = poly_create(p->count - 1);
    if (!r.coeff) return r;
    for (size_t i = 1; i < p->count; i++) {
        Rational k = rational_from_i64((i64)i);
        Rational prod = rational_multiply(&p->coeff[i], &k);
        rational_destroy(&k);
        rational_destroy(&r.coeff[i - 1]);
        r.coeff[i - 1] = prod;
    }
    for (size_t i = 0; i < r.count; i++) {
        rational_normalize(&r.coeff[i]);
    }
    r.trimmed = false;
    return r;
}

Poly poly_integral(const Poly* p) {
    Poly r = poly_create(p->count + 1);
    if (!r.coeff) return r;
    for (size_t i = 0; i < p->count; i++) {
        Rational k = rational_from_i64((i64)(i + 1));
        Rational div = rational_divide(&p->coeff[i], &k);
        rational_destroy(&k);
        rational_destroy(&r.coeff[i + 1]);
        r.coeff[i + 1] = div;
    }
    r.trimmed = false;
    return r;
}

bool_t poly_equal(const Poly* a, const Poly* b) {
    Poly da = poly_copy(a);
    Poly db = poly_copy(b);
    poly_trim(&da);
    poly_trim(&db);
    bool_t eq = da.count == db.count;
    if (eq) {
        for (size_t i = 0; i < da.count; i++) {
            if (!rational_equal(&da.coeff[i], &db.coeff[i])) {
                eq = false;
                break;
            }
        }
    }
    poly_destroy(&da);
    poly_destroy(&db);
    return eq;
}
