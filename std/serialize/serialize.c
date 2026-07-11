#include "serialize.h"
#include "../memory/memory.h"
#include <string.h>
#include <stdio.h>
#include <stdint.h>   /* SIZE_MAX */

/*
 * MessagePack-style binary serialization.
 *
 * Format type bytes (single leading tag per value):
 *   0x00-0x7f  positive fixint        (value 0..127, 1 byte total)
 *   0x80-0x8f  fixmap                 (count 0..15)
 *   0x90-0x9f  fixarray               (count 0..15)
 *   0xa0-0xbf  fixstr                 (len 0..31)
 *   0xc0       nil
 *   0xc1       tag marker (custom; reserved in upstream MessagePack)
 *   0xc2       false
 *   0xc3       true
 *   0xc4       bin8   (1-byte len, then bytes)
 *   0xc5       bin16  (2-byte len LE, then bytes)
 *   0xc6       bin32  (4-byte len LE, then bytes)
 *   0xca       float32 (4 bytes LE)
 *   0xcb       float64 (8 bytes LE)
 *   0xcc       uint8  (1 byte)
 *   0xcd       uint16 (2 bytes LE)
 *   0xce       uint32 (4 bytes LE)
 *   0xcf       uint64 (8 bytes LE)
 *   0xd0       int8   (1 byte)
 *   0xd1       int16  (2 bytes LE)
 *   0xd2       int32  (4 bytes LE)
 *   0xd3       int64  (8 bytes LE)
 *   0xd9       str8   (1-byte len, then bytes)
 *   0xda       str16  (2-byte len LE, then bytes)
 *   0xdb       str32  (4-byte len LE, then bytes)
 *   0xdc       array16 (2-byte count LE)
 *   0xdd       array32 (4-byte count LE)
 *   0xde       map16  (2-byte count LE)
 *   0xdf       map32  (4-byte count LE)
 *   0xe0-0xff  negative fixint        (value -32..-1, 1 byte total)
 *
 * Integers use variable-length encoding: the smallest representation that
 * fits the value is chosen, so small integers take a single byte.
 * deserializer_peek_type() therefore reports the encoding width (e.g. a
 * value written via write_u32(5) is encoded as a positive fixint and
 * reported as SER_UINT8). The numeric value always round-trips correctly
 * and the integer read functions accept any integer encoding.
 *
 * All multi-byte payloads use little-endian byte order.
 */

#define SER_INITIAL_CAP 64

struct Serializer {
    u8*   buf;
    size_t len;
    size_t cap;
};

struct Deserializer {
    const u8* data;
    size_t    len;
    size_t    cursor;
    int       owns_data;   /* non-zero when this instance owns data and must free it */
};

/* ----------------------------------------------------------------------- */
/* Serializer internal helpers                                             */
/* ----------------------------------------------------------------------- */

static int ser_ensure(Serializer* s, size_t add) {
    if (!s) return 0;
    if (add > SIZE_MAX - s->len) return 0;            /* overflow guard */
    if (s->len + add <= s->cap) return 1;

    size_t need = s->len + add;
    size_t cap = s->cap ? s->cap : SER_INITIAL_CAP;
    while (cap < need) {
        size_t ncap = cap * 2;
        if (ncap <= cap) {                            /* overflow */
            cap = need;
            break;
        }
        cap = ncap;
    }

    u8* nb = (u8*)kmm_v4_malloc(cap);
    if (!nb) return 0;
    if (s->len > 0 && s->buf) memcpy(nb, s->buf, s->len);
    if (s->buf) kmm_v4_free(s->buf);
    s->buf = nb;
    s->cap = cap;
    return 1;
}

static void ser_put_u8(Serializer* s, u8 b) {
    if (!ser_ensure(s, 1)) return;
    s->buf[s->len++] = b;
}

static void ser_put_bytes(Serializer* s, const void* data, size_t n) {
    if (n == 0 || !data) return;
    if (!ser_ensure(s, n)) return;
    memcpy(s->buf + s->len, data, n);
    s->len += n;
}

static void ser_put_u16_le(Serializer* s, u16 v) {
    ser_put_u8(s, (u8)(v & 0xff));
    ser_put_u8(s, (u8)((v >> 8) & 0xff));
}

