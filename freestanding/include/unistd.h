// freestanding/include/unistd.h — 裸机模式的 <unistd.h> 替身（仅原型）
//
// 裸机没有系统调用。std.web / std.time 的头会 include 它。
// 这里给类型（ssize_t 来自 sys/types.h shim）与原型，函数一律保持未定义 ——
// 如实反映"这些能力在裸机需要用户自己接"。
#ifndef KAULA_FS_COMPAT_UNISTD_H
#define KAULA_FS_COMPAT_UNISTD_H

#include <stddef.h>
#include <sys/types.h>

#ifdef __cplusplus
extern "C" {
#endif

// 读/写：串口与 UART 直写请用 freestanding.io 的弱钩子
ssize_t read(int fd, void* buf, size_t count);
ssize_t write(int fd, const void* buf, size_t count);

// 文件与目录：裸机一般无文件系统
int close(int fd);
off_t lseek(int fd, off_t offset, int whence);
int unlink(const char* path);
int rmdir(const char* path);
int access(const char* pathname, int mode);
char* getcwd(char* buf, size_t size);
int chdir(const char* path);
int mkdir(const char* path, mode_t mode);

// 进程：裸机只有当前内核
pid_t getpid(void);
pid_t getppid(void);
int execv(const char* path, char* const argv[]);
int execvp(const char* file, char* const argv[]);
void _exit(int status);
unsigned int sleep(unsigned int seconds);
int usleep(unsigned int usec);

// 终端
int isatty(int fd);
int ttyname_r(int fd, char* buf, size_t buflen);

#define STDIN_FILENO  0
#define STDOUT_FILENO 1
#define STDERR_FILENO 2

#define F_OK 0
#define X_OK 1
#define W_OK 2
#define R_OK 4

#ifdef __cplusplus
}
#endif

#endif /* KAULA_FS_COMPAT_UNISTD_H */
