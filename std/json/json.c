#include "json.h"
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <ctype.h>

static JsonValue* json_parse_value(const char** text, int* error);
static String json_serialize_value(JsonValue* value, int indent_level, bool_t pretty);

JsonValue* json_create_null() {
    JsonValue* value = (JsonValue*)calloc(1, sizeof(JsonValue));
    if (value) value->type = JSON_NULL;
    return value;
}

JsonValue* json_create_bool(bool_t value) {
    JsonValue* val = (JsonValue*)calloc(1, sizeof(JsonValue));
    if (val) { val->type = JSON_BOOL; val->bool_val = value; }
    return val;
}

JsonValue* json_create_number(f64 value) {
    JsonValue* val = (JsonValue*)calloc(1, sizeof(JsonValue));
    if (val) { val->type = JSON_NUMBER; val->number_val = value; }
    return val;
}

JsonValue* json_create_string(const String value) {
    JsonValue* val = (JsonValue*)calloc(1, sizeof(JsonValue));
    if (val && value) {
        val->type = JSON_STRING;
        val->string_val = string_copy(value);
    }
    return val;
}

JsonValue* json_create_array() {
    JsonValue* val = (JsonValue*)calloc(1, sizeof(JsonValue));
    if (val) {
        val->type = JSON_ARRAY;
        val->array_val = (JsonArray*)calloc(1, sizeof(JsonArray));
        if (val->array_val) val->array_val->capacity = 8;
    }
    return val;
}

JsonValue* json_create_object() {
    JsonValue* val = (JsonValue*)calloc(1, sizeof(JsonValue));
    if (val) {
        val->type = JSON_OBJECT;
        val->object_val = (JsonObject*)calloc(1, sizeof(JsonObject));
        if (val->object_val) val->object_val->capacity = 8;
    }
    return val;
}

void json_destroy(JsonValue* value) {
    if (!value) return;
    if (value->type == JSON_STRING) {
        string_free(value->string_val);
    } else if (value->type == JSON_ARRAY) {
        if (value->array_val) {
            for (size_t i = 0; i < value->array_val->size; i++) {
                json_destroy(value->array_val->items[i]);
            }
            free(value->array_val->items);
            free(value->array_val);
        }
    } else if (value->type == JSON_OBJECT) {
        if (value->object_val) {
            for (size_t i = 0; i < value->object_val->size; i++) {
                string_free(value->object_val->pairs[i].key);
                json_destroy(value->object_val->pairs[i].value);
            }
            free(value->object_val->pairs);
            free(value->object_val);
        }
    }
    free(value);
}

void json_array_append(JsonValue* array, JsonValue* element) {
    if (!array || !element || array->type != JSON_ARRAY) return;
    if (array->array_val->size >= array->array_val->capacity) {
        array->array_val->capacity *= 2;
        array->array_val->items = (JsonValue**)realloc(array->array_val->items, array->array_val->capacity * sizeof(JsonValue*));
    }
    array->array_val->items[array->array_val->size++] = element;
}

void json_array_set(JsonValue* array, size_t index, JsonValue* element) {
    if (!array || array->type != JSON_ARRAY || index >= array->array_val->size) return;
    json_destroy(array->array_val->items[index]);
    array->array_val->items[index] = element;
}

JsonValue* json_array_get(JsonValue* array, size_t index) {
    if (!array || array->type != JSON_ARRAY || index >= array->array_val->size) return NULL;
    return array->array_val->items[index];
}

size_t json_array_size(JsonValue* array) {
    if (!array || array->type != JSON_ARRAY) return 0;
    return array->array_val->size;
}

void json_array_remove(JsonValue* array, size_t index) {
    if (!array || array->type != JSON_ARRAY || index >= array->array_val->size) return;
    json_destroy(array->array_val->items[index]);
    memmove(&array->array_val->items[index], &array->array_val->items[index + 1], (array->array_val->size - index - 1) * sizeof(JsonValue*));
    array->array_val->size--;
}

