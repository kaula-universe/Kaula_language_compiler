#include "heap.h"
#include <stdlib.h>
#include <string.h>

Heap* heap_create(HeapCompareFunc compare) {
    Heap* h = (Heap*)kmm_v4_malloc(sizeof(Heap));
    if (!h) return NULL;
    h->items = NULL;
    h->count = 0;
    h->capacity = 0;
    h->compare = compare;
    return h;
}

void heap_destroy(Heap* heap) {
    if (!heap) return;
    kmm_v4_free(heap->items);
    kmm_v4_free(heap);
}

static void heap_sift_up(Heap* heap, size_t index) {
    while (index > 0) {
        size_t parent = (index - 1) / 2;
        if (heap->compare(heap->items[index], heap->items[parent]) >= 0) break;
        void* temp = heap->items[index];
        heap->items[index] = heap->items[parent];
        heap->items[parent] = temp;
        index = parent;
    }
}

static void heap_sift_down(Heap* heap, size_t index) {
    while (true) {
        size_t left = 2 * index + 1;
        size_t right = 2 * index + 2;
        size_t smallest = index;
        
        if (left < heap->count && heap->compare(heap->items[left], heap->items[smallest]) < 0) {
            smallest = left;
        }
        if (right < heap->count && heap->compare(heap->items[right], heap->items[smallest]) < 0) {
            smallest = right;
        }
        if (smallest == index) break;
        
        void* temp = heap->items[index];
        heap->items[index] = heap->items[smallest];
        heap->items[smallest] = temp;
        index = smallest;
    }
}

void heap_push(Heap* heap, void* item) {
    if (!heap || !item) return;
    
    if (heap->count >= heap->capacity) {
        heap->capacity = heap->capacity == 0 ? 16 : heap->capacity * 2;
        heap->items = (void**)kmm_v4_realloc(heap->items, heap->capacity * sizeof(void*));
        if (!heap->items) return;
    }
    
    heap->items[heap->count++] = item;
    heap_sift_up(heap, heap->count - 1);
}

void* heap_pop(Heap* heap) {
    if (!heap || heap->count == 0) return NULL;
    
    void* item = heap->items[0];
    heap->items[0] = heap->items[--heap->count];
    heap_sift_down(heap, 0);
    return item;
}

void* heap_peek(const Heap* heap) {
    if (!heap || heap->count == 0) return NULL;
    return heap->items[0];
}

void heap_build(void** items, size_t count, size_t elem_size, HeapCompareFunc compare) {
    (void)elem_size;
    for (size_t i = count / 2; i > 0; i--) {
        size_t idx = i - 1;
        while (true) {
            size_t left = 2 * idx + 1;
            size_t right = 2 * idx + 2;
            size_t smallest = idx;
            
            if (left < count && compare(items[left], items[smallest]) < 0) {
                smallest = left;
            }
            if (right < count && compare(items[right], items[smallest]) < 0) {
                smallest = right;
            }
            if (smallest == idx) break;
            
            void* temp = items[idx];
            items[idx] = items[smallest];
            items[smallest] = temp;
            idx = smallest;
        }
    }
}

void heap_sort(void** items, size_t count, size_t elem_size, HeapCompareFunc compare) {
    (void)elem_size;
    heap_build(items, count, elem_size, compare);
    
    for (size_t i = count; i > 1; i--) {
        void* temp = items[0];
        items[0] = items[i - 1];
        items[i - 1] = temp;
        
        size_t idx = 0;
        size_t n = i - 1;
        while (true) {
            size_t left = 2 * idx + 1;
            size_t right = 2 * idx + 2;
            size_t smallest = idx;
            
            if (left < n && compare(items[left], items[smallest]) < 0) {
                smallest = left;
            }
            if (right < n && compare(items[right], items[smallest]) < 0) {
                smallest = right;
            }
            if (smallest == idx) break;
            
            void* temp = items[idx];
            items[idx] = items[smallest];
            items[smallest] = temp;
            idx = smallest;
        }
    }
}

void heap_merge(Heap* dst, const Heap* src) {
    if (!dst || !src) return;
    
    for (size_t i = 0; i < src->count; i++) {
        heap_push(dst, src->items[i]);
    }
}

Heap* heap_merge_k(Heap** heaps, size_t count) {
    if (!heaps || count == 0) return NULL;
    if (count == 1) return heaps[0];
    
    Heap* result = heap_create(heaps[0]->compare);
    for (size_t i = 0; i < count; i++) {
        heap_merge(result, heaps[i]);
    }
    return result;
}

void heap_decrease_key(Heap* heap, size_t index, void* new_value) {
    if (!heap || index >= heap->count) return;
    heap->items[index] = new_value;
    heap_sift_up(heap, index);
}

void heap_increase_key(Heap* heap, size_t index, void* new_value) {
    if (!heap || index >= heap->count) return;
    heap->items[index] = new_value;
    heap_sift_down(heap, index);
}

size_t heap_size(const Heap* heap) {
    return heap ? heap->count : 0;
}

bool_t heap_is_empty(const Heap* heap) {
    return heap && heap->count == 0;
}

int heap_compare_int(const void* a, const void* b) {
    return *(int*)a - *(int*)b;
}

int heap_compare_int_desc(const void* a, const void* b) {
    return *(int*)b - *(int*)a;
}

int heap_compare_float(const void* a, const void* b) {
    f64 diff = *(f64*)a - *(f64*)b;
    if (diff < 0) return -1;
    if (diff > 0) return 1;
    return 0;
}