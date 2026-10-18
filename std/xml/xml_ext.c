#include "xml_ext.h"
#include "../memory/memory.h"
#include "../io/io.h"
#include <stdio.h>
#include <string.h>
#include <stdlib.h>
#include <ctype.h>

XMLNode* xml_create_node(const char* tag) {
    XMLNode* node = (XMLNode*)kmm_v4_malloc(sizeof(XMLNode));
    if (!node) return NULL;
    node->tag = tag ? string_create(tag) : STRING_EMPTY;
    node->text = STRING_EMPTY;
    node->attributes = NULL;
    node->children = NULL;
    node->next = NULL;
    node->parent = NULL;
    return node;
}

void xml_add_attribute(XMLNode* node, const char* name, const char* value) {
    XMLAttribute* attr;
    XMLAttribute* existing;
    if (!node || !name || !value) return;
    existing = node->attributes;
    while (existing) {
        if (strcmp(existing->name.ptr, name) == 0) {
            if (existing->value.len > 0) string_free(existing->value);
            existing->value = string_create(value);
            return;
        }
        existing = existing->next;
    }
    attr = (XMLAttribute*)kmm_v4_malloc(sizeof(XMLAttribute));
    if (!attr) return;
    attr->name = string_create(name);
    attr->value = string_create(value);
    attr->next = node->attributes;
    node->attributes = attr;
}

void xml_add_child(XMLNode* parent, XMLNode* child) {
    XMLNode* last;
    if (!parent || !child) return;
    child->parent = parent;
    child->next = NULL;
    if (!parent->children) {
        parent->children = child;
    } else {
        last = parent->children;
        while (last->next) last = last->next;
        last->next = child;
    }
}

const char* xml_node_get_attribute(XMLNode* node, const char* name) {
    XMLAttribute* attr;
    if (!node || !name) return NULL;
    attr = node->attributes;
    while (attr) {
        if (strcmp(attr->name.ptr, name) == 0) return attr->value.ptr;
        attr = attr->next;
    }
    return NULL;
}

XMLNode* xml_node_get_child_by_tag(XMLNode* node, const char* tag) {
    XMLNode* child;
    if (!node || !tag) return NULL;
    child = node->children;
    while (child) {
        if (child->tag.len > 0 && strcmp(child->tag.ptr, tag) == 0) return child;
        child = child->next;
    }
    return NULL;
}

static void append_text(char** dest, const char* s) {
    size_t old_len;
    size_t s_len;
    char* new_str;
    if (!s) return;
    s_len = strlen(s);
    if (s_len == 0) return;
    if (*dest) {
        old_len = strlen(*dest);
        new_str = (char*)kmm_v4_malloc(old_len + s_len + 1);
        if (!new_str) return;
        memcpy(new_str, *dest, old_len);
        memcpy(new_str + old_len, s, s_len);
        new_str[old_len + s_len] = '\0';
        kmm_v4_free(*dest);
        *dest = new_str;
    } else {
        *dest = string_create(s).ptr;
    }
}

static void collect_text(XMLNode* node, char** result) {
    XMLNode* child;
    if (!node) return;
    if (node->text.len > 0) {
        append_text(result, node->text.ptr);
    }
    child = node->children;
    while (child) {
        collect_text(child, result);
        child = child->next;
    }
}

String xml_node_get_text_content(XMLNode* node) {
    char* result = NULL;
    if (!node) return STRING_EMPTY;
    collect_text(node, &result);
    return result ? string_create(result) : STRING_EMPTY;
}

void xml_free(XMLNode* root) {
    XMLAttribute* attr;
    XMLAttribute* next_attr;
    XMLNode* child;
    XMLNode* next_child;
    if (!root) return;
    attr = root->attributes;
    while (attr) {
        next_attr = attr->next;
        if (attr->name.len > 0) string_free(attr->name);
        if (attr->value.len > 0) string_free(attr->value);
        kmm_v4_free(attr);
        attr = next_attr;
    }
    child = root->children;
    while (child) {
        next_child = child->next;
        xml_free(child);
        child = next_child;
    }
    if (root->tag.len > 0) string_free(root->tag);
    if (root->text.len > 0) string_free(root->text);
    kmm_v4_free(root);
}

