#include "concurrent.h"
#include "../memory/memory.h"
#include <stdlib.h>
#include <string.h>

#ifdef _WIN32
#include <windows.h>
#else
#include <pthread.h>
#include <unistd.h>
#include <sys/syscall.h>
#include <semaphore.h>
#include <sys/sysinfo.h>
#include <time.h>
#endif

// 线程实现
Thread thread_create(ThreadFunction func, void* arg) {
#ifdef _WIN32
    return (Thread)CreateThread(NULL, 0, (LPTHREAD_START_ROUTINE)func, arg, 0, NULL);
#else
    pthread_t* thread = (pthread_t*)kmm_v4_malloc(sizeof(pthread_t));
    if (thread) {
        if (pthread_create(thread, NULL, func, arg) != 0) {
            // KMM 管理内存，无需手动释放
            return NULL;
        }
    }
    return thread;
#endif
}

void thread_join(Thread thread) {
#ifdef _WIN32
    WaitForSingleObject((HANDLE)thread, INFINITE);
    CloseHandle((HANDLE)thread);
#else
    if (thread) {
        pthread_join(*(pthread_t*)thread, NULL);
        // KMM 管理内存，无需手动释放
    }
#endif
}

void thread_detach(Thread thread) {
#ifdef _WIN32
    CloseHandle((HANDLE)thread);
#else
    if (thread) {
        pthread_detach(*(pthread_t*)thread);
        // KMM 管理内存，无需手动释放
    }
#endif
}

Thread thread_self() {
#ifdef _WIN32
    return (Thread)GetCurrentThread();
#else
    pthread_t* thread = (pthread_t*)kmm_v4_malloc(sizeof(pthread_t));
    if (thread) {
        *thread = pthread_self();
    }
    return thread;
#endif
}

bool thread_equal(Thread t1, Thread t2) {
#ifdef _WIN32
    return GetThreadId((HANDLE)t1) == GetThreadId((HANDLE)t2);
#else
    if (!t1 || !t2) return false;
    return pthread_equal(*(pthread_t*)t1, *(pthread_t*)t2) != 0;
#endif
}

// 互斥锁实现
Mutex mutex_create() {
#ifdef _WIN32
    HANDLE mutex = CreateMutex(NULL, FALSE, NULL);
    return (Mutex)mutex;
#else
    pthread_mutex_t* mutex = (pthread_mutex_t*)kmm_v4_malloc(sizeof(pthread_mutex_t));
    if (mutex) {
        pthread_mutex_init(mutex, NULL);
    }
    return mutex;
#endif
}

void mutex_destroy(Mutex mutex) {
#ifdef _WIN32
    CloseHandle((HANDLE)mutex);
#else
    if (mutex) {
        pthread_mutex_destroy((pthread_mutex_t*)mutex);
        // KMM 管理内存，无需手动释放
    }
#endif
}

void mutex_lock(Mutex mutex) {
#ifdef _WIN32
    WaitForSingleObject((HANDLE)mutex, INFINITE);
#else
    if (mutex) {
        pthread_mutex_lock((pthread_mutex_t*)mutex);
    }
#endif
}

void mutex_unlock(Mutex mutex) {
#ifdef _WIN32
    ReleaseMutex((HANDLE)mutex);
#else
    if (mutex) {
        pthread_mutex_unlock((pthread_mutex_t*)mutex);
    }
#endif
}

bool mutex_trylock(Mutex mutex) {
#ifdef _WIN32
    return WaitForSingleObject((HANDLE)mutex, 0) == WAIT_OBJECT_0;
#else
    if (!mutex) return false;
    return pthread_mutex_trylock((pthread_mutex_t*)mutex) == 0;
#endif
}

// 条件变量实现
typedef struct {
#ifdef _WIN32
    CONDITION_VARIABLE cond;
#else
    pthread_cond_t cond;
#endif
} ConditionImpl;

Condition condition_create() {
#ifdef _WIN32
    ConditionImpl* impl = (ConditionImpl*)kmm_v4_malloc(sizeof(ConditionImpl));
    if (impl) {
        InitializeConditionVariable(&impl->cond);
    }
    return (Condition)impl;
#else
    pthread_cond_t* cond = (pthread_cond_t*)kmm_v4_malloc(sizeof(pthread_cond_t));
    if (cond) {
        pthread_cond_init(cond, NULL);
    }
    return cond;
#endif
}

