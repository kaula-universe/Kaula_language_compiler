#include "msgpack.h"
#include <stdlib.h>
#include <string.h>

MPWriter* mp_writer_create(void) {
    MPWriter* w = (MPWriter*)kmm_v4_malloc(sizeof(MPWriter));
    if (!w) return NULL;
    w->buffer = NULL;
    w->size = 0;
    w->capacity = 0;
    return w;
}

void mp_writer_destroy(MPWriter* writer) {
    if (!writer) return;
    kmm_v4_free(writer->buffer);
    kmm_v4_free(writer);
}

static void mp_writer_reserve(MPWriter* writer, size_t size) {
    if (!writer) return;
    if (writer->capacity < writer->size + size) {
        writer->capacity = (writer->size + size) * 2;
        writer->buffer = (u8*)kmm_v4_realloc(writer->buffer, writer->capacity);
    }
}

u8* mp_writer_data(const MPWriter* writer) {
    return writer ? writer->buffer : NULL;
}

void mp_write_nil(MPWriter* writer) {
    if (!writer) return;
    mp_writer_reserve(writer, 1);
    writer->buffer[writer->size++] = MSGPACK_NIL;
}

void mp_write_bool(MPWriter* writer, bool_t value) {
    if (!writer) return;
    mp_writer_reserve(writer, 1);
    writer->buffer[writer->size++] = value ? MSGPACK_TRUE : MSGPACK_FALSE;
}

void mp_write_int(MPWriter* writer, i64 value) {
    if (!writer) return;
    
    if (value >= 0) {
        mp_write_uint(writer, (u64)value);
        return;
    }
    
    if (value >= -32) {
        mp_writer_reserve(writer, 1);
        writer->buffer[writer->size++] = (u8)value;
    } else if (value >= -128) {
        mp_writer_reserve(writer, 2);
        writer->buffer[writer->size++] = MSGPACK_INT8;
        writer->buffer[writer->size++] = (u8)(i8)value;
    } else if (value >= -32768) {
        mp_writer_reserve(writer, 3);
        writer->buffer[writer->size++] = MSGPACK_INT16;
        writer->buffer[writer->size++] = (u8)((i16)value);
        writer->buffer[writer->size++] = (u8)((i16)value >> 8);
    } else if (value >= -2147483648LL) {
        mp_writer_reserve(writer, 5);
        writer->buffer[writer->size++] = MSGPACK_INT32;
        for (int i = 0; i < 4; i++) {
            writer->buffer[writer->size++] = (u8)((i32)value >> (8 * i));
        }
    } else {
        mp_writer_reserve(writer, 9);
        writer->buffer[writer->size++] = MSGPACK_INT64;
        for (int i = 0; i < 8; i++) {
            writer->buffer[writer->size++] = (u8)(value >> (8 * i));
        }
    }
}

void mp_write_uint(MPWriter* writer, u64 value) {
    if (!writer) return;
    
    if (value <= 127) {
        mp_writer_reserve(writer, 1);
        writer->buffer[writer->size++] = (u8)value;
    } else if (value <= 255) {
        mp_writer_reserve(writer, 2);
        writer->buffer[writer->size++] = MSGPACK_UINT8;
        writer->buffer[writer->size++] = (u8)value;
    } else if (value <= 65535) {
        mp_writer_reserve(writer, 3);
        writer->buffer[writer->size++] = MSGPACK_UINT16;
        writer->buffer[writer->size++] = (u8)(value);
        writer->buffer[writer->size++] = (u8)(value >> 8);
    } else if (value <= 4294967295ULL) {
        mp_writer_reserve(writer, 5);
        writer->buffer[writer->size++] = MSGPACK_UINT32;
        for (int i = 0; i < 4; i++) {
            writer->buffer[writer->size++] = (u8)(value >> (8 * i));
        }
    } else {
        mp_writer_reserve(writer, 9);
        writer->buffer[writer->size++] = MSGPACK_UINT64;
        for (int i = 0; i < 8; i++) {
            writer->buffer[writer->size++] = (u8)(value >> (8 * i));
        }
    }
}

void mp_write_float(MPWriter* writer, f32 value) {
    if (!writer) return;
    mp_writer_reserve(writer, 5);
    writer->buffer[writer->size++] = MSGPACK_FLOAT;
    union { f32 f; u32 u; } u;
    u.f = value;
    for (int i = 0; i < 4; i++) {
        writer->buffer[writer->size++] = (u8)(u.u >> (8 * i));
    }
}

