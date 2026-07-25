#include "string.h"
#include "../memory/memory.h"
#include <string.h>
#include <ctype.h>
#include <stdio.h>
#include <stdlib.h>

// 辅助：从 String 分配新的数据缓冲区
static char* alloc_data(size_t len) {
    char* data = (char*)kmm_v4_malloc(len + 1);
    if (data) data[len] = '\0';
    return data;
}

// 辅助：从 const char* 创建 String（分配+复制）
static String string_from_cstr(const char* s, size_t len) {
    String result = {.len = len, .ptr = alloc_data(len)};
    if (result.ptr && len > 0) memcpy(result.ptr, s, len);
    return result;
}

// 字符串创建函数
String string_create(const char* str) {
    if (!str) return STRING_EMPTY;
    size_t len = strlen(str);
    return string_from_cstr(str, len);
}

String string_create_from_char(char c) {
    String result = {.len = 1, .ptr = alloc_data(1)};
    if (result.ptr) result.ptr[0] = c;
    return result;
}

String string_create_from_int(i64 value) {
    char buf[32];
    int n = snprintf(buf, sizeof(buf), "%lld", (long long)value);
    return string_from_cstr(buf, (size_t)n);
}

String string_create_from_float(f64 value) {
    char buf[64];
    int n = snprintf(buf, sizeof(buf), "%lf", value);
    return string_from_cstr(buf, (size_t)n);
}

String string_create_from_bool(bool value) {
    return value ? string_wrap("true") : string_wrap("false");
}

String string_copy(String str) {
    return string_from_cstr(str.ptr, str.len);
}

String string_substring(String str, size_t start, size_t length) {
    if (start >= str.len) return STRING_EMPTY;
    if (start + length > str.len) length = str.len - start;
    return string_from_cstr(str.ptr + start, length);
}

// 字符串操作函数
size_t string_length(String str) {
    return str.len;
}

char string_char_at(String str, size_t index) {
    if (index >= str.len) return '\0';
    return str.ptr[index];
}

void string_set_char_at(String str, size_t index, char c) {
    if (index < str.len) str.ptr[index] = c;
}

String string_concat(String str1, String str2) {
    size_t total = str1.len + str2.len;
    char* data = alloc_data(total);
    if (data) {
        if (str1.len > 0) memcpy(data, str1.ptr, str1.len);
        if (str2.len > 0) memcpy(data + str1.len, str2.ptr, str2.len);
    }
    return (String){.len = total, .ptr = data};
}

String string_concat_char(String str, char c) {
    String cs = string_create_from_char(c);
    String result = string_concat(str, cs);
    return result;
}

String string_concat_int(String str, i64 value) {
    String vs = string_create_from_int(value);
    String result = string_concat(str, vs);
    return result;
}

String string_concat_float(String str, f64 value) {
    String vs = string_create_from_float(value);
    String result = string_concat(str, vs);
    return result;
}

String string_concat_bool(String str, bool value) {
    String vs = string_create_from_bool(value);
    String result = string_concat(str, vs);
    return result;
}

// 字符串比较函数
int string_compare(String str1, String str2) {
    size_t min = str1.len < str2.len ? str1.len : str2.len;
    int cmp = (min > 0) ? memcmp(str1.ptr, str2.ptr, min) : 0;
    if (cmp != 0) return cmp;
    if (str1.len < str2.len) return -1;
    if (str1.len > str2.len) return 1;
    return 0;
}

int string_compare_ignore_case(String str1, String str2) {
    size_t min = str1.len < str2.len ? str1.len : str2.len;
    for (size_t i = 0; i < min; i++) {
        char c1 = tolower((unsigned char)str1.ptr[i]);
        char c2 = tolower((unsigned char)str2.ptr[i]);
        if (c1 != c2) return c1 - c2;
    }
    if (str1.len < str2.len) return -1;
    if (str1.len > str2.len) return 1;
    return 0;
}

bool string_equals(String str1, String str2) {
    if (str1.len != str2.len) return false;
    return str1.len == 0 || memcmp(str1.ptr, str2.ptr, str1.len) == 0;
}

bool string_equals_ignore_case(String str1, String str2) {
    return string_compare_ignore_case(str1, str2) == 0;
}

// 字符串查找函数
size_t string_index_of(String str, char c) {
    for (size_t i = 0; i < str.len; i++) {
        if (str.ptr[i] == c) return i;
    }
    return (size_t)-1;
}

size_t string_index_of_string(String str, String substr) {
    if (substr.len == 0) return 0;
    if (substr.len > str.len) return (size_t)-1;
    for (size_t i = 0; i <= str.len - substr.len; i++) {
        if (memcmp(str.ptr + i, substr.ptr, substr.len) == 0) return i;
    }
    return (size_t)-1;
}

