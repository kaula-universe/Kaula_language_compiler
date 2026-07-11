#pragma once
#include "../base/types.h"
#include "../memory/memory.h"
#include "../string/string.h"

typedef struct DateTime {
    i64 year;
    i64 month;
    i64 day;
    i64 hour;
    i64 minute;
    i64 second;
    i64 nanosecond;
} DateTime;

typedef struct Duration {
    i64 days;
    i64 hours;
    i64 minutes;
    i64 seconds;
    i64 nanoseconds;
} Duration;

typedef struct TimeZone {
    String name;
    i64 offset_seconds;
    bool_t is_dst;
} TimeZone;

DateTime datetime_now(void);
DateTime datetime_now_utc(void);
DateTime datetime_from_timestamp(i64 timestamp_ms);
DateTime datetime_from_timestamp_ns(i64 timestamp_ns);

i64 datetime_to_timestamp(const DateTime* dt);
i64 datetime_to_timestamp_ns(const DateTime* dt);

DateTime datetime_add(const DateTime* dt, const Duration* dur);
DateTime datetime_subtract(const DateTime* dt, const Duration* dur);
Duration datetime_diff(const DateTime* a, const DateTime* b);

bool_t datetime_equal(const DateTime* a, const DateTime* b);
bool_t datetime_less(const DateTime* a, const DateTime* b);
bool_t datetime_greater(const DateTime* a, const DateTime* b);

String datetime_to_string(const DateTime* dt, const char* format);
DateTime datetime_from_string(const char* str, const char* format);

String datetime_to_iso8601(const DateTime* dt);
DateTime datetime_from_iso8601(const char* str);

DateTime datetime_with_timezone(const DateTime* dt, const TimeZone* tz);
DateTime datetime_to_utc(const DateTime* dt, const TimeZone* tz);

Duration duration_create(i64 days, i64 hours, i64 minutes, i64 seconds, i64 nanoseconds);
Duration duration_from_seconds(f64 seconds);
Duration duration_from_nanoseconds(i64 nanoseconds);

f64 duration_to_seconds(const Duration* dur);
i64 duration_to_nanoseconds(const Duration* dur);

Duration duration_add(const Duration* a, const Duration* b);
Duration duration_subtract(const Duration* a, const Duration* b);

TimeZone* timezone_create(const char* name, i64 offset_seconds, bool_t is_dst);
void timezone_destroy(TimeZone* tz);
TimeZone* timezone_local(void);
TimeZone* timezone_utc(void);

bool_t datetime_is_valid(const DateTime* dt);
i64 datetime_day_of_week(const DateTime* dt);
i64 datetime_day_of_year(const DateTime* dt);
i64 datetime_week_of_year(const DateTime* dt);