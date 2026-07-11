#include "calendar.h"
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <stdio.h>

#ifdef _WIN32
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

Calendar* calendar_create(const char* name, i64 first_day_of_week) {
    Calendar* cal = (Calendar*)kmm_v4_malloc(sizeof(Calendar));
    if (!cal) return NULL;
    cal->name = string_copy(name);
    cal->first_day_of_week = first_day_of_week;
    return cal;
}

void calendar_destroy(Calendar* cal) {
    if (!cal) return;
    kmm_v4_free(cal->name);
    kmm_v4_free(cal);
}

bool_t calendar_is_leap_year(i64 year) {
    if ((year % 4 == 0 && year % 100 != 0) || year % 400 == 0) {
        return true;
    }
    return false;
}

i64 calendar_days_in_month(i64 year, i64 month) {
    i64 days[] = {31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31};
    if (month < 1 || month > 12) return 0;
    if (month == 2 && calendar_is_leap_year(year)) return 29;
    return days[month - 1];
}

i64 calendar_days_in_year(i64 year) {
    return calendar_is_leap_year(year) ? 366 : 365;
}

Date calendar_add_days(const Date* date, i64 days) {
    if (!date) { Date zero = {0}; return zero; }
    
    Date result = *date;
    result.day += days;
    
    while (result.day > calendar_days_in_month(result.year, result.month)) {
        result.day -= calendar_days_in_month(result.year, result.month);
        result.month++;
        if (result.month > 12) { result.month = 1; result.year++; }
    }
    
    while (result.day <= 0) {
        result.month--;
        if (result.month <= 0) { result.month = 12; result.year--; }
        result.day += calendar_days_in_month(result.year, result.month);
    }
    
    return result;
}

Date calendar_subtract_days(const Date* date, i64 days) {
    return calendar_add_days(date, -days);
}

i64 calendar_diff_days(const Date* a, const Date* b) {
    if (!a || !b) return 0;
    
    struct tm tm_a = {0};
    tm_a.tm_year = (int)(a->year - 1900);
    tm_a.tm_mon = (int)(a->month - 1);
    tm_a.tm_mday = (int)a->day;
    
    struct tm tm_b = {0};
    tm_b.tm_year = (int)(b->year - 1900);
    tm_b.tm_mon = (int)(b->month - 1);
    tm_b.tm_mday = (int)b->day;
    
    time_t t_a = mktime(&tm_a);
    time_t t_b = mktime(&tm_b);
    
    return (i64)(difftime(t_a, t_b) / 86400.0);
}

i64 calendar_day_of_week(const Date* date) {
    if (!date) return 0;
    
    struct tm tm_info = {0};
    tm_info.tm_year = (int)(date->year - 1900);
    tm_info.tm_mon = (int)(date->month - 1);
    tm_info.tm_mday = (int)date->day;
    
    time_t t = mktime(&tm_info);
    struct tm* tm = gmtime(&t);
    return tm->tm_wday;
}

i64 calendar_day_of_year(const Date* date) {
    if (!date) return 0;
    
    struct tm tm_info = {0};
    tm_info.tm_year = (int)(date->year - 1900);
    tm_info.tm_mon = (int)(date->month - 1);
    tm_info.tm_mday = (int)date->day;
    
    time_t t = mktime(&tm_info);
    struct tm* tm = gmtime(&t);
    return tm->tm_yday + 1;
}

i64 calendar_week_of_year(const Date* date) {
    if (!date) return 0;
    
    struct tm tm_info = {0};
    tm_info.tm_year = (int)(date->year - 1900);
    tm_info.tm_mon = (int)(date->month - 1);
    tm_info.tm_mday = (int)date->day;
    
    time_t t = mktime(&tm_info);
    struct tm* tm = gmtime(&t);
    return (i64)tm->tm_yday / 7 + 1;
}

bool_t calendar_is_weekend(const Date* date) {
    i64 dow = calendar_day_of_week(date);
    return dow == 0 || dow == 6;
}

bool_t calendar_is_workday(const Date* date) {
    return !calendar_is_weekend(date);
}

Date calendar_next_workday(const Date* date) {
    Date result = calendar_add_days(date, 1);
    while (calendar_is_weekend(&result)) {
        result = calendar_add_days(&result, 1);
    }
    return result;
}

Date calendar_previous_workday(const Date* date) {
    Date result = calendar_subtract_days(date, 1);
    while (calendar_is_weekend(&result)) {
        result = calendar_subtract_days(&result, 1);
    }
    return result;
}

Date calendar_first_day_of_month(i64 year, i64 month) {
    Date d = {year, month, 1};
    return d;
}

