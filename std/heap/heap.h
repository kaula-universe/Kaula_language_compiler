#pragma once
#include "../base/types.h"
#include "../memory/memory.h"

typedef int (*HeapCompareFunc)(const void* a, const void* b);

typedef struct Heap {
    void** items;
    size_t count;
    size_t capacity;
    HeapCompareFunc compare;
} Heap;

Heap* heap_create(HeapCompareFunc compare);
void heap_destroy(Heap* heap);

void heap_push(Heap* heap, void* item);
void* heap_pop(Heap* heap);
void* heap_peek(const Heap* heap);

void heap_build(void** items, size_t count, size_t elem_size, HeapCompareFunc compare);
void heap_sort(void** items, size_t count, size_t elem_size, HeapCompareFunc compare);

void heap_merge(Heap* dst, const Heap* src);
Heap* heap_merge_k(Heap** heaps, size_t count);

void heap_decrease_key(Heap* heap, size_t index, void* new_value);
void heap_increase_key(Heap* heap, size_t index, void* new_value);

size_t heap_size(const Heap* heap);
bool_t heap_is_empty(const Heap* heap);

int heap_compare_int(const void* a, const void* b);
int heap_compare_int_desc(const void* a, const void* b);
int heap_compare_float(const void* a, const void* b);