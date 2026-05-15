#include "string.h"
#include <stdlib.h>
#include <string.h>
#include <ctype.h>
#include <stdio.h>

// 字符串创建函数
String string_create(const char* str) {
    if (!str) return NULL;
    size_t len = strlen(str);
    String result = (String)malloc(len + 1);
    if (result) {
        strcpy(result, str);
    }
    return result;
}

String string_create_from_char(char c) {
    String result = (String)malloc(2);
    if (result) {
        result[0] = c;
        result[1] = '\0';
    }
    return result;
}

String string_create_from_int(i64 value) {
    String result = (String)malloc(20); // 足够容纳64位整数
    if (result) {
        snprintf(result, 20, "%lld", value);
    }
    return result;
}

String string_create_from_float(f64 value) {
    String result = (String)malloc(30); // 足够容纳浮点数
    if (result) {
        snprintf(result, 30, "%lf", value);
    }
    return result;
}

String string_create_from_bool(bool value) {
    return string_create(value ? "true" : "false");
}

String string_copy(const String str) {
    return string_create(str);
}

String string_substring(const String str, size_t start, size_t length) {
    if (!str) return NULL;
    size_t str_len = strlen(str);
    if (start >= str_len) return string_create("");
    if (start + length > str_len) {
        length = str_len - start;
    }
    String result = (String)malloc(length + 1);
    if (result) {
        strncpy(result, str + start, length);
        result[length] = '\0';
    }
    return result;
}

// 字符串操作函数
size_t string_length(const String str) {
    if (!str) return 0;
    return strlen(str);
}

char string_char_at(const String str, size_t index) {
    if (!str || index >= strlen(str)) return '\0';
    return str[index];
}

void string_set_char_at(String str, size_t index, char c) {
    if (str && index < strlen(str)) {
        str[index] = c;
    }
}

String string_concat(const String str1, const String str2) {
    if (!str1) return string_copy(str2);
    if (!str2) return string_copy(str1);
    size_t len1 = strlen(str1);
    size_t len2 = strlen(str2);
    String result = (String)malloc(len1 + len2 + 1);
    if (result) {
        strcpy(result, str1);
        strcat(result, str2);
    }
    return result;
}

String string_concat_char(const String str, char c) {
    String c_str = string_create_from_char(c);
    String result = string_concat(str, c_str);
    string_free(c_str);
    return result;
}

String string_concat_int(const String str, i64 value) {
    String int_str = string_create_from_int(value);
    String result = string_concat(str, int_str);
    string_free(int_str);
    return result;
}

String string_concat_float(const String str, f64 value) {
    String float_str = string_create_from_float(value);
    String result = string_concat(str, float_str);
    string_free(float_str);
    return result;
}

String string_concat_bool(const String str, bool value) {
    String bool_str = string_create_from_bool(value);
    String result = string_concat(str, bool_str);
    string_free(bool_str);
    return result;
}

// 字符串比较函数
int string_compare(const String str1, const String str2) {
    if (!str1 && !str2) return 0;
    if (!str1) return -1;
    if (!str2) return 1;
    return strcmp(str1, str2);
}

int string_compare_ignore_case(const String str1, const String str2) {
    if (!str1 && !str2) return 0;
    if (!str1) return -1;
    if (!str2) return 1;
    const char* p1 = str1;
    const char* p2 = str2;
    while (*p1 && *p2) {
        char c1 = tolower((unsigned char)*p1);
        char c2 = tolower((unsigned char)*p2);
        if (c1 != c2) return c1 - c2;
        p1++;
        p2++;
    }
    return *p1 - *p2;
}

bool string_equals(const String str1, const String str2) {
    return string_compare(str1, str2) == 0;
}

bool string_equals_ignore_case(const String str1, const String str2) {
    return string_compare_ignore_case(str1, str2) == 0;
}

// 字符串查找函数
size_t string_index_of(const String str, char c) {
    if (!str) return (size_t)-1;
    const char* p = strchr(str, c);
    if (!p) return (size_t)-1;
    return p - str;
}

size_t string_index_of_string(const String str, const String substr) {
    if (!str || !substr) return (size_t)-1;
    const char* p = strstr(str, substr);
    if (!p) return (size_t)-1;
    return p - str;
}

size_t string_last_index_of(const String str, char c) {
    if (!str) return (size_t)-1;
    const char* p = strrchr(str, c);
    if (!p) return (size_t)-1;
    return p - str;
}

size_t string_last_index_of_string(const String str, const String substr) {
    if (!str || !substr) return (size_t)-1;
    size_t str_len = strlen(str);
    size_t substr_len = strlen(substr);
    if (substr_len > str_len) return (size_t)-1;
    for (size_t i = str_len - substr_len; i >= 0; i--) {
        if (strncmp(str + i, substr, substr_len) == 0) {
            return i;
        }
        if (i == 0) break;
    }
    return (size_t)-1;
}

bool string_contains(const String str, char c) {
    return string_index_of(str, c) != (size_t)-1;
}

bool string_contains_string(const String str, const String substr) {
    return string_index_of_string(str, substr) != (size_t)-1;
}

