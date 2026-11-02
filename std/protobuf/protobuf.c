#include "protobuf.h"
#include <stdlib.h>
#include <string.h>
#include <math.h>

PBWriter* pb_writer_create(void) {
    PBWriter* w = (PBWriter*)kmm_v4_malloc(sizeof(PBWriter));
    if (!w) return NULL;
    w->buffer = NULL;
    w->size = 0;
    w->capacity = 0;
    return w;
}

void pb_writer_destroy(PBWriter* writer) {
    if (!writer) return;
    kmm_v4_free(writer->buffer);
    kmm_v4_free(writer);
}

void pb_writer_reserve(PBWriter* writer, size_t size) {
    if (!writer) return;
    if (writer->capacity < writer->size + size) {
        writer->capacity = (writer->size + size) * 2;
        writer->buffer = (u8*)kmm_v4_realloc(writer->buffer, writer->capacity);
    }
}

u8* pb_writer_data(const PBWriter* writer) {
    return writer ? writer->buffer : NULL;
}

void pb_write_varint(PBWriter* writer, i64 value) {
    if (!writer) return;
    
    pb_writer_reserve(writer, 10);
    
    while (true) {
        u8 b = value & 0x7F;
        value >>= 7;
        if (value != 0) {
            b |= 0x80;
        }
        writer->buffer[writer->size++] = b;
        if (value == 0) break;
    }
}

void pb_write_fixed32(PBWriter* writer, u32 value) {
    if (!writer) return;
    
    pb_writer_reserve(writer, 4);
    
    for (int i = 0; i < 4; i++) {
        writer->buffer[writer->size++] = (u8)(value >> (8 * i));
    }
}

void pb_write_fixed64(PBWriter* writer, u64 value) {
    if (!writer) return;
    
    pb_writer_reserve(writer, 8);
    
    for (int i = 0; i < 8; i++) {
        writer->buffer[writer->size++] = (u8)(value >> (8 * i));
    }
}

void pb_write_float(PBWriter* writer, f32 value) {
    if (!writer) return;
    union { f32 f; u32 u; } u;
    u.f = value;
    pb_write_fixed32(writer, u.u);
}

void pb_write_double(PBWriter* writer, f64 value) {
    if (!writer) return;
    union { f64 f; u64 u; } u;
    u.f = value;
    pb_write_fixed64(writer, u.u);
}

void pb_write_bool(PBWriter* writer, bool_t value) {
    if (!writer) return;
    pb_writer_reserve(writer, 1);
    writer->buffer[writer->size++] = value ? 1 : 0;
}

void pb_write_string(PBWriter* writer, const char* value) {
    if (!writer || !value) return;
    
    size_t len = strlen(value);
    pb_write_varint(writer, (i64)len);
    pb_writer_reserve(writer, len);
    memcpy(writer->buffer + writer->size, value, len);
    writer->size += len;
}

void pb_write_u8s(PBWriter* writer, const u8* data, size_t len) {
    if (!writer || !data) return;
    
    pb_write_varint(writer, (i64)len);
    pb_writer_reserve(writer, len);
    memcpy(writer->buffer + writer->size, data, len);
    writer->size += len;
}

void pb_write_field_varint(PBWriter* writer, i32 field_number, i64 value) {
    i64 tag = ((i64)field_number << 3) | 0;
    pb_write_varint(writer, tag);
    pb_write_varint(writer, value);
}

void pb_write_field_fixed32(PBWriter* writer, i32 field_number, u32 value) {
    i64 tag = ((i64)field_number << 3) | 5;
    pb_write_varint(writer, tag);
    pb_write_fixed32(writer, value);
}

void pb_write_field_fixed64(PBWriter* writer, i32 field_number, u64 value) {
    i64 tag = ((i64)field_number << 3) | 1;
    pb_write_varint(writer, tag);
    pb_write_fixed64(writer, value);
}

void pb_write_field_string(PBWriter* writer, i32 field_number, const char* value) {
    i64 tag = ((i64)field_number << 3) | 2;
    pb_write_varint(writer, tag);
    pb_write_string(writer, value);
}

PBReader* pb_reader_create(const u8* buffer, size_t size) {
    PBReader* r = (PBReader*)kmm_v4_malloc(sizeof(PBReader));
    if (!r) return NULL;
    r->buffer = buffer;
    r->size = size;
    r->offset = 0;
    return r;
}

