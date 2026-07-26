#include "crypto.h"
#include "../memory/memory.h"
#include <string.h>
#include <stdio.h>

// MD5 实现
#define MD5_F(x, y, z) ((x & y) | (~x & z))
#define MD5_G(x, y, z) ((x & z) | (y & ~z))
#define MD5_H(x, y, z) (x ^ y ^ z)
#define MD5_I(x, y, z) (y ^ (x | ~z))
#define MD5_ROTATE_LEFT(x, n) ((x << n) | (x >> (32 - n)))

#define MD5_FF(a, b, c, d, x, s, ac) { a += MD5_F(b, c, d) + x + ac; a = MD5_ROTATE_LEFT(a, s) + b; }
#define MD5_GG(a, b, c, d, x, s, ac) { a += MD5_G(b, c, d) + x + ac; a = MD5_ROTATE_LEFT(a, s) + b; }
#define MD5_HH(a, b, c, d, x, s, ac) { a += MD5_H(b, c, d) + x + ac; a = MD5_ROTATE_LEFT(a, s) + b; }
#define MD5_II(a, b, c, d, x, s, ac) { a += MD5_I(b, c, d) + x + ac; a = MD5_ROTATE_LEFT(a, s) + b; }

static void md5_transform(u32 state[4], const u8 block[64]) {
    u32 a = state[0], b = state[1], c = state[2], d = state[3];
    u32 x[16];
    for (int i = 0; i < 16; i++) x[i] = block[i*4] | (block[i*4+1] << 8) | (block[i*4+2] << 16) | (block[i*4+3] << 24);
    
    MD5_FF(a, b, c, d, x[0], 7, 0xd76aa478); MD5_FF(d, a, b, c, x[1], 12, 0xe8c7b756);
    MD5_FF(c, d, a, b, x[2], 17, 0x242070db); MD5_FF(b, c, d, a, x[3], 22, 0xc1bdceee);
    MD5_FF(a, b, c, d, x[4], 7, 0xf57c0faf); MD5_FF(d, a, b, c, x[5], 12, 0x4787c62a);
    MD5_FF(c, d, a, b, x[6], 17, 0xa8304613); MD5_FF(b, c, d, a, x[7], 22, 0xfd469501);
    MD5_FF(a, b, c, d, x[8], 7, 0x698098d8); MD5_FF(d, a, b, c, x[9], 12, 0x8b44f7af);
    MD5_FF(c, d, a, b, x[10], 17, 0xffff5bb1); MD5_FF(b, c, d, a, x[11], 22, 0x895cd7be);
    MD5_FF(a, b, c, d, x[12], 7, 0x6b901122); MD5_FF(d, a, b, c, x[13], 12, 0xfd987193);
    MD5_FF(c, d, a, b, x[14], 17, 0xa679438e); MD5_FF(b, c, d, a, x[15], 22, 0x49b40821);
    
    MD5_GG(a, b, c, d, x[1], 5, 0xf61e2562); MD5_GG(d, a, b, c, x[6], 9, 0xc040b340);
    MD5_GG(c, d, a, b, x[11], 14, 0x265e5a51); MD5_GG(b, c, d, a, x[0], 20, 0xe9b6c7aa);
    MD5_GG(a, b, c, d, x[5], 5, 0xd62f105d); MD5_GG(d, a, b, c, x[10], 9, 0x02441453);
    MD5_GG(c, d, a, b, x[15], 14, 0xd8a1e681); MD5_GG(b, c, d, a, x[4], 20, 0xe7d3fbc8);
    MD5_GG(a, b, c, d, x[9], 5, 0x21e1cde6); MD5_GG(d, a, b, c, x[14], 9, 0xc33707d6);
    MD5_GG(c, d, a, b, x[3], 14, 0xf4d50d87); MD5_GG(b, c, d, a, x[8], 20, 0x455a14ed);
    MD5_GG(a, b, c, d, x[13], 5, 0xa9e3e905); MD5_GG(d, a, b, c, x[2], 9, 0xfcefa3f8);
    MD5_GG(c, d, a, b, x[7], 14, 0x676f02d9); MD5_GG(b, c, d, a, x[12], 20, 0x8d2a4c8a);
    
    MD5_HH(a, b, c, d, x[5], 4, 0xfffa3942); MD5_HH(d, a, b, c, x[8], 11, 0x8771f681);
    MD5_HH(c, d, a, b, x[11], 16, 0x6d9d6122); MD5_HH(b, c, d, a, x[14], 23, 0xfde5380c);
    MD5_HH(a, b, c, d, x[1], 4, 0xa4beea44); MD5_HH(d, a, b, c, x[4], 11, 0x4bdecfa9);
    MD5_HH(c, d, a, b, x[7], 16, 0xf6bb4b60); MD5_HH(b, c, d, a, x[10], 23, 0xbebfbc70);
    MD5_HH(a, b, c, d, x[13], 4, 0x289b7ec6); MD5_HH(d, a, b, c, x[0], 11, 0xeaa127fa);
    MD5_HH(c, d, a, b, x[3], 16, 0xd4ef3085); MD5_HH(b, c, d, a, x[6], 23, 0x04881d05);
    MD5_HH(a, b, c, d, x[9], 4, 0xd9d4d039); MD5_HH(d, a, b, c, x[12], 11, 0xe6db99e5);
    MD5_HH(c, d, a, b, x[15], 16, 0x1fa27cf8); MD5_HH(b, c, d, a, x[2], 23, 0xc4ac5665);
    
    MD5_II(a, b, c, d, x[0], 6, 0xf4292244); MD5_II(d, a, b, c, x[7], 10, 0x432aff97);
    MD5_II(c, d, a, b, x[14], 15, 0xab9423a7); MD5_II(b, c, d, a, x[5], 21, 0xfc93a039);
    MD5_II(a, b, c, d, x[12], 6, 0x655b59c3); MD5_II(d, a, b, c, x[3], 10, 0x8f0ccc92);
    MD5_II(c, d, a, b, x[10], 15, 0xffeff47d); MD5_II(b, c, d, a, x[1], 21, 0x85845dd1);
    MD5_II(a, b, c, d, x[8], 6, 0x6fa87e4f); MD5_II(d, a, b, c, x[15], 10, 0xfe2ce6e0);
    MD5_II(c, d, a, b, x[6], 15, 0xa3014314); MD5_II(b, c, d, a, x[13], 21, 0x4e0811a1);
    MD5_II(a, b, c, d, x[4], 6, 0xf7537e82); MD5_II(d, a, b, c, x[11], 10, 0xbd3af235);
    MD5_II(c, d, a, b, x[2], 15, 0x2ad7d2bb); MD5_II(b, c, d, a, x[9], 21, 0xeb86d391);
    
    state[0] += a; state[1] += b; state[2] += c; state[3] += d;
}

