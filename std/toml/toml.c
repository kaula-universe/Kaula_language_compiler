#include "toml.h"
#include "../memory/memory.h"
#include <string.h>
#include <stdio.h>
#include <ctype.h>

static void toml_skip_whitespace(const char** p) { while (**p && isspace((unsigned char)**p) && **p != '\n') (*p)++; }
static void toml_skip_to_eol(const char** p) { while (**p && **p != '\n') (*p)++; if (**p == '\n') (*p)++; }

TomlValue* toml_create_string(String val) { TomlValue* v = kmm_v4_calloc(1, sizeof(TomlValue)); v->type = TOML_STRING; v->string_val = val; return v; }
TomlValue* toml_create_int(i64 val) { TomlValue* v = kmm_v4_calloc(1, sizeof(TomlValue)); v->type = TOML_INTEGER; v->int_val = val; return v; }
TomlValue* toml_create_float(f64 val) { TomlValue* v = kmm_v4_calloc(1, sizeof(TomlValue)); v->type = TOML_FLOAT; v->float_val = val; return v; }
TomlValue* toml_create_bool(bool_t val) { TomlValue* v = kmm_v4_calloc(1, sizeof(TomlValue)); v->type = TOML_BOOLEAN; v->bool_val = val; return v; }
TomlValue* toml_create_table() { TomlValue* v = kmm_v4_calloc(1, sizeof(TomlValue)); v->type = TOML_TABLE; v->table_val = kmm_v4_calloc(1, sizeof(TomlTable)); v->table_val->capacity = 8; return v; }

static void toml_table_set(TomlTable* table, const String key, TomlValue* val) {
    for (size_t i = 0; i < table->size; i++) { if (string_equals(table->keys[i], key)) { table->values[i] = val; return; } }
    if (table->size >= table->capacity) { table->capacity *= 2; table->keys = kmm_v4_realloc(table->keys, table->capacity * sizeof(String)); table->values = kmm_v4_realloc(table->values, table->capacity * sizeof(TomlValue*)); }
    table->keys[table->size] = string_copy(key); table->values[table->size] = val; table->size++;
}

TomlValue* toml_get(TomlTable* table, const String key) {
    if (!table || !key) return NULL;
    for (size_t i = 0; i < table->size; i++) { if (string_equals(table->keys[i], key)) return table->values[i]; }
    return NULL;
}

TomlTable* toml_get_table(TomlTable* table, const String key) { TomlValue* v = toml_get(table, key); return (v && v->type == TOML_TABLE) ? v->table_val : NULL; }
String toml_get_string(TomlTable* table, const String key) { TomlValue* v = toml_get(table, key); return (v && v->type == TOML_STRING) ? v->string_val : NULL; }
i64 toml_get_int(TomlTable* table, const String key) { TomlValue* v = toml_get(table, key); return (v && (v->type == TOML_INTEGER || v->type == TOML_FLOAT)) ? (v->type == TOML_INTEGER ? v->int_val : (i64)v->float_val) : 0; }
f64 toml_get_float(TomlTable* table, const String key) { TomlValue* v = toml_get(table, key); return (v && (v->type == TOML_FLOAT || v->type == TOML_INTEGER)) ? (v->type == TOML_FLOAT ? v->float_val : (f64)v->int_val) : 0.0; }
bool_t toml_get_bool(TomlTable* table, const String key) { TomlValue* v = toml_get(table, key); return (v && v->type == TOML_BOOLEAN) ? v->bool_val : false; }

static TomlValue* toml_parse_value(const char* val_str) {
    while (isspace((unsigned char)*val_str)) val_str++;
    size_t len = strlen(val_str);
    if (val_str[0] == '"' && val_str[len-1] == '"') {
        char* s = (char*)kmm_v4_malloc(len - 1); memcpy(s, val_str + 1, len - 2); s[len-2] = '\0';
        return toml_create_string(s);
    }
    if (strcmp(val_str, "true") == 0) return toml_create_bool(true);
    if (strcmp(val_str, "false") == 0) return toml_create_bool(false);
    char* end;
    f64 f = strtod(val_str, &end);
    if (*end == '\0' && strchr(val_str, '.')) return toml_create_float(f);
    i64 i = strtoll(val_str, &end, 10);
    if (*end == '\0') return toml_create_int(i);
    return toml_create_string(string_copy(val_str));
}

