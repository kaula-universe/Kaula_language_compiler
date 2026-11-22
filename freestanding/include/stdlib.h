// freestanding/include/stdlib.h — 裸机模式的 <stdlib.h> 替身（仅原型）
//
// 语义边界：malloc/free 在裸机由 KMM 分配器（kmm_v4_alloc / fs_alloc）提供，
// 标准名不可用（且与静态池语义冲突），因此这里**不声明** malloc/calloc/realloc/
// free —— 需要内存的代码请用 freestanding.memory.fs_alloc 或 std.memory。
// 下面只声明纯计算/转换类函数与进程退出类函数。
#ifndef KAULA_FS_COMPAT_STDLIB_H
#define KAULA_FS_COMPAT_STDLIB_H

#include <stddef.h>

#ifdef __cplusplus
extern "C" {
#endif

// 进程控制：裸机无进程，exit/abort 停机（Kaula 的 assert 走 kaula_assert_failed）
void exit(int status);
void abort(void);
void _Exit(int status);
int atexit(void (*func)(void));

// 环境与系统：裸机没有
char* getenv(const char* name);
int system(const char* command);

// 排序/查找（纯计算，libc 实现缺失；freestanding.math 有等价工具）
void qsort(void* base, size_t nmemb, size_t size, int (*compar)(const void*, const void*));
void* bsearch(const void* key, const void* base, size_t nmemb, size_t size,
              int (*compar)(const void*, const void*));

// 数值转换（纯计算）
int atoi(const char* s);
long atol(const char* s);
long long atoll(const char* s);
double atof(const char* s);
long strtol(const char* s, char** endptr, int base);
unsigned long strtoul(const char* s, char** endptr, int base);
long long strtoll(const char* s, char** endptr, int base);
unsigned long long strtoull(const char* s, char** endptr, int base);
double strtod(const char* s, char** endptr);
float strtof(const char* s, char** endptr);
long double strtold(const char* s, char** endptr);

// 伪随机：freestanding.math 已提供 math_srand/math_rand（弱符号），此处不重复声明
int abs(int j);
long labs(long j);
long long llabs(long long j);

#ifndef __DEFINED_div_t
#define __DEFINED_div_t
typedef struct { int quot; int rem; } div_t;
typedef struct { long quot; long rem; } ldiv_t;
typedef struct { long long quot; long long rem; } lldiv_t;
#endif
div_t div(int numer, int denom);
ldiv_t ldiv(long numer, long denom);
lldiv_t lldiv(long long numer, long long denom);

#define EXIT_SUCCESS 0
#define EXIT_FAILURE 1
#define RAND_MAX 0x7fffffff
#define MB_CUR_MAX 1

#ifdef __cplusplus
}
#endif

#endif /* KAULA_FS_COMPAT_STDLIB_H */
