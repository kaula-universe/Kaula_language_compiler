// kaula_runtime.h - Minimal stubs for Kaula runtime features
#ifndef KAULA_RUNTIME_H
#define KAULA_RUNTIME_H

#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>

// ===== 整数溢出检查（缺口4：debug 模式默认启用，零开销 __builtin_*_overflow） =====
// 使用 GCC/Clang 内置溢出检查函数，编译期优化为条件标志位检查，零运行时开销。
// 溢出时打印错误信息并 abort，与 KMM fatal error 处理一致。

static inline int64_t kaula_add_i64(int64_t a, int64_t b) {
    int64_t result;
    if (__builtin_add_overflow(a, b, &result)) {
        fprintf(stderr, "KAULA FATAL: integer overflow in addition (%lld + %lld)\n",
                (long long)a, (long long)b);
        abort();
    }
    return result;
}

static inline int64_t kaula_sub_i64(int64_t a, int64_t b) {
    int64_t result;
    if (__builtin_sub_overflow(a, b, &result)) {
        fprintf(stderr, "KAULA FATAL: integer overflow in subtraction (%lld - %lld)\n",
                (long long)a, (long long)b);
        abort();
    }
    return result;
}

static inline int64_t kaula_mul_i64(int64_t a, int64_t b) {
    int64_t result;
    if (__builtin_mul_overflow(a, b, &result)) {
        fprintf(stderr, "KAULA FATAL: integer overflow in multiplication (%lld * %lld)\n",
                (long long)a, (long long)b);
        abort();
    }
    return result;
}

static inline uint64_t kaula_add_u64(uint64_t a, uint64_t b) {
    uint64_t result;
    if (__builtin_add_overflow(a, b, &result)) {
        fprintf(stderr, "KAULA FATAL: unsigned integer overflow in addition (%llu + %llu)\n",
                (unsigned long long)a, (unsigned long long)b);
        abort();
    }
    return result;
}

static inline uint64_t kaula_sub_u64(uint64_t a, uint64_t b) {
    uint64_t result;
    if (__builtin_sub_overflow(a, b, &result)) {
        fprintf(stderr, "KAULA FATAL: unsigned integer overflow in subtraction (%llu - %llu)\n",
                (unsigned long long)a, (unsigned long long)b);
        abort();
    }
    return result;
}

static inline uint64_t kaula_mul_u64(uint64_t a, uint64_t b) {
    uint64_t result;
    if (__builtin_mul_overflow(a, b, &result)) {
        fprintf(stderr, "KAULA FATAL: unsigned integer overflow in multiplication (%llu * %llu)\n",
                (unsigned long long)a, (unsigned long long)b);
        abort();
    }
    return result;
}

#endif
