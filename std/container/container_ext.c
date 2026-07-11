#include "container_ext.h"
#include <string.h>

#define HASHSET_INITIAL_CAPACITY 16
#define HASHSET_LOAD_FACTOR 0.75
#define DEQUE_INITIAL_CAPACITY 8
#define PQ_INITIAL_CAPACITY 16

typedef struct HashNode {
    struct HashNode* next;
    void* data;
} HashNode;

struct HashSet {
    HashNode** buckets;
    size_t bucket_count;
    size_t size;
    size_t elem_size;
    HashFunc hash_func;
    HashEqualFunc equal_func;
};

struct Deque {
    unsigned char* buffer;
    size_t capacity;
    size_t elem_size;
    size_t front;
    size_t size;
};

struct PriorityQueue {
    unsigned char* data;
    size_t capacity;
    size_t size;
    size_t elem_size;
    CompareFunc cmp;
};

static size_t hash_string(const void* key) {
    const char* str = *(const char* const*)key;
    size_t h = 5381;
    if (!str) return h;
    while (*str) {
        h = ((h << 5) + h) + (unsigned char)*str;
        str++;
    }
    return h;
}

static int equal_string(const void* a, const void* b) {
    const char* sa = *(const char* const*)a;
    const char* sb = *(const char* const*)b;
    if (!sa && !sb) return 1;
    if (!sa || !sb) return 0;
    return strcmp(sa, sb) == 0;
}

static void hashset_resize(HashSet* set, size_t new_bucket_count) {
    HashNode** new_buckets;
    size_t i;
    if (!set || new_bucket_count == 0) return;
    new_buckets = (HashNode**)kmm_v4_malloc(new_bucket_count * sizeof(HashNode*));
    if (!new_buckets) return;
    memset(new_buckets, 0, new_bucket_count * sizeof(HashNode*));
    for (i = 0; i < set->bucket_count; i++) {
        HashNode* node = set->buckets[i];
        while (node) {
            HashNode* next = node->next;
            size_t idx = set->hash_func(node->data) % new_bucket_count;
            node->next = new_buckets[idx];
            new_buckets[idx] = node;
            node = next;
        }
    }
    kmm_v4_free(set->buckets);
    set->buckets = new_buckets;
    set->bucket_count = new_bucket_count;
}

HashSet* hashset_create(size_t elem_size, HashFunc hash, HashEqualFunc equal) {
    HashSet* set;
    if (elem_size == 0) return NULL;
    set = (HashSet*)kmm_v4_malloc(sizeof(HashSet));
    if (!set) return NULL;
    set->buckets = (HashNode**)kmm_v4_malloc(HASHSET_INITIAL_CAPACITY * sizeof(HashNode*));
    if (!set->buckets) {
        kmm_v4_free(set);
        return NULL;
    }
    memset(set->buckets, 0, HASHSET_INITIAL_CAPACITY * sizeof(HashNode*));
    set->bucket_count = HASHSET_INITIAL_CAPACITY;
    set->size = 0;
    set->elem_size = elem_size;
    set->hash_func = hash;
    set->equal_func = equal;
    return set;
}

void hashset_destroy(HashSet* set) {
    size_t i;
    if (!set) return;
    for (i = 0; i < set->bucket_count; i++) {
        HashNode* node = set->buckets[i];
        while (node) {
            HashNode* next = node->next;
            kmm_v4_free(node->data);
            kmm_v4_free(node);
            node = next;
        }
    }
    kmm_v4_free(set->buckets);
    kmm_v4_free(set);
}

bool_t hashset_insert(HashSet* set, const void* elem) {
    size_t idx;
    HashNode* node;
    if (!set || !elem) return false;
    idx = set->hash_func(elem) % set->bucket_count;
    node = set->buckets[idx];
    while (node) {
        if (set->equal_func(node->data, elem)) {
            return false;
        }
        node = node->next;
    }
    node = (HashNode*)kmm_v4_malloc(sizeof(HashNode));
    if (!node) return false;
    node->data = kmm_v4_malloc(set->elem_size);
    if (!node->data) {
        kmm_v4_free(node);
        return false;
    }
    memcpy(node->data, elem, set->elem_size);
    node->next = set->buckets[idx];
    set->buckets[idx] = node;
    set->size++;
    if (set->size > set->bucket_count * HASHSET_LOAD_FACTOR) {
        hashset_resize(set, set->bucket_count * 2);
    }
    return true;
}

