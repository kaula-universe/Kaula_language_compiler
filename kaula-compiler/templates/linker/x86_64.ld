/* Kaula bare-metal linker script for x86_64 (PVH/multiboot ELF)
 * Loads the kernel at 1M, entry _start (defined in the boot stub). */

ENTRY(_start)

SECTIONS
{
    . = 1M;

    .note.Xen : { *(.note.Xen) }
    .bootstrap : { *(.bootstrap) }
    .text : { *(.text*) }
    .rodata : { *(.rodata*) }
    .data : { *(.data*) }
    .bss : {
        *(COMMON)
        *(.bss*)
    }
}
