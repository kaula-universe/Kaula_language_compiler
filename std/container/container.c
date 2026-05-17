#include "container.h"
#include "../memory/memory.h"
#include <stdlib.h>
#include <string.h>

// 动态数组（Vector）实现
Vector* vector_create(size_t initial_capacity) {
    size_t cap = initial_capacity > 0 ? initial_capacity : 4;
    size_t data_size = cap * sizeof(void*);
    
    // 一次性分配Vector结构体和data数组，确保清零
    size_t total_size = sizeof(Vector) + data_size;
    void* mem = kmm_v4_malloc(total_size);
    if (!mem) return NULL;
    
    // 清零整个分配区域
    memset(mem, 0, total_size);
    
    Vector* vector = (Vector*)mem;
    vector->capacity = cap;
    vector->size = 0;
    // data指向Vector结构体之后的内存
    vector->data = (void**)((char*)mem + sizeof(Vector));
    
    return vector;
}

void vector_destroy(Vector* vector) {
    // KMM 管理内存，无需手动释放
}

void vector_reserve(Vector* vector, size_t capacity) {
    if (!vector || capacity <= vector->capacity) return;
    
    // 尝试分配新空间
    void** new_data = (void**)kmm_v4_malloc(capacity * sizeof(void*));
    if (new_data) {
        // 复制旧数据
        memcpy(new_data, vector->data, vector->size * sizeof(void*));
        vector->data = new_data;
        vector->capacity = capacity;
    }
    // 如果分配失败，保持原状（不崩溃）
}

void vector_push_back(Vector* vector, void* element) {
    if (!vector) return;
    
    if (vector->size >= vector->capacity) {
        // 自动扩容，每次翻倍
        size_t new_capacity = vector->capacity * 2;
        vector_reserve(vector, new_capacity);
        // 如果扩容失败，vector_reserve不会修改capacity，这里不继续push
    }
    
    if (vector->size < vector->capacity) {
        vector->data[vector->size++] = element;
    }
}

void* vector_get(Vector* vector, size_t index) {
    if (vector && index < vector->size) {
        return vector->data[index];
    }
    return NULL;
}

void* vector_pop_back(Vector* vector) {
    if (vector && vector->size > 0) {
        return vector->data[--vector->size];
    }
    return NULL;
}

void vector_set(Vector* vector, size_t index, void* element) {
    if (vector && index < vector->size) {
        vector->data[index] = element;
    }
}

void vector_remove(Vector* vector, size_t index) {
    if (vector && index < vector->size) {
        memmove(&vector->data[index], &vector->data[index + 1], (vector->size - index - 1) * sizeof(void*));
        vector->size--;
    }
}

size_t vector_size(Vector* vector) {
    if (vector) {
        return vector->size;
    }
    return 0;
}

bool vector_is_empty(Vector* vector) {
    return vector_size(vector) == 0;
}

void vector_clear(Vector* vector) {
    if (vector) {
        vector->size = 0;
    }
}

// 整数版本实现
void vector_push_back_int(Vector* vector, int64_t element) {
    if (!vector) return;
    
    if (vector->size >= vector->capacity) {
        size_t new_capacity = vector->capacity * 2;
        vector_reserve(vector, new_capacity);
    }
    
    if (vector->size < vector->capacity) {
        // 将int64_t存储为指针（在64位平台上安全）
        vector->data[vector->size++] = (void*)(intptr_t)element;
    }
}

int64_t vector_pop_back_int(Vector* vector) {
    if (vector && vector->size > 0) {
        return (int64_t)(intptr_t)vector->data[--vector->size];
    }
    return 0;
}

int64_t vector_get_int(Vector* vector, size_t index) {
    if (vector && index < vector->size) {
        return (int64_t)(intptr_t)vector->data[index];
    }
    return 0;
}

void vector_set_int(Vector* vector, size_t index, int64_t element) {
    if (vector && index < vector->size) {
        vector->data[index] = (void*)(intptr_t)element;
    }
}

// 链表（LinkedList）实现
LinkedList* linked_list_create() {
    LinkedList* list = (LinkedList*)kmm_v4_malloc(sizeof(LinkedList));
    if (list) {
        list->head = NULL;
        list->tail = NULL;
        list->size = 0;
    }
    return list;
}

void linked_list_destroy(LinkedList* list) {
    // KMM 管理内存，无需手动释放
}

