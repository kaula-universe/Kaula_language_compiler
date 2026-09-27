#include "async.h"
#include "../memory/memory.h"
#include "../concurrent/concurrent.h"
#include <string.h>
#include <stdatomic.h>

// 杞婚噺绾у紓姝ヤ换鍔¤妭鐐?
typedef struct AsyncNode {
    void (*func)(void*);   // 寮傛鍑芥暟
    void* arg;               // 鍙傛暟
    void* result;            // 缁撴灉
    atomic_int status;       // 鐘舵€侊細0=pending, 1=running, 2=completed, 3=cancelled
    _Atomic(struct AsyncNode*) next;  // 涓嬩竴涓妭鐐癸紙鍘熷瓙锛?
} AsyncNode;

// 杞婚噺绾у紓姝ヤ簨浠跺惊鐜?- 鏃犻攣璁捐
typedef struct LightAsyncLoop {
    _Atomic(AsyncNode*) head;     // 浠诲姟闃熷垪澶?
    _Atomic(AsyncNode*) tail;     // 浠诲姟闃熷垪灏?
    _Atomic int count;           // 浠诲姟璁℃暟
    _Atomic int running;          // 杩愯鏍囧織
    _Atomic int worker_active;    // 娲昏穬worker鏁?
    ThreadPool thread_pool;       // 绾跨▼姹?
} LightAsyncLoop;

// 鍒涘缓杞婚噺绾у紓姝ヤ簨浠跺惊鐜?
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

    loop->thread_pool = thread_pool_create(concurrent_get_processor_count());

    return loop;
}

// 閿€姣佽交閲忕骇寮傛浜嬩欢寰幆
void light_async_loop_destroy(LightAsyncLoop* loop) {
    if (loop && loop->thread_pool) {
        thread_pool_destroy(loop->thread_pool);
    }
    (void)loop;
}

// 娣诲姞寮傛浠诲姟鍒板惊鐜紙鏃犻攣Michael-Scott闃熷垪锛?
int light_async_loop_add(LightAsyncLoop* loop, void (*func)(void*), void* arg) {
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

static void async_task_wrapper(void* arg) {
    AsyncNode* node = (AsyncNode*)arg;
    atomic_store_explicit(&node->status, 1, memory_order_release);
    /* 回调为 void 返回：异步任务没有返回值，node->result 恒为 NULL */
        node->func(node->arg);
    atomic_store_explicit(&node->status, 2, memory_order_release);
}

// 鑾峰彇骞舵墽琛屼笅涓€涓换鍔★紙鏃犻攣Michael-Scott闃熷垪锛?
// 杩斿洖 1 琛ㄧず鍒嗗彂浜嗕换鍔★紝0 琛ㄧず闃熷垪绌?
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

    Task task = {async_task_wrapper, next};
    thread_pool_add_task(loop->thread_pool, task);

    return 1;
}

// 鎵归噺鎵ц浠诲姟
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

// 妫€鏌ュ惊鐜槸鍚﹁繍琛屼腑
int light_async_loop_is_running(LightAsyncLoop* loop) {
    if (!loop) return 0;
    return atomic_load_explicit(&loop->running, memory_order_acquire);
}

// 鍚姩寰幆
void light_async_loop_start(LightAsyncLoop* loop) {
    if (!loop) return;
    atomic_store_explicit(&loop->running, 1, memory_order_release);
}

// 鍋滄寰幆
void light_async_loop_stop(LightAsyncLoop* loop) {
    if (!loop) return;
    atomic_store_explicit(&loop->running, 0, memory_order_release);
}

// 鑾峰彇闃熷垪澶у皬
int light_async_loop_size(LightAsyncLoop* loop) {
    if (!loop) return 0;
    return atomic_load_explicit(&loop->count, memory_order_acquire);
}

// 妫€鏌ヤ换鍔＄姸鎬?
int light_async_node_status(AsyncNode* node) {
    if (!node) return -1;
    return atomic_load_explicit(&node->status, memory_order_acquire);
}

// 鑾峰彇浠诲姟缁撴灉
void* light_async_node_result(AsyncNode* node) {
    if (!node) return NULL;
    return node->result;
}

// 杞婚噺绾у畾鏃跺櫒鑺傜偣
typedef struct TimerNode {
    uint64_t timeout_ms;      // 瓒呮椂鏃堕棿锛堟绉掞級
    uint64_t start_time;      // 寮€濮嬫椂闂?
    void (*callback)(void*); // 鍥炶皟鍑芥暟
    void* arg;                // 鍙傛暟
    struct TimerNode* next;   // 涓嬩竴涓妭鐐?
} TimerNode;

