#include "datetime.h"
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <stdio.h>

#if defined(_WIN32)
#include <windows.h>

static char* strptime(const char* buf, const char* format, struct tm* tm) {
    if (!buf || !format || !tm) return NULL;
    
    i64 year = 0, month = 0, day = 0;
    while (*format) {
        if (*format == '%') {
            format++;
            switch (*format) {
                case 'Y': sscanf(buf, "%lld", &year); break;
                case 'm': sscanf(buf, "%lld", &month); break;
                case 'd': sscanf(buf, "%lld", &day); break;
                case 'H': sscanf(buf, "%lld", (i64*)&tm->tm_hour); break;
                case 'M': sscanf(buf, "%lld", (i64*)&tm->tm_min); break;
                case 'S': sscanf(buf, "%lld", (i64*)&tm->tm_sec); break;
            }
            while (*buf && *buf != '-' && *buf != '/' && *buf != ' ' && *buf != ':') buf++;
            if (*buf) buf++;
        } else {
            buf++;
        }
        format++;
    }
    
    if (year > 0) tm->tm_year = (int)(year - 1900);
    if (month > 0) tm->tm_mon = (int)(month - 1);
    if (day > 0) tm->tm_mday = (int)day;
    
    return (char*)buf;
}
#endif

DateTime datetime_now(void) {
    return datetime_from_timestamp_ns(0);
}

DateTime datetime_now_utc(void) {
    DateTime dt;
    memset(&dt, 0, sizeof(DateTime));
    
#if defined(_WIN32)
    FILETIME ft;
    GetSystemTimeAsFileTime(&ft);
    i64 ticks = ((i64)ft.dwHighDateTime << 32) | ft.dwLowDateTime;
    ticks -= 116444736000000000LL;
    dt.second = ticks / 10000000LL;
    dt.nanosecond = (ticks % 10000000LL) * 100;
#else
    struct timespec ts;
    clock_gettime(CLOCK_REALTIME, &ts);
    dt.second = ts.tv_sec;
    dt.nanosecond = ts.tv_nsec;
#endif
    
    time_t t = (time_t)dt.second;
    struct tm* tm_info = gmtime(&t);
    
    dt.year = tm_info->tm_year + 1900;
    dt.month = tm_info->tm_mon + 1;
    dt.day = tm_info->tm_mday;
    dt.hour = tm_info->tm_hour;
    dt.minute = tm_info->tm_min;
    
    return dt;
}

DateTime datetime_from_timestamp(i64 timestamp_ms) {
    return datetime_from_timestamp_ns(timestamp_ms * 1000000LL);
}

DateTime datetime_from_timestamp_ns(i64 timestamp_ns) {
    DateTime dt;
    memset(&dt, 0, sizeof(DateTime));
    
    if (timestamp_ns == 0) {
#if defined(_WIN32)
        FILETIME ft;
        GetSystemTimeAsFileTime(&ft);
        i64 ticks = ((i64)ft.dwHighDateTime << 32) | ft.dwLowDateTime;
        ticks -= 116444736000000000LL;
        dt.second = ticks / 10000000LL;
        dt.nanosecond = (ticks % 10000000LL) * 100;
#else
        struct timespec ts;
        clock_gettime(CLOCK_REALTIME, &ts);
        dt.second = ts.tv_sec;
        dt.nanosecond = ts.tv_nsec;
#endif
    } else {
        dt.second = timestamp_ns / 1000000000LL;
        dt.nanosecond = timestamp_ns % 1000000000LL;
    }
    
    time_t t = (time_t)dt.second;
    struct tm* tm_info = localtime(&t);
    
    dt.year = tm_info->tm_year + 1900;
    dt.month = tm_info->tm_mon + 1;
    dt.day = tm_info->tm_mday;
    dt.hour = tm_info->tm_hour;
    dt.minute = tm_info->tm_min;
    
    return dt;
}

i64 datetime_to_timestamp(const DateTime* dt) {
    if (!dt) return 0;
    
    struct tm tm_info = {0};
    tm_info.tm_year = (int)(dt->year - 1900);
    tm_info.tm_mon = (int)(dt->month - 1);
    tm_info.tm_mday = (int)dt->day;
    tm_info.tm_hour = (int)dt->hour;
    tm_info.tm_min = (int)dt->minute;
    tm_info.tm_sec = (int)dt->second;
    
    time_t t = mktime(&tm_info);
    return (i64)t * 1000LL;
}

