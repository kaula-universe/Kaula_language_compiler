#ifndef __KAULA_JSON_H__
#define __KAULA_JSON_H__

#include <stdint.h>
#include <stddef.h>
#include "../string/string.h"

// ==================== JSON 类型枚举 ====================

typedef enum {
    JSON_NULL = 0,
    JSON_BOOL,
    JSON_NUMBER,
    JSON_STRING,
    JSON_ARRAY,
    JSON_OBJECT
} JsonType;

// ==================== 联合域结构 ====================

// 数组项
typedef struct {
    struct JsonValue** items;
    size_t size;
    size_t capacity;
} JsonArray;

// 对象键值对
typedef struct {
    String key;
    struct JsonValue* value;
} JsonPair;

// 对象
typedef struct {
    JsonPair* pairs;
    size_t size;
    size_t capacity;
} JsonObject;

// ==================== 联合域主结构 ====================

typedef struct JsonValue {
    JsonType type;
    union {
        // 内联标量值（无额外分配）
        int     bool_val;
        double  number_val;
        char*   string_val;
        // 复合类型（需要额外分配）
        JsonArray*  array_val;
        JsonObject* object_val;
    } value;
} JsonValue;

// ==================== API ====================

// 创建函数
JsonValue* json_create_null(void);
JsonValue* json_create_bool(int value);
JsonValue* json_create_number(double value);
JsonValue* json_create_string(const String value);
JsonValue* json_create_array(void);
JsonValue* json_create_object(void);

// 销毁函数
void json_destroy(JsonValue* value);

// 数组操作
void json_array_append(JsonValue* array, JsonValue* element);
void json_array_set(JsonValue* array, size_t index, JsonValue* element);
JsonValue* json_array_get(JsonValue* array, size_t index);
size_t json_array_size(JsonValue* array);
void json_array_remove(JsonValue* array, size_t index);

// 对象操作
void json_object_set(JsonValue* object, const String key, JsonValue* value);
JsonValue* json_object_get(JsonValue* object, const String key);
void json_object_remove(JsonValue* object, const String key);
int json_object_has(JsonValue* object, const String key);
size_t json_object_size(JsonValue* object);
JsonPair* json_object_pairs(JsonValue* object);

// 类型查询
JsonType json_get_type(JsonValue* value);
int json_is_null(JsonValue* value);
int json_is_bool(JsonValue* value);
int json_is_number(JsonValue* value);
int json_is_string(JsonValue* value);
int json_is_array(JsonValue* value);
int json_is_object(JsonValue* value);

// 值获取
int json_get_bool(JsonValue* value);
double json_get_number(JsonValue* value);
String json_get_string(JsonValue* value);

// 解析函数
JsonValue* json_parse(const String text);
JsonValue* json_parse_file(const String path);

// 序列化函数
String json_serialize(JsonValue* value);
String json_serialize_pretty(JsonValue* value, int indent_level);
int json_to_file(JsonValue* value, const String path);
int json_to_file_pretty(JsonValue* value, const String path);

// 复制函数
JsonValue* json_deep_copy(JsonValue* value);

#endif // __KAULA_JSON_H__
