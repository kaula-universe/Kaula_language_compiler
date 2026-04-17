#include "kaula.h"

// ==================== 跨平台优先级队列实现 ====================

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
        // 直接使用 tail 指针位置，实现空穴管理
        queue->tasks[queue->tail] = task;
        queue->tail = (queue->tail + 1) % queue->capacity;
        queue->size++;
    }
}

static inline Task* simple_queue_dequeue(SimpleQueue* queue) {
    if (!simple_queue_is_empty(queue)) {
        // 直接使用 head 指针位置，实现空穴管理
        Task* task = &queue->tasks[queue->head];
        queue->head = (queue->head + 1) % queue->capacity;
        queue->size--;
        return task;
    }
    return NULL;
}

static inline PriorityQueue* priority_queue_create(int capacity_per_queue) {
    PriorityQueue* pq = (PriorityQueue*)fast_alloc(sizeof(PriorityQueue));
    if (!pq) return NULL;
    
    for (int i = 0; i < PRIORITY_LEVELS; i++) {
        pq->queues[i] = simple_queue_create(capacity_per_queue);
        if (!pq->queues[i]) {
            // 清理已分配的队列
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

static inline void priority_queue_add(PriorityQueue* pq, int priority, 
                                     void* (*func)(void*), void* arg) {
    if (priority < 0 || priority >= PRIORITY_LEVELS) {
        priority = 0; // 默认优先级
    }
    Task task = {func, arg, priority};
    simple_queue_enqueue(pq->queues[priority], task);
}

static inline void* priority_queue_execute_next(PriorityQueue* pq) {
    // 从高优先级到低优先级依次检查
    for (int i = PRIORITY_LEVELS - 1; i >= 0; i--) {
        if (!simple_queue_is_empty(pq->queues[i])) {
            Task* task = simple_queue_dequeue(pq->queues[i]);
            if (task && task->func) {
                return task->func(task->arg);
            }
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
    while (executed < max_tasks) {
        void* result = priority_queue_execute_next(pq);
        if (!result) break;
        executed++;
        (void)result; // 避免未使用变量警告
    }
    return executed;
}
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