/* Kaula bare-metal linker script for riscv64
 * Loads the kernel at 0x80000000 (QEMU virt 惯例，M-mode 直接引导，-bios none）。
 * Boot stub: riscv64.S（通用入口，无 bootloader 协议）。
 *
 * Contract with boot stubs:
 *   - __bss_start / __bss_end: 由 boot stub 在进入 C 代码前清零
 *   - stack_top:               内核栈（位于 .bss 末尾，清零后使用） */

ENTRY(_start)

SECTIONS
{
    . = 0x80000000;

    .text      : { KEEP(*(.text.boot)) *(.text*) }
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
