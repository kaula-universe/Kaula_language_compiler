#pragma once
#include "../base/types.h"
#include "../memory/memory.h"
#include "../string/string.h"

typedef size_t (*HashFunc)(const void* key);
typedef int (*HashEqualFunc)(const void* a, const void* b);
typedef int (*CompareFunc)(const void* a, const void* b);

typedef struct HashSet HashSet;
typedef struct Deque Deque;
typedef struct PriorityQueue PriorityQueue;

HashSet* hashset_create(size_t elem_size, HashFunc hash, HashEqualFunc equal);
void hashset_destroy(HashSet* set);
bool_t hashset_insert(HashSet* set, const void* elem);
bool_t hashset_remove(HashSet* set, const void* elem);
bool_t hashset_contains(const HashSet* set, const void* elem);
size_t hashset_size(const HashSet* set);
bool_t hashset_is_empty(const HashSet* set);
void hashset_clear(HashSet* set);
void** hashset_to_array(const HashSet* set, size_t* out_count);

HashSet* hashset_create_string(void);
bool_t hashset_insert_string(HashSet* set, const char* str);
bool_t hashset_contains_string(const HashSet* set, const char* str);
bool_t hashset_remove_string(HashSet* set, const char* str);

Deque* deque_create(size_t elem_size);
void deque_destroy(Deque* deque);
bool_t deque_push_front(Deque* deque, const void* elem);
bool_t deque_push_back(Deque* deque, const void* elem);
bool_t deque_pop_front(Deque* deque, void* out);
bool_t deque_pop_back(Deque* deque, void* out);
void* deque_front(const Deque* deque);
void* deque_back(const Deque* deque);
void* deque_at(const Deque* deque, size_t index);
size_t deque_size(const Deque* deque);
bool_t deque_is_empty(const Deque* deque);
void deque_clear(Deque* deque);

PriorityQueue* pq_create(size_t elem_size, CompareFunc cmp);
void pq_destroy(PriorityQueue* pq);
bool_t pq_push(PriorityQueue* pq, const void* elem);
bool_t pq_pop(PriorityQueue* pq, void* out);
void* pq_top(const PriorityQueue* pq);
size_t pq_size(const PriorityQueue* pq);
bool_t pq_is_empty(const PriorityQueue* pq);
void pq_clear(PriorityQueue* pq);