void json_object_set(JsonValue* object, const String key, JsonValue* value) {
    if (!object || !key || !value || object->type != JSON_OBJECT) return;
    JsonObject* obj = object->object_val;
    for (size_t i = 0; i < obj->size; i++) {
        if (string_equals(obj->pairs[i].key, key)) {
            json_destroy(obj->pairs[i].value);
            obj->pairs[i].value = value;
            return;
        }
    }
    if (obj->size >= obj->capacity) {
        obj->capacity *= 2;
        obj->pairs = (JsonPair*)realloc(obj->pairs, obj->capacity * sizeof(JsonPair));
    }
    obj->pairs[obj->size].key = string_copy(key);
    obj->pairs[obj->size].value = value;
    obj->size++;
}

JsonValue* json_object_get(JsonValue* object, const String key) {
    if (!object || !key || object->type != JSON_OBJECT) return NULL;
    JsonObject* obj = object->object_val;
    for (size_t i = 0; i < obj->size; i++) {
        if (string_equals(obj->pairs[i].key, key)) return obj->pairs[i].value;
    }
    return NULL;
}

void json_object_remove(JsonValue* object, const String key) {
    if (!object || !key || object->type != JSON_OBJECT) return;
    JsonObject* obj = object->object_val;
    for (size_t i = 0; i < obj->size; i++) {
        if (string_equals(obj->pairs[i].key, key)) {
            string_free(obj->pairs[i].key);
            json_destroy(obj->pairs[i].value);
            memmove(&obj->pairs[i], &obj->pairs[i + 1], (obj->size - i - 1) * sizeof(JsonPair));
            obj->size--;
            return;
        }
    }
}

bool_t json_object_has(JsonValue* object, const String key) {
    return json_object_get(object, key) != NULL;
}

size_t json_object_size(JsonValue* object) {
    if (!object || object->type != JSON_OBJECT) return 0;
    return object->object_val->size;
}

JsonPair* json_object_pairs(JsonValue* object) {
    if (!object || object->type != JSON_OBJECT) return NULL;
    return object->object_val->pairs;
}

JsonType json_get_type(JsonValue* value) {
    return value ? value->type : JSON_NULL;
}

bool_t json_is_null(JsonValue* value) { return json_get_type(value) == JSON_NULL; }
bool_t json_is_bool(JsonValue* value) { return json_get_type(value) == JSON_BOOL; }
bool_t json_is_number(JsonValue* value) { return json_get_type(value) == JSON_NUMBER; }
bool_t json_is_string(JsonValue* value) { return json_get_type(value) == JSON_STRING; }
bool_t json_is_array(JsonValue* value) { return json_get_type(value) == JSON_ARRAY; }
bool_t json_is_object(JsonValue* value) { return json_get_type(value) == JSON_OBJECT; }

bool_t json_get_bool(JsonValue* value) { return (value && value->type == JSON_BOOL) ? value->bool_val : false; }
f64 json_get_number(JsonValue* value) { return (value && value->type == JSON_NUMBER) ? value->number_val : 0.0; }
String json_get_string(JsonValue* value) { return (value && value->type == JSON_STRING) ? value->string_val : NULL; }

static void json_skip_whitespace(const char** text) {
    while (**text && isspace((unsigned char)**text)) (*text)++;
}

static String json_parse_string_content(const char** text, int* error) {
    if (**text != '"') { *error = 1; return NULL; }
    (*text)++;
    const char* start = *text;
    size_t capacity = 64;
    String result = (String)malloc(capacity);
    size_t len = 0;
    while (**text && **text != '"') {
        if (**text == '\\' && *(*text + 1)) {
            (*text)++;
            char c;
            switch (**text) {
                case '"': c = '"'; break;
                case '\\': c = '\\'; break;
                case '/': c = '/'; break;
                case 'n': c = '\n'; break;
                case 'r': c = '\r'; break;
                case 't': c = '\t'; break;
                case 'b': c = '\b'; break;
                case 'f': c = '\f'; break;
                default: c = **text; break;
            }
            if (len + 2 > capacity) { capacity *= 2; result = (String)realloc(result, capacity); }
            result[len++] = c;
        } else {
            if (len + 2 > capacity) { capacity *= 2; result = (String)realloc(result, capacity); }
            result[len++] = **text;
        }
        (*text)++;
    }
    if (**text != '"') { *error = 1; free(result); return NULL; }
    (*text)++;
    result[len] = '\0';
    return result;
}