void linked_list_push_front(LinkedList* list, void* element) {
    if (list) {
        ListNode* node = (ListNode*)kmm_v4_malloc(sizeof(ListNode));
        if (node) {
            node->data = element;
            node->next = list->head;
            node->prev = NULL;
            if (list->head) {
                list->head->prev = node;
            } else {
                list->tail = node;
            }
            list->head = node;
            list->size++;
        }
    }
}

void linked_list_push_back(LinkedList* list, void* element) {
    if (list) {
        ListNode* node = (ListNode*)kmm_v4_malloc(sizeof(ListNode));
        if (node) {
            node->data = element;
            node->next = NULL;
            node->prev = list->tail;
            if (list->tail) {
                list->tail->next = node;
            } else {
                list->head = node;
            }
            list->tail = node;
            list->size++;
        }
    }
}

void* linked_list_pop_front(LinkedList* list) {
    if (list && list->head) {
        ListNode* node = list->head;
        void* data = node->data;
        list->head = node->next;
        if (list->head) {
            list->head->prev = NULL;
        } else {
            list->tail = NULL;
        }
        // KMM 管理内存，无需手动释放
        list->size--;
        return data;
    }
    return NULL;
}

void* linked_list_pop_back(LinkedList* list) {
    if (list && list->tail) {
        ListNode* node = list->tail;
        void* data = node->data;
        list->tail = node->prev;
        if (list->tail) {
            list->tail->next = NULL;
        } else {
            list->head = NULL;
        }
        // KMM 管理内存，无需手动释放
        list->size--;
        return data;
    }
    return NULL;
}

void* linked_list_get(LinkedList* list, size_t index) {
    if (list && index < list->size) {
        ListNode* current;
        if (index < list->size / 2) {
            current = list->head;
            for (size_t i = 0; i < index; i++) {
                current = current->next;
            }
        } else {
            current = list->tail;
            for (size_t i = list->size - 1; i > index; i--) {
                current = current->prev;
            }
        }
        return current->data;
    }
    return NULL;
}

void linked_list_remove(LinkedList* list, size_t index) {
    if (list && index < list->size) {
        ListNode* current;
        if (index < list->size / 2) {
            current = list->head;
            for (size_t i = 0; i < index; i++) {
                current = current->next;
            }
        } else {
            current = list->tail;
            for (size_t i = list->size - 1; i > index; i--) {
                current = current->prev;
            }
        }
        if (current->prev) {
            current->prev->next = current->next;
        } else {
            list->head = current->next;
        }
        if (current->next) {
            current->next->prev = current->prev;
        } else {
            list->tail = current->prev;
        }
        // KMM 管理内存，无需手动释放
        list->size--;
    }
}

size_t linked_list_size(LinkedList* list) {
    if (list) {
        return list->size;
    }
    return 0;
}

bool linked_list_is_empty(LinkedList* list) {
    return linked_list_size(list) == 0;
}

void linked_list_clear(LinkedList* list) {
    if (list) {
        // KMM 管理内存，无需手动释放
        list->head = NULL;
        list->tail = NULL;
        list->size = 0;
    }
}

// 哈希表（HashMap）实现
HashMap* hash_map_create(size_t initial_capacity, size_t (*hash_func)(void* key), int (*equal_func)(void* key1, void* key2)) {
    HashMap* map = (HashMap*)kmm_v4_malloc(sizeof(HashMap));
    if (map) {
        map->capacity = initial_capacity > 0 ? initial_capacity : 16;
        map->size = 0;
        map->hash_func = hash_func;
        map->equal_func = equal_func;
        map->buckets = (HashNode**)kmm_v4_calloc(map->capacity, sizeof(HashNode*));
    }
    return map;
}

void hash_map_destroy(HashMap* map) {
    // KMM 管理内存，无需手动释放
}

static void hash_map_resize(HashMap* map, size_t new_capacity) {
    HashNode** old_buckets = map->buckets;
    size_t old_capacity = map->capacity;
    
    map->buckets = (HashNode**)kmm_v4_calloc(new_capacity, sizeof(HashNode*));
    if (!map->buckets) {
        map->buckets = old_buckets;
        return;
    }
    map->capacity = new_capacity;
    
    for (size_t i = 0; i < old_capacity; i++) {
        HashNode* current = old_buckets[i];
        while (current) {
            HashNode* next = current->next;
            size_t new_hash = map->hash_func(current->key) % new_capacity;
            current->next = map->buckets[new_hash];
            map->buckets[new_hash] = current;
            current = next;
        }
    }
    
    // KMM 管理内存，无需手动释放
}

