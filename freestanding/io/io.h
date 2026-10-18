#ifndef KAULA_FREESTANDING_IO_H
#define KAULA_FREESTANDING_IO_H

// freestanding/io/io.h — 输入输出模块
// 与 std.io 同级、同名函数（print/println/print_char/...），但无 <stdio.h> 依赖：
//   1. 输出走 fs_output_putchar 弱符号钩子 —— 默认空操作，
//      裸机下用户覆写为 UART 串口输出，托管下覆写为控制台输出
//   2. print/println 内置迷你 printf，支持 %d %i %u %x %X %o %s %c %f %g %%
//   3. 无输入函数（裸机读取依赖具体硬件，由用户自行实现）
//
// 符号 FS_WEAK：托管模式下 std.io 的强符号优先

#include <stdarg.h>
#include "../base/types.h"

/* ============================================================================
 * 输出钩子（weak，可覆写）
 * ============================================================================ */

/**
 * fs_output_putchar - 单字符输出钩子
 * 默认空操作。裸机用户覆写为 UART/MMIO 写寄存器；
 * 托管用户覆写为 putchar 等控制台输出。
 * 覆写方法: 在用户代码中定义强符号同名函数即可
 */
extern void fs_output_putchar(char c);

/* ============================================================================
 * 底层输出（裸机常用）
 * ============================================================================ */

/**
 * fs_putchar - 输出单个字符（调用 fs_output_putchar）
 */
extern void fs_putchar(char c);

/**
 * fs_puts - 输出字符串（不含换行）
 */
extern void fs_puts(const char* s);

/**
 * fs_putint - 输出有符号十进制整数
 */
extern void fs_putint(i64 value);

/**
 * fs_putuint - 输出无符号十进制整数
 */
extern void fs_putuint(u64 value);

/**
 * fs_puthex - 输出无符号十六进制（0x 前缀，小写）
 */
extern void fs_puthex(u64 value);

/**
 * fs_puthex_noprefix - 输出无符号十六进制（无前缀，小写）
 */
extern void fs_puthex_noprefix(u64 value);

/**
 * fs_putbin - 输出无符号二进制（"0b" 前缀）
 */
extern void fs_putbin(u64 value);

/* ============================================================================
 * 与 std.io 同名的格式化输出
 * ============================================================================ */

/**
 * print - 格式化输出（支持 %d %i %u %x %X %o %s %c %f %g %%）
 */
extern void print(const char* format, ...);

/**
 * println - 格式化输出并换行
 */
extern void println(const char* format, ...);

/**
 * println_multi - 多参数类型推导输出（与 std.io 相同调用约定）
 * 参数: arg_count, (type, value)... type: 0=int, 1=double, 2=string
 */
extern void println_multi(int arg_count, ...);

/**
 * print_char - 输出单个字符（与 std.io 同名）
 */
extern void print_char(char c);

/**
 * print_int - 输出整数（与 std.io 同名）
 */
extern void print_int(i64 value);

/**
 * print_float - 输出浮点数（与 std.io 同名，%f 风格）
 */
extern void print_float(f64 value);

/**
 * print_bool - 输出 true/false（与 std.io 同名）
 */
extern void print_bool(bool value);

/**
 * print_hex - 输出十六进制（与 fs_puthex 等价）
 */
extern void print_hex(u64 value);

#endif /* KAULA_FREESTANDING_IO_H */
