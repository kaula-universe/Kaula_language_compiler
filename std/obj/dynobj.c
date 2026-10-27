#include "dynobj.h"
#include "string_object.h"
#include "../memory/memory.h"
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

// 内存管理函数别名 - 与 string_object.c 保持一致
#define memory_alloc std_malloc
#define memory_free std_free

// ============================================================================
// 64 位整数对象（IntObject 只有 32 位，动态对象需要完整 int64 精度）
// ============================================================================

typedef struct DynIntObject {
    Object base;
    int64_t value;
} DynIntObject;

static ObjectVTable dyn_int_vtable;

static void dyn_int_destroy(Object* self) {
    memory_free(self);
}

static bool dyn_int_equals_impl(Object* self, Object* other) {
    if (self == other) return true;
    if (self == NULL || other == NULL) return false;
    if (strcmp(self->type_name, other->type_name) != 0) return false;
    DynIntObject* a = (DynIntObject*)self;
    DynIntObject* b = (DynIntObject*)other;
    return a->value == b->value;
}

static size_t dyn_int_hash_impl(Object* self) {
    DynIntObject* a = (DynIntObject*)self;
    return (size_t)a->value;
}

static const char* dyn_int_to_string_impl(Object* self) {
    DynIntObject* a = (DynIntObject*)self;
    static char buffer[32];
    snprintf(buffer, sizeof(buffer), "%lld", (long long)a->value);
    return buffer;
}

static void dyn_int_init_vtable(void) {
    dyn_int_vtable.destroy = dyn_int_destroy;
    dyn_int_vtable.equals = dyn_int_equals_impl;
    dyn_int_vtable.hash = dyn_int_hash_impl;
    dyn_int_vtable.to_string = dyn_int_to_string_impl;
}

static DynIntObject* dyn_int_create(int64_t value) {
    static bool vtable_init = false;
    if (!vtable_init) {
        dyn_int_init_vtable();
        vtable_init = true;
    }
    DynIntObject* obj = (DynIntObject*)object_create(sizeof(DynIntObject), "IntObject");
    if (obj == NULL) return NULL;
    obj->base.vtable = &dyn_int_vtable;
    obj->value = value;
    return obj;
}

// ============================================================================
// 64 位浮点对象
// ============================================================================

typedef struct DynFloatObject {
    Object base;
    double value;
} DynFloatObject;

static ObjectVTable dyn_float_vtable;

static void dyn_float_destroy(Object* self) {
    memory_free(self);
}

static bool dyn_float_equals_impl(Object* self, Object* other) {
    if (self == other) return true;
    if (self == NULL || other == NULL) return false;
    if (strcmp(self->type_name, other->type_name) != 0) return false;
    DynFloatObject* a = (DynFloatObject*)self;
    DynFloatObject* b = (DynFloatObject*)other;
    return a->value == b->value;
}

static size_t dyn_float_hash_impl(Object* self) {
    DynFloatObject* a = (DynFloatObject*)self;
    return (size_t)a->value;
}

static const char* dyn_float_to_string_impl(Object* self) {
    DynFloatObject* a = (DynFloatObject*)self;
    static char buffer[32];
    snprintf(buffer, sizeof(buffer), "%g", a->value);
    return buffer;
}

static void dyn_float_init_vtable(void) {
    dyn_float_vtable.destroy = dyn_float_destroy;
    dyn_float_vtable.equals = dyn_float_equals_impl;
    dyn_float_vtable.hash = dyn_float_hash_impl;
    dyn_float_vtable.to_string = dyn_float_to_string_impl;
}

static DynFloatObject* dyn_float_create(double value) {
    static bool vtable_init = false;
    if (!vtable_init) {
        dyn_float_init_vtable();
        vtable_init = true;
    }
    DynFloatObject* obj = (DynFloatObject*)object_create(sizeof(DynFloatObject), "FloatObject");
    if (obj == NULL) return NULL;
    obj->base.vtable = &dyn_float_vtable;
    obj->value = value;
    return obj;
}

// ============================================================================
// 装箱/拆箱
// ============================================================================

Object* dynobj_box_i64(int64_t v) {
    return (Object*)dyn_int_create(v);
}

Object* dynobj_box_f64(double v) {
    return (Object*)dyn_float_create(v);
}

Object* dynobj_box_bool(int b) {
    return (Object*)dyn_int_create(b ? 1 : 0);
}

