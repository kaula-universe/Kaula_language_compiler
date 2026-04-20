#ifndef STD_TIME_TIME_H
#define STD_TIME_TIME_H

#include "../base/types.h"

#if STD_PLATFORM_WINDOWS
    #include <windows.h>
    #include <sys/timeb.h>
#else
    #include <time.h>
    #include <sys/time.h>
#endif

typedef struct {
    i64 sec;
    i64 nsec;
} TimeSpec;

typedef struct {
    i32 year;
    i32 month;
    i32 day;
    i32 hour;
    i32 minute;
    i32 second;
    i32 millisecond;
} DateTime;

extern i64 time_now_i64(void);
extern double time_now_double(void);
extern i64 time_now_ms(void);
extern i64 time_now_us(void);  // 微秒级时间戳
extern i64 time_now_ns(void);  // 纳秒级时间戳
extern void time_sleep(i32 milliseconds);
extern void time_sleep_us(i32 microseconds);  // 微秒级睡眠
extern void time_sleep_ns(i64 nanoseconds);   // 纳秒级睡眠
extern TimeSpec time_get_spec(void);
extern i64 time_diff_ms(TimeSpec start, TimeSpec end);
extern f64 time_diff_seconds(TimeSpec start, TimeSpec end);

extern DateTime time_to_datetime(i64 timestamp);
extern i64 time_from_datetime(DateTime dt);

extern char* time_format_string(i64 timestamp, const char* format);
extern char* time_format_now(const char* format);
extern i64 time_parse_string(const char* str, const char* format);

extern const char* time_weekday_name(i32 weekday);
extern const char* time_month_name(i32 month);

#endif // STD_TIME_TIME_H