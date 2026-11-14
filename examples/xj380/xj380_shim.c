/*
 * xj380_shim.c — Kaula freestanding 程序与 XJ380 用户态的适配层
 *
 * 职责：
 *   1. 覆写 freestanding.io 的弱钩子 fs_output_putchar：把字符按行缓冲后
 *      交给 XJ380 的 XAPI_OUTPUT 系统调用，串口日志即可看到程序输出。
 *   2. 提供 C 入口 main：XJ380 的 crt0.S -> xapi_start 会调用 main，
 *      这里转调 Kaula 编译器生成的 kaula_main，并在返回时冲刷残余缓冲。
 *
 * 约定与来源：
 *   - XAPI_OUTPUT = 7381，见 OpenXJ380/user/xapi/include/libsys.h
 *   - enter_syscall 由 XJ380 的 user/xapi/libsys.cpp 提供（extern "C"）
 *   - 加载地址 0x200000、_start/xapi_start 由 XJ380 的 crt0.S + constart.cpp 提供
 *
 * 本文件不含任何 Kaula 业务逻辑，只做 ABI 桥接，因此可以复用到其他
 * freestanding 程序上。
 */

#include <stdint.h>

#define XAPI_OUTPUT 7381

extern uint64_t enter_syscall(uint64_t number, uint64_t a1, uint64_t a2, uint64_t a3,
                              uint64_t a4, uint64_t a5, uint64_t a6);
extern int32_t kaula_main(void);

#define LINE_CAPACITY 256
static char xj_line[LINE_CAPACITY];
static int xj_line_len;

static void xj_flush(void)
{
    if (xj_line_len == 0) return;
    xj_line[xj_line_len] = '\0';
    enter_syscall(XAPI_OUTPUT, (uint64_t)(uintptr_t)xj_line, 0, 0, 0, 0, 0);
    xj_line_len = 0;
}

/* 强符号覆写 freestanding/io/io.c 中的 FS_WEAK 默认空实现
 *
 * 缓冲满时先冲刷再接纳当前字符；早期实现是先判断容量、丢弃字符再冲刷，
 * 会让超过 LINE_CAPACITY-1 的行静默丢字节。这里按"先腾空间再写入"处理，
 * 保证任意长度输出都不丢字符且不越界。
 */
void fs_output_putchar(char c)
{
    if (xj_line_len >= LINE_CAPACITY - 1) {
        xj_flush();
    }
    xj_line[xj_line_len++] = c;
    if (c == '\n') {
        xj_flush();
    }
}

int main(int argc, char **argv, char **envp)
{
    (void)argc;
    (void)argv;
    (void)envp;

    int32_t result = kaula_main();
    xj_flush();
    return (int)result;
}