void mp_write_double(MPWriter* writer, f64 value) {
    if (!writer) return;
    mp_writer_reserve(writer, 9);
    writer->buffer[writer->size++] = MSGPACK_DOUBLE;
    union { f64 f; u64 u; } u;
    u.f = value;
    for (int i = 0; i < 8; i++) {
        writer->buffer[writer->size++] = (u8)(u.u >> (8 * i));
    }
}

void mp_write_string(MPWriter* writer, const char* value) {
    if (!writer || !value) return;
    
    size_t len = strlen(value);
    
    if (len <= 31) {
        mp_writer_reserve(writer, 1 + len);
        writer->buffer[writer->size++] = MSGPACK_FIXSTR_START | (u8)len;
    } else if (len <= 255) {
        mp_writer_reserve(writer, 2 + len);
        writer->buffer[writer->size++] = MSGPACK_STR8;
        writer->buffer[writer->size++] = (u8)len;
    } else if (len <= 65535) {
        mp_writer_reserve(writer, 3 + len);
        writer->buffer[writer->size++] = MSGPACK_STR16;
        writer->buffer[writer->size++] = (u8)len;
        writer->buffer[writer->size++] = (u8)(len >> 8);
    } else {
        mp_writer_reserve(writer, 5 + len);
        writer->buffer[writer->size++] = MSGPACK_STR32;
        for (int i = 0; i < 4; i++) {
            writer->buffer[writer->size++] = (u8)(len >> (8 * i));
        }
    }
    
    memcpy(writer->buffer + writer->size, value, len);
    writer->size += len;
}

void mp_write_u8s(MPWriter* writer, const u8* data, size_t len) {
    if (!writer || !data) return;
    
    mp_writer_reserve(writer, 5 + len);
    
    if (len <= 255) {
        writer->buffer[writer->size++] = MSGPACK_BIN8;
        writer->buffer[writer->size++] = (u8)len;
    } else if (len <= 65535) {
        writer->buffer[writer->size++] = MSGPACK_BIN16;
        writer->buffer[writer->size++] = (u8)len;
        writer->buffer[writer->size++] = (u8)(len >> 8);
    } else {
        writer->buffer[writer->size++] = MSGPACK_BIN32;
        for (int i = 0; i < 4; i++) {
            writer->buffer[writer->size++] = (u8)(len >> (8 * i));
        }
    }
    
    memcpy(writer->buffer + writer->size, data, len);
    writer->size += len;
}

void mp_write_array_header(MPWriter* writer, size_t count) {
    if (!writer) return;
    
    if (count <= 15) {
        mp_writer_reserve(writer, 1);
        writer->buffer[writer->size++] = MSGPACK_FIXARRAY_START | (u8)count;
    } else if (count <= 65535) {
        mp_writer_reserve(writer, 3);
        writer->buffer[writer->size++] = MSGPACK_ARRAY16;
        writer->buffer[writer->size++] = (u8)count;
        writer->buffer[writer->size++] = (u8)(count >> 8);
    } else {
        mp_writer_reserve(writer, 5);
        writer->buffer[writer->size++] = MSGPACK_ARRAY32;
        for (int i = 0; i < 4; i++) {
            writer->buffer[writer->size++] = (u8)(count >> (8 * i));
        }
    }
}

void mp_write_map_header(MPWriter* writer, size_t count) {
    if (!writer) return;
    
    if (count <= 15) {
        mp_writer_reserve(writer, 1);
        writer->buffer[writer->size++] = MSGPACK_FIXMAP_START | (u8)count;
    } else if (count <= 65535) {
        mp_writer_reserve(writer, 3);
        writer->buffer[writer->size++] = MSGPACK_MAP16;
        writer->buffer[writer->size++] = (u8)count;
        writer->buffer[writer->size++] = (u8)(count >> 8);
    } else {
        mp_writer_reserve(writer, 5);
        writer->buffer[writer->size++] = MSGPACK_MAP32;
        for (int i = 0; i < 4; i++) {
            writer->buffer[writer->size++] = (u8)(count >> (8 * i));
        }
    }
}

