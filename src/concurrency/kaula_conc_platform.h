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

/*
 * 定时器哨兵：epoll 后端把 timerfd 注册进来时用它做 data.fd，
 * 与真实 fd（>= 0）、唤醒哨兵（-1）都不冲突，驱动据此认出
 * 「这一轮是定时器到期」而不是 IO 就绪。
 *
 * 定义在这里而不是 io_backend.h：kaula_conc_io_primitives.c 只包含
 * platform.h，而它也要用这个标签来注册 timerfd。
 */
#define KAULA_P_TIMER_FD (-2)

/* ========== 单调时钟（纳秒） ========== */

#if KAULA_PLATFORM_WINDOWS
static BOOL CALLBACK kaula_p_qpc_freq_init(PINIT_ONCE once, PVOID param, PVOID* ctx) {
    (void)once; (void)ctx;
    QueryPerformanceFrequency((LARGE_INTEGER*)param);
    return TRUE;
}
static inline uint64_t kaula_p_now_ns(void) {
    static INIT_ONCE once = INIT_ONCE_STATIC_INIT;
    static LARGE_INTEGER freq;
    InitOnceExecuteOnce(&once, kaula_p_qpc_freq_init, &freq, NULL);
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
    if (clock_gettime(CLOCK_MONOTONIC, &ts) != 0) return 0;
    return (uint64_t)ts.tv_sec * 1000000000ULL + (uint64_t)ts.tv_nsec;
}
#else
static inline uint64_t kaula_p_now_ns(void) {
    struct timeval tv;
    gettimeofday(&tv, NULL);
    return (uint64_t)tv.tv_sec * 1000000000ULL + (uint64_t)tv.tv_usec * 1000ULL;
}
#endif

/* ========== 时钟分辨率 ========== */

/*
 * 把系统时钟粒度抬到 1ms，并在退出时还原。
 *
 * 为什么必须做：Windows 默认的系统时钟间隔是 15.625ms。
 * 任何带超时的等待（WaitForSingleObject / GetQueuedCompletionStatus /
 * Sleep）都会被向上量化到下一个时钟节拍 —— 请求 1ms 实际阻塞 15.6ms。
 *
 * 这对内核是系统性的：Worker park 超时 1ms、IoDriver 批等待、
 * 任务的完成事件等待，全都会被吃掉一个节拍。实测后果是每次
 * suspend/resume 往返多付约 20ms 固定税（与请求的睡眠时长无关），
 * 于是亚毫秒定时完全无法用。
 *
 * 只抬分辨率不改量化方向：亚毫秒请求仍应折成「不等待、立即重查」，
 * 见 kaula_conc_io_backend.c 的超时换算。
 *
 * 非 Windows 平台无需处理：POSIX 的条件变量超时是纳秒级的。
 */
#if KAULA_PLATFORM_WINDOWS
/* timeapi.h / winmm.lib。链接时需要 -lwinmm。 */
#include <timeapi.h>

static inline void kaula_p_timer_resolution_raise(void) {
    timeBeginPeriod(1);
}

static inline void kaula_p_timer_resolution_restore(void) {
    timeEndPeriod(1);
}
#else
static inline void kaula_p_timer_resolution_raise(void) {}
static inline void kaula_p_timer_resolution_restore(void) {}
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

