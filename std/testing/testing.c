#include "testing.h"
#include <stdlib.h>
#include <string.h>
#include <stdio.h>

static TestReport* g_current_report = NULL;
static TestSuite** g_all_suites = NULL;
static TestCase** g_all_cases = NULL;
static size_t g_suite_count = 0;
static size_t g_case_count = 0;
static size_t g_suite_capacity = 16;
static size_t g_case_capacity = 64;

TestSuite* test_suite_create(const String name, void (*setup)(void), void (*teardown)(void)) {
    TestSuite* suite = (TestSuite*)calloc(1, sizeof(TestSuite));
    if (suite) {
        suite->name = string_copy(name);
        suite->setup = setup;
        suite->teardown = teardown;
    }
    if (g_suite_count >= g_suite_capacity) {
        g_suite_capacity *= 2;
        g_all_suites = (TestSuite**)realloc(g_all_suites, g_suite_capacity * sizeof(TestSuite*));
    }
    g_all_suites[g_suite_count++] = suite;
    return suite;
}

void test_suite_destroy(TestSuite* suite) {
    if (suite) {
        string_free(suite->name);
        free(suite);
    }
}

TestCase* test_case_create(const String name, void (*func)(void), const String suite_name) {
    TestCase* tc = (TestCase*)calloc(1, sizeof(TestCase));
    if (tc) {
        tc->name = string_copy(name);
        tc->func = func;
        tc->suite_name = suite_name ? string_copy(suite_name) : NULL;
    }
    if (g_case_count >= g_case_capacity) {
        g_case_capacity *= 2;
        g_all_cases = (TestCase**)realloc(g_all_cases, g_case_capacity * sizeof(TestCase*));
    }
    g_all_cases[g_case_count++] = tc;
    return tc;
}

TestReport* test_run_all() {
    if (g_case_count == 0) {
        TestReport* report = (TestReport*)calloc(1, sizeof(TestReport));
        return report;
    }
    TestReport* report = (TestReport*)calloc(1, sizeof(TestReport));
    report->capacity = g_case_count;
    report->results = (TestResult*)calloc(g_case_count, sizeof(TestResult));
    g_current_report = report;
    for (size_t i = 0; i < g_case_count; i++) {
        TestCase* tc = g_all_cases[i];
        TestSuite* suite = NULL;
        if (tc->suite_name) {
            for (size_t j = 0; j < g_suite_count; j++) {
                if (string_equals(g_all_suites[j]->name, tc->suite_name)) {
                    suite = g_all_suites[j];
                    break;
                }
            }
        }
        if (suite && suite->setup) suite->setup();
        i64 start = 0; i64 end = 0;
        #ifdef _WIN32
        LARGE_INTEGER freq, c1, c2;
        QueryPerformanceFrequency(&freq); QueryPerformanceCounter(&c1);
        start = c1.QuadPart;
        tc->func();
        QueryPerformanceCounter(&c2);
        end = c2.QuadPart;
        report->results[report->count].duration_ms = (end - start) * 1000 / freq.QuadPart;
        #else
        struct timespec t1, t2;
        clock_gettime(CLOCK_MONOTONIC, &t1); start = t1.tv_sec * 1000 + t1.tv_nsec / 1000000;
        tc->func();
        clock_gettime(CLOCK_MONOTONIC, &t2); end = t2.tv_sec * 1000 + t2.tv_nsec / 1000000;
        report->results[report->count].duration_ms = end - start;
        #endif
        report->results[report->count].name = string_copy(tc->name);
        report->results[report->count].passed = true;
        report->count++;
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
            print("  [PASS] %s (%lld ms)\n", r->name, r->duration_ms);
        } else {
            print("  [FAIL] %s: %s (%s:%d)\n", r->name, r->message, r->file, r->line);
        }
    }
    if (report->failed == 0) print("All tests passed!\n");
    else print("%d test(s) failed!\n", report->failed);
}

void test_report_destroy(TestReport* report) {
    if (report) {
        for (size_t i = 0; i < report->count; i++) {
            string_free(report->results[i].name);
            string_free(report->results[i].message);
            string_free(report->results[i].file);
        }
        free(report->results);
        free(report);
    }
}

bool_t test_assert_true(bool_t condition, const String message, const String file, int line) {
    if (!g_current_report || g_current_report->count == 0) return condition;
    TestResult* r = &g_current_report->results[g_current_report->count - 1];
    if (!condition) {
        r->passed = false;
        r->message = string_copy(message);
        r->file = string_copy(file);
        r->line = line;
    }
    return condition;
}

bool_t test_assert_false(bool_t condition, const String message, const String file, int line) {
    return test_assert_true(!condition, message, file, line);
}

bool_t test_assert_equals(i64 expected, i64 actual, const String message, const String file, int line) {
    if (expected == actual) return true;
    if (!g_current_report || g_current_report->count == 0) return false;
    TestResult* r = &g_current_report->results[g_current_report->count - 1];
    r->passed = false;
    r->message = string_create_from_int(expected);
    r->message = string_concat(r->message, string_create(" != "));
    r->message = string_concat(r->message, string_create_from_int(actual));
    string_free(r->message); r->message = string_copy(message);
    r->file = string_copy(file);
    r->line = line;
    return false;
}

bool_t test_assert_string_equals(const String expected, const String actual, const String message, const String file, int line) {
    if (string_equals(expected, actual)) return true;
    if (!g_current_report || g_current_report->count == 0) return false;
    TestResult* r = &g_current_report->results[g_current_report->count - 1];
    r->passed = false;
    r->message = string_copy(message);
    r->file = string_copy(file);
    r->line = line;
    return false;
}

bool_t test_assert_null(void* ptr, const String message, const String file, int line) {
    return test_assert_true(ptr == NULL, message, file, line);
}

bool_t test_assert_not_null(void* ptr, const String message, const String file, int line) {
    return test_assert_true(ptr != NULL, message, file, line);
}
