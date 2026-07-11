#include "toml_ext.h"
#include "../memory/memory.h"
#include "../io/io.h"
#include <stdio.h>
#include <string.h>
#include <stdlib.h>
#include <ctype.h>

typedef struct TOMLParser {
    const char* input;
    size_t pos;
    size_t len;
    int line;
    int col;
} TOMLParser;

static TOMLNode* create_node(TOMLValueType type) {
    TOMLNode* node = (TOMLNode*)kmm_v4_malloc(sizeof(TOMLNode));
    if (!node) return NULL;
    node->type = type;
    memset(&node->data, 0, sizeof(node->data));
    return node;
}

static void toml_free_node(TOMLNode* node) {
    TOMLKeyValue* kv;
    TOMLKeyValue* next_kv;
    size_t i;
    if (!node) return;
    switch (node->type) {
        case TOML_STRING:
            if (node->data.string_val) string_free(node->data.string_val);
            break;
        case TOML_DATETIME:
            if (node->data.datetime_val) string_free(node->data.datetime_val);
            break;
        case TOML_TABLE:
            kv = node->data.table.entries;
            while (kv) {
                next_kv = kv->next;
                if (kv->key) string_free(kv->key);
                toml_free_node(kv->value);
                kmm_v4_free(kv);
                kv = next_kv;
            }
            break;
        case TOML_ARRAY:
            for (i = 0; i < node->data.array.count; i++) {
                toml_free_node(node->data.array.items[i]);
            }
            if (node->data.array.items) kmm_v4_free(node->data.array.items);
            break;
        default:
            break;
    }
    kmm_v4_free(node);
}

void toml_free(TOMLNode* root) {
    toml_free_node(root);
}

TOMLValueType toml_node_type(TOMLNode* node) {
    if (!node) return TOML_TABLE;
    return node->type;
}

size_t toml_array_size(TOMLNode* node) {
    if (!node || node->type != TOML_ARRAY) return 0;
    return node->data.array.count;
}

size_t toml_table_size(TOMLNode* node) {
    if (!node || node->type != TOML_TABLE) return 0;
    return node->data.table.count;
}

TOMLNode* toml_node_get_array(TOMLNode* node, size_t index) {
    if (!node || node->type != TOML_ARRAY) return NULL;
    if (index >= node->data.array.count) return NULL;
    return node->data.array.items[index];
}

TOMLNode* toml_node_get_table(TOMLNode* node, const char* key) {
    TOMLKeyValue* kv;
    if (!node || node->type != TOML_TABLE || !key) return NULL;
    kv = node->data.table.entries;
    while (kv) {
        if (strcmp(kv->key, key) == 0) return kv->value;
        kv = kv->next;
    }
    return NULL;
}

bool_t toml_node_get_bool(TOMLNode* node, bool_t* out) {
    if (!node || node->type != TOML_BOOLEAN || !out) return false;
    *out = node->data.bool_val;
    return true;
}

bool_t toml_node_get_int(TOMLNode* node, int64_t* out) {
    if (!node || !out) return false;
    if (node->type == TOML_INTEGER) {
        *out = node->data.int_val;
        return true;
    }
    if (node->type == TOML_FLOAT) {
        *out = (int64_t)node->data.float_val;
        return true;
    }
    return false;
}

bool_t toml_node_get_float(TOMLNode* node, double* out) {
    if (!node || !out) return false;
    if (node->type == TOML_FLOAT) {
        *out = node->data.float_val;
        return true;
    }
    if (node->type == TOML_INTEGER) {
        *out = (double)node->data.int_val;
        return true;
    }
    return false;
}

bool_t toml_node_get_string(TOMLNode* node, const char** out) {
    if (!node || node->type != TOML_STRING || !out) return false;
    *out = node->data.string_val;
    return true;
}

