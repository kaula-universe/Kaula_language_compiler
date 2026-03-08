#include "task.h"
#include <stdlib.h>
#include <string.h>

// 任务队列函数 - 使用src目录中的高性能实现
extern SimpleQueue* simple_queue_create(int capacity);
extern int simple_queue_is_empty(SimpleQueue* queue);
extern int simple_queue_is_full(SimpleQueue* queue);
extern void simple_queue_enqueue(SimpleQueue* queue, Task task);
extern Task* simple_queue_dequeue(SimpleQueue* queue);

// 优先级队列函数 - 使用src目录中的高性能实现
extern PriorityQueue* priority_queue_create(int capacity_per_queue);
extern void priority_queue_add(PriorityQueue* pq, int priority, void* (*func)(void*), void* arg);
extern void* priority_queue_execute_next(PriorityQueue* pq);
extern int priority_queue_batch_add(PriorityQueue* pq, int priority, void* (*func)(void*), void** args, int count);
extern int priority_queue_batch_execute(PriorityQueue* pq, int max_tasks);

// 优先级级别常量
#define PRIORITY_LEVELS 3

// 任务函数
Task task_create(void* (*func)(void*), void* arg, int priority) {
    Task task;
    task.func = func;
    task.arg = arg;
    task.priority = priority;
    return task;
}

TaskQueue* task_queue_create(int capacity) {
    return (TaskQueue*)simple_queue_create(capacity);
}

void task_queue_destroy(TaskQueue* queue) {
    // 简单队列使用fast_alloc分配，不需要单独释放
    (void)queue;
}

void task_queue_enqueue(TaskQueue* queue, Task task) {
    simple_queue_enqueue((SimpleQueue*)queue, task);
}

Task* task_queue_dequeue(TaskQueue* queue) {
    return simple_queue_dequeue((SimpleQueue*)queue);
}

bool task_queue_is_empty(TaskQueue* queue) {
    return simple_queue_is_empty((SimpleQueue*)queue) != 0;
}

bool task_queue_is_full(TaskQueue* queue) {
    return simple_queue_is_full((SimpleQueue*)queue) != 0;
}

int task_queue_size(TaskQueue* queue) {
    if (queue) {
        SimpleQueue* sq = (SimpleQueue*)queue;
        return sq->size;
    }
    return 0;
}

PriorityTaskQueue* priority_task_queue_create(int capacity_per_queue) {
    return (PriorityTaskQueue*)priority_queue_create(capacity_per_queue);
}

void priority_task_queue_destroy(PriorityTaskQueue* pq) {
    // 优先级队列使用fast_alloc分配，不需要单独释放
    (void)pq;
}

void priority_task_queue_add(PriorityTaskQueue* pq, Task task) {
    priority_queue_add((PriorityQueue*)pq, task.priority, task.func, task.arg);
}

Task* priority_task_queue_execute_next(PriorityTaskQueue* pq) {
    priority_queue_execute_next((PriorityQueue*)pq);
    return NULL; // src实现不返回Task指针
}

int priority_task_queue_batch_add(PriorityTaskQueue* pq, Task* tasks, int count) {
    int added = 0;
    for (int i = 0; i < count; i++) {
        priority_queue_add((PriorityQueue*)pq, tasks[i].priority, tasks[i].func, tasks[i].arg);
        added++;
    }
    return added;
}

int priority_task_queue_batch_execute(PriorityTaskQueue* pq, int max_tasks) {
    return priority_queue_batch_execute((PriorityQueue*)pq, max_tasks);
}

bool priority_task_queue_is_empty(PriorityTaskQueue* pq) {
    if (pq) {
        PriorityQueue* pq_impl = (PriorityQueue*)pq;
        for (int i = 0; i < PRIORITY_LEVELS; i++) {
            if (!simple_queue_is_empty(pq_impl->queues[i])) {
                return false;
            }
        }
    }
    return true;
}

int priority_task_queue_size(PriorityTaskQueue* pq) {
    if (pq) {
        PriorityQueue* pq_impl = (PriorityQueue*)pq;
        int size = 0;
        for (int i = 0; i < PRIORITY_LEVELS; i++) {
            size += pq_impl->queues[i]->size;
        }
        return size;
    }
    return 0;
}
