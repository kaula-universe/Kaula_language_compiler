// freestanding/include/fcntl.h — 裸机模式的 <fcntl.h> 替身（仅原型）
//
// 裸机没有 POSIX 文件描述符。std.web 的头会 include 它。
// 这里只给 open 相关原型与 O_* 常量（数值与 POSIX 一致），保持未定义。
#ifndef KAULA_FS_COMPAT_FCNTL_H
#define KAULA_FS_COMPAT_FCNTL_H

#include <sys/types.h>

#ifdef __cplusplus
extern "C" {
#endif

#define O_RDONLY    0
#define O_WRONLY    1
#define O_RDWR      2
#define O_CREAT   0100
#define O_EXCL    0200
#define O_TRUNC  01000
#define O_APPEND  02000
#define O_NONBLOCK 04000

#define F_DUPFD     0
#define F_GETFD     1
#define F_SETFD     2
#define F_GETFL     3
#define F_SETFL     4
#define FD_CLOEXEC  1

int open(const char* pathname, int flags, ...);
int creat(const char* pathname, mode_t mode);
int fcntl(int fd, int cmd, ...);

#ifdef __cplusplus
}
#endif

#endif /* KAULA_FS_COMPAT_FCNTL_H */
