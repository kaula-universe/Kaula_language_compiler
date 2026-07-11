#include "hash_ext.h"
#include "../memory/memory.h"
#include <string.h>

/* ============================================================
 * Internal helpers
 * ============================================================ */

#define ROTL32(x, n) (((x) << (n)) | ((x) >> (32 - (n))))
#define ROTR64(x, n) (((x) >> (n)) | ((x) << (64 - (n))))

static u32 be32_read(const u8* p) {
    return ((u32)p[0] << 24) | ((u32)p[1] << 16) | ((u32)p[2] << 8) | (u32)p[3];
}

static void be32_write(u8* p, u32 v) {
    p[0] = (u8)(v >> 24);
    p[1] = (u8)(v >> 16);
    p[2] = (u8)(v >> 8);
    p[3] = (u8)v;
}

static u64 be64_read(const u8* p) {
    return ((u64)p[0] << 56) | ((u64)p[1] << 48) | ((u64)p[2] << 40) |
           ((u64)p[3] << 32) | ((u64)p[4] << 24) | ((u64)p[5] << 16) |
           ((u64)p[6] << 8)  | (u64)p[7];
}

static void be64_write(u8* p, u64 v) {
    p[0] = (u8)(v >> 56);
    p[1] = (u8)(v >> 48);
    p[2] = (u8)(v >> 40);
    p[3] = (u8)(v >> 32);
    p[4] = (u8)(v >> 24);
    p[5] = (u8)(v >> 16);
    p[6] = (u8)(v >> 8);
    p[7] = (u8)v;
}

static String bytes_to_hex(const u8* data, size_t len) {
    static const char hexchars[] = "0123456789abcdef";
    String out;
    size_t i;
    out = (String)kmm_v4_malloc(len * 2 + 1);
    if (!out) return NULL;
    for (i = 0; i < len; i++) {
        out[i * 2]     = hexchars[(data[i] >> 4) & 0xF];
        out[i * 2 + 1] = hexchars[data[i] & 0xF];
    }
    out[len * 2] = '\0';
    return out;
}

/* ============================================================
 * SHA-1 (FIPS 180-4)
 * ============================================================ */

static void sha1_transform(u32 state[5], const u8 block[64]) {
    u32 w[80];
    u32 a, b, c, d, e, f, k, t;
    int i;
    for (i = 0; i < 16; i++) {
        w[i] = be32_read(block + i * 4);
    }
    for (i = 16; i < 80; i++) {
        w[i] = ROTL32(w[i - 3] ^ w[i - 8] ^ w[i - 14] ^ w[i - 16], 1);
    }
    a = state[0]; b = state[1]; c = state[2]; d = state[3]; e = state[4];
    for (i = 0; i < 80; i++) {
        if (i < 20) {
            f = (b & c) | (~b & d);
            k = 0x5A827999u;
        } else if (i < 40) {
            f = b ^ c ^ d;
            k = 0x6ED9EBA1u;
        } else if (i < 60) {
            f = (b & c) | (b & d) | (c & d);
            k = 0x8F1BBCDCu;
        } else {
            f = b ^ c ^ d;
            k = 0xCA62C1D6u;
        }
        t = ROTL32(a, 5) + f + e + k + w[i];
        e = d; d = c; c = ROTL32(b, 30); b = a; a = t;
    }
    state[0] += a; state[1] += b; state[2] += c; state[3] += d; state[4] += e;
}

static void sha1_init(u32 state[5], u64* bit_count) {
    state[0] = 0x67452301u;
    state[1] = 0xEFCDAB89u;
    state[2] = 0x98BADCFEu;
    state[3] = 0x10325476u;
    state[4] = 0xC3D2E1F0u;
    *bit_count = 0;
}

