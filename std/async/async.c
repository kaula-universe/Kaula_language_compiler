#ifdef _WIN32
// 确保只包含winsock2.h，避免与winsock.h冲突
#define WIN32_LEAN_AND_MEAN
#endif

#include "async.h"
#include "../memory/memory.h"
#include "../error/error.h"

#ifdef _WIN32
#include <windows.h>
#include <winsock2.h>
#include <ws2tcpip.h>
#pragma comment(lib, "ws2_32.lib")
#else
#include <unistd.h>
#include <fcntl.h>
#include <sys/epoll.h>
#include <sys/socket.h>
#include <netinet/in.h>
#include <arpa/inet.h>
#endif

// 异步任务结构体
typedef struct {
    Thread thread;
    ThreadFunction func;
    void* arg;
    AsyncCallback callback;
    void* user_data;
    void* result;
    AsyncTaskStatus status;
    Mutex mutex;
    Condition condition;
} AsyncTaskImpl;

// 异步事件循环结构体
typedef struct {
    ThreadPool thread_pool;
    bool running;
    Mutex mutex;
    Condition condition;
} AsyncEventLoopImpl;

// 异步I/O结构体
typedef struct {
    AsyncEventLoop loop;
    #ifdef _WIN32
    WSAEVENT event;
    #else
    int epoll_fd;
    #endif
} AsyncIOImpl;

// 协程结构体（简单实现）
typedef struct {
    CoroutineFunction func;
    void* arg;
    bool running;
} CoroutineImpl;

// 包装函数，执行任务并调用回调
static void* task_wrapper(void* task_ptr) {
    AsyncTaskImpl* task = (AsyncTaskImpl*)task_ptr;
    
    mutex_lock(task->mutex);
    task->status = ASYNC_TASK_RUNNING;
    mutex_unlock(task->mutex);
    
    // 执行任务
    task->result = task->func(task->arg);
    
    mutex_lock(task->mutex);
    task->status = ASYNC_TASK_COMPLETED;
    mutex_unlock(task->mutex);
    
    // 调用回调
    if (task->callback) {
        task->callback(task->result, task->user_data);
    }
    
    condition_signal(task->condition);
    return NULL;
}

// 异步任务创建与管理
AsyncTask async_task_create(ThreadFunction func, void* arg, AsyncCallback callback, void* user_data) {
    AsyncTaskImpl* task = (AsyncTaskImpl*)std_malloc(sizeof(AsyncTaskImpl));
    if (!task) {
        return NULL;
    }
    
    task->func = func;
    task->arg = arg;
    task->callback = callback;
    task->user_data = user_data;
    task->result = NULL;
    task->status = ASYNC_TASK_PENDING;
    task->mutex = mutex_create();
    task->condition = condition_create();
    
    task->thread = thread_create(task_wrapper, task);
    if (!task->thread) {
        std_free(task);
        return NULL;
    }
    
    return (AsyncTask)task;
}

void async_task_cancel(AsyncTask task) {
    if (!task) return;
    
    AsyncTaskImpl* impl = (AsyncTaskImpl*)task;
    mutex_lock(impl->mutex);
    if (impl->status == ASYNC_TASK_PENDING) {
        impl->status = ASYNC_TASK_FAILED;
    }
    mutex_unlock(impl->mutex);
}

AsyncTaskStatus async_task_get_status(AsyncTask task) {
    if (!task) return ASYNC_TASK_FAILED;
    
    AsyncTaskImpl* impl = (AsyncTaskImpl*)task;
    mutex_lock(impl->mutex);
    AsyncTaskStatus status = impl->status;
    mutex_unlock(impl->mutex);
    
    return status;
}

void* async_task_get_result(AsyncTask task) {
    if (!task) return NULL;
    
    AsyncTaskImpl* impl = (AsyncTaskImpl*)task;
    mutex_lock(impl->mutex);
    void* result = impl->result;
    mutex_unlock(impl->mutex);
    
    return result;
}

void async_task_wait(AsyncTask task) {
    if (!task) return;
    
    AsyncTaskImpl* impl = (AsyncTaskImpl*)task;
    mutex_lock(impl->mutex);
    while (impl->status == ASYNC_TASK_PENDING || impl->status == ASYNC_TASK_RUNNING) {
        condition_wait(impl->condition, impl->mutex);
    }
    mutex_unlock(impl->mutex);
    
    thread_join(impl->thread);
    
    // 清理资源
    mutex_destroy(impl->mutex);
    condition_destroy(impl->condition);
    std_free(impl);
}