bool_t hashset_remove(HashSet* set, const void* elem) {
    size_t idx;
    HashNode *node, *prev;
    if (!set || !elem) return false;
    idx = set->hash_func(elem) % set->bucket_count;
    node = set->buckets[idx];
    prev = NULL;
    while (node) {
        if (set->equal_func(node->data, elem)) {
            if (prev) {
                prev->next = node->next;
            } else {
                set->buckets[idx] = node->next;
            }
            kmm_v4_free(node->data);
            kmm_v4_free(node);
            set->size--;
            return true;
        }
        prev = node;
        node = node->next;
    }
    return false;
}

bool_t hashset_contains(const HashSet* set, const void* elem) {
    size_t idx;
    HashNode* node;
    if (!set || !elem) return false;
    idx = set->hash_func(elem) % set->bucket_count;
    node = set->buckets[idx];
    while (node) {
        if (set->equal_func(node->data, elem)) {
            return true;
        }
        node = node->next;
    }
    return false;
}

size_t hashset_size(const HashSet* set) {
    if (!set) return 0;
    return set->size;
}

bool_t hashset_is_empty(const HashSet* set) {
    if (!set) return true;
    return set->size == 0;
}

void hashset_clear(HashSet* set) {
    size_t i;
    if (!set) return;
    for (i = 0; i < set->bucket_count; i++) {
        HashNode* node = set->buckets[i];
        while (node) {
            HashNode* next = node->next;
            kmm_v4_free(node->data);
            kmm_v4_free(node);
            node = next;
        }
        set->buckets[i] = NULL;
    }
    set->size = 0;
}

void** hashset_to_array(const HashSet* set, size_t* out_count) {
    void** arr;
    size_t count = 0;
    size_t i;
    if (!set || !out_count) return NULL;
    arr = (void**)kmm_v4_malloc(set->size * sizeof(void*));
    if (!arr) {
        *out_count = 0;
        return NULL;
    }
    for (i = 0; i < set->bucket_count; i++) {
        HashNode* node = set->buckets[i];
        while (node) {
            arr[count++] = node->data;
            node = node->next;
        }
    }
    *out_count = count;
    return arr;
}

HashSet* hashset_create_string(void) {
    return hashset_create(sizeof(char*), hash_string, equal_string);
}

bool_t hashset_insert_string(HashSet* set, const char* str) {
    char* dup;
    if (!set || !str) return false;
    dup = (char*)kmm_v4_malloc(strlen(str) + 1);
    if (!dup) return false;
    strcpy(dup, str);
    if (!hashset_insert(set, &dup)) {
        kmm_v4_free(dup);
        return false;
    }
    return true;
}

bool_t hashset_contains_string(const HashSet* set, const char* str) {
    if (!set || !str) return false;
    return hashset_contains(set, &str);
}

bool_t hashset_remove_string(HashSet* set, const char* str) {
    size_t idx;
    HashNode *node, *prev;
    if (!set || !str) return false;
    idx = set->hash_func(&str) % set->bucket_count;
    node = set->buckets[idx];
    prev = NULL;
    while (node) {
        if (set->equal_func(node->data, &str)) {
            if (prev) {
                prev->next = node->next;
            } else {
                set->buckets[idx] = node->next;
            }
            kmm_v4_free(*(char**)node->data);
            kmm_v4_free(node->data);
            kmm_v4_free(node);
            set->size--;
            return true;
        }
        prev = node;
        node = node->next;
    }
    return false;
}

