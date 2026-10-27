// freestanding/include/sys/time.h — 裸机模式的 <sys/time.h> 替身（仅原型）
//
// 语义边界：裸机没有内核定时器/网络。std.time 与 std.web 的头会 include 它，
// 这里给出类型（timeval/timespec/timezone，宽度与 time.h shim 一致）与原型，
// 链接期未定义即"需要用户接 PIT/HPET 或没有网络"。
#ifndef KAULA_FS_COMPAT_SYS_TIME_H
#define KAULA_FS_COMPAT_SYS_TIME_H

#include <stddef.h>
#include <sys/types.h>

#ifdef __cplusplus
extern "C" {
#endif

#ifndef __DEFINED_struct_timezone
#define __DEFINED_struct_timezone
struct timezone { int tz_minuteswest; int tz_dsttime; };
#endif

struct timeval;   /* 定义在 time.h shim 里（含 struct timespec）*/

int gettimeofday(struct timeval* tv, void* tz);
int settimeofday(const struct timeval* tv, const struct timezone* tz);
int utimes(const char* path, const struct timeval times[2]);
int nanosleep(const struct timespec* req, struct timespec* rem);
int clock_gettime(int clk_id, struct timespec* tp);
int clock_getres(int clk_id, struct timespec* res);
int timer_create(void* timerid, int clockid, void* sevp);
int timer_settime(void* timerid, int flags, const void* new_value, void* old_value);

#ifdef __cplusplus
}
#endif

#endif /* KAULA_FS_COMPAT_SYS_TIME_H */
