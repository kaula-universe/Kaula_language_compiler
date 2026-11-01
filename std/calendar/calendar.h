#pragma once
#include "../base/types.h"
#include "../memory/memory.h"
#include "../string/string.h"

typedef struct Calendar {
    String name;
    i64 first_day_of_week;
} Calendar;

typedef struct Date {
    i64 year;
    i64 month;
    i64 day;
} Date;

typedef struct MonthInfo {
    String name;
    String short_name;
    i64 days;
} MonthInfo;

typedef struct WeekdayInfo {
    String name;
    String short_name;
    i64 index;
} WeekdayInfo;

Calendar* calendar_create(const char* name, i64 first_day_of_week);
void calendar_destroy(Calendar* cal);

bool_t calendar_is_leap_year(i64 year);
i64 calendar_days_in_month(i64 year, i64 month);
i64 calendar_days_in_year(i64 year);

Date calendar_add_days(const Date* date, i64 days);
Date calendar_subtract_days(const Date* date, i64 days);
i64 calendar_diff_days(const Date* a, const Date* b);

i64 calendar_day_of_week(const Date* date);
i64 calendar_day_of_year(const Date* date);
i64 calendar_week_of_year(const Date* date);

bool_t calendar_is_weekend(const Date* date);
bool_t calendar_is_workday(const Date* date);

Date calendar_next_workday(const Date* date);
Date calendar_previous_workday(const Date* date);

Date calendar_first_day_of_month(i64 year, i64 month);
Date calendar_last_day_of_month(i64 year, i64 month);
Date calendar_first_day_of_week(const Date* date);
Date calendar_last_day_of_week(const Date* date);

String calendar_format_date(const Date* date, const char* format);
Date calendar_parse_date(const char* str, const char* format);

MonthInfo calendar_get_month_info(i64 month);
WeekdayInfo calendar_get_weekday_info(i64 weekday);

i64 calendar_weekdays_in_month(i64 year, i64 month, i64 weekday);
i64 calendar_nth_weekday_in_month(i64 year, i64 month, i64 weekday, i64 nth);

bool_t calendar_date_equal(const Date* a, const Date* b);
bool_t calendar_date_less(const Date* a, const Date* b);
bool_t calendar_date_greater(const Date* a, const Date* b);

Date calendar_today(void);
Date calendar_yesterday(void);
Date calendar_tomorrow(void);