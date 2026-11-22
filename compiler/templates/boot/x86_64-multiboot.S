# Kaula bare-metal boot stub for x86_64 (Multiboot v1)
# Booted by GRUB / QEMU -kernel (multiboot 支持) via the Multiboot v1 header.
# Enters 64-bit long mode, zeroes BSS, sets up stack, calls _kaula_entry
# (the freestanding template entry). Entry symbol: _start (matches linker script).
#
# 验证: qemu-system-x86_64 -kernel kernel.elf 或 GRUB 菜单项
#   亦可用: qemu-system-x86_64 -kernel kernel.elf -device isa-debug-exit

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

# 32-bit entry point
.section .bootstrap, "ax"
.code32
.global _start
_start:
    cli
    cld

    # 清零 BSS（__bss_start..__bss_end，含页表与栈；页表随后显式初始化）
    movl $__bss_start, %edi
    movl $__bss_end, %ecx
    subl %edi, %ecx
    shrl $2, %ecx
    xorl %eax, %eax
    rep stosl

    # Load GDT
    lgdt gdt_ptr

    # Enable PAE
    movl %cr4, %eax
    orl  $0x20, %eax
    # Enable SSE (OSFXSR + OSXMMEXCPT)
    orl  $0x600, %eax
    movl %eax, %cr4

    # Setup identity page tables for low 1GB (2MB huge pages)
    movl $pd, %eax
    orl  $0x03, %eax
    movl %eax, pdpt
    movl $pdpt, %eax
    orl  $0x03, %eax
    movl %eax, pml4

    movl $pd, %edi
    movl $0x00000000, %eax
    movl $512, %ecx
1:
    orl  $0x83, %eax        # present | writable | PS(huge)
    movl %eax, (%edi)
    addl $0x200000, %eax
    addl $8, %edi
    loop 1b

    # CR3 -> PML4
    movl $pml4, %eax
    movl %eax, %cr3

    # EFER.LME = 1
    movl $0xC0000080, %ecx
    rdmsr
    orl  $0x100, %eax
    wrmsr

    # Enable paging + PE
    movl %cr0, %eax
    orl  $0x80000001, %eax
    movl %eax, %cr0

    # Far jump to 64-bit
    ljmp $0x08, $start64

.code64
start64:
    movw $0x10, %ax
    movw %ax, %ds
    movw %ax, %es
    movw %ax, %fs
    movw %ax, %gs
    movw %ax, %ss

    # Set up stack
    movabsq $stack_top, %rsp

    # Call Kaula bare-metal entry
    call _kaula_entry
.hang:
    hlt
    jmp .hang

.section .rodata
.align 8
gdt:
    .quad 0x0000000000000000
    .quad 0x00AF9A000000FFFF   # kernel code (64-bit)
    .quad 0x00CF92000000FFFF   # kernel data
gdt_ptr:
    .word gdt_ptr - gdt - 1
    .quad gdt

# 页表与栈位于 .bss（boot stub 顶部已清零，随后显式初始化页表）
.section .bss
.align 4096
pml4:
    .skip 4096
pdpt:
    .skip 4096
pd:
    .skip 4096