static void ser_put_u32_le(Serializer* s, u32 v) {
    ser_put_u8(s, (u8)(v & 0xff));
    ser_put_u8(s, (u8)((v >> 8) & 0xff));
    ser_put_u8(s, (u8)((v >> 16) & 0xff));
    ser_put_u8(s, (u8)((v >> 24) & 0xff));
}

static void ser_put_u64_le(Serializer* s, u64 v) {
    ser_put_u8(s, (u8)(v & 0xff));
    ser_put_u8(s, (u8)((v >> 8) & 0xff));
    ser_put_u8(s, (u8)((v >> 16) & 0xff));
    ser_put_u8(s, (u8)((v >> 24) & 0xff));
    ser_put_u8(s, (u8)((v >> 32) & 0xff));
    ser_put_u8(s, (u8)((v >> 40) & 0xff));
    ser_put_u8(s, (u8)((v >> 48) & 0xff));
    ser_put_u8(s, (u8)((v >> 56) & 0xff));
}

/* Encode an unsigned integer using the smallest available representation. */
static void ser_write_uint(Serializer* s, u64 v) {
    if (v <= 0x7f) {
        ser_put_u8(s, (u8)v);                              /* positive fixint */
    } else if (v <= 0xff) {
        ser_put_u8(s, 0xcc); ser_put_u8(s, (u8)v);         /* uint8 */
    } else if (v <= 0xffff) {
        ser_put_u8(s, 0xcd); ser_put_u16_le(s, (u16)v);    /* uint16 */
    } else if (v <= 0xffffffffu) {
        ser_put_u8(s, 0xce); ser_put_u32_le(s, (u32)v);    /* uint32 */
    } else {
        ser_put_u8(s, 0xcf); ser_put_u64_le(s, v);         /* uint64 */
    }
}

/* Encode a signed integer using the smallest available representation. */
static void ser_write_sint(Serializer* s, i64 v) {
    if (v >= 0) {
        ser_write_uint(s, (u64)v);                         /* shares unsigned path */
    } else if (v >= -32) {
        ser_put_u8(s, (u8)(i8)v);                          /* negative fixint */
    } else if (v >= -128) {
        ser_put_u8(s, 0xd0); ser_put_u8(s, (u8)(i8)v);     /* int8 */
    } else if (v >= -32768) {
        ser_put_u8(s, 0xd1); ser_put_u16_le(s, (u16)(i16)v); /* int16 */
    } else if (v >= -2147483648LL) {
        ser_put_u8(s, 0xd2); ser_put_u32_le(s, (u32)(i32)v); /* int32 */
    } else {
        ser_put_u8(s, 0xd3); ser_put_u64_le(s, (u64)v);      /* int64 */
    }
}

static void ser_write_str_header(Serializer* s, size_t len) {
    if (len <= 31) {
        ser_put_u8(s, (u8)(0xa0 | (u8)len));               /* fixstr */
    } else if (len <= 0xff) {
        ser_put_u8(s, 0xd9); ser_put_u8(s, (u8)len);       /* str8 */
    } else if (len <= 0xffff) {
        ser_put_u8(s, 0xda); ser_put_u16_le(s, (u16)len);  /* str16 */
    } else {
        ser_put_u8(s, 0xdb); ser_put_u32_le(s, (u32)len);  /* str32 */
    }
}

static void ser_write_bin_header(Serializer* s, size_t len) {
    if (len <= 0xff) {
        ser_put_u8(s, 0xc4); ser_put_u8(s, (u8)len);       /* bin8 */
    } else if (len <= 0xffff) {
        ser_put_u8(s, 0xc5); ser_put_u16_le(s, (u16)len);  /* bin16 */
    } else {
        ser_put_u8(s, 0xc6); ser_put_u32_le(s, (u32)len);  /* bin32 */
    }
}

static void ser_write_array_header(Serializer* s, size_t count) {
    if (count <= 15) {
        ser_put_u8(s, (u8)(0x90 | (u8)count));             /* fixarray */
    } else if (count <= 0xffff) {
        ser_put_u8(s, 0xdc); ser_put_u16_le(s, (u16)count); /* array16 */
    } else {
        ser_put_u8(s, 0xdd); ser_put_u32_le(s, (u32)count); /* array32 */
    }
}

