// freestanding/include/ctype.h — 裸机模式的 <ctype.h> 替身（仅原型）
//
// freestanding.string 已实现 is_alpha/is_digit/is_alnum/is_space/is_upper/
// is_lower/is_xdigit（弱符号），但标准名这一组在托管 Linux/macOS 上有强符号，
// 裸机没有 —— 这里只声明，链接期由 freestanding.string 的弱符号或用户实现接管。
#ifndef KAULA_FS_COMPAT_CTYPE_H
#define KAULA_FS_COMPAT_CTYPE_H

#ifdef __cplusplus
extern "C" {
#endif

int isalnum(int c);
int isalpha(int c);
int isblank(int c);
int iscntrl(int c);
int isdigit(int c);
int isgraph(int c);
int islower(int c);
int isprint(int c);
int ispunct(int c);
int isspace(int c);
int isupper(int c);
int isxdigit(int c);
int tolower(int c);
int toupper(int c);

#ifdef __cplusplus
}
#endif

#endif /* KAULA_FS_COMPAT_CTYPE_H */
