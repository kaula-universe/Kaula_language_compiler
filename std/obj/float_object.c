#include "float_object.h"
#include <stdio.h>
#include <stdlib.h>
#include <math.h>
#include <string.h>
#include "../memory/memory.h"

// 浮点数对象虚函数表
static ObjectVTable float_object_vtable;

// 浮点数对象销毁函数
static void float_object_destroy(Object* self) {
    std_free(self);
}

// 浮点数对象比较函数
static bool float_object_equals_impl(Object* self, Object* other) {
    if (self == other) return true;
    if (self == NULL || other == NULL) return false;
    if (strcmp(self->type_name, other->type_name) != 0) return false;
    
    FloatObject* float_self = (FloatObject*)self;
    FloatObject* float_other = (FloatObject*)other;
    // 浮点数比较需要考虑精度
    return fabs(float_self->value - float_other->value) < 1e-6;
}

// 浮点数对象哈希函数
static size_t float_object_hash_impl(Object* self) {
    if (self == NULL) return 0;
    FloatObject* float_self = (FloatObject*)self;
    // 将浮点数转换为整数进行哈希
    union {
        float f;
        uint32_t i;
    } u;
    u.f = float_self->value;
    return (size_t)u.i;
}

// 浮点数对象转换为字符串函数
static const char* float_object_to_string_impl(Object* self) {
    if (self == NULL) return "NULL";
    FloatObject* float_self = (FloatObject*)self;
    
    // 静态缓冲区，线程不安全但足够用
    static char buffer[32];
    snprintf(buffer, sizeof(buffer), "%f", float_self->value);
    return buffer;
}

// 初始化虚函数表
static void float_object_init_vtable() {
    float_object_vtable.destroy = float_object_destroy;
    float_object_vtable.equals = float_object_equals_impl;
    float_object_vtable.hash = float_object_hash_impl;
    float_object_vtable.to_string = float_object_to_string_impl;
}

// 创建浮点数对象
FloatObject* float_object_create(float value) {
    static bool vtable_init = false;
    if (!vtable_init) {
        float_object_init_vtable();
        vtable_init = true;
    }
    
    FloatObject* obj = (FloatObject*)object_create(sizeof(FloatObject), "FloatObject");
    if (obj == NULL) return NULL;
    
    obj->base.vtable = &float_object_vtable;
    obj->value = value;
    
    return obj;
}

// 获取浮点数值
float float_object_get_value(FloatObject* self) {
    if (self == NULL) return 0.0f;
    return self->value;
}

// 设置浮点数值
void float_object_set_value(FloatObject* self, float value) {
    if (self != NULL) {
        self->value = value;
    }
}

// 浮点数加法
FloatObject* float_object_add(FloatObject* self, FloatObject* other) {
    if (self == NULL || other == NULL) return NULL;
    return float_object_create(self->value + other->value);
}

// 浮点数减法
FloatObject* float_object_subtract(FloatObject* self, FloatObject* other) {
    if (self == NULL || other == NULL) return NULL;
    return float_object_create(self->value - other->value);
}

// 浮点数乘法
FloatObject* float_object_multiply(FloatObject* self, FloatObject* other) {
    if (self == NULL || other == NULL) return NULL;
    return float_object_create(self->value * other->value);
}

// 浮点数除法
FloatObject* float_object_divide(FloatObject* self, FloatObject* other) {
    if (self == NULL || other == NULL || other->value == 0.0f) return NULL;
    return float_object_create(self->value / other->value);
}

// 浮点数比较
bool float_object_equals(FloatObject* self, FloatObject* other) {
    if (self == NULL || other == NULL) return false;
    return fabs(self->value - other->value) < 1e-6;
}

// 浮点数哈希
size_t float_object_hash(FloatObject* self) {
    if (self == NULL) return 0;
    union {
        float f;
        uint32_t i;
    } u;
    u.f = self->value;
    return (size_t)u.i;
}

// 浮点数转换为字符串
const char* float_object_to_string(FloatObject* self) {
    if (self == NULL) return "NULL";
    return float_object_to_string_impl((Object*)self);
}
