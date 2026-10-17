// freestanding/include/time.h — 裸机模式的 <time.h> 替身（仅原型）
//
// 语义边界：裸机没有系统时钟。std.time 的 time_now_ns / time_now_ms 在裸机
// 没有实现（需要用户接 PIT/HPET/RTC），这里只声明让引用能编译；
// freestanding.math 的 math_srand/math_rand 是纯软件 PRNG，与时钟无关。
#ifndef KAULA_FS_COMPAT_TIME_H
#define KAULA_FS_COMPAT_TIME_H

#include <stddef.h>
#include <sys/types.h>   /* time_t / clock_t（与 shim 保持一致） */

#ifdef __cplusplus
extern "C" {
#endif

#define CLOCKS_PER_SEC 1000000L

/*
 * 保护宏必须同时认 glibc / Linux 自己用的那几个（_STRUCT_TIMESPEC、
 * __timeval_defined），否则同一个 TU 里既包含本 shim 又（直接或间接）
 * 包含 glibc 的 <time.h> 时，struct timespec 会被定义两遍，
 * clang 直接报 "redefinition of 'timespec'"。
 *
 * 典型触发面：裸机程序 import 了 std.concurrent 之类会拉进
 * src/concurrency/kaula_conc_platform.h 的模块，而该头文件在非
 * KAULA_FREESTANDING 分支里用的是系统 <time.h>。""/<> 两种包含方式
 * 会解析到不同文件，光靠本 shim 自己的 KAULA_FS_COMPAT_TIME_H 挡不住。
 * 这里改为"谁的保护区先立起来就认谁的"，两种顺序都能编过。
 */
#if !defined(__DEFINED_struct_timespec) && !defined(_STRUCT_TIMESPEC)
#define __DEFINED_struct_timespec
#define _STRUCT_TIMESPEC 1
struct timespec { time_t tv_sec; long tv_nsec; };
#endif

#if !defined(__DEFINED_struct_timeval) && !defined(__timeval_defined)
#define __DEFINED_struct_timeval
#define __timeval_defined 1
struct timeval { time_t tv_sec; long tv_usec; };
#endif

struct tm {
    int tm_sec;
    int tm_min;
    int tm_hour;
    int tm_mday;
    int tm_mon;
    int tm_year;
    int tm_wday;
    int tm_yday;
    int tm_isdst;
    long __tm_gmtoff;
    const char* __tm_zone;
};

clock_t clock(void);
time_t time(time_t* t);
double difftime(time_t t1, time_t t0);
time_t mktime(struct tm* tm);
char* asctime(const struct tm* tm);
char* ctime(const time_t* t);
struct tm* gmtime(const time_t* t);
struct tm* localtime(const time_t* t);
size_t strftime(char* s, size_t max, const char* format, const struct tm* tm);
int timespec_get(struct timespec* ts, int base);
int nanosleep(const struct timespec* req, struct timespec* rem);

#ifdef __cplusplus
}
#endif

#endif /* KAULA_FS_COMPAT_TIME_H */
