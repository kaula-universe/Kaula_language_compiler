#include "bigint.h"
#include "../memory/memory.h"
#include <stdio.h>
#include <string.h>

#define NTT_MOD1 998244353u
#define NTT_MOD2 469762049u
#define NTT_ROOT 3u
#define NTT_DIGIT_BITS 15
#define NTT_DIGIT_BASE (1u << NTT_DIGIT_BITS)
#define NTT_DIGIT_MASK (NTT_DIGIT_BASE - 1u)
#define NTT_MAX_LEN (1u << 20)
#define NTT_MIN_LIMBS 64
#define NTT_WLEN_MAX 32

typedef struct NttContext {
    u32 mod;
    u32 root;
    bool_t initialized;
    u32 wlen[NTT_WLEN_MAX];
    u32 iwlen[NTT_WLEN_MAX];
} NttContext;

static NttContext g_ntt_ctx[2];
static u32 g_crt_inv_p1_mod_p2 = 0;

#ifdef _MSC_VER
#include <intrin.h>
static u32 clz64(u64 x) {
    unsigned long idx;
    return _BitScanReverse64(&idx, x) ? (u32)(63 - idx) : 64;
}
#else
static u32 clz64(u64 x) {
    return x ? (u32)__builtin_clzll(x) : 64;
}
#endif

static u32 ntt_powmod(u64 base, u64 exp, u32 mod) {
    u64 result = 1 % mod;
    base %= mod;
    while (exp > 0) {
        if (exp & 1) result = result * base % mod;
        base = base * base % mod;
        exp >>= 1;
    }
    return (u32)result;
}

static u32 ntt_add(u32 a, u32 b, u32 mod) {
    u32 r = a + b;
    return r >= mod ? r - mod : r;
}

static u32 ntt_sub(u32 a, u32 b, u32 mod) {
    return a >= b ? a - b : a + mod - b;
}

static u32 ntt_mul(u32 a, u32 b, u32 mod) {
    return (u32)(((u64)a * b) % mod);
}

static void ntt_ctx_init(NttContext* ctx, u32 mod, u32 root) {
    ctx->mod = mod;
    ctx->root = root;
    ctx->initialized = true;
    u32 max_k = 0;
    u32 m = mod - 1;
    while (!(m & 1) && max_k < NTT_WLEN_MAX - 1) {
        m >>= 1;
        max_k++;
    }
    for (u32 k = 1; k <= max_k; k++) {
        u64 exp = (u64)(mod - 1) >> k;
        ctx->wlen[k] = ntt_powmod(root, exp, mod);
        ctx->iwlen[k] = ntt_powmod(ctx->wlen[k], mod - 2, mod);
    }
}

static NttContext* ntt_get_ctx(size_t index, u32* mod_out) {
    NttContext* ctx = &g_ntt_ctx[index];
    if (!ctx->initialized) {
        ntt_ctx_init(ctx, index == 0 ? NTT_MOD1 : NTT_MOD2, NTT_ROOT);
    }
    *mod_out = ctx->mod;
    return ctx;
}

static void ntt_bit_reverse(u32* a, size_t n) {
    for (size_t i = 1, j = 0; i < n; i++) {
        size_t bit = n >> 1;
        for (; j & bit; bit >>= 1) j ^= bit;
        j ^= bit;
        if (i < j) {
            u32 t = a[i];
            a[i] = a[j];
            a[j] = t;
        }
    }
}

static void ntt_transform(u32* a, size_t n, const NttContext* ctx, bool_t invert) {
    ntt_bit_reverse(a, n);
    size_t len = 2;
    u32 k = 1;
    while (len <= n) {
        u32 wlen = invert ? ctx->iwlen[k] : ctx->wlen[k];
        size_t half = len >> 1;
        for (size_t i = 0; i < n; i += len) {
            u32 w = 1;
            for (size_t j = 0; j < half; j++) {
                u32 u = a[i + j];
                u32 v = ntt_mul(a[i + j + half], w, ctx->mod);
                a[i + j] = ntt_add(u, v, ctx->mod);
                a[i + j + half] = ntt_sub(u, v, ctx->mod);
                w = ntt_mul(w, wlen, ctx->mod);
            }
        }
        len <<= 1;
        k++;
    }
    if (invert) {
        u32 inv_n = ntt_powmod((u64)n, ctx->mod - 2, ctx->mod);
        for (size_t i = 0; i < n; i++) {
            a[i] = ntt_mul(a[i], inv_n, ctx->mod);
        }
    }
}