TomlDocument* toml_parse(const String text) {
    if (!text) return NULL;
    TomlDocument* doc = kmm_v4_calloc(1, sizeof(TomlDocument));
    doc->root = kmm_v4_calloc(1, sizeof(TomlTable)); doc->root->capacity = 16;
    TomlTable* current = doc->root;
    const char* p = text;
    while (*p) {
        toml_skip_whitespace(&p);
        if (!*p) break;
        if (*p == '#') { toml_skip_to_eol(&p); continue; }
        if (*p == '[') {
            p++;
            if (*p == '[') { p++; }
            while (isspace((unsigned char)*p)) p++;
            const char* name_start = p;
            while (*p && *p != ']' && *p != '\n') p++;
            size_t name_len = p - name_start;
            char* name = (char*)kmm_v4_malloc(name_len + 1);
            memcpy(name, name_start, name_len); name[name_len] = '\0';
            while (*p && *p != ']') p++; if (*p == ']') p++; if (*p == ']') p++;
            TomlValue* table = toml_create_table();
            toml_table_set(current, name, table);
            current = table->table_val;
            // KMM 管理内存，无需手动释放
        } else {
            const char* key_start = p;
            while (*p && *p != '=' && *p != '\n') p++;
            size_t key_len = p - key_start;
            while (key_len > 0 && isspace((unsigned char)key_start[key_len-1])) key_len--;
            char* key = (char*)kmm_v4_malloc(key_len + 1);
            memcpy(key, key_start, key_len); key[key_len] = '\0';
            if (*p == '=') p++;
            while (isspace((unsigned char)*p)) p++;
            const char* val_start = p;
            while (*p && *p != '\n' && *p != '#') p++;
            size_t val_len = p - val_start;
            while (val_len > 0 && isspace((unsigned char)val_start[val_len-1])) val_len--;
            char* val_str = (char*)kmm_v4_malloc(val_len + 1);
            memcpy(val_str, val_start, val_len); val_str[val_len] = '\0';
            toml_table_set(current, key, toml_parse_value(val_str));
            // KMM 管理内存，无需手动释放
        }
    }
    return doc;
}

TomlDocument* toml_parse_file(const String path) {
    FILE* f = fopen(path, "r"); if (!f) return NULL;
    fseek(f, 0, SEEK_END); long len = ftell(f); fseek(f, 0, SEEK_SET);
    String buf = (String)kmm_v4_malloc(len + 1); fread(buf, 1, len, f); buf[len] = '\0'; fclose(f);
    TomlDocument* doc = toml_parse(buf);
    // KMM 管理内存，无需手动释放
    return doc;
}

void toml_destroy(TomlDocument* doc) {
    // KMM 管理内存，无需手动释放
    (void)doc;
}

String toml_serialize(TomlDocument* doc) {
    if (!doc || !doc->root) return NULL;
    String result = string_create("");
    for (size_t i = 0; i < doc->root->size; i++) {
        String line = string_create(doc->root->keys[i]);
        line = string_concat(line, " = ");
        TomlValue* v = doc->root->values[i];
        if (v->type == TOML_STRING) { String q = string_create("\""); line = string_concat(line, q); string_free(q); line = string_concat(line, v->string_val); String q2 = string_create("\""); line = string_concat(line, q2); string_free(q2); }
        else if (v->type == TOML_INTEGER) { line = string_concat(line, string_create_from_int(v->int_val)); }
        else if (v->type == TOML_FLOAT) { line = string_concat(line, string_create_from_float(v->float_val)); }
        else if (v->type == TOML_BOOLEAN) { line = string_concat(line, v->bool_val ? string_create("true") : string_create("false")); }
        else if (v->type == TOML_TABLE) { String tbl = string_create("\n"); tbl = string_concat(tbl, doc->root->keys[i]); tbl = string_concat(tbl, string_create("\n")); line = string_concat(line, tbl); string_free(tbl); }
        result = string_concat(result, line);
        result = string_concat(result, string_create("\n"));
        string_free(line);
    }
    return result;
}