void condition_destroy(Condition condition) {
#ifdef _WIN32
    (void)condition;
#else
    if (condition) {
        pthread_cond_destroy((pthread_cond_t*)condition);
    }
#endif
}

void condition_wait(Condition condition, Mutex mutex) {
#ifdef _WIN32
    if (!condition || !mutex) return;
    ConditionImpl* impl = (ConditionImpl*)condition;
    SleepConditionVariableCS(&impl->cond, (CRITICAL_SECTION*)mutex, INFINITE);
#else
    if (condition && mutex) {
        pthread_cond_wait((pthread_cond_t*)condition, (pthread_mutex_t*)mutex);
    }
#endif
}

bool condition_timedwait(Condition condition, Mutex mutex, uint64_t timeout_ms) {
#ifdef _WIN32
    if (!condition || !mutex) return false;
    ConditionImpl* impl = (ConditionImpl*)condition;
    return SleepConditionVariableCS(&impl->cond, (CRITICAL_SECTION*)mutex, (DWORD)timeout_ms) != FALSE;
#else
    if (!condition || !mutex) return false;
    struct timespec ts;
    clock_gettime(CLOCK_REALTIME, &ts);
    ts.tv_sec += timeout_ms / 1000;
    ts.tv_nsec += (timeout_ms % 1000) * 1000000;
    if (ts.tv_nsec >= 1000000000) {
        ts.tv_sec++;
        ts.tv_nsec -= 1000000000;
    }
    return pthread_cond_timedwait((pthread_cond_t*)condition, (pthread_mutex_t*)mutex, &ts) == 0;
#endif
}

void condition_signal(Condition condition) {
#ifdef _WIN32
    if (!condition) return;
    ConditionImpl* impl = (ConditionImpl*)condition;
    WakeConditionVariable(&impl->cond);
#else
    if (condition) {
        pthread_cond_signal((pthread_cond_t*)condition);
    }
#endif
}

void condition_broadcast(Condition condition) {
#ifdef _WIN32
    if (!condition) return;
    ConditionImpl* impl = (ConditionImpl*)condition;
    WakeAllConditionVariable(&impl->cond);
#else
    if (condition) {
        pthread_cond_broadcast((pthread_cond_t*)condition);
    }
#endif
}

// 信号量实现
Semaphore semaphore_create(uint32_t initial_value) {
#ifdef _WIN32
    HANDLE semaphore = CreateSemaphore(NULL, initial_value, UINT32_MAX, NULL);
    return (Semaphore)semaphore;
#else
    sem_t* semaphore = (sem_t*)kmm_v4_malloc(sizeof(sem_t));
    if (semaphore) {
        sem_init(semaphore, 0, initial_value);
    }
    return semaphore;
#endif
}

void semaphore_destroy(Semaphore semaphore) {
#ifdef _WIN32
    CloseHandle((HANDLE)semaphore);
#else
    if (semaphore) {
        sem_destroy((sem_t*)semaphore);
        // KMM 管理内存，无需手动释放
    }
#endif
}

void semaphore_wait(Semaphore semaphore) {
#ifdef _WIN32
    WaitForSingleObject((HANDLE)semaphore, INFINITE);
#else
    if (semaphore) {
        sem_wait((sem_t*)semaphore);
    }
#endif
}

bool semaphore_trywait(Semaphore semaphore) {
#ifdef _WIN32
    return WaitForSingleObject((HANDLE)semaphore, 0) == WAIT_OBJECT_0;
#else
    if (!semaphore) return false;
    return sem_trywait((sem_t*)semaphore) == 0;
#endif
}

bool semaphore_timedwait(Semaphore semaphore, uint64_t timeout_ms) {
#ifdef _WIN32
    return WaitForSingleObject((HANDLE)semaphore, (DWORD)timeout_ms) == WAIT_OBJECT_0;
#else
    if (!semaphore) return false;
    struct timespec ts;
    clock_gettime(CLOCK_REALTIME, &ts);
    ts.tv_sec += timeout_ms / 1000;
    ts.tv_nsec += (timeout_ms % 1000) * 1000000;
    if (ts.tv_nsec >= 1000000000) {
        ts.tv_sec++;
        ts.tv_nsec -= 1000000000;
    }
    return sem_timedwait((sem_t*)semaphore, &ts) == 0;
#endif
}

