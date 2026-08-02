# Kaula bare-metal boot stub for i386 (32-bit, Multiboot v1)
# Booted by GRUB / QEMU -kernel via the Multiboot v1 header.
# Zeroes BSS, sets up stack, calls _kaula_entry (the freestanding template entry).
# Entry symbol: _start (matches linker script).
#
# 验证: qemu-system-i386 -kernel kernel.elf

# ===== Multiboot v1 header =====
# flags: ALIGN | MEMINFO | AOUT_KLUDGE（带地址字段，GRUB 与 QEMU 均可确定性加载）
# 地址字段按链接地址 1M 编写（与模板链接脚本一致）
.section .multiboot, "a"
.align 4
.long 0x1BADB002             # MAGIC
.long 0x00010003             # FLAGS: ALIGN | MEMINFO | AOUT_KLUDGE
.long -(0x1BADB002 + 0x00010003)  # CHECKSUM
.long 0x100000               # header_addr（Multiboot 头所在物理地址）
.long 0x100000               # load_addr（代码段起始）
.long 0                     # load_end_addr（0 = 到文件末尾）
.long 0                     # bss_end_addr（0 = 无独立 BSS）
.long _start                # entry_addr

.section .text, "ax"
.code32
.global _start
_start:
    cli
    cld

    # 清零 BSS（__bss_start..__bss_end，含栈；随后设置栈）
    movl $__bss_start, %edi
    movl $__bss_end, %ecx
    subl %edi, %ecx
    shrl $2, %ecx
    xorl %eax, %eax
    rep stosl

    # Set up stack
    movl $stack_top, %esp

    # Call Kaula bare-metal entry
    call _kaula_entry
.hang:
    hlt
    jmp .hang