static void table_insert(TOMLNode* table, const char* key, TOMLNode* value) {
    TOMLKeyValue* kv;
    TOMLKeyValue* existing;
    if (!table || !key || !value) return;
    existing = table->data.table.entries;
    while (existing) {
        if (strcmp(existing->key, key) == 0) {
            toml_free_node(existing->value);
            existing->value = value;
            return;
        }
        existing = existing->next;
    }
    kv = (TOMLKeyValue*)kmm_v4_malloc(sizeof(TOMLKeyValue));
    if (!kv) return;
    kv->key = string_copy(key);
    kv->value = value;
    kv->next = table->data.table.entries;
    table->data.table.entries = kv;
    table->data.table.count++;
}

static TOMLNode* find_or_create_table(TOMLNode* root, char** parts, size_t num_parts) {
    TOMLNode* current = root;
    size_t i;
    for (i = 0; i < num_parts; i++) {
        TOMLNode* child = toml_node_get_table(current, parts[i]);
        if (!child) {
            child = create_node(TOML_TABLE);
            if (!child) return NULL;
            table_insert(current, parts[i], child);
        } else if (child->type != TOML_TABLE) {
            return NULL;
        }
        current = child;
    }
    return current;
}

static char* strdup_safe(const char* s) {
    size_t len;
    char* dup;
    if (!s) return NULL;
    len = strlen(s);
    dup = (char*)kmm_v4_malloc(len + 1);
    if (!dup) return NULL;
    memcpy(dup, s, len + 1);
    return dup;
}

static void parser_advance(TOMLParser* p, size_t n) {
    size_t i;
    for (i = 0; i < n && p->pos < p->len; i++) {
        if (p->input[p->pos] == '\n') {
            p->line++;
            p->col = 1;
        } else {
            p->col++;
        }
        p->pos++;
    }
}

static char parser_peek(TOMLParser* p) {
    if (p->pos >= p->len) return '\0';
    return p->input[p->pos];
}

static void parser_skip_whitespace(TOMLParser* p) {
    while (p->pos < p->len && (p->input[p->pos] == ' ' || p->input[p->pos] == '\t')) {
        parser_advance(p, 1);
    }
}

static void parser_skip_comment(TOMLParser* p) {
    if (parser_peek(p) == '#') {
        while (p->pos < p->len && p->input[p->pos] != '\n') {
            parser_advance(p, 1);
        }
    }
}

static void parser_skip_line_ending(TOMLParser* p) {
    parser_skip_whitespace(p);
    parser_skip_comment(p);
    if (parser_peek(p) == '\r') parser_advance(p, 1);
    if (parser_peek(p) == '\n') parser_advance(p, 1);
}

static bool_t parse_basic_string(TOMLParser* p, char** out) {
    size_t cap = 32;
    size_t len = 0;
    char* buf = (char*)kmm_v4_malloc(cap);
    if (!buf) return false;
    parser_advance(p, 1);
    while (p->pos < p->len && p->input[p->pos] != '"') {
        char c = p->input[p->pos];
        if (c == '\\' && p->pos + 1 < p->len) {
            parser_advance(p, 1);
            c = p->input[p->pos];
            switch (c) {
                case 'n': c = '\n'; break;
                case 't': c = '\t'; break;
                case 'r': c = '\r'; break;
                case '\\': c = '\\'; break;
                case '"': c = '"'; break;
                case '0': c = '\0'; break;
                default: break;
            }
        }
        if (len + 1 >= cap) {
            char* new_buf;
            cap *= 2;
            new_buf = (char*)kmm_v4_malloc(cap);
            if (!new_buf) { kmm_v4_free(buf); return false; }
            memcpy(new_buf, buf, len);
            kmm_v4_free(buf);
            buf = new_buf;
        }
        buf[len++] = c;
        parser_advance(p, 1);
    }
    if (parser_peek(p) != '"') { kmm_v4_free(buf); return false; }
    parser_advance(p, 1);
    buf[len] = '\0';
    *out = buf;
    return true;
}

