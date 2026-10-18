/* Kaula bare-metal linker script for x86_64
 * Loads the kernel at 1M, entry _start (defined in the boot stub).
 * Boot stubs: x86_64-pvh.S (Xen PVH) / x86_64-multiboot.S (GRUB/QEMU).
 *
 * Contract with boot stubs:
 *   - __bss_start / __bss_end: 由 boot stub 在进入 C 代码前清零
 *   - stack_top:               内核栈（位于 .bss 末尾，清零后使用） */

ENTRY(_start)

SECTIONS
{
    . = 1M;

    .note.Xen  : { KEEP(*(.note.Xen)) }    /* Xen PVH ELF note (pvh 模式) */
    .multiboot : { KEEP(*(.multiboot)) }   /* Multiboot v1 header (multiboot 模式) */
    .bootstrap : { *(.bootstrap) }         /* boot stub 32 位入口代码 */
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
