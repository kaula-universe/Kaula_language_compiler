#include "object.h"
#include "../memory/memory.h"
#include <string.h>
#include <stdlib.h>

// 内存管理函数别名 - 使用标准分配
#define memory_alloc(size) malloc(size)
#define memory_free(ptr) free(ptr)

// 基础虚函数表
static ObjectVTable object_vtable = {
    .destroy = NULL, // 由具体实现覆盖
    .equals = NULL, // 由具体实现覆盖
    .hash = NULL, // 由具体实现覆盖
    .to_string = NULL // 由具体实现覆盖
};

// 创建对象
Object* object_create(size_t size, const char* type_name) {
    Object* obj = (Object*)memory_alloc(size);
    if (obj == NULL) return NULL;
    
    obj->type_name = type_name;
    obj->type_size = size;
    obj->ref_count = 1;
    obj->vtable = &object_vtable;
    
    return obj;
}

// 销毁对象
void object_destroy(Object* self) {
    if (self != NULL) {
        memory_free(self);
    }
}

// 增加引用计数
void object_retain(Object* self) {
    if (self != NULL) {
        self->ref_count++;
    }
}

// 减少引用计数
void object_release(Object* self) {
    if (self != NULL && --self->ref_count == 0) {
        if (self->vtable->destroy != NULL) {
            self->vtable->destroy(self);
        } else {
            object_destroy(self);
        }
    }
}

// 比较对象
bool object_equals(Object* self, Object* other) {
    if (self == other) return true;
    if (self == NULL || other == NULL) return false;
    if (self->vtable->equals != NULL) {
        return self->vtable->equals(self, other);
    }
    return false;
}

// 获取对象哈希值
size_t object_hash(Object* self) {
    if (self == NULL) return 0;
    if (self->vtable->hash != NULL) {
        return self->vtable->hash(self);
    }
    return (size_t)self;
}

// 将对象转换为字符串
const char* object_to_string(Object* self) {
    if (self == NULL) return "NULL";
    if (self->vtable->to_string != NULL) {
        return self->vtable->to_string(self);
    }
    return self->type_name;
}