// 杞婚噺绾у畾鏃跺櫒绠＄悊鍣?
typedef struct LightTimerManager {
    TimerNode* head;   // 瀹氭椂鍣ㄩ槦鍒楋紙闈炲師瀛愶紝绠€鍖栧疄鐜帮級
    uint64_t current_time;  // 褰撳墠鏃堕棿
} LightTimerManager;

// 鍒涘缓杞婚噺绾у畾鏃跺櫒绠＄悊鍣?
LightTimerManager* light_timer_manager_create() {
    LightTimerManager* mgr = (LightTimerManager*)kmm_v4_malloc(sizeof(LightTimerManager));
    if (!mgr) return NULL;

    mgr->head = NULL;
    mgr->current_time = 0;

    return mgr;
}

// 閿€姣佽交閲忕骇瀹氭椂鍣ㄧ鐞嗗櫒
void light_timer_manager_destroy(LightTimerManager* mgr) {
    // KMM 绠＄悊鍐呭瓨锛屾棤闇€鎵嬪姩閲婃斁
    (void)mgr;
}

// 娣诲姞瀹氭椂鍣ㄤ换鍔?
int light_timer_manager_add(LightTimerManager* mgr, uint64_t timeout_ms, void (*callback)(void*), void* arg) {
    if (!mgr || !callback) return 0;

    TimerNode* node = (TimerNode*)kmm_v4_malloc(sizeof(TimerNode));
    if (!node) return 0;

    node->timeout_ms = timeout_ms;
    node->start_time = 0;  // 灏嗗湪 poll 鏃惰缃?
    node->callback = callback;
    node->arg = arg;
    node->next = NULL;

    // 绠€鍗曠殑澶存彃娉?
    node->next = mgr->head;
    mgr->head = node;

    return 1;
}

// 瑙﹀彂瓒呮椂鐨勫畾鏃跺櫒锛堥渶瑕佸湪澶栭儴璋冪敤鏇存柊 current_time锛?
int light_timer_manager_poll(LightTimerManager* mgr, uint64_t current_time) {
    if (!mgr) return 0;

    mgr->current_time = current_time;

    int triggered = 0;
    TimerNode* current = mgr->head;
    TimerNode* prev = NULL;

    while (current) {
        uint64_t elapsed = current_time - current->start_time;
        if (elapsed >= current->timeout_ms) {
            // 瑙﹀彂鍥炶皟
            if (current->callback) {
                current->callback(current->arg);
            }

            // 浠庨槦鍒椾腑绉婚櫎
            if (prev) {
                prev->next = current->next;
            } else {
                mgr->head = current->next;
            }

            TimerNode* to_free = current;
            current = current->next;
            // KMM 绠＄悊鍐呭瓨锛屾棤闇€鎵嬪姩閲婃斁
            triggered++;
        } else {
            prev = current;
            current = current->next;
        }
    }

    return triggered;
}

// 鍏煎鎬у埆鍚?
typedef LightAsyncLoop AsyncEventLoop;
typedef LightTimerManager AsyncTimerManager;

AsyncEventLoop* async_event_loop_create() {
    return light_async_loop_create();
}

void async_event_loop_destroy(AsyncEventLoop* loop) {
    light_async_loop_destroy((LightAsyncLoop*)loop);
}

int async_event_loop_add(AsyncEventLoop* loop, void (*func)(void*), void* arg) {
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

// 绠€鍖栫殑寮傛I/O鎿嶄綔锛堜娇鐢ㄤ换鍔￠槦鍒楋級
int async_io_read(AsyncEventLoop* loop, int fd, void* buffer, size_t size) {
    (void)fd;
    (void)buffer;
    (void)size;
    // 绠€鍖栧疄鐜帮細鍦ㄥ惊鐜腑鎵ц璇诲彇
    return light_async_loop_add((LightAsyncLoop*)loop, NULL, NULL);
}

int async_io_write(AsyncEventLoop* loop, int fd, const void* buffer, size_t size) {
    (void)fd;
    (void)buffer;
    (void)size;
    // 绠€鍖栧疄鐜帮細鍦ㄥ惊鐜腑鎵ц鍐欏叆
    return light_async_loop_add((LightAsyncLoop*)loop, NULL, NULL);
}

// 鍏煎鏃PI
void async_event_loop_run(AsyncEventLoop* loop) {
    light_async_loop_start((LightAsyncLoop*)loop);
}

void async_event_loop_post(AsyncEventLoop* loop, void (*func)(void*), void* arg) {
    light_async_loop_add((LightAsyncLoop*)loop, func, arg);
}
