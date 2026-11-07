#include "format.h"
#include "../memory/memory.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <float.h>
#include <limits.h>
#include <math.h>

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
    
    int result = vsnprintf(buffer, size, format, args);

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
    char* buffer = (char*)kmm_v4_malloc((size_t)size + 1);
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
    return format_int_opts(buffer, size, value, base, NULL);
}

int format_uint(char* buffer, size_t size, u64 value, int base) {
    /* Keep the historical uppercase default of the plain unsigned API. */
    FormatOptions options = {0};
    options.uppercase = true;
    return format_uint_opts(buffer, size, value, base, &options);
}

int format_float(char* buffer, size_t size, f64 value, int precision) {
    if (precision < 0) {
        if (buffer && size) buffer[0] = '\0';
        return -1;
    }
    FormatOptions options = {0};
    options.precision = precision;
    return format_float_opts(buffer, size, value, &options);
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
    
    if (size == 1) { buffer[0] = '\0'; return 0; }
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

/* All output is a prefix of the complete formatted field. Repeated padding is
 * bounded by destination capacity, even for INT32_MAX width. */
typedef struct { char* data; size_t capacity; size_t used; } FormatWriter;

static void format_write(FormatWriter* w, const char* text, size_t count) {
    size_t room = w->capacity - w->used;
    if (count > room) count = room;
    if (count) memcpy(w->data + w->used, text, count);
    w->used += count;
}

static void format_repeat(FormatWriter* w, char c, size_t count) {
    size_t room = w->capacity - w->used;
    if (count > room) count = room;
    if (count) memset(w->data + w->used, c, count);
    w->used += count;
}

static int format_field(char* buffer, size_t size, const char* lead,
                        const char* digits, const FormatOptions* o) {
    if (!buffer || !size) return -1;
    size_t prefix = strlen(lead), len = strlen(digits);
    size_t pad = o && o->width > 0 && (size_t)o->width > prefix + len
        ? (size_t)o->width - prefix - len : 0;
    FormatWriter w = {buffer, size - 1 > INT_MAX ? INT_MAX : size - 1, 0};
    char fill = o && o->pad_char ? o->pad_char : ' ';
    if (o && o->left_align) {
        format_write(&w, lead, prefix);
        format_write(&w, digits, len);
        format_repeat(&w, fill, pad);
    } else if (o && o->zero_pad) {
        format_write(&w, lead, prefix);
        format_repeat(&w, '0', pad);
        format_write(&w, digits, len);
    } else {
        format_repeat(&w, fill, pad);
        format_write(&w, lead, prefix);
        format_write(&w, digits, len);
    }
    buffer[w.used] = '\0';
    return (int)w.used;
}

/* Group only the decimal integral part; leave fraction/exponent untouched. */
static void format_group(char* out, const char* text) {
    size_t integral = 0, pos = 0;
    while (text[integral] >= '0' && text[integral] <= '9') ++integral;
    for (size_t i = 0; i < integral; ++i) {
        if (i && (integral - i) % 3 == 0) out[pos++] = ',';
        out[pos++] = text[i];
    }
    strcpy(out + pos, text + integral);
}

static int format_integer(char* buffer, size_t size, u64 value, bool negative,
                          i32 base, const FormatOptions* o) {
    if (!buffer || !size) return -1;
    buffer[0] = '\0';
    if (base < 2 || base > 36) return -1;
    const char* alphabet = o && o->uppercase
        ? "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ" : "0123456789abcdefghijklmnopqrstuvwxyz";
    char reversed[64], digits[65], grouped[86], lead[4];
    size_t n = 0, p = 0;
    do { reversed[n++] = alphabet[value % (u64)base]; value /= (u64)base; } while (value);
    for (size_t i = 0; i < n; ++i) digits[i] = reversed[n - 1 - i];
    digits[n] = '\0';
    if (negative) lead[p++] = '-';
    else if (o && o->show_sign) lead[p++] = '+';
    if (o && o->show_base && (base == 2 || base == 8 || base == 16)) {
        lead[p++] = '0';
        lead[p++] = base == 2 ? 'b' : base == 8 ? 'o' : 'x';
        if (o->uppercase) lead[p - 1] -= 'a' - 'A';
    }
    lead[p] = '\0';
    if (o && o->thousands && base == 10) {
        format_group(grouped, digits);
        return format_field(buffer, size, lead, grouped, o);
    }
    return format_field(buffer, size, lead, digits, o);
}

int format_int_opts(char* buffer, size_t size, i64 value, int base, const FormatOptions* options) {
    u64 magnitude = value < 0 ? (u64)(-(value + 1)) + 1 : (u64)value;
    return format_integer(buffer, size, magnitude, value < 0, base, options);
}

i32 format_uint_opts(char* buffer, u64 size, u64 value, i32 base, const FormatOptions* options) {
    if (size > SIZE_MAX) return -1;
    return format_integer(buffer, (size_t)size, value, false, base, options);
}

/* DBL_MAX has DBL_MAX_10_EXP+1 integral digits. Extra room covers decimal
 * separator, 20 fractional digits, exponent and terminator, including grouping. */
#define FORMAT_FLOAT_CAP (DBL_MAX_10_EXP + 64)

int format_float_opts(char* buffer, size_t size, f64 value, const FormatOptions* options) {
    if (!buffer || !size) return -1;
    buffer[0] = '\0';
    int precision = options && options->precision >= 0 ? options->precision : 6;
    if (precision > 20) precision = 20;
    bool upper = options && options->uppercase;
    bool scientific = options && options->scientific;
    char digits[FORMAT_FLOAT_CAP], grouped[FORMAT_FLOAT_CAP * 2], lead[2] = {0};
    if (signbit(value)) lead[0] = '-';
    else if (options && options->show_sign) lead[0] = '+';
    int result = snprintf(digits, sizeof(digits), scientific ? (upper ? "%.*E" : "%.*e")
                                                          : (upper ? "%.*F" : "%.*f"),
                          precision, fabs(value));
    if (result < 0 || (size_t)result >= sizeof(digits)) return -1;
    if (options && options->thousands) {
        format_group(grouped, digits);
        return format_field(buffer, size, lead, grouped, options);
    }
    return format_field(buffer, size, lead, digits, options);
}

FormatOptions* format_options_create(void) {
    FormatOptions* o = (FormatOptions*)kmm_v4_calloc(1, sizeof(FormatOptions));
    if (o) o->precision = 6;
    return o;
}

void format_options_set_layout(FormatOptions* o, i32 width, bool left_align, bool zero_pad, i32 pad_char) {
    if (!o) return;
    o->width = width;
    o->left_align = left_align;
    o->zero_pad = zero_pad;
    o->pad_char = pad_char > 0 && pad_char <= 127 ? (char)pad_char : ' ';
}

void format_options_set_precision(FormatOptions* o, i32 precision) {
    if (o) o->precision = precision;
}

void format_options_set_flags(FormatOptions* o, bool show_sign, bool show_base,
                              bool uppercase, bool thousands, bool scientific) {
    if (!o) return;
    o->show_sign = show_sign;
    o->show_base = show_base;
    o->uppercase = uppercase;
    o->thousands = thousands;
    o->scientific = scientific;
}

static char* format_text_buffer(const FormatOptions* o, size_t minimum, size_t* capacity) {
    *capacity = o && o->width > 0 && (size_t)o->width >= minimum ? (size_t)o->width + 1 : minimum;
    return (char*)kmm_v4_malloc(*capacity);
}

char* format_int_text(i64 value, i32 base, const FormatOptions* options) {
    if (base < 2 || base > 36) return NULL;
    size_t size;
    char* buffer = format_text_buffer(options, 96, &size);
    if (!buffer || format_int_opts(buffer, size, value, base, options) < 0) return NULL;
    return buffer;
}

char* format_uint_text(u64 value, i32 base, const FormatOptions* options) {
    if (base < 2 || base > 36) return NULL;
    size_t size;
    char* buffer = format_text_buffer(options, 96, &size);
    if (!buffer || format_uint_opts(buffer, size, value, base, options) < 0) return NULL;
    return buffer;
}

char* format_float_text(f64 value, const FormatOptions* options) {
    size_t size;
    char* buffer = format_text_buffer(options, FORMAT_FLOAT_CAP * 2, &size);
    if (!buffer || format_float_opts(buffer, size, value, options) < 0) return NULL;
    return buffer;
}

// ==================== 格式化构建器实现 ====================

struct FormatBuilder {
    char* buffer;
    size_t size;
    size_t length;
    size_t capacity;
};

FormatBuilder* format_builder_create(size_t initial_size) {
    FormatBuilder* fb = (FormatBuilder*)kmm_v4_calloc(1, sizeof(FormatBuilder));
    if (!fb) {
        return NULL;
    }
    
    fb->capacity = initial_size > 0 ? initial_size : 256;
    fb->buffer = (char*)kmm_v4_malloc(fb->capacity);
    if (!fb->buffer) {
        return NULL;
    }
    
    fb->buffer[0] = '\0';
    fb->size = 0;
    fb->length = 0;
    
    return fb;
}

void format_builder_destroy(FormatBuilder* fb) {
    // KMM 管理内存，无需手动释放
}

static int format_builder_ensure_capacity(FormatBuilder* fb, size_t additional) {
    if (fb->length + additional + 1 <= fb->capacity) {
        return 0;
    }
    
    size_t new_capacity = fb->capacity * 2;
    while (new_capacity < fb->length + additional + 1) {
        new_capacity *= 2;
    }
    
    char* new_buffer = (char*)kmm_v4_realloc(fb->buffer, new_capacity);
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
    
    char temp[FORMAT_FLOAT_CAP];
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
        char* new_buffer = (char*)kmm_v4_realloc(fb->buffer, fb->capacity);
        if (new_buffer) {
            fb->buffer = new_buffer;
        }
    }
}