i64 datetime_to_timestamp_ns(const DateTime* dt) {
    if (!dt) return 0;
    return datetime_to_timestamp(dt) * 1000000LL + dt->nanosecond;
}

DateTime datetime_add(const DateTime* dt, const Duration* dur) {
    if (!dt || !dur) { DateTime zero = {0}; return zero; }
    
    DateTime result = *dt;
    result.nanosecond += dur->nanoseconds;
    if (result.nanosecond >= 1000000000LL) { result.second++; result.nanosecond -= 1000000000LL; }
    if (result.nanosecond < 0) { result.second--; result.nanosecond += 1000000000LL; }
    
    result.second += dur->seconds;
    result.minute += dur->minutes;
    result.hour += dur->hours;
    result.day += dur->days;
    
    result.minute += result.second / 60;
    result.second %= 60;
    if (result.second < 0) { result.second += 60; result.minute--; }
    
    result.hour += result.minute / 60;
    result.minute %= 60;
    if (result.minute < 0) { result.minute += 60; result.hour--; }
    
    result.day += result.hour / 24;
    result.hour %= 24;
    if (result.hour < 0) { result.hour += 24; result.day--; }
    
    while (result.day > 0) {
        i64 days_in_month[] = {31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31};
        i64 dim = days_in_month[result.month - 1];
        if (result.month == 2) {
            if ((result.year % 4 == 0 && result.year % 100 != 0) || result.year % 400 == 0) {
                dim = 29;
            }
        }
        if (result.day <= dim) break;
        result.day -= dim;
        result.month++;
        if (result.month > 12) { result.month = 1; result.year++; }
    }
    
    while (result.day <= 0) {
        result.month--;
        if (result.month <= 0) { result.month = 12; result.year--; }
        i64 days_in_month[] = {31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31};
        i64 dim = days_in_month[result.month - 1];
        if (result.month == 2) {
            if ((result.year % 4 == 0 && result.year % 100 != 0) || result.year % 400 == 0) {
                dim = 29;
            }
        }
        result.day += dim;
    }
    
    return result;
}

DateTime datetime_subtract(const DateTime* dt, const Duration* dur) {
    if (!dt || !dur) { DateTime zero = {0}; return zero; }
    
    Duration neg = {
        -dur->days, -dur->hours, -dur->minutes, -dur->seconds, -dur->nanoseconds
    };
    return datetime_add(dt, &neg);
}

Duration datetime_diff(const DateTime* a, const DateTime* b) {
    if (!a || !b) { Duration zero = {0}; return zero; }
    
    i64 ns_a = datetime_to_timestamp_ns(a);
    i64 ns_b = datetime_to_timestamp_ns(b);
    i64 diff = ns_a - ns_b;
    
    return duration_from_nanoseconds(diff);
}

bool_t datetime_equal(const DateTime* a, const DateTime* b) {
    if (!a || !b) return false;
    return datetime_to_timestamp_ns(a) == datetime_to_timestamp_ns(b);
}

bool_t datetime_less(const DateTime* a, const DateTime* b) {
    if (!a || !b) return false;
    return datetime_to_timestamp_ns(a) < datetime_to_timestamp_ns(b);
}

bool_t datetime_greater(const DateTime* a, const DateTime* b) {
    if (!a || !b) return false;
    return datetime_to_timestamp_ns(a) > datetime_to_timestamp_ns(b);
}

String datetime_to_string(const DateTime* dt, const char* format) {
    if (!dt || !format) return NULL;
    
    char buffer[256];
    struct tm tm_info = {0};
    tm_info.tm_year = (int)(dt->year - 1900);
    tm_info.tm_mon = (int)(dt->month - 1);
    tm_info.tm_mday = (int)dt->day;
    tm_info.tm_hour = (int)dt->hour;
    tm_info.tm_min = (int)dt->minute;
    tm_info.tm_sec = (int)dt->second;
    
    strftime(buffer, sizeof(buffer), format, &tm_info);
    return string_copy(buffer);
}

DateTime datetime_from_string(const char* str, const char* format) {
    DateTime dt = {0};
    if (!str || !format) return dt;
    
    struct tm tm_info = {0};
    strptime(str, format, &tm_info);
    
    dt.year = tm_info.tm_year + 1900;
    dt.month = tm_info.tm_mon + 1;
    dt.day = tm_info.tm_mday;
    dt.hour = tm_info.tm_hour;
    dt.minute = tm_info.tm_min;
    dt.second = tm_info.tm_sec;
    
    return dt;
}