static void sha1_update_core(u32 state[5], u64* bit_count, u8 buffer[64],
                             size_t* buf_len, const void* data, size_t len) {
    const u8* p = (const u8*)data;
    *bit_count += (u64)len * 8;
    if (*buf_len > 0) {
        size_t need = 64 - *buf_len;
        if (len < need) {
            memcpy(buffer + *buf_len, p, len);
            *buf_len += len;
            return;
        }
        memcpy(buffer + *buf_len, p, need);
        sha1_transform(state, buffer);
        p += need;
        len -= need;
        *buf_len = 0;
    }
    while (len >= 64) {
        sha1_transform(state, p);
        p += 64;
        len -= 64;
    }
    if (len > 0) {
        memcpy(buffer, p, len);
        *buf_len = len;
    }
}

static void sha1_final_core(u32 state[5], u64 bit_count, u8 buffer[64],
                            size_t buf_len, u8 out[20]) {
    size_t i;
    buffer[buf_len++] = 0x80;
    if (buf_len > 56) {
        memset(buffer + buf_len, 0, 64 - buf_len);
        sha1_transform(state, buffer);
        buf_len = 0;
    }
    memset(buffer + buf_len, 0, 56 - buf_len);
    be64_write(buffer + 56, bit_count);
    sha1_transform(state, buffer);
    for (i = 0; i < 5; i++) {
        be32_write(out + i * 4, state[i]);
    }
}

SHA1Digest sha1_hash(const void* data, size_t len) {
    SHA1Digest result;
    u32 state[5];
    u64 bit_count;
    u8 buffer[64];
    size_t buf_len = 0;
    sha1_init(state, &bit_count);
    if (data && len > 0) {
        sha1_update_core(state, &bit_count, buffer, &buf_len, data, len);
    }
    sha1_final_core(state, bit_count, buffer, buf_len, result.digest);
    return result;
}

String sha1_hex(const void* data, size_t len) {
    SHA1Digest d = sha1_hash(data, len);
    return bytes_to_hex(d.digest, 20);
}

struct SHA1Context {
    u32 state[5];
    u64 bit_count;
    u8  buffer[64];
    size_t buf_len;
};

SHA1Context* sha1_ctx_create(void) {
    SHA1Context* ctx = (SHA1Context*)kmm_v4_malloc(sizeof(SHA1Context));
    if (!ctx) return NULL;
    sha1_init(ctx->state, &ctx->bit_count);
    ctx->buf_len = 0;
    return ctx;
}

void sha1_ctx_update(SHA1Context* ctx, const void* data, size_t len) {
    if (!ctx || !data || len == 0) return;
    sha1_update_core(ctx->state, &ctx->bit_count, ctx->buffer, &ctx->buf_len, data, len);
}

SHA1Digest sha1_ctx_final(SHA1Context* ctx) {
    SHA1Digest result;
    if (!ctx) {
        memset(&result, 0, sizeof(result));
        return result;
    }
    sha1_final_core(ctx->state, ctx->bit_count, ctx->buffer, ctx->buf_len, result.digest);
    return result;
}

void sha1_ctx_destroy(SHA1Context* ctx) {
    if (!ctx) return;
    memset(ctx, 0, sizeof(SHA1Context));
    kmm_v4_free(ctx);
}

/* ============================================================
 * SHA-512 / SHA-384 (FIPS 180-4)
 * ============================================================ */