void hash_map_put(HashMap* map, void* key, void* value) {
    if (map) {
        if (map->size >= map->capacity * 3 / 4) {
            hash_map_resize(map, map->capacity * 2);
        }
        
        size_t hash = map->hash_func(key) % map->capacity;
        HashNode* current = map->buckets[hash];
        while (current) {
            if (map->equal_func(current->key, key)) {
                current->value = value;
                return;
            }
            current = current->next;
        }
        HashNode* node = (HashNode*)kmm_v4_malloc(sizeof(HashNode));
        if (node) {
            node->key = key;
            node->value = value;
            node->next = map->buckets[hash];
            map->buckets[hash] = node;
            map->size++;
        }
    }
}

void* hash_map_get(HashMap* map, void* key) {
    if (map) {
        size_t hash = map->hash_func(key) % map->capacity;
        HashNode* current = map->buckets[hash];
        while (current) {
            if (map->equal_func(current->key, key)) {
                return current->value;
            }
            current = current->next;
        }
    }
    return NULL;
}

void hash_map_remove(HashMap* map, void* key) {
    if (map) {
        size_t hash = map->hash_func(key) % map->capacity;
        HashNode* current = map->buckets[hash];
        HashNode* prev = NULL;
        while (current) {
            if (map->equal_func(current->key, key)) {
                if (prev) {
                    prev->next = current->next;
                } else {
                    map->buckets[hash] = current->next;
                }
                // KMM 管理内存，无需手动释放
                map->size--;
                return;
            }
            prev = current;
            current = current->next;
        }
    }
}

size_t hash_map_size(HashMap* map) {
    if (map) {
        return map->size;
    }
    return 0;
}

bool hash_map_is_empty(HashMap* map) {
    return hash_map_size(map) == 0;
}

void hash_map_clear(HashMap* map) {
    if (map) {
        // KMM 管理内存，无需手动释放
        for (size_t i = 0; i < map->capacity; i++) {
            map->buckets[i] = NULL;
        }
        map->size = 0;
    }
}

bool hash_map_contains(HashMap* map, void* key) {
    return hash_map_get(map, key) != NULL;
}

// 栈（Stack）实现
Stack* stack_create(size_t initial_capacity) {
    Stack* stack = (Stack*)kmm_v4_malloc(sizeof(Stack));
    if (stack) {
        stack->capacity = initial_capacity > 0 ? initial_capacity : 4;
        stack->size = 0;
        stack->data = (void**)kmm_v4_malloc(stack->capacity * sizeof(void*));
    }
    return stack;
}

void stack_destroy(Stack* stack) {
    // KMM 管理内存，无需手动释放
}

void stack_push(Stack* stack, void* element) {
    if (stack) {
        if (stack->size >= stack->capacity) {
            void** new_data = (void**)kmm_v4_realloc(stack->data, stack->capacity * 2 * sizeof(void*));
            if (new_data) {
                stack->data = new_data;
                stack->capacity *= 2;
            }
        }
        stack->data[stack->size++] = element;
    }
}

void* stack_pop(Stack* stack) {
    if (stack && stack->size > 0) {
        return stack->data[--stack->size];
    }
    return NULL;
}

void* stack_peek(Stack* stack) {
    if (stack && stack->size > 0) {
        return stack->data[stack->size - 1];
    }
    return NULL;
}

size_t stack_size(Stack* stack) {
    if (stack) {
        return stack->size;
    }
    return 0;
}

bool stack_is_empty(Stack* stack) {
    return stack_size(stack) == 0;
}

void stack_clear(Stack* stack) {
    if (stack) {
        stack->size = 0;
    }
}

// 队列（Queue）实现
Queue* queue_create(size_t initial_capacity) {
    Queue* queue = (Queue*)kmm_v4_malloc(sizeof(Queue));
    if (queue) {
        queue->capacity = initial_capacity > 0 ? initial_capacity : 4;
        queue->size = 0;
        queue->head = 0;
        queue->tail = 0;
        queue->data = (void**)kmm_v4_malloc(queue->capacity * sizeof(void*));
    }
    return queue;
}

