#pragma once
#include "../base/types.h"
#include "../memory/memory.h"
#include "../string/string.h"
#include "../net/net.h"

typedef struct TLSContext {
    Socket* socket;
    void* ssl;
    bool_t connected;
} TLSContext;

typedef enum {
    TLS_VERSION_TLS10 = 0x0301,
    TLS_VERSION_TLS11 = 0x0302,
    TLS_VERSION_TLS12 = 0x0303,
    TLS_VERSION_TLS13 = 0x0304,
} TLSVersion;

typedef enum {
    TLS_CIPHER_AES128_GCM_SHA256 = 0x1301,
    TLS_CIPHER_AES256_GCM_SHA384 = 0x1302,
    TLS_CIPHER_CHACHA20_POLY1305_SHA256 = 0x1303,
} TLSCipher;

TLSContext* tls_client_create(Socket* socket);
TLSContext* tls_server_create(Socket* socket);
void tls_destroy(TLSContext* ctx);

bool_t tls_connect(TLSContext* ctx, const char* hostname);
bool_t tls_accept(TLSContext* ctx);

ssize_t tls_read(TLSContext* ctx, u8* buffer, size_t size);
ssize_t tls_write(TLSContext* ctx, const u8* buffer, size_t size);

void tls_shutdown(TLSContext* ctx);

bool_t tls_set_version(TLSContext* ctx, TLSVersion version);
bool_t tls_set_cipher(TLSContext* ctx, TLSCipher cipher);

String tls_get_peer_certificate(TLSContext* ctx);
String tls_get_cipher_name(TLSContext* ctx);
TLSVersion tls_get_version(TLSContext* ctx);

bool_t tls_load_certificate(TLSContext* ctx, const char* cert_file, const char* key_file);
bool_t tls_load_ca_certificates(TLSContext* ctx, const char* ca_file);

bool_t tls_verify_certificate(TLSContext* ctx);