static const u64 SHA512_K[80] = {
    0x428a2f98d728ae22ULL, 0x7137449123ef65cdULL, 0xb5c0fbcfec4d3b2fULL, 0xe9b5dba58189dbbcULL,
    0x3956c25bf348b538ULL, 0x59f111f1b605d019ULL, 0x923f82a4af194f9bULL, 0xab1c5ed5da6d8118ULL,
    0xd807aa98a3030242ULL, 0x12835b0145706fbeULL, 0x243185be4ee4b28cULL, 0x550c7dc3d5ffb4e2ULL,
    0x72be5d74f27b896fULL, 0x80deb1fe3b1696b1ULL, 0x9bdc06a725c71235ULL, 0xc19bf174cf692694ULL,
    0xe49b69c19ef14ad2ULL, 0xefbe4786384f25e3ULL, 0x0fc19dc68b8cd5b5ULL, 0x240ca1cc77ac9c65ULL,
    0x2de92c6f592b0275ULL, 0x4a7484aa6ea6e483ULL, 0x5cb0a9dcbd41fbd4ULL, 0x76f988da831153b5ULL,
    0x983e5152ee66dfabULL, 0xa831c66d2db43210ULL, 0xb00327c898fb213fULL, 0xbf597fc7beef0ee4ULL,
    0xc6e00bf33da88fc2ULL, 0xd5a79147930aa725ULL, 0x06ca6351e003826fULL, 0x142929670a0e6e70ULL,
    0x27b70a8546d22ffcULL, 0x2e1b21385c26c926ULL, 0x4d2c6dfc5ac42aedULL, 0x53380d139d95b3dfULL,
    0x650a73548baf63deULL, 0x766a0abb3c77b2a8ULL, 0x81c2c92e47edaee6ULL, 0x92722c851482353bULL,
    0xa2bfe8a14cf10364ULL, 0xa81a664bbc423001ULL, 0xc24b8b70d0f89791ULL, 0xc76c51a30654be30ULL,
    0xd192e819d6ef5218ULL, 0xd69906245565a910ULL, 0xf40e35855771202aULL, 0x106aa07032bbd1b8ULL,
    0x19a4c116b8d2d0c8ULL, 0x1e376c085141ab53ULL, 0x2748774cdf8eeb99ULL, 0x34b0bcb5e19b48a8ULL,
    0x391c0cb3c5c95a63ULL, 0x4ed8aa4ae3418acbULL, 0x5b9cca4f7763e373ULL, 0x682e6ff3d6b2b8a3ULL,
    0x748f82ee5defb2fcULL, 0x78a5636f43172f60ULL, 0x84c87814a1f0ab72ULL, 0x8cc702081a6439ecULL,
    0x90befffa23631e28ULL, 0xa4506cebde82bde9ULL, 0xbef9a3f7b2c67915ULL, 0xc67178f2e372532bULL,
    0xca273eceea26619cULL, 0xd186b8c721c0c207ULL, 0xeada7dd6cde0eb1eULL, 0xf57d4f7fee6ed178ULL,
    0x06f067aa72176fbaULL, 0x0a637dc5a2c898a6ULL, 0x113f9804bef90daeULL, 0x1b710b35131c471bULL,
    0x28db77f523047d84ULL, 0x32caab7b40c72493ULL, 0x3c9ebe0a15c9bebcULL, 0x431d67c49c100d4cULL,
    0x4cc5d4becb3e42b6ULL, 0x597f299cfc657e2aULL, 0x5fcb6fab3ad6faecULL, 0x6c44198c4a475817ULL
};

static void sha512_transform(u64 state[8], const u8 block[128]) {
    u64 w[80];
    u64 a, b, c, d, e, f, g, h;
    u64 t1, t2, s0, s1, S0, S1, ch, maj;
    int i;
    for (i = 0; i < 16; i++) {
        w[i] = be64_read(block + i * 8);
    }
    for (i = 16; i < 80; i++) {
        s0 = ROTR64(w[i - 15], 1) ^ ROTR64(w[i - 15], 8) ^ (w[i - 15] >> 7);
        s1 = ROTR64(w[i - 2], 19) ^ ROTR64(w[i - 2], 61) ^ (w[i - 2] >> 6);
        w[i] = w[i - 16] + s0 + w[i - 7] + s1;
    }
    a = state[0]; b = state[1]; c = state[2]; d = state[3];
    e = state[4]; f = state[5]; g = state[6]; h = state[7];
    for (i = 0; i < 80; i++) {
        S1  = ROTR64(e, 14) ^ ROTR64(e, 18) ^ ROTR64(e, 41);
        ch  = (e & f) ^ (~e & g);
        t1  = h + S1 + ch + SHA512_K[i] + w[i];
        S0  = ROTR64(a, 28) ^ ROTR64(a, 34) ^ ROTR64(a, 39);
        maj = (a & b) ^ (a & c) ^ (b & c);
        t2  = S0 + maj;
        h = g; g = f; f = e; e = d + t1; d = c; c = b; b = a; a = t1 + t2;
    }
    state[0] += a; state[1] += b; state[2] += c; state[3] += d;
    state[4] += e; state[5] += f; state[6] += g; state[7] += h;
}

