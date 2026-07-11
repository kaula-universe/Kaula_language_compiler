#ifndef STD_DEQUE_DEQUE_H
#define STD_DEQUE_DEQUE_H

#include "../base/types.h"

typedef struct Deque Deque;

Deque* deque_create(size_t element_size);
void deque_destroy(Deque* dq);

size_t deque_size(const Deque* dq);
bool_t deque_is_empty(const Deque* dq);

void deque_push_front(Deque* dq, const void* element);
void deque_push_back(Deque* dq, const void* element);

bool_t deque_pop_front(Deque* dq, void* element);
bool_t deque_pop_back(Deque* dq, void* element);

bool_t deque_peek_front(const Deque* dq, void* element);
bool_t deque_peek_back(const Deque* dq, void* element);

void* deque_at(const Deque* dq, size_t index);

void deque_clear(Deque* dq);
void deque_reserve(Deque* dq, size_t capacity);

size_t deque_capacity(const Deque* dq);

void deque_insert(Deque* dq, size_t index, const void* element);
void deque_remove(Deque* dq, size_t index);

void deque_swap(Deque* a, Deque* b);

#endif