size_t string_last_index_of(String str, char c) {
    for (size_t i = str.len; i > 0; i--) {
        if (str.ptr[i - 1] == c) return i - 1;
    }
    return (size_t)-1;
}

size_t string_last_index_of_string(String str, String substr) {
    if (substr.len == 0) return str.len;
    if (substr.len > str.len) return (size_t)-1;
    for (size_t i = str.len - substr.len; i > 0; i--) {
        if (memcmp(str.ptr + i - 1, substr.ptr, substr.len) == 0) return i - 1;
    }
    if (memcmp(str.ptr, substr.ptr, substr.len) == 0) return 0;
    return (size_t)-1;
}

bool string_contains(String str, char c) {
    return string_index_of(str, c) != (size_t)-1;
}

bool string_contains_string(String str, String substr) {
    return string_index_of_string(str, substr) != (size_t)-1;
}

// 字符串修改函数
String string_to_upper(String str) {
    String result = string_from_cstr(str.ptr, str.len);
    for (size_t i = 0; i < result.len; i++) {
        result.ptr[i] = (char)toupper((unsigned char)result.ptr[i]);
    }
    return result;
}

String string_to_lower(String str) {
    String result = string_from_cstr(str.ptr, str.len);
    for (size_t i = 0; i < result.len; i++) {
        result.ptr[i] = (char)tolower((unsigned char)result.ptr[i]);
    }
    return result;
}

String string_trim(String str) {
    size_t start = 0, end = str.len;
    while (start < end && isspace((unsigned char)str.ptr[start])) start++;
    while (end > start && isspace((unsigned char)str.ptr[end - 1])) end--;
    return string_substring(str, start, end - start);
}

String string_trim_left(String str) {
    size_t start = 0;
    while (start < str.len && isspace((unsigned char)str.ptr[start])) start++;
    return string_substring(str, start, str.len - start);
}

String string_trim_right(String str) {
    size_t end = str.len;
    while (end > 0 && isspace((unsigned char)str.ptr[end - 1])) end--;
    return string_substring(str, 0, end);
}

String string_replace(String str, char old_char, char new_char) {
    String result = string_from_cstr(str.ptr, str.len);
    for (size_t i = 0; i < result.len; i++) {
        if (result.ptr[i] == old_char) result.ptr[i] = new_char;
    }
    return result;
}

String string_replace_string(String str, String old_substr, String new_substr) {
    if (old_substr.len == 0) return string_copy(str);

    size_t count = 0;
    for (size_t i = 0; i <= str.len - old_substr.len; ) {
        if (memcmp(str.ptr + i, old_substr.ptr, old_substr.len) == 0) {
            count++;
            i += old_substr.len;
        } else {
            i++;
        }
    }

    size_t new_len = str.len + count * (new_substr.len - old_substr.len);
    char* data = alloc_data(new_len);
    if (!data) return (String){.len = 0, .ptr = NULL};

    size_t pos = 0;
    for (size_t i = 0; i < str.len; ) {
        if (i <= str.len - old_substr.len && memcmp(str.ptr + i, old_substr.ptr, old_substr.len) == 0) {
            if (new_substr.len > 0) memcpy(data + pos, new_substr.ptr, new_substr.len);
            pos += new_substr.len;
            i += old_substr.len;
        } else {
            data[pos++] = str.ptr[i++];
        }
    }
    return (String){.len = new_len, .ptr = data};
}

// 字符串分割函数
String* string_split(String str, char delimiter, size_t* count) {
    size_t token_count = 1;
    for (size_t i = 0; i < str.len; i++) {
        if (str.ptr[i] == delimiter) token_count++;
    }
    if (count) *count = token_count;
    String* result = (String*)kmm_v4_malloc(token_count * sizeof(String));
    if (!result) return NULL;

    size_t start = 0, index = 0;
    for (size_t i = 0; i <= str.len; i++) {
        if (i == str.len || str.ptr[i] == delimiter) {
            result[index++] = string_substring(str, start, i - start);
            start = i + 1;
        }
    }
    return result;
}

String* string_split_string(String str, String delimiter, size_t* count) {
    if (delimiter.len == 0) { if (count) *count = 0; return NULL; }
    size_t token_count = 1;
    for (size_t i = 0; i <= str.len - delimiter.len; ) {
        if (memcmp(str.ptr + i, delimiter.ptr, delimiter.len) == 0) {
            token_count++;
            i += delimiter.len;
        } else { i++; }
    }
    if (count) *count = token_count;
    String* result = (String*)kmm_v4_malloc(token_count * sizeof(String));
    if (!result) return NULL;

    size_t start = 0, index = 0;
    for (size_t i = 0; i <= str.len; ) {
        if (i <= str.len - delimiter.len && memcmp(str.ptr + i, delimiter.ptr, delimiter.len) == 0) {
            result[index++] = string_substring(str, start, i - start);
            start = i + delimiter.len;
            i += delimiter.len;
        } else { i++; }
    }
    result[index] = string_substring(str, start, str.len - start);
    return result;
}