static void sha512_init(u64 state[8], u64* bit_count, int is_384) {
    if (is_384) {
        state[0] = 0xcbbb9d5dc1059ed8ULL;
        state[1] = 0x629a292a367cd507ULL;
        state[2] = 0x9159015a3070dd17ULL;
        state[3] = 0x152fecd8f70e5939ULL;
        state[4] = 0x67332667ffc00b31ULL;
        state[5] = 0x8eb44a8768581511ULL;
        state[6] = 0xdb0c2e0d64f98fa7ULL;
        state[7] = 0x47b5481dbefa4fa4ULL;
    } else {
        state[0] = 0x6a09e667f3bcc908ULL;
        state[1] = 0xbb67ae8584caa73bULL;
        state[2] = 0x3c6ef372fe94f82bULL;
        state[3] = 0xa54ff53a5f1d36f1ULL;
        state[4] = 0x510e527fade682d1ULL;
        state[5] = 0x9b05688c2b3e6c1fULL;
        state[6] = 0x1f83d9abfb41bd6bULL;
        state[7] = 0x5be0cd19137e2179ULL;
    }
    *bit_count = 0;
}

static void sha512_update_core(u64 state[8], u64* bit_count, u8 buffer[128],
                               size_t* buf_len, const void* data, size_t len) {
    const u8* p = (const u8*)data;
    *bit_count += (u64)len * 8;
    if (*buf_len > 0) {
        size_t need = 128 - *buf_len;
        if (len < need) {
            memcpy(buffer + *buf_len, p, len);
            *buf_len += len;
            return;
        }
        memcpy(buffer + *buf_len, p, need);
        sha512_transform(state, buffer);
        p += need;
        len -= need;
        *buf_len = 0;
    }
    while (len >= 128) {
        sha512_transform(state, p);
        p += 128;
        len -= 128;
    }
    if (len > 0) {
        memcpy(buffer, p, len);
        *buf_len = len;
    }
}

static void sha512_final_core(u64 state[8], u64 bit_count, u8 buffer[128],
                              size_t buf_len, u8* out, int is_384) {
    size_t out_words = is_384 ? 6 : 8;
    size_t i;
    buffer[buf_len++] = 0x80;
    if (buf_len > 112) {
        memset(buffer + buf_len, 0, 128 - buf_len);
        sha512_transform(state, buffer);
        buf_len = 0;
    }
    memset(buffer + buf_len, 0, 112 - buf_len);
    /* 128-bit big-endian bit length (high 64 bits assumed 0 for practical sizes) */
    be64_write(buffer + 112, 0);
    be64_write(buffer + 120, bit_count);
    sha512_transform(state, buffer);
    for (i = 0; i < out_words; i++) {
        be64_write(out + i * 8, state[i]);
    }
}

SHA384Digest sha384_hash(const void* data, size_t len) {
    SHA384Digest result;
    u64 state[8];
    u64 bit_count;
    u8 buffer[128];
    size_t buf_len = 0;
    sha512_init(state, &bit_count, 1);
    if (data && len > 0) {
        sha512_update_core(state, &bit_count, buffer, &buf_len, data, len);
    }
    sha512_final_core(state, bit_count, buffer, buf_len, result.digest, 1);
    return result;
}

String sha384_hex(const void* data, size_t len) {
    SHA384Digest d = sha384_hash(data, len);
    return bytes_to_hex(d.digest, 48);
}

SHA512Digest sha512_hash(const void* data, size_t len) {
    SHA512Digest result;
    u64 state[8];
    u64 bit_count;
    u8 buffer[128];
    size_t buf_len = 0;
    sha512_init(state, &bit_count, 0);
    if (data && len > 0) {
        sha512_update_core(state, &bit_count, buffer, &buf_len, data, len);
    }
    sha512_final_core(state, bit_count, buffer, buf_len, result.digest, 0);
    return result;
}