void queue_destroy(Queue* queue) {
    // KMM 管理内存，无需手动释放
}

void queue_enqueue(Queue* queue, void* element) {
    if (queue) {
        if (queue->size >= queue->capacity) {
            void** new_data = (void**)kmm_v4_malloc(queue->capacity * 2 * sizeof(void*));
            if (new_data) {
                for (size_t i = 0; i < queue->size; i++) {
                    new_data[i] = queue->data[(queue->head + i) % queue->capacity];
                }
                // KMM 管理内存，无需手动释放
                queue->data = new_data;
                queue->head = 0;
                queue->tail = queue->size;
                queue->capacity *= 2;
            }
        }
        queue->data[queue->tail] = element;
        queue->tail = (queue->tail + 1) % queue->capacity;
        queue->size++;
    }
}

void* queue_dequeue(Queue* queue) {
    if (queue && queue->size > 0) {
        void* element = queue->data[queue->head];
        queue->head = (queue->head + 1) % queue->capacity;
        queue->size--;
        return element;
    }
    return NULL;
}

void* queue_front(Queue* queue) {
    if (queue && queue->size > 0) {
        return queue->data[queue->head];
    }
    return NULL;
}

size_t queue_size(Queue* queue) {
    if (queue) {
        return queue->size;
    }
    return 0;
}

bool queue_is_empty(Queue* queue) {
    return queue_size(queue) == 0;
}

void queue_clear(Queue* queue) {
    if (queue) {
        queue->size = 0;
        queue->head = 0;
        queue->tail = 0;
    }
}

// 通用哈希函数
size_t hash_string(void* key) {
    char* str = (char*)key;
    size_t hash = 5381;
    int c;
    while ((c = *str++)) {
        hash = ((hash << 5) + hash) + c;
    }
    return hash;
}

size_t hash_int(void* key) {
    return *(int*)key;
}

size_t hash_float(void* key) {
    union {
        float f;
        uint32_t u;
    } u;
    u.f = *(float*)key;
    return u.u;
}

// 通用比较函数
int equal_string(void* key1, void* key2) {
    return strcmp((char*)key1, (char*)key2) == 0;
}

int equal_int(void* key1, void* key2) {
    return *(int*)key1 == *(int*)key2;
}

int equal_float(void* key1, void* key2) {
    return *(float*)key1 == *(float*)key2;
}

// 集合（Set）实现
Set* set_create(size_t initial_capacity, size_t (*hash_func)(void* key), int (*equal_func)(void* key1, void* key2)) {
    Set* set = (Set*)kmm_v4_malloc(sizeof(Set));
    if (set) {
        set->capacity = initial_capacity > 0 ? initial_capacity : 16;
        set->size = 0;
        set->hash_func = hash_func;
        set->equal_func = equal_func;
        set->buckets = (SetNode**)kmm_v4_calloc(set->capacity, sizeof(SetNode*));
    }
    return set;
}

void set_destroy(Set* set) {
    // KMM 管理内存，无需手动释放
}

void set_add(Set* set, void* element) {
    if (set && element) {
        if (set_contains(set, element)) return;
        size_t hash = set->hash_func(element) % set->capacity;
        SetNode* node = (SetNode*)kmm_v4_malloc(sizeof(SetNode));
        if (node) {
            node->data = element;
            node->next = set->buckets[hash];
            set->buckets[hash] = node;
            set->size++;
        }
    }
}

void set_remove(Set* set, void* element) {
    if (set && element) {
        size_t hash = set->hash_func(element) % set->capacity;
        SetNode* current = set->buckets[hash];
        SetNode* prev = NULL;
        while (current) {
            if (set->equal_func(current->data, element)) {
                if (prev) {
                    prev->next = current->next;
                } else {
                    set->buckets[hash] = current->next;
                }
                // KMM 管理内存，无需手动释放
                set->size--;
                return;
            }
            prev = current;
            current = current->next;
        }
    }
}

bool set_contains(Set* set, void* element) {
    if (!set || !element) return false;
    size_t hash = set->hash_func(element) % set->capacity;
    SetNode* current = set->buckets[hash];
    while (current) {
        if (set->equal_func(current->data, element)) return true;
        current = current->next;
    }
    return false;
}

size_t set_size(Set* set) {
    return set ? set->size : 0;
}

bool set_is_empty(Set* set) {
    return set_size(set) == 0;
}

