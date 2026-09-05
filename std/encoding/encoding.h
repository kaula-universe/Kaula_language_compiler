#pragma once
#include "../base/types.h"
#include "../string/string.h"
#include <stdint.h>
#include <stdbool.h>
#include <time.h>

// ==================== 字符编码类型 ====================
typedef enum {
    ENCODING_AUTO = 0,    // 自动检测
    ENCODING_UTF8,        // UTF-8 (默认)
    ENCODING_UTF16,       // UTF-16
    ENCODING_UTF32,       // UTF-32
    ENCODING_GBK,         // GBK (中文Windows)
    ENCODING_GB2312,      // GB2312
    ENCODING_BIG5,        // Big5 (繁体中文)
    ENCODING_SHIFT_JIS,   // Shift-JIS (日文)
    ENCODING_EUC_KR,      // EUC-KR (韩文)
    ENCODING_ISO_8859_1,  // Latin-1
    ENCODING_WINDOWS_1252 // Windows-1252
} TextEncoding;

// ==================== 字符编码转换 API ====================
extern void enc_init(void);
extern void enc_cleanup(void);

extern void enc_set_encoding(TextEncoding encoding);
extern TextEncoding enc_get_encoding(void);
extern void enc_set_console_encoding(void);
extern const char* enc_get_encoding_name(TextEncoding encoding);

extern char* enc_convert_encoding(const char* input, TextEncoding from, TextEncoding to);
extern char* enc_to_utf8(const char* input, TextEncoding from_encoding);
extern char* enc_from_utf8(const char* input, TextEncoding to_encoding);
extern bool enc_is_utf8(const char* input);
extern bool enc_detect_encoding(const char* input, TextEncoding* out_encoding);

// ==================== Unicode UTF-8 工具 ====================
extern size_t enc_utf8_char_count(const char* str);
extern size_t enc_utf8_byte_length(const char* str, size_t char_index);
extern char* enc_utf8_substring(const char* str, size_t start, size_t count);
extern size_t enc_utf8_strlen(const char* str);
extern int enc_utf8_encode(uint32_t codepoint, char* out_buffer);
extern int enc_utf8_decode(const char* input, uint32_t* out_codepoint);
extern char* enc_utf8_normalize(const char* input);

// ==================== 平台控制台适配 ====================
extern void enc_setup_console(void);
extern void enc_fix_console_output(void);

// ==================== 数据编码 ====================

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
