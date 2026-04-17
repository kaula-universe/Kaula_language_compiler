#ifndef STD_IO_IO_H
#define STD_IO_IO_H

#include <stdio.h>
#include "../base/types.h"

// ==================== 跨平台路径分隔符 ====================
#if STD_PLATFORM_WINDOWS
    #define PATH_SEPARATOR '\\'
    #define PATH_SEPARATOR_STR "\\"
#else
    #define PATH_SEPARATOR '/'
    #define PATH_SEPARATOR_STR "/"
#endif

// 标准输入输出函数
extern void print(const char* format, ...);
extern void println(const char* format, ...);
extern void print_char(char c);
extern void print_int(i64 value);
extern void print_float(f64 value);
extern void print_bool(bool value);

// 标准输入函数
extern char read_char();
extern i64 read_int();
extern f64 read_float();
extern bool read_bool();
extern char* read_line();
extern char* read_string(size_t max_length);

// 文件操作函数
typedef FILE* File;

extern File file_open(const char* path, const char* mode);
extern void file_close(File file);
extern size_t file_read(File file, void* buffer, size_t size);
extern size_t file_write(File file, const void* buffer, size_t size);
extern size_t file_read_line(File file, char* buffer, size_t size);
extern int file_seek(File file, long offset, int whence);
extern long file_tell(File file);
extern void file_flush(File file);
extern bool file_eof(File file);
extern bool file_error(File file);

// 格式化文件输入输出函数
extern int file_printf(File file, const char* format, ...);
extern int file_scanf(File file, const char* format, ...);

// 文件状态函数
extern bool file_exists(const char* path);
extern size_t file_size(const char* path);
extern bool file_is_regular(const char* path);
extern bool file_is_directory(const char* path);

// 目录操作函数
extern bool directory_create(const char* path);
extern bool directory_remove(const char* path);
extern bool directory_exists(const char* path);

// 路径操作函数（跨平台）
extern char* path_join(const char* path1, const char* path2);
extern char* path_join_multiple(const char* path1, const char* path2, const char* path3);
extern char* path_basename(const char* path);
extern char* path_dirname(const char* path);
extern bool path_is_absolute(const char* path);
extern char* path_normalize(const char* path);
extern char* path_to_unix(const char* path);
extern char* path_to_windows(const char* path);

// 输入输出错误处理
extern int io_get_error();
extern const char* io_get_error_message();
extern void io_clear_error();

#endif // STD_IO_IO_H