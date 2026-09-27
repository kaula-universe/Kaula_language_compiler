// kaula_runtime.h - Minimal stubs for Kaula runtime features
//
// 托管模式与裸机模式共用：代码生成器在启用溢出检查（debug）时会对整数运算
// 产出 kaula_add_i64 / kaula_sub_i64 / ... 调用，声明必须对两种环境都可用。
// 因此本头文件不直接依赖 <stdio.h>/<stdlib.h>：裸机（-ffreestanding -nostdlib）
// 下没有 libc，fprintf/abort 不可用，改为调用弱钩子 kaula_fatal 并停机。
#ifndef KAULA_RUNTIME_H
#define KAULA_RUNTIME_H

#include <stdint.h>

// ===== 致命错误上报 =====
// 托管模式: 直接 fprintf(stderr) + abort()。
// 裸机模式: 调用弱钩子 kaula_fatal（用户可覆写为串口/LED 指示），随后 __builtin_trap 停机。
#if defined(KAULA_FREESTANDING) || defined(KAULA_NO_LIBC)
extern void kaula_fatal(const char* msg) __attribute__((weak));
static inline void kaula_report_fatal(const char* op) {
    if (kaula_fatal) kaula_fatal(op);
    __builtin_trap();
}
#else
#include <stdio.h>
#include <stdlib.h>
static inline void kaula_report_fatal(const char* op) {
    fprintf(stderr, "KAULA FATAL: %s\n", op);
    abort();
}
#endif

// ===== assert() 内建失败上报 =====
//
// 之前直接生成 `fprintf(stderr, ...); exit(1);` —— 裸机下 fprintf/exit 都不存在，
// 生成的 C 直接编译失败（"call to undeclared function 'exit'"）。统一走本函数：
// 托管模式 = 打印断言文本 + exit(1)；裸机模式 = 弱钩子 kaula_fatal + trap。
#if defined(KAULA_FREESTANDING) || defined(KAULA_NO_LIBC)
__attribute__((noreturn))
static inline void kaula_assert_failed(const char* expr, const char* file, int line) {
    (void)expr; (void)file; (void)line;
    if (kaula_fatal) kaula_fatal("assertion failed");
    __builtin_trap();
}
#else
__attribute__((noreturn))
static inline void kaula_assert_failed(const char* expr, const char* file, int line) {
    fprintf(stderr, "assertion failed: (%s) at %s:%d\n", expr, file, line);
    exit(1);
}
#endif

// ===== 整数溢出检查（debug 模式默认启用，零开销 __builtin_*_overflow） =====
// 使用 GCC/Clang 内置溢出检查函数，编译期优化为条件标志位检查，零运行时开销。

static inline int64_t kaula_add_i64(int64_t a, int64_t b) {
    int64_t result;
    if (__builtin_add_overflow(a, b, &result)) {
        kaula_report_fatal("integer overflow in addition");
    }
    return result;
}

static inline int64_t kaula_sub_i64(int64_t a, int64_t b) {
    int64_t result;
    if (__builtin_sub_overflow(a, b, &result)) {
        kaula_report_fatal("integer overflow in subtraction");
    }
    return result;
}

static inline int64_t kaula_mul_i64(int64_t a, int64_t b) {
    int64_t result;
    if (__builtin_mul_overflow(a, b, &result)) {
        kaula_report_fatal("integer overflow in multiplication");
    }
    return result;
}

static inline uint64_t kaula_add_u64(uint64_t a, uint64_t b) {
    uint64_t result;
    if (__builtin_add_overflow(a, b, &result)) {
        kaula_report_fatal("unsigned integer overflow in addition");
    }
    return result;
}

static inline uint64_t kaula_sub_u64(uint64_t a, uint64_t b) {
    uint64_t result;
    if (__builtin_sub_overflow(a, b, &result)) {
        kaula_report_fatal("unsigned integer overflow in subtraction");
    }
    return result;
}

static inline uint64_t kaula_mul_u64(uint64_t a, uint64_t b) {
    uint64_t result;
    if (__builtin_mul_overflow(a, b, &result)) {
        kaula_report_fatal("unsigned integer overflow in multiplication");
    }
    return result;
}

#endif
