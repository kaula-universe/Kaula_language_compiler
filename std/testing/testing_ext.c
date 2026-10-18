#include "testing_ext.h"
#include "../memory/memory.h"
#include "../time/time.h"
#include "../io/io.h"
#include <stdio.h>
#include <string.h>
#include <stdlib.h>
#include <stdarg.h>

static TestSuiteNode* g_suites = NULL;
static TestStats g_stats = {0, 0, 0, 0.0};
static FILE* g_output = NULL;
static bool_t g_expect_failed = false;

static void output_print(const char* fmt, ...) {
    va_list args;
    va_start(args, fmt);
    if (g_output) {
        vfprintf(g_output, fmt, args);
    } else {
        vprintf(fmt, args);
    }
    va_end(args);
}

static TestSuiteNode* find_suite(const char* name) {
    TestSuiteNode* s = g_suites;
    while (s) {
        if (strcmp(s->name.ptr, name) == 0) return s;
        s = s->next;
    }
    return NULL;
}

void test_register_suite(const char* name) {
    TestSuiteNode* existing;
    TestSuiteNode* suite;
    if (!name) return;
    existing = find_suite(name);
    if (existing) return;
    suite = (TestSuiteNode*)kmm_v4_malloc(sizeof(TestSuiteNode));
    if (!suite) return;
    suite->name = string_create(name);
    suite->cases = NULL;
    suite->case_count = 0;
    suite->next = g_suites;
    g_suites = suite;
}

void test_register_case(const char* suite_name, const char* case_name, TestFunc func) {
    TestSuiteNode* suite;
    TestCaseNode* c;
    if (!suite_name || !case_name || !func) return;
    suite = find_suite(suite_name);
    if (!suite) {
        test_register_suite(suite_name);
        suite = find_suite(suite_name);
        if (!suite) return;
    }
    c = (TestCaseNode*)kmm_v4_malloc(sizeof(TestCaseNode));
    if (!c) return;
    c->name = string_create(case_name);
    c->func = func;
    c->next = suite->cases;
    suite->cases = c;
    suite->case_count++;
}

void test_assert_with_msg(bool_t condition, const char* msg, const char* file, int line) {
    if (!condition) {
        output_print("Assertion failed: %s\n  at %s:%d\n", msg ? msg : "unknown", file ? file : "?", line);
        abort();
    }
}

void test_expect_true(bool_t condition, const char* file, int line) {
    if (!condition) {
        output_print("Expectation failed: expected true\n  at %s:%d\n", file ? file : "?", line);
        g_expect_failed = true;
    }
}

void test_expect_false(bool_t condition, const char* file, int line) {
    if (condition) {
        output_print("Expectation failed: expected false\n  at %s:%d\n", file ? file : "?", line);
        g_expect_failed = true;
    }
}

void test_expect_equal_int(int expected, int actual, const char* file, int line) {
    if (expected != actual) {
        output_print("Expectation failed: expected %d, got %d\n  at %s:%d\n", expected, actual, file ? file : "?", line);
        g_expect_failed = true;
    }
}

void test_expect_equal_string(const char* expected, const char* actual, const char* file, int line) {
    if (!expected || !actual || strcmp(expected, actual) != 0) {
        output_print("Expectation failed: expected \"%s\", got \"%s\"\n  at %s:%d\n",
                     expected ? expected : "(null)", actual ? actual : "(null)",
                     file ? file : "?", line);
        g_expect_failed = true;
    }
}

static void reset_expect_flag(void) {
    g_expect_failed = false;
}

static bool_t get_expect_flag(void) {
    return g_expect_failed;
}

void test_run_suite_by_name(const char* suite_name) {
    TestSuiteNode* suite;
    TestCaseNode* c;
    double start_ms;
    if (!suite_name) return;
    suite = find_suite(suite_name);
    if (!suite) {
        output_print("Suite not found: %s\n", suite_name);
        return;
    }
    output_print("=== Running suite: %s ===\n", suite->name.ptr);
    start_ms = time_now_ms();
    c = suite->cases;
    while (c) {
        bool_t failed;
        output_print("  [ RUN      ] %s\n", c->name.ptr);
        reset_expect_flag();
        g_stats.total++;
        c->func();
        failed = get_expect_flag();
        if (failed) {
            g_stats.failed++;
            output_print("  [  FAILED  ] %s\n", c->name.ptr);
        } else {
            g_stats.passed++;
            output_print("  [       OK ] %s\n", c->name.ptr);
        }
        c = c->next;
    }
    g_stats.elapsed_ms += time_now_ms() - start_ms;
    output_print("=== Suite %s: %zu passed, %zu failed ===\n\n",
                 suite->name.ptr, g_stats.passed, g_stats.failed);
}

void test_run_all_suites(void) {
    TestSuiteNode* s = g_suites;
    double start_ms = time_now_ms();
    g_stats.total = 0;
    g_stats.passed = 0;
    g_stats.failed = 0;
    g_stats.elapsed_ms = 0.0;
    output_print("========== Running all test suites ==========\n\n");
    while (s) {
        TestSuiteNode* next = s->next;
        test_run_suite_by_name(s->name.ptr);
        s = next;
    }
    g_stats.elapsed_ms = time_now_ms() - start_ms;
    output_print("========== Summary: %zu total, %zu passed, %zu failed (%.2f ms) ==========\n",
                 g_stats.total, g_stats.passed, g_stats.failed, g_stats.elapsed_ms);
}

void test_benchmark(const char* name, BenchmarkFunc func, size_t iterations) {
    double start_ms, elapsed_ms;
    size_t i;
    if (!name || !func || iterations == 0) return;
    output_print("[BENCHMARK] %s (%zu iterations)...\n", name, iterations);
    start_ms = time_now_ms();
    for (i = 0; i < iterations; i++) {
        func(i);
    }
    elapsed_ms = time_now_ms() - start_ms;
    output_print("[BENCHMARK] %s: %.2f ms total, %.6f ms per iteration\n",
                 name, elapsed_ms, elapsed_ms / (double)iterations);
}

void test_set_output(const char* filename) {
    if (g_output) {
        fclose(g_output);
        g_output = NULL;
    }
    if (filename) {
        g_output = fopen(filename, "w");
    }
}

void test_get_stats(TestStats* stats) {
    if (!stats) return;
    stats->total = g_stats.total;
    stats->passed = g_stats.passed;
    stats->failed = g_stats.failed;
    stats->elapsed_ms = g_stats.elapsed_ms;
}
