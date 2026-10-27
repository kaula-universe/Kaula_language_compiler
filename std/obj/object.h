#ifndef STD_OBJ_OBJECT_H
#define STD_OBJ_OBJECT_H

#include "../base/types.h"

// 基础对象结构
typedef struct Object {
    // 类型信息
    const char* type_name;
    size_t type_size;
    
    // 引用计数
    size_t ref_count;
    
    // 虚函数表
    struct ObjectVTable* vtable;
} Object;

// 虚函数表
typedef struct ObjectVTable {
    void (*destroy)(Object* self);
    bool (*equals)(Object* self, Object* other);
    size_t (*hash)(Object* self);
    const char* (*to_string)(Object* self);
} ObjectVTable;

// 基础方法
extern Object* object_create(size_t size, const char* type_name);
extern void object_destroy(Object* self);
extern void object_retain(Object* self);
extern void object_release(Object* self);
extern bool object_equals(Object* self, Object* other);
extern size_t object_hash(Object* self);
extern const char* object_to_string(Object* self);

#endif // STD_OBJ_OBJECT_H