// 异步事件循环
AsyncEventLoop async_event_loop_create() {
    AsyncEventLoopImpl* loop = (AsyncEventLoopImpl*)std_malloc(sizeof(AsyncEventLoopImpl));
    if (!loop) {
        return NULL;
    }
    
    loop->thread_pool = thread_pool_create(4); // 默认4个线程
    loop->running = false;
    loop->mutex = mutex_create();
    loop->condition = condition_create();
    
    return (AsyncEventLoop)loop;
}

void async_event_loop_destroy(AsyncEventLoop loop) {
    if (!loop) return;
    
    AsyncEventLoopImpl* impl = (AsyncEventLoopImpl*)loop;
    async_event_loop_stop(loop);
    thread_pool_destroy(impl->thread_pool);
    mutex_destroy(impl->mutex);
    condition_destroy(impl->condition);
    std_free(impl);
}

void async_event_loop_run(AsyncEventLoop loop) {
    if (!loop) return;
    
    AsyncEventLoopImpl* impl = (AsyncEventLoopImpl*)loop;
    mutex_lock(impl->mutex);
    impl->running = true;
    mutex_unlock(impl->mutex);
    
    // 事件循环
    while (impl->running) {
        concurrent_sleep(10); // 避免忙等
    }
}

void async_event_loop_stop(AsyncEventLoop loop) {
    if (!loop) return;
    
    AsyncEventLoopImpl* impl = (AsyncEventLoopImpl*)loop;
    mutex_lock(impl->mutex);
    impl->running = false;
    mutex_unlock(impl->mutex);
    condition_signal(impl->condition);
}

void async_event_loop_post(AsyncEventLoop loop, ThreadFunction func, void* arg) {
    if (!loop) return;
    
    AsyncEventLoopImpl* impl = (AsyncEventLoopImpl*)loop;
    
    // 创建一个简单的任务
    Task task;
    task.func = func;
    task.arg = arg;
    task.priority = 0;
    
    thread_pool_add_task(impl->thread_pool, task);
}

// 异步I/O操作
AsyncIO async_io_create(AsyncEventLoop loop) {
    if (!loop) return NULL;
    
    AsyncIOImpl* io = (AsyncIOImpl*)std_malloc(sizeof(AsyncIOImpl));
    if (!io) {
        return NULL;
    }
    
    io->loop = loop;
    
    #ifdef _WIN32
    io->event = WSACreateEvent();
    if (io->event == WSA_INVALID_EVENT) {
        std_free(io);
        return NULL;
    }
    #else
    io->epoll_fd = epoll_create1(0);
    if (io->epoll_fd < 0) {
        std_free(io);
        return NULL;
    }
    #endif
    
    return (AsyncIO)io;
}

void async_io_destroy(AsyncIO io) {
    if (!io) return;
    
    AsyncIOImpl* impl = (AsyncIOImpl*)io;
    
    #ifdef _WIN32
    WSACloseEvent(impl->event);
    #else
    close(impl->epoll_fd);
    #endif
    
    std_free(impl);
}

// 读取任务函数
static void* read_task(void* arg) {
    struct {
        int fd;
        void* buffer;
        size_t size;
        AsyncCallback callback;
        void* user_data;
    }* params = (void*)arg;
    
    ssize_t bytes_read = 0;
    #ifdef _WIN32
    bytes_read = recv(params->fd, params->buffer, params->size, 0);
    #else
    bytes_read = read(params->fd, params->buffer, params->size);
    #endif
    
    if (params->callback) {
        params->callback((void*)(intptr_t)bytes_read, params->user_data);
    }
    
    std_free(params);
    return NULL;
}

bool async_io_read(AsyncIO io, int fd, void* buffer, size_t size, AsyncCallback callback, void* user_data) {
    if (!io) return false;
    
    // 简单实现：在线程池中执行同步读取
    AsyncEventLoopImpl* loop = (AsyncEventLoopImpl*)((AsyncIOImpl*)io)->loop;
    
    void* params = std_malloc(sizeof(struct {
        int fd;
        void* buffer;
        size_t size;
        AsyncCallback callback;
        void* user_data;
    }));
    if (!params) return false;
    
    ((struct {
        int fd;
        void* buffer;
        size_t size;
        AsyncCallback callback;
        void* user_data;
    }*)params)->fd = fd;
    ((struct {
        int fd;
        void* buffer;
        size_t size;
        AsyncCallback callback;
        void* user_data;
    }*)params)->buffer = buffer;
    ((struct {
        int fd;
        void* buffer;
        size_t size;
        AsyncCallback callback;
        void* user_data;
    }*)params)->size = size;
    ((struct {
        int fd;
        void* buffer;
        size_t size;
        AsyncCallback callback;
        void* user_data;
    }*)params)->callback = callback;
    ((struct {
        int fd;
        void* buffer;
        size_t size;
        AsyncCallback callback;
        void* user_data;
    }*)params)->user_data = user_data;
    
    Task task;
    task.func = read_task;
    task.arg = params;
    task.priority = 0;
    
    thread_pool_add_task(loop->thread_pool, task);
    return true;
}

