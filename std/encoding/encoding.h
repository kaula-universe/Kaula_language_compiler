#pragma once
#include "../base/types.h"
#include "../string/string.h"

// Hex encoding
String hex_encode(const void* data, size_t len);
void*  hex_decode(const char* hex, size_t* out_len);
String hex_encode_upper(const void* data, size_t len);

// Base32 encoding
String base32_encode(const void* data, size_t len);
void*  base32_decode(const char* encoded, size_t* out_len);

// URL encoding/decoding
String url_encode(const char* str);
String url_decode(const char* str);
String url_encode_component(const char* str);
String url_decode_component(const char* str);

// Base64 (补充：如果原模块没有的话)
String base64_encode(const void* data, size_t len);
void*  base64_decode(const char* encoded, size_t* out_len);

// Quoted-Printable
String qp_encode(const char* str);
String qp_decode(const char* str);

// Percent-encoding (RFC 3986)
String percent_encode(const char* str, const char* reserved_chars);
String percent_decode(const char* str);

// Pascal string encoding (length-prefixed)
String pascal_encode(const char* str);
String pascal_decode(const char* encoded);
