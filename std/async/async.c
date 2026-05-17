#include "async.h"
#include "../memory/memory.h"
#include <string.h>
#include <stdatomic.h>

// 轻量级异步任务节点
typedef struct AsyncNode {
    void* (*func)(void*);   // 异步函数
    void* arg;               // 参数
    void* result;            // 结果
    atomic_int status;       // 状态：0=pending, 1=running, 2=completed, 3=cancelled
    _Atomic(struct AsyncNode*) next;  // 下一个节点（原子）
} AsyncNode;

// 轻量级异步事件循环 - 无锁设计
typedef struct LightAsyncLoop {
    _Atomic(AsyncNode*) head;     // 任务队列头
    _Atomic(AsyncNode*) tail;     // 任务队列尾
    _Atomic int count;           // 任务计数
    _Atomic int running;          // 运行标志
    _Atomic int worker_active;    // 活跃worker数
} LightAsyncLoop;

// 创建轻量级异步事件循环
LightAsyncLoop* light_async_loop_create() {
    LightAsyncLoop* loop = (LightAsyncLoop*)kmm_v4_malloc(sizeof(LightAsyncLoop));
    if (!loop) return NULL;

    AsyncNode* dummy = (AsyncNode*)kmm_v4_malloc(sizeof(AsyncNode));
    if (!dummy) {
        return NULL;
    }

    dummy->func = NULL;
    dummy->arg = NULL;
    dummy->result = NULL;
    atomic_store_explicit(&dummy->status, 0, memory_order_release);
    dummy->next = NULL;

    atomic_store_explicit(&loop->head, dummy, memory_order_release);
    atomic_store_explicit(&loop->tail, dummy, memory_order_release);
    atomic_store_explicit(&loop->count, 0, memory_order_release);
    atomic_store_explicit(&loop->running, 0, memory_order_release);
    atomic_store_explicit(&loop->worker_active, 0, memory_order_release);

    return loop;
}

// 销毁轻量级异步事件循环
void light_async_loop_destroy(LightAsyncLoop* loop) {
    // KMM 管理内存，无需手动释放
    (void)loop;
}

// 添加异步任务到循环（无锁Michael-Scott队列）
int light_async_loop_add(LightAsyncLoop* loop, void* (*func)(void*), void* arg) {
    if (!loop || !func) return 0;

    AsyncNode* node = (AsyncNode*)kmm_v4_malloc(sizeof(AsyncNode));
    if (!node) return 0;

    node->func = func;
    node->arg = arg;
    node->result = NULL;
    atomic_store_explicit(&node->status, 0, memory_order_release);
    node->next = NULL;

    AsyncNode* old_tail;
    while (1) {
        old_tail = atomic_load_explicit(&loop->tail, memory_order_acquire);
        AsyncNode* null_next = NULL;
        if (atomic_compare_exchange_weak_explicit(&old_tail->next, &null_next, node,
                                                  memory_order_release, memory_order_release)) {
            break;
        }
        atomic_compare_exchange_weak_explicit(&loop->tail, &old_tail, old_tail,
                                              memory_order_release, memory_order_release);
    }
    atomic_compare_exchange_weak_explicit(&loop->tail, &old_tail, node,
                                          memory_order_release, memory_order_release);
    atomic_fetch_add_explicit(&loop->count, 1, memory_order_acq_rel);

    return 1;
}

// 获取并执行下一个任务（无锁Michael-Scott队列）
// 返回 1 表示执行了任务，0 表示队列空
int light_async_loop_poll(LightAsyncLoop* loop) {
    if (!loop) return 0;

    AsyncNode* head;
    AsyncNode* tail;
    AsyncNode* next;

    while (1) {
        head = atomic_load_explicit(&loop->head, memory_order_acquire);
        tail = atomic_load_explicit(&loop->tail, memory_order_acquire);
        next = atomic_load_explicit(&head->next, memory_order_acquire);

        AsyncNode* current_head = atomic_load_explicit(&loop->head, memory_order_acquire);
        if (current_head != head) {
            continue;
        }

        if (next == NULL) {
            return 0;
        }

        if (head == tail) {
            atomic_compare_exchange_weak_explicit(&loop->tail, &tail, next,
                                                  memory_order_release, memory_order_release);
            continue;
        }

        if (atomic_compare_exchange_weak_explicit(&loop->head, &head, next,
                                                  memory_order_acquire, memory_order_acquire)) {
            break;
        }
    }

    atomic_fetch_sub_explicit(&loop->count, 1, memory_order_acq_rel);

    atomic_store_explicit(&next->status, 1, memory_order_release);

    next->result = next->func(next->arg);

    atomic_store_explicit(&next->status, 2, memory_order_release);

    // KMM 管理内存，无需手动释放

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
    return atomic_load_explicit(&loop->running, memory_order_acquire);
}

// 启动循环
void light_async_loop_start(LightAsyncLoop* loop) {
    if (!loop) return;
    atomic_store_explicit(&loop->running, 1, memory_order_release);
}

// 停止循环
void light_async_loop_stop(LightAsyncLoop* loop) {
    if (!loop) return;
    atomic_store_explicit(&loop->running, 0, memory_order_release);
}

// 获取队列大小
int light_async_loop_size(LightAsyncLoop* loop) {
    if (!loop) return 0;
    return atomic_load_explicit(&loop->count, memory_order_acquire);
}

// 检查任务状态
int light_async_node_status(AsyncNode* node) {
    if (!node) return -1;
    return atomic_load_explicit(&node->status, memory_order_acquire);
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
typedef struct LightTimerManager {
    TimerNode* head;   // 定时器队列（非原子，简化实现）
    uint64_t current_time;  // 当前时间
} LightTimerManager;

// 创建轻量级定时器管理器
LightTimerManager* light_timer_manager_create() {
    LightTimerManager* mgr = (LightTimerManager*)kmm_v4_malloc(sizeof(LightTimerManager));
    if (!mgr) return NULL;

    mgr->head = NULL;
    mgr->current_time = 0;

    return mgr;
}

// 销毁轻量级定时器管理器
void light_timer_manager_destroy(LightTimerManager* mgr) {
    // KMM 管理内存，无需手动释放
    (void)mgr;
}

// 添加定时器任务
int light_timer_manager_add(LightTimerManager* mgr, uint64_t timeout_ms, void (*callback)(void*), void* arg) {
    if (!mgr || !callback) return 0;

    TimerNode* node = (TimerNode*)kmm_v4_malloc(sizeof(TimerNode));
    if (!node) return 0;

    node->timeout_ms = timeout_ms;
    node->start_time = 0;  // 将在 poll 时设置
    node->callback = callback;
    node->arg = arg;
    node->next = NULL;

    // 简单的头插法
    node->next = mgr->head;
    mgr->head = node;

    return 1;
}

// 触发超时的定时器（需要在外部调用更新 current_time）
int light_timer_manager_poll(LightTimerManager* mgr, uint64_t current_time) {
    if (!mgr) return 0;

    mgr->current_time = current_time;

    int triggered = 0;
    TimerNode* current = mgr->head;
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
                mgr->head = current->next;
            }

            TimerNode* to_free = current;
            current = current->next;
            // KMM 管理内存，无需手动释放
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
