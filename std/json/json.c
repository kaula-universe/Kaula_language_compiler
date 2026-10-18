#include "json.h"
#include "../memory/memory.h"
#include <string.h>
#include <stdio.h>
#include <ctype.h>
#include <stdlib.h>

static JsonValue* json_parse_value(const char** text, int* error);
static String json_serialize_value(JsonValue* value, int indent_level, int pretty);

JsonValue* json_create_null() {
    JsonValue* value = (JsonValue*)kmm_v4_calloc(1, sizeof(JsonValue));
    if (value) value->type = JSON_NULL;
    return value;
}

JsonValue* json_create_bool(int value) {
    JsonValue* val = (JsonValue*)kmm_v4_calloc(1, sizeof(JsonValue));
    if (val) { val->type = JSON_BOOL; val->value.bool_val = value; }
    return val;
}

JsonValue* json_create_number(double value) {
    JsonValue* val = (JsonValue*)kmm_v4_calloc(1, sizeof(JsonValue));
    if (val) { val->type = JSON_NUMBER; val->value.number_val = value; }
    return val;
}

JsonValue* json_create_string(const String value) {
    JsonValue* val = (JsonValue*)kmm_v4_calloc(1, sizeof(JsonValue));
    if (val && value.ptr) {
        val->type = JSON_STRING;
        val->value.string_val = string_copy(value).ptr;
    }
    return val;
}

JsonValue* json_create_array() {
    JsonValue* val = (JsonValue*)kmm_v4_calloc(1, sizeof(JsonValue));
    if (val) {
        val->type = JSON_ARRAY;
        val->value.array_val = (JsonArray*)kmm_v4_calloc(1, sizeof(JsonArray));
        if (val->value.array_val) {
            val->value.array_val->capacity = 8;
            val->value.array_val->items = (JsonValue**)kmm_v4_calloc(val->value.array_val->capacity, sizeof(JsonValue*));
        }
    }
    return val;
}

JsonValue* json_create_object() {
    JsonValue* val = (JsonValue*)kmm_v4_calloc(1, sizeof(JsonValue));
    if (val) {
        val->type = JSON_OBJECT;
        val->value.object_val = (JsonObject*)kmm_v4_calloc(1, sizeof(JsonObject));
        if (val->value.object_val) {
            val->value.object_val->capacity = 8;
            val->value.object_val->pairs = (JsonPair*)kmm_v4_calloc(val->value.object_val->capacity, sizeof(JsonPair));
        }
    }
    return val;
}

void json_destroy(JsonValue* value) {
    if (!value) return;
    switch (value->type) {
        case JSON_STRING:
            /* string_val is char*; kmm_v4_free is a no-op (bump allocator) */
            break;
        case JSON_ARRAY:
            if (value->value.array_val) {
                for (size_t i = 0; i < value->value.array_val->size; i++) {
                    json_destroy(value->value.array_val->items[i]);
                }
                kmm_v4_free(value->value.array_val->items);
                kmm_v4_free(value->value.array_val);
            }
            break;
        case JSON_OBJECT:
            if (value->value.object_val) {
                for (size_t i = 0; i < value->value.object_val->size; i++) {
                    string_free(value->value.object_val->pairs[i].key);
                    json_destroy(value->value.object_val->pairs[i].value);
                }
                kmm_v4_free(value->value.object_val->pairs);
                kmm_v4_free(value->value.object_val);
            }
            break;
        default:
            break;
    }
    kmm_v4_free(value);
}

void json_array_append(JsonValue* array, JsonValue* element) {
    if (!array || !element || array->type != JSON_ARRAY) return;
    JsonArray* arr = array->value.array_val;
    if (arr->size >= arr->capacity) {
        arr->capacity *= 2;
        arr->items = (JsonValue**)kmm_v4_realloc(arr->items, arr->capacity * sizeof(JsonValue*));
    }
    arr->items[arr->size++] = element;
}

void json_array_set(JsonValue* array, size_t index, JsonValue* element) {
    if (!array || array->type != JSON_ARRAY || index >= array->value.array_val->size) return;
    json_destroy(array->value.array_val->items[index]);
    array->value.array_val->items[index] = element;
}

JsonValue* json_array_get(JsonValue* array, size_t index) {
    if (!array || array->type != JSON_ARRAY || index >= array->value.array_val->size) return NULL;
    return array->value.array_val->items[index];
}

