#ifndef KAULA_FREESTANDING_H
#define KAULA_FREESTANDING_H

// Kaula Freestanding 库 — 与 std 同级别的无依赖标准库
//
// 设计目标:
//   - 与 std 相同的调用方式:  import freestanding.memory / freestanding.string ...
//   - 与 std 相同的处理逻辑:  同名函数行为一致（memset/strlen/print/println ...）
//   - 无任何依赖:             不使用 libc/OS 头文件，仅依赖编译器自带的
//                             <stdint.h>/<stddef.h>/<stdbool.h> 等 freestanding 头
//   - 双环境可跑:             裸机（-ffreestanding -nostdlib）与托管 OS 均可编译运行
//   - 所有符号均为弱符号 (FS_WEAK)，托管模式下与 libc / kaula_runtime 共存不冲突
//   - kaula_freestanding_runtime.c 直接复用本库实现（unity include）

#include "base/types.h"
#include "memory/memory.h"
#include "string/string.h"
#include "math/math.h"
#include "io/io.h"

#endif /* KAULA_FREESTANDING_H */
