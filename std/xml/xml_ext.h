#pragma once
#include "../base/types.h"
#include "../string/string.h"

typedef struct XMLAttribute {
    String name;
    String value;
    struct XMLAttribute* next;
} XMLAttribute;

typedef struct XMLNode {
    String tag;
    String text;
    XMLAttribute* attributes;
    struct XMLNode* children;
    struct XMLNode* next;
    struct XMLNode* parent;
} XMLNode;

XMLNode* xml_parse_file(const char* filename);
XMLNode* xml_parse_string(const char* str);

const char* xml_node_get_attribute(XMLNode* node, const char* name);
XMLNode* xml_node_get_child_by_tag(XMLNode* node, const char* tag);
String xml_node_get_text_content(XMLNode* node);

String xml_serialize(XMLNode* root);

XMLNode* xml_create_node(const char* tag);
void xml_add_child(XMLNode* parent, XMLNode* child);
void xml_add_attribute(XMLNode* node, const char* name, const char* value);

void xml_free(XMLNode* root);
