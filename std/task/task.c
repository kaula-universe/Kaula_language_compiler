#include "task.h"
#include <stdlib.h>
#include <string.h>
#include <stdatomic.h>

// 轻量级任务节点 - 仅包含必要信息
typedef struct TaskNode {
    void* (*func)(void*);  // 任务函数
    void* arg;             // 参数
    int priority;           // 优先级
    struct TaskNode* next; // 下一个节点
} TaskNode;

// 轻量级任务队列 - 无锁设计，使用原子操作
typedef struct LightTaskQueue {
    _Atomic(TaskNode*) head;    // 头指针（原子）
    _Atomic(TaskNode*) tail;    // 尾指针（原子）
    _Atomic int count;          // 任务计数（原子）
    _Atomic int shutdown;       // 关闭标志
} LightTaskQueue;

// 创建轻量级任务队列
LightTaskQueue* light_task_queue_create(int capacity) {
    (void)capacity;  // 未使用，保留接口兼容性
    LightTaskQueue* q = (LightTaskQueue*)malloc(sizeof(LightTaskQueue));
    if (!q) return NULL;

    TaskNode* dummy = (TaskNode*)malloc(sizeof(TaskNode));
    if (!dummy) {
        free(q);
        return NULL;
    }

    dummy->func = NULL;
    dummy->arg = NULL;
    dummy->priority = 0;
    dummy->next = NULL;

    atomic_store_explicit(&q->head, dummy, memory_order_relaxed);
    atomic_store_explicit(&q->tail, dummy, memory_order_relaxed);
    atomic_store_explicit(&q->count, 0, memory_order_relaxed);
    atomic_store_explicit(&q->shutdown, 0, memory_order_relaxed);

    return q;
}

// 销毁轻量级任务队列
void light_task_queue_destroy(LightTaskQueue* q) {
    if (!q) return;

    // 释放所有节点
    TaskNode* current = atomic_load_explicit(&q->head, memory_order_relaxed);
    while (current) {
        TaskNode* next = current->next;
        free(current);
        current = next;
    }
    free(q);
}

// 添加任务到队列（无锁，线程安全）
int light_task_queue_add(LightTaskQueue* q, void* (*func)(void*), void* arg, int priority) {
    if (!q || !func || atomic_load_explicit(&q->shutdown, memory_order_relaxed)) {
        return 0;
    }

    TaskNode* node = (TaskNode*)malloc(sizeof(TaskNode));
    if (!node) return 0;

    node->func = func;
    node->arg = arg;
    node->priority = priority;
    node->next = NULL;

    // 使用原子操作入队
    TaskNode* old_tail = atomic_load_explicit(&q->tail, memory_order_relaxed);
    atomic_store_explicit(&old_tail->next, node, memory_order_relaxed);
    atomic_store_explicit(&q->tail, node, memory_order_relaxed);
    atomic_fetch_add_explicit(&q->count, 1, memory_order_relaxed);

    return 1;
}

// 获取任务（无锁，线程安全）
// 返回 NULL 表示队列空或已关闭
TaskNode* light_task_queue_dequeue(LightTaskQueue* q) {
    if (!q) return NULL;

    TaskNode* head = atomic_load_explicit(&q->head, memory_order_relaxed);
    TaskNode* next = atomic_load_explicit(&head->next, memory_order_relaxed);

    if (next == NULL) {
        return NULL;  // 队列空
    }

    // 移动头指针
    atomic_store_explicit(&q->head, next, memory_order_relaxed);
    atomic_fetch_sub_explicit(&q->count, 1, memory_order_relaxed);

    // 保留任务节点用于执行
    next->func = head->func;
    next->arg = head->arg;
    next->priority = head->priority;

    // 释放旧的头节点
    free(head);

    return next;
}

// 检查队列是否为空
int light_task_queue_is_empty(LightTaskQueue* q) {
    if (!q) return 1;
    return atomic_load_explicit(&q->count, memory_order_relaxed) == 0;
}

// 获取队列任务数
int light_task_queue_size(LightTaskQueue* q) {
    if (!q) return 0;
    return atomic_load_explicit(&q->count, memory_order_relaxed);
}

// 关闭队列（禁止新任务入队）
void light_task_queue_shutdown(LightTaskQueue* q) {
    if (!q) return;
    atomic_store_explicit(&q->shutdown, 1, memory_order_relaxed);
}

// 执行下一个任务（如果存在）
void* light_task_queue_execute_next(LightTaskQueue* q) {
    TaskNode* node = light_task_queue_dequeue(q);
    if (!node) return NULL;

    void* result = node->func(node->arg);
    free(node);
    return result;
}

// 批量执行任务
int light_task_queue_batch_execute(LightTaskQueue* q, int max_tasks) {
    int executed = 0;
    for (int i = 0; i < max_tasks; i++) {
        TaskNode* node = light_task_queue_dequeue(q);
        if (!node) break;

        node->func(node->arg);
        free(node);
        executed++;
    }
    return executed;
}

