#include "time.h"
#include <stdlib.h>
#include <string.h>
#include <stdio.h>

#if STD_PLATFORM_WINDOWS
    #include <windows.h>
#else
    #include <time.h>
    #include <sys/time.h>
#endif

static i64 _time_now_impl(void) {
#if STD_PLATFORM_WINDOWS
    FILETIME ft;
    GetSystemTimeAsFileTime(&ft);
    ULARGE_INTEGER uli;
    uli.LowPart = ft.dwLowDateTime;
    uli.HighPart = ft.dwHighDateTime;
    return (i64)(uli.QuadPart / 10000);
#else
    struct timeval tv;
    gettimeofday(&tv, NULL);
    return (i64)tv.tv_sec * 1000 + tv.tv_usec / 1000;
#endif
}

i64 time_now_i64(void) {
    return _time_now_impl();
}

double time_now_double(void) {
    return (double)_time_now_impl() / 1000.0;
}

i64 time_now_ms(void) {
    return _time_now_impl();
}

i64 time_now_us(void) {
#if STD_PLATFORM_WINDOWS
    FILETIME ft;
    GetSystemTimeAsFileTime(&ft);
    ULARGE_INTEGER uli;
    uli.LowPart = ft.dwLowDateTime;
    uli.HighPart = ft.dwHighDateTime;
    return (i64)(uli.QuadPart / 10);  // 100ns -> 微秒
#else
    struct timeval tv;
    gettimeofday(&tv, NULL);
    return (i64)tv.tv_sec * 1000000 + tv.tv_usec;
#endif
}

i64 time_now_ns(void) {
#if STD_PLATFORM_WINDOWS
    LARGE_INTEGER counter;
    QueryPerformanceCounter(&counter);
    static LARGE_INTEGER frequency = {0};
    if (frequency.QuadPart == 0) {
        QueryPerformanceFrequency(&frequency);
    }
    return (i64)(counter.QuadPart * 1000000000ULL / frequency.QuadPart);
#else
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (i64)ts.tv_sec * 1000000000ULL + ts.tv_nsec;
#endif
}

void time_sleep(i32 milliseconds) {
#if STD_PLATFORM_WINDOWS
    Sleep(milliseconds);
#else
    struct timespec ts;
    ts.tv_sec = milliseconds / 1000;
    ts.tv_nsec = (milliseconds % 1000) * 1000000;
    nanosleep(&ts, NULL);
#endif
}

void time_sleep_us(i32 microseconds) {
#if STD_PLATFORM_WINDOWS
    // Windows 最小睡眠精度约 1ms，使用 Sleep(0) 让出时间片
    if (microseconds >= 1000) {
        Sleep(microseconds / 1000);
    } else {
        // 亚毫秒级睡眠，使用自旋等待
        LARGE_INTEGER start, end, freq;
        QueryPerformanceCounter(&start);
        QueryPerformanceFrequency(&freq);
        i64 target = (i64)microseconds * freq.QuadPart / 1000000;
        do {
            QueryPerformanceCounter(&end);
        } while ((end.QuadPart - start.QuadPart) < target);
    }
#else
    struct timespec ts;
    ts.tv_sec = microseconds / 1000000;
    ts.tv_nsec = (microseconds % 1000000) * 1000;
    nanosleep(&ts, NULL);
#endif
}

void time_sleep_ns(i64 nanoseconds) {
#if STD_PLATFORM_WINDOWS
    // Windows 最小睡眠精度约 1ms，亚毫秒级使用自旋等待
    if (nanoseconds >= 1000000) {
        Sleep(nanoseconds / 1000000);
    } else {
        LARGE_INTEGER start, end, freq;
        QueryPerformanceCounter(&start);
        QueryPerformanceFrequency(&freq);
        i64 target = nanoseconds * freq.QuadPart / 1000000000ULL;
        do {
            QueryPerformanceCounter(&end);
        } while ((end.QuadPart - start.QuadPart) < target);
    }
#else
    struct timespec ts;
    ts.tv_sec = nanoseconds / 1000000000ULL;
    ts.tv_nsec = nanoseconds % 1000000000ULL;
    nanosleep(&ts, NULL);
#endif
}

void time_sleep_seconds(f64 seconds) {
    time_sleep((i32)(seconds * 1000));
}

TimeSpec time_get_spec(void) {
    TimeSpec ts;
#if STD_PLATFORM_WINDOWS
    FILETIME ft;
    GetSystemTimeAsFileTime(&ft);
    ULARGE_INTEGER uli;
    uli.LowPart = ft.dwLowDateTime;
    uli.HighPart = ft.dwHighDateTime;
    i64 total_nsec = (i64)uli.QuadPart * 100;
    ts.sec = total_nsec / 1000000000;
    ts.nsec = total_nsec % 1000000000;
#elif defined(CLOCK_MONOTONIC)
    struct timespec ts_now;
    clock_gettime(CLOCK_MONOTONIC, &ts_now);
    ts.sec = ts_now.tv_sec;
    ts.nsec = ts_now.tv_nsec;
#else
    struct timeval tv;
    gettimeofday(&tv, NULL);
    ts.sec = tv.tv_sec;
    ts.nsec = tv.tv_usec * 1000;
#endif
    return ts;
}

i64 time_diff_ms(TimeSpec start, TimeSpec end) {
    return (end.sec - start.sec) * 1000 + (end.nsec - start.nsec) / 1000000;
}

