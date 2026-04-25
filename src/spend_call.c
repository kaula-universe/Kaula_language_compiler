#include "kaula.h"

Spendable* spendable_create(size_t capacity) {
    Spendable* sp = (Spendable*)fast_alloc(sizeof(Spendable));
    sp->count = 0;
    sp->call_counter = 0;
    sp->components = (void**)fast_calloc(capacity, sizeof(void*));
    sp->is_locked = false;
    return sp;
}

void spendable_destroy(Spendable* sp) {
    if (sp->components) {
        fast_free(sp->components);
    }
    fast_free(sp);
}

void spendable_add(Spendable* sp, void* component) {
    sp->components[sp->count++] = component;
}

void* spendable_call(Spendable* sp) {
    if (sp->call_counter >= sp->count) {
        return NULL;
    }
    return sp->components[sp->call_counter++];
}

// 新 API: spend_lock - 锁定目标并开始消费流程
void spend_lock(void* target) {
    Spendable* sp = (Spendable*)target;
    if (sp) {
        sp->is_locked = true;
    }
}

// 新 API: spend_call - 消费指定索引的元素
void* spend_call(void* target, int index) {
    Spendable* sp = (Spendable*)target;
    if (sp == NULL || index < 1 || index > sp->count) {
        return NULL;
    }
    // 返回指定索引的元素（1-based index 转换为 0-based）
    return sp->components[index - 1];
}

// 新 API: spend_unlock - 解除锁定
void spend_unlock(void* target) {
    Spendable* sp = (Spendable*)target;
    if (sp) {
        sp->is_locked = false;
    }
}