void mp_write_ext(MPWriter* writer, i8 type, const u8* data, size_t len) {
    if (!writer || !data) return;
    
    mp_writer_reserve(writer, 6 + len);
    
    if (len == 1) {
        writer->buffer[writer->size++] = MSGPACK_EXT8;
        writer->buffer[writer->size++] = 1;
    } else if (len == 2) {
        writer->buffer[writer->size++] = MSGPACK_EXT8;
        writer->buffer[writer->size++] = 2;
    } else if (len == 4) {
        writer->buffer[writer->size++] = MSGPACK_EXT8;
        writer->buffer[writer->size++] = 4;
    } else if (len == 8) {
        writer->buffer[writer->size++] = MSGPACK_EXT8;
        writer->buffer[writer->size++] = 8;
    } else if (len == 16) {
        writer->buffer[writer->size++] = MSGPACK_EXT8;
        writer->buffer[writer->size++] = 16;
    } else if (len <= 255) {
        writer->buffer[writer->size++] = MSGPACK_EXT8;
        writer->buffer[writer->size++] = (u8)len;
    } else if (len <= 65535) {
        writer->buffer[writer->size++] = MSGPACK_EXT16;
        writer->buffer[writer->size++] = (u8)len;
        writer->buffer[writer->size++] = (u8)(len >> 8);
    } else {
        writer->buffer[writer->size++] = MSGPACK_EXT32;
        for (int i = 0; i < 4; i++) {
            writer->buffer[writer->size++] = (u8)(len >> (8 * i));
        }
    }
    
    writer->buffer[writer->size++] = (u8)type;
    memcpy(writer->buffer + writer->size, data, len);
    writer->size += len;
}

MPReader* mp_reader_create(const u8* buffer, size_t size) {
    MPReader* r = (MPReader*)kmm_v4_malloc(sizeof(MPReader));
    if (!r) return NULL;
    r->buffer = buffer;
    r->size = size;
    r->offset = 0;
    return r;
}

void mp_reader_destroy(MPReader* reader) {
    kmm_v4_free(reader);
}

MsgPackType mp_read_type(MPReader* reader) {
    if (!reader || reader->offset >= reader->size) return 0;
    return (MsgPackType)reader->buffer[reader->offset];
}

bool_t mp_read_nil(MPReader* reader) {
    if (!reader || reader->offset >= reader->size) return false;
    if (reader->buffer[reader->offset] != MSGPACK_NIL) return false;
    reader->offset++;
    return true;
}

bool_t mp_read_bool(MPReader* reader, bool_t* value) {
    if (!reader || !value || reader->offset >= reader->size) return false;
    
    u8 b = reader->buffer[reader->offset++];
    if (b == MSGPACK_TRUE) {
        *value = true;
        return true;
    } else if (b == MSGPACK_FALSE) {
        *value = false;
        return true;
    }
    
    return false;
}

bool_t mp_read_int(MPReader* reader, i64* value) {
    if (!reader || !value || reader->offset >= reader->size) return false;
    
    u8 b = reader->buffer[reader->offset++];
    
    if (b <= 127) {
        *value = (i64)b;
        return true;
    }
    
    if (b >= 224) {
        *value = (i64)(i8)b;
        return true;
    }
    
    switch (b) {
        case MSGPACK_INT8: {
            if (reader->offset >= reader->size) return false;
            *value = (i64)(i8)reader->buffer[reader->offset++];
            return true;
        }
        case MSGPACK_INT16: {
            if (reader->offset + 2 > reader->size) return false;
            i16 v = reader->buffer[reader->offset++] |
                    ((i16)reader->buffer[reader->offset++]) << 8;
            *value = (i64)v;
            return true;
        }
        case MSGPACK_INT32: {
            if (reader->offset + 4 > reader->size) return false;
            i32 v = 0;
            for (int i = 0; i < 4; i++) {
                v |= (i32)reader->buffer[reader->offset++] << (8 * i);
            }
            *value = (i64)v;
            return true;
        }
        case MSGPACK_INT64: {
            if (reader->offset + 8 > reader->size) return false;
            i64 v = 0;
            for (int i = 0; i < 8; i++) {
                v |= (i64)reader->buffer[reader->offset++] << (8 * i);
            }
            *value = v;
            return true;
        }
        case MSGPACK_UINT8: {
            if (reader->offset >= reader->size) return false;
            *value = (i64)reader->buffer[reader->offset++];
            return true;
        }
        case MSGPACK_UINT16: {
            if (reader->offset + 2 > reader->size) return false;
            u16 v = reader->buffer[reader->offset++] |
                    ((u16)reader->buffer[reader->offset++]) << 8;
            *value = (i64)v;
            return true;
        }
        case MSGPACK_UINT32: {
            if (reader->offset + 4 > reader->size) return false;
            u32 v = 0;
            for (int i = 0; i < 4; i++) {
                v |= (u32)reader->buffer[reader->offset++] << (8 * i);
            }
            *value = (i64)v;
            return true;
        }
        case MSGPACK_UINT64: {
            if (reader->offset + 8 > reader->size) return false;
            u64 v = 0;
            for (int i = 0; i < 8; i++) {
                v |= (u64)reader->buffer[reader->offset++] << (8 * i);
            }
            *value = (i64)v;
            return true;
        }
        default:
            return false;
    }
}

