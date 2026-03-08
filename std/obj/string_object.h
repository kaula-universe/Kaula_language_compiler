#ifndef STD_OBJ_STRING_OBJECT_H
#define STD_OBJ_STRING_OBJECT_H

#include "object.h"

// 字符串对象结构
typedef struct StringObject {
    Object base;
    char* value;
} StringObject;

// 字符串对象方法
extern StringObject* string_object_create(const char* value);
extern const char* string_object_get_value(StringObject* self);
extern void string_object_set_value(StringObject* self, const char* value);
extern StringObject* string_object_concat(StringObject* self, StringObject* other);
extern size_t string_object_length(StringObject* self);
extern bool string_object_equals(StringObject* self, StringObject* other);
extern size_t string_object_hash(StringObject* self);
extern const char* string_object_to_string(StringObject* self);

#endif // STD_OBJ_STRING_OBJECT_H
