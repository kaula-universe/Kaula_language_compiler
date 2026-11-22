#ifndef KAULA_FREESTANDING_STRING_H
#define KAULA_FREESTANDING_STRING_H

// freestanding/string/string.h — 字符串模块
// 与 std.string 同级，但无 <string.h> 依赖：全部常用字符串函数自实现
// 处理逻辑与 C 标准库一致，托管模式下 libc 强符号优先，裸机模式下本实现生效

#include <stddef.h>
#include <stdint.h>
#include "../base/types.h"

/* ============================================================================
 * C 字符串操作（与 libc 同名同语义）
 * ============================================================================ */

/**
 * strlen - 返回字符串长度（不含结尾 '\0'）
 */
extern size_t strlen(const char* s);

/**
 * strcmp - 比较两个字符串
 * 返回: <0 / 0 / >0
 */
extern int strcmp(const char* a, const char* b);

/**
 * strncmp - 比较两个字符串（最多 n 字节）
 */
extern int strncmp(const char* a, const char* b, size_t n);

/**
 * strcasecmp - 忽略大小写比较两个字符串
 */
extern int strcasecmp(const char* a, const char* b);

/**
 * strncasecmp - 忽略大小写比较两个字符串（最多 n 字节）
 */
extern int strncasecmp(const char* a, const char* b, size_t n);

/**
 * strcpy - 拷贝字符串（含 '\0'），返回 dst
 */
extern char* strcpy(char* dst, const char* src);

/**
 * strncpy - 拷贝字符串（最多 n 字节），返回 dst
 */
extern char* strncpy(char* dst, const char* src, size_t n);

/**
 * strcat - 追加字符串到 dst 末尾，返回 dst
 */
extern char* strcat(char* dst, const char* src);

/**
 * strncat - 追加 src 的最多 n 字节到 dst 末尾，返回 dst
 */
extern char* strncat(char* dst, const char* src, size_t n);

/**
 * strchr - 查找字符 c 在 s 中首次出现的位置，未找到返回 NULL
 */
extern char* strchr(const char* s, int c);

/**
 * strrchr - 查找字符 c 在 s 中最后一次出现的位置，未找到返回 NULL
 */
extern char* strrchr(const char* s, int c);

/**
 * strstr - 查找子串 needle 在 haystack 中首次出现的位置，未找到返回 NULL
 */
extern char* strstr(const char* haystack, const char* needle);

/**
 * strspn - 返回 s 开头连续由 accept 中字符组成的前缀长度
 */
extern size_t strspn(const char* s, const char* accept);

/**
 * strcspn - 返回 s 开头不含 reject 中任何字符的前缀长度
 */
extern size_t strcspn(const char* s, const char* reject);

/**
 * memchr - 在内存块中查找字节 c，返回首次出现位置，未找到返回 NULL
 */
extern void* memchr(const void* s, int c, size_t n);

/* ============================================================================
 * 字符分类与转换（无 <ctype.h> 依赖，裸机常用）
 * ============================================================================ */

extern bool is_alpha(char c);
extern bool is_digit(char c);
extern bool is_alnum(char c);
extern bool is_space(char c);
extern bool is_upper(char c);
extern bool is_lower(char c);
extern bool is_xdigit(char c);
extern char to_upper(char c);
extern char to_lower(char c);

/* ============================================================================
 * 数字 <-> 字符串（无 libc 依赖）
 * 注意：使用 fs_ 前缀，避免与 <stdlib.h> 中的 itoa/atoi 声明冲突
 * ============================================================================ */

/**
 * fs_itoa - 整数转十进制字符串（写入 buf，返回 buf；buf 必须 >= 33 字节）
 */
extern char* fs_itoa(i64 value, char* buf);

/**
 * fs_utoa - 无符号整数转十进制字符串（返回 buf）
 */
extern char* fs_utoa(u64 value, char* buf);

/**
 * fs_itoa_hex - 整数转十六进制字符串（写入 buf，返回 buf）
 * @uppercase: 非 0 输出大写
 */
extern char* fs_itoa_hex(u64 value, char* buf, bool uppercase);

/**
 * fs_atoi - 十进制字符串转整数（跳过前导空白，支持符号；非法输入返回 0）
 */
extern i64 fs_atoi(const char* s);

/**
 * fs_atou - 十进制字符串转无符号整数（支持 0x 前缀十六进制）
 */
extern u64 fs_atou(const char* s);

/* ============================================================================
 * 字符串分配（基于 freestanding.memory 的 fs_alloc，无 malloc 依赖）
 * ============================================================================ */

/**
 * fs_strdup - 复制字符串（使用 fs_alloc 分配），失败返回 NULL
 */
extern char* fs_strdup(const char* s);

#endif /* KAULA_FREESTANDING_STRING_H */