void md5_init(MD5Context* ctx) {
    ctx->count = 0;
    ctx->state[0] = 0x67452301; ctx->state[1] = 0xefcdab89;
    ctx->state[2] = 0x98badcfe; ctx->state[3] = 0x10325476;
}

void md5_update(MD5Context* ctx, const u8* data, size_t len) {
    size_t index = (size_t)(ctx->count % 64);
    ctx->count += len;
    size_t i = 0;
    if (index) {
        size_t part = 64 - index;
        if (len < part) { memcpy(ctx->buffer + index, data, len); return; }
        memcpy(ctx->buffer + index, data, part);
        md5_transform(ctx->state, ctx->buffer);
        i = part;
    }
    for (; i + 64 <= len; i += 64) md5_transform(ctx->state, data + i);
    memcpy(ctx->buffer, data + i, len - i);
}

void md5_final(MD5Context* ctx, u8* digest) {
    u8 padding[64] = { 0x80 };
    u64 bits = ctx->count * 8;
    size_t index = ctx->count % 64;
    size_t pad_len = (index < 56) ? (56 - index) : (120 - index);
    md5_update(ctx, padding, pad_len);
    md5_update(ctx, (const u8*)&bits, 8);
    memcpy(digest, ctx->state, 16);
}

String md5_string(const String input) {
    MD5Context ctx; u8 digest[16];
    md5_init(&ctx); md5_update(&ctx, (const u8*)input.ptr, input.len); md5_final(&ctx, digest);
    char* buf = (char*)kmm_v4_malloc(33);
    for (int i = 0; i < 16; i++) sprintf(buf + i*2, "%02x", digest[i]);
    buf[32] = '\0';
    String result = {32, buf}; return result;
}

