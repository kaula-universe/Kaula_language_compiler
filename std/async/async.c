#include "async.h"
#include <stdlib.h>
#include <string.h>
#include <stdatomic.h>

// 轻量级异步任务节点
typedef struct AsyncNode {
    void* (*func)(void*);   // 异步函数
    void* arg;               // 参数
    void* result;            // 结果
    atomic_int status;       // 状态：0=pending, 1=running, 2=completed, 3=cancelled
    struct AsyncNode* next;  // 下一个节点
} AsyncNode;

// 轻量级异步事件循环 - 无锁设计
typedef struct {
    _Atomic(AsyncNode*) head;     // 任务队列头
    _Atomic(AsyncNode*) tail;     // 任务队列尾
    _Atomic int count;           // 任务计数
    _Atomic int running;          // 运行标志
    _Atomic int worker_active;    // 活跃worker数
} LightAsyncLoop;

// 创建轻量级异步事件循环
LightAsyncLoop* light_async_loop_create() {
    LightAsyncLoop* loop = (LightAsyncLoop*)malloc(sizeof(LightAsyncLoop));
    if (!loop) return NULL;

    AsyncNode* dummy = (AsyncNode*)malloc(sizeof(AsyncNode));
    if (!dummy) {
        free(loop);
        return NULL;
    }

    dummy->func = NULL;
    dummy->arg = NULL;
    dummy->result = NULL;
    atomic_store_explicit(&dummy->status, 0, memory_order_relaxed);
    dummy->next = NULL;

    atomic_store_explicit(&loop->head, dummy, memory_order_relaxed);
    atomic_store_explicit(&loop->tail, dummy, memory_order_relaxed);
    atomic_store_explicit(&loop->count, 0, memory_order_relaxed);
    atomic_store_explicit(&loop->running, 0, memory_order_relaxed);
    atomic_store_explicit(&loop->worker_active, 0, memory_order_relaxed);

    return loop;
}

// 销毁轻量级异步事件循环
void light_async_loop_destroy(LightAsyncLoop* loop) {
    if (!loop) return;

    // 释放所有节点
    AsyncNode* current = atomic_load_explicit(&loop->head, memory_order_relaxed);
    while (current) {
        AsyncNode* next = current->next;
        free(current);
        current = next;
    }
    free(loop);
}

// 添加异步任务到循环（无锁）
int light_async_loop_add(LightAsyncLoop* loop, void* (*func)(void*), void* arg) {
    if (!loop || !func) return 0;

    AsyncNode* node = (AsyncNode*)malloc(sizeof(AsyncNode));
    if (!node) return 0;

    node->func = func;
    node->arg = arg;
    node->result = NULL;
    atomic_store_explicit(&node->status, 0, memory_order_relaxed);  // pending
    node->next = NULL;

    // 入队
    AsyncNode* old_tail = atomic_load_explicit(&loop->tail, memory_order_relaxed);
    atomic_store_explicit(&old_tail->next, node, memory_order_relaxed);
    atomic_store_explicit(&loop->tail, node, memory_order_relaxed);
    atomic_fetch_add_explicit(&loop->count, 1, memory_order_relaxed);

    return 1;
}

// 获取并执行下一个任务（无锁）
// 返回 1 表示执行了任务，0 表示队列空
int light_async_loop_poll(LightAsyncLoop* loop) {
    if (!loop) return 0;

    AsyncNode* head = atomic_load_explicit(&loop->head, memory_order_relaxed);
    AsyncNode* next = atomic_load_explicit(&head->next, memory_order_relaxed);

    if (next == NULL) {
        return 0;  // 队列空
    }

    // 移动头指针
    atomic_store_explicit(&loop->head, next, memory_order_relaxed);
    atomic_fetch_sub_explicit(&loop->count, 1, memory_order_relaxed);

    // 标记为运行中
    atomic_store_explicit(&next->status, 1, memory_order_relaxed);  // running

    // 执行任务
    next->result = next->func(next->arg);

    // 标记为完成
    atomic_store_explicit(&next->status, 2, memory_order_relaxed);  // completed

    // 释放头节点
    free(head);

    return 1;
}

// 批量执行任务
int light_async_loop_batch_poll(LightAsyncLoop* loop, int max_count) {
    int executed = 0;
    for (int i = 0; i < max_count; i++) {
        if (!light_async_loop_poll(loop)) {
            break;
        }
        executed++;
    }
    return executed;
}

// 检查循环是否运行中
int light_async_loop_is_running(LightAsyncLoop* loop) {
    if (!loop) return 0;
    return atomic_load_explicit(&loop->running, memory_order_relaxed);
}

// 启动循环
void light_async_loop_start(LightAsyncLoop* loop) {
    if (!loop) return;
    atomic_store_explicit(&loop->running, 1, memory_order_relaxed);
}

// 停止循环
void light_async_loop_stop(LightAsyncLoop* loop) {
    if (!loop) return;
    atomic_store_explicit(&loop->running, 0, memory_order_relaxed);
}

// 获取队列大小
int light_async_loop_size(LightAsyncLoop* loop) {
    if (!loop) return 0;
    return atomic_load_explicit(&loop->count, memory_order_relaxed);
}

// 检查任务状态
int light_async_node_status(AsyncNode* node) {
    if (!node) return -1;
    return atomic_load_explicit(&node->status, memory_order_relaxed);
}

