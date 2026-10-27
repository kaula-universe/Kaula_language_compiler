// freestanding/include/errno.h — 裸机模式的 <errno.h> 替身
//
// errno 在裸机是"当前线程最后错误码"，KMM 分配器与 std.error 都读它。
// 这里用 __thread 局部变量给出真实存储（各架构均支持 TLS 变量；
// freestanding/math 与 KMM 也已经在用同一种做法），常量取值与 POSIX 一致。
#ifndef KAULA_FS_COMPAT_ERRNO_H
#define KAULA_FS_COMPAT_ERRNO_H

#ifdef __cplusplus
extern "C" {
#endif

#if defined(__GNUC__) || defined(__clang__)
extern __thread int errno;
#define errno errno
#elif defined(_MSC_VER)
extern __declspec(thread) int errno;
#define errno errno
#else
extern int errno;
#define errno errno
#endif

#define EPERM            1
#define ENOENT           2
#define ESRCH            3
#define EINTR            4
#define EIO              5
#define ENXIO            6
#define E2BIG            7
#define ENOEXEC          8
#define EBADF            9
#define ECHILD          10
#define EAGAIN          11
#define ENOMEM          12
#define EACCES          13
#define EFAULT          14
#define EBUSY           16
#define EEXIST          17
#define EXDEV           18
#define ENODEV          19
#define ENOTDIR         20
#define EISDIR          21
#define EINVAL          22
#define ENFILE          23
#define EMFILE          24
#define ENOTTY          25
#define EFBIG           27
#define ENOSPC          28
#define ESPIPE          29
#define EROFS           30
#define EMLINK          31
#define EPIPE           32
#define EDOM            33
#define ERANGE          34
#define EDEADLK         35
#define ENAMETOOLONG    36
#define ENOSYS          38
#define ENOTEMPTY       39
#define ELOOP           40
#define ENOMSG          42
#define EIDRM           43
#define EOVERFLOW       75
#define EILSEQ          84
#define ENOTSOCK        88
#define EDESTADDRREQ    89
#define EPROTOTYPE      91
#define ENOPROTOOPT     92
#define EAFNOSUPPORT    97
#define EADDRINUSE     100
#define ENETDOWN       100
#define ETIMEDOUT      110
#define ECONNREFUSED   111

#ifdef __cplusplus
}
#endif

#endif /* KAULA_FS_COMPAT_ERRNO_H */
