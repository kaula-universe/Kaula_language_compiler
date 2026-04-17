#include "io.h"
#include "../format/format.h"
#include <stdlib.h>
#include <string.h>
#include <stdarg.h>

#if STD_PLATFORM_WINDOWS
    #include <direct.h>
    #include <io.h>
    #include <windows.h>
    #define ACCESS _access
    #define STAT _stat64
    #define MKDIR(path) _mkdir(path)
    #define RMDIR _rmdir
    #define GETCWD _getcwd
    #define CHDIR _chdir
#else
    #include <unistd.h>
    #include <sys/stat.h>
    #include <sys/types.h>
    #include <dirent.h>
    #define ACCESS access
    #define STAT stat
    #define MKDIR(path) mkdir(path, 0755)
    #define RMDIR rmdir
    #define GETCWD getcwd
    #define CHDIR chdir
#endif

// ==================== 标准输入输出实现 ====================

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
    printf("%lld", (long long)value);
}

void print_float(f64 value) {
    printf("%f", value);
}

void print_bool(bool value) {
    printf("%s", value ? "true" : "false");
}

// ==================== 跨平台路径操作实现 ====================

char* path_join(const char* path1, const char* path2) {
    if (!path1 || !path2) {
        return NULL;
    }
    
    size_t len1 = strlen(path1);
    size_t len2 = strlen(path2);
    
    // 检查 path1 是否已有分隔符
    bool has_separator = false;
    if (len1 > 0) {
        char last_char = path1[len1 - 1];
        has_separator = (last_char == '/' || last_char == '\\');
    }
    
    size_t total_len = len1 + len2 + 1;
    if (!has_separator) {
        total_len += 2; // 需要添加分隔符
    }
    
    char* result = (char*)malloc(total_len);
    if (!result) {
        return NULL;
    }
    
    strcpy(result, path1);
    
    if (!has_separator) {
        strcat(result, PATH_SEPARATOR_STR);
    }
    
    // 跳过 path2 开头的分隔符
    const char* path2_start = path2;
    while (*path2_start == '/' || *path2_start == '\\') {
        path2_start++;
    }
    
    strcat(result, path2_start);
    
    return result;
}

char* path_join_multiple(const char* path1, const char* path2, const char* path3) {
    if (!path1 || !path2) {
        return NULL;
    }
    
    char* temp = path_join(path1, path2);
    if (!temp) {
        return NULL;
    }
    
    if (!path3) {
        return temp;
    }
    
    char* result = path_join(temp, path3);
    free(temp);
    
    return result;
}

char* path_basename(const char* path) {
    if (!path) {
        return NULL;
    }
    
    const char* last_sep = NULL;
    const char* p = path;
    
    while (*p) {
        if (*p == '/' || *p == '\\') {
            last_sep = p;
        }
        p++;
    }
    
    if (last_sep) {
        return strdup(last_sep + 1);
    } else {
        return strdup(path);
    }
}

char* path_dirname(const char* path) {
    if (!path) {
        return NULL;
    }
    
    const char* last_sep = NULL;
    const char* p = path;
    
    while (*p) {
        if (*p == '/' || *p == '\\') {
            last_sep = p;
        }
        p++;
    }
    
    if (last_sep) {
        size_t len = last_sep - path;
        char* result = (char*)malloc(len + 1);
        if (!result) {
            return NULL;
        }
        strncpy(result, path, len);
        result[len] = '\0';
        return result;
    } else {
        return strdup(".");
    }
}

bool path_is_absolute(const char* path) {
    if (!path) {
        return false;
    }
    
#if STD_PLATFORM_WINDOWS
    // Windows: 检查盘符 (C:\) 或 UNC 路径 (\\server)
    if ((path[0] >= 'A' && path[0] <= 'Z') || (path[0] >= 'a' && path[0] <= 'z')) {
        return path[1] == ':';
    }
    return path[0] == '\\' && path[1] == '\\';
#else
    // Unix: 检查是否以 / 开头
    return path[0] == '/';
#endif
}

char* path_normalize(const char* path) {
    if (!path) {
        return NULL;
    }
    
    // 简化实现：统一使用当前平台的分隔符
    char* result = strdup(path);
    if (!result) {
        return NULL;
    }
    
    char* p = result;
    while (*p) {
        if (*p == '/') {
            *p = PATH_SEPARATOR;
        }
        p++;
    }
    
    return result;
}

