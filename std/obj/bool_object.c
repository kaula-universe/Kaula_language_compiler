#include "bool_object.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "../memory/memory.h"

// 布尔对象虚函数表
static ObjectVTable bool_object_vtable;

// 布尔对象销毁函数
static void bool_object_destroy(Object* self) {
    std_free(self);
}

// 布尔对象比较函数
static bool bool_object_equals_impl(Object* self, Object* other) {
    if (self == other) return true;
    if (self == NULL || other == NULL) return false;
    if (strcmp(self->type_name, other->type_name) != 0) return false;
    
    BoolObject* bool_self = (BoolObject*)self;
    BoolObject* bool_other = (BoolObject*)other;
    return bool_self->value == bool_other->value;
}

// 布尔对象哈希函数
static size_t bool_object_hash_impl(Object* self) {
    if (self == NULL) return 0;
    BoolObject* bool_self = (BoolObject*)self;
    return bool_self->value ? 1 : 0;
}

// 布尔对象转换为字符串函数
static const char* bool_object_to_string_impl(Object* self) {
    if (self == NULL) return "NULL";
    BoolObject* bool_self = (BoolObject*)self;
    return bool_self->value ? "true" : "false";
}

// 初始化虚函数表
static void bool_object_init_vtable() {
    bool_object_vtable.destroy = bool_object_destroy;
    bool_object_vtable.equals = bool_object_equals_impl;
    bool_object_vtable.hash = bool_object_hash_impl;
    bool_object_vtable.to_string = bool_object_to_string_impl;
}

// 创建布尔对象
BoolObject* bool_object_create(bool value) {
    static bool vtable_init = false;
    if (!vtable_init) {
        bool_object_init_vtable();
        vtable_init = true;
    }
    
    BoolObject* obj = (BoolObject*)object_create(sizeof(BoolObject), "BoolObject");
    if (obj == NULL) return NULL;
    
    obj->base.vtable = &bool_object_vtable;
    obj->value = value;
    
    return obj;
}

// 获取布尔值
bool bool_object_get_value(BoolObject* self) {
    if (self == NULL) return false;
    return self->value;
}

// 设置布尔值
void bool_object_set_value(BoolObject* self, bool value) {
    if (self != NULL) {
        self->value = value;
    }
}

// 布尔与操作
BoolObject* bool_object_and(BoolObject* self, BoolObject* other) {
    if (self == NULL || other == NULL) return NULL;
    return bool_object_create(self->value && other->value);
}

// 布尔或操作
BoolObject* bool_object_or(BoolObject* self, BoolObject* other) {
    if (self == NULL || other == NULL) return NULL;
    return bool_object_create(self->value || other->value);
}

// 布尔非操作
BoolObject* bool_object_not(BoolObject* self) {
    if (self == NULL) return NULL;
    return bool_object_create(!self->value);
}

// 布尔比较
bool bool_object_equals(BoolObject* self, BoolObject* other) {
    if (self == NULL || other == NULL) return false;
    return self->value == other->value;
}

// 布尔哈希
size_t bool_object_hash(BoolObject* self) {
    if (self == NULL) return 0;
    return self->value ? 1 : 0;
}

// 布尔转换为字符串
const char* bool_object_to_string(BoolObject* self) {
    if (self == NULL) return "NULL";
    return bool_object_to_string_impl((Object*)self);
}