// 字符串转换函数
i64 string_to_int(String str) {
    if (str.len == 0) return 0;
    char buf[64];
    size_t copy = str.len < 63 ? str.len : 63;
    memcpy(buf, str.ptr, copy);
    buf[copy] = '\0';
    return atoll(buf);
}

f64 string_to_float(String str) {
    if (str.len == 0) return 0.0;
    char buf[128];
    size_t copy = str.len < 127 ? str.len : 127;
    memcpy(buf, str.ptr, copy);
    buf[copy] = '\0';
    return atof(buf);
}

bool string_to_bool(String str) {
    return string_equals(str, string_wrap("true")) || string_equals(str, string_wrap("1"));
}

void string_free(String str) {
    (void)str;
}

String string_realloc(String str, size_t new_size) {
    char* data = alloc_data(new_size);
    if (data && str.ptr) {
        size_t copy = str.len < new_size ? str.len : new_size;
        memcpy(data, str.ptr, copy);
    }
    return (String){.len = new_size, .ptr = data};
}

// 字符串工具函数
bool string_is_empty(String str) {
    return str.len == 0;
}

bool string_starts_with(String str, String prefix) {
    if (prefix.len > str.len) return false;
    return prefix.len == 0 || memcmp(str.ptr, prefix.ptr, prefix.len) == 0;
}

bool string_ends_with(String str, String suffix) {
    if (suffix.len > str.len) return false;
    return suffix.len == 0 || memcmp(str.ptr + str.len - suffix.len, suffix.ptr, suffix.len) == 0;
}

size_t string_count(String str, char c) {
    size_t cnt = 0;
    for (size_t i = 0; i < str.len; i++) if (str.ptr[i] == c) cnt++;
    return cnt;
}

size_t string_count_string(String str, String substr) {
    if (substr.len == 0) return 0;
    size_t cnt = 0;
    for (size_t i = 0; i <= str.len - substr.len; ) {
        if (memcmp(str.ptr + i, substr.ptr, substr.len) == 0) { cnt++; i += substr.len; }
        else { i++; }
    }
    return cnt;
}

// 正则表达式匹配实现
#ifdef _WIN32
static int simple_wildcard_match(const char* str, const char* pattern) {
    if (!str || !pattern) return 0;
    while (*str && *pattern) {
        if (*pattern == '*') {
            pattern++;
            if (!*pattern) return 1;
            while (*str) {
                if (simple_wildcard_match(str, pattern)) return 1;
                str++;
            }
            return 0;
        } else if (*pattern == '?' || *str == *pattern) {
            str++; pattern++;
        } else { return 0; }
    }
    return (*str == '\0' && *pattern == '\0');
}

bool string_match_regex(String str, String pattern) {
    char str_buf[4096], pat_buf[4096];
    size_t sc = str.len < 4095 ? str.len : 4095;
    size_t pc = pattern.len < 4095 ? pattern.len : 4095;
    memcpy(str_buf, str.ptr, sc); str_buf[sc] = '\0';
    memcpy(pat_buf, pattern.ptr, pc); pat_buf[pc] = '\0';
    return simple_wildcard_match(str_buf, pat_buf) != 0;
}

size_t string_match_regex_offset(String str, String pattern, size_t start_offset) {
    if (start_offset >= str.len) return (size_t)-1;
    String sub = string_substring(str, start_offset, str.len - start_offset);
    return string_match_regex(sub, pattern) ? start_offset : (size_t)-1;
}

String* string_find_all_regex(String str, String pattern, size_t* count) {
    if (string_match_regex(str, pattern)) {
        String* results = (String*)kmm_v4_malloc(sizeof(String));
        if (results) results[0] = string_copy(str);
        if (count) *count = 1;
        return results;
    }
    if (count) *count = 0;
    return NULL;
}

String string_replace_regex(String str, String pattern, String replacement) {
    (void)pattern; (void)replacement;
    return string_copy(str);
}
#else
#include <regex.h>

bool string_match_regex(String str, String pattern) {
    char str_buf[4096], pat_buf[4096];
    size_t sc = str.len < 4095 ? str.len : 4095;
    size_t pc = pattern.len < 4095 ? pattern.len : 4095;
    memcpy(str_buf, str.ptr, sc); str_buf[sc] = '\0';
    memcpy(pat_buf, pattern.ptr, pc); pat_buf[pc] = '\0';
    regex_t regex;
    int ret = regcomp(&regex, pat_buf, REG_EXTENDED | REG_NOSUB);
    if (ret != 0) return false;
    ret = regexec(&regex, str_buf, 0, NULL, 0);
    regfree(&regex);
    return ret == 0;
}

