#include "encoding.h"
#include "../memory/memory.h"
#include "../string/string.h"
#include <string.h>

/* ============================================================
 * Internal helpers
 * ============================================================ */

static const char hex_lower[] = "0123456789abcdef";
static const char hex_upper[] = "0123456789ABCDEF";

static int hex_value(char c) {
    if (c >= '0' && c <= '9') return c - '0';
    if (c >= 'a' && c <= 'f') return c - 'a' + 10;
    if (c >= 'A' && c <= 'F') return c - 'A' + 10;
    return -1;
}

static int is_unreserved(char c) {
    return (c >= 'A' && c <= 'Z') ||
           (c >= 'a' && c <= 'z') ||
           (c >= '0' && c <= '9') ||
           c == '-' || c == '_' || c == '.' || c == '~';
}

static int char_in_set(char c, const char* set) {
    size_t i;
    if (!set) return 0;
    for (i = 0; set[i] != '\0'; i++) {
        if (set[i] == c) return 1;
    }
    return 0;
}

/* ============================================================
 * Hex encoding
 * ============================================================ */

String hex_encode(const void* data, size_t len) {
    const unsigned char* p = (const unsigned char*)data;
    char* out;
    size_t i;
    if (!data && len > 0) return STRING_EMPTY;
    out = (char*)kmm_v4_malloc(len * 2 + 1);
    if (!out) return STRING_EMPTY;
    for (i = 0; i < len; i++) {
        out[i * 2]     = hex_lower[(p[i] >> 4) & 0x0F];
        out[i * 2 + 1] = hex_lower[p[i] & 0x0F];
    }
    out[len * 2] = '\0';
    return (String){.len = len * 2, .ptr = out};
}

String hex_encode_upper(const void* data, size_t len) {
    const unsigned char* p = (const unsigned char*)data;
    char* out;
    size_t i;
    if (!data && len > 0) return STRING_EMPTY;
    out = (char*)kmm_v4_malloc(len * 2 + 1);
    if (!out) return STRING_EMPTY;
    for (i = 0; i < len; i++) {
        out[i * 2]     = hex_upper[(p[i] >> 4) & 0x0F];
        out[i * 2 + 1] = hex_upper[p[i] & 0x0F];
    }
    out[len * 2] = '\0';
    return (String){.len = len * 2, .ptr = out};
}

void* hex_decode(const char* hex, size_t* out_len) {
    size_t hlen;
    size_t olen;
    size_t i;
    unsigned char* out;
    if (!hex) return NULL;
    hlen = strlen(hex);
    if (hlen % 2 != 0) return NULL;
    olen = hlen / 2;
    out = (unsigned char*)kmm_v4_malloc(olen + 1);
    if (!out) return NULL;
    for (i = 0; i < olen; i++) {
        int hi = hex_value(hex[i * 2]);
        int lo = hex_value(hex[i * 2 + 1]);
        if (hi < 0 || lo < 0) {
            kmm_v4_free(out);
            return NULL;
        }
        out[i] = (unsigned char)((hi << 4) | lo);
    }
    out[olen] = 0;
    if (out_len) *out_len = olen;
    return out;
}

/* ============================================================
 * Base32 encoding (RFC 4648, alphabet A-Z2-7)
 * ============================================================ */

static const char base32_alphabet[] = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";

String base32_encode(const void* data, size_t len) {
    const unsigned char* p = (const unsigned char*)data;
    size_t out_len;
    size_t i = 0;
    size_t o = 0;
    char* out;
    size_t j;
    if (!data && len > 0) return STRING_EMPTY;
    out_len = ((len + 4) / 5) * 8;
    out = (char*)kmm_v4_malloc(out_len + 1);
    if (!out) return STRING_EMPTY;

    while (i < len) {
        size_t remaining = len - i;
        unsigned long long buf = 0;
        int n = remaining >= 5 ? 5 : (int)remaining;
        int data_chars;
        int j2;
        for (j = 0; j < 5; j++) {
            buf <<= 8;
            if ((int)j < n) buf |= p[i + j];
        }
        data_chars = (n == 1) ? 2 : (n == 2) ? 4 : (n == 3) ? 5 : (n == 4) ? 7 : 8;
        for (j2 = 0; j2 < 8; j2++) {
            if (j2 < data_chars) {
                int idx = (int)((buf >> (35 - j2 * 5)) & 0x1F);
                out[o + j2] = base32_alphabet[idx];
            } else {
                out[o + j2] = '=';
            }
        }
        o += 8;
        i += (size_t)n;
    }
    out[o] = '\0';
    return (String){.len = o, .ptr = out};
}