Object* dynobj_box_cstr(const char* s) {
    return (Object*)string_object_create(s != NULL ? s : "");
}

int64_t dynobj_unbox_i64(Object* v) {
    if (v == NULL) return 0;
    if (strcmp(v->type_name, "IntObject") == 0) {
        return ((DynIntObject*)v)->value;
    }
    return 0;
}

double dynobj_unbox_f64(Object* v) {
    if (v == NULL) return 0;
    if (strcmp(v->type_name, "FloatObject") == 0) {
        return ((DynFloatObject*)v)->value;
    }
    if (strcmp(v->type_name, "IntObject") == 0) {
        return (double)((DynIntObject*)v)->value;
    }
    return 0;
}

int dynobj_unbox_bool(Object* v) {
    if (v == NULL) return 0;
    return dynobj_unbox_i64(v) != 0;
}

const char* dynobj_unbox_cstr(Object* v) {
    if (v == NULL) return "";
    if (strcmp(v->type_name, "StringObject") == 0) {
        return string_object_get_value((StringObject*)v);
    }
    return object_to_string(v);
}

bool dynobj_is_int(Object* v) {
    return v != NULL && strcmp(v->type_name, "IntObject") == 0;
}

bool dynobj_is_float(Object* v) {
    return v != NULL && strcmp(v->type_name, "FloatObject") == 0;
}

bool dynobj_is_string(Object* v) {
    return v != NULL && strcmp(v->type_name, "StringObject") == 0;
}

// ============================================================================
// 函数对象
// ============================================================================

static ObjectVTable func_object_vtable;

static void func_object_destroy(Object* self) {
    memory_free(self);
}

static void func_object_init_vtable(void) {
    func_object_vtable.destroy = func_object_destroy;
    func_object_vtable.equals = NULL;
    func_object_vtable.hash = NULL;
    func_object_vtable.to_string = NULL;
}

FuncObject* func_object_create(void* fnptr) {
    static bool vtable_init = false;
    if (!vtable_init) {
        func_object_init_vtable();
        vtable_init = true;
    }
    FuncObject* obj = (FuncObject*)object_create(sizeof(FuncObject), "FuncObject");
    if (obj == NULL) return NULL;
    obj->base.vtable = &func_object_vtable;
    obj->fnptr = fnptr;
    return obj;
}

void* func_object_fnptr(Object* self) {
    if (self == NULL || strcmp(self->type_name, "FuncObject") != 0) return NULL;
    return ((FuncObject*)self)->fnptr;
}

// ============================================================================
// 动态对象（哈希表）
// ============================================================================

static const size_t DYNOBJ_DEFAULT_BUCKETS = 16;

static uint64_t dynobj_hash_key(const char* key) {
    // FNV-1a
    uint64_t hash = 1469598103934665603ULL;
    while (*key) {
        hash ^= (unsigned char)*key;
        hash *= 1099511628211ULL;
        key++;
    }
    return hash;
}

static void dynobj_rehash(DynObject* self) {
    size_t new_size = self->bucket_count * 2;
    DynEntry** new_buckets = (DynEntry**)memory_alloc(new_size * sizeof(DynEntry*));
    if (new_buckets == NULL) return;
    memset(new_buckets, 0, new_size * sizeof(DynEntry*));

    for (size_t i = 0; i < self->bucket_count; i++) {
        DynEntry* entry = self->buckets[i];
        while (entry != NULL) {
            DynEntry* next = entry->next;
            size_t idx = dynobj_hash_key(entry->key) % new_size;
            entry->next = new_buckets[idx];
            new_buckets[idx] = entry;
            entry = next;
        }
    }

    memory_free(self->buckets);
    self->buckets = new_buckets;
    self->bucket_count = new_size;
}

static void dyn_object_destroy(Object* base) {
    DynObject* self = (DynObject*)base;
    dynobj_clear(base);
    memory_free(self->buckets);
    memory_free(self);
}

static ObjectVTable dyn_object_vtable;

