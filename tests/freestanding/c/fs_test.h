/* fs_test.h — freestanding 库 C 层测试台架
 *
 * 设计目标:
 *   1. "全函数逐个过关": 每个被测函数在台账里占一行, 记录断言数与失败数,
 *      最终输出 per-function PASS/FAIL 明细, 便于定位到具体函数.
 *   2. "双环境同源": 同一套测试源码编译两次
 *        - 定义 KAULA_FREESTANDING  → 被测的 memset/memcpy/strlen 等是库自身实现
 *        - 不定义                   → 托管模式, libc 同名强符号优先, 只测无冲突的
 *                                      fs_ 前缀 / math_ 前缀 / print 系列函数
 *      libc 同名函数相关的用例用 #ifdef KAULA_FREESTANDING 保护。
 *   3. "输出可断言": 用强符号覆写弱钩子 fs_output_putchar, 把 io 模块的真实
 *      字节输出捕获到缓冲区, 从而对 print/println 做逐字节比对。
 *
 * 注意: 本头文件属于测试宿主(非 freestanding 产物), 允许使用 <stdio.h>。
 */
#ifndef FS_TEST_H
#define FS_TEST_H

#include <stdio.h>
#include <stddef.h>
#include <stdint.h>
#include <stdarg.h>

/* 被测库的头文件声明（实现由 src/kaula_freestanding_runtime.c 或
 * build/lib/kaula_freestanding.lib 提供，见 scripts/run_freestanding_tests.py） */
#include "../../../freestanding/freestanding.h"

/* ------------------------------------------------------------------ 台账 */

typedef struct {
    const char *mod;    /* 模块名 */
    const char *fn;     /* 函数名 */
    int checks;         /* 断言数 */
    int fails;          /* 失败数 */
} FsFunc;

#define FS_LEDGER_MAX 512
extern FsFunc g_ledger[FS_LEDGER_MAX];
extern int g_ledger_n;
extern FsFunc *g_cur;

void fs_fail_at(const char *file, int line, const char *fmt, ...);

/* 开启一个被测函数的记账条目 */
void fs_open(const char *mod, const char *fn);

/* FS_FN("string", "strlen") 开启记账; 可在同一行给出说明 */
#define FS_FN(mod, fn) fs_open(mod, fn)

/* ---------------------------------------------------------------- 断言宏 */

#define CHECK(cond, label)                                                    \
    do {                                                                      \
        g_cur->checks++;                                                      \
        if (!(cond)) fs_fail_at(__FILE__, __LINE__, "%s", label);            \
    } while (0)

#define CHECK_INT(actual, expected, label)                                   \
    do {                                                                      \
        g_cur->checks++;                                                      \
        long long a_ = (long long)(actual);                                   \
        long long e_ = (long long)(expected);                                 \
        if (a_ != e_)                                                         \
            fs_fail_at(__FILE__, __LINE__, "%s (got %lld, want %lld)",        \
                       label, a_, e_);                                        \
    } while (0)

#define CHECK_UINT(actual, expected, label)                                  \
    do {                                                                      \
        g_cur->checks++;                                                      \
        unsigned long long a_ = (unsigned long long)(actual);                \
        unsigned long long e_ = (unsigned long long)(expected);              \
        if (a_ != e_)                                                         \
            fs_fail_at(__FILE__, __LINE__, "%s (got %llu, want %llu)",        \
                       label, a_, e_);                                        \
    } while (0)

#define CHECK_F64(actual, expected, eps, label)                              \
    do {                                                                      \
        g_cur->checks++;                                                      \
        double a_ = (double)(actual);                                         \
        double e_ = (double)(expected);                                       \
        double d_ = a_ - e_;                                                  \
        if (d_ < 0) d_ = -d_;                                                \
        if (!(d_ <= (eps)))                                                   \
            fs_fail_at(__FILE__, __LINE__, "%s (got %.17g, want %.17g)",      \
                       label, a_, e_);                                        \
    } while (0)

#define CHECK_STR(actual, expected, label)                                   \
    do {                                                                      \
        g_cur->checks++;                                                      \
        const char *a_ = (actual);                                            \
        const char *e_ = (expected);                                          \
        if (a_ == NULL || e_ == NULL || fs_tcmp(a_, e_) != 0)                 \
            fs_fail_at(__FILE__, __LINE__, "%s (got \"%s\", want \"%s\")",    \
                       label, a_ ? a_ : "(null)", e_ ? e_ : "(null)");        \
    } while (0)

#define CHECK_NULL(p, label)     CHECK((p) == NULL, label)
#define CHECK_NOTNULL(p, label)  CHECK((p) != NULL, label)

/* 逐字节比较 n 字节 */
#define CHECK_MEM(got, want, n, label)                                        \
    do {                                                                      \
        g_cur->checks++;                                                      \
        if (fs_memcmp_t((got), (want), (n)) != 0)                             \
            fs_fail_at(__FILE__, __LINE__, "%s (bytes differ)", label);       \
    } while (0)

/* ---------------------------------------------------------------- 工具 */

/* 台账内部用的字符串比较: FS 构建下即被测 strcmp, 托管构建下即 libc strcmp */
int fs_tcmp(const char *a, const char *b);
int fs_memcmp_t(const void *a, const void *b, size_t n);

/* 台架内部: 不依赖被测库/字符串函数的定长拷贝与比较（避免自证） */
void fs_copy_str(char *dst, size_t cap, const char *src);
int  fs_same_str(const char *a, const char *b);

/* 特殊浮点值: 由 IEEE-754 位模式构造, 不依赖 <math.h> 的 NAN/INFINITY 宏 */
double fs_nan(void);
double fs_pinf(void);
double fs_ninf(void);

/* ------------------------------------------------------------ 输出捕获 */

#define FS_CAP_MAX (64 * 1024)
extern char g_cap[FS_CAP_MAX];
extern size_t g_cap_len;

void cap_reset(void);
const char *cap_str(void);      /* NUL 结尾的已捕获内容 */
size_t cap_len(void);
int cap_eq(const char *want, const char *label);

/* ------------------------------------------------------- 套件声明 */

void suite_base(void);
void suite_memory(void);
void suite_string(void);
void suite_math(void);
void suite_io(void);

#endif /* FS_TEST_H */
