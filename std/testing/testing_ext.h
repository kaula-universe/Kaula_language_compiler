#pragma once
#include "../base/types.h"
#include "../string/string.h"
#include "testing.h"

typedef struct TestStats {
    size_t total;
    size_t passed;
    size_t failed;
    double elapsed_ms;
} TestStats;

typedef void (*TestFunc)(void);
typedef void (*BenchmarkFunc)(size_t iteration);

typedef struct TestCaseNode {
    String name;
    TestFunc func;
    struct TestCaseNode* next;
} TestCaseNode;

typedef struct TestSuiteNode {
    String name;
    TestCaseNode* cases;
    size_t case_count;
    struct TestSuiteNode* next;
} TestSuiteNode;

void test_assert_with_msg(bool_t condition, const char* msg, const char* file, int line);

void test_expect_true(bool_t condition, const char* file, int line);
void test_expect_false(bool_t condition, const char* file, int line);
void test_expect_equal_int(int expected, int actual, const char* file, int line);
void test_expect_equal_string(const char* expected, const char* actual, const char* file, int line);

void test_register_suite(const char* name);
void test_register_case(const char* suite_name, const char* case_name, TestFunc func);

void test_run_suite_by_name(const char* suite_name);
void test_run_all_suites(void);

void test_benchmark(const char* name, BenchmarkFunc func, size_t iterations);

void test_set_output(const char* filename);
void test_get_stats(TestStats* stats);

#define TEST_ASSERT_WITH_MSG(cond, msg) test_assert_with_msg((cond), (msg), __FILE__, __LINE__)
#define TEST_EXPECT_TRUE(cond) test_expect_true((cond), __FILE__, __LINE__)
#define TEST_EXPECT_FALSE(cond) test_expect_false((cond), __FILE__, __LINE__)
#define TEST_EXPECT_EQUAL_INT(exp, act) test_expect_equal_int((exp), (act), __FILE__, __LINE__)
#define TEST_EXPECT_EQUAL_STRING(exp, act) test_expect_equal_string((exp), (act), __FILE__, __LINE__)