String md5_file(const String path) {
    FILE* f = fopen(path.ptr, "rb"); if (!f) return (String){0, NULL};
    MD5Context ctx; md5_init(&ctx); u8 buf[4096]; size_t n;
    while ((n = fread(buf, 1, 4096, f)) > 0) md5_update(&ctx, buf, n);
    fclose(f); u8 digest[16]; md5_final(&ctx, digest);
    char* hex = (char*)kmm_v4_malloc(33);
    for (int i = 0; i < 16; i++) sprintf(hex + i*2, "%02x", digest[i]);
    hex[32] = '\0';
    String result = {32, hex}; return result;
}

// SHA-256 实现
#define SHA256_ROTR(x, n) ((x >> n) | (x << (32 - n)))
#define SHA256_CH(x, y, z) ((x & y) ^ (~x & z))
#define SHA256_MAJ(x, y, z) ((x & y) ^ (x & z) ^ (y & z))
#define SHA256_EP0(x) (SHA256_ROTR(x, 2) ^ SHA256_ROTR(x, 13) ^ SHA256_ROTR(x, 22))
#define SHA256_EP1(x) (SHA256_ROTR(x, 6) ^ SHA256_ROTR(x, 11) ^ SHA256_ROTR(x, 25))
#define SHA256_SIG0(x) (SHA256_ROTR(x, 7) ^ SHA256_ROTR(x, 18) ^ (x >> 3))
#define SHA256_SIG1(x) (SHA256_ROTR(x, 17) ^ SHA256_ROTR(x, 19) ^ (x >> 10))

static const u32 sha256_k[64] = {
    0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
    0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
    0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
    0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
    0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
    0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
    0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
    0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2
};

void sha256_init(SHA256Context* ctx) {
    ctx->count = 0;
    ctx->state[0] = 0x6a09e667; ctx->state[1] = 0xbb67ae85;
    ctx->state[2] = 0x3c6ef372; ctx->state[3] = 0xa54ff53a;
    ctx->state[4] = 0x510e527f; ctx->state[5] = 0x9b05688c;
    ctx->state[6] = 0x1f83d9ab; ctx->state[7] = 0x5be0cd19;
}

void sha256_update(SHA256Context* ctx, const u8* data, size_t len) {
    size_t index = ctx->count % 64; ctx->count += len; size_t i = 0;
    if (index) { size_t part = 64 - index; if (len < part) { memcpy(ctx->buffer + index, data, len); return; } memcpy(ctx->buffer + index, data, part); ctx->count = ctx->count; }
    for (; i + 64 <= len; i += 64) {
        u32 w[64]; for (int j = 0; j < 16; j++) w[j] = (data[i+j*4] << 24) | (data[i+j*4+1] << 16) | (data[i+j*4+2] << 8) | data[i+j*4+3];
        for (int j = 16; j < 64; j++) w[j] = SHA256_SIG1(w[j-2]) + w[j-7] + SHA256_SIG0(w[j-15]) + w[j-16];
        u32 a = ctx->state[0], b = ctx->state[1], c = ctx->state[2], d = ctx->state[3];
        u32 e = ctx->state[4], f = ctx->state[5], g = ctx->state[6], h = ctx->state[7];
        for (int j = 0; j < 64; j++) {
            u32 t1 = h + SHA256_EP1(e) + SHA256_CH(e, f, g) + sha256_k[j] + w[j];
            u32 t2 = SHA256_EP0(a) + SHA256_MAJ(a, b, c);
            h = g; g = f; f = e; e = d + t1; d = c; c = b; b = a; a = t1 + t2;
        }
        ctx->state[0] += a; ctx->state[1] += b; ctx->state[2] += c; ctx->state[3] += d;
        ctx->state[4] += e; ctx->state[5] += f; ctx->state[6] += g; ctx->state[7] += h;
    }
    if (i < len) memcpy(ctx->buffer, data + i, len - i);
}

