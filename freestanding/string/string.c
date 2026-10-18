// freestanding/string/string.c — 字符串模块实现
// 全部函数自实现、无 <string.h>/<stdlib.h>/<ctype.h> 依赖；
// 语义与 C 标准库一致。
//
// 与 libc 同名的函数（strlen/strcmp/strcpy/...）仅在裸机模式（KAULA_FREESTANDING）
// 下定义：托管模式下这些弱符号会被从静态库中提取并遮蔽 libc 实现，导致
// 语义/性能异常。is_*/to_*/fs_* 系列无 libc 冲突，始终定义。
//
// 本文件带包含保护，可被 kaula_freestanding_runtime.c 以 unity 方式包含。

#ifndef KAULA_FREESTANDING_STRING_C
#define KAULA_FREESTANDING_STRING_C

#include "string.h"
#include "../base/fs_common.h"
#include "../memory/memory.h"
#include <stddef.h>
#include <stdint.h>

// ==================== libc 同名函数（仅裸机模式定义） ====================
// 托管模式下不定义这些符号：弱符号会从静态库中被提取并遮蔽 libc 强符号实现
#ifdef KAULA_FREESTANDING

// ==================== 基础字符串操作 ====================

FS_WEAK size_t strlen(const char* s) {
    if (FS_UNLIKELY(s == NULL)) return 0;
    const unsigned char* p = (const unsigned char*)s;
    size_t len = 0;
    while (*p != 0) {
        p++;
        len++;
    }
    return len;
}

FS_WEAK int strcmp(const char* a, const char* b) {
    if (a == b) return 0;
    if (a == NULL) return -1;
    if (b == NULL) return 1;
    while (*a != 0 && *a == *b) {
        a++;
        b++;
    }
    return (int)(unsigned char)*a - (int)(unsigned char)*b;
}

FS_WEAK int strncmp(const char* a, const char* b, size_t n) {
    if (a == b || n == 0) return 0;
    if (a == NULL) return -1;
    if (b == NULL) return 1;
    while (n > 0 && *a != 0 && *a == *b) {
        a++;
        b++;
        n--;
    }
    if (n == 0) return 0;
    return (int)(unsigned char)*a - (int)(unsigned char)*b;
}

// 简易 ASCII 折叠（无 <ctype.h> 依赖）
static inline int fs_tolower_ascii(int c) {
    if (c >= 'A' && c <= 'Z') return c + ('a' - 'A');
    return c;
}

FS_WEAK int strcasecmp(const char* a, const char* b) {
    if (a == b) return 0;
    if (a == NULL) return -1;
    if (b == NULL) return 1;
    while (*a != 0) {
        int ca = fs_tolower_ascii((unsigned char)*a);
        int cb = fs_tolower_ascii((unsigned char)*b);
        if (ca != cb) return ca - cb;
        a++;
        b++;
    }
    return fs_tolower_ascii((unsigned char)*a) - fs_tolower_ascii((unsigned char)*b);
}

FS_WEAK int strncasecmp(const char* a, const char* b, size_t n) {
    if (a == b || n == 0) return 0;
    if (a == NULL) return -1;
    if (b == NULL) return 1;
    while (n > 0 && *a != 0) {
        int ca = fs_tolower_ascii((unsigned char)*a);
        int cb = fs_tolower_ascii((unsigned char)*b);
        if (ca != cb) return ca - cb;
        a++;
        b++;
        n--;
    }
    if (n == 0) return 0;
    return fs_tolower_ascii((unsigned char)*a) - fs_tolower_ascii((unsigned char)*b);
}

FS_WEAK char* strcpy(char* dst, const char* src) {
    if (dst == NULL) return NULL;
    char* d = dst;
    while ((*d++ = *src++) != 0) { /* copy */ }
    return dst;
}

FS_WEAK char* strncpy(char* dst, const char* src, size_t n) {
    if (dst == NULL) return NULL;
    char* d = dst;
    while (n > 0 && *src != 0) {
        *d++ = *src++;
        n--;
    }
    // 与 libc 一致：剩余位置填 '\0'
    while (n > 0) {
        *d++ = 0;
        n--;
    }
    return dst;
}

FS_WEAK char* strcat(char* dst, const char* src) {
    if (dst == NULL) return NULL;
    char* d = dst;
    while (*d != 0) d++;
    while ((*d++ = *src++) != 0) { /* copy */ }
    return dst;
}

FS_WEAK char* strncat(char* dst, const char* src, size_t n) {
    if (dst == NULL) return NULL;
    char* d = dst;
    while (*d != 0) d++;
    while (n > 0 && *src != 0) {
        *d++ = *src++;
        n--;
    }
    *d = 0;
    return dst;
}

FS_WEAK char* strchr(const char* s, int c) {
    if (s == NULL) return NULL;
    char ch = (char)c;
    do {
        if (*s == ch) return (char*)s;
    } while (*s++ != 0);
    return NULL;
}

FS_WEAK char* strrchr(const char* s, int c) {
    if (s == NULL) return NULL;
    const char* found = NULL;
    char ch = (char)c;
    do {
        if (*s == ch) found = s;
    } while (*s++ != 0);
    return (char*)found;
}

FS_WEAK char* strstr(const char* haystack, const char* needle) {
    if (haystack == NULL || needle == NULL) return NULL;
    if (*needle == 0) return (char*)haystack;
    for (const char* p = haystack; *p != 0; p++) {
        const char* h = p;
        const char* n = needle;
        while (*h != 0 && *n != 0 && *h == *n) {
            h++;
            n++;
        }
        if (*n == 0) return (char*)p; // needle 完全匹配
    }
    return NULL;
}