void set_clear(Set* set) {
    if (set) {
        // KMM 管理内存，无需手动释放
        for (size_t i = 0; i < set->capacity; i++) {
            set->buckets[i] = NULL;
        }
        set->size = 0;
    }
}

Set* set_union(Set* set1, Set* set2) {
    if (!set1 || !set2) return NULL;
    Set* result = set_create(set1->capacity + set2->capacity, set1->hash_func, set1->equal_func);
    if (!result) return NULL;
    for (size_t i = 0; i < set1->capacity; i++) {
        SetNode* current = set1->buckets[i];
        while (current) {
            set_add(result, current->data);
            current = current->next;
        }
    }
    for (size_t i = 0; i < set2->capacity; i++) {
        SetNode* current = set2->buckets[i];
        while (current) {
            set_add(result, current->data);
            current = current->next;
        }
    }
    return result;
}

Set* set_intersection(Set* set1, Set* set2) {
    if (!set1 || !set2) return NULL;
    Set* result = set_create(set1->capacity, set1->hash_func, set1->equal_func);
    if (!result) return NULL;
    for (size_t i = 0; i < set1->capacity; i++) {
        SetNode* current = set1->buckets[i];
        while (current) {
            if (set_contains(set2, current->data)) {
                set_add(result, current->data);
            }
            current = current->next;
        }
    }
    return result;
}

Set* set_difference(Set* set1, Set* set2) {
    if (!set1 || !set2) return NULL;
    Set* result = set_create(set1->capacity, set1->hash_func, set1->equal_func);
    if (!result) return NULL;
    for (size_t i = 0; i < set1->capacity; i++) {
        SetNode* current = set1->buckets[i];
        while (current) {
            if (!set_contains(set2, current->data)) {
                set_add(result, current->data);
            }
            current = current->next;
        }
    }
    return result;
}

// TreeMap 实现（红黑树）
static TreeMapNode* tree_map_create_node(void* key, void* value) {
    TreeMapNode* node = (TreeMapNode*)kmm_v4_malloc(sizeof(TreeMapNode));
    if (node) {
        node->key = key;
        node->value = value;
        node->left = NULL;
        node->right = NULL;
        node->parent = NULL;
        node->color = TREE_NODE_RED;
    }
    return node;
}

static void tree_map_rotate_left(TreeMap* map, TreeMapNode* x) {
    TreeMapNode* y = x->right;
    x->right = y->left;
    if (y->left) y->left->parent = x;
    y->parent = x->parent;
    if (!x->parent) map->root = y;
    else if (x == x->parent->left) x->parent->left = y;
    else x->parent->right = y;
    y->left = x;
    x->parent = y;
}

static void tree_map_rotate_right(TreeMap* map, TreeMapNode* x) {
    TreeMapNode* y = x->left;
    x->left = y->right;
    if (y->right) y->right->parent = x;
    y->parent = x->parent;
    if (!x->parent) map->root = y;
    else if (x == x->parent->right) x->parent->right = y;
    else x->parent->left = y;
    y->right = x;
    x->parent = y;
}

static void tree_map_fix_insert_violation(TreeMap* map, TreeMapNode* z) {
    while (z->parent && z->parent->color == TREE_NODE_RED) {
        if (z->parent == z->parent->parent->left) {
            TreeMapNode* y = z->parent->parent->right;
            if (y && y->color == TREE_NODE_RED) {
                z->parent->color = TREE_NODE_BLACK;
                y->color = TREE_NODE_BLACK;
                z->parent->parent->color = TREE_NODE_RED;
                z = z->parent->parent;
            } else {
                if (z == z->parent->right) {
                    z = z->parent;
                    tree_map_rotate_left(map, z);
                }
                z->parent->color = TREE_NODE_BLACK;
                z->parent->parent->color = TREE_NODE_RED;
                tree_map_rotate_right(map, z->parent->parent);
            }
        } else {
            TreeMapNode* y = z->parent->parent->left;
            if (y && y->color == TREE_NODE_RED) {
                z->parent->color = TREE_NODE_BLACK;
                y->color = TREE_NODE_BLACK;
                z->parent->parent->color = TREE_NODE_RED;
                z = z->parent->parent;
            } else {
                if (z == z->parent->left) {
                    z = z->parent;
                    tree_map_rotate_right(map, z);
                }
                z->parent->color = TREE_NODE_BLACK;
                z->parent->parent->color = TREE_NODE_RED;
                tree_map_rotate_left(map, z->parent->parent);
            }
        }
    }
    map->root->color = TREE_NODE_BLACK;
}

