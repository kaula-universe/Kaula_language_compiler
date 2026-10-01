/*
 * Kaula 并发内核 · 平台抽象层
 *
 * 并发内核的所有 OS 依赖都收敛在这里，上层模块只调用 kaula_p_* 前缀的函数。
 * 后端选择遵循文档附录 B：
 *
 *   线程    Windows: CreateThread      Unix: pthread_create
 *   唤醒    Windows: SetEvent          Linux: eventfd      macOS: EVFILT_USER
 *   阻塞    Windows: WaitForSingleObj  Unix: pthread_cond_timedwait
 *   IO 复用 Windows: IOCP             Linux: epoll        macOS: kqueue
 *
 * 裸机（freestanding）不使用本头文件。
 */

#ifndef KAULA_CONC_PLATFORM_H
#define KAULA_CONC_PLATFORM_H

#include <stdint.h>
#include <stddef.h>
#include <stdbool.h>
#include <string.h>
#include <stdatomic.h>

/*
 * 平台宏。
 *
 * 直接复用 src/platform.h 的检测链，避免两处检测逻辑漂移。
 * toolkit_build.py 编译运行时时不定义这些宏，所以这里必须自带兜底 ——
 * 否则 KAULA_PLATFORM_WINDOWS 求值为 0，会在 Windows 上误拉 pthread.h。
 */
#ifndef KAULA_PLATFORM_WINDOWS
    #if defined(_WIN32) || defined(_WIN64)
        #define KAULA_PLATFORM_WINDOWS 1
    #elif defined(__EMSCRIPTEN__)
        #define KAULA_PLATFORM_WINDOWS 0
    #elif defined(__linux__)
        #define KAULA_PLATFORM_WINDOWS 0
    #elif defined(__APPLE__) && defined(__MACH__)
        #define KAULA_PLATFORM_WINDOWS 0
    #else
        #define KAULA_PLATFORM_WINDOWS 0
    #endif
#endif

#ifndef KAULA_PLATFORM_UNIX
    #if KAULA_PLATFORM_WINDOWS
        #define KAULA_PLATFORM_UNIX 0
    #else
        #define KAULA_PLATFORM_UNIX 1
    #endif
#endif

#ifndef KAULA_PLATFORM_LINUX
    #if defined(__linux__)
        #define KAULA_PLATFORM_LINUX 1
    #else
        #define KAULA_PLATFORM_LINUX 0
    #endif
#endif

#ifndef KAULA_PLATFORM_MACOS
    #if defined(__APPLE__) && defined(__MACH__)
        #define KAULA_PLATFORM_MACOS 1
    #else
        #define KAULA_PLATFORM_MACOS 0
    #endif
#endif

/* 缓存行大小：并发结构的热点字段按此对齐，避免 false sharing */
#ifndef KAULA_CACHE_LINE
    #if KAULA_PLATFORM_WINDOWS
        #define KAULA_CACHE_LINE 64
    #elif defined(__aarch64__) || defined(__arm64__)
        #define KAULA_CACHE_LINE 128
    #else
        #define KAULA_CACHE_LINE 64
    #endif
#endif

#if KAULA_PLATFORM_WINDOWS
    #ifndef WIN32_LEAN_AND_MEAN
        #define WIN32_LEAN_AND_MEAN
    #endif
    #ifndef NOMINMAX
        #define NOMINMAX
    #endif
    #include <windows.h>
#else
    #include <pthread.h>
    #include <unistd.h>
    #include <errno.h>
    #include <sys/time.h>
    #include <fcntl.h>
    #if defined(__linux__)
        #include <sys/epoll.h>
        #include <sys/eventfd.h>
    #elif defined(__APPLE__)
        #include <sys/event.h>
    #endif
#endif

/* WAIT_FOREVER 哨兵：表示无限等待 */
#define KAULA_P_WAIT_FOREVER 0xFFFFFFFFFFFFFFFFULL

/* 信号量上限：Windows CreateSemaphore 强制 LONG 参数 */
#define KAULA_P_SEM_MAX 0x7FFFFFFF

/*
 * 唤醒事件标签：塞进事件结构的 data 字段，用来把"唤醒"与"IO 就绪"区分开。
 * 约定：data == WAKE_TAG 即唤醒通道；data > 0 才是 fd。
 */
