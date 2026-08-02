# Kaula bare-metal boot stub for x86_64 (Xen PVH ELF note)
# QEMU loads this ELF64 kernel via the PVH note and jumps to _start
# in 32-bit protected mode. We transition to long mode and call
# _kaula_entry (the freestanding template entry).
#
# Placeholders: none. Entry symbol: _start (matches linker script).

# ===== Xen PVH ELF note (XEN_ELFNOTE_PHYS32_ENTRY = 18) =====
.section .note.Xen, "a", @note
.align 4
.long 4                  # namesz
.long 8                  # descsz: 64-bit entry address
.long 18                 # type: XEN_ELFNOTE_PHYS32_ENTRY
.asciz "Xen"
.align 4
.quad _start             # 32-bit entry point (long-mode trampoline)
.align 4

# 32-bit entry point
.section .bootstrap, "ax"
.code32
.global _start
_start:
    cli

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

.section .bss
.align 4096
pml4:
    .skip 4096
pdpt:
    .skip 4096
pd:
    .skip 4096

.align 16
.skip 65536
stack_top:
