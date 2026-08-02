// freestanding/io/io.c — 输入输出模块实现
// 无 <stdio.h>/libc 依赖：输出经 fs_output_putchar 弱钩子（默认空操作），
// 内置迷你 printf（%d %i %u %x %X %o %s %c %f %g %% 及 -0 标志/宽度/精度）
// 符号 FS_WEAK

#ifndef KAULA_FREESTANDING_IO_C
#define KAULA_FREESTANDING_IO_C

#include "io.h"
#include "../base/fs_common.h"
#include "../string/string.h"
#include "../math/math.h"
#include <stdarg.h>
#include <stdint.h>

// ==================== 输出钩子（默认空操作，用户可覆写为强符号） ====================

FS_WEAK void fs_output_putchar(char c) {
    (void)c; // 默认丢弃输出；裸机覆写为 UART，托管覆写为控制台
}

// ==================== 底层输出 ====================

FS_WEAK void fs_putchar(char c) {
    fs_output_putchar(c);
}

FS_WEAK void fs_puts(const char* s) {
    if (s == NULL) return;
    while (*s != 0) {
        fs_output_putchar(*s);
        s++;
    }
}

FS_WEAK void fs_putint(i64 value) {
    char buf[24];
    fs_puts(fs_itoa(value, buf));
}

FS_WEAK void fs_putuint(u64 value) {
    char buf[24];
    fs_puts(fs_utoa(value, buf));
}

FS_WEAK void fs_puthex_noprefix(u64 value) {
    char buf[20];
    fs_puts(fs_itoa_hex(value, buf, false));
}

FS_WEAK void fs_puthex(u64 value) {
    fs_puts("0x");
    fs_puthex_noprefix(value);
}

FS_WEAK void fs_putbin(u64 value) {
    fs_puts("0b");
    for (int i = 63; i >= 0; i--) {
        fs_output_putchar(((value >> i) & 1ULL) ? '1' : '0');
    }
}

// ==================== 浮点格式化（无 libc） ====================

// 输出 f64，精度 prec 位小数（不支持指数形式；超大值按整数输出）
static void fs_format_double(f64 val, int prec) {
    if (math_isnan(val)) {
        fs_puts("nan");
        return;
    }
    if (math_isinf(val)) {
        if (math_signbit(val)) fs_putchar('-');
        fs_puts("inf");
        return;
    }
    if (prec < 0) prec = 0;
    if (prec > 12) prec = 12;

    // 处理符号（含负零）
    if (math_signbit(val)) {
        fs_putchar('-');
        val = math_abs(val);
    }
    if (val == 0.0) {
        fs_putchar('0');
        if (prec > 0) {
            fs_putchar('.');
            for (int i = 0; i < prec; i++) fs_putchar('0');
        }
        return;
    }

    // 超过 2^63 的值：直接按无符号整数近似输出
    if (val >= 9.2233720368547758e18) {
        fs_putuint((u64)val);
        return;
    }

    u64 ip = (u64)val;
    fs_putuint(ip);

    if (prec > 0) {
        fs_putchar('.');
        f64 frac = val - (f64)ip;
        for (int i = 0; i < prec; i++) {
            frac *= 10.0;
            int d = (int)frac;
            if (d > 9) d = 9;
            fs_output_putchar((char)('0' + d));
            frac -= (f64)d;
        }
    }
}

// %g 语义（与 std.io 的 println_multi 一致）：整数值直接输出整数
static void fs_format_g(f64 val, int prec) {
    if (math_isnan(val) || math_isinf(val)) {
        fs_format_double(val, 0);
        return;
    }
    i64 iv = (i64)val;
    if (val == (f64)iv && math_abs(val) < 1e15) {
        fs_putint(iv);
        return;
    }
    if (prec <= 0) prec = 6;
    fs_format_double(val, prec);
}

// ==================== 迷你 printf 核心 ====================

// 输出无符号整数（base 2/8/10/16，大写可选，最小宽度 zero_pad）
static void fs_print_u64_base(u64 value, unsigned base, bool uppercase, int width, bool zero_pad, bool left_align, bool show_prefix) {
    char digits[] = "0123456789abcdef";
    char upper[] = "0123456789ABCDEF";
    char tmp[68];
    int i = 0;
    const char* table = uppercase ? upper : digits;

    u64 v = value;
    if (v == 0) {
        tmp[i++] = '0';
    }
    while (v > 0) {
        tmp[i++] = table[v % base];
        v /= base;
    }

    if (show_prefix) {
        if (base == 16) fs_puts(uppercase ? "0X" : "0x");
        else if (base == 8) fs_putchar('0');
        else if (base == 2) fs_puts("0b");
    }

    int digits_len = i;
    int pad = left_align ? 0 : (width - digits_len);
    if (pad > 0 && zero_pad) {
        while (pad-- > 0) fs_putchar('0');
    } else if (pad > 0) {
        while (pad-- > 0) fs_putchar(' ');
    }
    while (i > 0) {
        fs_putchar(tmp[--i]);
    }
}

