#pragma once
#include "../base/types.h"
#include "../memory/memory.h"
#include "../string/string.h"

typedef enum {
    MSGPACK_NIL = 0xC0,
    MSGPACK_FALSE = 0xC2,
    MSGPACK_TRUE = 0xC3,
    MSGPACK_FLOAT = 0xCA,
    MSGPACK_DOUBLE = 0xCB,
    MSGPACK_INT8 = 0xD0,
    MSGPACK_INT16 = 0xD1,
    MSGPACK_INT32 = 0xD2,
    MSGPACK_INT64 = 0xD3,
    MSGPACK_UINT8 = 0xCC,
    MSGPACK_UINT16 = 0xCD,
    MSGPACK_UINT32 = 0xCE,
    MSGPACK_UINT64 = 0xCF,
    MSGPACK_FIXSTR_START = 0xA0,
    MSGPACK_FIXSTR_END = 0xBF,
    MSGPACK_STR8 = 0xD9,
    MSGPACK_STR16 = 0xDA,
    MSGPACK_STR32 = 0xDB,
    MSGPACK_FIXARRAY_START = 0x90,
    MSGPACK_FIXARRAY_END = 0x9F,
    MSGPACK_ARRAY16 = 0xDC,
    MSGPACK_ARRAY32 = 0xDD,
    MSGPACK_FIXMAP_START = 0x80,
    MSGPACK_FIXMAP_END = 0x8F,
    MSGPACK_MAP16 = 0xDE,
    MSGPACK_MAP32 = 0xDF,
    MSGPACK_BIN8 = 0xC4,
    MSGPACK_BIN16 = 0xC5,
    MSGPACK_BIN32 = 0xC6,
    MSGPACK_EXT8 = 0xC7,
    MSGPACK_EXT16 = 0xC8,
    MSGPACK_EXT32 = 0xC9,
} MsgPackType;

typedef struct MPWriter {
    u8* buffer;
    size_t size;
    size_t capacity;
} MPWriter;

typedef struct MPReader {
    const u8* buffer;
    size_t size;
    size_t offset;
} MPReader;

MPWriter* mp_writer_create(void);
void mp_writer_destroy(MPWriter* writer);
u8* mp_writer_data(const MPWriter* writer);

void mp_write_nil(MPWriter* writer);
void mp_write_bool(MPWriter* writer, bool_t value);
void mp_write_int(MPWriter* writer, i64 value);
void mp_write_uint(MPWriter* writer, u64 value);
void mp_write_float(MPWriter* writer, f32 value);
void mp_write_double(MPWriter* writer, f64 value);
void mp_write_string(MPWriter* writer, const char* value);
void mp_write_bytes(MPWriter* writer, const u8* data, size_t len);

void mp_write_array_header(MPWriter* writer, size_t count);
void mp_write_map_header(MPWriter* writer, size_t count);

void mp_write_ext(MPWriter* writer, i8 type, const u8* data, size_t len);

MPReader* mp_reader_create(const u8* buffer, size_t size);
void mp_reader_destroy(MPReader* reader);

MsgPackType mp_read_type(MPReader* reader);

bool_t mp_read_nil(MPReader* reader);
bool_t mp_read_bool(MPReader* reader, bool_t* value);
bool_t mp_read_int(MPReader* reader, i64* value);
bool_t mp_read_uint(MPReader* reader, u64* value);
bool_t mp_read_float(MPReader* reader, f32* value);
bool_t mp_read_double(MPReader* reader, f64* value);
bool_t mp_read_string(MPReader* reader, KString* value);
bool_t mp_read_bytes(MPReader* reader, u8** data, size_t* len);

bool_t mp_read_array_header(MPReader* reader, size_t* count);
bool_t mp_read_map_header(MPReader* reader, size_t* count);

bool_t mp_read_ext(MPReader* reader, i8* type, u8** data, size_t* len);