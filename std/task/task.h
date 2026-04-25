#ifndef STD_TASK_TASK_H
#define STD_TASK_TASK_H

#include "../base/types.h"
#include "../../src/kaula.h"
#include <stdatomic.h>

// 轻量级任务节点 - 仅包含必要信息
typedef struct TaskNode TaskNode;

// 轻量级任务队列 - 无锁设计，使用原子操作
typedef struct LightTaskQueue LightTaskQueue;

// TaskParam 结构体 - 用于 task(优先级) 语法
typedef struct TaskParam {
    int priority;    // 优先级
    void* data;      // 任务数据
} TaskParam;

// 轻量级任务队列 API
LightTaskQueue* light_task_queue_create(int capacity);
void light_task_queue_destroy(LightTaskQueue* q);
int light_task_queue_add(LightTaskQueue* q, void* (*func)(void*), void* arg, int priority);
int light_task_queue_is_empty(LightTaskQueue* q);
int light_task_queue_size(LightTaskQueue* q);
void* light_task_queue_execute_next(LightTaskQueue* q);
int light_task_queue_batch_execute(LightTaskQueue* q, int max_tasks);

// 兼容性别名
typedef LightTaskQueue TaskQueue;
TaskQueue* task_queue_create(int capacity);
void task_queue_destroy(TaskQueue* queue);
void task_queue_enqueue(TaskQueue* queue, void* (*func)(void*), void* arg);
void* task_queue_dequeue(TaskQueue* queue);
int task_queue_is_empty(TaskQueue* queue);
int task_queue_size(TaskQueue* queue);

#endif // STD_TASK_TASK_H