// 字符串修改函数
String string_to_upper(const String str) {
    if (!str) return NULL;
    String result = string_copy(str);
    if (result) {
        for (size_t i = 0; result[i]; i++) {
            result[i] = toupper((unsigned char)result[i]);
        }
    }
    return result;
}

String string_to_lower(const String str) {
    if (!str) return NULL;
    String result = string_copy(str);
    if (result) {
        for (size_t i = 0; result[i]; i++) {
            result[i] = tolower((unsigned char)result[i]);
        }
    }
    return result;
}

String string_trim(const String str) {
    if (!str) return NULL;
    size_t start = 0;
    size_t end = strlen(str) - 1;
    while (start <= end && isspace((unsigned char)str[start])) {
        start++;
    }
    while (end >= start && isspace((unsigned char)str[end])) {
        end--;
    }
    return string_substring(str, start, end - start + 1);
}

String string_trim_left(const String str) {
    if (!str) return NULL;
    size_t start = 0;
    while (str[start] && isspace((unsigned char)str[start])) {
        start++;
    }
    return string_substring(str, start, strlen(str) - start);
}

String string_trim_right(const String str) {
    if (!str) return NULL;
    size_t end = strlen(str) - 1;
    while (end >= 0 && isspace((unsigned char)str[end])) {
        end--;
    }
    return string_substring(str, 0, end + 1);
}

String string_replace(const String str, char old_char, char new_char) {
    if (!str) return NULL;
    String result = string_copy(str);
    if (result) {
        for (size_t i = 0; result[i]; i++) {
            if (result[i] == old_char) {
                result[i] = new_char;
            }
        }
    }
    return result;
}

String string_replace_string(const String str, const String old_substr, const String new_substr) {
    if (!str || !old_substr || !new_substr) return string_copy(str);
    size_t old_len = strlen(old_substr);
    if (old_len == 0) return string_copy(str);
    size_t new_len = strlen(new_substr);
    size_t count = 0;
    const char* p = str;
    while ((p = strstr(p, old_substr)) != NULL) {
        count++;
        p += old_len;
    }
    size_t str_len = strlen(str);
    String result = (String)malloc(str_len + count * (new_len - old_len) + 1);
    if (result) {
        char* dst = result;
        const char* src = str;
        while ((p = strstr(src, old_substr)) != NULL) {
            size_t len = p - src;
            strncpy(dst, src, len);
            dst += len;
            strcpy(dst, new_substr);
            dst += new_len;
            src = p + old_len;
        }
        strcpy(dst, src);
    }
    return result;
}

// 字符串分割函数
String* string_split(const String str, char delimiter, size_t* count) {
    if (!str) {
        if (count) *count = 0;
        return NULL;
    }
    size_t str_len = strlen(str);
    size_t token_count = 0;
    for (size_t i = 0; i < str_len; i++) {
        if (str[i] == delimiter) {
            token_count++;
        }
    }
    token_count++;
    if (count) *count = token_count;
    String* result = (String*)malloc(token_count * sizeof(String));
    if (result) {
        size_t start = 0;
        size_t index = 0;
        for (size_t i = 0; i <= str_len; i++) {
            if (i == str_len || str[i] == delimiter) {
                size_t length = i - start;
                result[index] = string_substring(str, start, length);
                index++;
                start = i + 1;
            }
        }
    }
    return result;
}

String* string_split_string(const String str, const String delimiter, size_t* count) {
    if (!str || !delimiter) {
        if (count) *count = 0;
        return NULL;
    }
    size_t delimiter_len = strlen(delimiter);
    if (delimiter_len == 0) {
        if (count) *count = 0;
        return NULL;
    }
    size_t str_len = strlen(str);
    size_t token_count = 0;
    const char* p = str;
    while ((p = strstr(p, delimiter)) != NULL) {
        token_count++;
        p += delimiter_len;
    }
    token_count++;
    if (count) *count = token_count;
    String* result = (String*)malloc(token_count * sizeof(String));
    if (result) {
        size_t start = 0;
        size_t index = 0;
        p = str;
        while ((p = strstr(p, delimiter)) != NULL) {
            size_t length = p - str - start;
            result[index] = string_substring(str, start, length);
            index++;
            start = p - str + delimiter_len;
            p += delimiter_len;
        }
        result[index] = string_substring(str, start, str_len - start);
    }
    return result;
}

// 字符串转换函数
i64 string_to_int(const String str) {
    if (!str) return 0;
    return atoll(str);
}

f64 string_to_float(const String str) {
    if (!str) return 0.0;
    return atof(str);
}

bool string_to_bool(const String str) {
    if (!str) return false;
    return strcmp(str, "true") == 0 || strcmp(str, "1") == 0;
}

// 字符串内存管理
void string_free(String str) {
    if (str) {
        free(str);
    }
}

String string_realloc(String str, size_t new_size) {
    if (!str) {
        return (String)malloc(new_size);
    }
    return (String)realloc(str, new_size);
}

// 字符串工具函数
bool string_is_empty(const String str) {
    return !str || str[0] == '\0';
}

