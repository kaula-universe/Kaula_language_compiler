#ifndef STD_STRING_STRING_H
#define STD_STRING_STRING_H

#include "../base/types.h"

// 字符串类型
typedef char* String;

// 字符串创建函数
extern String string_create(const char* str);
extern String string_create_from_char(char c);
extern String string_create_from_int(i64 value);
extern String string_create_from_float(f64 value);
extern String string_create_from_bool(bool value);
extern String string_copy(const String str);
extern String string_substring(const String str, size_t start, size_t length);

// 字符串操作函数
extern size_t string_length(const String str);
extern char string_char_at(const String str, size_t index);
extern void string_set_char_at(String str, size_t index, char c);
extern String string_concat(const String str1, const String str2);
extern String string_concat_char(const String str, char c);
extern String string_concat_int(const String str, i64 value);
extern String string_concat_float(const String str, f64 value);
extern String string_concat_bool(const String str, bool value);

// 字符串比较函数
extern int string_compare(const String str1, const String str2);
extern int string_compare_ignore_case(const String str1, const String str2);
extern bool string_equals(const String str1, const String str2);
extern bool string_equals_ignore_case(const String str1, const String str2);

// 字符串查找函数
extern size_t string_index_of(const String str, char c);
extern size_t string_index_of_string(const String str, const String substr);
extern size_t string_last_index_of(const String str, char c);
extern size_t string_last_index_of_string(const String str, const String substr);
extern bool string_contains(const String str, char c);
extern bool string_contains_string(const String str, const String substr);

// 字符串修改函数
extern String string_to_upper(const String str);
extern String string_to_lower(const String str);
extern String string_trim(const String str);
extern String string_trim_left(const String str);
extern String string_trim_right(const String str);
extern String string_replace(const String str, char old_char, char new_char);
extern String string_replace_string(const String str, const String old_substr, const String new_substr);

// 字符串分割函数
extern String* string_split(const String str, char delimiter, size_t* count);
extern String* string_split_string(const String str, const String delimiter, size_t* count);

// 字符串转换函数
extern i64 string_to_int(const String str);
extern f64 string_to_float(const String str);
extern bool string_to_bool(const String str);

// 字符串内存管理
extern void string_free(String str);
extern String string_realloc(String str, size_t new_size);

// 字符串工具函数
extern bool string_is_empty(const String str);
extern bool string_starts_with(const String str, const String prefix);
extern bool string_ends_with(const String str, const String suffix);
extern size_t string_count(const String str, char c);
extern size_t string_count_string(const String str, const String substr);

#endif // STD_STRING_STRING_H