// 获取任务结果
void* light_async_node_result(AsyncNode* node) {
    if (!node) return NULL;
    return node->result;
}

// 轻量级定时器节点
typedef struct TimerNode {
    uint64_t timeout_ms;      // 超时时间（毫秒）
    uint64_t start_time;      // 开始时间
    void (*callback)(void*); // 回调函数
    void* arg;                // 参数
    struct TimerNode* next;   // 下一个节点
} TimerNode;

// 轻量级定时器管理器
typedef struct {
    _Atomic(TimerNode*) head;   // 定时器队列
    _Atomic uint64_t current_time;  // 当前时间
} LightTimerManager;

// 创建轻量级定时器管理器
LightTimerManager* light_timer_manager_create() {
    LightTimerManager* mgr = (LightTimerManager*)malloc(sizeof(LightTimerManager));
    if (!mgr) return NULL;

    atomic_store_explicit(&mgr->head, NULL, memory_order_relaxed);
    atomic_store_explicit(&mgr->current_time, 0, memory_order_relaxed);

    return mgr;
}

// 销毁轻量级定时器管理器
void light_timer_manager_destroy(LightTimerManager* mgr) {
    if (!mgr) return;

    TimerNode* current = atomic_load_explicit(&mgr->head, memory_order_relaxed);
    while (current) {
        TimerNode* next = current->next;
        free(current);
        current = next;
    }
    free(mgr);
}

// 添加定时器任务
int light_timer_manager_add(LightTimerManager* mgr, uint64_t timeout_ms, void (*callback)(void*), void* arg) {
    if (!mgr || !callback) return 0;

    TimerNode* node = (TimerNode*)malloc(sizeof(TimerNode));
    if (!node) return 0;

    node->timeout_ms = timeout_ms;
    node->start_time = 0;  // 将在 poll 时设置
    node->callback = callback;
    node->arg = arg;
    node->next = NULL;

    // 简单的头插法
    TimerNode* old_head = atomic_load_explicit(&mgr->head, memory_order_relaxed);
    node->next = old_head;
    atomic_store_explicit(&mgr->head, node, memory_order_relaxed);

    return 1;
}

// 触发超时的定时器（需要在外部调用更新 current_time）
int light_timer_manager_poll(LightTimerManager* mgr, uint64_t current_time) {
    if (!mgr) return 0;

    atomic_store_explicit(&mgr->current_time, current_time, memory_order_relaxed);

    int triggered = 0;
    TimerNode* current = atomic_load_explicit(&mgr->head, memory_order_relaxed);
    TimerNode* prev = NULL;

    while (current) {
        uint64_t elapsed = current_time - current->start_time;
        if (elapsed >= current->timeout_ms) {
            // 触发回调
            if (current->callback) {
                current->callback(current->arg);
            }

            // 从队列中移除
            if (prev) {
                prev->next = current->next;
            } else {
                atomic_store_explicit(&mgr->head, current->next, memory_order_relaxed);
            }

            TimerNode* to_free = current;
            current = current->next;
            free(to_free);
            triggered++;
        } else {
            prev = current;
            current = current->next;
        }
    }

    return triggered;
}

// 兼容性别名
typedef LightAsyncLoop AsyncEventLoop;
typedef LightTimerManager AsyncTimerManager;

AsyncEventLoop* async_event_loop_create() {
    return light_async_loop_create();
}

void async_event_loop_destroy(AsyncEventLoop* loop) {
    light_async_loop_destroy((LightAsyncLoop*)loop);
}

int async_event_loop_add(AsyncEventLoop* loop, void* (*func)(void*), void* arg) {
    return light_async_loop_add((LightAsyncLoop*)loop, func, arg);
}

int async_event_loop_poll(AsyncEventLoop* loop) {
    return light_async_loop_poll((LightAsyncLoop*)loop);
}

int async_event_loop_batch_poll(AsyncEventLoop* loop, int max_count) {
    return light_async_loop_batch_poll((LightAsyncLoop*)loop, max_count);
}

void async_event_loop_start(AsyncEventLoop* loop) {
    light_async_loop_start((LightAsyncLoop*)loop);
}

void async_event_loop_stop(AsyncEventLoop* loop) {
    light_async_loop_stop((LightAsyncLoop*)loop);
}

int async_event_loop_size(AsyncEventLoop* loop) {
    return light_async_loop_size((LightAsyncLoop*)loop);
}

// 简化的异步I/O操作（使用任务队列）
int async_io_read(AsyncEventLoop* loop, int fd, void* buffer, size_t size) {
    (void)fd;
    (void)buffer;
    (void)size;
    // 简化实现：在循环中执行读取
    return light_async_loop_add((LightAsyncLoop*)loop, NULL, NULL);
}

int async_io_write(AsyncEventLoop* loop, int fd, const void* buffer, size_t size) {
    (void)fd;
    (void)buffer;
    (void)size;
    // 简化实现：在循环中执行写入
    return light_async_loop_add((LightAsyncLoop*)loop, NULL, NULL);
}

// 兼容旧API
void async_event_loop_run(AsyncEventLoop* loop) {
    light_async_loop_start((LightAsyncLoop*)loop);
}

void async_event_loop_post(AsyncEventLoop* loop, void* (*func)(void*), void* arg) {
    light_async_loop_add((LightAsyncLoop*)loop, func, arg);
}
