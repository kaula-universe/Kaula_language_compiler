#ifndef STD_JSON_JSON_H
#define STD_JSON_JSON_H

#include "../base/types.h"
#include "../string/string.h"

typedef enum {
    JSON_NULL,
    JSON_BOOL,
    JSON_NUMBER,
    JSON_STRING,
    JSON_ARRAY,
    JSON_OBJECT
} JsonType;

typedef struct JsonValue JsonValue;
typedef struct JsonPair JsonPair;

typedef struct JsonArray {
    JsonValue** items;
    size_t size;
    size_t capacity;
} JsonArray;

typedef struct JsonObject {
    JsonPair* pairs;
    size_t size;
    size_t capacity;
} JsonObject;

typedef struct JsonValue {
    JsonType type;
    union {
        bool_t bool_val;
        f64 number_val;
        String string_val;
        JsonArray* array_val;
        JsonObject* object_val;
    };
} JsonValue;

typedef struct JsonPair {
    String key;
    JsonValue* value;
} JsonPair;

extern JsonValue* json_create_null();
extern JsonValue* json_create_bool(bool_t value);
extern JsonValue* json_create_number(f64 value);
extern JsonValue* json_create_string(const String value);
extern JsonValue* json_create_array();
extern JsonValue* json_create_object();

extern void json_destroy(JsonValue* value);

extern void json_array_append(JsonValue* array, JsonValue* element);
extern void json_array_set(JsonValue* array, size_t index, JsonValue* element);
extern JsonValue* json_array_get(JsonValue* array, size_t index);
extern size_t json_array_size(JsonValue* array);
extern void json_array_remove(JsonValue* array, size_t index);

extern void json_object_set(JsonValue* object, const String key, JsonValue* value);
extern JsonValue* json_object_get(JsonValue* object, const String key);
extern void json_object_remove(JsonValue* object, const String key);
extern bool_t json_object_has(JsonValue* object, const String key);
extern size_t json_object_size(JsonValue* object);
extern JsonPair* json_object_pairs(JsonValue* object);

extern JsonType json_get_type(JsonValue* value);
extern bool_t json_is_null(JsonValue* value);
extern bool_t json_is_bool(JsonValue* value);
extern bool_t json_is_number(JsonValue* value);
extern bool_t json_is_string(JsonValue* value);
extern bool_t json_is_array(JsonValue* value);
extern bool_t json_is_object(JsonValue* value);

extern bool_t json_get_bool(JsonValue* value);
extern f64 json_get_number(JsonValue* value);
extern String json_get_string(JsonValue* value);

extern JsonValue* json_parse(const String text);
extern JsonValue* json_parse_file(const String path);

extern String json_serialize(JsonValue* value);
extern String json_serialize_pretty(JsonValue* value, int indent_level);

extern bool_t json_to_file(JsonValue* value, const String path);
extern bool_t json_to_file_pretty(JsonValue* value, const String path);

extern JsonValue* json_deep_copy(JsonValue* value);

#endif // STD_JSON_JSON_H