void sha256_final(SHA256Context* ctx, u8* digest) {
    u8 padding[128] = { 0x80 }; u64 bits = ctx->count * 8;
    size_t index = ctx->count % 64, pad_len = (index < 56) ? (56 - index) : (120 - index);
    sha256_update(ctx, padding, pad_len);
    u64 bits_big = 0; for (int i = 0; i < 8; i++) bits_big |= ((bits >> (56 - i*8)) & 0xFF) << (i*8);
    sha256_update(ctx, (const u8*)&bits_big, 8);
    for (int i = 0; i < 8; i++) { digest[i*4] = (ctx->state[i] >> 24) & 0xFF; digest[i*4+1] = (ctx->state[i] >> 16) & 0xFF; digest[i*4+2] = (ctx->state[i] >> 8) & 0xFF; digest[i*4+3] = ctx->state[i] & 0xFF; }
}

String sha256_string(const String input) {
    SHA256Context ctx; u8 digest[32];
    sha256_init(&ctx); sha256_update(&ctx, (const u8*)input.ptr, input.len); sha256_final(&ctx, digest);
    char* buf = (char*)kmm_v4_malloc(65); String result;
    for (int i = 0; i < 32; i++) sprintf(buf + i*2, "%02x", digest[i]);
    buf[64] = '\0'; result = (String){.len = 64, .ptr = buf}; return result;
}

String sha256_file(const String path) {
    FILE* f = fopen(path.ptr, "rb"); if (!f) return STRING_EMPTY;
    SHA256Context ctx; sha256_init(&ctx); u8 buf[4096]; size_t n;
    while ((n = fread(buf, 1, 4096, f)) > 0) sha256_update(&ctx, buf, n);
    fclose(f); u8 digest[32]; sha256_final(&ctx, digest);
    char* hex = (char*)kmm_v4_malloc(65); String result;
    for (int i = 0; i < 32; i++) sprintf(hex + i*2, "%02x", digest[i]);
    hex[64] = '\0'; result = (String){.len = 64, .ptr = hex}; return result;
}

// Base64 实现
static const char base64_chars[] = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";

String base64_encode(const u8* data, size_t len) {
    char* buf = (char*)kmm_v4_malloc((len + 2) / 3 * 4 + 1); String result;
    size_t i = 0, j = 0;
    while (i < len) {
        u32 octet_a = i < len ? data[i++] : 0;
        u32 octet_b = i < len ? data[i++] : 0;
        u32 octet_c = i < len ? data[i++] : 0;
        u32 triple = (octet_a << 16) | (octet_b << 8) | octet_c;
        buf[j++] = base64_chars[(triple >> 18) & 0x3F];
        buf[j++] = base64_chars[(triple >> 12) & 0x3F];
        buf[j++] = base64_chars[(triple >> 6) & 0x3F];
        buf[j++] = base64_chars[triple & 0x3F];
    }
    if (len % 3 == 1) { buf[j-2] = '='; buf[j-1] = '='; }
    else if (len % 3 == 2) { buf[j-1] = '='; }
    buf[j] = '\0'; result = (String){.len = j, .ptr = buf}; return result;
}

u8* base64_decode(const String input, size_t* out_len) {
    size_t len = input.len; if (len % 4 != 0) return NULL;
    size_t out_len_val = len / 4 * 3; if (len > 0 && input.ptr[len-1] == '=') out_len_val--; if (len > 1 && input.ptr[len-2] == '=') out_len_val--;
    u8* output = (u8*)kmm_v4_malloc(out_len_val + 1); size_t i = 0, j = 0;
    while (i < len) {
        u32 sextet_a = input.ptr[i] == '=' ? 0 : (u32)(strchr(base64_chars, input.ptr[i]) - base64_chars); i++;
        u32 sextet_b = input.ptr[i] == '=' ? 0 : (u32)(strchr(base64_chars, input.ptr[i]) - base64_chars); i++;
        u32 sextet_c = input.ptr[i] == '=' ? 0 : (u32)(strchr(base64_chars, input.ptr[i]) - base64_chars); i++;
        u32 sextet_d = input.ptr[i] == '=' ? 0 : (u32)(strchr(base64_chars, input.ptr[i]) - base64_chars); i++;
        u32 triple = (sextet_a << 18) | (sextet_b << 12) | (sextet_c << 6) | sextet_d;
        if (j < out_len_val) output[j++] = (triple >> 16) & 0xFF;
        if (j < out_len_val) output[j++] = (triple >> 8) & 0xFF;
        if (j < out_len_val) output[j++] = triple & 0xFF;
    }
    if (out_len) *out_len = out_len_val;
    return output;
}

