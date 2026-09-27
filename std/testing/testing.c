/* _POSIX_C_SOURCE 必须在所有 #include 之前定义，以确保 struct timespec
   和 clock_gettime 在 <time.h> 中可见（Linux 严格模式要求） */
#ifndef _POSIX_C_SOURCE
#define _POSIX_C_SOURCE 200809L
#endif

#include "testing.h"
#include "../memory/memory.h"
#include <string.h>
#include <stdio.h>
#ifdef _WIN32
#include <windows.h>
#else
#include <time.h>
#endif

static TestReport* g_current_report = NULL;
static TestSuite** g_all_suites = NULL;
static TestCase** g_all_cases = NULL;
static size_t g_suite_count = 0;
static size_t g_case_count = 0;
static size_t g_suite_capacity = 16;
static size_t g_case_capacity = 64;

TestSuite* test_suite_create(const String name, void (*setup)(void), void (*teardown)(void)) {
    TestSuite* suite = (TestSuite*)kmm_v4_calloc(1, sizeof(TestSuite));
    if (suite) {
        suite->name = string_copy(name);
        suite->setup = setup;
        suite->teardown = teardown;
    }
    /* g_all_suites 初始为 NULL，容量判断 (0 >= 16) 为假，
     * 旧实现因此永远不分配数组，首次 test_suite_create 就写入 NULL 指针。 */
    if (!g_all_suites) {
        g_all_suites = (TestSuite**)kmm_v4_calloc(g_suite_capacity, sizeof(TestSuite*));
        if (!g_all_suites) return suite;
    }
    if (g_suite_count >= g_suite_capacity) {
        size_t newcap = g_suite_capacity * 2;
        TestSuite** grown = (TestSuite**)kmm_v4_realloc(g_all_suites, newcap * sizeof(TestSuite*));
        if (!grown) return suite;
        g_all_suites = grown;
        g_suite_capacity = newcap;
    }
    g_all_suites[g_suite_count++] = suite;
    return suite;
}

void test_suite_destroy(TestSuite* suite) {
    // KMM 管理内存，无需手动释放
    (void)suite;
}

TestCase* test_case_create(const String name, void (*func)(void), const String suite_name) {
    TestCase* tc = (TestCase*)kmm_v4_calloc(1, sizeof(TestCase));
    if (tc) {
        tc->name = string_copy(name);
        tc->func = func;
        tc->suite_name = suite_name.len ? string_copy(suite_name) : (String){0, NULL};
    }
    /* 同 test_suite_create：首次调用必须先分配，否则写入 NULL 指针 */
    if (!g_all_cases) {
        g_all_cases = (TestCase**)kmm_v4_calloc(g_case_capacity, sizeof(TestCase*));
        if (!g_all_cases) return tc;
    }
    if (g_case_count >= g_case_capacity) {
        size_t newcap = g_case_capacity * 2;
        TestCase** grown = (TestCase**)kmm_v4_realloc(g_all_cases, newcap * sizeof(TestCase*));
        if (!grown) return tc;
        g_all_cases = grown;
        g_case_capacity = newcap;
    }
    g_all_cases[g_case_count++] = tc;
    return tc;
}