String datetime_to_iso8601(const DateTime* dt) {
    if (!dt) return NULL;
    
    char buffer[64];
    snprintf(buffer, sizeof(buffer), "%04lld-%02lld-%02lldT%02lld:%02lld:%02lld.%09lldZ",
             (long long)dt->year, (long long)dt->month, (long long)dt->day,
             (long long)dt->hour, (long long)dt->minute, (long long)dt->second,
             (long long)dt->nanosecond);
    return string_copy(buffer);
}

DateTime datetime_from_iso8601(const char* str) {
    DateTime dt = {0};
    if (!str) return dt;
    
    sscanf(str, "%lld-%lld-%lldT%lld:%lld:%lld.%lld",
           &dt.year, &dt.month, &dt.day,
           &dt.hour, &dt.minute, &dt.second, &dt.nanosecond);
    return dt;
}

DateTime datetime_with_timezone(const DateTime* dt, const TimeZone* tz) {
    if (!dt || !tz) { DateTime zero = {0}; return zero; }
    
    Duration offset = duration_create(0, tz->offset_seconds / 3600,
                                      (tz->offset_seconds % 3600) / 60,
                                      tz->offset_seconds % 60, 0);
    return datetime_add(dt, &offset);
}

DateTime datetime_to_utc(const DateTime* dt, const TimeZone* tz) {
    if (!dt || !tz) { DateTime zero = {0}; return zero; }
    
    Duration offset = duration_create(0, tz->offset_seconds / 3600,
                                      (tz->offset_seconds % 3600) / 60,
                                      tz->offset_seconds % 60, 0);
    return datetime_subtract(dt, &offset);
}

Duration duration_create(i64 days, i64 hours, i64 minutes, i64 seconds, i64 nanoseconds) {
    Duration d = {days, hours, minutes, seconds, nanoseconds};
    return d;
}

Duration duration_from_seconds(f64 seconds) {
    Duration d = {0};
    d.nanoseconds = (i64)(seconds * 1000000000.0);
    d.seconds = d.nanoseconds / 1000000000LL;
    d.nanoseconds %= 1000000000LL;
    d.minutes = d.seconds / 60;
    d.seconds %= 60;
    d.hours = d.minutes / 60;
    d.minutes %= 60;
    d.days = d.hours / 24;
    d.hours %= 24;
    return d;
}

Duration duration_from_nanoseconds(i64 nanoseconds) {
    Duration d = {0};
    d.nanoseconds = nanoseconds;
    d.seconds = d.nanoseconds / 1000000000LL;
    d.nanoseconds %= 1000000000LL;
    d.minutes = d.seconds / 60;
    d.seconds %= 60;
    d.hours = d.minutes / 60;
    d.minutes %= 60;
    d.days = d.hours / 24;
    d.hours %= 24;
    return d;
}

f64 duration_to_seconds(const Duration* dur) {
    if (!dur) return 0.0;
    return (f64)dur->days * 86400.0 +
           (f64)dur->hours * 3600.0 +
           (f64)dur->minutes * 60.0 +
           (f64)dur->seconds +
           (f64)dur->nanoseconds / 1000000000.0;
}

i64 duration_to_nanoseconds(const Duration* dur) {
    if (!dur) return 0;
    return dur->days * 86400LL * 1000000000LL +
           dur->hours * 3600LL * 1000000000LL +
           dur->minutes * 60LL * 1000000000LL +
           dur->seconds * 1000000000LL +
           dur->nanoseconds;
}

Duration duration_add(const Duration* a, const Duration* b) {
    if (!a || !b) { Duration zero = {0}; return zero; }
    
    Duration d;
    d.days = a->days + b->days;
    d.hours = a->hours + b->hours;
    d.minutes = a->minutes + b->minutes;
    d.seconds = a->seconds + b->seconds;
    d.nanoseconds = a->nanoseconds + b->nanoseconds;
    
    d.seconds += d.nanoseconds / 1000000000LL;
    d.nanoseconds %= 1000000000LL;
    d.minutes += d.seconds / 60;
    d.seconds %= 60;
    d.hours += d.minutes / 60;
    d.minutes %= 60;
    d.days += d.hours / 24;
    d.hours %= 24;
    
    return d;
}

