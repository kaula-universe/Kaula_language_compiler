#include "string_object.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "../memory/memory.h"

// 内存管理函数别名
#define memory_alloc std_malloc
#define memory_free std_free

// 字符串对象虚函数表
static ObjectVTable string_object_vtable;

// 字符串对象销毁函数
static void string_object_destroy(Object* self) {
    if (self != NULL) {
        StringObject* string_self = (StringObject*)self;
        if (string_self->value != NULL) {
            memory_free(string_self->value);
        }
        memory_free(self);
    }
}

// 字符串对象比较函数
static bool string_object_equals_impl(Object* self, Object* other) {
    if (self == other) return true;
    if (self == NULL || other == NULL) return false;
    if (strcmp(self->type_name, other->type_name) != 0) return false;
    
    StringObject* string_self = (StringObject*)self;
    StringObject* string_other = (StringObject*)other;
    
    if (string_self->value == NULL && string_other->value == NULL) return true;
    if (string_self->value == NULL || string_other->value == NULL) return false;
    
    return strcmp(string_self->value, string_other->value) == 0;
}

// 字符串对象哈希函数
static size_t string_object_hash_impl(Object* self) {
    if (self == NULL) return 0;
    StringObject* string_self = (StringObject*)self;
    if (string_self->value == NULL) return 0;
    
    // 简单的字符串哈希函数
    size_t hash = 0;
    const char* str = string_self->value;
    while (*str) {
        hash = hash * 31 + *str++;
    }
    return hash;
}

// 字符串对象转换为字符串函数
static const char* string_object_to_string_impl(Object* self) {
    if (self == NULL) return "NULL";
    StringObject* string_self = (StringObject*)self;
    return string_self->value != NULL ? string_self->value : "";
}

// 初始化虚函数表
static void string_object_init_vtable() {
    string_object_vtable.destroy = string_object_destroy;
    string_object_vtable.equals = string_object_equals_impl;
    string_object_vtable.hash = string_object_hash_impl;
    string_object_vtable.to_string = string_object_to_string_impl;
}

// 创建字符串对象
StringObject* string_object_create(const char* value) {
    static bool vtable_init = false;
    if (!vtable_init) {
        string_object_init_vtable();
        vtable_init = true;
    }
    
    StringObject* obj = (StringObject*)object_create(sizeof(StringObject), "StringObject");
    if (obj == NULL) return NULL;
    
    obj->base.vtable = &string_object_vtable;
    obj->value = NULL;
    
    if (value != NULL) {
        size_t len = strlen(value);
        obj->value = (char*)memory_alloc(len + 1);
        if (obj->value == NULL) {
            memory_free(obj);
            return NULL;
        }
        strcpy(obj->value, value);
    }
    
    return obj;
}

// 获取字符串值
const char* string_object_get_value(StringObject* self) {
    if (self == NULL) return "";
    return self->value != NULL ? self->value : "";
}

// 设置字符串值
void string_object_set_value(StringObject* self, const char* value) {
    if (self != NULL) {
        if (self->value != NULL) {
            memory_free(self->value);
            self->value = NULL;
        }
        
        if (value != NULL) {
            size_t len = strlen(value);
            self->value = (char*)memory_alloc(len + 1);
            if (self->value != NULL) {
                strcpy(self->value, value);
            }
        }
    }
}

// 字符串连接
StringObject* string_object_concat(StringObject* self, StringObject* other) {
    if (self == NULL || other == NULL) return NULL;
    
    const char* str1 = self->value != NULL ? self->value : "";
    const char* str2 = other->value != NULL ? other->value : "";
    
    size_t len1 = strlen(str1);
    size_t len2 = strlen(str2);
    size_t total_len = len1 + len2;
    
    char* new_str = (char*)memory_alloc(total_len + 1);
    if (new_str == NULL) return NULL;
    
    strcpy(new_str, str1);
    strcat(new_str, str2);
    
    StringObject* result = string_object_create(new_str);
    memory_free(new_str);
    
    return result;
}

// 获取字符串长度
size_t string_object_length(StringObject* self) {
    if (self == NULL || self->value == NULL) return 0;
    return strlen(self->value);
}

// 字符串比较
bool string_object_equals(StringObject* self, StringObject* other) {
    if (self == NULL || other == NULL) return false;
    if (self->value == NULL && other->value == NULL) return true;
    if (self->value == NULL || other->value == NULL) return false;
    return strcmp(self->value, other->value) == 0;
}

// 字符串哈希
size_t string_object_hash(StringObject* self) {
    if (self == NULL || self->value == NULL) return 0;
    size_t hash = 0;
    const char* str = self->value;
    while (*str) {
        hash = hash * 31 + *str++;
    }
    return hash;
}

// 字符串转换为字符串
const char* string_object_to_string(StringObject* self) {
    if (self == NULL) return "NULL";
    return self->value != NULL ? self->value : "";
}
