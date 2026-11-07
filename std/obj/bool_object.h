#ifndef STD_OBJ_BOOL_OBJECT_H
#define STD_OBJ_BOOL_OBJECT_H

#include "object.h"

// 布尔对象结构
typedef struct BoolObject {
    Object base;
    bool value;
} BoolObject;

// 布尔对象方法
extern BoolObject* bool_object_create(bool value);
extern bool bool_object_get_value(BoolObject* self);
extern void bool_object_set_value(BoolObject* self, bool value);
extern BoolObject* bool_object_and(BoolObject* self, BoolObject* other);
extern BoolObject* bool_object_or(BoolObject* self, BoolObject* other);
extern BoolObject* bool_object_not(BoolObject* self);
extern bool bool_object_equals(BoolObject* self, BoolObject* other);
extern size_t bool_object_hash(BoolObject* self);
extern const char* bool_object_to_string(BoolObject* self);

#endif // STD_OBJ_BOOL_OBJECT_H
