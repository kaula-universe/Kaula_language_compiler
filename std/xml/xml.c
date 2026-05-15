#include "xml.h"
#include <stdlib.h>
#include <string.h>
#include <stdio.h>

XmlDocument* xml_create(const String version, const String encoding) {
    XmlDocument* doc = (XmlDocument*)calloc(1, sizeof(XmlDocument));
    if (doc) {
        doc->version = version ? string_copy(version) : string_create("1.0");
        doc->encoding = encoding ? string_copy(encoding) : string_create("UTF-8");
    }
    return doc;
}

static void xml_destroy_node(XmlNode* node) {
    if (!node) return;
    string_free(node->name);
    string_free(node->content);
    XmlAttribute* attr = node->attributes;
    while (attr) {
        XmlAttribute* next = attr->next;
        string_free(attr->name);
        string_free(attr->value);
        free(attr);
        attr = next;
    }
    XmlNode* child = node->first_child;
    while (child) {
        XmlNode* next = child->next_sibling;
        xml_destroy_node(child);
        child = next;
    }
    free(node);
}

void xml_destroy(XmlDocument* doc) {
    if (doc) {
        xml_destroy_node(doc->root);
        string_free(doc->version);
        string_free(doc->encoding);
        free(doc);
    }
}

XmlNode* xml_create_element(const String name) {
    XmlNode* node = (XmlNode*)calloc(1, sizeof(XmlNode));
    if (node) {
        node->name = string_copy(name);
        node->type = XML_ELEMENT;
    }
    return node;
}

void xml_destroy_element(XmlNode* node) { xml_destroy_node(node); }

void xml_append_child(XmlNode* parent, XmlNode* child) {
    if (!parent || !child) return;
    child->parent = parent;
    if (!parent->first_child) parent->first_child = child;
    else {
        XmlNode* last = parent->first_child;
        while (last->next_sibling) last = last->next_sibling;
        last->next_sibling = child;
    }
    parent->child_count++;
}

XmlNode* xml_get_first_child(XmlNode* node) { return node ? node->first_child : NULL; }
XmlNode* xml_get_next_sibling(XmlNode* node) { return node ? node->next_sibling : NULL; }
size_t xml_get_child_count(XmlNode* node) { return node ? node->child_count : 0; }

void xml_set_attribute(XmlNode* node, const String name, const String value) {
    if (!node || !name || !value) return;
    XmlAttribute* attr = node->attributes;
    while (attr) {
        if (string_equals(attr->name, name)) { string_free(attr->value); attr->value = string_copy(value); return; }
        attr = attr->next;
    }
    XmlAttribute* new_attr = (XmlAttribute*)calloc(1, sizeof(XmlAttribute));
    new_attr->name = string_copy(name);
    new_attr->value = string_copy(value);
    new_attr->next = node->attributes;
    node->attributes = new_attr;
}

String xml_get_attribute(XmlNode* node, const String name) {
    if (!node || !name) return NULL;
    XmlAttribute* attr = node->attributes;
    while (attr) {
        if (string_equals(attr->name, name)) return attr->value;
        attr = attr->next;
    }
    return NULL;
}

XmlNode* xml_find_element(XmlNode* node, const String name) {
    if (!node || !name) return NULL;
    if (node->type == XML_ELEMENT && string_equals(node->name, name)) return node;
    XmlNode* child = node->first_child;
    while (child) {
        XmlNode* found = xml_find_element(child, name);
        if (found) return found;
        child = child->next_sibling;
    }
    return NULL;
}

XmlNode* xml_find_element_by_attribute(XmlNode* node, const String attr_name, const String attr_value) {
    if (!node) return NULL;
    if (node->type == XML_ELEMENT) {
        String val = xml_get_attribute(node, attr_name);
        if (val && string_equals(val, attr_value)) return node;
    }
    XmlNode* child = node->first_child;
    while (child) {
        XmlNode* found = xml_find_element_by_attribute(child, attr_name, attr_value);
        if (found) return found;
        child = child->next_sibling;
    }
    return NULL;
}

