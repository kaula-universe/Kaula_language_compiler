#pragma once
#include "../base/types.h"
#include "../string/string.h"

typedef enum {
    SER_NULL,
    SER_BOOL,
    SER_INT8, SER_INT16, SER_INT32, SER_INT64,
    SER_UINT8, SER_UINT16, SER_UINT32, SER_UINT64,
    SER_FLOAT32, SER_FLOAT64,
    SER_STRING,
    SER_BINARY,
    SER_ARRAY,
    SER_MAP,
    SER_TIMESTAMP
} SerializeType;

typedef struct Serializer Serializer;
typedef struct Deserializer Deserializer;

// Serializer (write)
Serializer* serializer_create(void);
void        serializer_destroy(Serializer* s);
void        serializer_write_bool(Serializer* s, bool_t val);
void        serializer_write_i8(Serializer* s, i8 val);
void        serializer_write_i16(Serializer* s, i16 val);
void        serializer_write_i32(Serializer* s, i32 val);
void        serializer_write_i64(Serializer* s, i64 val);
void        serializer_write_u8(Serializer* s, u8 val);
void        serializer_write_u16(Serializer* s, u16 val);
void        serializer_write_u32(Serializer* s, u32 val);
void        serializer_write_u64(Serializer* s, u64 val);
void        serializer_write_f32(Serializer* s, f32 val);
void        serializer_write_f64(Serializer* s, f64 val);
void        serializer_write_string(Serializer* s, const char* str);
void        serializer_write_binary(Serializer* s, const void* data, size_t len);
void        serializer_write_null(Serializer* s);
void        serializer_begin_array(Serializer* s, size_t count);
void        serializer_begin_map(Serializer* s, size_t count);
void        serializer_write_key(Serializer* s, const char* key);
void        serializer_write_tag(Serializer* s, const char* tag);
u8*         serializer_get_buffer(Serializer* s, size_t* out_len);

// Deserializer (read)
Deserializer* deserializer_create(const u8* data, size_t len);
void          deserializer_destroy(Deserializer* d);
bool_t        deserializer_read_bool(Deserializer* d);
i8   deserializer_read_i8(Deserializer* d);
i16  deserializer_read_i16(Deserializer* d);
i32  deserializer_read_i32(Deserializer* d);
i64  deserializer_read_i64(Deserializer* d);
u8   deserializer_read_u8(Deserializer* d);
u16  deserializer_read_u16(Deserializer* d);
u32  deserializer_read_u32(Deserializer* d);
u64  deserializer_read_u64(Deserializer* d);
f32  deserializer_read_f32(Deserializer* d);
f64  deserializer_read_f64(Deserializer* d);
String deserializer_read_string(Deserializer* d);
void*  deserializer_read_binary(Deserializer* d, size_t* out_len);
SerializeType deserializer_peek_type(Deserializer* d);
size_t        deserializer_begin_array(Deserializer* d);
size_t        deserializer_begin_map(Deserializer* d);
String        deserializer_read_key(Deserializer* d);
String        deserializer_read_tag(Deserializer* d);
bool_t        deserializer_is_null(Deserializer* d);
bool_t        deserializer_at_end(Deserializer* d);

// Convenience: serialize to/from file
bool_t serializer_to_file(Serializer* s, const char* path);
Deserializer* deserializer_from_file(const char* path);
