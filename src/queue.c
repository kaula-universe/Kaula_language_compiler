#include "kaula.h"

static inline SimpleQueue* simple_queue_create(int capacity) {
    SimpleQueue* queue = (SimpleQueue*)fast_alloc(sizeof(SimpleQueue));
    queue->capacity = capacity;
    queue->head = 0;
    queue->tail = 0;
    queue->size = 0;
    queue->tasks = (Task*)fast_calloc(capacity, sizeof(Task));
    return queue;
}

static inline int simple_queue_is_empty(SimpleQueue* queue) {
    return queue->size == 0;
}

static inline int simple_queue_is_full(SimpleQueue* queue) {
    return queue->size >= queue->capacity;
}

static inline void simple_queue_enqueue(SimpleQueue* queue, Task task) {
    // 直接使用tail指针位置，实现空穴管理
    queue->tasks[queue->tail] = task;
    queue->tail = (queue->tail + 1) % queue->capacity;
    queue->size++;
}

static inline Task* simple_queue_dequeue(SimpleQueue* queue) {
    // 直接使用head指针位置，实现空穴管理
    Task* task = &queue->tasks[queue->head];
    queue->head = (queue->head + 1) % queue->capacity;
    queue->size--;
    return task;
}

static inline PriorityQueue* priority_queue_create(int capacity_per_queue) {
    PriorityQueue* pq = (PriorityQueue*)fast_alloc(sizeof(PriorityQueue));
    for (int i = 0; i < PRIORITY_LEVELS; i++) {
        pq->queues[i] = simple_queue_create(capacity_per_queue);
    }
    return pq;
}

static inline void priority_queue_add(PriorityQueue* pq, int priority, 
                                     void* (*func)(void*), void* arg) {
    Task task = {func, arg, priority};
    simple_queue_enqueue(pq->queues[priority], task);
}

static inline void* priority_queue_execute_next(PriorityQueue* pq) {
    for (int i = 0; i < PRIORITY_LEVELS; i++) {
        if (!simple_queue_is_empty(pq->queues[i])) {
            Task* task = simple_queue_dequeue(pq->queues[i]);
            return task->func(task->arg);
        }
    }
    return NULL;
}

static inline int priority_queue_batch_add(PriorityQueue* pq, int priority,
                                          void* (*func)(void*), void** args,
                                          int count) {
    int added = 0;
    for (int i = 0; i < count; i++) {
        if (!simple_queue_is_full(pq->queues[priority])) {
            Task task = {func, args[i], priority};
            simple_queue_enqueue(pq->queues[priority], task);
            added++;
        }
    }
    return added;
}

static inline int priority_queue_batch_execute(PriorityQueue* pq, int max_tasks) {
    int executed = 0;
    for (int i = 0; i < max_tasks; i++) {
        if (priority_queue_execute_next(pq) == NULL) break;
        executed++;
    }
    return executed;
}