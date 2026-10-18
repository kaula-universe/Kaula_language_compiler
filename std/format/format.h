#ifndef STD_FORMAT_FORMAT_H
#define STD_FORMAT_FORMAT_H

#include "../base/types.h"
#include <stdarg.h>
#include <stddef.h>

/**
 * @file format.h
 * @brief Kaula 格式化库 - 跨平台字符串格式化
 * 
 * 提供统一的格式化接口，支持：
 * - 安全的字符串格式化
 * - 跨平台兼容
 * - 内存安全
 * - 高性能
 */

// ==================== 跨平台格式化宏 ====================
#if defined(_WIN32)
    #define FORMAT_PLATFORM_WINDOWS 1
    #define FORMAT_PLATFORM_UNIX 0
#else
    #define FORMAT_PLATFORM_WINDOWS 0
    #define FORMAT_PLATFORM_UNIX 1
#endif

// 编译器特性检测
#if defined(__GNUC__) || defined(__clang__)
    #define FORMAT_PRINTF_FUNC(fmt_idx, arg_idx) \
        __attribute__((format(printf, fmt_idx, arg_idx)))
#else
    #define FORMAT_PRINTF_FUNC(fmt_idx, arg_idx)
#endif

// ==================== 基础格式化函数 ====================

/**
 * @brief 安全的字符串格式化
 * @param buffer 目标缓冲区
 * @param size 缓冲区大小
 * @param format 格式化字符串
 * @param ... 可变参数
 * @return 写入的字符数（不包括终止符），失败返回 -1
 */
extern int format_printf(char* buffer, size_t size, const char* format, ...)
    FORMAT_PRINTF_FUNC(3, 4);

/**
 * @brief 带参数的字符串格式化
 * @param buffer 目标缓冲区
 * @param size 缓冲区大小
 * @param format 格式化字符串
 * @param args 可变参数列表
 * @return 写入的字符数（不包括终止符），失败返回 -1
 */
extern int format_vprintf(char* buffer, size_t size, const char* format, va_list args);

/**
 * @brief 动态字符串格式化（自动分配内存）
 * @param format 格式化字符串
 * @param ... 可变参数
 * @return 格式化后的字符串（需要手动释放），失败返回 NULL
 */
extern char* format_alloc(const char* format, ...)
    FORMAT_PRINTF_FUNC(1, 2);

/**
 * @brief 动态字符串格式化（va_list 版本）
 * @param format 格式化字符串
 * @param args 可变参数列表
 * @return 格式化后的字符串（需要手动释放），失败返回 NULL
 */
extern char* format_valloc(const char* format, va_list args);

// ==================== 类型格式化函数 ====================

/**
 * @brief 格式化整数
 * @param buffer 目标缓冲区
 * @param size 缓冲区大小
 * @param value 整数值
 * @param base 进制（10=十进制，16=十六进制等）
 * @return 写入的字符数
 */
extern int format_int(char* buffer, size_t size, i64 value, int base);

/**
 * @brief 格式化无符号整数
 * @param buffer 目标缓冲区
 * @param size 缓冲区大小
 * @param value 无符号整数值
 * @param base 进制
 * @return 写入的字符数
 */
extern int format_uint(char* buffer, size_t size, u64 value, int base);

/**
 * @brief 格式化浮点数
 * @param buffer 目标缓冲区
 * @param size 缓冲区大小
 * @param value 浮点数值
 * @param precision 小数位数
 * @return 写入的字符数
 */
extern int format_float(char* buffer, size_t size, f64 value, int precision);

/**
 * @brief 格式化布尔值
 * @param buffer 目标缓冲区
 * @param size 缓冲区大小
 * @param value 布尔值
 * @return 写入的字符数
 */
extern int format_bool(char* buffer, size_t size, bool value);

/**
 * @brief 格式化字符
 * @param buffer 目标缓冲区
 * @param size 缓冲区大小
 * @param value 字符值
 * @return 写入的字符数
 */
extern int format_char(char* buffer, size_t size, char value);

/**
 * @brief 格式化字符串
 * @param buffer 目标缓冲区
 * @param size 缓冲区大小
 * @param value 字符串值
 * @param max_len 最大长度（-1 表示无限制）
 * @return 写入的字符数
 */
extern int format_string(char* buffer, size_t size, const char* value, int max_len);

/**
 * @brief 格式化指针地址
 * @param buffer 目标缓冲区
 * @param size 缓冲区大小
 * @param ptr 指针值
 * @return 写入的字符数
 */
extern int format_pointer(char* buffer, size_t size, const void* ptr);

// ==================== 格式化选项 ====================