bool_t mp_read_uint(MPReader* reader, u64* value) {
    if (!reader || !value || reader->offset >= reader->size) return false;
    
    u8 b = reader->buffer[reader->offset++];
    
    if (b <= 127) {
        *value = (u64)b;
        return true;
    }
    
    switch (b) {
        case MSGPACK_UINT8: {
            if (reader->offset >= reader->size) return false;
            *value = (u64)reader->buffer[reader->offset++];
            return true;
        }
        case MSGPACK_UINT16: {
            if (reader->offset + 2 > reader->size) return false;
            u16 v = reader->buffer[reader->offset++] |
                    ((u16)reader->buffer[reader->offset++]) << 8;
            *value = (u64)v;
            return true;
        }
        case MSGPACK_UINT32: {
            if (reader->offset + 4 > reader->size) return false;
            u32 v = 0;
            for (int i = 0; i < 4; i++) {
                v |= (u32)reader->buffer[reader->offset++] << (8 * i);
            }
            *value = (u64)v;
            return true;
        }
        case MSGPACK_UINT64: {
            if (reader->offset + 8 > reader->size) return false;
            u64 v = 0;
            for (int i = 0; i < 8; i++) {
                v |= (u64)reader->buffer[reader->offset++] << (8 * i);
            }
            *value = v;
            return true;
        }
        default:
            return false;
    }
}

bool_t mp_read_float(MPReader* reader, f32* value) {
    if (!reader || !value || reader->offset >= reader->size) return false;
    
    if (reader->buffer[reader->offset++] != MSGPACK_FLOAT) return false;
    if (reader->offset + 4 > reader->size) return false;
    
    union { f32 f; u32 u; } u;
    u.u = 0;
    for (int i = 0; i < 4; i++) {
        u.u |= (u32)reader->buffer[reader->offset++] << (8 * i);
    }
    *value = u.f;
    return true;
}

bool_t mp_read_double(MPReader* reader, f64* value) {
    if (!reader || !value || reader->offset >= reader->size) return false;
    
    if (reader->buffer[reader->offset++] != MSGPACK_DOUBLE) return false;
    if (reader->offset + 8 > reader->size) return false;
    
    union { f64 f; u64 u; } u;
    u.u = 0;
    for (int i = 0; i < 8; i++) {
        u.u |= (u64)reader->buffer[reader->offset++] << (8 * i);
    }
    *value = u.f;
    return true;
}

bool_t mp_read_string(MPReader* reader, KString* value) {
    if (!reader || !value || reader->offset >= reader->size) return false;
    
    u8 b = reader->buffer[reader->offset++];
    size_t len;
    
    if (b >= MSGPACK_FIXSTR_START && b <= MSGPACK_FIXSTR_END) {
        len = (size_t)(b & 0x1F);
    } else if (b == MSGPACK_STR8) {
        if (reader->offset >= reader->size) return false;
        len = (size_t)reader->buffer[reader->offset++];
    } else if (b == MSGPACK_STR16) {
        if (reader->offset + 2 > reader->size) return false;
        len = (size_t)reader->buffer[reader->offset++] |
              ((size_t)reader->buffer[reader->offset++]) << 8;
    } else if (b == MSGPACK_STR32) {
        if (reader->offset + 4 > reader->size) return false;
        len = 0;
        for (int i = 0; i < 4; i++) {
            len |= (size_t)reader->buffer[reader->offset++] << (8 * i);
        }
    } else {
        return false;
    }
    
    if (reader->offset + len > reader->size) return false;
    
    value->data = (char*)kmm_v4_malloc(len + 1);
    value->len = len;
    memcpy(value->data, reader->buffer + reader->offset, len);
    value->data[len] = '\0';
    reader->offset += len;
    
    return true;
}

