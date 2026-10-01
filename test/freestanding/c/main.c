/* main.c — freestanding 库 C 层测试入口（台架 + 各套件，单 TU）
 *
 * 编译两次（实现由外部提供，本 TU 只含台架与用例）:
 *   freestanding 模式: main.c + src/kaula_freestanding_runtime.c(-DKAULA_FREESTANDING)
 *                      → memset/memcpy/strlen 等为库自身实现
 *   托管模式:          main.c + build/lib/kaula_freestanding.lib
 *                      → libc 同名强符号优先, 仅验证无冲突的 fs_/math_/print 函数
 *
 * 强符号覆写弱钩子 fs_output_putchar 的能力在两种模式下都被验证（COFF weak override）。
 */
#include "fs_test.h"

/* ============================ 台账 ============================ */

FsFunc g_ledger[FS_LEDGER_MAX];
int g_ledger_n = 0;
FsFunc *g_cur = NULL;

void fs_open(const char *mod, const char *fn) {
    if (g_ledger_n < FS_LEDGER_MAX) {
        g_ledger[g_ledger_n].mod = mod;
        g_ledger[g_ledger_n].fn = fn;
        g_ledger[g_ledger_n].checks = 0;
        g_ledger[g_ledger_n].fails = 0;
        g_ledger_n++;
        g_cur = &g_ledger[g_ledger_n - 1];
    } else {
        fprintf(stderr, "ledger overflow at %s/%s\n", mod, fn);
        g_cur = &g_ledger[0];
    }
}

void fs_fail_at(const char *file, int line, const char *fmt, ...) {
    va_list ap;
    g_cur->fails++;
    fprintf(stderr, "  FAIL %s/%s @ %s:%d: ", g_cur->mod, g_cur->fn, file, line);
    va_start(ap, fmt);
    vfprintf(stderr, fmt, ap);
    va_end(ap);
    fputc('\n', stderr);
    fflush(stderr);
}

/* ============================ 输出捕获 ============================ */

char g_cap[FS_CAP_MAX];
size_t g_cap_len = 0;

/* 强符号覆写 io.c 的弱钩子: 捕获 io 模块真实字节流 */
void fs_output_putchar(char c) {
    if (g_cap_len < FS_CAP_MAX - 1) g_cap[g_cap_len++] = c;
    g_cap[g_cap_len] = '\0';
}

void cap_reset(void) {
    g_cap_len = 0;
    g_cap[0] = '\0';
}

const char *cap_str(void) {
    g_cap[g_cap_len] = '\0';
    return g_cap;
}

size_t cap_len(void) {
    return g_cap_len;
}

int cap_eq(const char *want, const char *label) {
    g_cur->checks++;
    const char *got = cap_str();
    if (fs_tcmp(got, want) != 0) {
        fs_fail_at(__FILE__, __LINE__, "%s\n         got  \"%s\"\n         want \"%s\"",
                   label, got, want);
        return 0;
    }
    return 1;
}

/* ============================ 台架工具 ============================ */

int fs_tcmp(const char *a, const char *b) {
    /* FS 构建下走被测 strcmp, 托管构建下走 libc strcmp */
    return strcmp(a, b);
}

int fs_memcmp_t(const void *a, const void *b, size_t n) {
    return memcmp(a, b, n);
}

void fs_copy_str(char *dst, size_t cap, const char *src) {
    size_t i = 0;
    if (cap == 0) return;
    while (src[i] != 0 && i < cap - 1) { dst[i] = src[i]; i++; }
    dst[i] = '\0';
}

int fs_same_str(const char *a, const char *b) {
    size_t i = 0;
    while (a[i] != 0 && a[i] == b[i]) i++;
    return (int)(unsigned char)a[i] - (int)(unsigned char)b[i];
}

static double bits_to_f64(uint64_t u) {
    double d;
    memcpy(&d, &u, sizeof(d));
    return d;
}

double fs_nan(void)  { return bits_to_f64(0x7FF8000000000000ULL); }
double fs_pinf(void) { return bits_to_f64(0x7FF0000000000000ULL); }
double fs_ninf(void) { return bits_to_f64(0xFFF0000000000000ULL); }

/* ============================ 套件 ============================ */

#include "test_base.c"
#include "test_memory.c"
#include "test_string.c"
#include "test_math.c"
#include "test_io.c"

/* ============================ main ============================ */

int main(void) {
#ifdef KAULA_FREESTANDING
    printf("=== freestanding lib test suite (mode: freestanding, -DKAULA_FREESTANDING) ===\n");
#else
    printf("=== freestanding lib test suite (mode: hosted, libc strong symbols win) ===\n");
#endif

    suite_base();
    suite_memory();
    suite_string();
    suite_math();
    suite_io();
    int fn_fail = 0, chk_total = 0, chk_fail = 0;
    printf("\n---- per-function ledger ----\n");
    for (int i = 0; i < g_ledger_n; i++) {
        FsFunc *f = &g_ledger[i];
        chk_total += f->checks;
        chk_fail += f->fails;
        if (f->fails == 0) {
            printf("  PASS  %-14s %-24s %3d checks\n", f->mod, f->fn, f->checks);
        } else {
            fn_fail++;
            printf("  FAIL  %-14s %-24s %3d checks, %d failed\n",
                   f->mod, f->fn, f->checks, f->fails);
        }
    }
    printf("\nFUNCTIONS: %d tested, %d passed, %d failed\n",
           g_ledger_n, g_ledger_n - fn_fail, fn_fail);
    printf("CHECKS   : %d total, %d failed\n", chk_total, chk_fail);
    if (fn_fail || chk_fail) {
        printf("RESULT   : FAILED\n");
        return 1;
    }
    printf("RESULT   : PASSED\n");
    return 0;
}
