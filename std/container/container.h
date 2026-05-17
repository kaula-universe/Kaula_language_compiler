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
extern void* vector_pop_back(Vector* vector);
extern void* vector_get(Vector* vector, size_t index);
extern void vector_set(Vector* vector, size_t index, void* element);
extern void vector_remove(Vector* vector, size_t index);
extern size_t vector_size(Vector* vector);
extern bool vector_is_empty(Vector* vector);
extern void vector_clear(Vector* vector);
extern void vector_reserve(Vector* vector, size_t capacity);

// 整数版本（用于存储索引等整数值）
extern void vector_push_back_int(Vector* vector, int64_t element);
extern int64_t vector_pop_back_int(Vector* vector);
extern int64_t vector_get_int(Vector* vector, size_t index);
extern void vector_set_int(Vector* vector, size_t index, int64_t element);

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

extern int equal_string(void* key1, void* key2);
extern int equal_int(void* key1, void* key2);
extern int equal_float(void* key1, void* key2);

// 集合（Set）
typedef struct SetNode {
    void* data;
    struct SetNode* next;
} SetNode;

typedef struct Set {
    SetNode** buckets;
    size_t size;
    size_t capacity;
    size_t (*hash_func)(void* key);
    int (*equal_func)(void* key1, void* key2);
} Set;

extern Set* set_create(size_t initial_capacity, size_t (*hash_func)(void* key), int (*equal_func)(void* key1, void* key2));
extern void set_destroy(Set* set);
extern void set_add(Set* set, void* element);
extern void set_remove(Set* set, void* element);
extern bool set_contains(Set* set, void* element);
extern size_t set_size(Set* set);
extern bool set_is_empty(Set* set);
extern void set_clear(Set* set);
extern Set* set_union(Set* set1, Set* set2);
extern Set* set_intersection(Set* set1, Set* set2);
extern Set* set_difference(Set* set1, Set* set2);

// 红黑树节点（TreeMap 使用）
typedef enum { TREE_NODE_RED, TREE_NODE_BLACK } TreeNodeColor;

typedef struct TreeMapNode {
    void* key;
    void* value;
    struct TreeMapNode* left;
    struct TreeMapNode* right;
    struct TreeMapNode* parent;
    TreeNodeColor color;
} TreeMapNode;

typedef struct TreeMap {
    TreeMapNode* root;
    size_t size;
    int (*compare_func)(void* key1, void* key2);
} TreeMap;

extern TreeMap* tree_map_create(int (*compare_func)(void* key1, void* key2));
extern void tree_map_destroy(TreeMap* map);
extern void tree_map_put(TreeMap* map, void* key, void* value);
extern void* tree_map_get(TreeMap* map, void* key);
extern void tree_map_remove(TreeMap* map, void* key);
extern bool tree_map_contains(TreeMap* map, void* key);
extern size_t tree_map_size(TreeMap* map);
extern bool tree_map_is_empty(TreeMap* map);
extern void tree_map_clear(TreeMap* map);
extern void* tree_map_first_key(TreeMap* map);
extern void* tree_map_last_key(TreeMap* map);
extern void* tree_map_lower_bound(TreeMap* map, void* key);
extern void* tree_map_upper_bound(TreeMap* map, void* key);

// 优先级队列（PriorityQueue）
typedef struct PriorityQueueNode {
    void* data;
    int priority;
} PriorityQueueNode;

typedef struct PriorityQueue {
    PriorityQueueNode* data;
    size_t size;
    size_t capacity;
} PriorityQueue;

extern PriorityQueue* priority_queue_create(size_t initial_capacity);
extern void priority_queue_destroy(PriorityQueue* pq);
extern void priority_queue_push(PriorityQueue* pq, void* element, int priority);
extern void* priority_queue_pop(PriorityQueue* pq);
extern void* priority_queue_peek(PriorityQueue* pq);
extern size_t priority_queue_size(PriorityQueue* pq);
extern bool priority_queue_is_empty(PriorityQueue* pq);
extern void priority_queue_clear(PriorityQueue* pq);
extern void priority_queue_change_priority(PriorityQueue* pq, void* element, int new_priority);

#endif // STD_CONTAINER_CONTAINER_H