f64 time_diff_seconds(TimeSpec start, TimeSpec end) {
    return (f64)(end.sec - start.sec) + (f64)(end.nsec - start.nsec) / 1000000000.0;
}

static i32 _get_days_since_epoch(i32 year, i32 month, i32 day) {
    i32 days = 0;
    for (i32 y = 1970; y < year; y++) {
        days += 366;
        if (y % 4 == 0 && (y % 100 != 0 || y % 400 == 0)) {
            days--;
        }
    }
    static const i32 mdays[] = {0, 31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30};
    for (i32 m = 1; m < month; m++) {
        days += mdays[m];
    }
    if (month > 2 && year % 4 == 0 && (year % 100 != 0 || year % 400 == 0)) {
        days++;
    }
    days += day - 1;
    return days;
}

DateTime time_to_datetime(i64 timestamp) {
    DateTime dt;
    i64 secs = timestamp / 1000;
    i32 ms = (i32)(timestamp % 1000);

    i32 days = (i32)(secs / 86400);
    i32 remaining = (i32)(secs % 86400);

    dt.hour = remaining / 3600;
    remaining = remaining % 3600;
    dt.minute = remaining / 60;
    dt.second = remaining % 60;
    dt.millisecond = ms;

    i32 year = 1970;
    i32 days_left = days;
    while (days_left >= 365) {
        i32 days_in_year = 366;
        if (year % 4 != 0 || (year % 100 == 0 && year % 400 != 0)) {
            days_in_year = 365;
        }
        if (days_left < days_in_year) break;
        days_left -= days_in_year;
        year++;
    }

    i32 mdays[] = {0, 31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30};
    if (year % 4 == 0 && (year % 100 != 0 || year % 400 == 0)) {
        mdays[2] = 29;
    }

    i32 month = 1;
    while (month <= 12 && days_left >= mdays[month]) {
        days_left -= mdays[month];
        month++;
    }
    dt.year = year;
    dt.month = month;
    dt.day = days_left + 1;
    return dt;
}

i64 time_from_datetime(DateTime dt) {
    i32 days = _get_days_since_epoch(dt.year, dt.month, dt.day);
    i64 timestamp = (i64)days * 86400 + (i64)dt.hour * 3600 + (i64)dt.minute * 60 + (i64)dt.second;
    return timestamp * 1000 + dt.millisecond;
}

char* time_format_string(i64 timestamp, const char* format) {
    DateTime dt = time_to_datetime(timestamp);
    char* buffer = (char*)malloc(128);
    if (!buffer) return NULL;

    char* p = buffer;
    const char* fmt = format ? format : "%Y-%m-%d %H:%M:%S";

    while (*fmt) {
        if (*fmt == '%' && fmt[1]) {
            char spec = fmt[1];
            switch (spec) {
                case 'Y':
                    p += sprintf(p, "%04d", dt.year);
                    break;
                case 'm':
                    p += sprintf(p, "%02d", dt.month);
                    break;
                case 'd':
                    p += sprintf(p, "%02d", dt.day);
                    break;
                case 'H':
                    p += sprintf(p, "%02d", dt.hour);
                    break;
                case 'M':
                    p += sprintf(p, "%02d", dt.minute);
                    break;
                case 'S':
                    p += sprintf(p, "%02d", dt.second);
                    break;
                case 'f':
                    p += sprintf(p, "%03d", dt.millisecond);
                    break;
                default:
                    *p++ = *fmt;
                    *p++ = spec;
                    break;
            }
            fmt += 2;
        } else {
            *p++ = *fmt++;
        }
    }
    *p = '\0';
    return buffer;
}

char* time_format_now(const char* format) {
    return time_format_string(time_now_i64(), format);
}

static i32 _parse_int(const char* s, i32 digits) {
    i32 result = 0;
    for (i32 i = 0; i < digits && *s; i++, s++) {
        if (*s >= '0' && *s <= '9') {
            result = result * 10 + (*s - '0');
        }
    }
    return result;
}

i64 time_parse_string(const char* str, const char* format) {
    DateTime dt = {0};
    const char* s = str;
    const char* fmt = format ? format : "%Y-%m-%d %H:%M:%S";

    while (*fmt && *s) {
        if (*fmt == '%' && fmt[1]) {
            char spec = fmt[1];
            switch (spec) {
                case 'Y':
                    dt.year = _parse_int(s, 4);
                    s += 4;
                    break;
                case 'm':
                    dt.month = _parse_int(s, 2);
                    s += 2;
                    break;
                case 'd':
                    dt.day = _parse_int(s, 2);
                    s += 2;
                    break;
                case 'H':
                    dt.hour = _parse_int(s, 2);
                    s += 2;
                    break;
                case 'M':
                    dt.minute = _parse_int(s, 2);
                    s += 2;
                    break;
                case 'S':
                    dt.second = _parse_int(s, 2);
                    s += 2;
                    break;
                case 'f':
                    dt.millisecond = _parse_int(s, 3);
                    s += 3;
                    break;
                default:
                    s++;
                    break;
            }
            fmt += 2;
        } else {
            s++;
            fmt++;
        }
    }
    return time_from_datetime(dt);
}

static const char* _weekday_names[] = {"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"};

const char* time_weekday_name(i32 weekday) {
    if (weekday < 0 || weekday > 6) return "???";
    return _weekday_names[weekday];
}

static const char* _month_names[] = {"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"};

const char* time_month_name(i32 month) {
    if (month < 1 || month > 12) return "???";
    return _month_names[month - 1];
}