static void ser_write_map_header(Serializer* s, size_t count) {
    if (count <= 15) {
        ser_put_u8(s, (u8)(0x80 | (u8)count));             /* fixmap */
    } else if (count <= 0xffff) {
        ser_put_u8(s, 0xde); ser_put_u16_le(s, (u16)count); /* map16 */
    } else {
        ser_put_u8(s, 0xdf); ser_put_u32_le(s, (u32)count); /* map32 */
    }
}

/* ----------------------------------------------------------------------- */
/* Deserializer internal helpers                                           */
/* ----------------------------------------------------------------------- */

static u8 deser_read_u8_raw(Deserializer* d) {
    if (!d || d->cursor >= d->len) return 0;
    return d->data[d->cursor++];
}

static const u8* deser_read_ptr(Deserializer* d, size_t n) {
    if (!d || d->cursor + n > d->len) {
        if (d) d->cursor = d->len;
        return NULL;
    }
    const u8* p = d->data + d->cursor;
    d->cursor += n;
    return p;
}

static u16 deser_read_u16_le(Deserializer* d) {
    const u8* p = deser_read_ptr(d, 2);
    if (!p) return 0;
    return (u16)((u16)p[0] | ((u16)p[1] << 8));
}

static u32 deser_read_u32_le(Deserializer* d) {
    const u8* p = deser_read_ptr(d, 4);
    if (!p) return 0;
    return (u32)p[0] | ((u32)p[1] << 8) | ((u32)p[2] << 16) | ((u32)p[3] << 24);
}

static u64 deser_read_u64_le(Deserializer* d) {
    const u8* p = deser_read_ptr(d, 8);
    if (!p) return 0;
    u64 v = (u64)p[0];
    v |= (u64)p[1] << 8;
    v |= (u64)p[2] << 16;
    v |= (u64)p[3] << 24;
    v |= (u64)p[4] << 32;
    v |= (u64)p[5] << 40;
    v |= (u64)p[6] << 48;
    v |= (u64)p[7] << 56;
    return v;
}

/* Decode any integer encoding as unsigned. Negative values are returned
 * as their bit pattern reinterpreted as u64. */
static u64 deser_read_uint_any(Deserializer* d) {
    u8 tag = deser_read_u8_raw(d);
    if (tag <= 0x7f) return (u64)tag;                       /* positive fixint */
    if (tag >= 0xe0) return (u64)tag;                       /* negative fixint bits */
    switch (tag) {
        case 0xcc: return (u64)deser_read_u8_raw(d);        /* uint8 */
        case 0xcd: return (u64)deser_read_u16_le(d);        /* uint16 */
        case 0xce: return (u64)deser_read_u32_le(d);        /* uint32 */
        case 0xcf: return deser_read_u64_le(d);             /* uint64 */
        case 0xd0: return (u64)(i64)(i8)deser_read_u8_raw(d);   /* int8 */
        case 0xd1: return (u64)(i64)(i16)deser_read_u16_le(d);  /* int16 */
        case 0xd2: return (u64)(i64)(i32)deser_read_u32_le(d);  /* int32 */
        case 0xd3: return deser_read_u64_le(d);             /* int64 */
        default:   return 0;
    }
}

/* Decode any integer encoding as signed. */
static i64 deser_read_sint_any(Deserializer* d) {
    u8 tag = deser_read_u8_raw(d);
    if (tag <= 0x7f) return (i64)tag;                       /* positive fixint */
    if (tag >= 0xe0) return (i64)(i8)tag;                   /* negative fixint */
    switch (tag) {
        case 0xcc: return (i64)deser_read_u8_raw(d);        /* uint8 */
        case 0xcd: return (i64)deser_read_u16_le(d);        /* uint16 */
        case 0xce: return (i64)deser_read_u32_le(d);        /* uint32 */
        case 0xcf: return (i64)deser_read_u64_le(d);        /* uint64 */
        case 0xd0: return (i64)(i8)deser_read_u8_raw(d);    /* int8 */
        case 0xd1: return (i64)(i16)deser_read_u16_le(d);   /* int16 */
        case 0xd2: return (i64)(i32)deser_read_u32_le(d);   /* int32 */
        case 0xd3: return (i64)deser_read_u64_le(d);        /* int64 */
        default:   return 0;
    }
}