// AES-128 简化实现 (ECB mode)
static const u8 aes_sbox[256] = {
    0x63,0x7c,0x77,0x7b,0xf2,0x6b,0x6f,0xc5,0x30,0x01,0x67,0x2b,0xfe,0xd7,0xab,0x76,
    0xca,0x82,0xc9,0x7d,0xfa,0x59,0x47,0xf0,0xad,0xd4,0xa2,0xaf,0x9c,0xa4,0x72,0xc0,
    0xb7,0xfd,0x93,0x26,0x36,0x3f,0xf7,0xcc,0x34,0xa5,0xe5,0xf1,0x71,0xd8,0x31,0x15,
    0x04,0xc7,0x23,0xc3,0x18,0x96,0x05,0x9a,0x07,0x12,0x80,0xe2,0xeb,0x27,0xb2,0x75,
    0x09,0x83,0x2c,0x1a,0x1b,0x6e,0x5a,0xa0,0x52,0x3b,0xd6,0xb3,0x29,0xe3,0x2f,0x84,
    0x53,0xd1,0x00,0xed,0x20,0xfc,0xb1,0x5b,0x6a,0xcb,0xbe,0x39,0x4a,0x4c,0x58,0xcf,
    0xd0,0xef,0xaa,0xfb,0x43,0x4d,0x33,0x85,0x45,0xf9,0x02,0x7f,0x50,0x3c,0x9f,0xa8,
    0x51,0xa3,0x40,0x8f,0x92,0x9d,0x38,0xf5,0xbc,0xb6,0xda,0x21,0x10,0xff,0xf3,0xd2,
    0xcd,0x0c,0x13,0xec,0x5f,0x97,0x44,0x17,0xc4,0xa7,0x7e,0x3d,0x64,0x5d,0x19,0x73,
    0x60,0x81,0x4f,0xdc,0x22,0x2a,0x90,0x88,0x46,0xee,0xb8,0x14,0xde,0x5e,0x0b,0xdb,
    0xe0,0x32,0x3a,0x0a,0x49,0x06,0x24,0x5c,0xc2,0xd3,0xac,0x62,0x91,0x95,0xe4,0x79,
    0xe7,0xc8,0x37,0x6d,0x8d,0xd5,0x4e,0xa9,0x6c,0x56,0xf4,0xea,0x65,0x7a,0xae,0x08,
    0xba,0x78,0x25,0x2e,0x1c,0xa6,0xb4,0xc6,0xe8,0xdd,0x74,0x1f,0x4b,0xbd,0x8b,0x8a,
    0x70,0x3e,0xb5,0x66,0x48,0x03,0xf6,0x0e,0x61,0x35,0x57,0xb9,0x86,0xc1,0x1d,0x9e,
    0xe1,0xf8,0x98,0x11,0x69,0xd9,0x8e,0x94,0x9b,0x1e,0x87,0xe9,0xce,0x55,0x28,0xdf,
    0x8c,0xa1,0x89,0x0d,0xbf,0xe6,0x42,0x68,0x41,0x99,0x2d,0x0f,0xb0,0x54,0xbb,0x16
};
static const u8 aes_rsbox[256] = {
    0x52,0x09,0x6a,0xd5,0x30,0x36,0xa5,0x38,0xbf,0x40,0xa3,0x9e,0x81,0xf3,0xd7,0xfb,
    0x7c,0xe3,0x39,0x82,0x9b,0x2f,0xff,0x87,0x34,0x8e,0x43,0x44,0xc4,0xde,0xe9,0xcb,
    0x54,0x7b,0x94,0x32,0xa6,0xc2,0x23,0x3d,0xee,0x4c,0x95,0x0b,0x42,0xfa,0xc3,0x4e,
    0x08,0x2e,0xa1,0x66,0x28,0xd9,0x24,0xb2,0x76,0x5b,0xa2,0x49,0x6d,0x8b,0xd1,0x25,
    0x72,0xf8,0xf6,0x64,0x86,0x68,0x98,0x16,0xd4,0xa4,0x5c,0xcc,0x5d,0x65,0xb6,0x92,
    0x6c,0x70,0x48,0x50,0xfd,0xed,0xb9,0xda,0x5e,0x15,0x46,0x57,0xa7,0x8d,0x9d,0x84,
    0x90,0xd8,0xab,0x00,0x8c,0xbc,0xd3,0x0a,0xf7,0xe4,0x58,0x05,0xb8,0xb3,0x45,0x06,
    0xd0,0x2c,0x1e,0x8f,0xca,0x3f,0x0f,0x02,0xc1,0xaf,0xbd,0x03,0x01,0x13,0x8a,0x6b,
    0x3a,0x91,0x11,0x41,0x4f,0x67,0xdc,0xea,0x97,0xf2,0xcf,0xce,0xf0,0xb4,0xe6,0x73,
    0x96,0xac,0x74,0x22,0xe7,0xad,0x35,0x85,0xe2,0xf9,0x37,0xe8,0x1c,0x75,0xdf,0x6e,
    0x47,0xf1,0x1a,0x71,0x1d,0x29,0xc5,0x89,0x6f,0xb7,0x62,0x0e,0xaa,0x18,0xbe,0x1b,
    0xfc,0x56,0x3e,0x4b,0xc6,0xd2,0x79,0x20,0x9a,0xdb,0xc0,0xfe,0x78,0xcd,0x5a,0xf4,
    0x1f,0xdd,0xa8,0x33,0x88,0x07,0xc7,0x31,0xb1,0x12,0x10,0x59,0x27,0x80,0xec,0x5f,
    0x60,0x51,0x7f,0xa9,0x19,0xb5,0x4a,0x0d,0x2d,0xe5,0x7a,0x9f,0x93,0xc9,0x9c,0xef,
    0xa0,0xe0,0x3b,0x4d,0xae,0x2a,0xf5,0xb0,0xc8,0xeb,0xbb,0x3c,0x83,0x53,0x99,0x61,
    0x17,0x2b,0x04,0x7e,0xba,0x77,0xd6,0x26,0xe1,0x69,0x14,0x63,0x55,0x21,0x0c,0x7d
};

