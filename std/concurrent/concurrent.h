#ifndef STD_CONCURRENT_CONCURRENT_H
#define STD_CONCURRENT_CONCURRENT_H

#include "../base/types.h"
#include "../../src/kaula.h"

// 线程类型
typedef void* Thread;
typedef void* (*ThreadFunction)(void*);

// 线程函数
extern Thread thread_create(ThreadFunction func, void* arg);
extern void thread_join(Thread thread);
extern void thread_detach(Thread thread);
extern Thread thread_self();
extern bool thread_equal(Thread t1, Thread t2);

// 互斥锁
typedef void* Mutex;

extern Mutex mutex_create();
extern void mutex_destroy(Mutex mutex);
extern void mutex_lock(Mutex mutex);
extern void mutex_unlock(Mutex mutex);
extern bool mutex_trylock(Mutex mutex);

// 条件变量
typedef void* Condition;

extern Condition condition_create();
extern void condition_destroy(Condition condition);
extern void condition_wait(Condition condition, Mutex mutex);
extern bool condition_timedwait(Condition condition, Mutex mutex, uint64_t timeout_ms);
extern void condition_signal(Condition condition);
extern void condition_broadcast(Condition condition);

// 信号量
typedef void* Semaphore;

extern Semaphore semaphore_create(uint32_t initial_value);
extern void semaphore_destroy(Semaphore semaphore);
extern void semaphore_wait(Semaphore semaphore);
extern bool semaphore_trywait(Semaphore semaphore);
extern bool semaphore_timedwait(Semaphore semaphore, uint64_t timeout_ms);
extern void semaphore_post(Semaphore semaphore);
extern uint32_t semaphore_get_value(Semaphore semaphore);

// 读写锁
typedef void* ReadWriteLock;

extern ReadWriteLock rwlock_create();
extern void rwlock_destroy(ReadWriteLock rwlock);
extern void rwlock_read_lock(ReadWriteLock rwlock);
extern void rwlock_read_unlock(ReadWriteLock rwlock);
extern void rwlock_write_lock(ReadWriteLock rwlock);
extern void rwlock_write_unlock(ReadWriteLock rwlock);
extern bool rwlock_try_read_lock(ReadWriteLock rwlock);
extern bool rwlock_try_write_lock(ReadWriteLock rwlock);

// 原子操作
extern int atomic_add(volatile int* ptr, int value);
extern int atomic_sub(volatile int* ptr, int value);
extern int atomic_exchange(volatile int* ptr, int value);
extern bool atomic_compare_exchange(volatile int* ptr, int expected, int desired);
extern void atomic_store(volatile int* ptr, int value);
extern int atomic_load(volatile int* ptr);

// 任务调度

typedef void* ThreadPool;

extern ThreadPool thread_pool_create(size_t thread_count);
extern void thread_pool_destroy(ThreadPool pool);
extern void thread_pool_add_task(ThreadPool pool, Task task);
extern void thread_pool_wait_completion(ThreadPool pool);

// 并发工具
extern void concurrent_sleep(uint32_t milliseconds);
extern uint64_t concurrent_get_thread_id();
extern size_t concurrent_get_processor_count();

#endif // STD_CONCURRENT_CONCURRENT_H