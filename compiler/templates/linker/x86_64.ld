/* Kaula bare-metal linker script for x86_64
 * Loads the kernel at 1M, entry _start (defined in the boot stub).
 * Boot stubs: x86_64-pvh.S (Xen PVH) / x86_64-multiboot.S (GRUB/QEMU).
 *
 * Contract with boot stubs:
 *   - __bss_start / __bss_end: 由 boot stub 在进入 C 代码前清零
 *   - stack_top:               内核栈（位于 .bss 末尾，清零后使用） */

ENTRY(_start)

/* QEMU 加载裸机内核时**只扫程序头**（p_type == PT_NOTE）找 Xen PVH note，
 * 不会去读 section。lld 只在 note 段被显式分配到 PT_NOTE 时才生成该程序头，
 * 所以这里显式声明 PHDRS 并把 .note.Xen 挂上去。
 * 缺了它的症状：QEMU 报 "Error loading uncompressed kernel without PVH ELF Note"。*/
PHDRS
{
    text  PT_LOAD FLAGS(5);   /* R E */
    rodata PT_LOAD FLAGS(4);  /* R   */
    data  PT_LOAD FLAGS(6);   /* RW  */
    note  PT_NOTE FLAGS(4);   /* Xen PVH note */
}

SECTIONS
{
    . = 1M;

    /* 声明了 PHDRS 之后，lld 不再自动把段按 flags 归类 —— 每个输出段都必须
     * 显式挂到某个程序段上（`:text` / `:rodata` / `:data` / `:note`），
     * 否则会得到一堆空的 PT_LOAD，ELF 里"没有 LOAD 段"。
     * note 必须显式挂 `:note`：只声明 PHDRS 而不挂段时，lld 生成的是
     * Offset=0/FileSize=0 的空 PT_NOTE —— 头有了内容没有，QEMU 依旧报
     * "Error loading uncompressed kernel without PVH ELF Note"。*/
    .note.Xen  : { KEEP(*(.note.Xen)) } :note    /* Xen PVH ELF note (pvh 模式) */
    .multiboot : { KEEP(*(.multiboot)) } :text   /* Multiboot v1 header (multiboot 模式) */
    .bootstrap : { *(.bootstrap) } :text         /* boot stub 32 位入口代码 */
    .text      : { *(.text*) } :text
    .rodata    : { *(.rodata*) } :rodata
    .data      : { *(.data*) } :data

    __bss_start = .;
    .bss       : {
        *(COMMON)
        *(.bss*)
        *(.sbss*)
    } :data
    __bss_end = .;

    /* 内核栈：64KB，位于 BSS 末尾 */
    . = ALIGN(16);
    . += 65536;
    stack_top = .;

    /DISCARD/ : { *(.comment) *(.eh_frame*) *(.note.gnu.build-id) }
}