#ifdef __cplusplus
extern "C" {
#endif
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

/*
 * 两侧的线程入口原生签名都跟这个不一样，所以**都要**垫 trampoline：
 *   Windows  LPTHREAD_START_ROUTINE = DWORD  WINAPI (*)(LPVOID)  —— 返回 DWORD
 *   POSIX    void *(*)(void *)                                —— 返回 void*
 * 统一成 void (*)(void*) 是为了让上层不写平台分支，代价就是这一层转换。
 */
typedef struct {
    kaula_p_thread_fn fn;
    void*             arg;
} kaula_p_trampoline;

#if KAULA_PLATFORM_WINDOWS
typedef HANDLE kaula_p_thread_t;

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

/* 判活/失效：与 POSIX 版同名同语义，调用方不依赖底层表示 */
static inline int  kaula_p_thread_is_valid(kaula_p_thread_t* t) { return *t != NULL; }
static inline void kaula_p_thread_invalidate(kaula_p_thread_t* t) { *t = NULL; }
#else
typedef pthread_t kaula_p_thread_t;

/*
 * pthread 原生签名是 void *(*)(void *)，与 kaula_p_thread_fn 的
 * void (*)(void *) **不兼容**。直接传 fn 是 incompatible pointer type，
 * 而且即使编译器放行也是未定义行为：线程入口的返回值会被调用方当 void* 用。
 *
 * 之前这里写着「pthread 原生签名就是 void(*)(void*)，无需 trampoline」——
 * 那句话是错的，本机只有 Windows 时看不出来，一到 Linux 编译就报错。
 */
static void* kaula_p_thread_trampoline(void* p) {
    kaula_p_trampoline* t = (kaula_p_trampoline*)p;
    kaula_p_thread_fn fn = t->fn;
    void* arg = t->arg;
    fn(arg);
    return NULL;
}

static inline int kaula_p_thread_create(kaula_p_thread_t* out, kaula_p_thread_fn fn, void* arg) {
    kaula_p_trampoline* t = (kaula_p_trampoline*)malloc(sizeof(kaula_p_trampoline));
    if (t == NULL) return -1;
    t->fn = fn;
    t->arg = arg;
    if (pthread_create(out, NULL, kaula_p_thread_trampoline, t) != 0) {
        free(t);
        return -1;
    }
    return 0;
}

static inline int kaula_p_thread_join(kaula_p_thread_t t) {
    return pthread_join(t, NULL);
}

/*
 * 与事件同一类的问题：pthread_t 在 glibc 是整型、在 macOS 是指针、
 * 在别的平台可能是结构体 —— `t != 0` / `t = 0` 并非到处都编得过。
 * 判活与失效收进平台层，调用方不依赖表示。
 */
static inline int  kaula_p_thread_is_valid(kaula_p_thread_t* t) { return *t != 0; }
static inline void kaula_p_thread_invalidate(kaula_p_thread_t* t) { *t = 0; }
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

/*
 * 事件是否已创建 / 置为未创建。
 *
 * 必须走这两个函数，不能直接写 `e != 0` 或 `e = 0`：
 * Windows 下 kaula_p_event 是 HANDLE（指针），可以比 0；
 * POSIX 下它是 pthread_cond 包装的结构体，`!= 0` 与 `= 0` 都编不过。
 * 把判活/失效收进平台层，调用方就与表示无关了。
 */
static inline int  kaula_p_event_is_valid(kaula_p_event* e) { return *e != NULL; }
static inline void kaula_p_event_invalidate(kaula_p_event* e) { *e = NULL; }

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
    int             count;   /* 未被消费的 signal 计数 */
    int             created; /* 判活标志：结构体没法用 != 0 表达「已创建」 */
} kaula_p_event;

static inline int kaula_p_event_create(kaula_p_event* out) {
    memset(out, 0, sizeof(*out));
    if (pthread_mutex_init(&out->m, NULL) != 0) return -1;
    /*
     * 条件变量必须挂在 CLOCK_MONOTONIC 上：超时计算（worker_park 的
     * min(1ms, 最近到期剩余) 等）全部以 kaula_p_now_ns()=MONOTONIC
     * 为基准。默认 REALTIME 会被 host 时钟调整（NTP/手动改时间）
     * 打断 —— VM 里偶发睡过头，本地定时器到期没人 check，main 的
     * await 永远等不到 → 偶发挂死（实测 5%~13% 复现率）。
     */
    pthread_condattr_t attr;
    pthread_condattr_init(&attr);
    pthread_condattr_setclock(&attr, CLOCK_MONOTONIC);
    int rc = pthread_cond_init(&out->c, &attr);
    pthread_condattr_destroy(&attr);
    if (rc != 0) {
        pthread_mutex_destroy(&out->m);
        return -1;
    }
    out->created = 1;
    return 0;
}