typedef struct XMLParser {
    const char* input;
    size_t pos;
    size_t len;
} XMLParser;

static char parser_peek(XMLParser* p) {
    if (p->pos >= p->len) return '\0';
    return p->input[p->pos];
}

static void parser_advance(XMLParser* p, size_t n) {
    p->pos += n;
    if (p->pos > p->len) p->pos = p->len;
}

static void skip_whitespace(XMLParser* p) {
    while (p->pos < p->len && (p->input[p->pos] == ' ' || p->input[p->pos] == '\t' || p->input[p->pos] == '\n' || p->input[p->pos] == '\r')) {
        p->pos++;
    }
}

static char* parse_name(XMLParser* p) {
    size_t start;
    size_t len;
    char* name;
    start = p->pos;
    if (p->pos < p->len && (isalpha((unsigned char)p->input[p->pos]) || p->input[p->pos] == '_' || p->input[p->pos] == ':')) {
        p->pos++;
    } else {
        return NULL;
    }
    while (p->pos < p->len && (isalnum((unsigned char)p->input[p->pos]) || p->input[p->pos] == '_' || p->input[p->pos] == '-' || p->input[p->pos] == '.' || p->input[p->pos] == ':')) {
        p->pos++;
    }
    len = p->pos - start;
    name = (char*)kmm_v4_malloc(len + 1);
    if (!name) return NULL;
    memcpy(name, p->input + start, len);
    name[len] = '\0';
    return name;
}

static char* parse_attr_value(XMLParser* p) {
    char quote;
    size_t start;
    size_t len;
    char* value;
    if (p->pos >= p->len) return NULL;
    quote = p->input[p->pos];
    if (quote != '"' && quote != '\'') return NULL;
    p->pos++;
    start = p->pos;
    while (p->pos < p->len && p->input[p->pos] != quote) {
        p->pos++;
    }
    len = p->pos - start;
    if (p->pos < p->len) p->pos++;
    value = (char*)kmm_v4_malloc(len + 1);
    if (!value) return NULL;
    memcpy(value, p->input + start, len);
    value[len] = '\0';
    return value;
}

static void parse_attributes(XMLParser* p, XMLNode* node) {
    while (1) {
        char* name;
        char* value;
        skip_whitespace(p);
        if (p->pos >= p->len) break;
        if (p->input[p->pos] == '/' || p->input[p->pos] == '>') break;
        name = parse_name(p);
        if (!name) break;
        skip_whitespace(p);
        if (parser_peek(p) != '=') {
            kmm_v4_free(name);
            break;
        }
        parser_advance(p, 1);
        skip_whitespace(p);
        value = parse_attr_value(p);
        if (value) {
            xml_add_attribute(node, name, value);
            kmm_v4_free(value);
        }
        kmm_v4_free(name);
    }
}

static XMLNode* parse_element(XMLParser* p);

static char* decode_entities(const char* text) {
    size_t len;
    char* result;
    size_t i, j;
    if (!text) return NULL;
    len = strlen(text);
    result = (char*)kmm_v4_malloc(len + 1);
    if (!result) return NULL;
    j = 0;
    for (i = 0; i < len; i++) {
        if (text[i] == '&' && i + 1 < len) {
            if (strncmp(text + i, "&lt;", 4) == 0) {
                result[j++] = '<'; i += 3;
            } else if (strncmp(text + i, "&gt;", 4) == 0) {
                result[j++] = '>'; i += 3;
            } else if (strncmp(text + i, "&amp;", 5) == 0) {
                result[j++] = '&'; i += 4;
            } else if (strncmp(text + i, "&quot;", 6) == 0) {
                result[j++] = '"'; i += 5;
            } else if (strncmp(text + i, "&apos;", 6) == 0) {
                result[j++] = '\''; i += 5;
            } else {
                result[j++] = text[i];
            }
        } else {
            result[j++] = text[i];
        }
    }
    result[j] = '\0';
    return result;
}