#if KAULA_PLATFORM_WINDOWS
    #define KAULA_P_WAKE_TAG ((ULONG_PTR)0)
#else
    #define KAULA_P_WAKE_TAG (-1)
#endif

/* ========== 单调时钟（纳秒） ========== */

#if KAULA_PLATFORM_WINDOWS
static inline uint64_t kaula_p_now_ns(void) {
    static LARGE_INTEGER freq;
    if (freq.QuadPart == 0) QueryPerformanceFrequency(&freq);
    LARGE_INTEGER c;
    QueryPerformanceCounter(&c);

    /*
     * 拆成 整秒 + 余数 两步，避免 c.QuadPart * 1e9 溢出 long long
     * （QPC 计数在现代 Windows 上是 10MHz 左右，运行时约 10^15，
     *  乘 10^9 就到了 10^24，远超 2^63）。UBSan 会直接报溢出。
     */
    uint64_t ticks = (uint64_t)c.QuadPart;
    uint64_t f = (uint64_t)freq.QuadPart;
    if (f == 0) return 0;

    uint64_t sec = ticks / f;
    uint64_t rem = ticks % f;
    return sec * 1000000000ULL + (rem * 1000000000ULL) / f;
}
#elif defined(CLOCK_MONOTONIC)
static inline uint64_t kaula_p_now_ns(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (uint64_t)ts.tv_sec * 1000000000ULL + (uint64_t)ts.tv_nsec;
}
#else
static inline uint64_t kaula_p_now_ns(void) {
    struct timeval tv;
    gettimeofday(&tv, NULL);
    return (uint64_t)tv.tv_sec * 1000000000ULL + (uint64_t)tv.tv_usec * 1000ULL;
}
#endif

/* ========== 休眠 ========== */

static inline void kaula_p_sleep_ns(uint64_t ns) {
#if KAULA_PLATFORM_WINDOWS
    Sleep((DWORD)((ns + 999999) / 1000000));
#else
    struct timespec ts;
    ts.tv_sec  = (time_t)(ns / 1000000000ULL);
    ts.tv_nsec = (long)(ns % 1000000000ULL);
    while (nanosleep(&ts, &ts) == -1 && errno == EINTR) {
        /* 被信号打断则续睡 */
    }
#endif
}

static inline void kaula_p_yield(void) {
#if KAULA_PLATFORM_WINDOWS
    Sleep(0);
#else
    sched_yield();
#endif
}

/* ========== 环境变量（文档 §12.2：KAULA_WORKERS 等覆盖） ========== */

#if KAULA_PLATFORM_WINDOWS
static inline const char* kaula_p_get_env(const char* name) {
    static __thread char buf[512];
    DWORD n = GetEnvironmentVariableA(name, buf, (DWORD)sizeof(buf));
    if (n > 0 && n < sizeof(buf)) return buf;
    return NULL;
}
#else
#include <stdlib.h>
static inline const char* kaula_p_get_env(const char* name) {
    return getenv(name);
}
#endif

/* ========== 线程 ========== */

/*
 * 线程入口签名统一为 void(*)(void*)：
 * Windows 的 LPTHREAD_START_ROUTINE 返回 DWORD，与 POSIX 不同，
 * 统一成 void 后上层不必写平台分支。
 */
typedef void (*kaula_p_thread_fn)(void*);

#if KAULA_PLATFORM_WINDOWS
typedef HANDLE kaula_p_thread_t;

typedef struct {
    kaula_p_thread_fn fn;
    void*             arg;
} kaula_p_trampoline;

static DWORD WINAPI kaula_p_thread_trampoline(LPVOID p) {
    kaula_p_trampoline* t = (kaula_p_trampoline*)p;
    kaula_p_thread_fn fn = t->fn;
    void* arg = t->arg;
    /* 释放 trampoline 后再调用户函数，避免用户代码长时间持有它 */
    LocalFree(t);
    fn(arg);
    return 0;
}

static inline int kaula_p_thread_create(kaula_p_thread_t* out, kaula_p_thread_fn fn, void* arg) {
    kaula_p_trampoline* t = (kaula_p_trampoline*)LocalAlloc(LMEM_FIXED, sizeof(kaula_p_trampoline));
    if (t == NULL) return -1;
    t->fn = fn;
    t->arg = arg;
    HANDLE h = CreateThread(NULL, 0, kaula_p_thread_trampoline, t, 0, NULL);
    if (h == NULL) {
        LocalFree(t);
        return -1;
    }
    *out = h;
    return 0;
}

