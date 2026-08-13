/* Kaula 用户态程序链接脚本：加载地址 0x40000000（内核约定的用户区）
   各段 2MB 对齐：内核加载器以 2MB 大页映射 */
ENTRY(user_start)
SECTIONS {
    . = 0x40000000;
    .text : ALIGN(0x200000) { *(.text*) }
    .rodata : ALIGN(0x200000) { *(.rodata*) }
    .data : ALIGN(0x200000) { *(.data*) }
    __bss_start = .;
    .bss : ALIGN(0x200000) { *(.bss*) *(COMMON) }
    __bss_end = .;
}
