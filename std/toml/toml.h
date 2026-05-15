#ifndef STD_TOML_TOML_H
#define STD_TOML_TOML_H

#include "../base/types.h"
#include "../string/string.h"
#include "../json/json.h"

typedef enum {
    TOML_STRING,
    TOML_INTEGER,
    TOML_FLOAT,
    TOML_BOOLEAN,
    TOML_ARRAY,
    TOML_TABLE,
    TOML_DATETIME
} TomlType;

typedef struct TomlValue TomlValue;

typedef struct TomlTable {
    String* keys;
    TomlValue** values;
    size_t size;
    size_t capacity;
} TomlTable;

typedef struct TomlValue {
    TomlType type;
    union {
        String string_val;
        i64 int_val;
        f64 float_val;
        bool_t bool_val;
        TomlTable* table_val;
        struct {
            TomlValue** items;
            size_t size;
            size_t capacity;
        } array_val;
    };
} TomlValue;

typedef struct TomlDocument {
    TomlTable* root;
} TomlDocument;

extern TomlDocument* toml_parse(const String text);
extern TomlDocument* toml_parse_file(const String path);
extern void toml_destroy(TomlDocument* doc);

extern TomlValue* toml_get(TomlTable* table, const String key);
extern TomlTable* toml_get_table(TomlTable* table, const String key);
extern String toml_get_string(TomlTable* table, const String key);
extern i64 toml_get_int(TomlTable* table, const String key);
extern f64 toml_get_float(TomlTable* table, const String key);
extern bool_t toml_get_bool(TomlTable* table, const String key);

extern String toml_serialize(TomlDocument* doc);

#endif // STD_TOML_TOML_H