size_t json_array_size(JsonValue* array) {
    if (!array || array->type != JSON_ARRAY) return 0;
    return array->value.array_val->size;
}

void json_array_remove(JsonValue* array, size_t index) {
    if (!array || array->type != JSON_ARRAY || index >= array->value.array_val->size) return;
    json_destroy(array->value.array_val->items[index]);
    JsonArray* arr = array->value.array_val;
    memmove(&arr->items[index], &arr->items[index + 1], (arr->size - index - 1) * sizeof(JsonValue*));
    arr->size--;
}

void json_object_set(JsonValue* object, const String key, JsonValue* value) {
    if (!object || !key.ptr || !value || object->type != JSON_OBJECT) return;
    JsonObject* obj = object->value.object_val;
    for (size_t i = 0; i < obj->size; i++) {
        if (strcmp(obj->pairs[i].key.ptr, key.ptr) == 0) {
            json_destroy(obj->pairs[i].value);
            obj->pairs[i].value = value;
            return;
        }
    }
    if (obj->size >= obj->capacity) {
        obj->capacity *= 2;
        obj->pairs = (JsonPair*)kmm_v4_realloc(obj->pairs, obj->capacity * sizeof(JsonPair));
    }
    obj->pairs[obj->size].key = string_copy(key);
    obj->pairs[obj->size].value = value;
    obj->size++;
}

JsonValue* json_object_get(JsonValue* object, const String key) {
    if (!object || !key.ptr || object->type != JSON_OBJECT) return NULL;
    JsonObject* obj = object->value.object_val;
    for (size_t i = 0; i < obj->size; i++) {
        if (strcmp(obj->pairs[i].key.ptr, key.ptr) == 0) return obj->pairs[i].value;
    }
    return NULL;
}

void json_object_remove(JsonValue* object, const String key) {
    if (!object || !key.ptr || object->type != JSON_OBJECT) return;
    JsonObject* obj = object->value.object_val;
    for (size_t i = 0; i < obj->size; i++) {
        if (strcmp(obj->pairs[i].key.ptr, key.ptr) == 0) {
            string_free(obj->pairs[i].key);
            json_destroy(obj->pairs[i].value);
            memmove(&obj->pairs[i], &obj->pairs[i + 1], (obj->size - i - 1) * sizeof(JsonPair));
            obj->size--;
            return;
        }
    }
}

int json_object_has(JsonValue* object, const String key) {
    return json_object_get(object, key) != NULL;
}

size_t json_object_size(JsonValue* object) {
    if (!object || object->type != JSON_OBJECT) return 0;
    return object->value.object_val->size;
}

JsonPair* json_object_pairs(JsonValue* object) {
    if (!object || object->type != JSON_OBJECT) return NULL;
    return object->value.object_val->pairs;
}

JsonType json_get_type(JsonValue* value) {
    if (!value) return JSON_NULL;
    return value->type;
}

int json_is_null(JsonValue* value) { return !value || value->type == JSON_NULL; }
int json_is_bool(JsonValue* value) { return value && value->type == JSON_BOOL; }
int json_is_number(JsonValue* value) { return value && value->type == JSON_NUMBER; }
int json_is_string(JsonValue* value) { return value && value->type == JSON_STRING; }
int json_is_array(JsonValue* value) { return value && value->type == JSON_ARRAY; }
int json_is_object(JsonValue* value) { return value && value->type == JSON_OBJECT; }

int json_get_bool(JsonValue* value) {
    if (!value || value->type != JSON_BOOL) return 0;
    return value->value.bool_val;
}

double json_get_number(JsonValue* value) {
    if (!value || value->type != JSON_NUMBER) return 0.0;
    return value->value.number_val;
}

String json_get_string(JsonValue* value) {
    if (!value || value->type != JSON_STRING) return STRING_EMPTY;
    return string_create(value->value.string_val);
}

static void json_skip_whitespace(const char** text) {
    while (**text && isspace(**text)) (*text)++;
}

