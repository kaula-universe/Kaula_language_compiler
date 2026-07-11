#include "deque.h"
#include "../memory/memory.h"
#include <stdlib.h>
#include <string.h>

#define DEQUE_BLOCK_SIZE 4096

struct Deque {
    u8* data;
    size_t element_size;
    size_t capacity;
    size_t head;
    size_t tail;
    size_t count;
};

static size_t deque_block_count(size_t element_size, size_t count) {
    return (count * element_size + DEQUE_BLOCK_SIZE - 1) / DEQUE_BLOCK_SIZE;
}

Deque* deque_create(size_t element_size) {
    Deque* dq = (Deque*)kmm_v4_malloc(sizeof(Deque));
    if (!dq) return NULL;
    dq->element_size = element_size;
    dq->capacity = 8;
    dq->data = (u8*)kmm_v4_malloc(dq->capacity * element_size);
    if (!dq->data) {
        kmm_v4_free(dq);
        return NULL;
    }
    dq->head = 0;
    dq->tail = 0;
    dq->count = 0;
    return dq;
}

void deque_destroy(Deque* dq) {
    if (!dq) return;
    kmm_v4_free(dq->data);
    kmm_v4_free(dq);
}

size_t deque_size(const Deque* dq) {
    return dq ? dq->count : 0;
}

bool_t deque_is_empty(const Deque* dq) {
    return deque_size(dq) == 0;
}

static void deque_resize(Deque* dq, size_t new_capacity) {
    if (!dq) return;
    u8* new_data = (u8*)kmm_v4_malloc(new_capacity * dq->element_size);
    if (!new_data) return;
    
    for (size_t i = 0; i < dq->count; i++) {
        size_t idx = (dq->head + i) % dq->capacity;
        memcpy(new_data + i * dq->element_size, dq->data + idx * dq->element_size, dq->element_size);
    }
    
    kmm_v4_free(dq->data);
    dq->data = new_data;
    dq->capacity = new_capacity;
    dq->head = 0;
    dq->tail = dq->count;
}

void deque_push_front(Deque* dq, const void* element) {
    if (!dq || !element) return;
    if (dq->count >= dq->capacity) {
        deque_resize(dq, dq->capacity * 2);
    }
    dq->head = (dq->head == 0) ? dq->capacity - 1 : dq->head - 1;
    memcpy(dq->data + dq->head * dq->element_size, element, dq->element_size);
    dq->count++;
}

void deque_push_back(Deque* dq, const void* element) {
    if (!dq || !element) return;
    if (dq->count >= dq->capacity) {
        deque_resize(dq, dq->capacity * 2);
    }
    memcpy(dq->data + dq->tail * dq->element_size, element, dq->element_size);
    dq->tail = (dq->tail + 1) % dq->capacity;
    dq->count++;
}

bool_t deque_pop_front(Deque* dq, void* element) {
    if (!dq || !element || dq->count == 0) return false;
    memcpy(element, dq->data + dq->head * dq->element_size, dq->element_size);
    dq->head = (dq->head + 1) % dq->capacity;
    dq->count--;
    return true;
}

bool_t deque_pop_back(Deque* dq, void* element) {
    if (!dq || !element || dq->count == 0) return false;
    dq->tail = (dq->tail == 0) ? dq->capacity - 1 : dq->tail - 1;
    memcpy(element, dq->data + dq->tail * dq->element_size, dq->element_size);
    dq->count--;
    return true;
}

bool_t deque_peek_front(const Deque* dq, void* element) {
    if (!dq || !element || dq->count == 0) return false;
    memcpy(element, dq->data + dq->head * dq->element_size, dq->element_size);
    return true;
}

bool_t deque_peek_back(const Deque* dq, void* element) {
    if (!dq || !element || dq->count == 0) return false;
    size_t idx = (dq->tail == 0) ? dq->capacity - 1 : dq->tail - 1;
    memcpy(element, dq->data + idx * dq->element_size, dq->element_size);
    return true;
}

void* deque_at(const Deque* dq, size_t index) {
    if (!dq || index >= dq->count) return NULL;
    size_t idx = (dq->head + index) % dq->capacity;
    return dq->data + idx * dq->element_size;
}

void deque_clear(Deque* dq) {
    if (!dq) return;
    dq->head = 0;
    dq->tail = 0;
    dq->count = 0;
}

void deque_reserve(Deque* dq, size_t capacity) {
    if (!dq || capacity <= dq->capacity) return;
    deque_resize(dq, capacity);
}

size_t deque_capacity(const Deque* dq) {
    return dq ? dq->capacity : 0;
}

void deque_insert(Deque* dq, size_t index, const void* element) {
    if (!dq || !element || index > dq->count) return;
    if (dq->count >= dq->capacity) {
        deque_resize(dq, dq->capacity * 2);
    }
    
    if (index <= dq->count / 2) {
        dq->head = (dq->head == 0) ? dq->capacity - 1 : dq->head - 1;
        for (size_t i = 0; i < index; i++) {
            size_t src = (dq->head + i + 1) % dq->capacity;
            size_t dst = (dq->head + i) % dq->capacity;
            memcpy(dq->data + dst * dq->element_size, dq->data + src * dq->element_size, dq->element_size);
        }
        memcpy(dq->data + ((dq->head + index) % dq->capacity) * dq->element_size, element, dq->element_size);
    } else {
        for (size_t i = dq->count; i > index; i--) {
            size_t src = (dq->head + i - 1) % dq->capacity;
            size_t dst = (dq->head + i) % dq->capacity;
            memcpy(dq->data + dst * dq->element_size, dq->data + src * dq->element_size, dq->element_size);
        }
        memcpy(dq->data + ((dq->head + index) % dq->capacity) * dq->element_size, element, dq->element_size);
        dq->tail = (dq->tail + 1) % dq->capacity;
    }
    dq->count++;
}

void deque_remove(Deque* dq, size_t index) {
    if (!dq || index >= dq->count) return;
    
    if (index < dq->count / 2) {
        for (size_t i = index; i > 0; i--) {
            size_t src = (dq->head + i - 1) % dq->capacity;
            size_t dst = (dq->head + i) % dq->capacity;
            memcpy(dq->data + dst * dq->element_size, dq->data + src * dq->element_size, dq->element_size);
        }
        dq->head = (dq->head + 1) % dq->capacity;
    } else {
        for (size_t i = index; i < dq->count - 1; i++) {
            size_t src = (dq->head + i + 1) % dq->capacity;
            size_t dst = (dq->head + i) % dq->capacity;
            memcpy(dq->data + dst * dq->element_size, dq->data + src * dq->element_size, dq->element_size);
        }
        dq->tail = (dq->tail == 0) ? dq->capacity - 1 : dq->tail - 1;
    }
    dq->count--;
}

void deque_swap(Deque* a, Deque* b) {
    if (!a || !b) return;
    Deque temp = *a;
    *a = *b;
    *b = temp;
}