typedef struct {
    int width;          // 字段宽度
    int precision;      // 精度
    bool left_align;    // 左对齐
    bool show_sign;     // 显示符号
    bool show_base;     // 显示进制前缀（0x, 0b）
    bool zero_pad;      // 零填充
    char pad_char;      // 填充字符
} FormatOptions;

/**
 * @brief 带选项的整数格式化
 * @param buffer 目标缓冲区
 * @param size 缓冲区大小
 * @param value 整数值
 * @param base 进制
 * @param options 格式化选项
 * @return 写入的字符数
 */
extern int format_int_opts(char* buffer, size_t size, i64 value, int base, const FormatOptions* options);

/**
 * @brief 带选项的浮点数格式化
 * @param buffer 目标缓冲区
 * @param size 缓冲区大小
 * @param value 浮点数值
 * @param options 格式化选项
 * @return 写入的字符数
 */
extern int format_float_opts(char* buffer, size_t size, f64 value, const FormatOptions* options);

// ==================== 格式化构建器 ====================

typedef struct FormatBuilder FormatBuilder;

/**
 * @brief 创建格式化构建器
 * @param initial_size 初始缓冲区大小
 * @return 格式化构建器指针
 */
extern FormatBuilder* format_builder_create(size_t initial_size);

/**
 * @brief 销毁格式化构建器
 * @param fb 格式化构建器指针
 */
extern void format_builder_destroy(FormatBuilder* fb);

/**
 * @brief 追加格式化字符串
 * @param fb 格式化构建器指针
 * @param format 格式化字符串
 * @param ... 可变参数
 * @return 构建器自身
 */
extern FormatBuilder* format_builder_append(FormatBuilder* fb, const char* format, ...)
    FORMAT_PRINTF_FUNC(2, 3);

/**
 * @brief 追加字符串
 * @param fb 格式化构建器指针
 * @param str 字符串
 * @return 构建器自身
 */
extern FormatBuilder* format_builder_append_str(FormatBuilder* fb, const char* str);

/**
 * @brief 追加字符
 * @param fb 格式化构建器指针
 * @param c 字符
 * @return 构建器自身
 */
extern FormatBuilder* format_builder_append_char(FormatBuilder* fb, char c);

/**
 * @brief 追加整数
 * @param fb 格式化构建器指针
 * @param value 整数值
 * @return 构建器自身
 */
extern FormatBuilder* format_builder_append_int(FormatBuilder* fb, i64 value);

/**
 * @brief 追加浮点数
 * @param fb 格式化构建器指针
 * @param value 浮点数值
 * @param precision 小数位数
 * @return 构建器自身
 */
extern FormatBuilder* format_builder_append_float(FormatBuilder* fb, f64 value, int precision);

/**
 * @brief 获取构建结果
 * @param fb 格式化构建器指针
 * @return 格式化后的字符串（由构建器管理，无需释放）
 */
extern const char* format_builder_get(FormatBuilder* fb);

/**
 * @brief 获取构建结果长度
 * @param fb 格式化构建器指针
 * @return 字符串长度
 */
extern size_t format_builder_length(FormatBuilder* fb);

/**
 * @brief 清空构建器
 * @param fb 格式化构建器指针
 */
extern void format_builder_clear(FormatBuilder* fb);

/**
 * @brief 重置构建器（释放内存）
 * @param fb 格式化构建器指针
 */
extern void format_builder_reset(FormatBuilder* fb);

// ==================== 便捷宏 ====================

/**
 * @brief 快速格式化宏
 * @param fmt 格式化字符串
 * @param ... 可变参数
 * @return 格式化后的字符串（需要手动释放）
 */
#define FMT(fmt, ...) format_alloc(fmt, ##__VA_ARGS__)

/**
 * @brief 安全格式化宏（栈上缓冲区）
 * @param buf 缓冲区名
 * @param fmt 格式化字符串
 * @param ... 可变参数
 */
#define FMT_SAFE(buf, fmt, ...) \
    format_printf(buf, sizeof(buf), fmt, ##__VA_ARGS__)

/**
 * @brief 格式化构建器便捷宏
 * @param initial_size 初始大小
 * @param fmt 格式化字符串
 * @param ... 可变参数
 * @return 格式化后的字符串
 */
#define FMT_BUILD(initial_size, fmt, ...) \
    ({ \
        FormatBuilder* fb = format_builder_create(initial_size); \
        format_builder_append(fb, fmt, ##__VA_ARGS__); \
        const char* result = format_builder_get(fb); \
        format_builder_destroy(fb); \
        result; \
    })

#endif // STD_FORMAT_FORMAT_H
