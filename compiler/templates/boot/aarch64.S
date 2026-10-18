# Kaula bare-metal boot stub for aarch64 (ARM64, 通用入口，无 bootloader 协议)
# QEMU: qemu-system-aarch64 -M virt -cpu cortex-a57 -kernel kernel.elf
# 入口在 0x40000000（QEMU virt DRAM 起始，-kernel ELF 以 EL1 启动）。
# 清零 BSS、设置栈后调用 _kaula_entry（freestanding 模板入口）。
# 入口符号: _start（与链接脚本一致）。

.section .text, "ax"
.global _start
_start:
    # 清零 BSS（__bss_start..__bss_end，含栈；随后设置栈）
    adrp x0, __bss_start
    add  x0, x0, :lo12:__bss_start
    adrp x1, __bss_end
    add  x1, x1, :lo12:__bss_end
    mov  x2, xzr
1:
    cmp  x0, x1
    b.hs 2f
    str  x2, [x0]
    add  x0, x0, #8
    b    1b
2:
    # 设置栈（16 字节对齐，见链接脚本 stack_top）
    adrp x0, stack_top
    add  sp, x0, :lo12:stack_top

    # 调用 Kaula bare-metal 入口
    bl   _kaula_entry
.hang:
    wfi
    b    .hang