static inline int kaula_p_thread_join(kaula_p_thread_t t) {
    WaitForSingleObject(t, INFINITE);
    CloseHandle(t);
    return 0;
}
#else
typedef pthread_t kaula_p_thread_t;

static inline int kaula_p_thread_create(kaula_p_thread_t* out, kaula_p_thread_fn fn, void* arg) {
    /* pthread 原生签名就是 void(*)(void*)，无需 trampoline */
    return pthread_create(out, NULL, fn, arg);
}

static inline int kaula_p_thread_join(kaula_p_thread_t t) {
    return pthread_join(t, NULL);
}
#endif

/* ========== 可唤醒的休眠原语 ========== */

/*
 * kaula_p_event：自动复位事件。
 * 线程 park() 上去，唤醒方 signal() 一次将其放行。
 * 代替文档 §2.3 的 park + 1ms 轮询超时，消除空转。
 *
 * 语义：signal() 累积计数。N 次 signal 后可成功 wait 最多 N 次。
 */
/*
 * 注意：Windows 下 HANDLE 恰好是指针大小，POSIX 下本类型是结构体。
 * 因此所有 event 函数一律按指针传递，不存在按值传参，
 * 否则两平台的签名会不一致。
 */
#if KAULA_PLATFORM_WINDOWS
typedef HANDLE kaula_p_event;

static inline int kaula_p_event_create(kaula_p_event* out) {
    /*
     * 用信号量而非事件：Windows 自动复位事件不保留 signal 计数
     * （两次 SetEvent 后只有一次 WaitForSingleObject 能通过），
     * 而 POSIX 侧的 pthread_cond 版本会累积。内核的 park/unpark 需要
     * "N 次 signal 对应最多 N 次唤醒" 的语义，两平台必须一致。
     */
    HANDLE h = CreateSemaphoreA(NULL, 0, KAULA_P_SEM_MAX, NULL);
    if (h == NULL) return -1;
    *out = h;
    return 0;
}
static inline void kaula_p_event_destroy(kaula_p_event* e) { CloseHandle(*e); }
static inline void kaula_p_event_signal(kaula_p_event* e) { ReleaseSemaphore(*e, 1, NULL); }
static inline void kaula_p_event_reset(kaula_p_event* e) {
    while (WaitForSingleObject(*e, 0) == WAIT_OBJECT_0) { /* 抽干 */ }
}

/* 返回 1 = 被唤醒，0 = 超时 */
static inline int kaula_p_event_wait(kaula_p_event* e, uint64_t timeout_ns) {
    DWORD ms = (timeout_ns == KAULA_P_WAIT_FOREVER)
             ? INFINITE
             : (DWORD)((timeout_ns + 999999) / 1000000);
    DWORD r = WaitForSingleObject(*e, ms);
    return (r == WAIT_OBJECT_0) ? 1 : 0;
}
#else
typedef struct {
    pthread_mutex_t m;
    pthread_cond_t  c;
    int             count;  /* 未被消费的 signal 计数 */
} kaula_p_event;

static inline int kaula_p_event_create(kaula_p_event* out) {
    memset(out, 0, sizeof(*out));
    if (pthread_mutex_init(&out->m, NULL) != 0) return -1;
    if (pthread_cond_init(&out->c, NULL) != 0) {
        pthread_mutex_destroy(&out->m);
        return -1;
    }
    return 0;
}

static inline void kaula_p_event_destroy(kaula_p_event* e) {
    pthread_cond_destroy(&e->c);
    pthread_mutex_destroy(&e->m);
}

static inline void kaula_p_event_signal(kaula_p_event* e) {
    pthread_mutex_lock(&e->m);
    e->count++;
    pthread_cond_signal(&e->c);
    pthread_mutex_unlock(&e->m);
}

static inline void kaula_p_event_reset(kaula_p_event* e) {
    pthread_mutex_lock(&e->m);
    e->count = 0;
    pthread_mutex_unlock(&e->m);
}