TreeMap* tree_map_create(int (*compare_func)(void* key1, void* key2)) {
    TreeMap* map = (TreeMap*)kmm_v4_malloc(sizeof(TreeMap));
    if (map) {
        map->root = NULL;
        map->size = 0;
        map->compare_func = compare_func;
    }
    return map;
}

static void tree_map_destroy_nodes(TreeMapNode* node) {
    // KMM 管理内存，无需手动释放
}

void tree_map_destroy(TreeMap* map) {
    // KMM 管理内存，无需手动释放
}

void tree_map_put(TreeMap* map, void* key, void* value) {
    if (!map) return;
    if (!map->root) {
        map->root = tree_map_create_node(key, value);
        map->root->color = TREE_NODE_BLACK;
        map->size++;
        return;
    }
    TreeMapNode* current = map->root;
    TreeMapNode* parent = NULL;
    while (current) {
        parent = current;
        int cmp = map->compare_func(key, current->key);
        if (cmp == 0) {
            current->value = value;
            return;
        }
        current = cmp < 0 ? current->left : current->right;
    }
    TreeMapNode* node = tree_map_create_node(key, value);
    node->parent = parent;
    int cmp = map->compare_func(key, parent->key);
    if (cmp < 0) parent->left = node;
    else parent->right = node;
    tree_map_fix_insert_violation(map, node);
    map->size++;
}

static TreeMapNode* tree_map_search(TreeMap* map, void* key) {
    TreeMapNode* current = map->root;
    while (current) {
        int cmp = map->compare_func(key, current->key);
        if (cmp == 0) return current;
        current = cmp < 0 ? current->left : current->right;
    }
    return NULL;
}

void* tree_map_get(TreeMap* map, void* key) {
    if (!map) return NULL;
    TreeMapNode* node = tree_map_search(map, key);
    return node ? node->value : NULL;
}

bool tree_map_contains(TreeMap* map, void* key) {
    return tree_map_search(map, key) != NULL;
}

static TreeMapNode* tree_map_minimum(TreeMapNode* node) {
    while (node && node->left) node = node->left;
    return node;
}

static TreeMapNode* tree_map_maximum(TreeMapNode* node) {
    while (node && node->right) node = node->right;
    return node;
}

void* tree_map_first_key(TreeMap* map) {
    if (!map) return NULL;
    TreeMapNode* node = tree_map_minimum(map->root);
    return node ? node->key : NULL;
}

void* tree_map_last_key(TreeMap* map) {
    if (!map) return NULL;
    TreeMapNode* node = tree_map_maximum(map->root);
    return node ? node->key : NULL;
}

void* tree_map_lower_bound(TreeMap* map, void* key) {
    if (!map) return NULL;
    TreeMapNode* result = NULL;
    TreeMapNode* current = map->root;
    while (current) {
        int cmp = map->compare_func(key, current->key);
        if (cmp <= 0) {
            result = current;
            current = current->left;
        } else {
            current = current->right;
        }
    }
    return result ? result->key : NULL;
}

void* tree_map_upper_bound(TreeMap* map, void* key) {
    if (!map) return NULL;
    TreeMapNode* result = NULL;
    TreeMapNode* current = map->root;
    while (current) {
        int cmp = map->compare_func(key, current->key);
        if (cmp < 0) {
            result = current;
            current = current->left;
        } else {
            current = current->right;
        }
    }
    return result ? result->key : NULL;
}

size_t tree_map_size(TreeMap* map) {
    return map ? map->size : 0;
}

bool tree_map_is_empty(TreeMap* map) {
    return tree_map_size(map) == 0;
}

void tree_map_clear(TreeMap* map) {
    if (map) {
        // KMM 管理内存，无需手动释放
        map->root = NULL;
        map->size = 0;
    }
}