bool string_starts_with(const String str, const String prefix) {
    if (!str || !prefix) return false;
    size_t prefix_len = strlen(prefix);
    size_t str_len = strlen(str);
    if (prefix_len > str_len) return false;
    return strncmp(str, prefix, prefix_len) == 0;
}

bool string_ends_with(const String str, const String suffix) {
    if (!str || !suffix) return false;
    size_t suffix_len = strlen(suffix);
    size_t str_len = strlen(str);
    if (suffix_len > str_len) return false;
    return strcmp(str + str_len - suffix_len, suffix) == 0;
}

size_t string_count(const String str, char c) {
    if (!str) return 0;
    size_t count = 0;
    for (size_t i = 0; str[i]; i++) {
        if (str[i] == c) {
            count++;
        }
    }
    return count;
}

size_t string_count_string(const String str, const String substr) {
    if (!str || !substr) return 0;
    size_t substr_len = strlen(substr);
    if (substr_len == 0) return 0;
    size_t count = 0;
    const char* p = str;
    while ((p = strstr(p, substr)) != NULL) {
        count++;
        p += substr_len;
    }
    return count;
}

// 正则表达式匹配实现
#include <regex.h>

bool string_match_regex(const String str, const String pattern) {
    if (!str || !pattern) return false;
    regex_t regex;
    int ret = regcomp(&regex, pattern, REG_EXTENDED | REG_NOSUB);
    if (ret != 0) return false;
    ret = regexec(&regex, str, 0, NULL, 0);
    regfree(&regex);
    return ret == 0;
}

size_t string_match_regex_offset(const String str, const String pattern, size_t start_offset) {
    if (!str || !pattern) return (size_t)-1;
    size_t str_len = strlen(str);
    if (start_offset >= str_len) return (size_t)-1;
    regex_t regex;
    regmatch_t match;
    int ret = regcomp(&regex, pattern, REG_EXTENDED);
    if (ret != 0) return (size_t)-1;
    ret = regexec(&regex, str + start_offset, 1, &match, 0);
    regfree(&regex);
    if (ret != 0) return (size_t)-1;
    return start_offset + (size_t)match.rm_so;
}

String* string_find_all_regex(const String str, const String pattern, size_t* count) {
    if (!str || !pattern) {
        if (count) *count = 0;
        return NULL;
    }
    regex_t regex;
    int ret = regcomp(&regex, pattern, REG_EXTENDED);
    if (ret != 0) {
        if (count) *count = 0;
        return NULL;
    }
    size_t max_matches = 64;
    String* results = (String*)malloc(max_matches * sizeof(String));
    if (!results) {
        regfree(&regex);
        if (count) *count = 0;
        return NULL;
    }
    size_t match_count = 0;
    const char* search_start = str;
    regmatch_t match;
    while (regexec(&regex, search_start, 1, &match, 0) == 0) {
        if (match_count >= max_matches) {
            max_matches *= 2;
            String* new_results = (String*)realloc(results, max_matches * sizeof(String));
            if (!new_results) break;
            results = new_results;
        }
        size_t match_len = match.rm_eo - match.rm_so;
        results[match_count] = (String)malloc(match_len + 1);
        if (results[match_count]) {
            strncpy(results[match_count], search_start + match.rm_so, match_len);
            results[match_count][match_len] = '\0';
        }
        match_count++;
        search_start += match.rm_eo;
        if (*search_start == '\0') break;
    }
    regfree(&regex);
    if (count) *count = match_count;
    if (match_count == 0) {
        free(results);
        return NULL;
    }
    return results;
}

String string_replace_regex(const String str, const String pattern, const String replacement) {
    if (!str || !pattern) return string_copy(str);
    if (!replacement) return string_copy(str);
    regex_t regex;
    int ret = regcomp(&regex, pattern, REG_EXTENDED);
    if (ret != 0) return string_copy(str);
    String result = string_create("");
    const char* search_start = str;
    regmatch_t match;
    while (regexec(&regex, search_start, 1, &match, 0) == 0) {
        size_t prefix_len = match.rm_so;
        String prefix = string_substring(search_start, 0, prefix_len);
        String temp = string_concat(result, prefix);
        string_free(result);
        string_free(prefix);
        result = string_concat(temp, replacement);
        string_free(temp);
        search_start += match.rm_eo;
    }
    String suffix = string_create(search_start);
    String final_result = string_concat(result, suffix);
    string_free(result);
    string_free(suffix);
    regfree(&regex);
    return final_result;
}

bool string_validate_email(const String str) {
    return string_match_regex(str, "^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$");
}

bool string_validate_url(const String str) {
    return string_match_regex(str, "^(https?|ftp)://[a-zA-Z0-9.-]+(:[0-9]+)?(/[^ ]*)?$");
}

bool string_validate_ipv4(const String str) {
    return string_match_regex(str, "^([0-9]{1,3}\\.){3}[0-9]{1,3}$");
}

bool string_validate_number(const String str) {
    return string_match_regex(str, "^[+-]?[0-9]*\\.?[0-9]+([eE][+-]?[0-9]+)?$");
}