static size_t next_pow2(size_t n) {
    size_t r = 1;
    while (r < n) r <<= 1;
    return r;
}

static void bigint_trim(BigInt* x) {
    while (x->count > 1 && x->limbs[x->count - 1] == 0) x->count--;
    if (x->count == 1 && x->limbs[0] == 0) x->negative = false;
}

static BigInt bigint_alloc(size_t count, bool_t negative) {
    BigInt x;
    x.count = count > 0 ? count : 1;
    // 用系统 malloc(limbs 跨函数/跨存储存活; survivor 段会在函数返回时被回收导致悬空)
    x.limbs = (u64*)std_malloc(x.count * sizeof(u64));
    if (x.limbs) memset(x.limbs, 0, x.count * sizeof(u64));
    x.negative = negative;
    return x;
}

BigInt bigint_from_i64(i64 value) {
    BigInt x = bigint_alloc(1, value < 0);
    u64 v = value < 0 ? (u64)(-(value + 1)) + 1 : (u64)value;
    x.limbs[0] = v;
    bigint_trim(&x);
    return x;
}

BigInt bigint_from_u64(u64 value) {
    BigInt x = bigint_alloc(1, false);
    x.limbs[0] = value;
    bigint_trim(&x);
    return x;
}

BigInt bigint_copy(const BigInt* src) {
    BigInt x = bigint_alloc(src->count, src->negative);
    memcpy(x.limbs, src->limbs, src->count * sizeof(u64));
    return x;
}

void bigint_destroy(BigInt* x) {
    if (!x) return;
    if (x->limbs) {
        std_free(x->limbs);
        x->limbs = NULL;
    }
    x->count = 0;
    x->negative = false;
}

bool_t bigint_is_zero(const BigInt* x) {
    return x->count == 1 && x->limbs[0] == 0;
}

bool_t bigint_is_negative(const BigInt* x) {
    return x->negative && !bigint_is_zero(x);
}

bool_t bigint_is_odd(const BigInt* x) {
    return (x->limbs[0] & 1u) != 0;
}

int bigint_sign(const BigInt* x) {
    if (bigint_is_zero(x)) return 0;
    return x->negative ? -1 : 1;
}

size_t bigint_bit_length(const BigInt* x) {
    if (bigint_is_zero(x)) return 0;
    return (x->count - 1) * 64 + (size_t)(64 - clz64(x->limbs[x->count - 1]));
}

i64 bigint_to_i64(const BigInt* x) {
    u64 v = x->count > 0 ? x->limbs[0] : 0;
    if (x->negative) v = ~v + 1;
    return (i64)v;
}

static int bigint_cmp_mag(const BigInt* a, const BigInt* b) {
    if (a->count != b->count) return a->count < b->count ? -1 : 1;
    for (size_t i = a->count; i-- > 0;) {
        if (a->limbs[i] != b->limbs[i]) {
            return a->limbs[i] < b->limbs[i] ? -1 : 1;
        }
    }
    return 0;
}

int bigint_compare(const BigInt* a, const BigInt* b) {
    if (bigint_is_zero(a) && bigint_is_zero(b)) return 0;
    if (a->negative != b->negative) return a->negative ? -1 : 1;
    int c = bigint_cmp_mag(a, b);
    return a->negative ? -c : c;
}

BigInt bigint_abs(const BigInt* x) {
    BigInt r = bigint_copy(x);
    r.negative = false;
    return r;
}

BigInt bigint_negate(const BigInt* x) {
    BigInt r = bigint_copy(x);
    if (!bigint_is_zero(&r)) r.negative = !r.negative;
    return r;
}

static BigInt bigint_add_mag(const BigInt* a, const BigInt* b) {
    size_t n = a->count > b->count ? a->count : b->count;
    BigInt r = bigint_alloc(n + 1, false);
    u64 carry = 0;
    for (size_t i = 0; i < n; i++) {
        u64 av = i < a->count ? a->limbs[i] : 0;
        u64 bv = i < b->count ? b->limbs[i] : 0;
        __uint128_t s = (__uint128_t)av + bv + carry;
        r.limbs[i] = (u64)s;
        carry = (u64)(s >> 64);
    }
    r.limbs[n] = carry;
    bigint_trim(&r);
    return r;
}

