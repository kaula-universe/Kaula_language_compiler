#ifndef STD_IO_IO_H
#define STD_IO_IO_H

#include <stdio.h>
#include "../base/types.h"
#include "../string/string.h"

// 标准输入输出函数
extern void print(const char* format, ...);
extern void println(const char* format, ...);
extern void println_multi(int arg_count, ...);
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

// 路径操作已由 std.path 模块提供（path.h）

// 输入输出错误处理
extern int io_get_error();
extern const char* io_get_error_message();
extern void io_clear_error();

// 异步 I/O
typedef void* AsyncIO;
typedef void (*AsyncIOCallback)(void* user_data, size_t bytes_transferred, int error_code);

extern AsyncIO aio_create();
extern void aio_destroy(AsyncIO aio);
extern void aio_read_file(const String path, AsyncIOCallback callback, void* user_data);
extern void aio_write_file(const String path, const void* data, size_t len, AsyncIOCallback callback, void* user_data);
extern void aio_process_events(AsyncIO aio);
extern bool_t aio_poll(AsyncIO aio, uint32_t timeout_ms);

// 内存流
typedef struct MemoryStream {
    u8* data;
    size_t size;
    size_t capacity;
    size_t position;
} MemoryStream;

extern MemoryStream* memstream_create(size_t initial_capacity);
extern void memstream_destroy(MemoryStream* ms);
extern size_t memstream_write(MemoryStream* ms, const void* data, size_t len);
extern size_t memstream_read(MemoryStream* ms, void* buffer, size_t len);
extern void memstream_seek(MemoryStream* ms, size_t position);
extern size_t memstream_tell(MemoryStream* ms);
extern size_t memstream_size(MemoryStream* ms);
extern void* memstream_data(MemoryStream* ms);
extern void memstream_reset(MemoryStream* ms);

// 缓冲读写器
typedef struct BufferedWriter {
    MemoryStream* stream;
    size_t buffer_size;
} BufferedWriter;

typedef struct BufferedReader {
    MemoryStream* stream;
    size_t buffer_size;
} BufferedReader;

extern BufferedWriter* buffered_writer_create(size_t buffer_size);
extern void buffered_writer_destroy(BufferedWriter* bw);
extern size_t buffered_writer_write(BufferedWriter* bw, const void* data, size_t len);
extern void buffered_writer_flush(BufferedWriter* bw);

extern BufferedReader* buffered_reader_create(size_t buffer_size);
extern void buffered_reader_destroy(BufferedReader* br);
extern void buffered_reader_set_source(BufferedReader* br, MemoryStream* source);
extern size_t buffered_reader_read(BufferedReader* br, void* buffer, size_t len);

#endif // STD_IO_IO_H