Date calendar_last_day_of_month(i64 year, i64 month) {
    Date d = {year, month, calendar_days_in_month(year, month)};
    return d;
}

Date calendar_first_day_of_week(const Date* date) {
    if (!date) { Date zero = {0}; return zero; }
    
    i64 dow = calendar_day_of_week(date);
    return calendar_subtract_days(date, dow);
}

Date calendar_last_day_of_week(const Date* date) {
    if (!date) { Date zero = {0}; return zero; }
    
    i64 dow = calendar_day_of_week(date);
    return calendar_add_days(date, 6 - dow);
}

String calendar_format_date(const Date* date, const char* format) {
    if (!date || !format) return NULL;
    
    char buffer[256];
    struct tm tm_info = {0};
    tm_info.tm_year = (int)(date->year - 1900);
    tm_info.tm_mon = (int)(date->month - 1);
    tm_info.tm_mday = (int)date->day;
    
    strftime(buffer, sizeof(buffer), format, &tm_info);
    return string_copy(buffer);
}

Date calendar_parse_date(const char* str, const char* format) {
    Date d = {0};
    if (!str || !format) return d;
    
    struct tm tm_info = {0};
    strptime(str, format, &tm_info);
    
    d.year = tm_info.tm_year + 1900;
    d.month = tm_info.tm_mon + 1;
    d.day = tm_info.tm_mday;
    
    return d;
}

MonthInfo calendar_get_month_info(i64 month) {
    MonthInfo info = {NULL, NULL, 0};
    
    const char* names[] = {"January", "February", "March", "April", "May", "June",
                           "July", "August", "September", "October", "November", "December"};
    const char* short_names[] = {"Jan", "Feb", "Mar", "Apr", "May", "Jun",
                                 "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"};
    
    if (month >= 1 && month <= 12) {
        info.name = string_copy(names[month - 1]);
        info.short_name = string_copy(short_names[month - 1]);
        info.days = 31;
        if (month == 2) info.days = 28;
        else if (month == 4 || month == 6 || month == 9 || month == 11) info.days = 30;
    }
    
    return info;
}

WeekdayInfo calendar_get_weekday_info(i64 weekday) {
    WeekdayInfo info = {NULL, NULL, weekday};
    
    const char* names[] = {"Sunday", "Monday", "Tuesday", "Wednesday",
                           "Thursday", "Friday", "Saturday"};
    const char* short_names[] = {"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"};
    
    if (weekday >= 0 && weekday <= 6) {
        info.name = string_copy(names[weekday]);
        info.short_name = string_copy(short_names[weekday]);
    }
    
    return info;
}

i64 calendar_weekdays_in_month(i64 year, i64 month, i64 weekday) {
    if (weekday < 0 || weekday > 6) return 0;
    
    i64 count = 0;
    i64 days = calendar_days_in_month(year, month);
    
    for (i64 d = 1; d <= days; d++) {
        Date date = {year, month, d};
        if (calendar_day_of_week(&date) == weekday) {
            count++;
        }
    }
    
    return count;
}

i64 calendar_nth_weekday_in_month(i64 year, i64 month, i64 weekday, i64 nth) {
    if (weekday < 0 || weekday > 6 || nth < 1 || nth > 5) return 0;
    
    i64 count = 0;
    i64 days = calendar_days_in_month(year, month);
    
    for (i64 d = 1; d <= days; d++) {
        Date date = {year, month, d};
        if (calendar_day_of_week(&date) == weekday) {
            count++;
            if (count == nth) return d;
        }
    }
    
    return 0;
}

bool_t calendar_date_equal(const Date* a, const Date* b) {
    if (!a || !b) return false;
    return a->year == b->year && a->month == b->month && a->day == b->day;
}

bool_t calendar_date_less(const Date* a, const Date* b) {
    if (!a || !b) return false;
    if (a->year != b->year) return a->year < b->year;
    if (a->month != b->month) return a->month < b->month;
    return a->day < b->day;
}

bool_t calendar_date_greater(const Date* a, const Date* b) {
    if (!a || !b) return false;
    if (a->year != b->year) return a->year > b->year;
    if (a->month != b->month) return a->month > b->month;
    return a->day > b->day;
}

Date calendar_today(void) {
    time_t t = time(NULL);
    struct tm* tm_info = localtime(&t);
    
    Date d;
    d.year = tm_info->tm_year + 1900;
    d.month = tm_info->tm_mon + 1;
    d.day = tm_info->tm_mday;
    
    return d;
}

Date calendar_yesterday(void) {
    Date today = calendar_today();
    return calendar_subtract_days(&today, 1);
}

Date calendar_tomorrow(void) {
    Date today = calendar_today();
    return calendar_add_days(&today, 1);
}