FS_WEAK size_t strspn(const char* s, const char* accept) {
    if (s == NULL || accept == NULL) return 0;
    size_t len = 0;
    while (s[len] != 0 && strchr(accept, s[len]) != NULL) {
        len++;
    }
    return len;
}

FS_WEAK size_t strcspn(const char* s, const char* reject) {
    if (s == NULL || reject == NULL) return 0;
    size_t len = 0;
    while (s[len] != 0 && strchr(reject, s[len]) == NULL) {
        len++;
    }
    return len;
}

FS_WEAK void* memchr(const void* s, int c, size_t n) {
    if (s == NULL) return NULL;
    const unsigned char* p = (const unsigned char*)s;
    unsigned char ch = (unsigned char)c;
    for (size_t i = 0; i < n; i++) {
        if (p[i] == ch) return (void*)&p[i];
    }
    return NULL;
}

#endif /* KAULA_FREESTANDING */

// ==================== 字符分类与转换 ====================

FS_WEAK bool is_alpha(char c) {
    return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z');
}

FS_WEAK bool is_digit(char c) {
    return c >= '0' && c <= '9';
}

FS_WEAK bool is_alnum(char c) {
    return is_alpha(c) || is_digit(c);
}

FS_WEAK bool is_space(char c) {
    return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\v' || c == '\f';
}

FS_WEAK bool is_upper(char c) {
    return c >= 'A' && c <= 'Z';
}

FS_WEAK bool is_lower(char c) {
    return c >= 'a' && c <= 'z';
}

FS_WEAK bool is_xdigit(char c) {
    return is_digit(c) || (c >= 'A' && c <= 'F') || (c >= 'a' && c <= 'f');
}

FS_WEAK char to_upper(char c) {
    if (c >= 'a' && c <= 'z') return (char)(c - ('a' - 'A'));
    return c;
}

FS_WEAK char to_lower(char c) {
    if (c >= 'A' && c <= 'Z') return (char)(c + ('a' - 'A'));
    return c;
}

// ==================== 数字 <-> 字符串 ====================

FS_WEAK char* fs_itoa(i64 value, char* buf) {
    if (buf == NULL) return NULL;
    char tmp[24];
    int i = 0;
    u64 uv;
    if (value < 0) {
        uv = (u64)(-(value + 1)) + 1; // 安全取绝对值（处理 INT64_MIN）
    } else {
        uv = (u64)value;
    }
    if (uv == 0) {
        tmp[i++] = '0';
    }
    while (uv > 0) {
        tmp[i++] = (char)('0' + (int)(uv % 10));
        uv /= 10;
    }
    char* p = buf;
    if (value < 0) {
        *p++ = '-';
    }
    while (i > 0) {
        *p++ = tmp[--i];
    }
    *p = 0;
    return buf;
}

FS_WEAK char* fs_utoa(u64 value, char* buf) {
    if (buf == NULL) return NULL;
    char tmp[24];
    int i = 0;
    if (value == 0) {
        tmp[i++] = '0';
    }
    while (value > 0) {
        tmp[i++] = (char)('0' + (int)(value % 10));
        value /= 10;
    }
    char* p = buf;
    while (i > 0) {
        *p++ = tmp[--i];
    }
    *p = 0;
    return buf;
}

FS_WEAK char* fs_itoa_hex(u64 value, char* buf, bool uppercase) {
    if (buf == NULL) return NULL;
    char digits[] = "0123456789abcdef";
    char upper[] = "0123456789ABCDEF";
    char tmp[20];
    int i = 0;
    if (value == 0) {
        tmp[i++] = '0';
    }
    while (value > 0) {
        tmp[i++] = uppercase ? upper[value & 0xF] : digits[value & 0xF];
        value >>= 4;
    }
    char* p = buf;
    while (i > 0) {
        *p++ = tmp[--i];
    }
    *p = 0;
    return buf;
}

FS_WEAK i64 fs_atoi(const char* s) {
    if (s == NULL) return 0;
    while (is_space(*s)) s++;
    bool neg = false;
    if (*s == '-') {
        neg = true;
        s++;
    } else if (*s == '+') {
        s++;
    }
    u64 acc = 0;
    while (is_digit(*s)) {
        acc = acc * 10 + (u64)(*s - '0');
        if (acc > (u64)INT64_MAX + 1) break; // 溢出保护
        s++;
    }
    if (neg) {
        if (acc == (u64)INT64_MAX + 1) return INT64_MIN;
        return -(i64)acc;
    }
    if (acc > (u64)INT64_MAX) return INT64_MAX;
    return (i64)acc;
}

FS_WEAK u64 fs_atou(const char* s) {
    if (s == NULL) return 0;
    while (is_space(*s)) s++;
    u64 acc = 0;
    // 支持 0x / 0X 前缀十六进制
    if (s[0] == '0' && (s[1] == 'x' || s[1] == 'X')) {
        s += 2;
        while (is_xdigit(*s)) {
            char c = *s;
            u64 d = (c >= '0' && c <= '9') ? (u64)(c - '0')
                   : (u64)((c >= 'a' && c <= 'f') ? (c - 'a' + 10) : (c - 'A' + 10));
            acc = acc * 16 + d;
            s++;
        }
        return acc;
    }
    while (is_digit(*s)) {
        acc = acc * 10 + (u64)(*s - '0');
        s++;
    }
    return acc;
}

// ==================== 字符串分配 ====================

FS_WEAK char* fs_strdup(const char* s) {
    if (s == NULL) return NULL;
    size_t len = strlen(s);
    char* copy = (char*)fs_alloc(len + 1);
    if (copy == NULL) return NULL;
    memcpy(copy, s, len);
    copy[len] = 0;
    return copy;
}

#endif /* KAULA_FREESTANDING_STRING_C */
