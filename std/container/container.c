#include "container.h"
#include <stdlib.h>
#include <string.h>

// 动态数组（Vector）实现
Vector* vector_create(size_t initial_capacity) {
    Vector* vector = (Vector*)malloc(sizeof(Vector));
    if (vector) {
        vector->capacity = initial_capacity > 0 ? initial_capacity : 4;
        vector->size = 0;
        vector->data = (void**)malloc(vector->capacity * sizeof(void*));
    }
    return vector;
}

void vector_destroy(Vector* vector) {
    if (vector) {
        free(vector->data);
        free(vector);
    }
}

void vector_reserve(Vector* vector, size_t capacity) {
    if (vector && capacity > vector->capacity) {
        void** new_data = (void**)realloc(vector->data, capacity * sizeof(void*));
        if (new_data) {
            vector->data = new_data;
            vector->capacity = capacity;
        }
    }
}

void vector_push_back(Vector* vector, void* element) {
    if (vector) {
        if (vector->size >= vector->capacity) {
            vector_reserve(vector, vector->capacity * 2);
        }
        vector->data[vector->size++] = element;
    }
}

void* vector_get(Vector* vector, size_t index) {
    if (vector && index < vector->size) {
        return vector->data[index];
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

// 链表（LinkedList）实现
LinkedList* linked_list_create() {
    LinkedList* list = (LinkedList*)malloc(sizeof(LinkedList));
    if (list) {
        list->head = NULL;
        list->tail = NULL;
        list->size = 0;
    }
    return list;
}

void linked_list_destroy(LinkedList* list) {
    if (list) {
        ListNode* current = list->head;
        while (current) {
            ListNode* next = current->next;
            free(current);
            current = next;
        }
        free(list);
    }
}

void linked_list_push_front(LinkedList* list, void* element) {
    if (list) {
        ListNode* node = (ListNode*)malloc(sizeof(ListNode));
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
        ListNode* node = (ListNode*)malloc(sizeof(ListNode));
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
        free(node);
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
        free(node);
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
        free(current);
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
        ListNode* current = list->head;
        while (current) {
            ListNode* next = current->next;
            free(current);
            current = next;
        }
        list->head = NULL;
        list->tail = NULL;
        list->size = 0;
    }
}

// 哈希表（HashMap）实现
HashMap* hash_map_create(size_t initial_capacity, size_t (*hash_func)(void* key), int (*equal_func)(void* key1, void* key2)) {
    HashMap* map = (HashMap*)malloc(sizeof(HashMap));
    if (map) {
        map->capacity = initial_capacity > 0 ? initial_capacity : 16;
        map->size = 0;
        map->hash_func = hash_func;
        map->equal_func = equal_func;
        map->buckets = (HashNode**)calloc(map->capacity, sizeof(HashNode*));
    }
    return map;
}

void hash_map_destroy(HashMap* map) {
    if (map) {
        for (size_t i = 0; i < map->capacity; i++) {
            HashNode* current = map->buckets[i];
            while (current) {
                HashNode* next = current->next;
                free(current);
                current = next;
            }
        }
        free(map->buckets);
        free(map);
    }
}

void hash_map_put(HashMap* map, void* key, void* value) {
    if (map) {
        size_t hash = map->hash_func(key) % map->capacity;
        HashNode* current = map->buckets[hash];
        while (current) {
            if (map->equal_func(current->key, key)) {
                current->value = value;
                return;
            }
            current = current->next;
        }
        HashNode* node = (HashNode*)malloc(sizeof(HashNode));
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
                free(current);
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
        for (size_t i = 0; i < map->capacity; i++) {
            HashNode* current = map->buckets[i];
            while (current) {
                HashNode* next = current->next;
                free(current);
                current = next;
            }
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
    Stack* stack = (Stack*)malloc(sizeof(Stack));
    if (stack) {
        stack->capacity = initial_capacity > 0 ? initial_capacity : 4;
        stack->size = 0;
        stack->data = (void**)malloc(stack->capacity * sizeof(void*));
    }
    return stack;
}

void stack_destroy(Stack* stack) {
    if (stack) {
        free(stack->data);
        free(stack);
    }
}

void stack_push(Stack* stack, void* element) {
    if (stack) {
        if (stack->size >= stack->capacity) {
            void** new_data = (void**)realloc(stack->data, stack->capacity * 2 * sizeof(void*));
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
    Queue* queue = (Queue*)malloc(sizeof(Queue));
    if (queue) {
        queue->capacity = initial_capacity > 0 ? initial_capacity : 4;
        queue->size = 0;
        queue->head = 0;
        queue->tail = 0;
        queue->data = (void**)malloc(queue->capacity * sizeof(void*));
    }
    return queue;
}

void queue_destroy(Queue* queue) {
    if (queue) {
        free(queue->data);
        free(queue);
    }
}

void queue_enqueue(Queue* queue, void* element) {
    if (queue) {
        if (queue->size >= queue->capacity) {
            void** new_data = (void**)malloc(queue->capacity * 2 * sizeof(void*));
            if (new_data) {
                for (size_t i = 0; i < queue->size; i++) {
                    new_data[i] = queue->data[(queue->head + i) % queue->capacity];
                }
                free(queue->data);
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
