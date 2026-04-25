#include "kaula.h"

// ==================== 跨平台优先级队列实现 ====================

typedef struct Task {
    void* (*func)(void*);
    void* arg;
    int priority;
} Task;

typedef struct SimpleQueue {
    Task* tasks;
    int capacity;
    int head;
    int tail;
    int size;
} SimpleQueue;

typedef struct PriorityQueue {
    SimpleQueue* queues[PRIORITY_LEVELS];
} PriorityQueue;

// 简单的循环队列实现
static inline SimpleQueue* simple_queue_create(int capacity) {
    SimpleQueue* queue = (SimpleQueue*)fast_alloc(sizeof(SimpleQueue));
    if (!queue) return NULL;
    
    queue->capacity = capacity;
    queue->head = 0;
    queue->tail = 0;
    queue->size = 0;
    queue->tasks = (Task*)fast_calloc(capacity, sizeof(Task));
    if (!queue->tasks) {
        fast_free(queue);
        return NULL;
    }
    return queue;
}

static inline int simple_queue_is_empty(SimpleQueue* queue) {
    return queue->size == 0;
}

static inline int simple_queue_is_full(SimpleQueue* queue) {
    return queue->size >= queue->capacity;
}

static inline void simple_queue_enqueue(SimpleQueue* queue, Task task) {
    if (!simple_queue_is_full(queue)) {
        queue->tasks[queue->tail] = task;
        queue->tail = (queue->tail + 1) % queue->capacity;
        queue->size++;
    }
}

static inline Task* simple_queue_dequeue(SimpleQueue* queue) {
    if (!simple_queue_is_empty(queue)) {
        Task* task = &queue->tasks[queue->head];
        queue->head = (queue->head + 1) % queue->capacity;
        queue->size--;
        return task;
    }
    return NULL;
}

PriorityQueue* priority_queue_create(size_t capacity_per_queue) {
    PriorityQueue* pq = (PriorityQueue*)fast_alloc(sizeof(PriorityQueue));
    if (!pq) return NULL;
    
    for (int i = 0; i < PRIORITY_LEVELS; i++) {
        pq->queues[i] = simple_queue_create((int)capacity_per_queue);
        if (!pq->queues[i]) {
            for (int j = 0; j < i; j++) {
                fast_free(pq->queues[j]->tasks);
                fast_free(pq->queues[j]);
            }
            fast_free(pq);
            return NULL;
        }
    }
    return pq;
}

void priority_queue_destroy(PriorityQueue* pq) {
    if (!pq) return;
    for (int i = 0; i < PRIORITY_LEVELS; i++) {
        if (pq->queues[i]) {
            fast_free(pq->queues[i]->tasks);
            fast_free(pq->queues[i]);
        }
    }
    fast_free(pq);
}

void priority_queue_add(PriorityQueue* pq, int priority, void (*func)(void*), void* arg) {
    if (priority < 0 || priority >= PRIORITY_LEVELS) {
        priority = 0;
    }
    Task task = {(void*(*)(void*))func, arg, priority};
    simple_queue_enqueue(pq->queues[priority], task);
}

void* priority_queue_execute_next(PriorityQueue* pq) {
    for (int i = 0; i < PRIORITY_LEVELS; i++) {
        if (!simple_queue_is_empty(pq->queues[i])) {
            Task* task = simple_queue_dequeue(pq->queues[i]);
            if (task && task->func) {
                return task->func(task->arg);
            }
        }
    }
    return NULL;
}

int priority_queue_batch_add(PriorityQueue* pq, int priority, void (*func)(void*), void** args, int count) {
    int added = 0;
    for (int i = 0; i < count; i++) {
        if (!simple_queue_is_full(pq->queues[priority])) {
            Task task = {(void*(*)(void*))func, args[i], priority};
            simple_queue_enqueue(pq->queues[priority], task);
            added++;
        }
    }
    return added;
}

int priority_queue_batch_execute(PriorityQueue* pq, int max_tasks) {
    int executed = 0;
    while (executed < max_tasks) {
        void* result = priority_queue_execute_next(pq);
        if (!result) break;
        executed++;
    }
    return executed;
}