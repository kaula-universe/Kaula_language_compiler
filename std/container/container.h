#ifndef STD_CONTAINER_CONTAINER_H
#define STD_CONTAINER_CONTAINER_H

#include "../base/types.h"

// 动态数组（Vector）
typedef struct Vector {
    void** data;
    size_t size;
    size_t capacity;
} Vector;

extern Vector* vector_create(size_t initial_capacity);
extern void vector_destroy(Vector* vector);
extern void vector_push_back(Vector* vector, void* element);
extern void* vector_get(Vector* vector, size_t index);
extern void vector_set(Vector* vector, size_t index, void* element);
extern void vector_remove(Vector* vector, size_t index);
extern size_t vector_size(Vector* vector);
extern bool vector_is_empty(Vector* vector);
extern void vector_clear(Vector* vector);
extern void vector_reserve(Vector* vector, size_t capacity);

// 链表（LinkedList）
typedef struct ListNode {
    void* data;
    struct ListNode* next;
    struct ListNode* prev;
} ListNode;

typedef struct LinkedList {
    ListNode* head;
    ListNode* tail;
    size_t size;
} LinkedList;

extern LinkedList* linked_list_create();
extern void linked_list_destroy(LinkedList* list);
extern void linked_list_push_front(LinkedList* list, void* element);
extern void linked_list_push_back(LinkedList* list, void* element);
extern void* linked_list_pop_front(LinkedList* list);
extern void* linked_list_pop_back(LinkedList* list);
extern void* linked_list_get(LinkedList* list, size_t index);
extern void linked_list_remove(LinkedList* list, size_t index);
extern size_t linked_list_size(LinkedList* list);
extern bool linked_list_is_empty(LinkedList* list);
extern void linked_list_clear(LinkedList* list);

// 哈希表（HashMap）
typedef struct HashNode {
    void* key;
    void* value;
    struct HashNode* next;
} HashNode;

typedef struct HashMap {
    HashNode** buckets;
    size_t size;
    size_t capacity;
    size_t (*hash_func)(void* key);
    int (*equal_func)(void* key1, void* key2);
} HashMap;

extern HashMap* hash_map_create(size_t initial_capacity, size_t (*hash_func)(void* key), int (*equal_func)(void* key1, void* key2));
extern void hash_map_destroy(HashMap* map);
extern void hash_map_put(HashMap* map, void* key, void* value);
extern void* hash_map_get(HashMap* map, void* key);
extern void hash_map_remove(HashMap* map, void* key);
extern size_t hash_map_size(HashMap* map);
extern bool hash_map_is_empty(HashMap* map);
extern void hash_map_clear(HashMap* map);
extern bool hash_map_contains(HashMap* map, void* key);

// 栈（Stack）
typedef struct Stack {
    void** data;
    size_t size;
    size_t capacity;
} Stack;

extern Stack* stack_create(size_t initial_capacity);
extern void stack_destroy(Stack* stack);
extern void stack_push(Stack* stack, void* element);
extern void* stack_pop(Stack* stack);
extern void* stack_peek(Stack* stack);
extern size_t stack_size(Stack* stack);
extern bool stack_is_empty(Stack* stack);
extern void stack_clear(Stack* stack);

// 队列（Queue）
typedef struct Queue {
    void** data;
    size_t head;
    size_t tail;
    size_t size;
    size_t capacity;
} Queue;

extern Queue* queue_create(size_t initial_capacity);
extern void queue_destroy(Queue* queue);
extern void queue_enqueue(Queue* queue, void* element);
extern void* queue_dequeue(Queue* queue);
extern void* queue_front(Queue* queue);
extern size_t queue_size(Queue* queue);
extern bool queue_is_empty(Queue* queue);
extern void queue_clear(Queue* queue);

// 通用哈希函数
extern size_t hash_string(void* key);
extern size_t hash_int(void* key);
extern size_t hash_float(void* key);

// 通用比较函数
extern int equal_string(void* key1, void* key2);
extern int equal_int(void* key1, void* key2);
extern int equal_float(void* key1, void* key2);

#endif // STD_CONTAINER_CONTAINER_H