static JsonValue* json_parse_value(const char** text, int* error) {
    json_skip_whitespace(text);
    if (!**text) { *error = 1; return NULL; }
    if (**text == 'n') {
        if (strncmp(*text, "null", 4) == 0) { (*text) += 4; return json_create_null(); }
        *error = 1; return NULL;
    }
    if (**text == 't') {
        if (strncmp(*text, "true", 4) == 0) { (*text) += 4; return json_create_bool(true); }
        *error = 1; return NULL;
    }
    if (**text == 'f') {
        if (strncmp(*text, "false", 5) == 0) { (*text) += 5; return json_create_bool(false); }
        *error = 1; return NULL;
    }
    if (**text == '"') {
        String s = json_parse_string_content(text, error);
        if (*error) return NULL;
        JsonValue* val = json_create_string(s);
        free(s);
        return val;
    }
    if (**text == '[') {
        (*text)++;
        JsonValue* arr = json_create_array();
        json_skip_whitespace(text);
        if (**text == ']') { (*text)++; return arr; }
        while (1) {
            JsonValue* item = json_parse_value(text, error);
            if (*error) { json_destroy(arr); return NULL; }
            json_array_append(arr, item);
            json_skip_whitespace(text);
            if (**text == ',') { (*text)++; continue; }
            if (**text == ']') { (*text)++; break; }
            *error = 1; json_destroy(arr); return NULL;
        }
        return arr;
    }
    if (**text == '{') {
        (*text)++;
        JsonValue* obj = json_create_object();
        json_skip_whitespace(text);
        if (**text == '}') { (*text)++; return obj; }
        while (1) {
            json_skip_whitespace(text);
            String key = json_parse_string_content(text, error);
            if (*error) { json_destroy(obj); return NULL; }
            json_skip_whitespace(text);
            if (**text != ':') { *error = 1; string_free(key); json_destroy(obj); return NULL; }
            (*text)++;
            JsonValue* val = json_parse_value(text, error);
            if (*error) { string_free(key); json_destroy(obj); return NULL; }
            json_object_set(obj, key, val);
            string_free(key);
            json_skip_whitespace(text);
            if (**text == ',') { (*text)++; continue; }
            if (**text == '}') { (*text)++; break; }
            *error = 1; json_destroy(obj); return NULL;
        }
        return obj;
    }
    const char* num_start = *text;
    while (**text && (isdigit((unsigned char)**text) || **text == '.' || **text == '-' || **text == '+' || **text == 'e' || **text == 'E')) (*text)++;
    char* num_str = (char*)malloc(*text - num_start + 1);
    memcpy(num_str, num_start, *text - num_start);
    num_str[*text - num_start] = '\0';
    f64 num = atof(num_str);
    free(num_str);
    return json_create_number(num);
}

JsonValue* json_parse(const String text) {
    if (!text) return NULL;
    const char* p = text;
    int error = 0;
    JsonValue* val = json_parse_value(&p, &error);
    if (error) { json_destroy(val); return NULL; }
    return val;
}

JsonValue* json_parse_file(const String path) {
    FILE* f = fopen(path, "rb");
    if (!f) return NULL;
    fseek(f, 0, SEEK_END);
    long len = ftell(f);
    fseek(f, 0, SEEK_SET);
    String buf = (String)malloc(len + 1);
    fread(buf, 1, len, f);
    buf[len] = '\0';
    fclose(f);
    JsonValue* val = json_parse(buf);
    free(buf);
    return val;
}

static void json_write_escape(FILE* f, const String s) {
    fputc('"', f);
    for (size_t i = 0; s[i]; i++) {
        switch (s[i]) {
            case '"': fputs("\\\"", f); break;
            case '\\': fputs("\\\\", f); break;
            case '\n': fputs("\\n", f); break;
            case '\r': fputs("\\r", f); break;
            case '\t': fputs("\\t", f); break;
            case '\b': fputs("\\b", f); break;
            case '\f': fputs("\\f", f); break;
            default: fputc(s[i], f); break;
        }
    }
    fputc('"', f);
}

