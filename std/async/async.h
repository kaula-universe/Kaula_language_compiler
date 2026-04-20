#ifndef STD_ASYNC_ASYNC_H
#define STD_ASYNC_ASYNC_H

#include "../base/types.h"
#include <stdatomic.h>

// 前向声明
typedef struct AsyncNode AsyncNode;
typedef struct LightAsyncLoop LightAsyncLoop;
typedef struct LightTimerManager LightTimerManager;

// 异步任务状态
typedef enum {
    ASYNC_TASK_PENDING = 0,
    ASYNC_TASK_RUNNING = 1,
    ASYNC_TASK_COMPLETED = 2,
    ASYNC_TASK_CANCELLED = 3
} AsyncTaskStatus;

// 轻量级异步事件循环 API
LightAsyncLoop* light_async_loop_create(void);
void light_async_loop_destroy(LightAsyncLoop* loop);
int light_async_loop_add(LightAsyncLoop* loop, void* (*func)(void*), void* arg);
int light_async_loop_poll(LightAsyncLoop* loop);
int light_async_loop_batch_poll(LightAsyncLoop* loop, int max_count);
void light_async_loop_start(LightAsyncLoop* loop);
void light_async_loop_stop(LightAsyncLoop* loop);
int light_async_loop_size(LightAsyncLoop* loop);
int light_async_loop_is_running(LightAsyncLoop* loop);

// 异步任务节点 API
int light_async_node_status(AsyncNode* node);
void* light_async_node_result(AsyncNode* node);

// 轻量级定时器管理器 API
LightTimerManager* light_timer_manager_create(void);
void light_timer_manager_destroy(LightTimerManager* mgr);
int light_timer_manager_add(LightTimerManager* mgr, uint64_t timeout_ms, void (*callback)(void*), void* arg);
int light_timer_manager_poll(LightTimerManager* mgr, uint64_t current_time);

// 兼容性别名
typedef LightAsyncLoop AsyncEventLoop;
typedef LightTimerManager AsyncTimerManager;

// 兼容旧 API
AsyncEventLoop* async_event_loop_create(void);
void async_event_loop_destroy(AsyncEventLoop* loop);
int async_event_loop_add(AsyncEventLoop* loop, void* (*func)(void*), void* arg);
int async_event_loop_poll(AsyncEventLoop* loop);
int async_event_loop_batch_poll(AsyncEventLoop* loop, int max_count);
void async_event_loop_start(AsyncEventLoop* loop);
void async_event_loop_stop(AsyncEventLoop* loop);
int async_event_loop_size(AsyncEventLoop* loop);

// 兼容旧 API
void async_event_loop_run(AsyncEventLoop* loop);
void async_event_loop_post(AsyncEventLoop* loop, void* (*func)(void*), void* arg);

#endif // STD_ASYNC_ASYNC_H
