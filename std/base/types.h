#ifndef STD_BASE_TYPES_H
#define STD_BASE_TYPES_H

#include <stdint.h>
#include <stdbool.h>

// 平台检测宏
#if defined(_WIN32) || defined(_WIN64) || defined(__CYGWIN__)
    #define STD_PLATFORM_WINDOWS 1
    #define STD_PLATFORM_UNIX 0
#else
    #define STD_PLATFORM_WINDOWS 0
    #define STD_PLATFORM_UNIX 1
#endif

// 基础数据类型定义

// 整数类型
typedef int8_t i8;
typedef int16_t i16;
typedef int32_t i32;
typedef int64_t i64;

// 无符号整数类型
typedef uint8_t u8;
typedef uint16_t u16;
typedef uint32_t u32;
typedef uint64_t u64;

// 浮点类型
typedef float f32;
typedef double f64;

// 布尔类型
typedef bool bool_t;

// 字符类型
typedef char char_t;
typedef wchar_t wchar_t;

// 指针类型
typedef void* ptr;

// 大小类型
typedef size_t size_t;

// 定义ssize_t类型（在Windows上可能不存在）
#ifdef _WIN32
typedef intptr_t ssize_t;
#else
typedef ssize_t ssize_t;
#endif

// 空类型
typedef void void_t;

// 类型常量（避免与Windows定义冲突）
#ifndef TRUE
#define TRUE true
#endif
#ifndef FALSE
#define FALSE false
#endif
#ifndef NULL_PTR
#define NULL_PTR NULL
#endif

// 类型大小常量
#define SIZE_OF_I8 sizeof(i8)
#define SIZE_OF_I16 sizeof(i16)
#define SIZE_OF_I32 sizeof(i32)
#define SIZE_OF_I64 sizeof(i64)
#define SIZE_OF_U8 sizeof(u8)
#define SIZE_OF_U16 sizeof(u16)
#define SIZE_OF_U32 sizeof(u32)
#define SIZE_OF_U64 sizeof(u64)
#define SIZE_OF_F32 sizeof(f32)
#define SIZE_OF_F64 sizeof(f64)
#define SIZE_OF_BOOL sizeof(bool_t)
#define SIZE_OF_CHAR sizeof(char_t)
#define SIZE_OF_PTR sizeof(ptr)

// 类型范围常量
#define MIN_I8 INT8_MIN
#define MAX_I8 INT8_MAX
#define MIN_I16 INT16_MIN
#define MAX_I16 INT16_MAX
#define MIN_I32 INT32_MIN
#define MAX_I32 INT32_MAX
#define MIN_I64 INT64_MIN
#define MAX_I64 INT64_MAX
#define MIN_U8 0
#define MAX_U8 UINT8_MAX
#define MIN_U16 0
#define MAX_U16 UINT16_MAX
#define MIN_U32 0
#define MAX_U32 UINT32_MAX
#define MIN_U64 0
#define MAX_U64 UINT64_MAX

// 类型转换函数
extern i8   to_i8(ssize_t value);
extern i16  to_i16(ssize_t value);
extern i32  to_i32(ssize_t value);
extern i64  to_i64(ssize_t value);
extern u8   to_u8(size_t value);
extern u16  to_u16(size_t value);
extern u32  to_u32(size_t value);
extern u64  to_u64(size_t value);
extern f32  to_f32(double value);
extern f64  to_f64(double value);
extern bool to_bool(int value);
extern char to_char(int value);

// 类型比较函数
extern int compare_i8(i8 a, i8 b);
extern int compare_i16(i16 a, i16 b);
extern int compare_i32(i32 a, i32 b);
extern int compare_i64(i64 a, i64 b);
extern int compare_u8(u8 a, u8 b);
extern int compare_u16(u16 a, u16 b);
extern int compare_u32(u32 a, u32 b);
extern int compare_u64(u64 a, u64 b);
extern int compare_f32(f32 a, f32 b);
extern int compare_f64(f64 a, f64 b);
extern int compare_bool(bool a, bool b);
extern int compare_char(char a, char b);

// 类型检查函数
extern bool is_integer(ssize_t value);
extern bool is_unsigned(size_t value);
extern bool is_float(double value);
extern bool is_bool(bool value);
extern bool is_char(char value);

#endif // STD_BASE_TYPES_H