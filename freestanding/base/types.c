// freestanding/base/types.c — 与 std/base/types.c 相同的处理逻辑
// 全部符号 FS_WEAK：托管模式下 std.base / kaula_runtime 的强符号覆盖本实现，
// 裸机模式下本实现直接可用

#ifndef KAULA_FREESTANDING_BASE_TYPES_C
#define KAULA_FREESTANDING_BASE_TYPES_C

#include "types.h"
#include "fs_common.h"
#include <stdint.h>
#include <stdbool.h>
#include <limits.h>

// 类型转换函数（饱和转换，与 std/base/types.c 一致）
FS_WEAK i8 to_i8(ssize_t value) {
    if (value < INT8_MIN) return INT8_MIN;
    if (value > INT8_MAX) return INT8_MAX;
    return (i8)value;
}

FS_WEAK i16 to_i16(ssize_t value) {
    if (value < INT16_MIN) return INT16_MIN;
    if (value > INT16_MAX) return INT16_MAX;
    return (i16)value;
}

FS_WEAK i32 to_i32(ssize_t value) {
    if (value < INT32_MIN) return INT32_MIN;
    if (value > INT32_MAX) return INT32_MAX;
    return (i32)value;
}

FS_WEAK i64 to_i64(ssize_t value) {
    return (i64)value;
}

FS_WEAK u8 to_u8(size_t value) {
    if (value > UINT8_MAX) return UINT8_MAX;
    return (u8)value;
}

FS_WEAK u16 to_u16(size_t value) {
    if (value > UINT16_MAX) return UINT16_MAX;
    return (u16)value;
}

FS_WEAK u32 to_u32(size_t value) {
    if (value > UINT32_MAX) return UINT32_MAX;
    return (u32)value;
}

FS_WEAK u64 to_u64(size_t value) {
    return (u64)value;
}

FS_WEAK f32 to_f32(double value) {
    return (f32)value;
}

FS_WEAK f64 to_f64(double value) {
    return value;
}

FS_WEAK bool to_bool(int value) {
    return value != 0;
}

FS_WEAK char to_char(int value) {
    return (char)value;
}

// 类型比较函数
FS_WEAK int compare_i8(i8 a, i8 b) {
    if (a < b) return -1;
    if (a > b) return 1;
    return 0;
}

FS_WEAK int compare_i16(i16 a, i16 b) {
    if (a < b) return -1;
    if (a > b) return 1;
    return 0;
}

FS_WEAK int compare_i32(i32 a, i32 b) {
    if (a < b) return -1;
    if (a > b) return 1;
    return 0;
}

FS_WEAK int compare_i64(i64 a, i64 b) {
    if (a < b) return -1;
    if (a > b) return 1;
    return 0;
}

FS_WEAK int compare_u8(u8 a, u8 b) {
    if (a < b) return -1;
    if (a > b) return 1;
    return 0;
}

FS_WEAK int compare_u16(u16 a, u16 b) {
    if (a < b) return -1;
    if (a > b) return 1;
    return 0;
}

FS_WEAK int compare_u32(u32 a, u32 b) {
    if (a < b) return -1;
    if (a > b) return 1;
    return 0;
}

FS_WEAK int compare_u64(u64 a, u64 b) {
    if (a < b) return -1;
    if (a > b) return 1;
    return 0;
}

FS_WEAK int compare_f32(f32 a, f32 b) {
    if (a < b) return -1;
    if (a > b) return 1;
    return 0;
}

FS_WEAK int compare_f64(f64 a, f64 b) {
    if (a < b) return -1;
    if (a > b) return 1;
    return 0;
}

FS_WEAK int compare_bool(bool a, bool b) {
    if (a < b) return -1;
    if (a > b) return 1;
    return 0;
}

FS_WEAK int compare_char(char a, char b) {
    if (a < b) return -1;
    if (a > b) return 1;
    return 0;
}

// 类型检查函数
FS_WEAK bool is_integer(ssize_t value) {
    (void)value;
    return true; // 所有 ssize_t 都是整数
}

FS_WEAK bool is_unsigned(size_t value) {
    (void)value;
    return true; // 所有 size_t 都是无符号整数
}

FS_WEAK bool is_float(double value) {
    (void)value;
    return true; // 所有 double 都是浮点数
}

FS_WEAK bool is_bool(bool value) {
    (void)value;
    return true; // 所有 bool 都是布尔值
}

FS_WEAK bool is_char(char value) {
    (void)value;
    return true; // 所有 char 都是字符
}

#endif /* KAULA_FREESTANDING_BASE_TYPES_C */
