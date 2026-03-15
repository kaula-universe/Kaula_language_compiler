#ifndef KAULA_H
#define KAULA_H

#include <stdio.h>
#include <stdlib.h>
#include <stdint.h>
#include <string.h>
#include <time.h>
#include <stdbool.h>
#include <math.h>

// ==================== Configuration Constants ====================
#define VO_CACHE_SIZE 2048
#define QUEUE_CAPACITY 100000
#define SPENDABLE_CAPACITY 2048
#define HIGH_PRIORITY 0
#define MEDIUM_PRIORITY 1
#define LOW_PRIORITY 2
#define PRIORITY_LEVELS 3
#define MAX_RECURSION_DEPTH 8
#define MEMORY_POOL_SIZE (256 * 1024 * 1024)  // 256MB memory pool



// ==================== 1. High-Precision Time Measurement ====================
// Use QueryPerformanceCounter() for high-precision time measurement on Windows
#include <windows.h>

static inline uint64_t get_clock_cycles();
static inline double get_clock_frequency();

// Time conversion macros with division by zero protection
#define CYCLES_TO_NS(cycles) ({ \
    double freq = get_clock_frequency(); \
    (freq != 0.0) ? ((cycles) * 1000000000.0 / freq) : 0.0; \
})
#define CYCLES_TO_US(cycles) ({ \
    double freq = get_clock_frequency(); \
    (freq != 0.0) ? ((cycles) * 1000000.0 / freq) : 0.0; \
})
#define CYCLES_TO_MS(cycles) ({ \
    double freq = get_clock_frequency(); \
    (freq != 0.0) ? ((cycles) * 1000.0 / freq) : 0.0; \
})

// ==================== 2. High-Speed Memory Allocator ====================
typedef struct FastAllocator {
    uint8_t* base;
    size_t offset;
} FastAllocator;

extern FastAllocator global_allocator;

void fast_allocator_init();
void* fast_alloc(size_t size);
void* fast_calloc(size_t num, size_t size);
void fast_free(void* ptr);

// ==================== 3. VO (Virtual On-site) Implementation ====================
typedef struct VOData {
    void* value;
    void* (*code)(void*);
    uint8_t has_code;
    uint64_t last_access;
    int code_index;
} VOData;

typedef struct VOModule {
    VOData* data_cache;
    void* (*code_cache)[VO_CACHE_SIZE + 1];
    int cache_max;
} VOModule;

VOModule* vo_create(int cache_max);
void vo_data_load(VOModule* vo, int index, void* value);
void vo_code_load(VOModule* vo, int index, void* (*func)(void*));
void vo_associate(VOModule* vo, int data_index, int code_index);
void* vo_access(VOModule* vo, int index);

// ==================== 4. spend/call Implementation ====================
typedef struct Spendable {
    void** components;
    int count;
    int call_counter;
} Spendable;

static inline Spendable* spendable_create(int capacity);
static inline void spendable_add(Spendable* sp, void* component);
static inline void* spendable_call(Spendable* sp);

// ==================== 5. Three-Level Priority Queue System ====================
typedef struct Task {
    void* (*func)(void*);
    void* arg;
    int priority;
} Task;

typedef struct SimpleQueue {
    Task* tasks;
    int head;
    int tail;
    int size;
    int capacity;
} SimpleQueue;

static inline SimpleQueue* simple_queue_create(int capacity);
static inline int simple_queue_is_empty(SimpleQueue* queue);
static inline int simple_queue_is_full(SimpleQueue* queue);
static inline void simple_queue_enqueue(SimpleQueue* queue, Task task);
static inline Task* simple_queue_dequeue(SimpleQueue* queue);

typedef struct PriorityQueue {
    SimpleQueue* queues[PRIORITY_LEVELS];
} PriorityQueue;

static inline PriorityQueue* priority_queue_create(int capacity_per_queue);
static inline void priority_queue_add(PriorityQueue* pq, int priority, 
                                     void* (*func)(void*), void* arg);
static inline void* priority_queue_execute_next(PriorityQueue* pq);
static inline int priority_queue_batch_add(PriorityQueue* pq, int priority,
                                          void* (*func)(void*), void** args,
                                          int count);
static inline int priority_queue_batch_execute(PriorityQueue* pq, int max_tasks);



#endif // KAULA_H