Duration duration_subtract(const Duration* a, const Duration* b) {
    if (!a || !b) { Duration zero = {0}; return zero; }
    
    Duration d;
    d.days = a->days - b->days;
    d.hours = a->hours - b->hours;
    d.minutes = a->minutes - b->minutes;
    d.seconds = a->seconds - b->seconds;
    d.nanoseconds = a->nanoseconds - b->nanoseconds;
    
    if (d.nanoseconds < 0) { d.seconds--; d.nanoseconds += 1000000000LL; }
    if (d.seconds < 0) { d.minutes--; d.seconds += 60; }
    if (d.minutes < 0) { d.hours--; d.minutes += 60; }
    if (d.hours < 0) { d.days--; d.hours += 24; }
    
    return d;
}

TimeZone* timezone_create(const char* name, i64 offset_seconds, bool_t is_dst) {
    TimeZone* tz = (TimeZone*)kmm_v4_malloc(sizeof(TimeZone));
    if (!tz) return NULL;
    tz->name = string_copy(name);
    tz->offset_seconds = offset_seconds;
    tz->is_dst = is_dst;
    return tz;
}

void timezone_destroy(TimeZone* tz) {
    if (!tz) return;
    kmm_v4_free(tz->name);
    kmm_v4_free(tz);
}

TimeZone* timezone_local(void) {
    TimeZone* tz = (TimeZone*)kmm_v4_malloc(sizeof(TimeZone));
    if (!tz) return NULL;
    
#if defined(_WIN32)
    TIME_ZONE_INFORMATION tzi;
    GetTimeZoneInformation(&tzi);
    i64 offset = (i64)tzi.Bias * 60;
    if (tzi.StandardName[0]) offset += (i64)tzi.StandardBias * 60;
    if (tzi.DaylightName[0]) offset += (i64)tzi.DaylightBias * 60;
    
    tz->name = string_copy("local");
    tz->offset_seconds = -offset;
    tz->is_dst = tzi.DaylightName[0] != 0;
#else
    tz->name = string_copy("local");
    tz->offset_seconds = 0;
    tz->is_dst = false;
#endif
    
    return tz;
}

TimeZone* timezone_utc(void) {
    TimeZone* tz = (TimeZone*)kmm_v4_malloc(sizeof(TimeZone));
    if (!tz) return NULL;
    tz->name = string_copy("UTC");
    tz->offset_seconds = 0;
    tz->is_dst = false;
    return tz;
}

bool_t datetime_is_valid(const DateTime* dt) {
    if (!dt) return false;
    if (dt->year < 1 || dt->year > 9999) return false;
    if (dt->month < 1 || dt->month > 12) return false;
    
    i64 days_in_month[] = {31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31};
    i64 dim = days_in_month[dt->month - 1];
    if (dt->month == 2) {
        if ((dt->year % 4 == 0 && dt->year % 100 != 0) || dt->year % 400 == 0) {
            dim = 29;
        }
    }
    
    if (dt->day < 1 || dt->day > dim) return false;
    if (dt->hour < 0 || dt->hour > 23) return false;
    if (dt->minute < 0 || dt->minute > 59) return false;
    if (dt->second < 0 || dt->second > 59) return false;
    if (dt->nanosecond < 0 || dt->nanosecond >= 1000000000LL) return false;
    
    return true;
}

i64 datetime_day_of_week(const DateTime* dt) {
    if (!dt) return 0;
    
    struct tm tm_info = {0};
    tm_info.tm_year = (int)(dt->year - 1900);
    tm_info.tm_mon = (int)(dt->month - 1);
    tm_info.tm_mday = (int)dt->day;
    
    time_t t = mktime(&tm_info);
    struct tm* tm = gmtime(&t);
    return tm->tm_wday;
}

i64 datetime_day_of_year(const DateTime* dt) {
    if (!dt) return 0;
    
    struct tm tm_info = {0};
    tm_info.tm_year = (int)(dt->year - 1900);
    tm_info.tm_mon = (int)(dt->month - 1);
    tm_info.tm_mday = (int)dt->day;
    
    time_t t = mktime(&tm_info);
    struct tm* tm = gmtime(&t);
    return tm->tm_yday + 1;
}

i64 datetime_week_of_year(const DateTime* dt) {
    if (!dt) return 0;
    
    struct tm tm_info = {0};
    tm_info.tm_year = (int)(dt->year - 1900);
    tm_info.tm_mon = (int)(dt->month - 1);
    tm_info.tm_mday = (int)dt->day;
    
    time_t t = mktime(&tm_info);
    struct tm* tm = gmtime(&t);
    
    return (i64)tm->tm_yday / 7 + 1;
}