static String json_parse_string_content(const char** text, int* error) {
    if (**text != '"') { *error = 1; return STRING_EMPTY; }
    (*text)++;
    const char* start = *text;
    while (**text && **text != '"') {
        if (**text == '\\') (*text)++;
        if (**text) (*text)++;
    }
    if (**text != '"') { *error = 1; return STRING_EMPTY; }
    size_t len = *text - start;
    char* result = (char*)kmm_v4_malloc(len + 1);
    if (!result) { *error = 1; return STRING_EMPTY; }
    memcpy(result, start, len);
    result[len] = '\0';
    (*text)++;
    return (String){.len = len, .ptr = result};
}

static String json_parse_escaped_string(const char* str) {
    size_t len = strlen(str);
    char* result = (char*)kmm_v4_malloc(len + 1);
    if (!result) return STRING_EMPTY;
    size_t j = 0;
    for (size_t i = 0; i < len; i++) {
        if (str[i] == '\\' && i + 1 < len) {
            i++;
            switch (str[i]) {
                case '"': result[j++] = '"'; break;
                case '\\': result[j++] = '\\'; break;
                case '/': result[j++] = '/'; break;
                case 'n': result[j++] = '\n'; break;
                case 'r': result[j++] = '\r'; break;
                case 't': result[j++] = '\t'; break;
                case 'b': result[j++] = '\b'; break;
                case 'f': result[j++] = '\f'; break;
                default: result[j++] = '\\'; result[j++] = str[i]; break;
            }
        } else {
            result[j++] = str[i];
        }
    }
    result[j] = '\0';
    return (String){.len = j, .ptr = result};
}

static JsonValue* json_parse_value(const char** text, int* error) {
    json_skip_whitespace(text);
    if (**text == '"') {
        String str = json_parse_string_content(text, error);
        if (*error) return NULL;
        String unescaped = json_parse_escaped_string(str.ptr);
        string_free(str);
        JsonValue* val = json_create_string(unescaped);
        string_free(unescaped);
        return val;
    } else if (**text == '{') {
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
            if (**text == '}') { (*text)++; return obj; }
            break;
        }
        json_destroy(obj);
        *error = 1;
        return NULL;
    } else if (**text == '[') {
        (*text)++;
        JsonValue* arr = json_create_array();
        json_skip_whitespace(text);
        if (**text == ']') { (*text)++; return arr; }
        while (1) {
            JsonValue* val = json_parse_value(text, error);
            if (*error) { json_destroy(arr); return NULL; }
            json_array_append(arr, val);
            json_skip_whitespace(text);
            if (**text == ',') { (*text)++; continue; }
            if (**text == ']') { (*text)++; return arr; }
            break;
        }
        json_destroy(arr);
        *error = 1;
        return NULL;
    } else if (**text == 't') {
        if (strncmp(*text, "true", 4) == 0) { (*text) += 4; return json_create_bool(1); }
        *error = 1; return NULL;
    } else if (**text == 'f') {
        if (strncmp(*text, "false", 5) == 0) { (*text) += 5; return json_create_bool(0); }
        *error = 1; return NULL;
    } else if (**text == 'n') {
        if (strncmp(*text, "null", 4) == 0) { (*text) += 4; return json_create_null(); }
        *error = 1; return NULL;
    } else {
        const char* start = *text;
        while (**text && (isdigit(**text) || **text == '.' || **text == '-' || **text == '+' || **text == 'e' || **text == 'E')) (*text)++;
        char* num_str = (char*)kmm_v4_malloc(*text - start + 1);
        if (!num_str) { *error = 1; return NULL; }
        memcpy(num_str, start, *text - start);
        num_str[*text - start] = '\0';
        double val = atof(num_str);
        kmm_v4_free(num_str);
        return json_create_number(val);
    }
}

JsonValue* json_parse(const String text) {
    if (!text.ptr) return NULL;
    int error = 0;
    const char* p = text.ptr;
    JsonValue* result = json_parse_value(&p, &error);
    return error ? NULL : result;
}

JsonValue* json_parse_file(const String path) {
    FILE* f = fopen(path.ptr, "r");
    if (!f) return NULL;
    fseek(f, 0, SEEK_END);
    long len = ftell(f);
    fseek(f, 0, SEEK_SET);
    char* buffer = (char*)kmm_v4_malloc(len + 1);
    if (!buffer) { fclose(f); return NULL; }
    fread(buffer, 1, len, f);
    buffer[len] = '\0';
    fclose(f);
    JsonValue* result = json_parse((String){.len = (size_t)len, .ptr = buffer});
    kmm_v4_free(buffer);
    return result;
}

