#include "kaula.h"

static inline Spendable* spendable_create(size_t capacity) {
    Spendable* sp = (Spendable*)fast_alloc(sizeof(Spendable));
    sp->count = 0;
    sp->call_counter = 0;
    sp->components = (void**)fast_calloc(capacity, sizeof(void*));
    return sp;
}

static inline void spendable_destroy(Spendable* sp) {
    if (sp->components) {
        fast_free(sp->components);
    }
    fast_free(sp);
}

static inline void spendable_add(Spendable* sp, void* component) {
    sp->components[sp->count++] = component;
}

static inline void* spendable_call(Spendable* sp) {
    if (sp->call_counter >= sp->count) {
        return NULL;
    }
    return sp->components[sp->call_counter++];
}

// 新 API: spend_lock - 锁定目标并开始消费流程
static inline void spend_lock(void* target) {
    // 目标已经被锁定，消费流程开始
    // 在这个实现中，我们简单地将 target 视为 Spendable*
    // 调用者需要确保 target 是有效的 Spendable*
}

// 新 API: spend_call - 消费指定索引的元素
static inline void* spend_call(void* target, int index) {
    Spendable* sp = (Spendable*)target;
    if (sp == NULL || index < 1 || index > sp->count) {
        return NULL;
    }
    // 返回指定索引的元素（1-based index 转换为 0-based）
    return sp->components[index - 1];
}

// 新 API: spend_unlock - 解除锁定
static inline void spend_unlock(void* target) {
    // 目标解除锁定，消费流程结束
    // 在这个实现中，我们不需要做任何事情
}