XmlDocument* xml_parse(const String text) {
    if (!text) return NULL;
    XmlDocument* doc = xml_create("1.0", "UTF-8");
    const char* p = text;
    while (*p && *p != '<') p++;
    if (*p != '<') { xml_destroy(doc); return NULL; }
    if (strncmp(p, "<?xml", 5) == 0) {
        p += 5;
        while (*p && *p != '?') p++;
        if (*p) p++;
        while (*p && *p != '>') p++;
        if (*p) p++;
    }
    XmlNode* root = xml_create_element("root");
    doc->root = root;
    while (*p == '<') {
        p++;
        if (*p == '/') break;
        if (*p == '!') { while (*p && *p != '>') p++; if (*p) p++; continue; }
        const char* name_start = p;
        while (*p && *p != ' ' && *p != '>' && *p != '/') p++;
        size_t name_len = p - name_start;
        char* name = (char*)malloc(name_len + 1);
        memcpy(name, name_start, name_len);
        name[name_len] = '\0';
        XmlNode* node = xml_create_element(name);
        free(name);
        while (*p && *p != '>' && *p != '/') {
            while (*p == ' ') p++;
            const char* attr_name_start = p;
            while (*p && *p != '=' && *p != '>' && *p != ' ') p++;
            size_t attr_name_len = p - attr_name_start;
            if (attr_name_len > 0) {
                char* attr_name = (char*)malloc(attr_name_len + 1);
                memcpy(attr_name, attr_name_start, attr_name_len);
                attr_name[attr_name_len] = '\0';
                if (*p == '=') {
                    p++;
                    if (*p == '"') {
                        p++;
                        const char* val_start = p;
                        while (*p && *p != '"') p++;
                        size_t val_len = p - val_start;
                        char* val = (char*)malloc(val_len + 1);
                        memcpy(val, val_start, val_len);
                        val[val_len] = '\0';
                        xml_set_attribute(node, attr_name, val);
                        free(val);
                        if (*p == '"') p++;
                    }
                }
                free(attr_name);
            }
        }
        if (*p == '/') { p += 2; }
        else {
            p++;
            const char* content_start = p;
            while (*p && *p != '<') p++;
            size_t content_len = p - content_start;
            if (content_len > 0) {
                char* content = (char*)malloc(content_len + 1);
                memcpy(content, content_start, content_len);
                content[content_len] = '\0';
                XmlNode* text_node = (XmlNode*)calloc(1, sizeof(XmlNode));
                text_node->type = XML_TEXT;
                text_node->content = string_create(content);
                xml_append_child(node, text_node);
                free(content);
            }
            if (strncmp(p, "</", 2) == 0) {
                while (*p && *p != '>') p++;
                if (*p) p++;
            }
        }
        xml_append_child(root, node);
    }
    return doc;
}

XmlDocument* xml_parse_file(const String path) {
    FILE* f = fopen(path, "r"); if (!f) return NULL;
    fseek(f, 0, SEEK_END); long len = ftell(f); fseek(f, 0, SEEK_SET);
    String buf = (String)malloc(len + 1); fread(buf, 1, len, f); buf[len] = '\0'; fclose(f);
    XmlDocument* doc = xml_parse(buf); free(buf); return doc;
}

static void xml_write_node(StringBuilder* sb, XmlNode* node, int indent) {
    for (int i = 0; i < indent; i++) string_builder_append_char(sb, ' ');
    string_builder_append_char(sb, '<');
    string_builder_append(sb, node->name);
    XmlAttribute* attr = node->attributes;
    while (attr) {
        string_builder_append_char(sb, ' ');
        string_builder_append(sb, attr->name);
        string_builder_append(sb, "=\"");
        string_builder_append(sb, attr->value);
        string_builder_append_char(sb, '"');
        attr = attr->next;
    }
    if (!node->first_child && !node->content) {
        string_builder_append(sb, "/>\n");
        return;
    }
    string_builder_append_char(sb, '>');
    if (node->content) string_builder_append(sb, node->content);
    string_builder_append_char(sb, '\n');
    XmlNode* child = node->first_child;
    while (child) {
        if (child->type == XML_ELEMENT) xml_write_node(sb, child, indent + 2);
        else { string_builder_append(sb, child->content); string_builder_append_char(sb, '\n'); }
        child = child->next_sibling;
    }
    for (int i = 0; i < indent; i++) string_builder_append_char(sb, ' ');
    string_builder_append(sb, "</");
    string_builder_append(sb, node->name);
    string_builder_append(sb, ">\n");
}

String xml_serialize(XmlDocument* doc) {
    if (!doc || !doc->root) return NULL;
    StringBuilder* sb = string_builder_create();
    string_builder_append(sb, "<?xml version=\"");
    string_builder_append(sb, doc->version);
    string_builder_append(sb, "\" encoding=\"");
    string_builder_append(sb, doc->encoding);
    string_builder_append(sb, "\"?>\n");
    xml_write_node(sb, doc->root, 0);
    String result = string_builder_to_string(sb);
    string_builder_destroy(sb);
    return result;
}

bool_t xml_to_file(XmlDocument* doc, const String path) {
    String s = xml_serialize(doc); if (!s) return false;
    FILE* f = fopen(path, "w"); if (!f) { string_free(s); return false; }
    fputs(s, f); fclose(f); string_free(s); return true;
}
