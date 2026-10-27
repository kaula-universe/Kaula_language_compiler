// tests/freestanding/c/bss_probe.c — 验证"清零超出已加载镜像范围的内存"是否可用
//
// 背景：QEMU 用 -kernel 加载 ELF 时只给已加载镜像建映射（aarch64 virt 上表现为
// 越界访问 Data Abort → 跳到 0x200 的向量表）。内核的 BSS（KMM 静态池）远大于
// 镜像本身，boot stub 必须自己建映射（或在 MMU 关闭时直接跑）。
// 本程序在 0x40000000 之后清零 1MB，看会不会立刻挂。
#include <stdint.h>

#ifndef PROBE_BASE
#define PROBE_BASE 0x40000000ull
#endif
#ifndef PROBE_LEN
#define PROBE_LEN (1024ull * 1024)
#endif

void _start(void) {
    volatile uint32_t* p = (volatile uint32_t*)PROBE_BASE;
    for (uint64_t i = 0; i < PROBE_LEN / 4; ++i) {
        p[i] = 0;
    }
    volatile uint32_t* uart = (volatile uint32_t*)0x09000000u;
    const char* msg = "BSS-CLEAR-OK\n";
    for (const char* q = msg; *q; ++q) {
        while (uart[6] & (1u << 5)) { }
        uart[0] = (uint32_t)(unsigned char)*q;
    }
    for (;;) { }
}
