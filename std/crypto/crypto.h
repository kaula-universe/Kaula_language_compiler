#ifndef STD_CRYPTO_CRYPTO_H
#define STD_CRYPTO_CRYPTO_H

#include "../base/types.h"
#include "../string/string.h"

// MD5
typedef struct {
    u32 state[4];
    u64 count;
    u8 buffer[64];
} MD5Context;

extern void md5_init(MD5Context* ctx);
extern void md5_update(MD5Context* ctx, const u8* data, size_t len);
extern void md5_final(MD5Context* ctx, u8* digest);
extern String md5_string(const String input);
extern String md5_file(const String path);

// SHA-1
typedef struct {
    u32 state[5];
    u64 count;
    u8 buffer[64];
} SHA1Context;

extern void sha1_init(SHA1Context* ctx);
extern void sha1_update(SHA1Context* ctx, const u8* data, size_t len);
extern void sha1_final(SHA1Context* ctx, u8* digest);
extern String sha1_string(const String input);

// SHA-256
typedef struct {
    u32 state[8];
    u64 count;
    u8 buffer[64];
} SHA256Context;

extern void sha256_init(SHA256Context* ctx);
extern void sha256_update(SHA256Context* ctx, const u8* data, size_t len);
extern void sha256_final(SHA256Context* ctx, u8* digest);
extern String sha256_string(const String input);
extern String sha256_file(const String path);

// SHA-512
typedef struct {
    u64 state[8];
    u64 count[2];
    u8 buffer[128];
} SHA512Context;

extern void sha512_init(SHA512Context* ctx);
extern void sha512_update(SHA512Context* ctx, const u8* data, size_t len);
extern void sha512_final(SHA512Context* ctx, u8* digest);
extern String sha512_string(const String input);

// Base64
extern String base64_encode(const u8* data, size_t len);
extern u8* base64_decode(const String input, size_t* out_len);

// AES-128
typedef struct {
    u8 key[16];
    u8 round_keys[176];
} AES128Context;

extern void aes128_init(AES128Context* ctx, const u8* key);
extern void aes128_encrypt(AES128Context* ctx, const u8* input, u8* output);
extern void aes128_decrypt(AES128Context* ctx, const u8* input, u8* output);

// AES-256
typedef struct {
    u8 key[32];
    u8 round_keys[240];
} AES256Context;

extern void aes256_init(AES256Context* ctx, const u8* key);
extern void aes256_encrypt(AES256Context* ctx, const u8* input, u8* output);
extern void aes256_decrypt(AES256Context* ctx, const u8* input, u8* output);

// HMAC
extern String hmac_md5(const u8* key, size_t key_len, const u8* data, size_t data_len);
extern String hmac_sha256(const u8* key, size_t key_len, const u8* data, size_t data_len);

// CRC32
extern u32 crc32(const u8* data, size_t len);

#endif // STD_CRYPTO_CRYPTO_H
