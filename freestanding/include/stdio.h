// freestanding/include/stdio.h — 裸机模式的 <stdio.h> 替身
//
// 用途：freestanding 模式下 std.* 的头文件仍会被包含（如 std/io/io.h 需要
// FILE 与 stdio 原型），而交叉到 *-none-elf 后宿主 SDK 完全不在搜索路径里，
// 直接编不过是 "'stdio.h' file not found"（语料库 53 个程序里 28 个撞上）。
//
// 这里只提供"让头文件能被解析"的最小声明集：
//   - FILE 是不透明类型（裸机没有文件 I/O，std/io 的 file_* 全部留在未定义状态）
//   - 标准流与格式化函数声明为外部符号：freestanding.io 已实现 print/println/
//     print_char/print_int/print_float/print_bool，链接期能解析的会自动对上，
//     解析不了的（fopen/fread/fwrite…）保持未定义 —— 那是"裸机没有文件 I/O"
//     的如实反映，不该由替身头伪造出可用实现。
//
// 该目录只在 --freestanding 下加入 -I（见 kaulac），托管模式永远用系统 stdio.h。
#ifndef KAULA_FS_COMPAT_STDIO_H
#define KAULA_FS_COMPAT_STDIO_H

#include <stdarg.h>
#include <stddef.h>

#ifdef __cplusplus
extern "C" {
#endif

// 不透明 FILE：裸机无文件 I/O
typedef struct kaula_fs_file kaula_fs_FILE;
#ifndef __cplusplus
typedef kaula_fs_FILE FILE;
#endif

// 标准流：裸机不存在实体，声明仅用于让引用能编译（链接期会报未定义）
extern FILE* stdin;
extern FILE* stdout;
extern FILE* stderr;

// EOF / SEEK_*：与 POSIX 数值一致
#define EOF (-1)
#define SEEK_SET 0
#define SEEK_CUR 1
#define SEEK_END 2
#define BUFSIZ 1024
#define FILENAME_MAX 260

// freestanding.io 实际提供的输出（弱符号）
extern int putchar(int c);
extern int puts(const char* s);
extern int fputs(const char* s, FILE* f);
extern int fputc(int c, FILE* f);
extern int printf(const char* format, ...);
extern int fprintf(FILE* f, const char* format, ...);
extern int sprintf(char* buf, const char* format, ...);
extern int snprintf(char* buf, size_t n, const char* format, ...);
extern int vsnprintf(char* buf, size_t n, const char* format, va_list ap);
extern int fflush(FILE* f);

// 以下在裸机没有实现，仅声明以保证引用能编译
extern FILE* fopen(const char* path, const char* mode);
extern int fclose(FILE* f);
extern size_t fread(void* ptr, size_t size, size_t n, FILE* f);
extern size_t fwrite(const void* ptr, size_t size, size_t n, FILE* f);
extern int fseek(FILE* f, long offset, int whence);
extern long ftell(FILE* f);
extern int feof(FILE* f);
extern int ferror(FILE* f);
extern int remove(const char* path);
extern int rename(const char* from, const char* to);
extern FILE* tmpfile(void);
extern char* tmpnam(char* s);

#ifdef __cplusplus
}
#endif

#endif /* KAULA_FS_COMPAT_STDIO_H */
