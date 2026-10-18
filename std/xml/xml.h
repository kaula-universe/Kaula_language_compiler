#ifndef STD_XML_XML_H
#define STD_XML_XML_H

#include "../base/types.h"
#include "../string/string.h"
#include "../container/container.h"

typedef enum {
    XML_ELEMENT,
    XML_TEXT,
    XML_COMMENT,
    XML_ATTRIBUTE
} XmlNodeType;

typedef struct XmlAttribute {
    String name;
    String value;
    struct XmlAttribute* next;
} XmlAttribute;

typedef struct XmlNode {
    String name;
    String content;
    XmlNodeType type;
    struct XmlNode* parent;
    struct XmlNode* first_child;
    struct XmlNode* next_sibling;
    XmlAttribute* attributes;
    size_t child_count;
} XmlNode;

typedef struct XmlDocument {
    XmlNode* root;
    String version;
    String encoding;
} XmlDocument;

extern XmlDocument* xml_create(const String version, const String encoding);
extern void xml_destroy(XmlDocument* doc);
extern XmlNode* xml_create_element(const String name);
extern void xml_destroy_element(XmlNode* node);

extern void xml_append_child(XmlNode* parent, XmlNode* child);
extern XmlNode* xml_get_first_child(XmlNode* node);
extern XmlNode* xml_get_next_sibling(XmlNode* node);
extern size_t xml_get_child_count(XmlNode* node);

extern void xml_set_attribute(XmlNode* node, const String name, const String value);
extern String xml_get_attribute(XmlNode* node, const String name);

extern XmlNode* xml_find_element(XmlNode* node, const String name);
extern XmlNode* xml_find_element_by_attribute(XmlNode* node, const String attr_name, const String attr_value);

extern XmlDocument* xml_parse(const String text);
extern XmlDocument* xml_parse_file(const String path);

extern String xml_serialize(XmlDocument* doc);
extern bool_t xml_to_file(XmlDocument* doc, const String path);

#endif // STD_XML_XML_H
