#include "format.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

// ==================== 跨平台 snprintf 实现 ====================
#if FORMAT_PLATFORM_WINDOWS
    // Windows: 使用 snprintf_s (MSVC) 或 snprintf (MinGW)
    #if defined(_MSC_VER)
        #define format_snprintf _snprintf_s
    #else
        #define format_snprintf snprintf
    #endif
#else
    // Unix/Linux/macOS: 使用标准 snprintf
    #define format_snprintf snprintf
#endif

// ==================== 基础格式化函数实现 ====================

int format_printf(char* buffer, size_t size, const char* format, ...) {
    va_list args;
    va_start(args, format);
    int result = format_vprintf(buffer, size, format, args);
    va_end(args);
    return result;
}

int format_vprintf(char* buffer, size_t size, const char* format, va_list args) {
    if (!buffer || !format || size == 0) {
        return -1;
    }
    
    int result;
#if FORMAT_PLATFORM_WINDOWS && defined(_MSC_VER)
    result = _vsnprintf_s(buffer, size, _TRUNCATE, format, args);
#else
    result = vsnprintf(buffer, size, format, args);
#endif
    
    if (result < 0) {
        buffer[0] = '\0';
        return -1;
    }
    
    if ((size_t)result >= size) {
        buffer[size - 1] = '\0';
        return (int)(size - 1);
    }
    
    return result;
}

char* format_alloc(const char* format, ...) {
    va_list args;
    va_start(args, format);
    char* result = format_valloc(format, args);
    va_end(args);
    return result;
}

char* format_valloc(const char* format, va_list args) {
    if (!format) {
        return NULL;
    }
    
    // 第一次调用：计算所需大小
    va_list args_copy;
    va_copy(args_copy, args);
    
    int size;
#if FORMAT_PLATFORM_WINDOWS && defined(_MSC_VER)
    size = _vscprintf(format, args_copy);
#else
    size = vsnprintf(NULL, 0, format, args_copy);
#endif
    va_end(args_copy);
    
    if (size < 0) {
        return NULL;
    }
    
    // 分配内存（+1 用于终止符）
    char* buffer = (char*)malloc((size_t)size + 1);
    if (!buffer) {
        return NULL;
    }
    
    // 第二次调用：实际格式化
#if FORMAT_PLATFORM_WINDOWS && defined(_MSC_VER)
    vsprintf_s(buffer, (size_t)size + 1, format, args);
#else
    vsprintf(buffer, format, args);
#endif
    
    return buffer;
}

// ==================== 类型格式化函数实现 ====================

int format_int(char* buffer, size_t size, i64 value, int base) {
    if (!buffer || size == 0 || base < 2 || base > 36) {
        return -1;
    }
    
    char temp[65]; // 最大 64 位 + 符号
    int pos = 0;
    bool negative = false;
    
    if (value < 0) {
        negative = true;
        value = -value;
    }
    
    u64 uvalue = (u64)value;
    
    // 转换数字
    do {
        int digit = uvalue % base;
        temp[pos++] = (digit < 10) ? ('0' + digit) : ('a' + digit - 10);
        uvalue /= base;
    } while (uvalue > 0);
    
    if (negative) {
        temp[pos++] = '-';
    }
    
    // 反转字符串
    if (pos >= (int)size) {
        pos = (int)size - 1;
    }
    
    for (int i = 0; i < pos; i++) {
        buffer[i] = temp[pos - 1 - i];
    }
    buffer[pos] = '\0';
    
    return pos;
}

int format_uint(char* buffer, size_t size, u64 value, int base) {
    if (!buffer || size == 0 || base < 2 || base > 36) {
        return -1;
    }
    
    char temp[65];
    int pos = 0;
    
    do {
        int digit = value % base;
        temp[pos++] = (digit < 10) ? ('0' + digit) : ('A' + digit - 10);
        value /= base;
    } while (value > 0);
    
    if (pos >= (int)size) {
        pos = (int)size - 1;
    }
    
    for (int i = 0; i < pos; i++) {
        buffer[i] = temp[pos - 1 - i];
    }
    buffer[pos] = '\0';
    
    return pos;
}

