#ifndef KAULA_PLATFORM_H
#define KAULA_PLATFORM_H

/**
 * @file platform.h
 * @brief Kaula 跨平台支持头文件
 * 
 * 提供统一的跨平台宏和工具函数，支持：
 * - Windows (32/64 位)
 * - Linux (32/64 位)
 * - macOS (Intel/ARM)
 */

// Platform detection macros (defined in kaula.h)
// This header should be included AFTER kaula.h has defined the platform macros
#ifndef KAULA_PLATFORM_WINDOWS
#if defined(_WIN32) || defined(_WIN64)
    #define KAULA_PLATFORM_WINDOWS 1
    #define KAULA_PLATFORM_UNIX 0
    #define KAULA_PLATFORM_LINUX 0
    #define KAULA_PLATFORM_MACOS 0
#elif defined(__linux__)
    #define KAULA_PLATFORM_WINDOWS 0
    #define KAULA_PLATFORM_UNIX 1
    #define KAULA_PLATFORM_LINUX 1
    #define KAULA_PLATFORM_MACOS 0
#elif defined(__APPLE__) && defined(__MACH__)
    #define KAULA_PLATFORM_WINDOWS 0
    #define KAULA_PLATFORM_UNIX 1
    #define KAULA_PLATFORM_LINUX 0
    #define KAULA_PLATFORM_MACOS 1
#else
    #define KAULA_PLATFORM_WINDOWS 0
    #define KAULA_PLATFORM_UNIX 1
    #define KAULA_PLATFORM_LINUX 0
    #define KAULA_PLATFORM_MACOS 0
#endif
#endif

// ==================== 平台特定头文件 ====================
#if KAULA_PLATFORM_WINDOWS || defined(_WIN32) || defined(_WIN64) || defined(__MINGW32__)
    #ifndef WIN32_LEAN_AND_MEAN
        #define WIN32_LEAN_AND_MEAN
    #endif
    #include <windows.h>
    #include <winsock2.h>
    #pragma comment(lib, "ws2_32.lib")  // Windows Sockets
#elif KAULA_PLATFORM_LINUX
    #include <pthread.h>
    #include <sys/time.h>
    #include <unistd.h>
    #include <dlfcn.h>
#elif KAULA_PLATFORM_MACOS
    #include <pthread.h>
    #include <sys/time.h>
    #include <unistd.h>
    #include <mach/mach_time.h>
#endif

// ==================== 跨平台线程支持 ====================
#if KAULA_PLATFORM_WINDOWS
    typedef HANDLE kaula_thread_t;
    typedef DWORD kaula_thread_id_t;
    #define KAULA_THREAD_LOCAL __declspec(thread)
#elif KAULA_PLATFORM_UNIX
    typedef pthread_t kaula_thread_t;
    typedef pthread_t kaula_thread_id_t;
    #define KAULA_THREAD_LOCAL __thread
#endif

// ==================== 跨平台原子操作 ====================
#if KAULA_PLATFORM_WINDOWS
    #include <intrin.h>
    #define KAULA_ATOMIC_INC(ptr) _InterlockedIncrement((LONG*)(ptr))
    #define KAULA_ATOMIC_DEC(ptr) _InterlockedDecrement((LONG*)(ptr))
    #define KAULA_ATOMIC_ADD(ptr, val) _InterlockedAdd((LONG*)(ptr), (val))
    #define KAULA_ATOMIC_CAS(ptr, old, new) \
        (_InterlockedCompareExchange((LONG*)(ptr), (new), (old)) == (old))
#elif KAULA_PLATFORM_UNIX
    #include <stdatomic.h>
    #define KAULA_ATOMIC_INC(ptr) atomic_fetch_add((atomic_int*)(ptr), 1)
    #define KAULA_ATOMIC_DEC(ptr) atomic_fetch_sub((atomic_int*)(ptr), 1)
    #define KAULA_ATOMIC_ADD(ptr, val) atomic_fetch_add((atomic_int*)(ptr), (val))
    #define KAULA_ATOMIC_CAS(ptr, old, new) \
        atomic_compare_exchange_strong((atomic_int*)(ptr), (old), (new))
#endif

// ==================== 跨平台文件路径 ====================
#if KAULA_PLATFORM_WINDOWS
    #define KAULA_PATH_SEPARATOR '\\'
    #define KAULA_PATH_SEPARATOR_STR "\\"
#else
    #define KAULA_PATH_SEPARATOR '/'
    #define KAULA_PATH_SEPARATOR_STR "/"
#endif

// ==================== 跨平台动态库加载 ====================
#if KAULA_PLATFORM_WINDOWS
    typedef HMODULE kaula_lib_t;
    #define KAULA_LIB_LOAD(path) LoadLibraryA(path)
    #define KAULA_LIB_UNLOAD(lib) FreeLibrary(lib)
    #define KAULA_LIB_GET_SYM(lib, name) GetProcAddress(lib, name)
#else
    typedef void* kaula_lib_t;
    #define KAULA_LIB_LOAD(path) dlopen(path, RTLD_NOW)
    #define KAULA_LIB_UNLOAD(lib) dlclose(lib)
    #define KAULA_LIB_GET_SYM(lib, name) dlsym(lib, name)
#endif

