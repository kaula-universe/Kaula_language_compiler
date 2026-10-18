#include "io.h"
#include "../memory/memory.h"
#include "../format/format.h"
#include <stdlib.h>
#include <string.h>
#include <stdarg.h>

#if STD_PLATFORM_WINDOWS
    #include <direct.h>
    #include <io.h>
    #include <windows.h>
    #include <sys/stat.h>
    #define ACCESS _access
    #define STAT _stat
    #define MKDIR(path) _mkdir(path)
    #define RMDIR(path) _rmdir(path)
    #define GETCWD _getcwd
    #define CHDIR _chdir
    #ifndef S_ISREG
        #define S_ISREG(mode) (((mode) & S_IFMT) == S_IFREG)
    #endif
    #ifndef S_ISDIR
        #define S_ISDIR(mode) (((mode) & S_IFMT) == S_IFDIR)
    #endif
#else
    #include <unistd.h>
    #include <sys/stat.h>
    #include <sys/types.h>
    #include <dirent.h>
    #define ACCESS access
    #define STAT stat
    #define MKDIR(path) mkdir(path, 0755)
    #define RMDIR(path) rmdir(path)
    #define GETCWD getcwd
    #define CHDIR chdir
#endif

// ==================== 鏍囧噯杈撳叆杈撳嚭瀹炵幇 ====================

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

// println_multi 鏀寔澶氬弬鏁拌嚜鍔ㄧ被鍨嬫帹瀵兼墦鍗?
// type鍙傛暟: 0=int, 1=double, 2=string
void println_multi(int arg_count, ...) {
    va_list args;
    va_start(args, arg_count);
    
    for (int i = 0; i < arg_count; i++) {
        int type = va_arg(args, int);
        switch (type) {
            case 0: // int
                printf("%lld", (long long)va_arg(args, long long));
                break;
            case 1: { // double
                double val = va_arg(args, double);
                // 濡傛灉鏄暣鏁板€硷紝涓嶆樉绀哄皬鏁伴儴鍒?
                if (val == (long long)val && val >= -1e15 && val <= 1e15) {
                    printf("%lld", (long long)val);
                } else {
                    printf("%g", val);
                }
                break;
            }
            case 2: // string
                printf("%s", va_arg(args, char*));
                break;
        }
    }
    
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

// ==================== 鏍囧噯杈撳叆鍑芥暟瀹炵幇 ====================

char read_char() {
    int c = getchar();
    return (char)c;
}

i64 read_int() {
    i64 value;
    if (scanf("%lld", &value) != 1) {
        return 0;
    }
    return value;
}

f64 read_float() {
    f64 value;
    if (scanf("%lf", &value) != 1) {
        return 0.0;
    }
    return value;
}

bool read_bool() {
    char buffer[16];
    if (fgets(buffer, sizeof(buffer), stdin) == NULL) {
        return false;
    }
    // Remove trailing newline
    size_t len = strlen(buffer);
    if (len > 0 && buffer[len - 1] == '\n') {
        buffer[len - 1] = '\0';
    }
    return (strcmp(buffer, "true") == 0 || strcmp(buffer, "1") == 0 || strcmp(buffer, "yes") == 0);
}

char* read_line() {
    char* buffer = NULL;
    size_t capacity = 256;
    size_t len = 0;
    
    buffer = (char*)kmm_v4_malloc(capacity);
    if (!buffer) {
        return NULL;
    }
    
    int c;
    while ((c = getchar()) != EOF && c != '\n') {
        if (len + 1 >= capacity) {
            capacity *= 2;
            char* new_buffer = (char*)kmm_v4_realloc(buffer, capacity);
            if (!new_buffer) {
                return NULL;
            }
            buffer = new_buffer;
        }
        buffer[len++] = (char)c;
    }
    
    buffer[len] = '\0';
    return buffer;
}

char* read_string(size_t max_length) {
    if (max_length == 0) {
        return NULL;
    }
    
    char* buffer = (char*)kmm_v4_malloc(max_length + 1);
    if (!buffer) {
        return NULL;
    }
    
    int c;
    size_t i = 0;
    while (i < max_length && (c = getchar()) != EOF && c != '\n' && c != '\r') {
        buffer[i++] = (char)c;
    }
    buffer[i] = '\0';
    
    return buffer;
}

// ==================== 鏂囦欢鎿嶄綔瀹炵幇 ====================

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

// ==================== 鏂囦欢鐘舵€佸嚱鏁板疄鐜?====================

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

// ==================== 鐩綍鎿嶄綔瀹炵幇 ====================

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

// ==================== 閿欒澶勭悊瀹炵幇 ====================

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
