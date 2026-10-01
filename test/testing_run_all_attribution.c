/* 回归：test_run_all 的结果槽位分配顺序 + 用例数组首次分配。
 *
 * 两处曾经的缺陷：
 * 1) g_all_cases / g_all_suites 初始为 NULL，而容量判断 (0 >= 64) 为假，
 *    导致首次 test_case_create 就往 NULL 指针写 —— 整个 C 级 runner 不可用。
 * 2) 先跑 tc->func()、之后才 count++ 并无条件写 passed = true，而
 *    test_assert_* 通过 results[g_current_report->count - 1] 定位「当前用例」。
 *    于是断言失败被记到上一个用例头上，当前用例又被 passed = true 覆盖，
 *    test_run_all 永远报告全通过。
 *
 * 本测试构造「失败-通过-失败」顺序，验证失败归到正确用例且汇总计数正确。
 * 注意：String 有 SSO（<=15 字节内联在 .sso），不可直接读 .ptr，用 string_equals 比较。
 *
 * 不用 assert()：本仓库在 Windows + clang/MSVC CRT 下，断言失败走 _wassert
 * 会尝试弹窗，在无交互会话中直接触发 0xC0000005，看不到真正的失败原因。
 */
#include "../std/testing/testing.h"
#include <stdio.h>

static int failures = 0;

#define CHECK(cond, what)                                                        \
    do {                                                                          \
        if (cond) {                                                              \
            printf("  ok   %s\n", (what));                                        \
        } else {                                                                 \
            printf("  FAIL %s\n", (what));                                        \
            failures++;                                                          \
        }                                                                         \
    } while (0)

static int checks_run = 0;

static void case_fail(void) {
    checks_run++;
    (void)test_assert_equals(1, 2, "expected 1 got 2", __FILE__, __LINE__);
}

static void case_pass(void) {
    checks_run++;
    (void)test_assert_equals(3, 3, "sanity", __FILE__, __LINE__);
}

static void case_fail2(void) {
    checks_run++;
    (void)test_assert_true(0, "boom", __FILE__, __LINE__);
}

int main(void) {
    /* 首次调用即触发数组惰性分配（旧实现在此崩在 NULL 指针上） */
    test_case_create(string_create("A_fail"),  case_fail,  (String){0, NULL});
    test_case_create(string_create("B_pass"),  case_pass,  (String){0, NULL});
    test_case_create(string_create("C_fail2"), case_fail2, (String){0, NULL});

    TestReport* report = test_run_all();
    if (!report) {
        printf("FAIL test_run_all returned NULL\n");
        return 1;
    }

    CHECK(checks_run == 3,           "all 3 cases executed");
    CHECK(report->count == 3,        "report has 3 results");

    CHECK(string_equals(report->results[0].name, string_create("A_fail")),  "result[0] is A_fail");
    CHECK(string_equals(report->results[1].name, string_create("B_pass")),  "result[1] is B_pass");
    CHECK(string_equals(report->results[2].name, string_create("C_fail2")), "result[2] is C_fail2");

    /* 失败必须落在 A 和 C 上，B 必须通过。
     * 若 A 的失败被错记到 B，B 就会变成失败。 */
    CHECK(report->results[0].passed == false, "A_fail marked failed");
    CHECK(report->results[1].passed == true,  "B_pass marked passed");
    CHECK(report->results[2].passed == false, "C_fail2 marked failed");

    CHECK(report->failed == 2, "summary failed == 2");
    CHECK(report->passed == 1, "summary passed == 1");

    /* 失败位置只落在失败用例上 */
    CHECK(report->results[0].line != 0, "A_fail recorded a source line");
    CHECK(report->results[1].line == 0, "B_pass recorded no source line");
    CHECK(report->results[2].line != 0, "C_fail2 recorded a source line");

    if (failures) {
        printf("FAILED: %d check(s)\n", failures);
        return 1;
    }
    printf("OK: test_run_all attributes failures to the right case\n");
    return 0;
}