void aes128_init(AES128Context* ctx, const u8* key) {
    memcpy(ctx->key, key, 16);
    for (int i = 0; i < 16; i++) ctx->round_keys[i] = key[i];
    for (int i = 16; i < 176; i++) {
        u8 temp = ctx->round_keys[i-1];
        if (i % 16 == 0) {
            u8 rot = ctx->round_keys[i-3]; ctx->round_keys[i-3] = ctx->round_keys[i-2];
            ctx->round_keys[i-2] = ctx->round_keys[i-1]; ctx->round_keys[i-1] = rot;
            temp = aes_sbox[ctx->round_keys[i-16]] ^ (i/16 == 1 ? 0x01 : (i/16 == 2 ? 0x02 : 0x04));
            ctx->round_keys[i-16] = rot; ctx->round_keys[i-15] = ctx->round_keys[i-14];
            ctx->round_keys[i-14] = ctx->round_keys[i-13]; ctx->round_keys[i-13] = ctx->round_keys[i-16];
        }
        ctx->round_keys[i] = ctx->round_keys[i-16] ^ temp;
    }
}

void aes128_encrypt(AES128Context* ctx, const u8* input, u8* output) {
    u8 state[16]; for (int i = 0; i < 16; i++) state[i] = input[i];
    for (int i = 0; i < 16; i++) state[i] ^= ctx->round_keys[i];
    for (int round = 1; round < 10; round++) {
        for (int i = 0; i < 16; i++) state[i] = aes_sbox[state[i]];
        u8 temp[16];
        for (int i = 0; i < 4; i++) { temp[i] = state[i]; temp[i+4] = state[i+4]; temp[i+8] = state[i+8]; temp[i+12] = state[i+12]; }
        state[1] = temp[1]; state[2] = temp[2]; state[3] = temp[3];
        state[5] = temp[9]; state[9] = temp[13]; state[13] = temp[5];
        state[6] = temp[10]; state[10] = temp[14]; state[14] = temp[6];
        state[7] = temp[15]; state[11] = temp[7]; state[15] = temp[11];
        for (int i = 0; i < 16; i++) state[i] ^= ctx->round_keys[round*16 + i];
    }
    memcpy(output, state, 16);
}

