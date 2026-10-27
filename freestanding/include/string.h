// freestanding/include/string.h — 裸机模式的 <string.h> 替身（仅原型）
//
// 用途：src/kaula.h 与部分 std 头会 include <string.h>。交叉到 *-none-elf 后
// 宿主 SDK 不在搜索路径，缺这个头会让整个 TU 编不过。
//
// 语义边界：这里只声明，不实现。mem*/str* 里 freestanding 库真正实现的那部分
// （memset/memcpy/memmove/memcmp/strlen/strcmp/strncmp/strcpy/strncpy/strcat/
// strncat/strchr/strrchr/strstr/strspn/strcspn/memchr/strdup）由
// freestanding.string/memory 以弱符号提供，链接期自动对上；其余（strtok/
// strerror/strcoll/bzero…）保持未定义 —— 裸机没有对应实现，不该由替身头伪造。
#ifndef KAULA_FS_COMPAT_STRING_H
#define KAULA_FS_COMPAT_STRING_H

#include <stddef.h>

#ifdef __cplusplus
extern "C" {
#endif

void* memcpy(void* dst, const void* src, size_t n);
void* memmove(void* dst, const void* src, size_t n);
void* memset(void* dst, int c, size_t n);
int memcmp(const void* a, const void* b, size_t n);
void* memchr(const void* s, int c, size_t n);
void* memrchr(const void* s, int c, size_t n);
void bzero(void* s, size_t n);

size_t strlen(const char* s);
size_t strnlen(const char* s, size_t maxlen);
char* strcpy(char* dst, const char* src);
char* strncpy(char* dst, const char* src, size_t n);
char* strcat(char* dst, const char* src);
char* strncat(char* dst, const char* src, size_t n);
int strcmp(const char* a, const char* b);
int strncmp(const char* a, const char* b, size_t n);
int strcoll(const char* a, const char* b);
size_t strxfrm(char* dst, const char* src, size_t n);
char* strchr(const char* s, int c);
char* strrchr(const char* s, int c);
size_t strspn(const char* s, const char* accept);
size_t strcspn(const char* s, const char* reject);
char* strpbrk(const char* s, const char* accept);
char* strstr(const char* haystack, const char* needle);
char* strtok(char* s, const char* delim);
char* strerror(int errnum);
char* strdup(const char* s);
char* strndup(const char* s, size_t n);

// POSIX 大小写不敏感比较：freestanding.string 提供实现（弱符号）。
// 注意 MSVC UCRT 没有这两个符号 —— 裸机与 Linux/macOS 上本实现生效；
// 托管 Windows 上由 kaula_runtime 在 kaula.h 之外单独桥接。
int strcasecmp(const char* a, const char* b);
int strncasecmp(const char* a, const char* b, size_t n);

#ifdef __cplusplus
}
#endif

#endif /* KAULA_FS_COMPAT_STRING_H */