void semaphore_post(Semaphore semaphore) {
#ifdef _WIN32
    ReleaseSemaphore((HANDLE)semaphore, 1, NULL);
#else
    if (semaphore) {
        sem_post((sem_t*)semaphore);
    }
#endif
}

uint32_t semaphore_get_value(Semaphore semaphore) {
#ifdef _WIN32
    // Windows没有直接获取信号量值的API，这里返回0作为占位符
    return 0;
#else
    if (!semaphore) return 0;
    int value;
    sem_getvalue((sem_t*)semaphore, &value);
    return (uint32_t)value;
#endif
}

// 读写锁实现
ReadWriteLock rwlock_create() {
#ifdef _WIN32
    SRWLOCK* rwlock = (SRWLOCK*)kmm_v4_malloc(sizeof(SRWLOCK));
    if (rwlock) {
        InitializeSRWLock(rwlock);
    }
    return rwlock;
#else
    pthread_rwlock_t* rwlock = (pthread_rwlock_t*)kmm_v4_malloc(sizeof(pthread_rwlock_t));
    if (rwlock) {
        pthread_rwlock_init(rwlock, NULL);
    }
    return rwlock;
#endif
}

void rwlock_destroy(ReadWriteLock rwlock) {
#ifdef _WIN32
    // SRWLOCK 不需要销毁
    (void)rwlock;
#else
    if (rwlock) {
        pthread_rwlock_destroy((pthread_rwlock_t*)rwlock);
    }
#endif
}

void rwlock_read_lock(ReadWriteLock rwlock) {
#ifdef _WIN32
    if (rwlock) {
        AcquireSRWLockShared((SRWLOCK*)rwlock);
    }
#else
    if (rwlock) {
        pthread_rwlock_rdlock((pthread_rwlock_t*)rwlock);
    }
#endif
}

void rwlock_read_unlock(ReadWriteLock rwlock) {
#ifdef _WIN32
    if (rwlock) {
        ReleaseSRWLockShared((SRWLOCK*)rwlock);
    }
#else
    if (rwlock) {
        pthread_rwlock_unlock((pthread_rwlock_t*)rwlock);
    }
#endif
}

void rwlock_write_lock(ReadWriteLock rwlock) {
#ifdef _WIN32
    if (rwlock) {
        AcquireSRWLockExclusive((SRWLOCK*)rwlock);
    }
#else
    if (rwlock) {
        pthread_rwlock_wrlock((pthread_rwlock_t*)rwlock);
    }
#endif
}

void rwlock_write_unlock(ReadWriteLock rwlock) {
#ifdef _WIN32
    if (rwlock) {
        ReleaseSRWLockExclusive((SRWLOCK*)rwlock);
    }
#else
    if (rwlock) {
        pthread_rwlock_unlock((pthread_rwlock_t*)rwlock);
    }
#endif
}

bool rwlock_try_read_lock(ReadWriteLock rwlock) {
#ifdef _WIN32
    if (!rwlock) return false;
    return TryAcquireSRWLockShared((SRWLOCK*)rwlock) != FALSE;
#else
    if (!rwlock) return false;
    return pthread_rwlock_tryrdlock((pthread_rwlock_t*)rwlock) == 0;
#endif
}

bool rwlock_try_write_lock(ReadWriteLock rwlock) {
#ifdef _WIN32
    if (!rwlock) return false;
    return TryAcquireSRWLockExclusive((SRWLOCK*)rwlock) != FALSE;
#else
    if (!rwlock) return false;
    return pthread_rwlock_trywrlock((pthread_rwlock_t*)rwlock) == 0;
#endif
}

// 原子操作实现
int kaula_atomic_add(volatile int* ptr, int value) {
#ifdef _WIN32
    return InterlockedAdd((LONG*)ptr, value);
#else
    return __atomic_fetch_add(ptr, value, __ATOMIC_SEQ_CST);
#endif
}