/* 返回 1 = 被唤醒，0 = 超时 */
static inline int kaula_p_event_wait(kaula_p_event* e, uint64_t timeout_ns) {
    struct timespec ts;
    clock_gettime(CLOCK_REALTIME, &ts);
    ts.tv_sec  += (time_t)(timeout_ns / 1000000000ULL);
    ts.tv_nsec += (long)(timeout_ns % 1000000000ULL);
    if (ts.tv_nsec >= 1000000000L) { ts.tv_sec++; ts.tv_nsec -= 1000000000L; }

    int rc;
    pthread_mutex_lock(&e->m);
    if (e->count > 0) {
        /* 有累积的 signal，不必进条件变量 */
        e->count--;
        pthread_mutex_unlock(&e->m);
        return 1;
    }
    if (timeout_ns == KAULA_P_WAIT_FOREVER) {
        while (e->count == 0) {
            rc = pthread_cond_wait(&e->c, &e->m);
            if (rc != 0) break;
        }
        if (e->count > 0) e->count--;
    } else {
        while (e->count == 0) {
            rc = pthread_cond_timedwait(&e->c, &e->m, &ts);
            if (rc != 0) break;
        }
        if (e->count > 0) e->count--;
    }
    pthread_mutex_unlock(&e->m);
    return (e->count >= 0 && rc == 0) ? 1 : 0;
}
#endif

/* ========== 互斥量（仅内核内部冷路径用） ========== */

#if KAULA_PLATFORM_WINDOWS
typedef CRITICAL_SECTION kaula_p_mutex;

static inline int  kaula_p_mutex_init(kaula_p_mutex* m)    { InitializeCriticalSection(m); return 0; }
static inline void kaula_p_mutex_destroy(kaula_p_mutex* m) { DeleteCriticalSection(m); }
static inline void kaula_p_mutex_lock(kaula_p_mutex* m)    { EnterCriticalSection(m); }
static inline void kaula_p_mutex_unlock(kaula_p_mutex* m)  { LeaveCriticalSection(m); }
#else
typedef pthread_mutex_t kaula_p_mutex;

static inline int  kaula_p_mutex_init(kaula_p_mutex* m)    { return pthread_mutex_init(m, NULL); }
static inline void kaula_p_mutex_destroy(kaula_p_mutex* m) { pthread_mutex_destroy(m); }
static inline void kaula_p_mutex_lock(kaula_p_mutex* m)    { pthread_mutex_lock(m); }
static inline void kaula_p_mutex_unlock(kaula_p_mutex* m)  { pthread_mutex_unlock(m); }
#endif

/* ========== IO 复用后端选择 ========== */

#if KAULA_PLATFORM_WINDOWS
    #define KAULA_P_IO_IOCP  1
    #define KAULA_P_IO_EPOLL 0
    #define KAULA_P_IO_KQUEUE 0
#elif defined(__linux__)
    #define KAULA_P_IO_IOCP  0
    #define KAULA_P_IO_EPOLL 1
    #define KAULA_P_IO_KQUEUE 0
#elif defined(__APPLE__)
    #define KAULA_P_IO_IOCP  0
    #define KAULA_P_IO_EPOLL 0
    #define KAULA_P_IO_KQUEUE 1
#else
    #error "Kaula 并发内核：当前平台没有 IO 复用后端"
#endif

/* IO 等待句柄：不透明，IoDriver 内部按后端解释 */
#if KAULA_PLATFORM_WINDOWS
typedef HANDLE kaula_p_io_wait_t;
#else
typedef int kaula_p_io_wait_t;
#endif

/* ========== IO 复用原语 ========== */

/*
 * 创建 IO 复用等待句柄。返回 0 成功，-1 失败。
 * 实现见 kaula_conc_io_backend.c（后端相关，故非内联）。
 */
int kaula_p_io_wait_create(kaula_p_io_wait_t* out);

/* 销毁 IO 复用等待句柄 */
void kaula_p_io_wait_destroy(kaula_p_io_wait_t w);

/* 设置 fd 为非阻塞。返回 0 成功 */
int kaula_p_io_set_nonblocking(int fd);

/* 唤醒 IO 驱动线程，使其从等待中返回。返回 0 成功 */
int kaula_p_io_wake(kaula_p_io_wait_t w);

/* 清空唤醒通道的待处理计数（读到 WAKE_TAG 事件后调用） */
void kaula_p_io_drain_wake(kaula_p_io_wait_t w);

#endif /* KAULA_CONC_PLATFORM_H */