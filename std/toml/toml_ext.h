#pragma once
#include "../base/types.h"
#include "../string/string.h"

typedef enum TOMLValueType {
    TOML_TABLE,
    TOML_ARRAY,
    TOML_STRING,
    TOML_INTEGER,
    TOML_FLOAT,
    TOML_BOOLEAN,
    TOML_DATETIME
} TOMLValueType;

typedef struct TOMLNode TOMLNode;

typedef struct TOMLKeyValue {
    String key;
    TOMLNode* value;
    struct TOMLKeyValue* next;
} TOMLKeyValue;

typedef struct TOMLArray {
    TOMLNode** items;
    size_t count;
    size_t capacity;
} TOMLArray;

typedef struct TOMLTable {
    TOMLKeyValue* entries;
    size_t count;
} TOMLTable;

struct TOMLNode {
    TOMLValueType type;
    union {
        String string_val;
        int64_t int_val;
        double float_val;
        bool_t bool_val;
        String datetime_val;
        TOMLTable table;
        TOMLArray array;
    } data;
};

TOMLNode* toml_parse_file(const char* filename);
TOMLNode* toml_parse_string(const char* str);

String toml_serialize(TOMLNode* root);

TOMLValueType toml_node_type(TOMLNode* node);

TOMLNode* toml_node_get_array(TOMLNode* node, size_t index);
TOMLNode* toml_node_get_table(TOMLNode* node, const char* key);

bool_t toml_node_get_bool(TOMLNode* node, bool_t* out);
bool_t toml_node_get_int(TOMLNode* node, int64_t* out);
bool_t toml_node_get_float(TOMLNode* node, double* out);
bool_t toml_node_get_string(TOMLNode* node, const char** out);

size_t toml_array_size(TOMLNode* node);
size_t toml_table_size(TOMLNode* node);

void toml_free(TOMLNode* root);