int kaula_atomic_sub(volatile int* ptr, int value) {
#ifdef _WIN32
    return InterlockedAdd((LONG*)ptr, -value);
#else
    return __atomic_fetch_sub(ptr, value, __ATOMIC_SEQ_CST);
#endif
}

int kaula_atomic_exchange(volatile int* ptr, int value) {
#ifdef _WIN32
    return InterlockedExchange((LONG*)ptr, value);
#else
    return __atomic_exchange_n(ptr, value, __ATOMIC_SEQ_CST);
#endif
}

bool kaula_atomic_compare_exchange(volatile int* ptr, int expected, int desired) {
#ifdef _WIN32
    return InterlockedCompareExchange((LONG*)ptr, desired, expected) == expected;
#else
    return __atomic_compare_exchange_n(ptr, &expected, desired, 0, __ATOMIC_SEQ_CST, __ATOMIC_SEQ_CST);
#endif
}

void kaula_atomic_store(volatile int* ptr, int value) {
#ifdef _WIN32
    InterlockedExchange((LONG*)ptr, value);
#else
    __atomic_store_n(ptr, value, __ATOMIC_SEQ_CST);
#endif
}

int kaula_atomic_load(volatile int* ptr) {
#ifdef _WIN32
    return InterlockedCompareExchange((LONG*)ptr, 0, 0);
#else
    return __atomic_load_n(ptr, __ATOMIC_SEQ_CST);
#endif
}

// 线程池实现
typedef struct ThreadPoolImpl {
    Thread* threads;
    size_t thread_count;
    Task* tasks;
    size_t task_head;
    size_t task_tail;
    size_t task_capacity;
    Mutex mutex;
    Condition condition;
    bool running;
} ThreadPoolImpl;

static void* thread_pool_worker(void* arg) {
    ThreadPoolImpl* pool = (ThreadPoolImpl*)arg;
    while (kaula_atomic_load((volatile int*)&pool->running)) {
        mutex_lock(pool->mutex);
        while (pool->task_head == pool->task_tail && kaula_atomic_load((volatile int*)&pool->running)) {
            condition_wait(pool->condition, pool->mutex);
        }
        if (!kaula_atomic_load((volatile int*)&pool->running)) {
            mutex_unlock(pool->mutex);
            break;
        }
        Task task = pool->tasks[pool->task_head];
        pool->task_head = (pool->task_head + 1) % pool->task_capacity;
        mutex_unlock(pool->mutex);
        task.func(task.arg);
    }
    return NULL;
}

ThreadPool thread_pool_create(size_t thread_count) {
    ThreadPoolImpl* pool = (ThreadPoolImpl*)kmm_v4_malloc(sizeof(ThreadPoolImpl));
    if (pool) {
        pool->thread_count = thread_count;
        pool->threads = (Thread*)kmm_v4_malloc(thread_count * sizeof(Thread));
        pool->task_capacity = 1024;
        pool->tasks = (Task*)kmm_v4_malloc(pool->task_capacity * sizeof(Task));
        pool->task_head = 0;
        pool->task_tail = 0;
        pool->mutex = mutex_create();
        pool->condition = condition_create();
        pool->running = true;
        for (size_t i = 0; i < thread_count; i++) {
            pool->threads[i] = thread_create(thread_pool_worker, pool);
        }
    }
    return pool;
}

void thread_pool_destroy(ThreadPool pool) {
    ThreadPoolImpl* impl = (ThreadPoolImpl*)pool;
    if (impl) {
        kaula_atomic_store((volatile int*)&impl->running, 0);
        condition_broadcast(impl->condition);
        for (size_t i = 0; i < impl->thread_count; i++) {
            thread_join(impl->threads[i]);
        }
        mutex_destroy(impl->mutex);
        condition_destroy(impl->condition);
        // KMM 管理内存，无需手动释放
    }
}