bool_t mp_read_u8s(MPReader* reader, u8** data, size_t* len) {
    if (!reader || !data || !len || reader->offset >= reader->size) return false;
    
    u8 b = reader->buffer[reader->offset++];
    size_t length;
    
    if (b == MSGPACK_BIN8) {
        if (reader->offset >= reader->size) return false;
        length = (size_t)reader->buffer[reader->offset++];
    } else if (b == MSGPACK_BIN16) {
        if (reader->offset + 2 > reader->size) return false;
        length = (size_t)reader->buffer[reader->offset++] |
                 ((size_t)reader->buffer[reader->offset++]) << 8;
    } else if (b == MSGPACK_BIN32) {
        if (reader->offset + 4 > reader->size) return false;
        length = 0;
        for (int i = 0; i < 4; i++) {
            length |= (size_t)reader->buffer[reader->offset++] << (8 * i);
        }
    } else {
        return false;
    }
    
    if (reader->offset + length > reader->size) return false;
    
    *data = (u8*)kmm_v4_malloc(length);
    memcpy(*data, reader->buffer + reader->offset, length);
    *len = length;
    reader->offset += length;
    
    return true;
}

bool_t mp_read_array_header(MPReader* reader, size_t* count) {
    if (!reader || !count || reader->offset >= reader->size) return false;
    
    u8 b = reader->buffer[reader->offset++];
    
    if (b >= MSGPACK_FIXARRAY_START && b <= MSGPACK_FIXARRAY_END) {
        *count = (size_t)(b & 0x0F);
        return true;
    }
    
    if (b == MSGPACK_ARRAY16) {
        if (reader->offset + 2 > reader->size) return false;
        *count = (size_t)reader->buffer[reader->offset++] |
                 ((size_t)reader->buffer[reader->offset++]) << 8;
        return true;
    }
    
    if (b == MSGPACK_ARRAY32) {
        if (reader->offset + 4 > reader->size) return false;
        *count = 0;
        for (int i = 0; i < 4; i++) {
            *count |= (size_t)reader->buffer[reader->offset++] << (8 * i);
        }
        return true;
    }
    
    return false;
}

bool_t mp_read_map_header(MPReader* reader, size_t* count) {
    if (!reader || !count || reader->offset >= reader->size) return false;
    
    u8 b = reader->buffer[reader->offset++];
    
    if (b >= MSGPACK_FIXMAP_START && b <= MSGPACK_FIXMAP_END) {
        *count = (size_t)(b & 0x0F);
        return true;
    }
    
    if (b == MSGPACK_MAP16) {
        if (reader->offset + 2 > reader->size) return false;
        *count = (size_t)reader->buffer[reader->offset++] |
                 ((size_t)reader->buffer[reader->offset++]) << 8;
        return true;
    }
    
    if (b == MSGPACK_MAP32) {
        if (reader->offset + 4 > reader->size) return false;
        *count = 0;
        for (int i = 0; i < 4; i++) {
            *count |= (size_t)reader->buffer[reader->offset++] << (8 * i);
        }
        return true;
    }
    
    return false;
}

bool_t mp_read_ext(MPReader* reader, i8* type, u8** data, size_t* len) {
    if (!reader || !type || !data || !len || reader->offset >= reader->size) return false;
    
    u8 b = reader->buffer[reader->offset++];
    size_t length;
    
    switch (b) {
        case MSGPACK_EXT8:
            if (reader->offset >= reader->size) return false;
            length = (size_t)reader->buffer[reader->offset++];
            break;
        case MSGPACK_EXT16:
            if (reader->offset + 2 > reader->size) return false;
            length = (size_t)reader->buffer[reader->offset++] |
                     ((size_t)reader->buffer[reader->offset++]) << 8;
            break;
        case MSGPACK_EXT32:
            if (reader->offset + 4 > reader->size) return false;
            length = 0;
            for (int i = 0; i < 4; i++) {
                length |= (size_t)reader->buffer[reader->offset++] << (8 * i);
            }
            break;
        default:
            return false;
    }
    
    if (reader->offset >= reader->size) return false;
    *type = (i8)reader->buffer[reader->offset++];
    
    if (reader->offset + length > reader->size) return false;
    
    *data = (u8*)kmm_v4_malloc(length);
    memcpy(*data, reader->buffer + reader->offset, length);
    *len = length;
    reader->offset += length;
    
    return true;
}