static bool_t parse_literal_string(TOMLParser* p, char** out) {
    size_t start;
    size_t len;
    char* buf;
    if (parser_peek(p) != '\'') return false;
    parser_advance(p, 1);
    start = p->pos;
    while (p->pos < p->len && p->input[p->pos] != '\'') {
        parser_advance(p, 1);
    }
    if (parser_peek(p) != '\'') return false;
    len = p->pos - start;
    buf = (char*)kmm_v4_malloc(len + 1);
    if (!buf) return false;
    memcpy(buf, p->input + start, len);
    buf[len] = '\0';
    parser_advance(p, 1);
    *out = buf;
    return true;
}

static bool_t parse_keylike(TOMLParser* p, char** out) {
    size_t start;
    size_t len;
    char* buf;
    if (parser_peek(p) == '"') return parse_basic_string(p, out);
    if (parser_peek(p) == '\'') return parse_literal_string(p, out);
    start = p->pos;
    while (p->pos < p->len && (isalnum((unsigned char)p->input[p->pos]) || p->input[p->pos] == '_' || p->input[p->pos] == '-')) {
        parser_advance(p, 1);
    }
    len = p->pos - start;
    if (len == 0) return false;
    buf = (char*)kmm_v4_malloc(len + 1);
    if (!buf) return false;
    memcpy(buf, p->input + start, len);
    buf[len] = '\0';
    *out = buf;
    return true;
}

static TOMLNode* parse_value(TOMLParser* p);

static bool_t array_append(TOMLNode* arr, TOMLNode* item) {
    if (!arr || !item) return false;
    if (arr->data.array.count >= arr->data.array.capacity) {
        size_t new_cap = arr->data.array.capacity == 0 ? 8 : arr->data.array.capacity * 2;
        TOMLNode** new_items = (TOMLNode**)kmm_v4_malloc(new_cap * sizeof(TOMLNode*));
        if (!new_items) return false;
        if (arr->data.array.items) {
            memcpy(new_items, arr->data.array.items, arr->data.array.count * sizeof(TOMLNode*));
            kmm_v4_free(arr->data.array.items);
        }
        arr->data.array.items = new_items;
        arr->data.array.capacity = new_cap;
    }
    arr->data.array.items[arr->data.array.count++] = item;
    return true;
}

static TOMLNode* parse_array(TOMLParser* p) {
    TOMLNode* arr = create_node(TOML_ARRAY);
    if (!arr) return NULL;
    parser_advance(p, 1);
    while (1) {
        parser_skip_whitespace(p);
        if (parser_peek(p) == '\n' || parser_peek(p) == ',') {
            if (parser_peek(p) == ',') parser_advance(p, 1);
            parser_skip_whitespace(p);
            while (parser_peek(p) == '\n' || parser_peek(p) == '\r') {
                parser_skip_line_ending(p);
                parser_skip_whitespace(p);
            }
            if (parser_peek(p) == ']') break;
            continue;
        }
        if (parser_peek(p) == ']') break;
        {
            TOMLNode* item = parse_value(p);
            if (!item) { toml_free_node(arr); return NULL; }
            if (!array_append(arr, item)) { toml_free_node(item); toml_free_node(arr); return NULL; }
        }
        parser_skip_whitespace(p);
    }
    if (parser_peek(p) != ']') { toml_free_node(arr); return NULL; }
    parser_advance(p, 1);
    return arr;
}

