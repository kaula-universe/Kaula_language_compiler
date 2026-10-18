#include "kaula.h"
#include <stdio.h>
#include <stdlib.h>

// 消费流违规：打印错误并终止（编译期已静态证明不会发生；此处为运行时安全网）
static void spend_panic(const char* msg) {
    fprintf(stderr, "[Spendable] %s\n", msg);
    abort();
}

Spendable* spendable_create(size_t capacity) {
    Spendable* sp = (Spendable*)fast_alloc(sizeof(Spendable));
    if (capacity < 1) {
        capacity = 1;
    }
    sp->components = (void**)fast_calloc(capacity, sizeof(void*));
    sp->consumed = (uint8_t*)fast_calloc(capacity, sizeof(uint8_t));
    sp->count = 0;
    sp->capacity = capacity;
    sp->remaining = 0;
    sp->is_locked = false;
    return sp;
}

void spendable_destroy(Spendable* sp) {
    if (sp == NULL) {
        return;
    }
    if (sp->components) {
        fast_free(sp->components);
    }
    if (sp->consumed) {
        fast_free(sp->consumed);
    }
    fast_free(sp);
}

void spendable_add(Spendable* sp, void* component) {
    if (sp == NULL) {
        return;
    }
    if (sp->count >= sp->capacity) {
        spend_panic("capacity exceeded: too many components");
    }
    sp->components[sp->count++] = component;
    sp->remaining++;
}

// 顺序消费（legacy API）：消费下一个未消费元素
void* spendable_call(Spendable* sp) {
    if (sp == NULL) {
        return NULL;
    }
    if (sp->is_locked) {
        spend_panic("sequential call() is not allowed inside a locked spend block");
    }
    for (size_t i = 0; i < sp->count; i++) {
        if (!sp->consumed[i]) {
            sp->consumed[i] = 1;
            sp->remaining--;
            return sp->components[i];
        }
    }
    return NULL;
}

// spend_lock - 锁定目标并开始消费流程
void spend_lock(void* target) {
    Spendable* sp = (Spendable*)target;
    if (sp == NULL) {
        spend_panic("spend_lock: null target");
    }
    if (sp->is_locked) {
        spend_panic("spend_lock: already locked (nested spend is not allowed)");
    }
    if (sp->remaining != sp->count) {
        spend_panic("spend_lock: components were consumed before locking");
    }
    sp->is_locked = true;
}

// spend_call - 消费指定索引的元素（1-based），校验锁定/越界/重复消费
void* spend_call(void* target, int index) {
    Spendable* sp = (Spendable*)target;
    if (sp == NULL) {
        spend_panic("spend_call: null target");
    }
    if (!sp->is_locked) {
        spend_panic("spend_call: target is not locked (missing spend_lock)");
    }
    if (index < 1 || (size_t)index > sp->count) {
        spend_panic("spend_call: index out of range");
    }
    size_t i = (size_t)(index - 1);
    if (sp->consumed[i]) {
        spend_panic("spend_call: element already consumed (duplicate call)");
    }
    sp->consumed[i] = 1;
    sp->remaining--;
    return sp->components[i];
}

// spend_consume_all - 消费全部剩余元素（call(default) 兜底）
void spend_consume_all(void* target) {
    Spendable* sp = (Spendable*)target;
    if (sp == NULL) {
        spend_panic("spend_consume_all: null target");
    }
    if (!sp->is_locked) {
        spend_panic("spend_consume_all: target is not locked (missing spend_lock)");
    }
    for (size_t i = 0; i < sp->count; i++) {
        if (!sp->consumed[i]) {
            sp->consumed[i] = 1;
        }
    }
    sp->remaining = 0;
}

// spend_unlock - 解除锁定；校验所有元素均已消费
void spend_unlock(void* target) {
    Spendable* sp = (Spendable*)target;
    if (sp == NULL) {
        spend_panic("spend_unlock: null target");
    }
    if (!sp->is_locked) {
        spend_panic("spend_unlock: target is not locked");
    }
    if (sp->remaining != 0) {
        // 编译期已证明全消费；若到达此处说明编译器有缺陷或目标在消费途中被提前退出
        fprintf(stderr, "[Spendable] spend_unlock: %zu element(s) not consumed (resource leak)\n", sp->remaining);
        abort();
    }
    sp->is_locked = false;
}