static BigInt bigint_sub_mag(const BigInt* a, const BigInt* b) {
    size_t n = a->count;
    BigInt r = bigint_alloc(n, false);
    u64 borrow = 0;
    for (size_t i = 0; i < n; i++) {
        u64 av = a->limbs[i];
        u64 bv = i < b->count ? b->limbs[i] : 0;
        __uint128_t d = (__uint128_t)av - bv - borrow;
        borrow = (u64)(d >> 64) & 1;
        r.limbs[i] = (u64)d;
    }
    bigint_trim(&r);
    return r;
}

BigInt bigint_add(const BigInt* a, const BigInt* b) {
    if (a->negative == b->negative) {
        BigInt r = bigint_add_mag(a, b);
        r.negative = a->negative;
        return r;
    }
    int c = bigint_cmp_mag(a, b);
    if (c == 0) return bigint_from_i64(0);
    if (c > 0) {
        BigInt r = bigint_sub_mag(a, b);
        r.negative = a->negative;
        return r;
    }
    BigInt r = bigint_sub_mag(b, a);
    r.negative = b->negative;
    return r;
}

BigInt bigint_subtract(const BigInt* a, const BigInt* b) {
    BigInt nb = bigint_negate(b);
    BigInt r = bigint_add(a, &nb);
    bigint_destroy(&nb);
    return r;
}

static BigInt bigint_mul_schoolbook(const BigInt* a, const BigInt* b) {
    BigInt r = bigint_alloc(a->count + b->count, a->negative != b->negative);
    for (size_t i = 0; i < a->count; i++) {
        u64 carry = 0;
        for (size_t j = 0; j < b->count; j++) {
            __uint128_t cur = (__uint128_t)a->limbs[i] * b->limbs[j] + r.limbs[i + j] + carry;
            r.limbs[i + j] = (u64)cur;
            carry = (u64)(cur >> 64);
        }
        size_t k = i + b->count;
        while (carry > 0 && k < r.count) {
            __uint128_t s = (__uint128_t)r.limbs[k] + carry;
            r.limbs[k] = (u64)s;
            carry = (u64)(s >> 64);
            k++;
        }
    }
    bigint_trim(&r);
    return r;
}

static size_t bigint_digit_count(const BigInt* x) {
    size_t bits = bigint_bit_length(x);
    return bits == 0 ? 1 : (bits + NTT_DIGIT_BITS - 1) / NTT_DIGIT_BITS;
}

static u32 bigint_get_digit(const BigInt* x, size_t index) {
    size_t bit = (size_t)NTT_DIGIT_BITS * index;
    size_t limb = bit / 64;
    size_t shift = bit % 64;
    __uint128_t v = limb < x->count ? x->limbs[limb] : 0;
    if (shift + NTT_DIGIT_BITS > 64 && limb + 1 < x->count) {
        v |= (__uint128_t)x->limbs[limb + 1] << 64;
    }
    return (u32)((v >> shift) & NTT_DIGIT_MASK);
}

static BigInt bigint_from_digits(const u64* digits, size_t count) {
    size_t bits = (size_t)NTT_DIGIT_BITS * count;
    size_t limb_count = (bits + 63) / 64;
    BigInt r = bigint_alloc(limb_count, false);
    for (size_t j = 0; j < limb_count; j++) {
        size_t start_bit = 64 * j;
        size_t d0 = start_bit / NTT_DIGIT_BITS;
        size_t rem = start_bit % NTT_DIGIT_BITS;
        __uint128_t acc = 0;
        for (int k = 0; k < 5; k++) {
            if (d0 + (size_t)k < count) {
                acc |= (__uint128_t)digits[d0 + (size_t)k] << (15 * k);
            }
        }
        r.limbs[j] = (u64)((acc >> rem) & 0xFFFFFFFFFFFFFFFFull);
    }
    bigint_trim(&r);
    return r;
}