DynObject* dynobj_create(void) {
    static bool vtable_init = false;
    if (!vtable_init) {
        dyn_object_vtable.destroy = dyn_object_destroy;
        dyn_object_vtable.equals = NULL;
        dyn_object_vtable.hash = NULL;
        dyn_object_vtable.to_string = NULL;
        vtable_init = true;
    }

    DynObject* obj = (DynObject*)object_create(sizeof(DynObject), "DynObject");
    if (obj == NULL) return NULL;
    obj->base.vtable = &dyn_object_vtable;
    obj->bucket_count = DYNOBJ_DEFAULT_BUCKETS;
    obj->count = 0;
    obj->buckets = (DynEntry**)memory_alloc(obj->bucket_count * sizeof(DynEntry*));
    if (obj->buckets == NULL) {
        memory_free(obj);
        return NULL;
    }
    memset(obj->buckets, 0, obj->bucket_count * sizeof(DynEntry*));
    return obj;
}

Object* dynobj_set(Object* base, const char* key, Object* value) {
	if (base == NULL || key == NULL) return base;
	DynObject* self = (DynObject*)base;

	if (self->count >= self->bucket_count * 2) {
		dynobj_rehash(self);
	}

	size_t idx = dynobj_hash_key(key) % self->bucket_count;
	DynEntry* entry = self->buckets[idx];
	while (entry != NULL) {
		if (strcmp(entry->key, key) == 0) {
			if (value != NULL) object_retain(value);
			if (entry->value != NULL) object_release(entry->value);
			entry->value = value;
			return base;
		}
		entry = entry->next;
	}

	entry = (DynEntry*)memory_alloc(sizeof(DynEntry));
	if (entry == NULL) return base;
	entry->key = (char*)memory_alloc(strlen(key) + 1);
	if (entry->key == NULL) {
		memory_free(entry);
		return base;
	}
	strcpy(entry->key, key);
	entry->value = value;
	if (value != NULL) object_retain(value);
	entry->next = self->buckets[idx];
	self->buckets[idx] = entry;
	self->count++;
	return base;
}

Object* dynobj_get(Object* base, const char* key) {
    if (base == NULL || key == NULL) return NULL;
    DynObject* self = (DynObject*)base;

    size_t idx = dynobj_hash_key(key) % self->bucket_count;
    DynEntry* entry = self->buckets[idx];
    while (entry != NULL) {
        if (strcmp(entry->key, key) == 0) {
            return entry->value;
        }
        entry = entry->next;
    }
    return NULL;
}

bool dynobj_contains(Object* base, const char* key) {
    return dynobj_get(base, key) != NULL;
}

bool dynobj_delete(Object* base, const char* key) {
    if (base == NULL || key == NULL) return false;
    DynObject* self = (DynObject*)base;

    size_t idx = dynobj_hash_key(key) % self->bucket_count;
    DynEntry** link = &self->buckets[idx];
    while (*link != NULL) {
        DynEntry* entry = *link;
        if (strcmp(entry->key, key) == 0) {
            *link = entry->next;
            if (entry->value != NULL) object_release(entry->value);
            memory_free(entry->key);
            memory_free(entry);
            self->count--;
            return true;
        }
        link = &entry->next;
    }
    return false;
}

void dynobj_clear(Object* base) {
    if (base == NULL) return;
    DynObject* self = (DynObject*)base;
    for (size_t i = 0; i < self->bucket_count; i++) {
        DynEntry* entry = self->buckets[i];
        while (entry != NULL) {
            DynEntry* next = entry->next;
            if (entry->value != NULL) object_release(entry->value);
            memory_free(entry->key);
            memory_free(entry);
            entry = next;
        }
        self->buckets[i] = NULL;
    }
    self->count = 0;
}

size_t dynobj_size(Object* base) {
    if (base == NULL) return 0;
    return ((DynObject*)base)->count;
}

Object* dynobj_invoke(Object* base, const char* method, size_t nargs, ...) {
    if (base == NULL || method == NULL) return NULL;
    Object* fn = dynobj_get(base, method);
    if (fn == NULL || strcmp(fn->type_name, "FuncObject") != 0) return NULL;

    // 收集变参中的装箱参数
    Object** argv = NULL;
    if (nargs > 0) {
        argv = (Object**)memory_alloc(nargs * sizeof(Object*));
        if (argv == NULL) return NULL;
    }

    va_list ap;
    va_start(ap, nargs);
    for (size_t i = 0; i < nargs; i++) {
        argv[i] = va_arg(ap, Object*);
    }
    va_end(ap);

    void* fnptr = ((FuncObject*)fn)->fnptr;
    Object* result = ((Object* (*)(Object*, size_t, Object**))fnptr)(base, nargs, argv);

    if (argv != NULL) {
        memory_free(argv);
    }
    return result;
}