int format_float(char* buffer, size_t size, f64 value, int precision) {
    if (!buffer || size == 0 || precision < 0) {
        return -1;
    }
    
    if (precision > 20) {
        precision = 20;
    }
    
    char format_str[32];
    format_snprintf(format_str, sizeof(format_str), "%%.%df", precision);
    
    return format_snprintf(buffer, size, format_str, value);
}

int format_bool(char* buffer, size_t size, bool value) {
    if (!buffer || size == 0) {
        return -1;
    }
    
    const char* str = value ? "true" : "false";
    size_t len = strlen(str);
    
    if (len >= size) {
        len = size - 1;
    }
    
    memcpy(buffer, str, len);
    buffer[len] = '\0';
    
    return (int)len;
}

int format_char(char* buffer, size_t size, char value) {
    if (!buffer || size == 0) {
        return -1;
    }
    
    buffer[0] = value;
    buffer[1] = '\0';
    
    return 1;
}

int format_string(char* buffer, size_t size, const char* value, int max_len) {
    if (!buffer || size == 0 || !value) {
        return -1;
    }
    
    size_t len = strlen(value);
    
    if (max_len >= 0 && (size_t)max_len < len) {
        len = (size_t)max_len;
    }
    
    if (len >= size) {
        len = size - 1;
    }
    
    memcpy(buffer, value, len);
    buffer[len] = '\0';
    
    return (int)len;
}

int format_pointer(char* buffer, size_t size, const void* ptr) {
    if (!buffer || size == 0) {
        return -1;
    }
    
    if (!ptr) {
        return format_string(buffer, size, "(nil)", -1);
    }
    
    char temp[32];
    int pos = 0;
    
    // 添加 "0x" 前缀
    temp[pos++] = '0';
    temp[pos++] = 'x';
    
    // 格式化指针值
    uintptr_t addr = (uintptr_t)ptr;
    for (int i = sizeof(void*) * 2 - 1; i >= 0; i--) {
        int digit = (addr >> (i * 4)) & 0xF;
        temp[pos++] = (digit < 10) ? ('0' + digit) : ('a' + digit - 10);
    }
    
    if (pos >= (int)size) {
        pos = (int)size - 1;
    }
    
    memcpy(buffer, temp, pos);
    buffer[pos] = '\0';
    
    return pos;
}

// ==================== 带选项的格式化函数实现 ====================

int format_int_opts(char* buffer, size_t size, i64 value, int base, const FormatOptions* options) {
    if (!buffer || size == 0) {
        return -1;
    }
    
    char temp[65];
    int result = format_int(temp, sizeof(temp), value, base);
    
    if (result < 0) {
        return -1;
    }
    
    // 应用选项
    // TODO: 实现宽度、对齐、填充等选项
    
    return format_string(buffer, size, temp, -1);
}

int format_float_opts(char* buffer, size_t size, f64 value, const FormatOptions* options) {
    if (!buffer || size == 0) {
        return -1;
    }
    
    int precision = options ? options->precision : 6;
    return format_float(buffer, size, value, precision);
}

// ==================== 格式化构建器实现 ====================

struct FormatBuilder {
    char* buffer;
    size_t size;
    size_t length;
    size_t capacity;
};

FormatBuilder* format_builder_create(size_t initial_size) {
    FormatBuilder* fb = (FormatBuilder*)malloc(sizeof(FormatBuilder));
    if (!fb) {
        return NULL;
    }
    
    fb->capacity = initial_size > 0 ? initial_size : 256;
    fb->buffer = (char*)malloc(fb->capacity);
    if (!fb->buffer) {
        free(fb);
        return NULL;
    }
    
    fb->buffer[0] = '\0';
    fb->size = 0;
    fb->length = 0;
    
    return fb;
}

void format_builder_destroy(FormatBuilder* fb) {
    if (fb) {
        free(fb->buffer);
        free(fb);
    }
}

