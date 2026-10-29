/*
 * stdint.h —— Kaula freestanding 自足版
 *
 * 裸机目标（-target <arch>-none-elf）编译时，clang 资源目录的 stdint.h
 * 会 include_next 到宿主 glibc 的 /usr/include/stdint.h，后者再拉
 * bits/libc-header-start.h / bits/wordsize.h 等 glibc 专属头 ——
 * 在 ubuntu-24.04 / llvm-18 上确定性报 "file not found"，且与
 * -nostdlib / -nostdlibinc 无关（多架构路径不在排除范围内）。
 *
 * 本 shim 用编译器内建类型宏（__INT8_TYPE__ / __INT_LEAST64_TYPE__ 等，
 * GCC 与 Clang 均定义）直接给出 C11 stdint 的全部 typedef 与常用宏，
 * 不 include_next —— include 链在这里截断，glibc 头永不进入裸机编译。
 *
 * 与 freestanding/include 下其余 shim（stdio.h / string.h / sys/...）同
 * 属同一设计：裸机只有声明、没有 libc 实现，链接期缺失符号由
 * freestanding runtime（freestanding/io、math、string、memory）提供。
 */

#ifndef KAULA_FREESTANDING_STDINT_H
#define KAULA_FREESTANDING_STDINT_H

/* ==================== 类型（编译器内建，架构无关） ==================== */

typedef __INT8_TYPE__  int8_t;
typedef __INT16_TYPE__ int16_t;
typedef __INT32_TYPE__ int32_t;
typedef __INT64_TYPE__ int64_t;

typedef __UINT8_TYPE__  uint8_t;
typedef __UINT16_TYPE__ uint16_t;
typedef __UINT32_TYPE__ uint32_t;
typedef __UINT64_TYPE__ uint64_t;

typedef __INT_LEAST8_TYPE__  int_least8_t;
typedef __INT_LEAST16_TYPE__ int_least16_t;
typedef __INT_LEAST32_TYPE__ int_least32_t;
typedef __INT_LEAST64_TYPE__ int_least64_t;

typedef __UINT_LEAST8_TYPE__  uint_least8_t;
typedef __UINT_LEAST16_TYPE__ uint_least16_t;
typedef __UINT_LEAST32_TYPE__ uint_least32_t;
typedef __UINT_LEAST64_TYPE__ uint_least64_t;

typedef __INT_FAST8_TYPE__  int_fast8_t;
typedef __INT_FAST16_TYPE__ int_fast16_t;
typedef __INT_FAST32_TYPE__ int_fast32_t;
typedef __INT_FAST64_TYPE__ int_fast64_t;

typedef __UINT_FAST8_TYPE__  uint_fast8_t;
typedef __UINT_FAST16_TYPE__ uint_fast16_t;
typedef __UINT_FAST32_TYPE__ uint_fast32_t;
typedef __UINT_FAST64_TYPE__ uint_fast64_t;

typedef __INTPTR_TYPE__  intptr_t;
typedef __UINTPTR_TYPE__ uintptr_t;
typedef __INTMAX_TYPE__  intmax_t;
typedef __UINTMAX_TYPE__ uintmax_t;

/* ==================== 常量 ==================== */

#define INT8_MIN   (-__INT8_MAX__ - 1)
#define INT16_MIN  (-__INT16_MAX__ - 1)
#define INT32_MIN  (-__INT32_MAX__ - 1)
#define INT64_MIN  (-__INT64_MAX__ - 1)
#define INT8_MAX   __INT8_MAX__
#define INT16_MAX  __INT16_MAX__
#define INT32_MAX  __INT32_MAX__
#define INT64_MAX  __INT64_MAX__

#define UINT8_MAX  __UINT8_MAX__
#define UINT16_MAX __UINT16_MAX__
#define UINT32_MAX __UINT32_MAX__
#define UINT64_MAX __UINT64_MAX__

#define INT_LEAST8_MIN   (-__INT_LEAST8_MAX__ - 1)
#define INT_LEAST16_MIN  (-__INT_LEAST16_MAX__ - 1)
#define INT_LEAST32_MIN  (-__INT_LEAST32_MAX__ - 1)
#define INT_LEAST64_MIN  (-__INT_LEAST64_MAX__ - 1)
#define INT_LEAST8_MAX   __INT_LEAST8_MAX__
#define INT_LEAST16_MAX  __INT_LEAST16_MAX__
#define INT_LEAST32_MAX  __INT_LEAST32_MAX__
#define INT_LEAST64_MAX  __INT_LEAST64_MAX__

#define UINT_LEAST8_MAX  __UINT_LEAST8_MAX__
#define UINT_LEAST16_MAX __UINT_LEAST16_MAX__
#define UINT_LEAST32_MAX __UINT_LEAST32_MAX__
#define UINT_LEAST64_MAX __UINT_LEAST64_MAX__

#define INT_FAST8_MIN   (-__INT_FAST8_MAX__ - 1)
#define INT_FAST16_MIN  (-__INT_FAST16_MAX__ - 1)
#define INT_FAST32_MIN  (-__INT_FAST32_MAX__ - 1)
#define INT_FAST64_MIN  (-__INT_FAST64_MAX__ - 1)
#define INT_FAST8_MAX   __INT_FAST8_MAX__
#define INT_FAST16_MAX  __INT_FAST16_MAX__
#define INT_FAST32_MAX  __INT_FAST32_MAX__
#define INT_FAST64_MAX  __INT_FAST64_MAX__

#define UINT_FAST8_MAX  __UINT_FAST8_MAX__
#define UINT_FAST16_MAX __UINT_FAST16_MAX__
#define UINT_FAST32_MAX __UINT_FAST32_MAX__
#define UINT_FAST64_MAX __UINT_FAST64_MAX__

#define INTPTR_MIN  (-__INTPTR_MAX__ - 1)
#define INTPTR_MAX  __INTPTR_MAX__
#define UINTPTR_MAX __UINTPTR_MAX__
#define INTMAX_MIN  (-__INTMAX_MAX__ - 1)
#define INTMAX_MAX  __INTMAX_MAX__
#define UINTMAX_MAX __UINTMAX_MAX__

#define INT8_C(x)    x
#define INT16_C(x)   x
#define INT32_C(x)   x
#define INT64_C(x)   x##L
#define UINT8_C(x)   x
#define UINT16_C(x)  x
#define UINT32_C(x)  x##U
#define UINT64_C(x)  x##UL
#define INTMAX_C(x)  x##L
#define UINTMAX_C(x) x##UL

#endif /* KAULA_FREESTANDING_STDINT_H */
