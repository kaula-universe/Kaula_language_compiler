/* Kaula bare-metal linker script for i386 (32-bit)
 * Loads the kernel at 1M, entry _start (defined in the boot stub).
 * Boot stub: i386-multiboot.S (GRUB/QEMU, Multiboot v1).
 *
 * Contract with boot stubs:
 *   - __bss_start / __bss_end: 由 boot stub 在进入 C 代码前清零
 *   - stack_top:               内核栈（位于 .bss 末尾，清零后使用） */

ENTRY(_start)

SECTIONS
{
    . = 1M;

    .multiboot : { KEEP(*(.multiboot)) }   /* Multiboot v1 header */
    .text      : { *(.text*) }
    .rodata    : { *(.rodata*) }
    .data      : { *(.data*) }

    __bss_start = .;
    .bss       : {
        *(COMMON)
        *(.bss*)
        *(.sbss*)
    }
    __bss_end = .;

    /* 内核栈：64KB，位于 BSS 末尾 */
    . = ALIGN(16);
    . += 65536;
    stack_top = .;

    /DISCARD/ : { *(.comment) *(.eh_frame*) *(.note.gnu.build-id) }
}
