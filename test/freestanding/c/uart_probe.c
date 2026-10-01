// tests/freestanding/c/uart_probe.c — 串口钩子的最小验证程序
//
// 目的：把"钩子实现是否正确"与"自检内核是否跑到 io 段"两件事分开。
// 直接在裸机入口里写几个字符到目标 UART，跑一下就知道串口地址/寄存器对不对。
#include <stdint.h>

void _start(void) {
#if defined(KAULA_TEST_UART_RISCV)
    volatile uint32_t* uart = (volatile uint32_t*)0x10000000u;
    const char* msg = "RISCV-UART-OK\n";
#elif defined(KAULA_TEST_UART_AARCH64)
    volatile uint32_t* uart = (volatile uint32_t*)0x09000000u;
    const char* msg = "AARCH64-UART-OK\n";
#else
    volatile uint16_t* port = (volatile uint16_t*)0x3F8;
    const char* msg = "X86-UART-OK\n";
#endif
    for (const char* p = msg; *p; ++p) {
#if defined(KAULA_TEST_UART_X86)
        port[0] = (uint16_t)(unsigned char)*p;
#elif defined(KAULA_TEST_UART_RISCV)
        while (uart[5] & 0x20) { }
        uart[0] = (uint32_t)(unsigned char)*p;
#else
        while (uart[6] & (1u << 5)) { }
        uart[0] = (uint32_t)(unsigned char)*p;
#endif
    }
    for (;;) { }
}