void* base32_decode(const char* encoded, size_t* out_len) {
    size_t elen;
    size_t i;
    size_t o = 0;
    int bits = 0;
    unsigned long long buf = 0;
    unsigned char* out;
    size_t max_out;
    if (!encoded) return NULL;
    elen = strlen(encoded);
    max_out = (elen / 8 + 1) * 5;
    out = (unsigned char*)kmm_v4_malloc(max_out + 1);
    if (!out) return NULL;

    for (i = 0; i < elen; i++) {
        char c = encoded[i];
        int val;
        if (c == '=') break;
        if (c >= 'A' && c <= 'Z') val = c - 'A';
        else if (c >= 'a' && c <= 'z') val = c - 'a';
        else if (c >= '2' && c <= '7') val = c - '2' + 26;
        else if (c == ' ' || c == '\r' || c == '\n' || c == '\t') continue;
        else {
            kmm_v4_free(out);
            return NULL;
        }
        buf = (buf << 5) | (unsigned long long)val;
        bits += 5;
        if (bits >= 8) {
            bits -= 8;
            out[o++] = (unsigned char)((buf >> bits) & 0xFF);
        }
    }
    out[o] = 0;
    if (out_len) *out_len = o;
    return out;
}

/* ============================================================
 * Base64 encoding (standard alphabet, '=' padding)
 * ============================================================ */

static const char base64_alphabet[] =
    "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";

String base64_encode(const void* data, size_t len) {
    const unsigned char* p = (const unsigned char*)data;
    size_t out_len;
    size_t i = 0;
    size_t o = 0;
    char* out;
    if (!data && len > 0) return STRING_EMPTY;
    out_len = ((len + 2) / 3) * 4;
    out = (char*)kmm_v4_malloc(out_len + 1);
    if (!out) return STRING_EMPTY;

    while (i + 3 <= len) {
        unsigned int v = ((unsigned int)p[i] << 16) |
                         ((unsigned int)p[i + 1] << 8) |
                         (unsigned int)p[i + 2];
        out[o++] = base64_alphabet[(v >> 18) & 0x3F];
        out[o++] = base64_alphabet[(v >> 12) & 0x3F];
        out[o++] = base64_alphabet[(v >> 6) & 0x3F];
        out[o++] = base64_alphabet[v & 0x3F];
        i += 3;
    }
    if (i < len) {
        size_t rem = len - i;
        unsigned int v = (unsigned int)p[i] << 16;
        if (rem == 2) v |= (unsigned int)p[i + 1] << 8;
        out[o++] = base64_alphabet[(v >> 18) & 0x3F];
        out[o++] = base64_alphabet[(v >> 12) & 0x3F];
        if (rem == 2) out[o++] = base64_alphabet[(v >> 6) & 0x3F];
        else out[o++] = '=';
        out[o++] = '=';
    }
    out[o] = '\0';
    return (String){.len = o, .ptr = out};
}

void* base64_decode(const char* encoded, size_t* out_len) {
    size_t elen;
    size_t i;
    size_t o = 0;
    unsigned int buf = 0;
    int bits = 0;
    unsigned char* out;
    if (!encoded) return NULL;
    elen = strlen(encoded);
    out = (unsigned char*)kmm_v4_malloc(elen + 1);
    if (!out) return NULL;

    for (i = 0; i < elen; i++) {
        char c = encoded[i];
        int val;
        if (c == '=') break;
        if (c >= 'A' && c <= 'Z') val = c - 'A';
        else if (c >= 'a' && c <= 'z') val = c - 'a' + 26;
        else if (c >= '0' && c <= '9') val = c - '0' + 52;
        else if (c == '+') val = 62;
        else if (c == '/') val = 63;
        else if (c == ' ' || c == '\r' || c == '\n' || c == '\t') continue;
        else {
            kmm_v4_free(out);
            return NULL;
        }
        buf = (buf << 6) | (unsigned int)val;
        bits += 6;
        if (bits >= 8) {
            bits -= 8;
            out[o++] = (unsigned char)((buf >> bits) & 0xFF);
        }
    }
    out[o] = 0;
    if (out_len) *out_len = o;
    return out;
}

/* ============================================================
 * URL encoding / decoding
 * ============================================================ */

static int url_char_safe(char c, int component) {
    if (is_unreserved(c)) return 1;
    if (component) return 0;
    /* url_encode preserves URI reserved and sub-delims so a full URI
       stays structurally valid. url_encode_component encodes them all. */
    switch (c) {
        case '!': case '*': case '\'': case '(': case ')':
        case ';': case ':': case '@': case '&': case '=':
        case '+': case '$': case ',': case '/': case '?':
        case '#': case '[': case ']':
            return 1;
        default:
            return 0;
    }
}

