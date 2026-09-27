// Kaula Freestanding Runtime
// 裸机模式下的最小 C 运行时
//
// 本文件直接复用 freestanding 库（freestanding/）的实现：
//   - memory/memory.c → memset/memcpy/memmove/memcmp（LLVM 对未知大小 builtin
//     的降级调用目标，freestanding 模式下必须自行提供）
//   - string/string.c → strlen 等字符串函数
//   - math/math.c / io/io.c / base/types.c → 其余常用裸机函数
//
// 以 unity build（#include .c）方式包含，保证编译裸机程序时仅需编译本文件
// 即可获得全部 freestanding 库符号；各 .c 自带包含保护与 FS_WEAK 弱符号，
// 与 libc / 独立链接 libkaula_freestanding.a 均不冲突。
//
// 编译时需要 -I <freestanding 目录>（kaulac 自动添加）。

#include "memory/memory.c"
#include "string/string.c"
#include "math/math.c"
#include "io/io.c"
#include "base/types.c"

// ==================== std.memory 的裸机实现 ====================
//
// std/memory/memory.c 是托管实现（依赖 libc <stdlib.h>），裸机不链接 std 模块。
// 但 stdlib.json 登记的 std.memory.kmm_v4_alloc 等符号对 Kaula 程序是公开 API，
// 缺定义就是链接期 undefined symbol（语料里 18 个程序用到）。
// 它们的实体本来就只是 KMM 分配器的一层转发，这里补上裸机版本。
//
// kmm_scoped_allocator_v4.h 把 scope_enter/exit 定义成宏（供内联展开），
// 下面要导出同名函数，必须先 #undef。
#include "kmm_scoped_allocator_v4.h"

#undef kmm_v4_scope_enter
#undef kmm_v4_scope_exit

void* kmm_v4_alloc(size_t size) {
    return kmm_v4_alloc_auto(size);
}

void* kmm_v4_alloc_aligned(size_t size, size_t alignment) {
    if (alignment <= KMM_V4_ALIGNMENT) {
        return kmm_v4_alloc_auto(size);
    }
    if (size == 0 || alignment == 0) return NULL;
    if (size > SIZE_MAX - alignment) return NULL;
    if ((alignment & (alignment - 1)) != 0) return NULL;  // 必须是 2 的幂
    size_t total = size + alignment;
    void* raw = kmm_v4_alloc_auto(total);
    if (raw == NULL) return NULL;
    uintptr_t addr = (uintptr_t)raw;
    uintptr_t aligned_addr = (addr + alignment - 1) & ~((uintptr_t)alignment - 1);
    return (void*)aligned_addr;
}

void* kmm_v4_global_alloc(size_t size) {
    return kmm_v4_alloc_global(size);
}

bool kmm_v4_global_free(void* ptr) {
    return kmm_v4_survivor_free(ptr);
}

void kmm_v4_scope_enter(void) {
    kmm_v4_scope_push();
}

void kmm_v4_scope_exit(void) {
    kmm_v4_scope_pop();
}

// 注意：kmm_v4_bump / kmm_v4_offset_save / kmm_v4_offset_restore /
// kmm_v4_alloc_global / kmm_v4_survivor_free 在 kmm 头里已经是 static inline，
// 头一被包含就有实体，这里不能再定义同名符号（会 redefinition）。

