#ifndef STD_OBJ_INT_OBJECT_H
#define STD_OBJ_INT_OBJECT_H

#include "object.h"

// 整数对象结构
typedef struct IntObject {
    Object base;
    int value;
} IntObject;

// 整数对象方法
extern IntObject* int_object_create(int value);
extern int int_object_get_value(IntObject* self);
extern void int_object_set_value(IntObject* self, int value);
extern IntObject* int_object_add(IntObject* self, IntObject* other);
extern IntObject* int_object_subtract(IntObject* self, IntObject* other);
extern IntObject* int_object_multiply(IntObject* self, IntObject* other);
extern IntObject* int_object_divide(IntObject* self, IntObject* other);
extern bool int_object_equals(IntObject* self, IntObject* other);
extern size_t int_object_hash(IntObject* self);
extern const char* int_object_to_string(IntObject* self);

#endif // STD_OBJ_INT_OBJECT_H
