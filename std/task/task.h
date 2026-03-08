#ifndef STD_TASK_TASK_H
#define STD_TASK_TASK_H

#include "../base/types.h"
#include "../../src/kaula.h"

// 任务类型 - 使用src目录中的高性能实现

// 任务队列结构 - 使用src目录中的高性能实现
typedef struct SimpleQueue TaskQueue;
typedef struct PriorityQueue PriorityTaskQueue;

// 任务函数
extern Task task_create(void* (*func)(void*), void* arg, int priority);
extern TaskQueue* task_queue_create(int capacity);
extern void task_queue_destroy(TaskQueue* queue);
extern void task_queue_enqueue(TaskQueue* queue, Task task);
extern Task* task_queue_dequeue(TaskQueue* queue);
extern bool task_queue_is_empty(TaskQueue* queue);
extern bool task_queue_is_full(TaskQueue* queue);
extern int task_queue_size(TaskQueue* queue);

extern PriorityTaskQueue* priority_task_queue_create(int capacity_per_queue);
extern void priority_task_queue_destroy(PriorityTaskQueue* pq);
extern void priority_task_queue_add(PriorityTaskQueue* pq, Task task);
extern Task* priority_task_queue_execute_next(PriorityTaskQueue* pq);
extern int priority_task_queue_batch_add(PriorityTaskQueue* pq, Task* tasks, int count);
extern int priority_task_queue_batch_execute(PriorityTaskQueue* pq, int max_tasks);
extern bool priority_task_queue_is_empty(PriorityTaskQueue* pq);
extern int priority_task_queue_size(PriorityTaskQueue* pq);

#endif // STD_TASK_TASK_H