static BigInt bigint_mul_ntt(const BigInt* a, const BigInt* b) {
    BigInt zero;
    zero.count = 0;
    zero.limbs = NULL;
    zero.negative = false;

    size_t da = bigint_digit_count(a);
    size_t db = bigint_digit_count(b);
    size_t dc = da + db - 1;
    size_t n = next_pow2(dc);
    if (n > NTT_MAX_LEN) return zero;

    u32* xa = (u32*)kmm_v4_calloc(n, sizeof(u32));
    u32* xb = (u32*)kmm_v4_calloc(n, sizeof(u32));
    u64* conv = (u64*)kmm_v4_calloc(n + 4, sizeof(u64));
    if (!xa || !xb || !conv) {
        if (xa) kmm_v4_free(xa);
        if (xb) kmm_v4_free(xb);
        if (conv) kmm_v4_free(conv);
        return zero;
    }

    for (int prime = 0; prime < 2; prime++) {
        u32 mod;
        NttContext* ctx = ntt_get_ctx((size_t)prime, &mod);
        memset(xa, 0, n * sizeof(u32));
        memset(xb, 0, n * sizeof(u32));
        for (size_t i = 0; i < da; i++) xa[i] = bigint_get_digit(a, i);
        for (size_t i = 0; i < db; i++) xb[i] = bigint_get_digit(b, i);
        ntt_transform(xa, n, ctx, false);
        ntt_transform(xb, n, ctx, false);
        for (size_t i = 0; i < n; i++) {
            xa[i] = ntt_mul(xa[i], xb[i], mod);
        }
        ntt_transform(xa, n, ctx, true);
        if (prime == 0) {
            for (size_t i = 0; i < dc; i++) conv[i] = xa[i];
        } else {
            if (g_crt_inv_p1_mod_p2 == 0) {
                g_crt_inv_p1_mod_p2 = ntt_powmod(NTT_MOD1, NTT_MOD2 - 2, NTT_MOD2);
            }
            for (size_t i = 0; i < dc; i++) {
                u64 c1 = conv[i];
                u64 c2 = xa[i];
                u64 diff = (c2 + NTT_MOD2 - (u64)(c1 % NTT_MOD2)) % NTT_MOD2;
                u64 k = (u64)diff * g_crt_inv_p1_mod_p2 % NTT_MOD2;
                conv[i] = c1 + (u64)NTT_MOD1 * k;
            }
        }
    }

    kmm_v4_free(xa);
    kmm_v4_free(xb);

    u64 carry = 0;
    size_t total = dc;
    for (size_t i = 0; i < dc; i++) {
        u64 v = conv[i] + carry;
        conv[i] = v & NTT_DIGIT_MASK;
        carry = v >> NTT_DIGIT_BITS;
    }
    while (carry > 0) {
        conv[total++] = carry & NTT_DIGIT_MASK;
        carry >>= NTT_DIGIT_BITS;
    }

    BigInt r = bigint_from_digits(conv, total);
    r.negative = a->negative != b->negative;
    kmm_v4_free(conv);
    return r;
}

BigInt bigint_multiply(const BigInt* a, const BigInt* b) {
    if (bigint_is_zero(a) || bigint_is_zero(b)) return bigint_from_i64(0);
    if (a->count < NTT_MIN_LIMBS && b->count < NTT_MIN_LIMBS) {
        return bigint_mul_schoolbook(a, b);
    }
    BigInt r = bigint_mul_ntt(a, b);
    if (r.count == 0) {
        return bigint_mul_schoolbook(a, b);
    }
    return r;
}

static u64 bigint_divmod_small(BigInt* x, u64 divisor) {
    u64 rem = 0;
    for (size_t i = x->count; i-- > 0;) {
        __uint128_t cur = ((__uint128_t)rem << 64) | x->limbs[i];
        x->limbs[i] = (u64)(cur / divisor);
        rem = (u64)(cur % divisor);
    }
    bigint_trim(x);
    return rem;
}