static TOMLNode* parse_value(TOMLParser* p) {
    char c;
    parser_skip_whitespace(p);
    c = parser_peek(p);
    if (c == '"') {
        char* s;
        TOMLNode* n;
        if (!parse_basic_string(p, &s)) return NULL;
        n = create_node(TOML_STRING);
        if (!n) { kmm_v4_free(s); return NULL; }
        n->data.string_val = s;
        return n;
    }
    if (c == '\'') {
        char* s;
        TOMLNode* n;
        if (!parse_literal_string(p, &s)) return NULL;
        n = create_node(TOML_STRING);
        if (!n) { kmm_v4_free(s); return NULL; }
        n->data.string_val = s;
        return n;
    }
    if (c == '[') {
        return parse_array(p);
    }
    if (c == 't' || c == 'f') {
        const char* rest = p->input + p->pos;
        if (strncmp(rest, "true", 4) == 0) {
            TOMLNode* n = create_node(TOML_BOOLEAN);
            if (!n) return NULL;
            n->data.bool_val = true;
            parser_advance(p, 4);
            return n;
        }
        if (strncmp(rest, "false", 5) == 0) {
            TOMLNode* n = create_node(TOML_BOOLEAN);
            if (!n) return NULL;
            n->data.bool_val = false;
            parser_advance(p, 5);
            return n;
        }
    }
    if (c == '+' || c == '-' || isdigit((unsigned char)c)) {
        size_t start = p->pos;
        bool_t is_float = false;
        char* endptr;
        TOMLNode* n;
        if (c == '+' || c == '-') parser_advance(p, 1);
        while (p->pos < p->len && (isdigit((unsigned char)p->input[p->pos]) || p->input[p->pos] == '_')) {
            parser_advance(p, 1);
        }
        if (parser_peek(p) == '.') {
            is_float = true;
            parser_advance(p, 1);
            while (p->pos < p->len && (isdigit((unsigned char)p->input[p->pos]) || p->input[p->pos] == '_')) {
                parser_advance(p, 1);
            }
        }
        if (parser_peek(p) == 'e' || parser_peek(p) == 'E') {
            is_float = true;
            parser_advance(p, 1);
            if (parser_peek(p) == '+' || parser_peek(p) == '-') parser_advance(p, 1);
            while (p->pos < p->len && isdigit((unsigned char)p->input[p->pos])) {
                parser_advance(p, 1);
            }
        }
        {
            size_t len = p->pos - start;
            char* buf = (char*)kmm_v4_malloc(len + 1);
            size_t j = 0;
            size_t i;
            if (!buf) return NULL;
            for (i = 0; i < len; i++) {
                if (p->input[start + i] != '_') buf[j++] = p->input[start + i];
            }
            buf[j] = '\0';
            if (is_float) {
                n = create_node(TOML_FLOAT);
                if (!n) { kmm_v4_free(buf); return NULL; }
                n->data.float_val = strtod(buf, &endptr);
            } else {
                n = create_node(TOML_INTEGER);
                if (!n) { kmm_v4_free(buf); return NULL; }
                n->data.int_val = strtoll(buf, &endptr, 10);
            }
            kmm_v4_free(buf);
            return n;
        }
    }
    {
        size_t start = p->pos;
        while (p->pos < p->len && p->input[p->pos] != '\n' && p->input[p->pos] != '\r' && p->input[p->pos] != '#' && p->input[p->pos] != ',' && p->input[p->pos] != ']') {
            parser_advance(p, 1);
        }
        while (p->pos > start && (p->input[p->pos-1] == ' ' || p->input[p->pos-1] == '\t')) {
            p->pos--;
        }
    }
    return NULL;
}

static void free_key_parts(char** parts, size_t count) {
    size_t i;
    if (!parts) return;
    for (i = 0; i < count; i++) {
        if (parts[i]) kmm_v4_free(parts[i]);
    }
    kmm_v4_free(parts);
}

static bool_t parse_dotted_key(TOMLParser* p, char*** out_parts, size_t* out_count) {
    char** parts = NULL;
    size_t count = 0;
    size_t cap = 4;
    size_t i;
    parts = (char**)kmm_v4_malloc(cap * sizeof(char*));
    if (!parts) return false;
    while (1) {
        char* part;
        parser_skip_whitespace(p);
        if (!parse_keylike(p, &part)) {
            for (i = 0; i < count; i++) kmm_v4_free(parts[i]);
            kmm_v4_free(parts);
            return false;
        }
        if (count >= cap) {
            char** new_parts;
            cap *= 2;
            new_parts = (char**)kmm_v4_malloc(cap * sizeof(char*));
            if (!new_parts) {
                kmm_v4_free(part);
                for (i = 0; i < count; i++) kmm_v4_free(parts[i]);
                kmm_v4_free(parts);
                return false;
            }
            memcpy(new_parts, parts, count * sizeof(char*));
            kmm_v4_free(parts);
            parts = new_parts;
        }
        parts[count++] = part;
        parser_skip_whitespace(p);
        if (parser_peek(p) != '.') break;
        parser_advance(p, 1);
    }
    *out_parts = parts;
    *out_count = count;
    return true;
}

