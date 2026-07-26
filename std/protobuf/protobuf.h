#pragma once
#include "../base/types.h"
#include "../memory/memory.h"
#include "../string/string.h"

typedef struct PBWriter {
    u8* buffer;
    size_t size;
    size_t capacity;
} PBWriter;

typedef struct PBReader {
    const u8* buffer;
    size_t size;
    size_t offset;
} PBReader;

PBWriter* pb_writer_create(void);
void pb_writer_destroy(PBWriter* writer);
void pb_writer_reserve(PBWriter* writer, size_t size);
u8* pb_writer_data(const PBWriter* writer);

void pb_write_varint(PBWriter* writer, i64 value);
void pb_write_fixed32(PBWriter* writer, u32 value);
void pb_write_fixed64(PBWriter* writer, u64 value);
void pb_write_float(PBWriter* writer, f32 value);
void pb_write_double(PBWriter* writer, f64 value);
void pb_write_bool(PBWriter* writer, bool_t value);
void pb_write_string(PBWriter* writer, const char* value);
void pb_write_bytes(PBWriter* writer, const u8* data, size_t len);

void pb_write_field_varint(PBWriter* writer, i32 field_number, i64 value);
void pb_write_field_fixed32(PBWriter* writer, i32 field_number, u32 value);
void pb_write_field_fixed64(PBWriter* writer, i32 field_number, u64 value);
void pb_write_field_string(PBWriter* writer, i32 field_number, const char* value);

PBReader* pb_reader_create(const u8* buffer, size_t size);
void pb_reader_destroy(PBReader* reader);

bool_t pb_read_varint(PBReader* reader, i64* value);
bool_t pb_read_fixed32(PBReader* reader, u32* value);
bool_t pb_read_fixed64(PBReader* reader, u64* value);
bool_t pb_read_float(PBReader* reader, f32* value);
bool_t pb_read_double(PBReader* reader, f64* value);
bool_t pb_read_bool(PBReader* reader, bool_t* value);
bool_t pb_read_string(PBReader* reader, String* value);
bool_t pb_read_bytes(PBReader* reader, u8** data, size_t* len);

bool_t pb_read_field(PBReader* reader, i32* field_number, i32* wire_type);
bool_t pb_skip_field(PBReader* reader, i32 wire_type);

size_t pb_varint_size(i64 value);
size_t pb_string_size(const char* value);