#include "int_object.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "../memory/memory.h"

// 整数对象虚函数表
static ObjectVTable int_object_vtable;

// 整数对象销毁函数
static void int_object_destroy(Object* self) {
    std_free(self);
}

// 整数对象比较函数
static bool int_object_equals_impl(Object* self, Object* other) {
    if (self == other) return true;
    if (self == NULL || other == NULL) return false;
    if (strcmp(self->type_name, other->type_name) != 0) return false;
    
    IntObject* int_self = (IntObject*)self;
    IntObject* int_other = (IntObject*)other;
    return int_self->value == int_other->value;
}

// 整数对象哈希函数
static size_t int_object_hash_impl(Object* self) {
    if (self == NULL) return 0;
    IntObject* int_self = (IntObject*)self;
    return (size_t)int_self->value;
}

// 整数对象转换为字符串函数
static const char* int_object_to_string_impl(Object* self) {
    if (self == NULL) return "NULL";
    IntObject* int_self = (IntObject*)self;
    
    // 静态缓冲区，线程不安全但足够用
    static char buffer[32];
    snprintf(buffer, sizeof(buffer), "%d", int_self->value);
    return buffer;
}

// 初始化虚函数表
static void int_object_init_vtable() {
    int_object_vtable.destroy = int_object_destroy;
    int_object_vtable.equals = int_object_equals_impl;
    int_object_vtable.hash = int_object_hash_impl;
    int_object_vtable.to_string = int_object_to_string_impl;
}

// 创建整数对象
IntObject* int_object_create(int value) {
    static bool vtable_init = false;
    if (!vtable_init) {
        int_object_init_vtable();
        vtable_init = true;
    }
    
    IntObject* obj = (IntObject*)object_create(sizeof(IntObject), "IntObject");
    if (obj == NULL) return NULL;
    
    obj->base.vtable = &int_object_vtable;
    obj->value = value;
    
    return obj;
}

// 获取整数值
int int_object_get_value(IntObject* self) {
    if (self == NULL) return 0;
    return self->value;
}

// 设置整数值
void int_object_set_value(IntObject* self, int value) {
    if (self != NULL) {
        self->value = value;
    }
}

// 整数加法
IntObject* int_object_add(IntObject* self, IntObject* other) {
    if (self == NULL || other == NULL) return NULL;
    return int_object_create(self->value + other->value);
}

// 整数减法
IntObject* int_object_subtract(IntObject* self, IntObject* other) {
    if (self == NULL || other == NULL) return NULL;
    return int_object_create(self->value - other->value);
}

// 整数乘法
IntObject* int_object_multiply(IntObject* self, IntObject* other) {
    if (self == NULL || other == NULL) return NULL;
    return int_object_create(self->value * other->value);
}

// 整数除法
IntObject* int_object_divide(IntObject* self, IntObject* other) {
    if (self == NULL || other == NULL || other->value == 0) return NULL;
    return int_object_create(self->value / other->value);
}

// 整数比较
bool int_object_equals(IntObject* self, IntObject* other) {
    if (self == NULL || other == NULL) return false;
    return self->value == other->value;
}

// 整数哈希
size_t int_object_hash(IntObject* self) {
    if (self == NULL) return 0;
    return (size_t)self->value;
}

// 整数转换为字符串
const char* int_object_to_string(IntObject* self) {
    if (self == NULL) return "NULL";
    return int_object_to_string_impl((Object*)self);
}