static void parse_key_value(TOMLParser* p, TOMLNode* current_table) {
    char** key_parts = NULL;
    size_t num_parts = 0;
    TOMLNode* value;
    TOMLNode* parent_table;
    size_t i;
    if (!parse_dotted_key(p, &key_parts, &num_parts)) return;
    parser_skip_whitespace(p);
    if (parser_peek(p) != '=') {
        free_key_parts(key_parts, num_parts);
        return;
    }
    parser_advance(p, 1);
    value = parse_value(p);
    if (!value) {
        free_key_parts(key_parts, num_parts);
        return;
    }
    parent_table = current_table;
    for (i = 0; i < num_parts - 1; i++) {
        TOMLNode* child = toml_node_get_table(parent_table, key_parts[i]);
        if (!child) {
            child = create_node(TOML_TABLE);
            if (child) table_insert(parent_table, key_parts[i], child);
        }
        if (!child || child->type != TOML_TABLE) {
            toml_free_node(value);
            free_key_parts(key_parts, num_parts);
            return;
        }
        parent_table = child;
    }
    table_insert(parent_table, key_parts[num_parts - 1], value);
    free_key_parts(key_parts, num_parts);
}

static TOMLNode* toml_parse_internal(const char* str) {
    TOMLParser p;
    TOMLNode* root;
    TOMLNode* current_table;
    if (!str) return NULL;
    p.input = str;
    p.pos = 0;
    p.len = strlen(str);
    p.line = 1;
    p.col = 1;
    root = create_node(TOML_TABLE);
    if (!root) return NULL;
    current_table = root;
    while (p.pos < p.len) {
        parser_skip_whitespace(&p);
        if (p.pos >= p.len) break;
        if (p.input[p.pos] == '\n' || p.input[p.pos] == '\r') {
            parser_skip_line_ending(&p);
            continue;
        }
        if (p.input[p.pos] == '#') {
            parser_skip_comment(&p);
            continue;
        }
        if (p.input[p.pos] == '[') {
            bool_t is_array_of_tables = false;
            char** key_parts = NULL;
            size_t num_parts = 0;
            TOMLNode* parent_table;
            TOMLNode* new_table;
            TOMLNode* array_node;
            size_t i;
            parser_advance(&p, 1);
            if (parser_peek(&p) == '[') {
                is_array_of_tables = true;
                parser_advance(&p, 1);
            }
            parser_skip_whitespace(&p);
            if (!parse_dotted_key(&p, &key_parts, &num_parts)) {
                break;
            }
            parser_skip_whitespace(&p);
            if (is_array_of_tables) {
                if (parser_peek(&p) == ']') parser_advance(&p, 1);
            }
            if (parser_peek(&p) == ']') parser_advance(&p, 1);
            parent_table = root;
            for (i = 0; i < num_parts - 1; i++) {
                TOMLNode* child = toml_node_get_table(parent_table, key_parts[i]);
                if (!child) {
                    child = create_node(TOML_TABLE);
                    if (child) table_insert(parent_table, key_parts[i], child);
                }
                if (!child || child->type != TOML_TABLE) break;
                parent_table = child;
            }
            if (is_array_of_tables) {
                array_node = toml_node_get_table(parent_table, key_parts[num_parts - 1]);
                if (!array_node || array_node->type != TOML_ARRAY) {
                    array_node = create_node(TOML_ARRAY);
                    if (array_node) table_insert(parent_table, key_parts[num_parts - 1], array_node);
                }
                new_table = create_node(TOML_TABLE);
                if (new_table && array_node && array_node->type == TOML_ARRAY) {
                    array_append(array_node, new_table);
                }
                current_table = new_table;
            } else {
                TOMLNode* existing = toml_node_get_table(parent_table, key_parts[num_parts - 1]);
                if (existing && existing->type == TOML_TABLE) {
                    current_table = existing;
                } else {
                    new_table = create_node(TOML_TABLE);
                    if (new_table) table_insert(parent_table, key_parts[num_parts - 1], new_table);
                    current_table = new_table;
                }
            }
            free_key_parts(key_parts, num_parts);
            parser_skip_whitespace(&p);
            parser_skip_comment(&p);
            if (parser_peek(&p) == '\r') parser_advance(&p, 1);
            if (parser_peek(&p) == '\n') parser_advance(&p, 1);
            continue;
        }
        parse_key_value(&p, current_table);
        parser_skip_whitespace(&p);
        parser_skip_comment(&p);
        if (parser_peek(&p) == '\r') parser_advance(&p, 1);
        if (parser_peek(&p) == '\n') parser_advance(&p, 1);
    }
    return root;
}