static int format_builder_ensure_capacity(FormatBuilder* fb, size_t additional) {
    if (fb->length + additional + 1 <= fb->capacity) {
        return 0;
    }
    
    size_t new_capacity = fb->capacity * 2;
    while (new_capacity < fb->length + additional + 1) {
        new_capacity *= 2;
    }
    
    char* new_buffer = (char*)realloc(fb->buffer, new_capacity);
    if (!new_buffer) {
        return -1;
    }
    
    fb->buffer = new_buffer;
    fb->capacity = new_capacity;
    
    return 0;
}

FormatBuilder* format_builder_append(FormatBuilder* fb, const char* format, ...) {
    if (!fb || !format) {
        return fb;
    }
    
    va_list args;
    va_start(args, format);
    
    // 计算所需大小
    va_list args_copy;
    va_copy(args_copy, args);
    
    int needed;
#if FORMAT_PLATFORM_WINDOWS && defined(_MSC_VER)
    needed = _vscprintf(format, args_copy);
#else
    needed = vsnprintf(fb->buffer + fb->length, 0, format, args_copy);
#endif
    va_end(args_copy);
    
    if (needed < 0) {
        va_end(args);
        return fb;
    }
    
    // 确保容量
    if (format_builder_ensure_capacity(fb, (size_t)needed) != 0) {
        va_end(args);
        return fb;
    }
    
    // 实际格式化
#if FORMAT_PLATFORM_WINDOWS && defined(_MSC_VER)
    int written = vsprintf_s(fb->buffer + fb->length, fb->capacity - fb->length, format, args);
#else
    int written = vsprintf(fb->buffer + fb->length, format, args);
#endif
    va_end(args);
    
    if (written > 0) {
        fb->length += (size_t)written;
    }
    
    return fb;
}

FormatBuilder* format_builder_append_str(FormatBuilder* fb, const char* str) {
    if (!fb || !str) {
        return fb;
    }
    
    size_t len = strlen(str);
    if (format_builder_ensure_capacity(fb, len) != 0) {
        return fb;
    }
    
    memcpy(fb->buffer + fb->length, str, len);
    fb->length += len;
    fb->buffer[fb->length] = '\0';
    
    return fb;
}

FormatBuilder* format_builder_append_char(FormatBuilder* fb, char c) {
    if (!fb) {
        return fb;
    }
    
    if (format_builder_ensure_capacity(fb, 1) != 0) {
        return fb;
    }
    
    fb->buffer[fb->length++] = c;
    fb->buffer[fb->length] = '\0';
    
    return fb;
}

FormatBuilder* format_builder_append_int(FormatBuilder* fb, i64 value) {
    if (!fb) {
        return fb;
    }
    
    char temp[32];
    int len = format_int(temp, sizeof(temp), value, 10);
    
    if (len > 0) {
        format_builder_append_str(fb, temp);
    }
    
    return fb;
}

FormatBuilder* format_builder_append_float(FormatBuilder* fb, f64 value, int precision) {
    if (!fb) {
        return fb;
    }
    
    char temp[64];
    int len = format_float(temp, sizeof(temp), value, precision);
    
    if (len > 0) {
        format_builder_append_str(fb, temp);
    }
    
    return fb;
}

const char* format_builder_get(FormatBuilder* fb) {
    if (!fb) {
        return NULL;
    }
    
    return fb->buffer;
}

size_t format_builder_length(FormatBuilder* fb) {
    if (!fb) {
        return 0;
    }
    
    return fb->length;
}

void format_builder_clear(FormatBuilder* fb) {
    if (fb) {
        fb->length = 0;
        fb->buffer[0] = '\0';
    }
}

void format_builder_reset(FormatBuilder* fb) {
    if (fb) {
        format_builder_clear(fb);
        fb->capacity = 256;
        char* new_buffer = (char*)realloc(fb->buffer, fb->capacity);
        if (new_buffer) {
            fb->buffer = new_buffer;
        }
    }
}