void thread_pool_add_task(ThreadPool pool, Task task) {
    ThreadPoolImpl* impl = (ThreadPoolImpl*)pool;
    if (impl) {
        mutex_lock(impl->mutex);
        size_t next_tail = (impl->task_tail + 1) % impl->task_capacity;
        if (next_tail == impl->task_head) {
            size_t new_capacity = impl->task_capacity * 2;
            Task* new_tasks = (Task*)kmm_v4_malloc(new_capacity * sizeof(Task));
            if (new_tasks) {
                size_t count = (impl->task_tail >= impl->task_head) ? 
                    (impl->task_tail - impl->task_head) :
                    (impl->task_capacity - impl->task_head + impl->task_tail);
                for (size_t i = 0; i < count; i++) {
                    new_tasks[i] = impl->tasks[(impl->task_head + i) % impl->task_capacity];
                }
                impl->tasks = new_tasks;
                impl->task_capacity = new_capacity;
                impl->task_head = 0;
                impl->task_tail = count;
                next_tail = count + 1;
            } else {
                mutex_unlock(impl->mutex);
                return;
            }
        }
        impl->tasks[impl->task_tail] = task;
        impl->task_tail = next_tail;
        condition_signal(impl->condition);
        mutex_unlock(impl->mutex);
    }
}

void thread_pool_wait_completion(ThreadPool pool) {
    ThreadPoolImpl* impl = (ThreadPoolImpl*)pool;
    if (impl) {
        bool done;
        do {
            mutex_lock(impl->mutex);
            done = impl->task_head == impl->task_tail;
            mutex_unlock(impl->mutex);
            if (!done) {
                concurrent_sleep(1);
            }
        } while (!done);
    }
}

// 并发工具
void concurrent_sleep(uint32_t milliseconds) {
#ifdef _WIN32
    Sleep(milliseconds);
#else
    struct timespec ts;
    ts.tv_sec = milliseconds / 1000;
    ts.tv_nsec = (milliseconds % 1000) * 1000000L;
    nanosleep(&ts, NULL);
#endif
}

uint64_t concurrent_get_thread_id() {
#ifdef _WIN32
    return (uint64_t)GetCurrentThreadId();
#else
    return (uint64_t)syscall(SYS_gettid);
#endif
}

size_t concurrent_get_processor_count() {
#ifdef _WIN32
    SYSTEM_INFO info;
    GetSystemInfo(&info);
    return info.dwNumberOfProcessors;
#else
    return sysconf(_SC_NPROCESSORS_ONLN);
#endif
}

// Channel 实现
typedef struct Channel {
    void** buffer;
    size_t capacity;
    size_t head;
    size_t tail;
    size_t count;
    Mutex mutex;
    Condition not_full;
    Condition not_empty;
    bool_t closed;
} Channel;

Channel* channel_create(size_t capacity) {
    Channel* ch = (Channel*)kmm_v4_calloc(1, sizeof(Channel));
    if (ch) {
        ch->capacity = capacity > 0 ? capacity : 16;
        ch->buffer = (void**)kmm_v4_calloc(ch->capacity, sizeof(void*));
        ch->mutex = mutex_create();
        ch->not_full = condition_create();
        ch->not_empty = condition_create();
    }
    return ch;
}

void channel_destroy(Channel* ch) {
    // KMM 管理内存，无需手动释放
    if (ch) {
        mutex_destroy(ch->mutex);
        condition_destroy(ch->not_full);
        condition_destroy(ch->not_empty);
    }
}

bool_t channel_send(Channel* ch, void* data) {
    if (!ch) return false;
    mutex_lock(ch->mutex);
    while (ch->count >= ch->capacity && !ch->closed) {
        condition_wait(ch->not_full, ch->mutex);
    }
    if (ch->closed) { mutex_unlock(ch->mutex); return false; }
    ch->buffer[ch->tail] = data;
    ch->tail = (ch->tail + 1) % ch->capacity;
    ch->count++;
    condition_signal(ch->not_empty);
    mutex_unlock(ch->mutex);
    return true;
}

bool_t channel_send_timeout(Channel* ch, void* data, uint64_t timeout_ms) {
    if (!ch) return false;
    mutex_lock(ch->mutex);
    bool_t result = true;
    while (ch->count >= ch->capacity && !ch->closed) {
        if (!condition_timedwait(ch->not_full, ch->mutex, timeout_ms)) { result = false; break; }
    }
    if (result && !ch->closed) {
        ch->buffer[ch->tail] = data;
        ch->tail = (ch->tail + 1) % ch->capacity;
        ch->count++;
        condition_signal(ch->not_empty);
    }
    mutex_unlock(ch->mutex);
    return result;
}