static String url_encode_internal(const char* str, int component) {
    size_t len;
    size_t i;
    size_t o = 0;
    char* out;
    if (!str) return STRING_EMPTY;
    len = strlen(str);
    out = (char*)kmm_v4_malloc(len * 3 + 1);
    if (!out) return STRING_EMPTY;
    for (i = 0; i < len; i++) {
        unsigned char c = (unsigned char)str[i];
        if (url_char_safe((char)c, component)) {
            out[o++] = (char)c;
        } else {
            out[o++] = '%';
            out[o++] = hex_upper[(c >> 4) & 0x0F];
            out[o++] = hex_upper[c & 0x0F];
        }
    }
    out[o] = '\0';
    return (String){.len = o, .ptr = out};
}

String url_encode(const char* str) {
    return url_encode_internal(str, 0);
}

String url_encode_component(const char* str) {
    return url_encode_internal(str, 1);
}

static String url_decode_internal(const char* str) {
    size_t len;
    size_t i;
    size_t o = 0;
    char* out;
    if (!str) return STRING_EMPTY;
    len = strlen(str);
    out = (char*)kmm_v4_malloc(len + 1);
    if (!out) return STRING_EMPTY;
    for (i = 0; i < len; i++) {
        if (str[i] == '%' && i + 2 < len) {
            int hi = hex_value(str[i + 1]);
            int lo = hex_value(str[i + 2]);
            if (hi >= 0 && lo >= 0) {
                out[o++] = (char)((hi << 4) | lo);
                i += 2;
                continue;
            }
        }
        out[o++] = str[i];
    }
    out[o] = '\0';
    return (String){.len = o, .ptr = out};
}

String url_decode(const char* str) {
    return url_decode_internal(str);
}

String url_decode_component(const char* str) {
    return url_decode_internal(str);
}

/* ============================================================
 * Percent-encoding (RFC 3986)
 * ============================================================ */

String percent_encode(const char* str, const char* reserved_chars) {
    size_t len;
    size_t i;
    size_t o = 0;
    char* out;
    if (!str) return STRING_EMPTY;
    len = strlen(str);
    out = (char*)kmm_v4_malloc(len * 3 + 1);
    if (!out) return STRING_EMPTY;
    for (i = 0; i < len; i++) {
        unsigned char c = (unsigned char)str[i];
        int encode = 0;
        if (c < 0x20 || c >= 0x7F) {
            /* control chars and non-ASCII are always encoded */
            encode = 1;
        } else if (reserved_chars != NULL) {
            /* explicit reserved set: encode only listed printable chars */
            if (char_in_set((char)c, reserved_chars)) encode = 1;
        } else {
            /* no reserved set: full RFC 3986 encoding of non-unreserved */
            if (!is_unreserved((char)c)) encode = 1;
        }
        if (encode) {
            out[o++] = '%';
            out[o++] = hex_upper[(c >> 4) & 0x0F];
            out[o++] = hex_upper[c & 0x0F];
        } else {
            out[o++] = (char)c;
        }
    }
    out[o] = '\0';
    return (String){.len = o, .ptr = out};
}

String percent_decode(const char* str) {
    return url_decode_internal(str);
}

/* ============================================================
 * Quoted-Printable (RFC 2045)
 * ============================================================ */