/* Read a length-prefixed string payload (header already consumed). */
static String deser_read_string_body(Deserializer* d, size_t len) {
    char* str = (char*)kmm_v4_malloc(len + 1);
    if (!str) return NULL;
    if (len > 0) {
        if (d->cursor + len > d->len) {
            len = (d->len > d->cursor) ? (d->len - d->cursor) : 0;
        }
        if (len > 0) memcpy(str, d->data + d->cursor, len);
        d->cursor += len;
    }
    str[len] = '\0';
    return str;
}

/* ----------------------------------------------------------------------- */
/* Serializer public API                                                   */
/* ----------------------------------------------------------------------- */

Serializer* serializer_create(void) {
    Serializer* s = (Serializer*)kmm_v4_malloc(sizeof(Serializer));
    if (!s) return NULL;
    s->buf = NULL;
    s->len = 0;
    s->cap = 0;
    return s;
}

void serializer_destroy(Serializer* s) {
    if (!s) return;
    if (s->buf) kmm_v4_free(s->buf);
    kmm_v4_free(s);
}

void serializer_write_bool(Serializer* s, bool_t val) {
    ser_put_u8(s, val ? 0xc3 : 0xc2);
}

void serializer_write_i8(Serializer* s, i8 val)   { ser_write_sint(s, (i64)val); }
void serializer_write_i16(Serializer* s, i16 val)  { ser_write_sint(s, (i64)val); }
void serializer_write_i32(Serializer* s, i32 val)  { ser_write_sint(s, (i64)val); }
void serializer_write_i64(Serializer* s, i64 val)  { ser_write_sint(s, val); }

void serializer_write_u8(Serializer* s, u8 val)   { ser_write_uint(s, (u64)val); }
void serializer_write_u16(Serializer* s, u16 val)  { ser_write_uint(s, (u64)val); }
void serializer_write_u32(Serializer* s, u32 val)  { ser_write_uint(s, (u64)val); }
void serializer_write_u64(Serializer* s, u64 val)  { ser_write_uint(s, val); }

void serializer_write_f32(Serializer* s, f32 val) {
    u32 bits;
    memcpy(&bits, &val, sizeof(bits));
    ser_put_u8(s, 0xca);
    ser_put_u32_le(s, bits);
}

void serializer_write_f64(Serializer* s, f64 val) {
    u64 bits;
    memcpy(&bits, &val, sizeof(bits));
    ser_put_u8(s, 0xcb);
    ser_put_u64_le(s, bits);
}

void serializer_write_string(Serializer* s, const char* str) {
    if (!str) str = "";                                     /* NULL -> empty string */
    size_t len = strlen(str);
    ser_write_str_header(s, len);
    ser_put_bytes(s, str, len);
}

void serializer_write_binary(Serializer* s, const void* data, size_t len) {
    ser_write_bin_header(s, len);
    ser_put_bytes(s, data, len);
}

void serializer_write_null(Serializer* s) {
    ser_put_u8(s, 0xc0);
}

void serializer_begin_array(Serializer* s, size_t count) {
    ser_write_array_header(s, count);
}

void serializer_begin_map(Serializer* s, size_t count) {
    ser_write_map_header(s, count);
}

void serializer_write_key(Serializer* s, const char* key) {
    /* Map keys are encoded as strings. */
    serializer_write_string(s, key);
}

void serializer_write_tag(Serializer* s, const char* tag) {
    ser_put_u8(s, 0xc1);                                    /* tag marker */
    if (!tag) tag = "";
    size_t len = strlen(tag);
    ser_write_str_header(s, len);
    ser_put_bytes(s, tag, len);
}

u8* serializer_get_buffer(Serializer* s, size_t* out_len) {
    if (out_len) *out_len = s ? s->len : 0;
    /* Borrowed pointer: valid until serializer_destroy or the next write. */
    return s ? s->buf : NULL;
}

/* ----------------------------------------------------------------------- */
/* Deserializer public API                                                 */
/* ----------------------------------------------------------------------- */