static char* parse_text(XMLParser* p) {
    size_t start;
    size_t len;
    char* raw;
    char* decoded;
    start = p->pos;
    while (p->pos < p->len && p->input[p->pos] != '<') {
        p->pos++;
    }
    len = p->pos - start;
    if (len == 0) return NULL;
    raw = (char*)kmm_v4_malloc(len + 1);
    if (!raw) return NULL;
    memcpy(raw, p->input + start, len);
    raw[len] = '\0';
    decoded = decode_entities(raw);
    kmm_v4_free(raw);
    return decoded;
}

static void skip_comment(XMLParser* p) {
    if (p->pos + 4 <= p->len && strncmp(p->input + p->pos, "<!--", 4) == 0) {
        p->pos += 4;
        while (p->pos + 3 <= p->len && strncmp(p->input + p->pos, "-->", 3) != 0) {
            p->pos++;
        }
        if (p->pos + 3 <= p->len) p->pos += 3;
    }
}

static void skip_processing_instruction(XMLParser* p) {
    if (p->pos + 2 <= p->len && strncmp(p->input + p->pos, "<?", 2) == 0) {
        p->pos += 2;
        while (p->pos + 2 <= p->len && strncmp(p->input + p->pos, "?>", 2) != 0) {
            p->pos++;
        }
        if (p->pos + 2 <= p->len) p->pos += 2;
    }
}

static void skip_doctype(XMLParser* p) {
    if (p->pos + 9 <= p->len && strncmp(p->input + p->pos, "<!DOCTYPE", 9) == 0) {
        int depth = 1;
        p->pos += 9;
        while (p->pos < p->len && depth > 0) {
            if (p->input[p->pos] == '<') depth++;
            else if (p->input[p->pos] == '>') depth--;
            p->pos++;
        }
    } else if (p->pos + 8 <= p->len && strncmp(p->input + p->pos, "<![CDATA[", 9) == 0) {
        p->pos += 9;
        while (p->pos + 3 <= p->len && strncmp(p->input + p->pos, "]]>", 3) != 0) {
            p->pos++;
        }
        if (p->pos + 3 <= p->len) p->pos += 3;
    }
}

static XMLNode* parse_element(XMLParser* p) {
    XMLNode* node;
    char* tag;
    if (parser_peek(p) != '<') return NULL;
    parser_advance(p, 1);
    skip_whitespace(p);
    tag = parse_name(p);
    if (!tag) return NULL;
    node = xml_create_node(tag);
    kmm_v4_free(tag);
    if (!node) return NULL;
    parse_attributes(p, node);
    skip_whitespace(p);
    if (parser_peek(p) == '/') {
        parser_advance(p, 1);
        skip_whitespace(p);
        if (parser_peek(p) == '>') {
            parser_advance(p, 1);
        }
        return node;
    }
    if (parser_peek(p) == '>') {
        parser_advance(p, 1);
    }
    while (1) {
        if (p->pos >= p->len) break;
        if (p->input[p->pos] == '<') {
            if (p->pos + 1 < p->len && p->input[p->pos + 1] == '/') {
                parser_advance(p, 2);
                skip_whitespace(p);
                {
                    char* close_tag = parse_name(p);
                    if (close_tag) {
                        kmm_v4_free(close_tag);
                    }
                }
                skip_whitespace(p);
                if (parser_peek(p) == '>') parser_advance(p, 1);
                break;
            } else if (p->pos + 1 < p->len && p->input[p->pos + 1] == '!') {
                size_t save = p->pos;
                skip_comment(p);
                if (p->pos == save) skip_doctype(p);
                continue;
            } else if (p->pos + 1 < p->len && p->input[p->pos + 1] == '?') {
                skip_processing_instruction(p);
                continue;
            } else {
                XMLNode* child = parse_element(p);
                if (child) {
                    xml_add_child(node, child);
                }
                continue;
            }
        } else {
            char* text = parse_text(p);
            if (text) {
                if (node->text.len > 0) {
                    char* old = node->text.ptr;
                    size_t old_len = strlen(old);
                    size_t t_len = strlen(text);
                    char* new_str = (char*)kmm_v4_malloc(old_len + t_len + 1);
                    if (new_str) {
                        memcpy(new_str, old, old_len);
                        memcpy(new_str + old_len, text, t_len);
                        new_str[old_len + t_len] = '\0';
                        kmm_v4_free(old);
                        kmm_v4_free(text);
                        node->text = string_wrap(new_str);
                    }
                } else {
                    node->text = string_wrap(text);
                }
            }
        }
    }
    return node;
}

