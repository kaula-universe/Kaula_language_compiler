#include "types.h"
#include <stdint.h>
#include <stdbool.h>
#include <limits.h>

// 类型转换函数
i8 to_i8(ssize_t value) {
    if (value < INT8_MIN) return INT8_MIN;
    if (value > INT8_MAX) return INT8_MAX;
    return (i8)value;
}

i16 to_i16(ssize_t value) {
    if (value < INT16_MIN) return INT16_MIN;
    if (value > INT16_MAX) return INT16_MAX;
    return (i16)value;
}

i32 to_i32(ssize_t value) {
    if (value < INT32_MIN) return INT32_MIN;
    if (value > INT32_MAX) return INT32_MAX;
    return (i32)value;
}

i64 to_i64(ssize_t value) {
    return (i64)value;
}

u8 to_u8(size_t value) {
    if (value > UINT8_MAX) return UINT8_MAX;
    return (u8)value;
}

u16 to_u16(size_t value) {
    if (value > UINT16_MAX) return UINT16_MAX;
    return (u16)value;
}

u32 to_u32(size_t value) {
    if (value > UINT32_MAX) return UINT32_MAX;
    return (u32)value;
}

u64 to_u64(size_t value) {
    return (u64)value;
}

f32 to_f32(double value) {
    return (f32)value;
}

f64 to_f64(double value) {
    return value;
}

bool to_bool(int value) {
    return value != 0;
}

char to_char(int value) {
    return (char)value;
}

// 类型比较函数
int compare_i8(i8 a, i8 b) {
    if (a < b) return -1;
    if (a > b) return 1;
    return 0;
}

int compare_i16(i16 a, i16 b) {
    if (a < b) return -1;
    if (a > b) return 1;
    return 0;
}

int compare_i32(i32 a, i32 b) {
    if (a < b) return -1;
    if (a > b) return 1;
    return 0;
}

int compare_i64(i64 a, i64 b) {
    if (a < b) return -1;
    if (a > b) return 1;
    return 0;
}

int compare_u8(u8 a, u8 b) {
    if (a < b) return -1;
    if (a > b) return 1;
    return 0;
}

int compare_u16(u16 a, u16 b) {
    if (a < b) return -1;
    if (a > b) return 1;
    return 0;
}

int compare_u32(u32 a, u32 b) {
    if (a < b) return -1;
    if (a > b) return 1;
    return 0;
}

int compare_u64(u64 a, u64 b) {
    if (a < b) return -1;
    if (a > b) return 1;
    return 0;
}

int compare_f32(f32 a, f32 b) {
    if (a < b) return -1;
    if (a > b) return 1;
    return 0;
}

int compare_f64(f64 a, f64 b) {
    if (a < b) return -1;
    if (a > b) return 1;
    return 0;
}

int compare_bool(bool a, bool b) {
    if (a < b) return -1;
    if (a > b) return 1;
    return 0;
}

int compare_char(char a, char b) {
    if (a < b) return -1;
    if (a > b) return 1;
    return 0;
}

// 类型检查函数
bool is_integer(ssize_t value) {
    return true; // 所有ssize_t都是整数
}

bool is_unsigned(size_t value) {
    return true; // 所有size_t都是无符号整数
}

bool is_float(double value) {
    return true; // 所有double都是浮点数
}

bool is_bool(bool value) {
    return true; // 所有bool都是布尔值
}

bool is_char(char value) {
    return true; // 所有char都是字符
}
