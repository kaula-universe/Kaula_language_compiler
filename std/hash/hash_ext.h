#pragma once
#include "../base/types.h"
#include "../string/string.h"

typedef struct {
    u8 digest[20];
} SHA1Digest;

typedef struct {
    u8 digest[48];
} SHA384Digest;

typedef struct {
    u8 digest[64];
} SHA512Digest;

typedef struct {
    u64 hash;
} CRC64Digest;

typedef struct {
    u8 digest[16];
} MD5Digest;

SHA1Digest  sha1_hash(const void* data, size_t len);
String      sha1_hex(const void* data, size_t len);

SHA384Digest sha384_hash(const void* data, size_t len);
String       sha384_hex(const void* data, size_t len);

SHA512Digest sha512_hash(const void* data, size_t len);
String       sha512_hex(const void* data, size_t len);

CRC64Digest crc64_hash(const void* data, size_t len);
String      crc64_hex(const void* data, size_t len);
u64         crc64_compute(const void* data, size_t len);

// HMAC-SHA512
void  hmac_sha512(const void* key, size_t key_len,
                  const void* data, size_t data_len,
                  u8 out[64]);
String hmac_sha512_hex(const void* key, size_t key_len,
                       const void* data, size_t data_len);

// PBKDF2-HMAC-SHA512
void  pbkdf2_sha512(const void* password, size_t pw_len,
                    const void* salt, size_t salt_len,
                    int iterations, u8* out, size_t out_len);

// Hash context for streaming.
// The streaming contexts are named HashSHA1Context / HashSHA512Context on
// purpose: crypto.h already typedefs SHA1Context / SHA512Context to its own
// (differently laid out) anonymous structs, so sharing the names made
// "import std.hash" + "import std.crypto" fail with
// "typedef redefinition with different types".
typedef struct HashSHA1Context HashSHA1Context;
typedef struct HashSHA512Context HashSHA512Context;

HashSHA1Context*  sha1_ctx_create(void);
void          sha1_ctx_update(HashSHA1Context* ctx, const void* data, size_t len);
SHA1Digest    sha1_ctx_final(HashSHA1Context* ctx);
void          sha1_ctx_destroy(HashSHA1Context* ctx);

HashSHA512Context* sha512_ctx_create(void);
void           sha512_ctx_update(HashSHA512Context* ctx, const void* data, size_t len);
SHA512Digest   sha512_ctx_final(HashSHA512Context* ctx);
void           sha512_ctx_destroy(HashSHA512Context* ctx);

// Generic hash dispatch
String hash_compute(const char* algorithm, const void* data, size_t len);