void aes128_decrypt(AES128Context* ctx, const u8* input, u8* output) {
    u8 state[16]; for (int i = 0; i < 16; i++) state[i] = input[i];
    for (int round = 9; round > 0; round--) {
        for (int i = 0; i < 16; i++) state[i] ^= ctx->round_keys[round*16 + i];
        for (int i = 0; i < 16; i++) state[i] = aes_rsbox[state[i]];
    }
    for (int i = 0; i < 16; i++) state[i] ^= ctx->round_keys[i];
    memcpy(output, state, 16);
}

// HMAC
String hmac_sha256(const u8* key, size_t key_len, const u8* data, size_t data_len) {
    SHA256Context ctx; u8 k[64] = {0}; u8 ipad[64], opad[64], hash[32];
    if (key_len > 64) { sha256_init(&ctx); sha256_update(&ctx, key, key_len); sha256_final(&ctx, k); }
    else memcpy(k, key, key_len);
    for (int i = 0; i < 64; i++) { ipad[i] = k[i] ^ 0x36; opad[i] = k[i] ^ 0x5c; }
    sha256_init(&ctx); sha256_update(&ctx, ipad, 64); sha256_update(&ctx, data, data_len); sha256_final(&ctx, hash);
    sha256_init(&ctx); sha256_update(&ctx, opad, 64); sha256_update(&ctx, hash, 32); sha256_final(&ctx, hash);
    char* buf = (char*)kmm_v4_malloc(65); String result;
    for (int i = 0; i < 32; i++) sprintf(buf + i*2, "%02x", hash[i]);
    buf[64] = '\0'; result = (String){.len = 64, .ptr = buf}; return result;
}