XMLNode* xml_parse_string(const char* str) {
    XMLParser p;
    XMLNode* root = NULL;
    if (!str) return NULL;
    p.input = str;
    p.pos = 0;
    p.len = strlen(str);
    while (p.pos < p.len) {
        skip_whitespace(&p);
        if (p.pos >= p.len) break;
        if (strncmp(p.input + p.pos, "<?", 2) == 0) {
            skip_processing_instruction(&p);
            continue;
        }
        if (strncmp(p.input + p.pos, "<!DOCTYPE", 9) == 0) {
            skip_doctype(&p);
            continue;
        }
        if (strncmp(p.input + p.pos, "<!--", 4) == 0) {
            skip_comment(&p);
            continue;
        }
        if (p.input[p.pos] == '<') {
            root = parse_element(&p);
            break;
        }
        p.pos++;
    }
    return root;
}

XMLNode* xml_parse_file(const char* filename) {
    FILE* f;
    long size;
    char* buf;
    XMLNode* result;
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
    result = xml_parse_string(buf);
    kmm_v4_free(buf);
    return result;
}

static void append_str(char** dest, const char* s) {
    size_t old_len;
    size_t s_len;
    char* new_str;
    if (!dest || !s) return;
    s_len = strlen(s);
    if (*dest) {
        old_len = strlen(*dest);
        new_str = (char*)kmm_v4_malloc(old_len + s_len + 1);
        if (!new_str) return;
        memcpy(new_str, *dest, old_len);
        memcpy(new_str + old_len, s, s_len);
        new_str[old_len + s_len] = '\0';
        kmm_v4_free(*dest);
        *dest = new_str;
    } else {
        *dest = string_create(s).ptr;
    }
}

static void append_char(char** dest, char c) {
    char buf[2];
    buf[0] = c;
    buf[1] = '\0';
    append_str(dest, buf);
}

static void encode_append(char** dest, const char* text) {
    const char* p;
    if (!text) return;
    for (p = text; *p; p++) {
        switch (*p) {
            case '<': append_str(dest, "&lt;"); break;
            case '>': append_str(dest, "&gt;"); break;
            case '&': append_str(dest, "&amp;"); break;
            case '"': append_str(dest, "&quot;"); break;
            case '\'': append_str(dest, "&apos;"); break;
            default: append_char(dest, *p); break;
        }
    }
}

static void serialize_indent(char** out, int indent) {
    int i;
    char buf[512];
    int len = 0;
    for (i = 0; i < indent && len < 510; i++) {
        buf[len++] = ' ';
        buf[len++] = ' ';
    }
    buf[len] = '\0';
    append_str(out, buf);
}

static void serialize_node(char** out, XMLNode* node, int indent) {
    XMLAttribute* attr;
    XMLNode* child;
    bool_t has_children;
    if (!node || node->tag.len == 0) return;
    serialize_indent(out, indent);
    append_char(out, '<');
    append_str(out, node->tag.ptr);
    attr = node->attributes;
    while (attr) {
        append_char(out, ' ');
        append_str(out, attr->name.ptr);
        append_str(out, "=\"");
        encode_append(out, attr->value.ptr);
        append_char(out, '"');
        attr = attr->next;
    }
    has_children = node->children != NULL || (node->text.len > 0 && strlen(node->text.ptr) > 0);
    if (!has_children) {
        append_str(out, " />\n");
        return;
    }
    append_char(out, '>');
    child = node->children;
    if (child) {
        append_char(out, '\n');
        while (child) {
            serialize_node(out, child, indent + 1);
            child = child->next;
        }
        serialize_indent(out, indent);
    } else if (node->text.len > 0) {
        encode_append(out, node->text.ptr);
    }
    append_str(out, "</");
    append_str(out, node->tag.ptr);
    append_str(out, ">\n");
}

String xml_serialize(XMLNode* root) {
    char* result = NULL;
    if (!root) return STRING_EMPTY;
    append_str(&result, "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n");
    serialize_node(&result, root, 0);
    return result ? string_create(result) : STRING_EMPTY;
}
