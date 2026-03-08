#include "kaula.h"

static inline Spendable* spendable_create(int capacity) {
    Spendable* sp = (Spendable*)fast_alloc(sizeof(Spendable));
    sp->count = 0;
    sp->call_counter = 0;
    sp->components = (void**)fast_calloc(capacity, sizeof(void*));
    return sp;
}

static inline void spendable_add(Spendable* sp, void* component) {
    sp->components[sp->count++] = component;
    sp->call_counter++;
}

static inline void* spendable_call(Spendable* sp) {
    sp->call_counter--;
    void* component = sp->components[sp->call_counter];
    
    if (sp->call_counter == 0) {
        sp->count = 0;
    }
    
    return component;
}