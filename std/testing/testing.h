#ifndef STD_TESTING_TESTING_H
#define STD_TESTING_TESTING_H

#include "../base/types.h"
#include "../string/string.h"
#include "../io/io.h"

typedef struct {
    String name;
    void (*setup)(void);
    void (*teardown)(void);
} TestSuite;

typedef struct {
    String name;
    void (*func)(void);
    String suite_name;
} TestCase;

typedef struct {
    String name;
    bool_t passed;
    String message;
    i64 duration_ms;
    String file;
    int line;
} TestResult;

typedef struct {
    TestResult* results;
    size_t count;
    size_t capacity;
    size_t passed;
    size_t failed;
    i64 total_duration_ms;
} TestReport;

extern TestSuite* test_suite_create(const String name, void (*setup)(void), void (*teardown)(void));
extern void test_suite_destroy(TestSuite* suite);

extern TestCase* test_case_create(const String name, void (*func)(void), const String suite_name);

extern TestReport* test_run_all();
extern TestReport* test_run_suite(TestSuite* suite, TestCase** cases, size_t count);

extern void test_report_print(TestReport* report);
extern void test_report_destroy(TestReport* report);

extern bool_t test_assert_true(bool_t condition, const String message, const String file, int line);
extern bool_t test_assert_false(bool_t condition, const String message, const String file, int line);
extern bool_t test_assert_equals(i64 expected, i64 actual, const String message, const String file, int line);
extern bool_t test_assert_string_equals(const String expected, const String actual, const String message, const String file, int line);
extern bool_t test_assert_null(void* ptr, const String message, const String file, int line);
extern bool_t test_assert_not_null(void* ptr, const String message, const String file, int line);

// Macros for convenience
#define TEST_ASSERT_TRUE(cond) test_assert_true(cond, #cond, __FILE__, __LINE__)
#define TEST_ASSERT_FALSE(cond) test_assert_false(cond, #cond, __FILE__, __LINE__)
#define TEST_ASSERT_EQ(expected, actual) test_assert_equals(expected, actual, #expected " == " #actual, __FILE__, __LINE__)
#define TEST_ASSERT_STR_EQ(expected, actual) test_assert_string_equals(expected, actual, #expected " == " #actual, __FILE__, __LINE__)
#define TEST_ASSERT_NULL(ptr) test_assert_null(ptr, #ptr " == NULL", __FILE__, __LINE__)
#define TEST_ASSERT_NOT_NULL(ptr) test_assert_not_null(ptr, #ptr " != NULL", __FILE__, __LINE__)

#define TEST_CASE(name, suite) TestCase* tc_##name = test_case_create(#name, test_##name, #suite)
#define TEST_FUNC(name) void test_##name(void)

#endif // STD_TESTING_TESTING_H
