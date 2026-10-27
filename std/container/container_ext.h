#pragma once
#include "../base/types.h"
#include "../memory/memory.h"
#include "../string/string.h"
#include "../deque/deque.h"

typedef size_t (*HashFunc)(const void* key);
typedef int (*HashEqualFunc)(const void* a, const void* b);
typedef int (*CompareFunc)(const void* a, const void* b);

typedef struct HashSet HashSet;
typedef struct PriorityQueue PriorityQueue;
// Deque 由 std/deque/deque.h 提供（那里才是正式 API）。此前本文件另声明了一整套
// deque_*，其中 push_front/push_back 的返回类型还与 deque.h 不一致（bool_t vs
// void），同时包含这两个头就会 conflicting types，且两个 .c 各有一份强符号。
// 现在直接复用 deque.h 的声明。

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

PriorityQueue* pq_create(size_t elem_size, CompareFunc cmp);
void pq_destroy(PriorityQueue* pq);
bool_t pq_push(PriorityQueue* pq, const void* elem);
bool_t pq_pop(PriorityQueue* pq, void* out);
void* pq_top(const PriorityQueue* pq);
size_t pq_size(const PriorityQueue* pq);
bool_t pq_is_empty(const PriorityQueue* pq);
void pq_clear(PriorityQueue* pq);