static String json_serialize_value(JsonValue* value, int indent_level, int pretty) {
    if (!value) return string_create("null");
    
    switch (value->type) {
        case JSON_NULL: return string_create("null");
        case JSON_BOOL: return string_create(value->value.bool_val ? "true" : "false");
        case JSON_NUMBER: {
            char buf[64];
            if (value->value.number_val == (long long)value->value.number_val) {
                sprintf(buf, "%lld", (long long)value->value.number_val);
            } else {
                sprintf(buf, "%g", value->value.number_val);
            }
            return string_create(buf);
        }
        case JSON_STRING: {
            const char* src = value->value.string_val;
            size_t len = strlen(src);
            size_t buf_size = len * 2 + 3;
            char* buf = (char*)kmm_v4_malloc(buf_size);
            char* p = buf;
            *p++ = '"';
            for (size_t i = 0; i < len; i++) {
                switch (src[i]) {
                    case '"': *p++ = '\\'; *p++ = '"'; break;
                    case '\\': *p++ = '\\'; *p++ = '\\'; break;
                    case '\n': *p++ = '\\'; *p++ = 'n'; break;
                    case '\r': *p++ = '\\'; *p++ = 'r'; break;
                    case '\t': *p++ = '\\'; *p++ = 't'; break;
                    default: *p++ = src[i]; break;
                }
            }
            *p++ = '"';
            *p = '\0';
            String result = string_create(buf);
            kmm_v4_free(buf);
            return result;
        }
        case JSON_ARRAY: {
            if (!value->value.array_val || value->value.array_val->size == 0) return string_create("[]");
            String result = string_create("[");
            for (size_t i = 0; i < value->value.array_val->size; i++) {
                if (i > 0) {
                    String tmp = result;
                    result = string_concat(tmp, string_wrap(","));
                    string_free(tmp);
                }
                String item = json_serialize_value(value->value.array_val->items[i], indent_level + 1, pretty);
                String tmp = result;
                result = string_concat(tmp, item);
                string_free(tmp);
                string_free(item);
            }
            String tmp = result;
            result = string_concat(tmp, string_wrap("]"));
            string_free(tmp);
            return result;
        }
        case JSON_OBJECT: {
            if (!value->value.object_val || value->value.object_val->size == 0) return string_create("{}");
            String result = string_create("{");
            for (size_t i = 0; i < value->value.object_val->size; i++) {
                if (i > 0) {
                    String tmp = result;
                    result = string_concat(tmp, string_wrap(","));
                    string_free(tmp);
                }
                String key_json = json_serialize_value(json_create_string(value->value.object_val->pairs[i].key), 0, 0);
                String val_json = json_serialize_value(value->value.object_val->pairs[i].value, indent_level + 1, pretty);
                String tmp = result;
                result = string_concat(tmp, key_json);
                string_free(tmp);
                string_free(key_json);
                tmp = result;
                result = string_concat(tmp, string_wrap(":"));
                string_free(tmp);
                tmp = result;
                result = string_concat(tmp, val_json);
                string_free(tmp);
                string_free(val_json);
            }
            String tmp = result;
            result = string_concat(tmp, string_wrap("}"));
            string_free(tmp);
            return result;
        }
    }
    return string_create("null");
}

String json_serialize(JsonValue* value) {
    return json_serialize_value(value, 0, 0);
}

String json_serialize_pretty(JsonValue* value, int indent_level) {
    return json_serialize_value(value, indent_level, 1);
}

int json_to_file(JsonValue* value, const String path) {
    FILE* f = fopen(path.ptr, "w");
    if (!f) return 0;
    String json = json_serialize(value);
    fprintf(f, "%s", json.ptr);
    string_free(json);
    fclose(f);
    return 1;
}

int json_to_file_pretty(JsonValue* value, const String path) {
    FILE* f = fopen(path.ptr, "w");
    if (!f) return 0;
    String json = json_serialize_pretty(value, 0);
    fprintf(f, "%s", json.ptr);
    string_free(json);
    fclose(f);
    return 1;
}

JsonValue* json_deep_copy(JsonValue* value) {
    if (!value) return NULL;
    String json = json_serialize(value);
    JsonValue* result = json_parse(json);
    string_free(json);
    return result;
}