void* channel_receive(Channel* ch) {
    if (!ch) return NULL;
    mutex_lock(ch->mutex);
    while (ch->count == 0 && !ch->closed) {
        condition_wait(ch->not_empty, ch->mutex);
    }
    if (ch->count == 0) { mutex_unlock(ch->mutex); return NULL; }
    void* data = ch->buffer[ch->head];
    ch->head = (ch->head + 1) % ch->capacity;
    ch->count--;
    condition_signal(ch->not_full);
    mutex_unlock(ch->mutex);
    return data;
}

void* channel_receive_timeout(Channel* ch, uint64_t timeout_ms) {
    if (!ch) return NULL;
    mutex_lock(ch->mutex);
    void* data = NULL;
    while (ch->count == 0 && !ch->closed) {
        if (!condition_timedwait(ch->not_empty, ch->mutex, timeout_ms)) break;
    }
    if (ch->count > 0) {
        data = ch->buffer[ch->head];
        ch->head = (ch->head + 1) % ch->capacity;
        ch->count--;
        condition_signal(ch->not_full);
    }
    mutex_unlock(ch->mutex);
    return data;
}

bool_t channel_try_receive(Channel* ch, void** out_data) {
    if (!ch || !out_data) return false;
    if (!mutex_trylock(ch->mutex)) return false;
    if (ch->count == 0) { mutex_unlock(ch->mutex); return false; }
    *out_data = ch->buffer[ch->head];
    ch->head = (ch->head + 1) % ch->capacity;
    ch->count--;
    condition_signal(ch->not_full);
    mutex_unlock(ch->mutex);
    return true;
}

void channel_close(Channel* ch) {
    if (!ch) return;
    mutex_lock(ch->mutex);
    ch->closed = true;
    condition_broadcast(ch->not_empty);
    condition_broadcast(ch->not_full);
    mutex_unlock(ch->mutex);
}

bool_t channel_is_closed(Channel* ch) { return ch && ch->closed; }
size_t channel_len(Channel* ch) { return ch ? ch->count : 0; }

// Future/Promise 实现
struct Promise {
    Mutex mutex;
    Condition condition;
    void* result;
    int error_code;
    bool_t done;
    bool_t has_error;
    Future* future;
};

struct Future {
    Promise* promise;
};

Promise* promise_create() {
    Promise* p = (Promise*)kmm_v4_calloc(1, sizeof(Promise));
    if (p) {
        p->mutex = mutex_create();
        p->condition = condition_create();
        p->future = (Future*)kmm_v4_calloc(1, sizeof(Future));
        p->future->promise = p;
    }
    return p;
}

void promise_destroy(Promise* promise) {
    // KMM 管理内存，无需手动释放
    if (promise) {
        mutex_destroy(promise->mutex);
        condition_destroy(promise->condition);
    }
}

Future* promise_get_future(Promise* promise) { return promise ? promise->future : NULL; }

void promise_set_result(Promise* promise, void* result) {
    if (!promise) return;
    mutex_lock(promise->mutex);
    promise->result = result;
    promise->done = true;
    condition_broadcast(promise->condition);
    mutex_unlock(promise->mutex);
}

void promise_set_error(Promise* promise, int error_code) {
    if (!promise) return;
    mutex_lock(promise->mutex);
    promise->error_code = error_code;
    promise->done = true;
    promise->has_error = true;
    condition_broadcast(promise->condition);
    mutex_unlock(promise->mutex);
}

bool_t promise_is_done(Promise* promise) { return promise ? promise->done : false; }
bool_t promise_is_error(Promise* promise) { return promise ? promise->has_error : false; }

Future* future_create() { Promise* p = promise_create(); return p ? p->future : NULL; }

void future_destroy(Future* future) {
    // KMM 管理内存，无需手动释放
    if (future) {
        if (future->promise) {
            mutex_destroy(future->promise->mutex);
            condition_destroy(future->promise->condition);
        }
    }
}

