/*
 * test_xj380_shim.c — xj380_shim.c 的主机端单元测试
 *
 * 不依赖 XJ380 内核、QEMU 或 libc 之外的任何东西：用桩函数捕获
 * enter_syscall 的 XAPI_OUTPUT 输出，直接验证行缓冲的全部边界。
 *
 * 编译：
 *   clang -std=c11 -O1 -Wall -Wextra -Werror \
 *       -I examples/xj380 -o /tmp/test_xj380_shim examples/xj380/test_xj380_shim.c
 * 运行：/tmp/test_xj380_shim   （退出码 0 表示全部通过）
 */

#include <stdio.h>
#include <string.h>
#include <stdint.h>

#define XAPI_OUTPUT 7381

static char captured[65536];
static size_t captured_len;
static int flush_calls;

/* 桩：只处理 XAPI_OUTPUT，其余系统调用忽略 */
uint64_t enter_syscall(uint64_t number, uint64_t a1, uint64_t a2, uint64_t a3,
                       uint64_t a4, uint64_t a5, uint64_t a6)
{
    (void)a2; (void)a3; (void)a4; (void)a5; (void)a6;
    if (number == XAPI_OUTPUT) {
        const char *s = (const char *)(uintptr_t)a1;
        size_t n = strlen(s);
        if (captured_len + n < sizeof(captured)) {
            memcpy(captured + captured_len, s, n);
            captured_len += n;
            captured[captured_len] = '\0';
        }
        flush_calls++;
    }
    return 0;
}

/* 桩：Kaula 入口由每个用例注入行为 */
typedef int32_t (*kaula_hook_fn)(void);
static kaula_hook_fn kaula_hook;
int32_t kaula_main(void)
{
    return kaula_hook ? kaula_hook() : 0;
}

/* 把被测实现的 main 改名，避免与测试自身的 main 冲突 */
#define main xj380_shim_main
#include "xj380_shim.c"
#undef main

static int failures;
static int checks;

static void check(int condition, const char *name)
{
    checks++;
    if (condition) {
        printf("PASS  %s\n", name);
    } else {
        printf("FAIL  %s\n", name);
        failures++;
    }
}

static void reset(void)
{
    captured[0] = '\0';
    captured_len = 0;
    flush_calls = 0;
    kaula_hook = NULL;
}

static void emit(const char *s)
{
    while (*s) fs_output_putchar(*s++);
}

/* ---------- 用例 ---------- */

static int32_t hook_single_line(void)
{
    emit("KAULA-XJ380-HELLO\n");
    return 0;
}

static int32_t hook_multi_line(void)
{
    emit("line-1\nline-2\nline-3\n");
    return 0;
}

static int32_t hook_no_newline(void)
{
    emit("tail-without-newline");
    return 0;
}

static int32_t hook_empty(void)
{
    return 0;
}

static int32_t hook_long_line(void)
{
    /* 600 字节、无换行：跨多次缓冲冲刷，验证不丢字符、不越界 */
    for (int i = 0; i < 600; i++) fs_output_putchar((char)('a' + (i % 26)));
    return 0;
}

static int32_t hook_return_code(void)
{
    emit("rc\n");
    return 42;
}

static int32_t hook_exact_capacity(void)
{
    /* 恰好 255 字节后跟换行：边界不得越界或丢字符 */
    for (int i = 0; i < 255; i++) fs_output_putchar('x');
    fs_output_putchar('\n');
    return 0;
}

int main(void)
{
    reset();
    kaula_hook = hook_single_line;
    check(xj380_shim_main(0, NULL, NULL) == 0, "单行输出返回 0");
    check(strcmp(captured, "KAULA-XJ380-HELLO\n") == 0, "单行内容一致");
    check(flush_calls == 1, "单行只触发一次冲刷");

    reset();
    kaula_hook = hook_multi_line;
    (void)xj380_shim_main(0, NULL, NULL);
    check(strcmp(captured, "line-1\nline-2\nline-3\n") == 0, "多行内容与顺序一致");
    check(flush_calls == 3, "多行逐行冲刷");

    reset();
    kaula_hook = hook_no_newline;
    (void)xj380_shim_main(0, NULL, NULL);
    check(strcmp(captured, "tail-without-newline") == 0, "无换行结尾在返回时冲刷");
    check(flush_calls == 1, "尾部冲刷一次");

    reset();
    kaula_hook = hook_empty;
    (void)xj380_shim_main(0, NULL, NULL);
    check(captured_len == 0 && flush_calls == 0, "空输出不产生系统调用");

    reset();
    kaula_hook = hook_long_line;
    (void)xj380_shim_main(0, NULL, NULL);
    check(strlen(captured) == 600, "600 字节超长行无字符丢失");
    check(captured[0] == 'a' && captured[599] == (char)('a' + (599 % 26)),
          "超长行首尾字节正确");

    reset();
    kaula_hook = hook_return_code;
    check(xj380_shim_main(0, NULL, NULL) == 42, "kaula_main 返回码透传");
    check(strcmp(captured, "rc\n") == 0, "返回码用例输出正确");

    reset();
    kaula_hook = hook_exact_capacity;
    (void)xj380_shim_main(0, NULL, NULL);
    check(strlen(captured) == 256, "恰好 255 字节 + 换行长度为 256");
    check(captured[254] == 'x' && captured[255] == '\n', "容量边界内容正确");

    printf("\n%d checks, %d failures\n", checks, failures);
    return failures == 0 ? 0 : 1;
}
