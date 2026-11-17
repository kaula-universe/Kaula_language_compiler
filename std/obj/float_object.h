#ifndef STD_OBJ_FLOAT_OBJECT_H
#define STD_OBJ_FLOAT_OBJECT_H

#include "object.h"

// 浮点数对象结构
typedef struct FloatObject {
    Object base;
    float value;
} FloatObject;

// 浮点数对象方法
extern FloatObject* float_object_create(float value);
extern float float_object_get_value(FloatObject* self);
extern void float_object_set_value(FloatObject* self, float value);
extern FloatObject* float_object_add(FloatObject* self, FloatObject* other);
extern FloatObject* float_object_subtract(FloatObject* self, FloatObject* other);
extern FloatObject* float_object_multiply(FloatObject* self, FloatObject* other);
extern FloatObject* float_object_divide(FloatObject* self, FloatObject* other);
extern bool float_object_equals(FloatObject* self, FloatObject* other);
extern size_t float_object_hash(FloatObject* self);
extern const char* float_object_to_string(FloatObject* self);

#endif // STD_OBJ_FLOAT_OBJECT_H