void* future_get(Future* future) {
    if (!future || !future->promise) return NULL;
    mutex_lock(future->promise->mutex);
    while (!future->promise->done) {
        condition_wait(future->promise->condition, future->promise->mutex);
    }
    void* result = future->promise->result;
    mutex_unlock(future->promise->mutex);
    return result;
}

void* future_get_timeout(Future* future, uint64_t timeout_ms) {
    if (!future || !future->promise) return NULL;
    mutex_lock(future->promise->mutex);
    while (!future->promise->done) {
        if (!condition_timedwait(future->promise->condition, future->promise->mutex, timeout_ms)) break;
    }
    void* result = future->promise->done ? future->promise->result : NULL;
    mutex_unlock(future->promise->mutex);
    return result;
}

bool_t future_is_done(Future* future) { return future && future->promise ? future->promise->done : false; }
bool_t future_is_error(Future* future) { return future && future->promise ? future->promise->has_error : false; }
int future_get_error(Future* future) { return future && future->promise ? future->promise->error_code : 0; }

typedef struct MapTaskArg {
    Future* input;
    void* (*func)(void*);
    Promise* output;
} MapTaskArg;

static void* future_map_worker(void* arg) {
    MapTaskArg* mta = (MapTaskArg*)arg;
    void* input_result = future_get(mta->input);
    if (input_result) {
        void* output = mta->func(input_result);
        promise_set_result(mta->output, output);
    }
    return NULL;
}

Future* future_map(Future* input, void* (*func)(void*)) {
    if (!input || !func) return NULL;
    Promise* output = promise_create();
    MapTaskArg* mta = (MapTaskArg*)kmm_v4_malloc(sizeof(MapTaskArg));
    mta->input = input;
    mta->func = func;
    mta->output = output;
    thread_detach(thread_create(future_map_worker, mta));
    return output->future;
}

typedef struct AllTaskArg {
    Future** futures;
    size_t count;
    Promise* output;
    void** results;
    volatile int completed;
    Mutex mutex;
} AllTaskArg;

static void* all_worker(void* arg) {
    AllTaskArg* ata = (AllTaskArg*)arg;
    mutex_lock(ata->mutex);
    int idx = ata->completed++;
    mutex_unlock(ata->mutex);
    if (idx < (int)ata->count) {
        void* result = future_get(ata->futures[idx]);
        mutex_lock(ata->mutex);
        ata->results[idx] = result;
        bool_t all_done = true;
        for (size_t i = 0; i < ata->count; i++) { if (!future_is_done(ata->futures[i])) { all_done = false; break; } }
        if (all_done) promise_set_result(ata->output, ata->results);
        mutex_unlock(ata->mutex);
    }
    return NULL;
}

Future* future_all(Future** futures, size_t count) {
    if (!futures || count == 0) return NULL;
    Promise* output = promise_create();
    AllTaskArg* ata = (AllTaskArg*)kmm_v4_calloc(1, sizeof(AllTaskArg));
    ata->futures = futures;
    ata->count = count;
    ata->output = output;
    ata->results = (void**)kmm_v4_calloc(count, sizeof(void*));
    ata->mutex = mutex_create();
    for (size_t i = 0; i < count; i++) {
        thread_detach(thread_create(all_worker, ata));
    }
    return output->future;
}

Future* future_any(Future** futures, size_t count) {
    if (!futures || count == 0) return NULL;
    Promise* output = promise_create();
    typedef struct { Future** futures; size_t count; Promise* output; Mutex mutex; bool_t done; } AnyArg;
    AnyArg* aa = (AnyArg*)kmm_v4_calloc(1, sizeof(AnyArg));
    aa->futures = futures;
    aa->count = count;
    aa->output = output;
    aa->mutex = mutex_create();
    typedef struct { AnyArg* aa; size_t idx; } WorkerArg;
    WorkerArg* args = (WorkerArg*)kmm_v4_malloc(count * sizeof(WorkerArg));
    for (size_t i = 0; i < count; i++) {
        args[i].aa = aa;
        args[i].idx = i;
        typedef void* (*Func)(void*);
        typedef struct { WorkerArg* arg; } Closure;
        Closure* c = (Closure*)kmm_v4_malloc(sizeof(Closure)); c->arg = &args[i];
        thread_detach(thread_create((Func)future_map_worker, c));
    }
    return output->future;
}