Deque* deque_create(size_t elem_size) {
    Deque* deque;
    if (elem_size == 0) return NULL;
    deque = (Deque*)kmm_v4_malloc(sizeof(Deque));
    if (!deque) return NULL;
    deque->buffer = (unsigned char*)kmm_v4_malloc(DEQUE_INITIAL_CAPACITY * elem_size);
    if (!deque->buffer) {
        kmm_v4_free(deque);
        return NULL;
    }
    deque->capacity = DEQUE_INITIAL_CAPACITY;
    deque->elem_size = elem_size;
    deque->front = 0;
    deque->size = 0;
    return deque;
}

void deque_destroy(Deque* deque) {
    if (!deque) return;
    kmm_v4_free(deque->buffer);
    kmm_v4_free(deque);
}

static bool_t deque_grow(Deque* deque) {
    size_t new_cap;
    unsigned char* new_buf;
    size_t i;
    if (!deque) return false;
    new_cap = deque->capacity * 2;
    new_buf = (unsigned char*)kmm_v4_malloc(new_cap * deque->elem_size);
    if (!new_buf) return false;
    for (i = 0; i < deque->size; i++) {
        size_t src_idx = (deque->front + i) % deque->capacity;
        memcpy(new_buf + i * deque->elem_size, deque->buffer + src_idx * deque->elem_size, deque->elem_size);
    }
    kmm_v4_free(deque->buffer);
    deque->buffer = new_buf;
    deque->capacity = new_cap;
    deque->front = 0;
    return true;
}

bool_t deque_push_front(Deque* deque, const void* elem) {
    size_t new_front;
    if (!deque || !elem) return false;
    if (deque->size >= deque->capacity) {
        if (!deque_grow(deque)) return false;
    }
    new_front = (deque->front == 0) ? deque->capacity - 1 : deque->front - 1;
    memcpy(deque->buffer + new_front * deque->elem_size, elem, deque->elem_size);
    deque->front = new_front;
    deque->size++;
    return true;
}

bool_t deque_push_back(Deque* deque, const void* elem) {
    size_t back_idx;
    if (!deque || !elem) return false;
    if (deque->size >= deque->capacity) {
        if (!deque_grow(deque)) return false;
    }
    back_idx = (deque->front + deque->size) % deque->capacity;
    memcpy(deque->buffer + back_idx * deque->elem_size, elem, deque->elem_size);
    deque->size++;
    return true;
}

bool_t deque_pop_front(Deque* deque, void* out) {
    if (!deque || deque->size == 0) return false;
    if (out) {
        memcpy(out, deque->buffer + deque->front * deque->elem_size, deque->elem_size);
    }
    deque->front = (deque->front + 1) % deque->capacity;
    deque->size--;
    return true;
}

bool_t deque_pop_back(Deque* deque, void* out) {
    size_t back_idx;
    if (!deque || deque->size == 0) return false;
    back_idx = (deque->front + deque->size - 1) % deque->capacity;
    if (out) {
        memcpy(out, deque->buffer + back_idx * deque->elem_size, deque->elem_size);
    }
    deque->size--;
    return true;
}

void* deque_front(const Deque* deque) {
    if (!deque || deque->size == 0) return NULL;
    return deque->buffer + deque->front * deque->elem_size;
}

void* deque_back(const Deque* deque) {
    size_t back_idx;
    if (!deque || deque->size == 0) return NULL;
    back_idx = (deque->front + deque->size - 1) % deque->capacity;
    return deque->buffer + back_idx * deque->elem_size;
}

void* deque_at(const Deque* deque, size_t index) {
    size_t idx;
    if (!deque || index >= deque->size) return NULL;
    idx = (deque->front + index) % deque->capacity;
    return deque->buffer + idx * deque->elem_size;
}

size_t deque_size(const Deque* deque) {
    if (!deque) return 0;
    return deque->size;
}

bool_t deque_is_empty(const Deque* deque) {
    if (!deque) return true;
    return deque->size == 0;
}

void deque_clear(Deque* deque) {
    if (!deque) return;
    deque->front = 0;
    deque->size = 0;
}

