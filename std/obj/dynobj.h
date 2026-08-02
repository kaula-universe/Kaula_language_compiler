#ifndef STD_OBJ_DYNOBJ_H
#define STD_OBJ_DYNOBJ_H

#include "object.h"
#include <stddef.h>
#include <stdint.h>

// 动态对象（DynObject）：字符串键 → Object* 的哈希表
// 用于在语言层面模拟动态对象（类似 JS/Python 的 dict + 封装）
typedef struct DynEntry {
    char* key;
    Object* value;
    struct DynEntry* next;
} DynEntry;

typedef struct DynObject {
    Object base;
    DynEntry** buckets;
    size_t bucket_count;
    size_t count;
} DynObject;

// 动态对象创建/销毁
extern DynObject* dynobj_create(void);
extern Object* dynobj_set(Object* self, const char* key, Object* value);
extern Object* dynobj_get(Object* self, const char* key);
extern bool dynobj_contains(Object* self, const char* key);
extern bool dynobj_delete(Object* self, const char* key);
extern void dynobj_clear(Object* self);
extern size_t dynobj_size(Object* self);

// 动态方法调用：查找并调用对象中存储的函数对象
// 约定：方法函数签名为 Object* (*)(Object* self, size_t nargs, Object** argv)
extern Object* dynobj_invoke(Object* self, const char* method, size_t nargs, ...);

// 装箱/拆箱：Object* ↔ 基本类型（64 位精度）
extern Object* dynobj_box_i64(int64_t v);
extern Object* dynobj_box_f64(double v);
extern Object* dynobj_box_bool(int b);
extern Object* dynobj_box_cstr(const char* s);
extern int64_t dynobj_unbox_i64(Object* v);
extern double dynobj_unbox_f64(Object* v);
extern int dynobj_unbox_bool(Object* v);
extern const char* dynobj_unbox_cstr(Object* v);

// 类型判断
extern bool dynobj_is_int(Object* v);
extern bool dynobj_is_float(Object* v);
extern bool dynobj_is_string(Object* v);

// 函数对象：包装 C 函数指针，可存储在动态对象中作为方法
typedef struct FuncObject {
    Object base;
    void* fnptr;
} FuncObject;

extern FuncObject* func_object_create(void* fnptr);
extern void* func_object_fnptr(Object* self);

#endif // STD_OBJ_DYNOBJ_H