TestReport* test_run_all() {
    if (g_case_count == 0) {
        TestReport* report = (TestReport*)kmm_v4_calloc(1, sizeof(TestReport));
        return report;
    }
    TestReport* report = (TestReport*)kmm_v4_calloc(1, sizeof(TestReport));
    report->capacity = g_case_count;
    report->results = (TestResult*)kmm_v4_calloc(g_case_count, sizeof(TestResult));
    g_current_report = report;
    for (size_t i = 0; i < g_case_count; i++) {
        TestCase* tc = g_all_cases[i];
        TestSuite* suite = NULL;
        if (tc->suite_name.len > 0) {
            for (size_t j = 0; j < g_suite_count; j++) {
                if (string_equals(g_all_suites[j]->name, tc->suite_name)) {
                    suite = g_all_suites[j];
                    break;
                }
            }
        }
        if (suite && suite->setup) suite->setup();

        /* 关键顺序：必须先占好 results[count] 槽位并让 count 前移，
         * 再执行 tc->func()。
         *
         * test_assert_* 通过 g_current_report->results[g_current_report->count - 1]
         * 定位「当前正在跑的用例」。若在 func() 之后才 count++，func() 期间
         * count 仍指向本用例的下标，count-1 会落到上一个用例上，
         * 导致断言失败被记到错误的用例，而本用例因之后被无条件
         * passed = true 覆盖成「通过」——测试永远不会失败。 */
        TestResult* r = &report->results[report->count];
        r->name = string_copy(tc->name);
        r->passed = true;            /* 乐观默认：断言失败时由 test_assert_* 翻成 false */
        r->message = (String){0, NULL};
        r->file = (String){0, NULL};
        r->line = 0;
        r->duration_ms = 0;
        report->count++;

        i64 start = 0; i64 end = 0;
        #ifdef _WIN32
        LARGE_INTEGER freq, c1, c2;
        QueryPerformanceFrequency(&freq); QueryPerformanceCounter(&c1);
        start = c1.QuadPart;
        tc->func();
        QueryPerformanceCounter(&c2);
        end = c2.QuadPart;
        r->duration_ms = (end - start) * 1000 / freq.QuadPart;
        #else
        struct timespec t1, t2;
        clock_gettime(CLOCK_MONOTONIC, &t1); start = t1.tv_sec * 1000 + t1.tv_nsec / 1000000;
        tc->func();
        clock_gettime(CLOCK_MONOTONIC, &t2); end = t2.tv_sec * 1000 + t2.tv_nsec / 1000000;
        r->duration_ms = end - start;
        #endif
        if (suite && suite->teardown) suite->teardown();
    }
    for (size_t i = 0; i < report->count; i++) {
        if (report->results[i].passed) report->passed++; else report->failed++;
        report->total_duration_ms += report->results[i].duration_ms;
    }
    g_current_report = NULL;
    return report;
}

void test_report_print(TestReport* report) {
    if (!report) return;
    print("=== Test Report ===\n");
    print("Total: %d, Passed: %d, Failed: %d\n", report->count, report->passed, report->failed);
    print("Total Time: %lld ms\n", report->total_duration_ms);
    for (size_t i = 0; i < report->count; i++) {
        TestResult* r = &report->results[i];
        if (r->passed) {
            print("  [PASS] %s (%lld ms)\n", string_cdata(r->name), r->duration_ms);
        } else {
            print("  [FAIL] %s: %s (%s:%d)\n", string_cdata(r->name), string_cdata(r->message), string_cdata(r->file), r->line);
        }
    }
    if (report->failed == 0) print("All tests passed!\n");
    else print("%d test(s) failed!\n", report->failed);
}

void test_report_destroy(TestReport* report) {
    // KMM 管理内存，无需手动释放
    (void)report;
}

bool_t test_assert_true(bool_t condition, const char* message, const char* file, int line) {
    if (!g_current_report || g_current_report->count == 0) return condition;
    TestResult* r = &g_current_report->results[g_current_report->count - 1];
    if (!condition) {
        r->passed = false;
        r->message = string_create(message);
        r->file = string_create(file);
        r->line = line;
    }
    return condition;
}

bool_t test_assert_false(bool_t condition, const char* message, const char* file, int line) {
    return test_assert_true(!condition, message, file, line);
}

bool_t test_assert_equals(i64 expected, i64 actual, const char* message, const char* file, int line) {
    if (expected == actual) return true;
    if (!g_current_report || g_current_report->count == 0) return false;
    TestResult* r = &g_current_report->results[g_current_report->count - 1];
    r->passed = false;
    /* 此前这里先拼 "expected != actual" 再 string_free 掉并用 message 覆盖，
     * 纯属白做功且泄漏临时串；只保留 message。 */
    r->message = string_create(message);
    r->file = string_create(file);
    r->line = line;
    return false;
}

bool_t test_assert_string_equals(const char* expected, const char* actual, const char* message, const char* file, int line) {
    if (string_equals(string_create(expected), string_create(actual))) return true;
    if (!g_current_report || g_current_report->count == 0) return false;
    TestResult* r = &g_current_report->results[g_current_report->count - 1];
    r->passed = false;
    r->message = string_create(message);
    r->file = string_create(file);
    r->line = line;
    return false;
}

bool_t test_assert_null(void* ptr, const char* message, const char* file, int line) {
    return test_assert_true(ptr == NULL, message, file, line);
}

bool_t test_assert_not_null(void* ptr, const char* message, const char* file, int line) {
    return test_assert_true(ptr != NULL, message, file, line);
}

bool_t test_assert_int_range(i64 value, i64 minimum, i64 maximum, const char* message, const char* file, int line) {
    return test_assert_true(value >= minimum && value <= maximum, message, file, line);
}
