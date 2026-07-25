#ifndef STD_STRING_STRING_H
#define STD_STRING_STRING_H

#include "../base/types.h"
#include <string.h>

// String 类型由 types.h 提供（struct { size_t len; char* ptr; }）

// 常用常量
#define STRING_EMPTY ((String){.len=0, .ptr=""})

// 从 C 字符串创建 String（不复制，指针引用）
static inline String string_wrap(const char* str) {
    return (String){.len = strlen(str), .ptr = (char*)str};
}

// 字符串创建函数
extern String string_create(const char* str);
extern String string_create_from_char(char c);
extern String string_create_from_int(i64 value);
extern String string_create_from_float(f64 value);
extern String string_create_from_bool(bool value);
extern String string_copy(String str);
extern String string_substring(String str, size_t start, size_t length);

// 字符串操作函数
extern size_t string_length(String str);
extern char string_char_at(String str, size_t index);
extern void string_set_char_at(String str, size_t index, char c);
extern String string_concat(String str1, String str2);
extern String string_concat_char(String str, char c);
extern String string_concat_int(String str, i64 value);
extern String string_concat_float(String str, f64 value);
extern String string_concat_bool(String str, bool value);

// 字符串比较函数
extern int string_compare(String str1, String str2);
extern int string_compare_ignore_case(String str1, String str2);
extern bool string_equals(String str1, String str2);
extern bool string_equals_ignore_case(String str1, String str2);

// 字符串查找函数
extern size_t string_index_of(String str, char c);
extern size_t string_index_of_string(String str, String substr);
extern size_t string_last_index_of(String str, char c);
extern size_t string_last_index_of_string(String str, String substr);
extern bool string_contains(String str, char c);
extern bool string_contains_string(String str, String substr);

// 字符串修改函数
extern String string_to_upper(String str);
extern String string_to_lower(String str);
extern String string_trim(String str);
extern String string_trim_left(String str);
extern String string_trim_right(String str);
extern String string_replace(String str, char old_char, char new_char);
extern String string_replace_string(String str, String old_substr, String new_substr);

// 字符串分割函数
extern String* string_split(String str, char delimiter, size_t* count);
extern String* string_split_string(String str, String delimiter, size_t* count);

// 字符串转换函数
extern i64 string_to_int(String str);
extern f64 string_to_float(String str);
extern bool string_to_bool(String str);

// 字符串内存管理
extern void string_free(String str);
extern String string_realloc(String str, size_t new_size);

// StringBuilder（内部仍使用 char* buffer，输出转为 String）
typedef struct StringBuilder {
    char* buffer;
    size_t length;
    size_t capacity;
} StringBuilder;

extern StringBuilder* string_builder_create(void);
extern void string_builder_destroy(StringBuilder* sb);
extern void string_builder_append(StringBuilder* sb, const char* str);
extern void string_builder_append_char(StringBuilder* sb, char c);
extern String string_builder_to_string(StringBuilder* sb);

// 字符串工具函数
extern bool string_is_empty(String str);
extern bool string_starts_with(String str, String prefix);
extern bool string_ends_with(String str, String suffix);
extern size_t string_count(String str, char c);
extern size_t string_count_string(String str, String substr);

extern bool string_match_regex(String str, String pattern);
extern size_t string_match_regex_offset(String str, String pattern, size_t start_offset);
extern String* string_find_all_regex(String str, String pattern, size_t* count);
extern String string_replace_regex(String str, String pattern, String replacement);
extern bool string_validate_email(String str);
extern bool string_validate_url(String str);
extern bool string_validate_ipv4(String str);
extern bool string_validate_number(String str);

#endif // STD_STRING_STRING_H
