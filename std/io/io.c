#include "io.h"
#include <stdio.h>
#include <stdarg.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>

// 为Windows平台提供getline函数的替代实现
#ifdef _WIN32
#include <io.h>
#include <fcntl.h>

ssize_t getline(char** lineptr, size_t* n, FILE* stream) {
    if (*lineptr == NULL || *n == 0) {
        *n = 128;
        *lineptr = (char*)malloc(*n);
        if (*lineptr == NULL) {
            return -1;
        }
    }

    size_t pos = 0;
    int c;
    while ((c = fgetc(stream)) != EOF) {
        if (pos + 1 >= *n) {
            size_t new_size = *n * 2;
            char* new_line = (char*)realloc(*lineptr, new_size);
            if (new_line == NULL) {
                return -1;
            }
            *lineptr = new_line;
            *n = new_size;
        }
        (*lineptr)[pos++] = (char)c;
        if (c == '\n') {
            break;
        }
    }

    if (pos == 0 && c == EOF) {
        return -1;
    }

    (*lineptr)[pos] = '\0';
    return (ssize_t)pos;
}

// 为Windows平台提供S_ISREG和S_ISDIR宏
#define S_ISREG(mode) (((mode) & S_IFMT) == S_IFREG)
#define S_ISDIR(mode) (((mode) & S_IFMT) == S_IFDIR)
#endif

#ifdef _WIN32
#include <windows.h>
#endif

// 标准输入输出函数
void print(const char* format, ...) {
    va_list args;
    va_start(args, format);
    vprintf(format, args);
    va_end(args);
}

void println(const char* format, ...) {
    va_list args;
    va_start(args, format);
    vprintf(format, args);
    va_end(args);
    printf("\n");
}

void print_char(char c) {
    putchar(c);
}

void print_int(i64 value) {
    printf("%lld", value);
}

void print_float(f64 value) {
    printf("%lf", value);
}

void print_bool(bool value) {
    printf(value ? "true" : "false");
}

// 标准输入函数
char read_char() {
    return getchar();
}

i64 read_int() {
    i64 value;
    scanf("%lld", &value);
    return value;
}

f64 read_float() {
    f64 value;
    scanf("%lf", &value);
    return value;
}

bool read_bool() {
    char buffer[10];
    scanf("%s", buffer);
    return strcmp(buffer, "true") == 0 || strcmp(buffer, "1") == 0;
}

char* read_line() {
    char* line = NULL;
    size_t len = 0;
    getline(&line, &len, stdin);
    // 移除换行符
    size_t line_len = strlen(line);
    if (line_len > 0 && line[line_len - 1] == '\n') {
        line[line_len - 1] = '\0';
    }
    return line;
}

char* read_string(size_t max_length) {
    char* buffer = (char*)malloc(max_length + 1);
    if (buffer) {
        scanf("%s", buffer);
    }
    return buffer;
}

// 文件操作函数
File file_open(const char* path, const char* mode) {
    return fopen(path, mode);
}

void file_close(File file) {
    if (file) {
        fclose(file);
    }
}

size_t file_read(File file, void* buffer, size_t size) {
    if (!file) return 0;
    return fread(buffer, 1, size, file);
}

size_t file_write(File file, const void* buffer, size_t size) {
    if (!file) return 0;
    return fwrite(buffer, 1, size, file);
}

size_t file_read_line(File file, char* buffer, size_t size) {
    if (!file || !buffer) return 0;
    if (fgets(buffer, size, file)) {
        // 移除换行符
        size_t len = strlen(buffer);
        if (len > 0 && buffer[len - 1] == '\n') {
            buffer[len - 1] = '\0';
        }
        return len;
    }
    return 0;
}

int file_seek(File file, long offset, int whence) {
    if (!file) return -1;
    return fseek(file, offset, whence);
}

long file_tell(File file) {
    if (!file) return -1;
    return ftell(file);
}

void file_flush(File file) {
    if (file) {
        fflush(file);
    }
}

bool file_eof(File file) {
    if (!file) return true;
    return feof(file) != 0;
}

bool file_error(File file) {
    if (!file) return true;
    return ferror(file) != 0;
}

// 格式化文件输入输出函数
int file_printf(File file, const char* format, ...) {
    if (!file) return 0;
    va_list args;
    va_start(args, format);
    int result = vfprintf(file, format, args);
    va_end(args);
    return result;
}

int file_scanf(File file, const char* format, ...) {
    if (!file) return 0;
    va_list args;
    va_start(args, format);
    int result = vfscanf(file, format, args);
    va_end(args);
    return result;
}

// 文件状态函数
bool file_exists(const char* path) {
    struct stat st;
    return stat(path, &st) == 0;
}

size_t file_size(const char* path) {
    struct stat st;
    if (stat(path, &st) == 0) {
        return (size_t)st.st_size;
    }
    return 0;
}

bool file_is_regular(const char* path) {
    struct stat st;
    if (stat(path, &st) == 0) {
        return S_ISREG(st.st_mode);
    }
    return false;
}

bool file_is_directory(const char* path) {
    struct stat st;
    if (stat(path, &st) == 0) {
        return S_ISDIR(st.st_mode);
    }
    return false;
}

// 目录操作函数
bool directory_create(const char* path) {
    #ifdef _WIN32
    return CreateDirectory(path, NULL) != 0;
    #else
    return mkdir(path, 0755) == 0;
    #endif
}

bool directory_remove(const char* path) {
    #ifdef _WIN32
    return RemoveDirectory(path) != 0;
    #else
    return rmdir(path) == 0;
    #endif
}

bool directory_exists(const char* path) {
    return file_is_directory(path);
}

// 路径操作函数
char* path_join(const char* path1, const char* path2) {
    size_t len1 = strlen(path1);
    size_t len2 = strlen(path2);
    char* result = (char*)malloc(len1 + len2 + 2); // +2 for '/' and null terminator
    if (result) {
        strcpy(result, path1);
        if (len1 > 0 && path1[len1 - 1] != '\\' && path1[len1 - 1] != '/') {
            strcat(result, "/");
        }
        strcat(result, path2);
    }
    return result;
}

char* path_basename(const char* path) {
    const char* last_slash = strrchr(path, '/');
    #ifdef _WIN32
    const char* last_backslash = strrchr(path, '\\');
    if (last_backslash && (!last_slash || last_backslash > last_slash)) {
        last_slash = last_backslash;
    }
    #endif
    if (last_slash) {
        return (char*)(last_slash + 1);
    }
    return (char*)path;
}

char* path_dirname(const char* path) {
    const char* last_slash = strrchr(path, '/');
    #ifdef _WIN32
    const char* last_backslash = strrchr(path, '\\');
    if (last_backslash && (!last_slash || last_backslash > last_slash)) {
        last_slash = last_backslash;
    }
    #endif
    if (last_slash) {
        size_t len = last_slash - path;
        char* result = (char*)malloc(len + 1);
        if (result) {
            strncpy(result, path, len);
            result[len] = '\0';
        }
        return result;
    }
    return (char*)"";
}

bool path_is_absolute(const char* path) {
    #ifdef _WIN32
    return (path[0] && (path[1] == ':' && (path[2] == '\\' || path[2] == '/'))) || (path[0] == '\\' && path[1] == '\\');
    #else
    return path[0] == '/';
    #endif
}

// 输入输出错误处理
static int io_error = 0;
static char io_error_message[256] = "";

int io_get_error() {
    return io_error;
}

const char* io_get_error_message() {
    return io_error_message;
}

void io_clear_error() {
    io_error = 0;
    io_error_message[0] = '\0';
}