void tree_map_remove(TreeMap* map, void* key) {
    if (!map || !map->root) return;
    TreeMapNode* z = tree_map_search(map, key);
    if (!z) return;
    TreeMapNode* y = z;
    TreeMapNode* x;
    TreeNodeColor y_original_color = y->color;
    if (!z->left) {
        x = z->right;
        TreeMapNode* parent = z->parent;
        if (!parent) map->root = x;
        else if (z == parent->left) parent->left = x;
        else parent->right = x;
        if (x) x->parent = parent;
    } else if (!z->right) {
        x = z->left;
        TreeMapNode* parent = z->parent;
        if (!parent) map->root = x;
        else if (z == parent->left) parent->left = x;
        else parent->right = x;
        if (x) x->parent = parent;
    } else {
        y = tree_map_minimum(z->right);
        y_original_color = y->color;
        x = y->right;
        if (y->parent == z) {
            if (x) x->parent = y;
        } else {
            TreeMapNode* parent = y->parent;
            parent->left = x;
            if (x) x->parent = parent;
            y->right = z->right;
            y->right->parent = y;
        }
        y->left = z->left;
        y->left->parent = y;
        y->color = z->color;
        TreeMapNode* parent = z->parent;
        if (!parent) map->root = y;
        else if (z == parent->left) parent->left = y;
        else parent->right = y;
        y->parent = parent;
    }
    // KMM 管理内存，无需手动释放
    map->size--;
}

// PriorityQueue 实现
PriorityQueue* priority_queue_create(size_t initial_capacity) {
    PriorityQueue* pq = (PriorityQueue*)kmm_v4_malloc(sizeof(PriorityQueue));
    if (pq) {
        pq->capacity = initial_capacity > 0 ? initial_capacity : 16;
        pq->size = 0;
        pq->data = (PriorityQueueNode*)kmm_v4_malloc(pq->capacity * sizeof(PriorityQueueNode));
    }
    return pq;
}

void priority_queue_destroy(PriorityQueue* pq) {
    // KMM 管理内存，无需手动释放
}

static void priority_queue_sift_up(PriorityQueue* pq, size_t index) {
    while (index > 0) {
        size_t parent = (index - 1) / 2;
        if (pq->data[index].priority > pq->data[parent].priority) {
            PriorityQueueNode temp = pq->data[index];
            pq->data[index] = pq->data[parent];
            pq->data[parent] = temp;
            index = parent;
        } else {
            break;
        }
    }
}

static void priority_queue_sift_down(PriorityQueue* pq, size_t index) {
    while (2 * index + 1 < pq->size) {
        size_t largest = 2 * index + 1;
        if (largest + 1 < pq->size && pq->data[largest + 1].priority > pq->data[largest].priority) {
            largest++;
        }
        if (pq->data[index].priority >= pq->data[largest].priority) break;
        PriorityQueueNode temp = pq->data[index];
        pq->data[index] = pq->data[largest];
        pq->data[largest] = temp;
        index = largest;
    }
}

void priority_queue_push(PriorityQueue* pq, void* element, int priority) {
    if (!pq) return;
    if (pq->size >= pq->capacity) {
        pq->capacity *= 2;
        pq->data = (PriorityQueueNode*)kmm_v4_realloc(pq->data, pq->capacity * sizeof(PriorityQueueNode));
    }
    pq->data[pq->size].data = element;
    pq->data[pq->size].priority = priority;
    pq->size++;
    priority_queue_sift_up(pq, pq->size - 1);
}

void* priority_queue_pop(PriorityQueue* pq) {
    if (!pq || pq->size == 0) return NULL;
    void* result = pq->data[0].data;
    pq->size--;
    if (pq->size > 0) {
        pq->data[0] = pq->data[pq->size];
        priority_queue_sift_down(pq, 0);
    }
    return result;
}

void* priority_queue_peek(PriorityQueue* pq) {
    if (!pq || pq->size == 0) return NULL;
    return pq->data[0].data;
}

size_t priority_queue_size(PriorityQueue* pq) {
    return pq ? pq->size : 0;
}

bool priority_queue_is_empty(PriorityQueue* pq) {
    return priority_queue_size(pq) == 0;
}

void priority_queue_clear(PriorityQueue* pq) {
    if (pq) pq->size = 0;
}

void priority_queue_change_priority(PriorityQueue* pq, void* element, int new_priority) {
    if (!pq) return;
    for (size_t i = 0; i < pq->size; i++) {
        if (pq->data[i].data == element) {
            int old_priority = pq->data[i].priority;
            pq->data[i].priority = new_priority;
            if (new_priority > old_priority) {
                priority_queue_sift_up(pq, i);
            } else {
                priority_queue_sift_down(pq, i);
            }
            break;
        }
    }
}