// 写入任务函数
static void* write_task(void* arg) {
    struct {
        int fd;
        const void* buffer;
        size_t size;
        AsyncCallback callback;
        void* user_data;
    }* params = (void*)arg;
    
    ssize_t bytes_written = 0;
    #ifdef _WIN32
    bytes_written = send(params->fd, params->buffer, params->size, 0);
    #else
    bytes_written = write(params->fd, params->buffer, params->size);
    #endif
    
    if (params->callback) {
        params->callback((void*)(intptr_t)bytes_written, params->user_data);
    }
    
    std_free(params);
    return NULL;
}

bool async_io_write(AsyncIO io, int fd, const void* buffer, size_t size, AsyncCallback callback, void* user_data) {
    if (!io) return false;
    
    // 简单实现：在线程池中执行同步写入
    AsyncEventLoopImpl* loop = (AsyncEventLoopImpl*)((AsyncIOImpl*)io)->loop;
    
    void* params = std_malloc(sizeof(struct {
        int fd;
        const void* buffer;
        size_t size;
        AsyncCallback callback;
        void* user_data;
    }));
    if (!params) return false;
    
    ((struct {
        int fd;
        const void* buffer;
        size_t size;
        AsyncCallback callback;
        void* user_data;
    }*)params)->fd = fd;
    ((struct {
        int fd;
        const void* buffer;
        size_t size;
        AsyncCallback callback;
        void* user_data;
    }*)params)->buffer = buffer;
    ((struct {
        int fd;
        const void* buffer;
        size_t size;
        AsyncCallback callback;
        void* user_data;
    }*)params)->size = size;
    ((struct {
        int fd;
        const void* buffer;
        size_t size;
        AsyncCallback callback;
        void* user_data;
    }*)params)->callback = callback;
    ((struct {
        int fd;
        const void* buffer;
        size_t size;
        AsyncCallback callback;
        void* user_data;
    }*)params)->user_data = user_data;
    
    Task task;
    task.func = write_task;
    task.arg = params;
    task.priority = 0;
    
    thread_pool_add_task(loop->thread_pool, task);
    return true;
}

// 连接任务函数
static void* connect_task(void* arg) {
    struct {
        const char* host;
        int port;
        AsyncCallback callback;
        void* user_data;
    }* params = (void*)arg;
    
    int sockfd = -1;
    #ifdef _WIN32
    WSADATA wsaData;
    if (WSAStartup(MAKEWORD(2, 2), &wsaData) != 0) {
        if (params->callback) {
            params->callback((void*)-1, params->user_data);
        }
        std_free(params);
        return NULL;
    }
    #endif
    
    sockfd = socket(AF_INET, SOCK_STREAM, 0);
    if (sockfd < 0) {
        if (params->callback) {
            params->callback((void*)-1, params->user_data);
        }
        #ifdef _WIN32
        WSACleanup();
        #endif
        std_free(params);
        return NULL;
    }
    
    struct sockaddr_in addr;
    addr.sin_family = AF_INET;
    addr.sin_port = htons(params->port);
    addr.sin_addr.s_addr = inet_addr(params->host);
    
    int result = connect(sockfd, (struct sockaddr*)&addr, sizeof(addr));
    if (result < 0) {
        #ifdef _WIN32
        closesocket(sockfd);
        WSACleanup();
        #else
        close(sockfd);
        #endif
        if (params->callback) {
            params->callback((void*)-1, params->user_data);
        }
        std_free(params);
        return NULL;
    }
    
    if (params->callback) {
        params->callback((void*)(intptr_t)sockfd, params->user_data);
    }
    
    #ifdef _WIN32
    WSACleanup();
    #endif
    std_free(params);
    return NULL;
}

