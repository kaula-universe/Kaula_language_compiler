#include "concurrent.h"
#include <stdlib.h>
#include <string.h>

#ifdef _WIN32
#include <windows.h>
#else
#include <pthread.h>
#include <unistd.h>
#include <sys/syscall.h>
#endif

// 线程实现
Thread thread_create(ThreadFunction func, void* arg) {
#ifdef _WIN32
    return (Thread)CreateThread(NULL, 0, (LPTHREAD_START_ROUTINE)func, arg, 0, NULL);
#else
    pthread_t* thread = (pthread_t*)malloc(sizeof(pthread_t));
    if (thread) {
        if (pthread_create(thread, NULL, func, arg) != 0) {
            free(thread);
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
        free(thread);
    }
#endif
}

void thread_detach(Thread thread) {
#ifdef _WIN32
    CloseHandle((HANDLE)thread);
#else
    if (thread) {
        pthread_detach(*(pthread_t*)thread);
        free(thread);
    }
#endif
}

Thread thread_self() {
#ifdef _WIN32
    return (Thread)GetCurrentThread();
#else
    pthread_t* thread = (pthread_t*)malloc(sizeof(pthread_t));
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
    pthread_mutex_t* mutex = (pthread_mutex_t*)malloc(sizeof(pthread_mutex_t));
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
        free(mutex);
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
Condition condition_create() {
#ifdef _WIN32
    HANDLE event = CreateEvent(NULL, FALSE, FALSE, NULL);
    return (Condition)event;
#else
    pthread_cond_t* cond = (pthread_cond_t*)malloc(sizeof(pthread_cond_t));
    if (cond) {
        pthread_cond_init(cond, NULL);
    }
    return cond;
#endif
}

void condition_destroy(Condition condition) {
#ifdef _WIN32
    CloseHandle((HANDLE)condition);
#else
    if (condition) {
        pthread_cond_destroy((pthread_cond_t*)condition);
        free(condition);
    }
#endif
}

void condition_wait(Condition condition, Mutex mutex) {
#ifdef _WIN32
    mutex_unlock(mutex);
    WaitForSingleObject((HANDLE)condition, INFINITE);
    mutex_lock(mutex);
#else
    if (condition && mutex) {
        pthread_cond_wait((pthread_cond_t*)condition, (pthread_mutex_t*)mutex);
    }
#endif
}

bool condition_timedwait(Condition condition, Mutex mutex, uint64_t timeout_ms) {
#ifdef _WIN32
    mutex_unlock(mutex);
    DWORD result = WaitForSingleObject((HANDLE)condition, (DWORD)timeout_ms);
    mutex_lock(mutex);
    return result == WAIT_OBJECT_0;
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
    SetEvent((HANDLE)condition);
#else
    if (condition) {
        pthread_cond_signal((pthread_cond_t*)condition);
    }
#endif
}

void condition_broadcast(Condition condition) {
#ifdef _WIN32
    SetEvent((HANDLE)condition);
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
    sem_t* semaphore = (sem_t*)malloc(sizeof(sem_t));
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
        free(semaphore);
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
    // 在Windows上使用互斥锁模拟读写锁
    HANDLE mutex = CreateMutex(NULL, FALSE, NULL);
    return (ReadWriteLock)mutex;
#else
    pthread_rwlock_t* rwlock = (pthread_rwlock_t*)malloc(sizeof(pthread_rwlock_t));
    if (rwlock) {
        pthread_rwlock_init(rwlock, NULL);
    }
    return rwlock;
#endif
}

void rwlock_destroy(ReadWriteLock rwlock) {
#ifdef _WIN32
    CloseHandle((HANDLE)rwlock);
#else
    if (rwlock) {
        pthread_rwlock_destroy((pthread_rwlock_t*)rwlock);
        free(rwlock);
    }
#endif
}

void rwlock_read_lock(ReadWriteLock rwlock) {
#ifdef _WIN32
    // 在Windows上使用互斥锁模拟读写锁
    WaitForSingleObject((HANDLE)rwlock, INFINITE);
#else
    if (rwlock) {
        pthread_rwlock_rdlock((pthread_rwlock_t*)rwlock);
    }
#endif
}

void rwlock_read_unlock(ReadWriteLock rwlock) {
#ifdef _WIN32
    // 在Windows上使用互斥锁模拟读写锁
    ReleaseMutex((HANDLE)rwlock);
#else
    if (rwlock) {
        pthread_rwlock_unlock((pthread_rwlock_t*)rwlock);
    }
#endif
}

void rwlock_write_lock(ReadWriteLock rwlock) {
#ifdef _WIN32
    // 在Windows上使用互斥锁模拟读写锁
    WaitForSingleObject((HANDLE)rwlock, INFINITE);
#else
    if (rwlock) {
        pthread_rwlock_wrlock((pthread_rwlock_t*)rwlock);
    }
#endif
}

void rwlock_write_unlock(ReadWriteLock rwlock) {
#ifdef _WIN32
    // 在Windows上使用互斥锁模拟读写锁
    ReleaseMutex((HANDLE)rwlock);
#else
    if (rwlock) {
        pthread_rwlock_unlock((pthread_rwlock_t*)rwlock);
    }
#endif
}

bool rwlock_try_read_lock(ReadWriteLock rwlock) {
#ifdef _WIN32
    // 在Windows上使用互斥锁模拟读写锁
    return WaitForSingleObject((HANDLE)rwlock, 0) == WAIT_OBJECT_0;
#else
    if (!rwlock) return false;
    return pthread_rwlock_tryrdlock((pthread_rwlock_t*)rwlock) == 0;
#endif
}

bool rwlock_try_write_lock(ReadWriteLock rwlock) {
#ifdef _WIN32
    // 在Windows上使用互斥锁模拟读写锁
    return WaitForSingleObject((HANDLE)rwlock, 0) == WAIT_OBJECT_0;
#else
    if (!rwlock) return false;
    return pthread_rwlock_trywrlock((pthread_rwlock_t*)rwlock) == 0;
#endif
}

// 原子操作实现
int atomic_add(volatile int* ptr, int value) {
#ifdef _WIN32
    return InterlockedAdd((LONG*)ptr, value);
#else
    return __sync_fetch_and_add(ptr, value);
#endif
}

int atomic_sub(volatile int* ptr, int value) {
#ifdef _WIN32
    return InterlockedAdd((LONG*)ptr, -value);
#else
    return __sync_fetch_and_sub(ptr, value);
#endif
}

int atomic_exchange(volatile int* ptr, int value) {
#ifdef _WIN32
    return InterlockedExchange((LONG*)ptr, value);
#else
    return __sync_lock_test_and_set(ptr, value);
#endif
}

bool atomic_compare_exchange(volatile int* ptr, int expected, int desired) {
#ifdef _WIN32
    return InterlockedCompareExchange((LONG*)ptr, desired, expected) == expected;
#else
    return __sync_bool_compare_and_swap(ptr, expected, desired);
#endif
}

void atomic_store(volatile int* ptr, int value) {
#ifdef _WIN32
    InterlockedExchange((LONG*)ptr, value);
#else
    *ptr = value;
    __sync_synchronize();
#endif
}

int atomic_load(volatile int* ptr) {
#ifdef _WIN32
    return InterlockedCompareExchange((LONG*)ptr, 0, 0);
#else
    __sync_synchronize();
    return *ptr;
#endif
}

// 线程池实现
typedef struct ThreadPoolImpl {
    Thread* threads;
    size_t thread_count;
    Task* tasks;
    size_t task_count;
    size_t task_capacity;
    Mutex mutex;
    Condition condition;
    bool running;
} ThreadPoolImpl;

static void* thread_pool_worker(void* arg) {
    ThreadPoolImpl* pool = (ThreadPoolImpl*)arg;
    while (atomic_load((volatile int*)&pool->running)) {
        mutex_lock(pool->mutex);
        while (pool->task_count == 0 && atomic_load((volatile int*)&pool->running)) {
            condition_wait(pool->condition, pool->mutex);
        }
        if (!atomic_load((volatile int*)&pool->running)) {
            mutex_unlock(pool->mutex);
            break;
        }
        Task task = pool->tasks[0];
        memmove(&pool->tasks[0], &pool->tasks[1], (pool->task_count - 1) * sizeof(Task));
        pool->task_count--;
        mutex_unlock(pool->mutex);
        task.func(task.arg);
    }
    return NULL;
}

ThreadPool thread_pool_create(size_t thread_count) {
    ThreadPoolImpl* pool = (ThreadPoolImpl*)malloc(sizeof(ThreadPoolImpl));
    if (pool) {
        pool->thread_count = thread_count;
        pool->threads = (Thread*)malloc(thread_count * sizeof(Thread));
        pool->task_capacity = 1024;
        pool->tasks = (Task*)malloc(pool->task_capacity * sizeof(Task));
        pool->task_count = 0;
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
        atomic_store((volatile int*)&impl->running, 0);
        condition_broadcast(impl->condition);
        for (size_t i = 0; i < impl->thread_count; i++) {
            thread_join(impl->threads[i]);
        }
        mutex_destroy(impl->mutex);
        condition_destroy(impl->condition);
        free(impl->threads);
        free(impl->tasks);
        free(impl);
    }
}

void thread_pool_add_task(ThreadPool pool, Task task) {
    ThreadPoolImpl* impl = (ThreadPoolImpl*)pool;
    if (impl) {
        mutex_lock(impl->mutex);
        if (impl->task_count >= impl->task_capacity) {
            impl->task_capacity *= 2;
            impl->tasks = (Task*)realloc(impl->tasks, impl->task_capacity * sizeof(Task));
        }
        impl->tasks[impl->task_count++] = task;
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
            done = impl->task_count == 0;
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
    usleep(milliseconds * 1000);
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
