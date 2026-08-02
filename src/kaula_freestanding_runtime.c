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