TOMLNode* toml_parse_string(const char* str) {
    return toml_parse_internal(str);
}

TOMLNode* toml_parse_file(const char* filename) {
    FILE* f;
    long size;
    char* buf;
    TOMLNode* result;
    if (!filename) return NULL;
    f = fopen(filename, "rb");
    if (!f) return NULL;
    fseek(f, 0, SEEK_END);
    size = ftell(f);
    fseek(f, 0, SEEK_SET);
    if (size < 0) { fclose(f); return NULL; }
    buf = (char*)kmm_v4_malloc(size + 1);
    if (!buf) { fclose(f); return NULL; }
    if (size > 0) fread(buf, 1, size, f);
    buf[size] = '\0';
    fclose(f);
    result = toml_parse_internal(buf);
    kmm_v4_free(buf);
    return result;
}

static void serialize_indent(String* out, int indent) {
    int i;
    char buf[256];
    int len = 0;
    for (i = 0; i < indent && len < 255; i++) {
        buf[len++] = ' ';
        buf[len++] = ' ';
    }
    buf[len] = '\0';
    if (*out) {
        char* old = *out;
        size_t old_len = strlen(old);
        char* new_str = (char*)kmm_v4_malloc(old_len + len + 1);
        if (new_str) {
            memcpy(new_str, old, old_len);
            memcpy(new_str + old_len, buf, len);
            new_str[old_len + len] = '\0';
            string_free(old);
            *out = new_str;
        }
    } else {
        *out = string_copy(buf);
    }
}

static void serialize_append(String* out, const char* s) {
    if (!s) return;
    if (*out) {
        char* old = *out;
        size_t old_len = strlen(old);
        size_t s_len = strlen(s);
        char* new_str = (char*)kmm_v4_malloc(old_len + s_len + 1);
        if (new_str) {
            memcpy(new_str, old, old_len);
            memcpy(new_str + old_len, s, s_len);
            new_str[old_len + s_len] = '\0';
            string_free(old);
            *out = new_str;
        }
    } else {
        *out = string_copy(s);
    }
}

static void serialize_value(String* out, TOMLNode* node, int indent);

static void serialize_array(String* out, TOMLNode* node, int indent) {
    size_t i;
    serialize_append(out, "[");
    for (i = 0; i < node->data.array.count; i++) {
        if (i > 0) serialize_append(out, ", ");
        serialize_value(out, node->data.array.items[i], indent);
    }
    serialize_append(out, "]");
}

static void serialize_string_value(String* out, const char* s) {
    const char* p;
    char buf[2];
    serialize_append(out, "\"");
    for (p = s; *p; p++) {
        switch (*p) {
            case '\n': serialize_append(out, "\\n"); break;
            case '\t': serialize_append(out, "\\t"); break;
            case '\r': serialize_append(out, "\\r"); break;
            case '\\': serialize_append(out, "\\\\"); break;
            case '"': serialize_append(out, "\\\""); break;
            default:
                buf[0] = *p;
                buf[1] = '\0';
                serialize_append(out, buf);
                break;
        }
    }
    serialize_append(out, "\"");
}

