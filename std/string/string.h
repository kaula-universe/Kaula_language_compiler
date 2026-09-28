#ifndef STD_STRING_STRING_H
#define STD_STRING_STRING_H

#include "../base/types.h"
#include <string.h>

// String 类型：SSO 优化（<=15 字节零堆分配）

// 常用常量
#define STRING_EMPTY ((String){.len=0, .sso=""})

// 从 C 字符串创建 String（短字符串拷贝到 SSO，长字符串指针引用）
static inline String string_wrap(const char* str) {
    if (!str) return STRING_EMPTY;
    size_t len = strlen(str);
    String s;
    s.len = len;
    if (len <= STRING_SSO_CAPACITY) {
        memcpy(s.sso, str, len + 1);
    } else {
        s.ptr = (char*)str;
    }
    return s;
}

// 从「已按 len 长度填好、并写入了 str[len] = '\0'」的缓冲区创建 String。
// 与 string_wrap 的唯一区别是显式给定长度，因此缓冲区中间含 '\0' 也正确。
//
// 存在的理由：标准库里大量函数按固定长度往堆缓冲区里写，最后用
//     return (String){.len = len, .ptr = buf};
// 构造返回值。这种写法在 len <= STRING_SSO_CAPACITY 时是错的——String 落在
// SSO 区，string_cdata() 读的是从未初始化的 .sso，于是
//     unicode_str_to_upper("abc")   -> 空串
//     unicode_str_to_lower("ABC")   -> "0"
// 同时堆指针被丢弃。必须由本函数按长度分流到 .sso / .ptr。
//
// 所有权：与 string_wrap 一致，长串直接接管 str，短串拷贝后不释放
// （KMM 作用域分配器在作用域结束时统一回收）。
static inline String string_wrap_n(char* str, size_t len) {
    if (!str) return STRING_EMPTY;
    String s;
    s.len = len;
    if (len <= STRING_SSO_CAPACITY) {
        memcpy(s.sso, str, len + 1);
    } else {
        s.ptr = str;
    }
    return s;
}

// 字符串创建函数
extern String string_create(const char* str);
extern String string_create_from_char(char c);
extern String string_create_from_int(i64 value);
extern String string_create_from_float(f64 value);
extern String string_create_from_bool(bool value);
extern String string_copy(String str);
extern String string_substring(String str, size_t start, size_t length);

// 长寿命字符串（Task #18）：载荷从 KMM 幸存段（kmm_v4_alloc_global）分配。
// 不受 scope_pop 回卷影响；SOR 浅提升（仅复制 {len,ptr} 外壳）后载荷
// 指针依然有效——适用于 webview 回调、事件队列等需跨作用域存活的场景。
// 回收方式：随幸存段 checkpoint/rewind 或 destroy_pool，无单独 free。
extern String string_create_global(const char* str);
extern String string_clone_global(String str);

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

// StringBuilder（SSO 优化：短字符串使用结构体内部缓冲区，零额外堆分配）
#define SB_SSO_SIZE 32
typedef struct StringBuilder {
    char* buffer;
    size_t length;
    size_t capacity;
    char sso[SB_SSO_SIZE]; // 内嵌小字符串缓冲区，<=31字节零额外分配
} StringBuilder;

extern StringBuilder* string_builder_create(void);
extern void string_builder_destroy(StringBuilder* sb);
extern void string_builder_reset(StringBuilder* sb);
extern void string_builder_append(StringBuilder* sb, const char* str);
extern void string_builder_append_char(StringBuilder* sb, char c);
extern String string_builder_to_string(StringBuilder* sb);

// SSO 感知：只有 len > 15（堆分配）时才释放，SSO 字符串零释放
extern void string_survivor_free(String s);

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
