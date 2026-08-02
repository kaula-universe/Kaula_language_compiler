#ifndef KAULA_FREESTANDING_BASE_FS_COMMON_H
#define KAULA_FREESTANDING_BASE_FS_COMMON_H

// freestanding/base/fs_common.h — freestanding 库内部公共定义
// 不对外暴露，各模块 .c 实现共用

// FS_WEAK — 弱符号标记
// 托管模式（OS）下与 libc / kaula_runtime / std 的同名强符号共存时，
// 强符号优先，弱符号被丢弃，避免重复定义链接错误；
// 裸机模式（-nostdlib）下没有其他强符号，弱符号正常被链接使用。
#if defined(__GNUC__) || defined(__clang__)
    #define FS_WEAK __attribute__((weak))
#else
    #define FS_WEAK
#endif

// FS_LIKELY / FS_UNLIKELY — 分支预测提示
#if defined(__GNUC__) || defined(__clang__)
    #define FS_LIKELY(x)   __builtin_expect(!!(x), 1)
    #define FS_UNLIKELY(x) __builtin_expect(!!(x), 0)
#else
    #define FS_LIKELY(x)   (x)
    #define FS_UNLIKELY(x) (x)
#endif

#endif /* KAULA_FREESTANDING_BASE_FS_COMMON_H */