static String json_serialize_value(JsonValue* value, int indent_level, bool_t pretty) {
    String result = (String)malloc(1024);
    size_t capacity = 1024;
    size_t len = 0;
    #define APPEND(fmt, ...) do { \
        int n = snprintf(NULL, 0, fmt, ##__VA_ARGS__); \
        if (len + n + 1 > capacity) { while (len + n + 1 > capacity) capacity *= 2; result = (String)realloc(result, capacity); } \
        n = snprintf(result + len, capacity - len, fmt, ##__VA_ARGS__); \
        len += n; \
    } while(0)
    
    if (!value) { APPEND("null"); }
    else switch (value->type) {
        case JSON_NULL: APPEND("null"); break;
        case JSON_BOOL: APPEND(value->bool_val ? "true" : "false"); break;
        case JSON_NUMBER: {
            if (value->number_val == (i64)value->number_val) APPEND("%lld", (i64)value->number_val);
            else APPEND("%lf", value->number_val);
            break;
        }
        case JSON_STRING: json_write_escape(stdout, value->string_val); break;
        case JSON_ARRAY: {
            APPEND("[");
            for (size_t i = 0; i < value->array_val->size; i++) {
                if (i > 0) APPEND(",");
                if (pretty) APPEND("\n%*s", (indent_level + 1) * 2, "");
                String item = json_serialize_value(value->array_val->items[i], indent_level + 1, pretty);
                APPEND("%s", item); string_free(item);
            }
            if (pretty && value->array_val->size > 0) APPEND("\n%*s", indent_level * 2, "");
            APPEND("]");
            break;
        }
        case JSON_OBJECT: {
            APPEND("{");
            for (size_t i = 0; i < value->object_val->size; i++) {
                if (i > 0) APPEND(",");
                if (pretty) APPEND("\n%*s", (indent_level + 1) * 2, "");
                json_write_escape(stdout, value->object_val->pairs[i].key);
                APPEND(":");
                if (pretty) APPEND(" ");
                String v = json_serialize_value(value->object_val->pairs[i].value, indent_level + 1, pretty);
                APPEND("%s", v); string_free(v);
            }
            if (pretty && value->object_val->size > 0) APPEND("\n%*s", indent_level * 2, "");
            APPEND("}");
            break;
        }
    }
    #undef APPEND
    result[len] = '\0';
    return result;
}

String json_serialize(JsonValue* value) { return json_serialize_value(value, 0, false); }
String json_serialize_pretty(JsonValue* value, int indent_level) { return json_serialize_value(value, indent_level, true); }

bool_t json_to_file(JsonValue* value, const String path) {
    FILE* f = fopen(path, "w");
    if (!f) return false;
    String s = json_serialize(value);
    fputs(s, f);
    string_free(s);
    fclose(f);
    return true;
}

bool_t json_to_file_pretty(JsonValue* value, const String path) {
    FILE* f = fopen(path, "w");
    if (!f) return false;
    String s = json_serialize_pretty(value, 0);
    fputs(s, f);
    fputc('\n', f);
    string_free(s);
    fclose(f);
    return true;
}

JsonValue* json_deep_copy(JsonValue* value) {
    if (!value) return NULL;
    switch (value->type) {
        case JSON_NULL: return json_create_null();
        case JSON_BOOL: return json_create_bool(value->bool_val);
        case JSON_NUMBER: return json_create_number(value->number_val);
        case JSON_STRING: return json_create_string(value->string_val);
        case JSON_ARRAY: {
            JsonValue* arr = json_create_array();
            for (size_t i = 0; i < value->array_val->size; i++) {
                json_array_append(arr, json_deep_copy(value->array_val->items[i]));
            }
            return arr;
        }
        case JSON_OBJECT: {
            JsonValue* obj = json_create_object();
            for (size_t i = 0; i < value->object_val->size; i++) {
                json_object_set(obj, value->object_val->pairs[i].key, json_deep_copy(value->object_val->pairs[i].value));
            }
            return obj;
        }
    }
    return NULL;
}