static void bigint_divmod_mag(const BigInt* num, const BigInt* den,
                              BigInt* q_out, BigInt* r_out) {
    if (bigint_cmp_mag(num, den) < 0) {
        *q_out = bigint_from_i64(0);
        *r_out = bigint_copy(num);
        return;
    }
    size_t n = den->count;
    if (n == 1) {
        BigInt q = bigint_copy(num);
        u32 rem = bigint_divmod_small(&q, den->limbs[0]);
        *q_out = q;
        *r_out = bigint_from_u64(rem);
        return;
    }

    u32 shift = clz64(den->limbs[n - 1]);
    size_t m = num->count - n;
    size_t u_count = m + n + 1;
    u64* U = (u64*)kmm_v4_calloc(u_count, sizeof(u64));
    u64* V = (u64*)kmm_v4_calloc(n, sizeof(u64));
    if (!U || !V) {
        if (U) kmm_v4_free(U);
        if (V) kmm_v4_free(V);
        *q_out = bigint_from_i64(0);
        *r_out = bigint_from_i64(0);
        return;
    }

    if (shift == 0) {
        for (size_t i = 0; i < num->count; i++) U[i] = num->limbs[i];
        for (size_t i = 0; i < n; i++) V[i] = den->limbs[i];
    } else {
        u64 carry = 0;
        for (size_t i = 0; i < n; i++) {
            __uint128_t cur = ((__uint128_t)den->limbs[i] << shift) | carry;
            V[i] = (u64)cur;
            carry = (u64)(cur >> 64);
        }
        carry = 0;
        for (size_t i = 0; i < num->count; i++) {
            __uint128_t cur = ((__uint128_t)num->limbs[i] << shift) | carry;
            U[i] = (u64)cur;
            carry = (u64)(cur >> 64);
        }
        U[num->count] = carry;
    }

    u64* Q = (u64*)kmm_v4_calloc(m + 1, sizeof(u64));
    if (!Q) {
        kmm_v4_free(U);
        kmm_v4_free(V);
        *q_out = bigint_from_i64(0);
        *r_out = bigint_from_i64(0);
        return;
    }

    const __uint128_t B = (__uint128_t)1 << 64;
    for (size_t j = m + 1; j-- > 0;) {
        __uint128_t top = ((__uint128_t)U[j + n] << 64) | U[j + n - 1];
        u64 qhat = (u64)(top / V[n - 1]);
        u64 rhat = (u64)(top % V[n - 1]);
        while ((__uint128_t)qhat * V[n - 2] > (((__uint128_t)rhat << 64) | U[j + n - 2])) {
            qhat--;
            rhat += V[n - 1];
            if (rhat >= (u64)B) break;
        }
        u64 borrow = 0;
        for (size_t i = 0; i < n; i++) {
            __uint128_t p = (__uint128_t)qhat * V[i] + borrow;
            borrow = (u64)(p >> 64);
            u64 sub = (u64)p;
            u64 uv = U[j + i];
            if (uv < sub) {
                U[j + i] = (u64)(uv + (u64)B - sub);
                borrow++;
            } else {
                U[j + i] = uv - sub;
            }
        }
        if (U[j + n] < borrow) {
            u64 carry = 0;
            for (size_t i = 0; i < n; i++) {
                __uint128_t s = (__uint128_t)U[j + i] + V[i] + carry;
                U[j + i] = (u64)s;
                carry = (u64)(s >> 64);
            }
            U[j + n] = U[j + n] + carry;
            qhat--;
        } else {
            U[j + n] = U[j + n] - borrow;
        }
        Q[j] = qhat;
    }

    BigInt q = bigint_alloc(m + 1, false);
    memcpy(q.limbs, Q, (m + 1) * sizeof(u64));
    bigint_trim(&q);

    BigInt r = bigint_alloc(n, false);
    if (shift == 0) {
        memcpy(r.limbs, U, n * sizeof(u64));
    } else {
        u64 carry = 0;
        for (size_t i = n; i-- > 0;) {
            u64 cur = U[i];
            r.limbs[i] = (cur >> shift) | (carry << (64 - shift));
            carry = cur & (((u64)1 << shift) - 1u);
        }
    }
    bigint_trim(&r);

    kmm_v4_free(U);
    kmm_v4_free(V);
    kmm_v4_free(Q);
    *q_out = q;
    *r_out = r;
}

void bigint_divmod(const BigInt* a, const BigInt* b, BigInt* q, BigInt* r) {
    if (bigint_is_zero(b)) {
        *q = bigint_from_i64(0);
        *r = bigint_from_i64(0);
        return;
    }
    if (bigint_is_zero(a)) {
        *q = bigint_from_i64(0);
        *r = bigint_from_i64(0);
        return;
    }
    bool_t q_neg = a->negative != b->negative;
    bool_t r_neg = a->negative;
    BigInt mq, mr;
    bigint_divmod_mag(a, b, &mq, &mr);
    if (q_neg) mq.negative = true;
    if (r_neg) mr.negative = true;
    bigint_trim(&mq);
    bigint_trim(&mr);
    *q = mq;
    *r = mr;
}

BigInt bigint_divide(const BigInt* a, const BigInt* b) {
    BigInt q, r;
    bigint_divmod(a, b, &q, &r);
    bigint_destroy(&r);
    return q;
}