static void serialize_value(String* out, TOMLNode* node, int indent) {
    char buf[64];
    if (!node) return;
    switch (node->type) {
        case TOML_STRING:
            serialize_string_value(out, node->data.string_val);
            break;
        case TOML_INTEGER:
            snprintf(buf, sizeof(buf), "%lld", (long long)node->data.int_val);
            serialize_append(out, buf);
            break;
        case TOML_FLOAT:
            snprintf(buf, sizeof(buf), "%g", node->data.float_val);
            serialize_append(out, buf);
            break;
        case TOML_BOOLEAN:
            serialize_append(out, node->data.bool_val ? "true" : "false");
            break;
        case TOML_DATETIME:
            serialize_append(out, node->data.datetime_val);
            break;
        case TOML_ARRAY:
            serialize_array(out, node, indent);
            break;
        case TOML_TABLE:
            break;
    }
}

static void serialize_table_entries(String* out, TOMLNode* table, char* prefix, int indent) {
    TOMLKeyValue* kv = table->data.table.entries;
    TOMLKeyValue* tables_list = NULL;
    TOMLKeyValue* arrays_list = NULL;
    char* new_prefix = NULL;
    size_t prefix_len;
    while (kv) {
        TOMLKeyValue* next = kv->next;
        if (kv->value && kv->value->type == TOML_TABLE) {
            kv->next = tables_list;
            tables_list = kv;
        } else if (kv->value && kv->value->type == TOML_ARRAY && kv->value->data.array.count > 0 &&
                   kv->value->data.array.items[0]->type == TOML_TABLE) {
            kv->next = arrays_list;
            arrays_list = kv;
        } else {
            serialize_indent(out, indent);
            serialize_append(out, kv->key);
            serialize_append(out, " = ");
            serialize_value(out, kv->value, indent);
            serialize_append(out, "\n");
        }
        kv = next;
    }
    prefix_len = prefix ? strlen(prefix) : 0;
    while (tables_list) {
        TOMLKeyValue* next = tables_list->next;
        size_t key_len = strlen(tables_list->key);
        new_prefix = (char*)kmm_v4_malloc(prefix_len + key_len + 2);
        if (new_prefix) {
            if (prefix_len > 0) {
                memcpy(new_prefix, prefix, prefix_len);
                new_prefix[prefix_len] = '.';
                memcpy(new_prefix + prefix_len + 1, tables_list->key, key_len);
                new_prefix[prefix_len + 1 + key_len] = '\0';
            } else {
                memcpy(new_prefix, tables_list->key, key_len);
                new_prefix[key_len] = '\0';
            }
            serialize_append(out, "\n[");
            serialize_append(out, new_prefix);
            serialize_append(out, "]\n");
            serialize_table_entries(out, tables_list->value, new_prefix, indent);
            kmm_v4_free(new_prefix);
        }
        tables_list = next;
    }
    while (arrays_list) {
        TOMLKeyValue* next = arrays_list->next;
        size_t key_len = strlen(arrays_list->key);
        TOMLNode* arr = arrays_list->value;
        size_t i;
        new_prefix = (char*)kmm_v4_malloc(prefix_len + key_len + 2);
        if (new_prefix) {
            if (prefix_len > 0) {
                memcpy(new_prefix, prefix, prefix_len);
                new_prefix[prefix_len] = '.';
                memcpy(new_prefix + prefix_len + 1, arrays_list->key, key_len);
                new_prefix[prefix_len + 1 + key_len] = '\0';
            } else {
                memcpy(new_prefix, arrays_list->key, key_len);
                new_prefix[key_len] = '\0';
            }
            for (i = 0; i < arr->data.array.count; i++) {
                serialize_append(out, "\n[[");
                serialize_append(out, new_prefix);
                serialize_append(out, "]]\n");
                serialize_table_entries(out, arr->data.array.items[i], new_prefix, indent);
            }
            kmm_v4_free(new_prefix);
        }
        arrays_list = next;
    }
}

String toml_serialize(TOMLNode* root) {
    String result = NULL;
    if (!root || root->type != TOML_TABLE) return NULL;
    result = string_copy("");
    serialize_table_entries(&result, root, NULL, 0);
    return result;
}
