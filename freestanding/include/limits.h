/*
 * limits.h —— Kaula freestanding 自足版
 *
 * 与 freestanding/include/stdint.h 同一设计：编译器内建宏给出 C11 常用
 * 整数/字符界限，不 include_next，glibc 头永不进入裸机编译。
 * （clang 资源 limits.h 会 include_next 到宿主 /usr/include/limits.h →
 * bits/libc-header-start.h 缺失，裸机目标必炸。）
 */

#ifndef KAULA_FREESTANDING_LIMITS_H
#define KAULA_FREESTANDING_LIMITS_H

#define CHAR_BIT   __CHAR_BIT__

#define SCHAR_MIN  (-__SCHAR_MAX__ - 1)
#define SCHAR_MAX  __SCHAR_MAX__
#define UCHAR_MAX  (__SCHAR_MAX__ * 2 + 1)

#define CHAR_MIN   SCHAR_MIN
#define CHAR_MAX   SCHAR_MAX

#define SHRT_MIN   (-__SHRT_MAX__ - 1)
#define SHRT_MAX   __SHRT_MAX__
#define USHRT_MAX  (__SHRT_MAX__ * 2 + 1)

#define INT_MIN    (-__INT_MAX__ - 1)
#define INT_MAX    __INT_MAX__
#define UINT_MAX   (__INT_MAX__ * 2U + 1U)

#define LONG_MIN   (-__LONG_MAX__ - 1)
#define LONG_MAX   __LONG_MAX__
#define ULONG_MAX  (__LONG_MAX__ * 2UL + 1UL)

#define LLONG_MIN  (-__LONG_LONG_MAX__ - 1)
#define LLONG_MAX  __LONG_LONG_MAX__
#define ULLONG_MAX (__LONG_LONG_MAX__ * 2ULL + 1ULL)

#endif /* KAULA_FREESTANDING_LIMITS_H */