String sha512_hex(const void* data, size_t len) {
    SHA512Digest d = sha512_hash(data, len);
    return bytes_to_hex(d.digest, 64);
}

struct SHA512Context {
    u64 state[8];
    u64 bit_count;
    u8  buffer[128];
    size_t buf_len;
};

SHA512Context* sha512_ctx_create(void) {
    SHA512Context* ctx = (SHA512Context*)kmm_v4_malloc(sizeof(SHA512Context));
    if (!ctx) return NULL;
    sha512_init(ctx->state, &ctx->bit_count, 0);
    ctx->buf_len = 0;
    return ctx;
}

void sha512_ctx_update(SHA512Context* ctx, const void* data, size_t len) {
    if (!ctx || !data || len == 0) return;
    sha512_update_core(ctx->state, &ctx->bit_count, ctx->buffer, &ctx->buf_len, data, len);
}

SHA512Digest sha512_ctx_final(SHA512Context* ctx) {
    SHA512Digest result;
    if (!ctx) {
        memset(&result, 0, sizeof(result));
        return result;
    }
    sha512_final_core(ctx->state, ctx->bit_count, ctx->buffer, ctx->buf_len, result.digest, 0);
    return result;
}

void sha512_ctx_destroy(SHA512Context* ctx) {
    if (!ctx) return;
    memset(ctx, 0, sizeof(SHA512Context));
    kmm_v4_free(ctx);
}

/* ============================================================
 * CRC-64 (ISO 3309)
 * Reflected polynomial 0xD800000000000000 (x^64 + x^4 + x^3 + x + 1)
 * init = 0xFFFFFFFFFFFFFFFF, xorout = 0xFFFFFFFFFFFFFFFF, refin/refout = true
 * ============================================================ */

#define CRC64_POLY 0xD800000000000000ULL
#define CRC64_INIT 0xFFFFFFFFFFFFFFFFULL

u64 crc64_compute(const void* data, size_t len) {
    const u8* p = (const u8*)data;
    u64 crc = CRC64_INIT;
    size_t i;
    int j;
    for (i = 0; i < len; i++) {
        crc ^= (u64)p[i];
        for (j = 0; j < 8; j++) {
            if (crc & 1ULL) {
                crc = (crc >> 1) ^ CRC64_POLY;
            } else {
                crc >>= 1;
            }
        }
    }
    return crc ^ CRC64_INIT;
}

CRC64Digest crc64_hash(const void* data, size_t len) {
    CRC64Digest d;
    d.hash = crc64_compute(data, len);
    return d;
}

String crc64_hex(const void* data, size_t len) {
    static const char hexchars[] = "0123456789abcdef";
    u64 h = crc64_compute(data, len);
    String out;
    int i;
    out = (String)kmm_v4_malloc(17);
    if (!out) return NULL;
    for (i = 0; i < 16; i++) {
        out[i] = hexchars[(h >> (60 - i * 4)) & 0xF];
    }
    out[16] = '\0';
    return out;
}

/* ============================================================
 * HMAC-SHA512 (RFC 2104)
 * Block size for SHA-512 is 128 bytes.
 * ============================================================ */

#define SHA512_BLOCK_SIZE 128

