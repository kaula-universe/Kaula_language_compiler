// tests/freestanding/c/arch_uart.c — 裸机自检内核的串口输出钩子（按架构）
//
// 用途：freestanding.io 的输出默认走弱钩子 fs_output_putchar，裸机下没人实现
// 就是空操作 —— 内核能跑，但没法从 QEMU 观察结果。这个文件按目标架构实现该
// 钩子，测试 runner 把它额外链进 boot ELF（见 scripts/run_freestanding_tests.py
// 的 L7 层），QEMU 的 -nographic 就能看到自检输出。
//
// 串口地址（QEMU 模拟板的标准布局，与 QEMU 命令行一一对应）：
//   x86_64 / i386   : COM1  0x3F8   （-serial stdio / -nographic）
//   riscv64 virt    : NS16550A 0x10000000（UART0，data +0，LSR +5）
//   aarch64 virt    : PL011  0x09000000（UART0，DR +0，FR +0x18）
//
// 不实现测试退出设备（isa-debug-exit / sifive_test / semihosting）：内核跑完后
// 会停在 hlt/wfi + 死循环，runner 用超时终止并按串口内容判定成败。这样每个架构
// 只有一处需要匹配板级差异，不会因为退出约定的差异而漏测。

#include <stdint.h>

#if defined(KAULA_TEST_UART_X86)
static inline void ua_outb(uint16_t port, uint8_t v) {
    __asm__ __volatile__("outb %0, %1" : : "a"(v), "Nd"(port) : "memory");
}
static inline uint8_t ua_inb(uint16_t port) {
    uint8_t v;
    __asm__ __volatile__("inb %1, %0" : "=a"(v) : "Nd"(port) : "memory");
    return v;
}
#define COM1_DATA 0x3F8
#define COM1_LSR  0x3FD
void fs_output_putchar(int c) {
    ua_outb(COM1_LSR, 0x00);           /* DLAB=0 */
    while (!(ua_inb(COM1_LSR) & 0x20)) { /* 等待 THR 空 */
    }
    ua_outb(COM1_DATA, (uint8_t)c);
}

#elif defined(KAULA_TEST_UART_RISCV)
#define UART0_BASE 0x10000000u
static inline uint32_t ua_mmio_read(uint32_t addr) {
    return *(volatile uint32_t *)(uintptr_t)addr;
}
static inline void ua_mmio_write(uint32_t addr, uint32_t v) {
    *(volatile uint32_t *)(uintptr_t)addr = v;
}
void fs_output_putchar(int c) {
    while (ua_mmio_read(UART0_BASE + 0x05) & 0x20) { /* LSR: THRE */
    }
    ua_mmio_write(UART0_BASE + 0x00, (uint32_t)(uint8_t)c);
}

#elif defined(KAULA_TEST_UART_AARCH64)
#define UART0_BASE 0x09000000u
static inline uint32_t ua_mmio_read(uint32_t addr) {
    return *(volatile uint32_t *)(uintptr_t)addr;
}
static inline void ua_mmio_write(uint32_t addr, uint32_t v) {
    *(volatile uint32_t *)(uintptr_t)addr = v;
}
void fs_output_putchar(int c) {
    /* PL011: FR(0x18) 的 TXFF(bit5) 为 0 才可写 DR(0x00) */
    while (ua_mmio_read(UART0_BASE + 0x18) & (1u << 5)) {
    }
    ua_mmio_write(UART0_BASE + 0x00, (uint32_t)(uint8_t)c);
}

#else
#error "define one of KAULA_TEST_UART_X86 / _RISCV / _AARCH64"
#endif