// ==================== 跨平台内存屏障 ====================
#if KAULA_PLATFORM_WINDOWS
    #define KAULA_MEMORY_BARRIER() _mm_mfence()
    #define KAULA_READ_BARRIER() _mm_lfence()
    #define KAULA_WRITE_BARRIER() _mm_sfence()
#elif KAULA_PLATFORM_UNIX
    #if defined(__x86_64__) || defined(__i386__)
        #define KAULA_MEMORY_BARRIER() __asm__ __volatile__("mfence" ::: "memory")
        #define KAULA_READ_BARRIER() __asm__ __volatile__("lfence" ::: "memory")
        #define KAULA_WRITE_BARRIER() __asm__ __volatile__("sfence" ::: "memory")
    #elif defined(__aarch64__) || defined(__arm__)
        #define KAULA_MEMORY_BARRIER() __asm__ __volatile__("dmb ish" ::: "memory")
        #define KAULA_READ_BARRIER() __asm__ __volatile__("dmb ishld" ::: "memory")
        #define KAULA_WRITE_BARRIER() __asm__ __volatile__("dmb ishst" ::: "memory")
    #else
        #define KAULA_MEMORY_BARRIER() __sync_synchronize()
        #define KAULA_READ_BARRIER() __sync_synchronize()
        #define KAULA_WRITE_BARRIER() __sync_synchronize()
    #endif
#endif

// ==================== 跨平台性能计数器 ====================
static inline uint64_t kaula_get_ticks(void) {
#if KAULA_PLATFORM_WINDOWS
    LARGE_INTEGER counter;
    QueryPerformanceCounter(&counter);
    return (uint64_t)counter.QuadPart;
#elif KAULA_PLATFORM_MACOS
    return mach_absolute_time();
#elif defined(CLOCK_MONOTONIC)
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (uint64_t)ts.tv_sec * 1000000000ULL + (uint64_t)ts.tv_nsec;
#elif defined(_WIN32) || defined(__MINGW32__)
    LARGE_INTEGER counter;
    QueryPerformanceCounter(&counter);
    return (uint64_t)counter.QuadPart;
#else
    struct timeval tv;
    gettimeofday(&tv, NULL);
    return (uint64_t)tv.tv_sec * 1000000000ULL + (uint64_t)tv.tv_usec * 1000ULL;
#endif
}

static inline double kaula_get_tick_frequency(void) {
#if KAULA_PLATFORM_WINDOWS
    LARGE_INTEGER frequency;
    QueryPerformanceFrequency(&frequency);
    return (double)frequency.QuadPart;
#elif KAULA_PLATFORM_MACOS
    static mach_timebase_info_data_t info = {0, 0};
    if (info.denom == 0) {
        mach_timebase_info(&info);
    }
    return (double)info.numer / (double)info.denom;
#else
    return 1000000000.0; // 纳秒
#endif
}

// ==================== 跨平台睡眠函数 ====================
static inline void kaula_sleep_ms(int milliseconds) {
#if KAULA_PLATFORM_WINDOWS
    Sleep((DWORD)milliseconds);
#elif defined(CLOCK_MONOTONIC)
    struct timespec ts;
    ts.tv_sec = milliseconds / 1000;
    ts.tv_nsec = (milliseconds % 1000) * 1000000;
    nanosleep(&ts, NULL);
#else
    #if defined(_WIN32) || defined(__MINGW32__)
        Sleep((DWORD)milliseconds);
    #else
        usleep(milliseconds * 1000);
    #endif
#endif
}

static inline void kaula_sleep_us(int microseconds) {
#if KAULA_PLATFORM_WINDOWS
    LARGE_INTEGER frequency, start, target;
    QueryPerformanceFrequency(&frequency);
    QueryPerformanceCounter(&start);
    target.QuadPart = start.QuadPart + (frequency.QuadPart * microseconds / 1000000);
    while (1) {
        QueryPerformanceCounter(&start);
        if (start.QuadPart >= target.QuadPart) break;
    }
#else
    #if defined(_WIN32) || defined(__MINGW32__)
        // Windows 微秒级睡眠
        LARGE_INTEGER frequency, start, target;
        QueryPerformanceFrequency(&frequency);
        QueryPerformanceCounter(&start);
        target.QuadPart = start.QuadPart + (frequency.QuadPart * microseconds / 1000000);
        while (1) {
            QueryPerformanceCounter(&start);
            if (start.QuadPart >= target.QuadPart) break;
        }
    #else
        usleep(microseconds);
    #endif
#endif
}

// ==================== 跨平台环境变量 ====================
static inline const char* kaula_get_env(const char* name) {
#if KAULA_PLATFORM_WINDOWS
    static char buffer[1024];
    if (GetEnvironmentVariableA(name, buffer, sizeof(buffer)) > 0) {
        return buffer;
    }
    return NULL;
#else
    return getenv(name);
#endif
}

static inline int kaula_set_env(const char* name, const char* value) {
#if KAULA_PLATFORM_WINDOWS
    return SetEnvironmentVariableA(name, value) ? 0 : -1;
#elif defined(_WIN32) || defined(__MINGW32__)
    return SetEnvironmentVariableA(name, value) ? 0 : -1;
#else
    return setenv(name, value, 1);
#endif
}

#endif // KAULA_PLATFORM_H