void hmac_sha512(const void* key, size_t key_len,
                 const void* data, size_t data_len,
                 u8 out[64]) {
    u8 k[SHA512_BLOCK_SIZE];
    u8 k_ipad[SHA512_BLOCK_SIZE];
    u8 k_opad[SHA512_BLOCK_SIZE];
    u8 inner[64];
    SHA512Digest tmp;
    SHA512Context* ctx;
    int i;

    memset(k, 0, SHA512_BLOCK_SIZE);
    if (key && key_len > 0) {
        if (key_len > SHA512_BLOCK_SIZE) {
            tmp = sha512_hash(key, key_len);
            memcpy(k, tmp.digest, 64);
        } else {
            memcpy(k, key, key_len);
        }
    }
    for (i = 0; i < SHA512_BLOCK_SIZE; i++) {
        k_ipad[i] = k[i] ^ 0x36;
        k_opad[i] = k[i] ^ 0x5c;
    }

    /* inner = H((K ^ ipad) || data) */
    ctx = sha512_ctx_create();
    sha512_ctx_update(ctx, k_ipad, SHA512_BLOCK_SIZE);
    if (data && data_len > 0) {
        sha512_ctx_update(ctx, data, data_len);
    }
    tmp = sha512_ctx_final(ctx);
    memcpy(inner, tmp.digest, 64);
    sha512_ctx_destroy(ctx);

    /* out = H((K ^ opad) || inner) */
    ctx = sha512_ctx_create();
    sha512_ctx_update(ctx, k_opad, SHA512_BLOCK_SIZE);
    sha512_ctx_update(ctx, inner, 64);
    tmp = sha512_ctx_final(ctx);
    memcpy(out, tmp.digest, 64);
    sha512_ctx_destroy(ctx);

    /* secure zero of key-derived material */
    memset(k, 0, SHA512_BLOCK_SIZE);
    memset(k_ipad, 0, SHA512_BLOCK_SIZE);
    memset(k_opad, 0, SHA512_BLOCK_SIZE);
    memset(inner, 0, 64);
}

String hmac_sha512_hex(const void* key, size_t key_len,
                       const void* data, size_t data_len) {
    u8 out[64];
    hmac_sha512(key, key_len, data, data_len, out);
    return bytes_to_hex(out, 64);
}

/* ============================================================
 * PBKDF2-HMAC-SHA512 (RFC 8018)
 * hLen = 64, PRF = HMAC-SHA512
 * ============================================================ */

void pbkdf2_sha512(const void* password, size_t pw_len,
                   const void* salt, size_t salt_len,
                   int iterations, u8* out, size_t out_len) {
    const size_t hLen = 64;
    size_t blocks;
    size_t b, j, it, offset, copy_len;
    u8* salt_buf;
    u8 U[64];
    u8 T[64];

    if (!out || out_len == 0) return;
    if (iterations <= 0) iterations = 1;
    blocks = (out_len + hLen - 1) / hLen;

    salt_buf = (u8*)kmm_v4_malloc(salt_len + 4);
    if (!salt_buf) return;
    if (salt && salt_len > 0) {
        memcpy(salt_buf, salt, salt_len);
    }

    for (b = 1; b <= blocks; b++) {
        /* INT_32_BE(b) */
        salt_buf[salt_len + 0] = (u8)(b >> 24);
        salt_buf[salt_len + 1] = (u8)(b >> 16);
        salt_buf[salt_len + 2] = (u8)(b >> 8);
        salt_buf[salt_len + 3] = (u8)b;

        /* U_1 = HMAC(password, salt || INT_32_BE(b)) */
        hmac_sha512(password, pw_len, salt_buf, salt_len + 4, U);
        memcpy(T, U, hLen);

        /* U_2 .. U_c */
        for (it = 1; it < (size_t)iterations; it++) {
            hmac_sha512(password, pw_len, U, hLen, U);
            for (j = 0; j < hLen; j++) {
                T[j] ^= U[j];
            }
        }

        offset = (b - 1) * hLen;
        copy_len = (offset + hLen > out_len) ? (out_len - offset) : hLen;
        memcpy(out + offset, T, copy_len);
    }

    memset(U, 0, sizeof(U));
    memset(T, 0, sizeof(T));
    kmm_v4_free(salt_buf);
}

/* ============================================================
 * Generic hash dispatch by algorithm name
 * ============================================================ */

String hash_compute(const char* algorithm, const void* data, size_t len) {
    if (!algorithm) return NULL;
    if (strcmp(algorithm, "sha1") == 0)   return sha1_hex(data, len);
    if (strcmp(algorithm, "sha384") == 0) return sha384_hex(data, len);
    if (strcmp(algorithm, "sha512") == 0) return sha512_hex(data, len);
    if (strcmp(algorithm, "crc64") == 0)  return crc64_hex(data, len);
    return NULL;
}