char* path_to_unix(const char* path) {
    if (!path) {
        return NULL;
    }
    
    char* result = strdup(path);
    if (!result) {
        return NULL;
    }
    
    char* p = result;
    while (*p) {
        if (*p == '\\') {
            *p = '/';
        }
        p++;
    }
    
    return result;
}

char* path_to_windows(const char* path) {
    if (!path) {
        return NULL;
    }
    
    char* result = strdup(path);
    if (!result) {
        return NULL;
    }
    
    char* p = result;
    while (*p) {
        if (*p == '/') {
            *p = '\\';
        }
        p++;
    }
    
    return result;
}

// ==================== 文件操作实现 ====================

File file_open(const char* path, const char* mode) {
    if (!path || !mode) {
        return NULL;
    }
    
#if STD_PLATFORM_WINDOWS && defined(_MSC_VER)
    FILE* file;
    if (fopen_s(&file, path, mode) != 0) {
        return NULL;
    }
    return file;
#else
    return fopen(path, mode);
#endif
}

void file_close(File file) {
    if (file) {
        fclose(file);
    }
}

size_t file_read(File file, void* buffer, size_t size) {
    if (!file || !buffer) {
        return 0;
    }
    return fread(buffer, 1, size, file);
}

size_t file_write(File file, const void* buffer, size_t size) {
    if (!file || !buffer) {
        return 0;
    }
    return fwrite(buffer, 1, size, file);
}

size_t file_read_line(File file, char* buffer, size_t size) {
    if (!file || !buffer || size == 0) {
        return 0;
    }
    
    if (!fgets(buffer, (int)size, file)) {
        return 0;
    }
    
    return strlen(buffer);
}

int file_seek(File file, long offset, int whence) {
    if (!file) {
        return -1;
    }
    return fseek(file, offset, whence);
}

long file_tell(File file) {
    if (!file) {
        return -1;
    }
    return ftell(file);
}

void file_flush(File file) {
    if (file) {
        fflush(file);
    }
}

bool file_eof(File file) {
    if (!file) {
        return false;
    }
    return feof(file) != 0;
}

bool file_error(File file) {
    if (!file) {
        return false;
    }
    return ferror(file) != 0;
}

int file_printf(File file, const char* format, ...) {
    if (!file || !format) {
        return -1;
    }
    
    va_list args;
    va_start(args, format);
    int result = vfprintf(file, format, args);
    va_end(args);
    
    return result;
}

int file_scanf(File file, const char* format, ...) {
    if (!file || !format) {
        return -1;
    }
    
    va_list args;
    va_start(args, format);
    int result = vfscanf(file, format, args);
    va_end(args);
    
    return result;
}

// ==================== 文件状态函数实现 ====================

bool file_exists(const char* path) {
    if (!path) {
        return false;
    }
    return ACCESS(path, 0) == 0;
}

size_t file_size(const char* path) {
    if (!path) {
        return 0;
    }
    
    struct STAT st;
    if (STAT(path, &st) != 0) {
        return 0;
    }
    
    return (size_t)st.st_size;
}

bool file_is_regular(const char* path) {
    if (!path) {
        return false;
    }
    
    struct STAT st;
    if (STAT(path, &st) != 0) {
        return false;
    }
    
    return S_ISREG(st.st_mode);
}

bool file_is_directory(const char* path) {
    if (!path) {
        return false;
    }
    
    struct STAT st;
    if (STAT(path, &st) != 0) {
        return false;
    }
    
    return S_ISDIR(st.st_mode);
}

// ==================== 目录操作实现 ====================

bool directory_create(const char* path) {
    if (!path) {
        return false;
    }
    return MKDIR(path) == 0;
}

bool directory_remove(const char* path) {
    if (!path) {
        return false;
    }
    return RMDIR(path) == 0;
}

bool directory_exists(const char* path) {
    if (!path) {
        return false;
    }
    return file_is_directory(path);
}

// ==================== 错误处理实现 ====================

static int g_io_error = 0;

int io_get_error() {
    return g_io_error;
}

const char* io_get_error_message() {
    switch (g_io_error) {
        case 0: return "No error";
        case 1: return "File not found";
        case 2: return "Permission denied";
        case 3: return "Out of memory";
        case 4: return "Invalid argument";
        default: return "Unknown error";
    }
}

void io_clear_error() {
    g_io_error = 0;
}