Deserializer* deserializer_create(const u8* data, size_t len) {
    Deserializer* d = (Deserializer*)kmm_v4_malloc(sizeof(Deserializer));
    if (!d) return NULL;
    d->data = data;
    d->len = len;
    d->cursor = 0;
    d->owns_data = 0;
    return d;
}

void deserializer_destroy(Deserializer* d) {
    if (!d) return;
    if (d->owns_data && d->data) kmm_v4_free((void*)d->data);
    kmm_v4_free(d);
}

bool_t deserializer_read_bool(Deserializer* d) {
    u8 tag = deser_read_u8_raw(d);
    return tag == 0xc3 ? 1 : 0;
}

i8  deserializer_read_i8(Deserializer* d)  { return (i8)deser_read_sint_any(d); }
i16 deserializer_read_i16(Deserializer* d) { return (i16)deser_read_sint_any(d); }
i32 deserializer_read_i32(Deserializer* d) { return (i32)deser_read_sint_any(d); }
i64 deserializer_read_i64(Deserializer* d) { return (i64)deser_read_sint_any(d); }

u8  deserializer_read_u8(Deserializer* d)  { return (u8)deser_read_uint_any(d); }
u16 deserializer_read_u16(Deserializer* d) { return (u16)deser_read_uint_any(d); }
u32 deserializer_read_u32(Deserializer* d) { return (u32)deser_read_uint_any(d); }
u64 deserializer_read_u64(Deserializer* d) { return (u64)deser_read_uint_any(d); }

f32 deserializer_read_f32(Deserializer* d) {
    u8 tag = deser_read_u8_raw(d);
    if (tag != 0xca) return 0.0f;
    u32 bits = deser_read_u32_le(d);
    f32 val;
    memcpy(&val, &bits, sizeof(val));
    return val;
}

f64 deserializer_read_f64(Deserializer* d) {
    u8 tag = deser_read_u8_raw(d);
    if (tag != 0xcb) return 0.0;
    u64 bits = deser_read_u64_le(d);
    f64 val;
    memcpy(&val, &bits, sizeof(val));
    return val;
}

String deserializer_read_string(Deserializer* d) {
    u8 tag = deser_read_u8_raw(d);
    size_t len = 0;
    if (tag >= 0xa0 && tag <= 0xbf) {
        len = tag - 0xa0;                                   /* fixstr */
    } else if (tag == 0xd9) {
        len = deser_read_u8_raw(d);                         /* str8 */
    } else if (tag == 0xda) {
        len = deser_read_u16_le(d);                         /* str16 */
    } else if (tag == 0xdb) {
        len = deser_read_u32_le(d);                         /* str32 */
    } else {
        /* Not a string token; return empty string. */
        return deser_read_string_body(d, 0);
    }
    return deser_read_string_body(d, len);
}

void* deserializer_read_binary(Deserializer* d, size_t* out_len) {
    if (out_len) *out_len = 0;
    u8 tag = deser_read_u8_raw(d);
    size_t len = 0;
    if (tag == 0xc4) {
        len = deser_read_u8_raw(d);                         /* bin8 */
    } else if (tag == 0xc5) {
        len = deser_read_u16_le(d);                         /* bin16 */
    } else if (tag == 0xc6) {
        len = deser_read_u32_le(d);                         /* bin32 */
    } else {
        return NULL;                                        /* not a binary token */
    }

    if (d->cursor + len > d->len) {
        len = (d->len > d->cursor) ? (d->len - d->cursor) : 0;
    }

    void* buf = kmm_v4_malloc(len > 0 ? len : 1);
    if (!buf) return NULL;
    if (len > 0) memcpy(buf, d->data + d->cursor, len);
    d->cursor += len;
    if (out_len) *out_len = len;
    return buf;
}