// 优先级任务队列（简化版，使用多个队列）
#define PRIORITY_LEVELS 3

typedef struct PriorityTaskQueue {
    LightTaskQueue* queues[PRIORITY_LEVELS];  // 高、中、低优先级
    _Atomic int total_count;
} PriorityTaskQueue;

// 创建优先级任务队列
PriorityTaskQueue* priority_task_queue_create(int capacity_per_queue) {
    PriorityTaskQueue* pq = (PriorityTaskQueue*)malloc(sizeof(PriorityTaskQueue));
    if (!pq) return NULL;

    for (int i = 0; i < PRIORITY_LEVELS; i++) {
        pq->queues[i] = light_task_queue_create(capacity_per_queue);
        if (!pq->queues[i]) {
            for (int j = 0; j < i; j++) {
                light_task_queue_destroy(pq->queues[j]);
            }
            free(pq);
            return NULL;
        }
    }

    atomic_store_explicit(&pq->total_count, 0, memory_order_relaxed);
    return pq;
}

// 销毁优先级任务队列
void priority_task_queue_destroy(PriorityTaskQueue* pq) {
    if (!pq) return;
    for (int i = 0; i < PRIORITY_LEVELS; i++) {
        light_task_queue_destroy(pq->queues[i]);
    }
    free(pq);
}

// 添加优先级任务
void priority_task_queue_add(PriorityTaskQueue* pq, void* (*func)(void*), void* arg, int priority) {
    if (!pq || !func) return;

    // 限制优先级范围 [0, PRIORITY_LEVELS-1]
    int p = priority;
    if (p < 0) p = 0;
    if (p >= PRIORITY_LEVELS) p = PRIORITY_LEVELS - 1;

    light_task_queue_add(pq->queues[p], func, arg, priority);
    atomic_fetch_add_explicit(&pq->total_count, 1, memory_order_relaxed);
}

// 获取下一个要执行的任务
static TaskNode* priority_dequeue_one(PriorityTaskQueue* pq) {
    for (int i = 0; i < PRIORITY_LEVELS; i++) {
        TaskNode* node = light_task_queue_dequeue(pq->queues[i]);
        if (node) {
            atomic_fetch_sub_explicit(&pq->total_count, 1, memory_order_relaxed);
            return node;
        }
    }
    return NULL;
}

// 执行下一个优先级任务
void* priority_task_queue_execute_next(PriorityTaskQueue* pq) {
    TaskNode* node = priority_dequeue_one(pq);
    if (!node) return NULL;

    void* result = node->func(node->arg);
    free(node);
    return result;
}

// 批量添加任务
int priority_task_queue_batch_add(PriorityTaskQueue* pq, void* (*func)(void*), void** args, int count, int priority) {
    if (!pq || !func) return 0;

    int added = 0;
    for (int i = 0; i < count; i++) {
        if (light_task_queue_add(pq->queues[priority], func, args[i], priority)) {
            added++;
            atomic_fetch_add_explicit(&pq->total_count, 1, memory_order_relaxed);
        }
    }
    return added;
}

// 批量执行
int priority_task_queue_batch_execute(PriorityTaskQueue* pq, int max_tasks) {
    int executed = 0;
    for (int i = 0; i < max_tasks; i++) {
        TaskNode* node = priority_dequeue_one(pq);
        if (!node) break;

        node->func(node->arg);
        free(node);
        executed++;
    }
    return executed;
}

// 检查优先级队列是否为空
int priority_task_queue_is_empty(PriorityTaskQueue* pq) {
    if (!pq) return 1;
    return atomic_load_explicit(&pq->total_count, memory_order_relaxed) == 0;
}

// 获取优先级队列大小
int priority_task_queue_size(PriorityTaskQueue* pq) {
    if (!pq) return 0;
    return atomic_load_explicit(&pq->total_count, memory_order_relaxed);
}

// 兼容性别名
typedef LightTaskQueue TaskQueue;

TaskQueue* task_queue_create(int capacity) {
    return light_task_queue_create(capacity);
}

void task_queue_destroy(TaskQueue* queue) {
    light_task_queue_destroy(queue);
}

void task_queue_enqueue(TaskQueue* queue, void* (*func)(void*), void* arg) {
    light_task_queue_add(queue, func, arg, 0);
}

void* task_queue_dequeue(TaskQueue* queue) {
    TaskNode* node = light_task_queue_dequeue(queue);
    if (!node) return NULL;
    void* result = node->func(node->arg);
    free(node);
    return result;
}

int task_queue_is_empty(TaskQueue* queue) {
    return light_task_queue_is_empty(queue);
}

int task_queue_size(TaskQueue* queue) {
    return light_task_queue_size(queue);
}
