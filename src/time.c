#include "kaula.h"

#if KAULA_PLATFORM_WINDOWS
    #include <windows.h>
#else
    #include <time.h>
    #include <sys/time.h>
#endif

// ==================== 跨平台高精度时间测量 ====================

#if KAULA_PLATFORM_WINDOWS
// Windows: 使用 QueryPerformanceCounter
static inline uint64_t get_clock_cycles(void) {
    LARGE_INTEGER counter;
    QueryPerformanceCounter(&counter);
    return counter.QuadPart;
}

static inline double get_clock_frequency(void) {
    LARGE_INTEGER frequency;
    QueryPerformanceFrequency(&frequency);
    return (double)frequency.QuadPart;
}

static inline double time_now_impl(void) {
    LARGE_INTEGER counter;
    QueryPerformanceCounter(&counter);
    double freq = get_clock_frequency();
    return (freq > 0.0) ? (double)counter.QuadPart / freq : 0.0;
}

static inline void time_sleep_impl(double seconds) {
    if (seconds > 0.0) {
        DWORD ms = (DWORD)(seconds * 1000.0);
        Sleep(ms);
    }
}

#else
// Unix/Linux/macOS: 使用 clock_gettime 或 gettimeofday
static inline uint64_t get_clock_cycles(void) {
#if defined(CLOCK_MONOTONIC)
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (uint64_t)ts.tv_sec * 1000000000ULL + (uint64_t)ts.tv_nsec;
#else
    struct timeval tv;
    gettimeofday(&tv, NULL);
    return (uint64_t)tv.tv_sec * 1000000000ULL + (uint64_t)tv.tv_usec * 1000ULL;
#endif
}

static inline double get_clock_frequency(void) {
    return 1000000000.0; // 纳秒频率
}

static inline double time_now_impl(void) {
#if defined(CLOCK_MONOTONIC)
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (double)ts.tv_sec + (double)ts.tv_nsec / 1000000000.0;
#else
    struct timeval tv;
    gettimeofday(&tv, NULL);
    return (double)tv.tv_sec + (double)tv.tv_usec / 1000000.0;
#endif
}

static inline void time_sleep_impl(double seconds) {
    if (seconds > 0.0) {
#if defined(CLOCK_MONOTONIC)
        struct timespec ts;
        ts.tv_sec = (time_t)seconds;
        ts.tv_nsec = (long)((seconds - (double)ts.tv_sec) * 1000000000.0);
        nanosleep(&ts, NULL);
#else
        struct timeval tv;
        tv.tv_sec = (time_t)seconds;
        tv.tv_usec = (suseconds_t)((seconds - (double)tv.tv_sec) * 1000000.0);
        select(0, NULL, NULL, NULL, &tv);
#endif
    }
}
#endif

// ==================== 公开 API 实现 ====================

KAULA_EXPORT double time_now(TimeModule* tm) {
    (void)tm; // 暂未使用
    return time_now_impl();
}

KAULA_EXPORT void time_sleep(TimeModule* tm, double seconds) {
    (void)tm; // 暂未使用
    time_sleep_impl(seconds);
}