/* 与 Windows 版同名同语义：调用方不该知道底下是 HANDLE 还是 pthread_cond */
static inline int  kaula_p_event_is_valid(kaula_p_event* e) { return e->created; }
static inline void kaula_p_event_invalidate(kaula_p_event* e) { e->created = 0; }

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
    clock_gettime(CLOCK_MONOTONIC, &ts);  /* 与 condattr 及 kaula_p_now_ns 一致 */
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

/*
 * IO 等待句柄：不透明，IoDriver 内部按后端解释。
 *
 * POSIX 上是**两个** fd，不是��个：
 *   epoll 需要 epoll fd + 一个可写的唤醒通道（eventfd）。
 *   之前只存了 epoll fd，而 kaula_p_io_wake 往 epoll fd 上 write ——
 *   epoll fd 根本不可写，write 返回 EINVAL，于是**唤醒在 Linux 上整个失效**，
 *   驱动只能等下一次轮询超时。实测表现为挂起事务的服务时间从 4.6ms
 *   变成 52.9ms（13 倍），且 kaula_p_io_wake 直接返回 -1。
 *
 *   kqueue 只需一个：EVFILT_USER 挂在 kq 自己身上，无需第二个 fd，
 *   所以那边 wakefd 恒为 -1。
 */
#if KAULA_PLATFORM_WINDOWS
typedef HANDLE kaula_p_io_wait_t;
static inline int  kaula_p_io_wait_epfd(kaula_p_io_wait_t w) { return (int)(intptr_t)w; }
static inline int  kaula_p_io_wait_timerfd(kaula_p_io_wait_t w) { return -1; }
static inline int  kaula_p_io_wait_is_valid(kaula_p_io_wait_t* w) { return *w != NULL; }
static inline void kaula_p_io_wait_invalidate(kaula_p_io_wait_t* w) { *w = NULL; }
#else
typedef struct {
    int epfd;     /* epoll fd 或 kqueue fd */
    int wakefd;   /* epoll 专用：eventfd；kqueue 恒为 -1 */
    int timer_fd; /* epoll 专用：timerfd；kqueue 恒为 -1 */
} kaula_p_io_wait_t;
static inline int  kaula_p_io_wait_epfd(kaula_p_io_wait_t w) { return w.epfd; }
static inline int  kaula_p_io_wait_timerfd(kaula_p_io_wait_t w) { return w.timer_fd; }
static inline int  kaula_p_io_wait_is_valid(kaula_p_io_wait_t* w) { return w->epfd >= 0; }
static inline void kaula_p_io_wait_invalidate(kaula_p_io_wait_t* w) { w->epfd = -1; w->wakefd = -1; w->timer_fd = -1; }
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

/*
 * 武装定时器：delay_ns 后触发一次（相对现在）。
 * delay_ns == 0 或 KAULA_P_WAIT_FOREVER 表示解除武装。返回 0 成功。
 *
 * 存在的理由：epoll_wait 的超时以毫秒为单位，亚毫秒定时只能向上取整
 * （实测 200us 定时额外开销 1030us）。交给 timerfd 之后 epoll_wait 可以
 * 无限阻塞，精度由内核定时器保证，不再受毫秒粒度限制。
 *
 * 只有 epoll 后端真正实现；kqueue 与 IOCP 是空实现（返回 0），
 * 那两边仍靠 wait 超时 —— 亚毫秒精度是那边的已知边界。
 */
int kaula_p_io_arm_timer(kaula_p_io_wait_t w, uint64_t delay_ns);


#ifdef __cplusplus
}
#endif
#endif /* KAULA_CONC_PLATFORM_H */