bool async_io_connect(AsyncIO io, const char* host, int port, AsyncCallback callback, void* user_data) {
    if (!io) return false;
    
    // 简单实现：在线程池中执行同步连接
    AsyncEventLoopImpl* loop = (AsyncEventLoopImpl*)((AsyncIOImpl*)io)->loop;
    
    void* params = std_malloc(sizeof(struct {
        const char* host;
        int port;
        AsyncCallback callback;
        void* user_data;
    }));
    if (!params) return false;
    
    ((struct {
        const char* host;
        int port;
        AsyncCallback callback;
        void* user_data;
    }*)params)->host = host;
    ((struct {
        const char* host;
        int port;
        AsyncCallback callback;
        void* user_data;
    }*)params)->port = port;
    ((struct {
        const char* host;
        int port;
        AsyncCallback callback;
        void* user_data;
    }*)params)->callback = callback;
    ((struct {
        const char* host;
        int port;
        AsyncCallback callback;
        void* user_data;
    }*)params)->user_data = user_data;
    
    Task task;
    task.func = connect_task;
    task.arg = params;
    task.priority = 0;
    
    thread_pool_add_task(loop->thread_pool, task);
    return true;
}

// 接受连接任务函数
static void* accept_task(void* arg) {
    struct {
        int listen_fd;
        AsyncCallback callback;
        void* user_data;
    }* params = (void*)arg;
    
    struct sockaddr_in client_addr;
    socklen_t client_len = sizeof(client_addr);
    int client_fd = accept(params->listen_fd, (struct sockaddr*)&client_addr, &client_len);
    
    if (params->callback) {
        params->callback((void*)(intptr_t)client_fd, params->user_data);
    }
    
    std_free(params);
    return NULL;
}

bool async_io_accept(AsyncIO io, int listen_fd, AsyncCallback callback, void* user_data) {
    if (!io) return false;
    
    // 简单实现：在线程池中执行同步接受连接
    AsyncEventLoopImpl* loop = (AsyncEventLoopImpl*)((AsyncIOImpl*)io)->loop;
    
    void* params = std_malloc(sizeof(struct {
        int listen_fd;
        AsyncCallback callback;
        void* user_data;
    }));
    if (!params) return false;
    
    ((struct {
        int listen_fd;
        AsyncCallback callback;
        void* user_data;
    }*)params)->listen_fd = listen_fd;
    ((struct {
        int listen_fd;
        AsyncCallback callback;
        void* user_data;
    }*)params)->callback = callback;
    ((struct {
        int listen_fd;
        AsyncCallback callback;
        void* user_data;
    }*)params)->user_data = user_data;
    
    Task task;
    task.func = accept_task;
    task.arg = params;
    task.priority = 0;
    
    thread_pool_add_task(loop->thread_pool, task);
    return true;
}

// 定时器任务函数
static void* timer_task(void* arg) {
    struct {
        uint64_t timeout_ms;
        AsyncCallback callback;
        void* user_data;
    }* params = (void*)arg;
    
    concurrent_sleep((uint32_t)params->timeout_ms);
    
    if (params->callback) {
        params->callback(NULL, params->user_data);
    }
    
    std_free(params);
    return NULL;
}

// 异步定时器
bool async_timer_set(AsyncEventLoop loop, uint64_t timeout_ms, AsyncCallback callback, void* user_data) {
    if (!loop) return false;
    
    AsyncEventLoopImpl* impl = (AsyncEventLoopImpl*)loop;
    
    void* params = std_malloc(sizeof(struct {
        uint64_t timeout_ms;
        AsyncCallback callback;
        void* user_data;
    }));
    if (!params) return false;
    
    ((struct {
        uint64_t timeout_ms;
        AsyncCallback callback;
        void* user_data;
    }*)params)->timeout_ms = timeout_ms;
    ((struct {
        uint64_t timeout_ms;
        AsyncCallback callback;
        void* user_data;
    }*)params)->callback = callback;
    ((struct {
        uint64_t timeout_ms;
        AsyncCallback callback;
        void* user_data;
    }*)params)->user_data = user_data;
    
    Task task;
    task.func = timer_task;
    task.arg = params;
    task.priority = 0;
    
    thread_pool_add_task(impl->thread_pool, task);
    return true;
}

// 协程支持（简单实现）
Coroutine coroutine_create(CoroutineFunction func, void* arg) {
    CoroutineImpl* coro = (CoroutineImpl*)std_malloc(sizeof(CoroutineImpl));
    if (!coro) {
        return NULL;
    }
    
    coro->func = func;
    coro->arg = arg;
    coro->running = false;
    
    return (Coroutine)coro;
}

void coroutine_resume(Coroutine coro) {
    if (!coro) return;
    
    CoroutineImpl* impl = (CoroutineImpl*)coro;
    if (!impl->running) {
        impl->running = true;
        impl->func(impl->arg);
        impl->running = false;
    }
}

void coroutine_yield() {
    // 简单实现：让出CPU时间
    concurrent_sleep(1);
}

void coroutine_destroy(Coroutine coro) {
    if (!coro) return;
    
    CoroutineImpl* impl = (CoroutineImpl*)coro;
    std_free(impl);
}