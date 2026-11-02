#ifndef STD_ASYNC_ASYNC_H
#define STD_ASYNC_ASYNC_H

#include "../base/types.h"
#include <stdatomic.h>

// 鍓嶅悜澹版槑
typedef struct AsyncNode AsyncNode;
typedef struct LightAsyncLoop LightAsyncLoop;
typedef struct LightTimerManager LightTimerManager;

// 寮傛浠诲姟鐘舵€?
typedef enum {
    ASYNC_TASK_PENDING = 0,
    ASYNC_TASK_RUNNING = 1,
    ASYNC_TASK_COMPLETED = 2,
    ASYNC_TASK_CANCELLED = 3
} AsyncTaskStatus;

// 杞婚噺绾у紓姝ヤ簨浠跺惊鐜?API
LightAsyncLoop* light_async_loop_create(void);
void light_async_loop_destroy(LightAsyncLoop* loop);
int light_async_loop_add(LightAsyncLoop* loop, void (*func)(void*), void* arg);
int light_async_loop_poll(LightAsyncLoop* loop);
int light_async_loop_batch_poll(LightAsyncLoop* loop, int max_count);
void light_async_loop_start(LightAsyncLoop* loop);
void light_async_loop_stop(LightAsyncLoop* loop);
int light_async_loop_size(LightAsyncLoop* loop);
int light_async_loop_is_running(LightAsyncLoop* loop);

// 寮傛浠诲姟鑺傜偣 API
int light_async_node_status(AsyncNode* node);
void* light_async_node_result(AsyncNode* node);

// 杞婚噺绾у畾鏃跺櫒绠＄悊鍣?API
LightTimerManager* light_timer_manager_create(void);
void light_timer_manager_destroy(LightTimerManager* mgr);
int light_timer_manager_add(LightTimerManager* mgr, uint64_t timeout_ms, void (*callback)(void*), void* arg);
int light_timer_manager_poll(LightTimerManager* mgr, uint64_t current_time);

// 鍏煎鎬у埆鍚?
typedef LightAsyncLoop AsyncEventLoop;
typedef LightTimerManager AsyncTimerManager;

// 鍏煎鏃?API
AsyncEventLoop* async_event_loop_create(void);
void async_event_loop_destroy(AsyncEventLoop* loop);
int async_event_loop_add(AsyncEventLoop* loop, void (*func)(void*), void* arg);
int async_event_loop_poll(AsyncEventLoop* loop);
int async_event_loop_batch_poll(AsyncEventLoop* loop, int max_count);
void async_event_loop_start(AsyncEventLoop* loop);
void async_event_loop_stop(AsyncEventLoop* loop);
int async_event_loop_size(AsyncEventLoop* loop);

// 鍏煎鏃?API
void async_event_loop_run(AsyncEventLoop* loop);
void async_event_loop_post(AsyncEventLoop* loop, void (*func)(void*), void* arg);

#endif // STD_ASYNC_ASYNC_H
