// freestanding/include/sys/types.h — 裸机模式的 <sys/types.h> 替身
//
// 用途：std/base/types.h 在非 Windows 目标上会 include <sys/types.h> 取 ssize_t。
// 交叉到 *-none-elf 后没有宿主 SDK，该头不存在，26/53 个语料程序直接编不过
// （"'sys/types.h' file not found"）。
//
// 只提供 POSIX 里"纯类型"的那一部分（宽度与平台一致），不提供任何函数原型：
// 裸机没有系统调用，sbrk/mmap/read/write 之类不该在这里出现。
//
// 该目录只在 --freestanding 下加入 -I（见 kaulac），托管模式永远用系统头。
#ifndef KAULA_FS_COMPAT_SYS_TYPES_H
#define KAULA_FS_COMPAT_SYS_TYPES_H

#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

// ssize_t：POSIX 规定为与 size_t 同宽的有符号整型
#if !defined(__DEFINED_ssize_t) && !defined(_SSIZE_T_DEFINED)
#define __DEFINED_ssize_t
#define _SSIZE_T_DEFINED
#define _SSIZE_T
#define _SSIZE_T_DECLARED
typedef intptr_t ssize_t;
#endif

#if !defined(__DEFINED_off_t) && !defined(_OFF_T_DEFINED)
#define __DEFINED_off_t
#define _OFF_T_DEFINED
typedef int64_t off_t;
#endif

#if !defined(__DEFINED_time_t) && !defined(_TIME_T_DEFINED)
#define __DEFINED_time_t
#define _TIME_T_DEFINED
typedef int64_t time_t;
#endif

#if !defined(__DEFINED_pid_t) && !defined(_PID_T_DEFINED)
#define __DEFINED_pid_t
#define _PID_T_DEFINED
typedef int32_t pid_t;
#endif

#if !defined(__DEFINED_mode_t) && !defined(_MODE_T_DEFINED)
#define __DEFINED_mode_t
#define _MODE_T_DEFINED
typedef uint32_t mode_t;
#endif

#if !defined(__DEFINED_dev_t) && !defined(_DEV_T_DEFINED)
#define __DEFINED_dev_t
#define _DEV_T_DEFINED
typedef uint64_t dev_t;
#endif

#if !defined(__DEFINED_ino_t) && !defined(_INO_T_DEFINED)
#define __DEFINED_ino_t
#define _INO_T_DEFINED
typedef uint64_t ino_t;
#endif

#if !defined(__DEFINED_uid_t) && !defined(_UID_T_DEFINED)
#define __DEFINED_uid_t
#define _UID_T_DEFINED
typedef uint32_t uid_t;
#endif

#if !defined(__DEFINED_gid_t) && !defined(_GID_T_DEFINED)
#define __DEFINED_gid_t
#define _GID_T_DEFINED
typedef uint32_t gid_t;
#endif

#if !defined(__DEFINED_blkcnt_t) && !defined(_BLKCNT_T_DEFINED)
#define __DEFINED_blkcnt_t
#define _BLKCNT_T_DEFINED
typedef int64_t blkcnt_t;
#endif

#if !defined(__DEFINED_clock_t) && !defined(_CLOCK_T_DEFINED)
#define __DEFINED_clock_t
#define _CLOCK_T_DEFINED
typedef int64_t clock_t;
#endif

#ifdef __cplusplus
}
#endif

#endif /* KAULA_FS_COMPAT_SYS_TYPES_H */
