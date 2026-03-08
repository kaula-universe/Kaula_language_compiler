#ifndef STD_ASYNC_ASYNC_H
#define STD_ASYNC_ASYNC_H

#include "../base/types.h"
#include "../concurrent/concurrent.h"

// 异步任务类型
typedef void* AsyncTask;

// 异步回调函数类型
typedef void (*AsyncCallback)(void* result, void* user_data);

// 异步任务状态
typedef enum {
    ASYNC_TASK_PENDING,
    ASYNC_TASK_RUNNING,
    ASYNC_TASK_COMPLETED,
    ASYNC_TASK_FAILED
} AsyncTaskStatus;

// 异步事件循环
typedef void* AsyncEventLoop;

// 异步I/O操作类型
typedef void* AsyncIO;

// 异步任务创建与管理
extern AsyncTask async_task_create(ThreadFunction func, void* arg, AsyncCallback callback, void* user_data);
extern void async_task_cancel(AsyncTask task);
extern AsyncTaskStatus async_task_get_status(AsyncTask task);
extern void* async_task_get_result(AsyncTask task);
extern void async_task_wait(AsyncTask task);

// 异步事件循环
extern AsyncEventLoop async_event_loop_create();
extern void async_event_loop_destroy(AsyncEventLoop loop);
extern void async_event_loop_run(AsyncEventLoop loop);
extern void async_event_loop_stop(AsyncEventLoop loop);
extern void async_event_loop_post(AsyncEventLoop loop, ThreadFunction func, void* arg);

// 异步I/O操作
extern AsyncIO async_io_create(AsyncEventLoop loop);
extern void async_io_destroy(AsyncIO io);
extern bool async_io_read(AsyncIO io, int fd, void* buffer, size_t size, AsyncCallback callback, void* user_data);
extern bool async_io_write(AsyncIO io, int fd, const void* buffer, size_t size, AsyncCallback callback, void* user_data);
extern bool async_io_connect(AsyncIO io, const char* host, int port, AsyncCallback callback, void* user_data);
extern bool async_io_accept(AsyncIO io, int listen_fd, AsyncCallback callback, void* user_data);

// 异步定时器
extern bool async_timer_set(AsyncEventLoop loop, uint64_t timeout_ms, AsyncCallback callback, void* user_data);

// 协程支持（简单实现）
typedef void* Coroutine;
typedef void (*CoroutineFunction)(void* arg);

extern Coroutine coroutine_create(CoroutineFunction func, void* arg);
extern void coroutine_resume(Coroutine coro);
extern void coroutine_yield();
extern void coroutine_destroy(Coroutine coro);

#endif // STD_ASYNC_ASYNC_H