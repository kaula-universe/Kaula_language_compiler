# Kaula bare-metal boot stub for riscv64 (通用入口，无 bootloader 协议)
# QEMU: qemu-system-riscv64 -M virt -bios none -kernel kernel.elf
# CPU 从 0x80000000 以 M-mode 启动（a0 = hartid，单核程序可忽略）。
# 清零 BSS、设置栈后调用 _kaula_entry（freestanding 模板入口）。
# 入口符号: _start（与链接脚本一致）。

.section .text, "ax"
.global _start
_start:
    # 清零 BSS（__bss_start..__bss_end，含栈；随后设置栈）
    la   t0, __bss_start
    la   t1, __bss_end
    li   t2, 0
1:
    bgeu t0, t1, 2f
    sd   t2, 0(t0)
    addi t0, t0, 8
    j    1b
2:
    # 设置栈（16 字节对齐，见链接脚本 stack_top）
    la   sp, stack_top

    # 调用 Kaula bare-metal 入口
    call _kaula_entry
.hang:
    wfi
    j    .hang
