// freestanding/include/assert.h — 裸机模式的 <assert.h> 替身
//
// 标准 assert 展开成 fprintf(stderr,...) + abort()，两者在裸机都不存在。
// 这里改走 kaula_runtime.h 的 kaula_report_fatal：托管模式打印 + abort，
// 裸机调弱钩子 kaula_fatal 后 __builtin_trap 停机 —— 与 Kaula 内建
// assert() 走的是同一条路径（见 codegen 的 generateAssertCall）。
#ifndef KAULA_FS_COMPAT_ASSERT_H
#define KAULA_FS_COMPAT_ASSERT_H

#ifdef __cplusplus
extern "C" {
#endif

void __assert_fail(const char* assertion, const char* file, unsigned line, const char* function);

#ifdef __cplusplus
}
#endif

#ifndef NDEBUG
#ifdef __cplusplus
#define assert(expr) ((expr) ? (void)0 : __assert_fail(#expr, __FILE__, __LINE__, __func__))
#else
#define assert(expr) ((expr) ? (void)0 : __assert_fail(#expr, __FILE__, __LINE__, 0))
#endif
#else
#define assert(expr) ((void)0)
#endif

#define static_assert _Static_assert

#endif /* KAULA_FS_COMPAT_ASSERT_H */