String qp_encode(const char* str) {
    size_t len;
    size_t i = 0;
    size_t o = 0;
    size_t line_len = 0;
    size_t cap;
    char* out;
    if (!str) return STRING_EMPTY;
    len = strlen(str);
    /* worst case: 3 chars/byte + soft-break overhead (~len/4) + slack */
    cap = len * 3 + len / 4 + 32;
    out = (char*)kmm_v4_malloc(cap);
    if (!out) return STRING_EMPTY;

    while (i < len) {
        unsigned char c = (unsigned char)str[i];
        char token[3];
        int tlen = 0;

        if (c == '\r' && i + 1 < len && str[i + 1] == '\n') {
            out[o++] = '\r';
            out[o++] = '\n';
            line_len = 0;
            i += 2;
            continue;
        }
        if (c == '\n') {
            out[o++] = '\r';
            out[o++] = '\n';
            line_len = 0;
            i++;
            continue;
        }
        if (c == '\r') {
            token[0] = '=';
            token[1] = hex_upper[(c >> 4) & 0x0F];
            token[2] = hex_upper[c & 0x0F];
            tlen = 3;
        } else if (c == '=') {
            token[0] = '=';
            token[1] = '3';
            token[2] = 'D';
            tlen = 3;
        } else if (c == ' ' || c == '\t') {
            /* trailing whitespace before a line break must be encoded */
            if (i + 1 >= len || str[i + 1] == '\n' || str[i + 1] == '\r') {
                token[0] = '=';
                token[1] = hex_upper[(c >> 4) & 0x0F];
                token[2] = hex_upper[c & 0x0F];
                tlen = 3;
            } else {
                token[0] = (char)c;
                tlen = 1;
            }
        } else if (c >= 33 && c <= 126) {
            token[0] = (char)c;
            tlen = 1;
        } else {
            token[0] = '=';
            token[1] = hex_upper[(c >> 4) & 0x0F];
            token[2] = hex_upper[c & 0x0F];
            tlen = 3;
        }

        /* soft line break so each line stays within the 76 char limit.
           The trailing '=' counts as one of the 76 characters. */
        if (line_len + (size_t)tlen >= 76) {
            out[o++] = '=';
            out[o++] = '\r';
            out[o++] = '\n';
            line_len = 0;
        }
        {
            int k;
            for (k = 0; k < tlen; k++) out[o++] = token[k];
        }
        line_len += (size_t)tlen;
        i++;
    }
    out[o] = '\0';
    return (String){.len = o, .ptr = out};
}

String qp_decode(const char* str) {
    size_t len;
    size_t i = 0;
    size_t o = 0;
    char* out;
    if (!str) return STRING_EMPTY;
    len = strlen(str);
    out = (char*)kmm_v4_malloc(len + 1);
    if (!out) return STRING_EMPTY;

    while (i < len) {
        if (str[i] == '=') {
            if (i + 1 < len && str[i + 1] == '\n') {
                /* soft line break: =\n */
                i += 2;
                continue;
            }
            if (i + 2 < len && str[i + 1] == '\r' && str[i + 2] == '\n') {
                /* soft line break: =\r\n */
                i += 3;
                continue;
            }
            if (i + 2 < len) {
                int hi = hex_value(str[i + 1]);
                int lo = hex_value(str[i + 2]);
                if (hi >= 0 && lo >= 0) {
                    out[o++] = (char)((hi << 4) | lo);
                    i += 3;
                    continue;
                }
            }
            /* malformed escape: emit '=' literally */
            out[o++] = '=';
            i++;
        } else {
            out[o++] = str[i];
            i++;
        }
    }
    out[o] = '\0';
    return (String){.len = o, .ptr = out};
}

/* ============================================================
 * Pascal string encoding (length-prefixed, bencode style)
 *   encode: "<decimal-length>:<string-bytes>"
 *   e.g. "hello" -> "5:hello"
 * ============================================================ */

String pascal_encode(const char* str) {
    size_t len;
    size_t n;
    size_t d = 0;
    size_t prefix_len;
    char tmp[32];
    char* out;
    if (!str) return STRING_EMPTY;
    len = strlen(str);
    n = len;
    if (n == 0) {
        tmp[0] = '0';
        d = 1;
    } else {
        while (n > 0) {
            tmp[d++] = (char)('0' + (n % 10));
            n /= 10;
        }
    }
    /* reverse digit sequence into big-endian order */
    {
        size_t a, b;
        for (a = 0, b = d - 1; a < b; a++, b--) {
            char t = tmp[a];
            tmp[a] = tmp[b];
            tmp[b] = t;
        }
    }
    prefix_len = d + 1; /* digits + ':' */
    out = (char*)kmm_v4_malloc(prefix_len + len + 1);
    if (!out) return STRING_EMPTY;
    memcpy(out, tmp, d);
    out[d] = ':';
    memcpy(out + prefix_len, str, len);
    out[prefix_len + len] = '\0';
    return (String){.len = prefix_len + len, .ptr = out};
}

String pascal_decode(const char* encoded) {
    size_t len;
    size_t i = 0;
    size_t n = 0;
    const char* payload;
    char* out;
    if (!encoded) return STRING_EMPTY;
    len = strlen(encoded);
    while (i < len && encoded[i] >= '0' && encoded[i] <= '9') {
        n = n * 10 + (size_t)(encoded[i] - '0');
        i++;
    }
    if (i >= len || encoded[i] != ':') return STRING_EMPTY;
    i++; /* skip ':' */
    payload = encoded + i;
    if (i + n > len) return STRING_EMPTY; /* not enough payload bytes */
    out = (char*)kmm_v4_malloc(n + 1);
    if (!out) return STRING_EMPTY;
    memcpy(out, payload, n);
    out[n] = '\0';
    return (String){.len = n, .ptr = out};
}