// CRC32
static const u32 crc32_table[256] = {
    0x00000000,0x77073096,0xEE0E612C,0x990951BA,0x076DC419,0x706AF48F,0xE963A535,0x9E6495A3,
    0x0EDB8832,0x79DCB8A4,0xE0D5E91E,0x97D2D988,0x09B64C2B,0x7EB17CBD,0xE7B82D07,0x90BF1D91,
    0x1DB71064,0x6AB020F2,0xF3B97148,0x84BE41DE,0x1ADAD47D,0x6DDDE4EB,0xF4D4B551,0x83D385C7,
    0x136C9856,0x646BA8C0,0xFD62F97A,0x8A65C9EC,0x14015C4F,0x63066CD9,0xFA0F3D63,0x8D080DF5,
    0x3B6E20C8,0x4C69105E,0xD56041E4,0xA2677172,0x3C03E4D1,0x4B04D447,0xD20D85FD,0xA50AB56B,
    0x35B5A8FA,0x42B2986C,0xDBBBC9D6,0xACBCF940,0x32D86CE3,0x45DF5C75,0xDCD60DCF,0xABD13D59,
    0x26D930AC,0x51DE003A,0xC8D75180,0xBFD06116,0x21B4F4B5,0x56B3C423,0xCFBA9599,0xB8BDA50F,
    0x2802B89E,0x5F058808,0xC60CD9B2,0xB10BE924,0x2F6F7C87,0x58684C11,0xC1611DAB,0xB6662D3D,
    0x76DC4190,0x01DB7106,0x98D220BC,0xEFD5102A,0x71B18589,0x06B6B51F,0x9FBFE4A5,0xE8B8D433,
    0x7807C9A2,0x0F00F934,0x9609A88E,0xE10E9818,0x7F6A0DBB,0x086D3D2D,0x91646C97,0xE6635C01,
    0x6B6B51F4,0x1C6C6162,0x856530D8,0xF262004E,0x6C0695ED,0x1B01A57B,0x8208F4C1,0xF50FC457,
    0x65B0D9C6,0x12B7E950,0x8BBEB8EA,0xFCB9887C,0x62DD1DDF,0x15DA2D49,0x8CD37CF3,0xFBD44C65,
    0x4DB26158,0x3AB551CE,0xA3BC0074,0xD4BB30E2,0x4ADFA541,0x3DD895D7,0xA4D1C46D,0xD3D6F4FB,
    0x4369E96A,0x346ED9FC,0xAD678846,0xDA60B8D0,0x44042D73,0x33031DE5,0xAA0A4C5F,0xDD0D7CC9,
    0x5005713C,0x270241AA,0xBE0B1010,0xC90C2086,0x5768B525,0x206F85B3,0xB966D409,0xCE61E49F,
    0x5EDEF90E,0x29D9C998,0xB0D09822,0xC7D7A8B4,0x59B33D17,0x2EB40D81,0xB7BD5C3B,0xC0BA6CAD,
    0xEDB88320,0x9ABFB3B6,0x03B6E20C,0x74B1D29A,0xEAD54739,0x9DD277AF,0x04DB2615,0x73DC1683,
    0xE3630B12,0x94643B84,0x0D6D6A3E,0x7A6A5AA8,0xE40ECF0B,0x9309FF9D,0x0A00AE27,0x7D079EB1,
    0xF00F9344,0x8708A3D2,0x1E01F268,0x6906C2FE,0xF762575D,0x806567CB,0x196C3671,0x6E6B06E7,
    0xFED41B76,0x89D32BE0,0x10DA7A5A,0x67DD4ACC,0xF9B9DF6F,0x8EBEEFF9,0x17B7BE43,0x60B08ED5,
    0xD6D6A3E8,0xA1D1937E,0x38D8C2C4,0x4FDFF252,0xD1BB67F1,0xA6BC5767,0x3FB506DD,0x48B2364B,
    0xD80D2BDA,0xAF0A1B4C,0x36034AF6,0x41047A60,0xDF60EFC3,0xA867DF55,0x316E8EEF,0x4669BE79,
    0xCB61B38C,0xBC66831A,0x256FD2A0,0x5268E236,0xCC0C7795,0xBB0B4703,0x220216B9,0x5505262F,
    0xC5BA3BBE,0xB2BD0B28,0x2BB45A92,0x5CB36A04,0xC2D7FFA7,0xB5D0CF31,0x2CD99E8B,0x5BDEAE1D,
    0x9B64C2B0,0xEC63F226,0x756AA39C,0x026D930A,0x9C0906A9,0xEB0E363F,0x72076785,0x05005713,
    0x95BF4A82,0xE2B87A14,0x7BB12BAE,0x0CB61B38,0x92D28E9B,0xE5D5BE0D,0x7CDCEFB7,0x0BDBDF21,
    0x86D3D2D4,0xF1D4E242,0x68DDB3F8,0x1FDA836E,0x81BE16CD,0xF6B9265B,0x6FB077E1,0x18B74777,
    0x88085AE6,0xFF0F6A70,0x66063BCA,0x11010B5C,0x8F659EFF,0xF862AE69,0x616BFFD3,0x166CCF45,
    0xA00AE278,0xD70DD2EE,0x4E048354,0x3903B3C2,0xA7672661,0xD06016F7,0x4969474D,0x3E6E77DB,
    0xAED16A4A,0xD9D65ADC,0x40DF0B66,0x37D83BF0,0xA9BCAE53,0xDEBB9EC5,0x47B2CF7F,0x30B5FFE9,
    0xBDBDF21C,0xCABAC28A,0x53B39330,0x24B4A3A6,0xBAD03605,0xCDD70693,0x54DE5729,0x23D967BF,
    0xB3667A2E,0xC4614AB8,0x5D681B02,0x2A6F2B94,0xB40BBE37,0xC30C8EA1,0x5A05DF1B,0x2D02EF8D
};

u32 crc32(const u8* data, size_t len) {
    u32 crc = 0xFFFFFFFF;
    for (size_t i = 0; i < len; i++) crc = crc32_table[(crc ^ data[i]) & 0xFF] ^ (crc >> 8);
    return crc ^ 0xFFFFFFFF;
}