size_t string_match_regex_offset(String str, String pattern, size_t start_offset) {
    if (start_offset >= str.len) return (size_t)-1;
    String sub = string_substring(str, start_offset, str.len - start_offset);
    return string_match_regex(sub, pattern) ? start_offset : (size_t)-1;
}

String* string_find_all_regex(String str, String pattern, size_t* count) {
    char str_buf[4096], pat_buf[4096];
    size_t sc = str.len < 4095 ? str.len : 4095;
    size_t pc = pattern.len < 4095 ? pattern.len : 4095;
    memcpy(str_buf, str.ptr, sc); str_buf[sc] = '\0';
    memcpy(pat_buf, pattern.ptr, pc); pat_buf[pc] = '\0';
    regex_t regex;
    int ret = regcomp(&regex, pat_buf, REG_EXTENDED);
    if (ret != 0) { if (count) *count = 0; return NULL; }

    size_t max_matches = 64, match_count = 0;
    String* results = (String*)kmm_v4_malloc(max_matches * sizeof(String));
    if (!results) { regfree(&regex); if (count) *count = 0; return NULL; }

    const char* search = str_buf;
    regmatch_t match;
    while (regexec(&regex, search, 1, &match, 0) == 0) {
        if (match_count >= max_matches) {
            max_matches *= 2;
            String* nr = (String*)kmm_v4_malloc(max_matches * sizeof(String));
            if (!nr) break;
            memcpy(nr, results, match_count * sizeof(String));
            results = nr;
        }
        size_t ml = (size_t)(match.rm_eo - match.rm_so);
        results[match_count] = string_from_cstr(search + match.rm_so, ml);
        match_count++;
        search += match.rm_eo;
        if (*search == '\0') break;
    }
    regfree(&regex);
    if (count) *count = match_count;
    return match_count > 0 ? results : NULL;
}

String string_replace_regex(String str, String pattern, String replacement) {
    (void)pattern; (void)replacement;
    return string_copy(str);
}
#endif

bool string_validate_email(String str) {
    String pat = string_wrap("^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$");
    return string_match_regex(str, pat);
}

bool string_validate_url(String str) {
    String pat = string_wrap("^(https?|ftp)://[a-zA-Z0-9.-]+(:[0-9]+)?(/[^ ]*)?$");
    return string_match_regex(str, pat);
}

bool string_validate_ipv4(String str) {
    String pat = string_wrap("^([0-9]{1,3}\\.){3}[0-9]{1,3}$");
    return string_match_regex(str, pat);
}

bool string_validate_number(String str) {
    String pat = string_wrap("^[+-]?[0-9]*\\.?[0-9]+([eE][+-]?[0-9]+)?$");
    return string_match_regex(str, pat);
}

// ==================== StringBuilder ====================

StringBuilder* string_builder_create(void) {
    StringBuilder* sb = (StringBuilder*)kmm_v4_calloc(1, sizeof(StringBuilder));
    sb->capacity = 64;
    sb->buffer = (char*)kmm_v4_malloc(sb->capacity);
    if (sb->buffer) sb->buffer[0] = '\0';
    sb->length = 0;
    return sb;
}

void string_builder_destroy(StringBuilder* sb) { (void)sb; }

void string_builder_append(StringBuilder* sb, const char* str) {
    if (!sb || !str) return;
    size_t len = strlen(str);
    if (sb->length + len + 1 > sb->capacity) {
        while (sb->length + len + 1 > sb->capacity) sb->capacity *= 2;
        char* nb = (char*)kmm_v4_malloc(sb->capacity);
        if (nb) { memcpy(nb, sb->buffer, sb->length); sb->buffer = nb; }
    }
    memcpy(sb->buffer + sb->length, str, len);
    sb->length += len;
    sb->buffer[sb->length] = '\0';
}

void string_builder_append_char(StringBuilder* sb, char c) {
    if (!sb) return;
    if (sb->length + 2 > sb->capacity) {
        sb->capacity *= 2;
        char* nb = (char*)kmm_v4_malloc(sb->capacity);
        if (nb) { memcpy(nb, sb->buffer, sb->length); sb->buffer = nb; }
    }
    sb->buffer[sb->length++] = c;
    sb->buffer[sb->length] = '\0';
}

String string_builder_to_string(StringBuilder* sb) {
    if (!sb || !sb->buffer) return STRING_EMPTY;
    return string_from_cstr(sb->buffer, sb->length);
}
