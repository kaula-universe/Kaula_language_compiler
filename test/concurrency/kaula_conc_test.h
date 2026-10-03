#ifndef KAULA_CONC_TEST_H
#define KAULA_CONC_TEST_H

/*
 * Kaula 并发内核 · 测试骨架
 *
 * 极简断言框架：每个测试文件独立成一个可执行文件，
 * 返回 0 表示全部通过，非 0 表示失败。
 */

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <stdatomic.h>

#ifdef __cplusplus
extern "C" {
#endif

/* 测试统计 */
static int g_test_pass = 0;
static int g_test_fail = 0;
static const char* g_test_current = "";

/* ANSI 颜色：非 TTY 时自动禁用 */
#if defined(_WIN32) && !defined(KAULA_TEST_NO_COLOR)
    #include <io.h>
    #define KAULA_TEST_COLOR 1
    static int g_test_tty(void) { return _isatty(_fileno(stdout)); }
    #define KAULA_TEST_RED(s)   (g_test_tty() ? "\033[31m" s "\033[0m" : (s))
    #define KAULA_TEST_GREEN(s) (g_test_tty() ? "\033[32m" s "\033[0m" : (s))
    #define KAULA_TEST_YELLOW(s)(g_test_tty() ? "\033[33m" s "\033[0m" : (s))
#else
    #define KAULA_TEST_RED(s)   (s)
    #define KAULA_TEST_GREEN(s) (s)
    #define KAULA_TEST_YELLOW(s)(s)
    #define KAULA_TEST_TTY()    (1)
#endif

/* ========== 断言 ========== */

#define TEST_CASE(name) \
    do { g_test_current = (name); } while (0)

#define TEST_PASS() \
    do { g_test_pass++; } while (0)

#define TEST_FAIL(fmt, ...) \
    do { \
        g_test_fail++; \
        fprintf(stderr, "%s:%d  [FAIL] %s: " fmt "\n", \
                __FILE__, __LINE__, g_test_current, ##__VA_ARGS__); \
    } while (0)

/* 无格式化的断言 */
#define ASSERT_TRUE(cond, msg) \
    do { \
        if (cond) { TEST_PASS(); } \
        else { TEST_FAIL("%s", msg); } \
    } while (0)

/* 带格式化的断言（想看到实际值时用这个） */
#define ASSERT_MSG(cond, fmt, ...) \
    do { \
        if (cond) { TEST_PASS(); } \
        else { TEST_FAIL(fmt, __VA_ARGS__); } \
    } while (0)

#define ASSERT_FALSE(cond, msg) \
    do { \
        if (!(cond)) { TEST_PASS(); } \
        else { TEST_FAIL("%s", msg); } \
    } while (0)

#define ASSERT_EQ(actual, expected, msg) \
    do { \
        long long _a = (long long)(actual); \
        long long _e = (long long)(expected); \
        if (_a == _e) { TEST_PASS(); } \
        else { TEST_FAIL("%s (got %lld, want %lld)", msg, _a, _e); } \
    } while (0)

#define ASSERT_NE(actual, unexpected, msg) \
    do { \
        long long _a = (long long)(actual); \
        long long _u = (long long)(unexpected); \
        if (_a != _u) { TEST_PASS(); } \
        else { TEST_FAIL("%s (both %lld)", msg, _a); } \
    } while (0)

#define ASSERT_NOT_NULL(ptr, msg) \
    do { \
        if ((ptr) != NULL) { TEST_PASS(); } \
        else { TEST_FAIL("%s (got NULL)", msg); } \
    } while (0)

#define ASSERT_NULL(ptr, msg) \
    do { \
        if ((ptr) == NULL) { TEST_PASS(); } \
        else { TEST_FAIL("%s (got non-NULL)", msg); } \
    } while (0)

/* ========== 分节输出 ========== */

#define TEST_SECTION(title) \
    printf("\n--- %s ---\n", (title))

static int test_report(const char* suite) {
    printf("\n%s: %d passed, %d failed\n",
           suite, g_test_pass, g_test_fail);
    if (g_test_fail == 0) {
        printf("%s\n", KAULA_TEST_GREEN("ALL PASS"));
        return 0;
    }
    printf("%s\n", KAULA_TEST_RED("FAILED"));
    return 1;
}

#ifdef __cplusplus
}
#endif

#endif /* KAULA_CONC_TEST_H */