SerializeType deserializer_peek_type(Deserializer* d) {
    if (!d || d->cursor >= d->len) return SER_NULL;
    u8 tag = d->data[d->cursor];
    if (tag <= 0x7f) return SER_UINT8;                      /* positive fixint */
    if (tag >= 0xe0) return SER_INT8;                       /* negative fixint */
    if (tag >= 0xa0 && tag <= 0xbf) return SER_STRING;      /* fixstr */
    if (tag >= 0x90 && tag <= 0x9f) return SER_ARRAY;       /* fixarray */
    if (tag >= 0x80 && tag <= 0x8f) return SER_MAP;         /* fixmap */
    switch (tag) {
        case 0xc0: return SER_NULL;
        case 0xc1: return SER_TIMESTAMP;                    /* tag marker */
        case 0xc2: case 0xc3: return SER_BOOL;
        case 0xc4: case 0xc5: case 0xc6: return SER_BINARY;
        case 0xca: return SER_FLOAT32;
        case 0xcb: return SER_FLOAT64;
        case 0xcc: return SER_UINT8;
        case 0xcd: return SER_UINT16;
        case 0xce: return SER_UINT32;
        case 0xcf: return SER_UINT64;
        case 0xd0: return SER_INT8;
        case 0xd1: return SER_INT16;
        case 0xd2: return SER_INT32;
        case 0xd3: return SER_INT64;
        case 0xd9: case 0xda: case 0xdb: return SER_STRING;
        case 0xdc: case 0xdd: return SER_ARRAY;
        case 0xde: case 0xdf: return SER_MAP;
        default:   return SER_NULL;
    }
}

size_t deserializer_begin_array(Deserializer* d) {
    u8 tag = deser_read_u8_raw(d);
    if (tag >= 0x90 && tag <= 0x9f) return tag - 0x90;      /* fixarray */
    if (tag == 0xdc) return deser_read_u16_le(d);           /* array16 */
    if (tag == 0xdd) return deser_read_u32_le(d);           /* array32 */
    return 0;
}

size_t deserializer_begin_map(Deserializer* d) {
    u8 tag = deser_read_u8_raw(d);
    if (tag >= 0x80 && tag <= 0x8f) return tag - 0x80;      /* fixmap */
    if (tag == 0xde) return deser_read_u16_le(d);           /* map16 */
    if (tag == 0xdf) return deser_read_u32_le(d);           /* map32 */
    return 0;
}

String deserializer_read_key(Deserializer* d) {
    /* Keys are written as strings. */
    return deserializer_read_string(d);
}

String deserializer_read_tag(Deserializer* d) {
    u8 tag = deser_read_u8_raw(d);
    if (tag != 0xc1) {
        /* Not a tag marker; return empty string without consuming more. */
        return deser_read_string_body(d, 0);
    }
    return deserializer_read_string(d);
}

bool_t deserializer_is_null(Deserializer* d) {
    if (!d || d->cursor >= d->len) return 1;
    return d->data[d->cursor] == 0xc0 ? 1 : 0;
}

bool_t deserializer_at_end(Deserializer* d) {
    if (!d) return 1;
    return d->cursor >= d->len ? 1 : 0;
}

/* ----------------------------------------------------------------------- */
/* File convenience helpers                                                */
/* ----------------------------------------------------------------------- */

bool_t serializer_to_file(Serializer* s, const char* path) {
    if (!s || !path) return 0;
    size_t len = 0;
    u8* buf = serializer_get_buffer(s, &len);

    FILE* f = fopen(path, "wb");
    if (!f) return 0;

    bool_t ok = 1;
    if (len > 0) {
        size_t w = fwrite(buf, 1, len, f);
        if (w != len) ok = 0;
    }
    if (fclose(f) != 0) ok = 0;
    return ok;
}

Deserializer* deserializer_from_file(const char* path) {
    if (!path) return NULL;

    FILE* f = fopen(path, "rb");
    if (!f) return NULL;

    if (fseek(f, 0, SEEK_END) != 0) { fclose(f); return NULL; }
    long size = ftell(f);
    if (size < 0) { fclose(f); return NULL; }
    if (fseek(f, 0, SEEK_SET) != 0) { fclose(f); return NULL; }

    u8* buf = (u8*)kmm_v4_malloc((size_t)size ? (size_t)size : 1);
    if (!buf) { fclose(f); return NULL; }

    if (size > 0) {
        size_t rd = fread(buf, 1, (size_t)size, f);
        if (rd != (size_t)size) {
            kmm_v4_free(buf);
            fclose(f);
            return NULL;
        }
    }
    fclose(f);

    Deserializer* d = deserializer_create(buf, (size_t)size);
    if (!d) {
        kmm_v4_free(buf);
        return NULL;
    }
    d->owns_data = 1;   /* take ownership of the loaded buffer */
    return d;
}