void pb_reader_destroy(PBReader* reader) {
    kmm_v4_free(reader);
}

bool_t pb_read_varint(PBReader* reader, i64* value) {
    if (!reader || !value) return false;
    if (reader->offset >= reader->size) return false;
    
    *value = 0;
    int shift = 0;
    
    while (reader->offset < reader->size) {
        u8 b = reader->buffer[reader->offset++];
        *value |= ((i64)(b & 0x7F)) << shift;
        shift += 7;
        if (!(b & 0x80)) break;
    }
    
    return true;
}

bool_t pb_read_fixed32(PBReader* reader, u32* value) {
    if (!reader || !value) return false;
    if (reader->offset + 4 > reader->size) return false;
    
    *value = 0;
    for (int i = 0; i < 4; i++) {
        *value |= (u32)reader->buffer[reader->offset++] << (8 * i);
    }
    
    return true;
}

bool_t pb_read_fixed64(PBReader* reader, u64* value) {
    if (!reader || !value) return false;
    if (reader->offset + 8 > reader->size) return false;
    
    *value = 0;
    for (int i = 0; i < 8; i++) {
        *value |= (u64)reader->buffer[reader->offset++] << (8 * i);
    }
    
    return true;
}

bool_t pb_read_float(PBReader* reader, f32* value) {
    if (!reader || !value) return false;
    union { f32 f; u32 u; } u;
    if (!pb_read_fixed32(reader, &u.u)) return false;
    *value = u.f;
    return true;
}

bool_t pb_read_double(PBReader* reader, f64* value) {
    if (!reader || !value) return false;
    union { f64 f; u64 u; } u;
    if (!pb_read_fixed64(reader, &u.u)) return false;
    *value = u.f;
    return true;
}

bool_t pb_read_bool(PBReader* reader, bool_t* value) {
    if (!reader || !value) return false;
    if (reader->offset >= reader->size) return false;
    
    *value = reader->buffer[reader->offset++] != 0;
    return true;
}

bool_t pb_read_string(PBReader* reader, String* value) {
    if (!reader || !value) return false;
    
    i64 len;
    if (!pb_read_varint(reader, &len)) return false;
    
    if (reader->offset + (size_t)len > reader->size) return false;
    
    value->ptr = (char*)kmm_v4_malloc((size_t)len + 1);
    value->len = (size_t)len;
    memcpy(value->ptr, reader->buffer + reader->offset, (size_t)len);
    value->ptr[(size_t)len] = '\0';
    reader->offset += (size_t)len;
    
    return true;
}

bool_t pb_read_u8s(PBReader* reader, u8** data, size_t* len) {
    if (!reader || !data || !len) return false;
    
    i64 length;
    if (!pb_read_varint(reader, &length)) return false;
    
    if (reader->offset + (size_t)length > reader->size) return false;
    
    *data = (u8*)kmm_v4_malloc((size_t)length);
    memcpy(*data, reader->buffer + reader->offset, (size_t)length);
    *len = (size_t)length;
    reader->offset += (size_t)length;
    
    return true;
}

bool_t pb_read_field(PBReader* reader, i32* field_number, i32* wire_type) {
    if (!reader || !field_number || !wire_type) return false;
    
    i64 tag;
    if (!pb_read_varint(reader, &tag)) return false;
    
    *field_number = (i32)(tag >> 3);
    *wire_type = (i32)(tag & 7);
    
    return true;
}

bool_t pb_skip_field(PBReader* reader, i32 wire_type) {
    if (!reader) return false;
    
    switch (wire_type) {
        case 0: {
            i64 dummy;
            return pb_read_varint(reader, &dummy);
        }
        case 1: {
            reader->offset += 8;
            return reader->offset <= reader->size;
        }
        case 2: {
            i64 len;
            if (!pb_read_varint(reader, &len)) return false;
            reader->offset += (size_t)len;
            return reader->offset <= reader->size;
        }
        case 5: {
            reader->offset += 4;
            return reader->offset <= reader->size;
        }
        default:
            return false;
    }
}

size_t pb_varint_size(i64 value) {
    size_t size = 0;
    do {
        size++;
        value >>= 7;
    } while (value != 0);
    return size;
}

size_t pb_string_size(const char* value) {
    if (!value) return 0;
    size_t len = strlen(value);
    return pb_varint_size((i64)len) + len;
}