BigInt bigint_mod(const BigInt* a, const BigInt* b) {
    BigInt q, r;
    bigint_divmod(a, b, &q, &r);
    bigint_destroy(&q);
    return r;
}

BigInt bigint_gcd(const BigInt* a, const BigInt* b) {
    BigInt x = bigint_abs(a);
    BigInt y = bigint_abs(b);
    while (!bigint_is_zero(&y)) {
        BigInt q, r;
        bigint_divmod(&x, &y, &q, &r);
        bigint_destroy(&q);
        bigint_destroy(&x);
        x = y;
        y = r;
    }
    bigint_destroy(&y);
    return x;
}

BigInt bigint_pow(const BigInt* base, u64 exp) {
    if (exp == 0) return bigint_from_i64(1);
    BigInt result = bigint_from_i64(1);
    BigInt b = bigint_copy(base);
    while (exp > 0) {
        if (exp & 1) {
            BigInt t = bigint_multiply(&result, &b);
            bigint_destroy(&result);
            result = t;
        }
        exp >>= 1;
        if (exp > 0) {
            BigInt t = bigint_multiply(&b, &b);
            bigint_destroy(&b);
            b = t;
        }
    }
    bigint_destroy(&b);
    return result;
}

BigInt bigint_pow_mod(const BigInt* base, u64 exp, const BigInt* mod) {
    if (bigint_is_zero(mod)) return bigint_from_i64(0);
    BigInt result = bigint_from_i64(1);
    BigInt b = bigint_mod(base, mod);
    while (exp > 0) {
        if (exp & 1) {
            BigInt t = bigint_multiply(&result, &b);
            BigInt m = bigint_mod(&t, mod);
            bigint_destroy(&result);
            bigint_destroy(&t);
            result = m;
        }
        exp >>= 1;
        if (exp > 0) {
            BigInt t = bigint_multiply(&b, &b);
            BigInt m = bigint_mod(&t, mod);
            bigint_destroy(&b);
            bigint_destroy(&t);
            b = m;
        }
    }
    bigint_destroy(&b);
    return result;
}

BigInt bigint_from_string(const char* str) {
    if (!str || !*str) return bigint_from_i64(0);
    bool_t neg = false;
    const char* p = str;
    if (*p == '-' || *p == '+') {
        neg = (*p == '-');
        p++;
    }
    BigInt r = bigint_from_i64(0);
    BigInt ten = bigint_from_i64(10);
    BigInt digit;
    while (*p) {
        if (*p < '0' || *p > '9') break;
        BigInt t = bigint_multiply(&r, &ten);
        bigint_destroy(&r);
        r = t;
        digit = bigint_from_i64(*p - '0');
        BigInt s = bigint_add(&r, &digit);
        bigint_destroy(&r);
        bigint_destroy(&digit);
        r = s;
        p++;
    }
    bigint_destroy(&ten);
    if (neg && !bigint_is_zero(&r)) r.negative = true;
    return r;
}

static size_t bigint_decimal_chunks(const BigInt* x, u32* chunks_out, size_t capacity) {
    BigInt work = bigint_copy(x);
    size_t count = 0;
    const u32 div = 1000000000u;
    while (!bigint_is_zero(&work)) {
        if (count >= capacity) break;
        chunks_out[count++] = bigint_divmod_small(&work, div);
    }
    bigint_destroy(&work);
    return count;
}

char* bigint_to_string(const BigInt* x) {
    if (bigint_is_zero(x)) {
        char* s = (char*)kmm_v4_calloc(2, 1);
        s[0] = '0';
        return s;
    }
    size_t chunks_capacity = (x->count * 64 + 28) / 29 + 4;
    u32* chunks = (u32*)kmm_v4_malloc(chunks_capacity * sizeof(u32));
    if (!chunks) return NULL;
    size_t n = bigint_decimal_chunks(x, chunks, chunks_capacity);
    size_t len = (x->negative ? 1 : 0) + 1 + n * 9;
    char* buf = (char*)kmm_v4_calloc(len + 1, 1);
    if (!buf) {
        kmm_v4_free(chunks);
        return NULL;
    }
    char* p = buf;
    if (x->negative) *p++ = '-';
    for (size_t i = n; i-- > 0;) {
        if (i == n - 1) {
            p += sprintf(p, "%u", chunks[i]);
        } else {
            p += sprintf(p, "%09u", chunks[i]);
        }
    }
    kmm_v4_free(chunks);
    return buf;
}