static void fs_printf_va(const char* format, va_list ap) {
    if (format == NULL) return;

    while (*format != 0) {
        if (*format != '%') {
            fs_putchar(*format);
            format++;
            continue;
        }
        format++; // 跳过 '%'

        // 标志
        bool left_align = false;
        bool zero_pad = false;
        for (;;) {
            if (*format == '-') { left_align = true; format++; }
            else if (*format == '0') { zero_pad = true; format++; }
            else break;
        }

        // 宽度
        int width = 0;
        while (*format >= '0' && *format <= '9') {
            width = width * 10 + (*format - '0');
            format++;
        }

        // 长度修饰符：'l'/'ll' 标记 64 位整数，其余忽略
        bool is_long = false;
        while (*format == 'l') {
            is_long = true;
            format++;
        }
        while (*format == 'h' || *format == 'z' ||
               *format == 'j' || *format == 't') {
            format++;
        }

        bool uppercase = false;
        switch (*format) {
        case '%':
            fs_putchar('%');
            format++;
            break;
        case 'c': {
            char c = (char)va_arg(ap, int);
            fs_putchar(c);
            format++;
            break;
        }
        case 's': {
            const char* s = va_arg(ap, const char*);
            if (s == NULL) s = "(null)";
            fs_puts(s);
            format++;
            break;
        }
        case 'd':
        case 'i': {
            i64 v = is_long ? (i64)va_arg(ap, long long) : (i64)va_arg(ap, int);
            char buf[24];
            fs_puts(fs_itoa(v, buf));
            format++;
            break;
        }
        case 'u': {
            u64 v = is_long ? (u64)va_arg(ap, unsigned long long) : (u64)va_arg(ap, unsigned int);
            fs_print_u64_base(v, 10, false, width, zero_pad, left_align, false);
            format++;
            break;
        }
        case 'x':
        case 'X': {
            uppercase = (*format == 'X');
            u64 v = is_long ? (u64)va_arg(ap, unsigned long long) : (u64)va_arg(ap, unsigned int);
            fs_print_u64_base(v, 16, uppercase, width, zero_pad, left_align, false);
            format++;
            break;
        }
        case 'o': {
            u64 v = is_long ? (u64)va_arg(ap, unsigned long long) : (u64)va_arg(ap, unsigned int);
            fs_print_u64_base(v, 8, false, width, zero_pad, left_align, false);
            format++;
            break;
        }
        case 'b': {
            u64 v = is_long ? (u64)va_arg(ap, unsigned long long) : (u64)va_arg(ap, unsigned int);
            fs_print_u64_base(v, 2, false, width, zero_pad, left_align, false);
            format++;
            break;
        }
        case 'p': {
            void* p = va_arg(ap, void*);
            fs_puts("0x");
            fs_print_u64_base((u64)(uintptr_t)p, 16, false, 0, false, false, false);
            format++;
            break;
        }
        case 'f': {
            double d = va_arg(ap, double);
            fs_format_double(d, 6);
            format++;
            break;
        }
        case 'g': {
            double d = va_arg(ap, double);
            fs_format_g(d, 6);
            format++;
            break;
        }
        default:
            fs_putchar('%');
            break;
        }
    }
}

// ==================== 与 std.io 同名的格式化输出 ====================

FS_WEAK void print(const char* format, ...) {
    va_list args;
    va_start(args, format);
    fs_printf_va(format, args);
    va_end(args);
}

FS_WEAK void println(const char* format, ...) {
    va_list args;
    va_start(args, format);
    fs_printf_va(format, args);
    va_end(args);
    fs_putchar('\n');
}

// type: 0=int, 1=double, 2=string（与 std.io 相同调用约定）
FS_WEAK void println_multi(int arg_count, ...) {
    va_list args;
    va_start(args, arg_count);

    for (int i = 0; i < arg_count; i++) {
        int type = va_arg(args, int);
        switch (type) {
        case 0: { // int
            long long v = va_arg(args, long long);
            fs_putint((i64)v);
            break;
        }
        case 1: { // double
            double val = va_arg(args, double);
            fs_format_g(val, 6);
            break;
        }
        case 2: { // string
            const char* s = va_arg(args, const char*);
            if (s == NULL) s = "(null)";
            fs_puts(s);
            break;
        }
        default:
            break;
        }
    }
    va_end(args);
    fs_putchar('\n');
}

FS_WEAK void print_char(char c) {
    fs_putchar(c);
}

FS_WEAK void print_int(i64 value) {
    fs_putint(value);
}

FS_WEAK void print_float(f64 value) {
    fs_format_double(value, 6);
}

FS_WEAK void print_bool(bool value) {
    fs_puts(value ? "true" : "false");
}

FS_WEAK void print_hex(u64 value) {
    fs_puthex(value);
}

#endif /* KAULA_FREESTANDING_IO_C */