static void pq_swap(unsigned char* data, size_t i, size_t j, size_t elem_size) {
    unsigned char* pa = data + i * elem_size;
    unsigned char* pb = data + j * elem_size;
    size_t k;
    for (k = 0; k < elem_size; k++) {
        unsigned char tmp = pa[k];
        pa[k] = pb[k];
        pb[k] = tmp;
    }
}

static void pq_sift_up(PriorityQueue* pq, size_t index) {
    unsigned char* arr = pq->data;
    size_t parent;
    while (index > 0) {
        parent = (index - 1) / 2;
        if (pq->cmp(arr + index * pq->elem_size, arr + parent * pq->elem_size) < 0) {
            pq_swap(arr, index, parent, pq->elem_size);
            index = parent;
        } else {
            break;
        }
    }
}

static void pq_sift_down(PriorityQueue* pq, size_t index) {
    unsigned char* arr = pq->data;
    size_t left, right, smallest;
    while (1) {
        left = 2 * index + 1;
        right = 2 * index + 2;
        smallest = index;
        if (left < pq->size && pq->cmp(arr + left * pq->elem_size, arr + smallest * pq->elem_size) < 0) {
            smallest = left;
        }
        if (right < pq->size && pq->cmp(arr + right * pq->elem_size, arr + smallest * pq->elem_size) < 0) {
            smallest = right;
        }
        if (smallest != index) {
            pq_swap(arr, index, smallest, pq->elem_size);
            index = smallest;
        } else {
            break;
        }
    }
}

PriorityQueue* pq_create(size_t elem_size, CompareFunc cmp) {
    PriorityQueue* pq;
    if (elem_size == 0 || !cmp) return NULL;
    pq = (PriorityQueue*)kmm_v4_malloc(sizeof(PriorityQueue));
    if (!pq) return NULL;
    pq->data = (unsigned char*)kmm_v4_malloc(PQ_INITIAL_CAPACITY * elem_size);
    if (!pq->data) {
        kmm_v4_free(pq);
        return NULL;
    }
    pq->capacity = PQ_INITIAL_CAPACITY;
    pq->size = 0;
    pq->elem_size = elem_size;
    pq->cmp = cmp;
    return pq;
}

void pq_destroy(PriorityQueue* pq) {
    if (!pq) return;
    kmm_v4_free(pq->data);
    kmm_v4_free(pq);
}

bool_t pq_push(PriorityQueue* pq, const void* elem) {
    unsigned char* new_data;
    if (!pq || !elem) return false;
    if (pq->size >= pq->capacity) {
        size_t new_cap = pq->capacity * 2;
        new_data = (unsigned char*)kmm_v4_malloc(new_cap * pq->elem_size);
        if (!new_data) return false;
        memcpy(new_data, pq->data, pq->size * pq->elem_size);
        kmm_v4_free(pq->data);
        pq->data = new_data;
        pq->capacity = new_cap;
    }
    memcpy(pq->data + pq->size * pq->elem_size, elem, pq->elem_size);
    pq->size++;
    pq_sift_up(pq, pq->size - 1);
    return true;
}

bool_t pq_pop(PriorityQueue* pq, void* out) {
    if (!pq || pq->size == 0) return false;
    if (out) {
        memcpy(out, pq->data, pq->elem_size);
    }
    pq->size--;
    if (pq->size > 0) {
        memcpy(pq->data, pq->data + pq->size * pq->elem_size, pq->elem_size);
        pq_sift_down(pq, 0);
    }
    return true;
}

void* pq_top(const PriorityQueue* pq) {
    if (!pq || pq->size == 0) return NULL;
    return pq->data;
}

size_t pq_size(const PriorityQueue* pq) {
    if (!pq) return 0;
    return pq->size;
}

bool_t pq_is_empty(const PriorityQueue* pq) {
    if (!pq) return true;
    return pq->size == 0;
}

void pq_clear(PriorityQueue* pq) {
    if (!pq) return;
    pq->size = 0;
}
