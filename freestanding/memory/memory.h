#ifndef KAULA_FREESTANDING_MEMORY_H
#define KAULA_FREESTANDING_MEMORY_H

// freestanding/memory/memory.h — 内存模块
// 与 std.memory 同级，但无任何依赖：
//   - memset/memcpy/memmove/memcmp 裸机实现（LLVM 对未知大小 builtin 的降级调用目标）
//   - fs_alloc 系列：BSS 静态池 bump 分配器（裸机与 OS 均可运行，无 malloc 依赖）
//
// 托管模式（OS）下这些符号均为弱符号，libc 的同名强符号优先；
// 裸机模式（-nostdlib）下本模块实现被直接链接使用。

#include <stddef.h>
#include <stdint.h>
#include "../base/types.h"

/* ============================================================================
 * 内存操作函数（C 标准语义，与 libc / kaula_freestanding_runtime 一致）
 * ============================================================================ */

/**
 * memset - 用字节值 c 填充内存
 * @dst: 目标地址
 * @c:   填充字节值（int 低 8 位）
 * @n:   字节数
 * 返回: dst
 */
extern void* memset(void* dst, int c, size_t n);

/**
 * memcpy - 拷贝内存（不处理重叠）
 * @dst: 目标地址
 * @src: 源地址
 * @n:   字节数
 * 返回: dst
 */
extern void* memcpy(void* restrict dst, const void* restrict src, size_t n);

/**
 * memmove - 拷贝内存（安全处理重叠）
 * @dst: 目标地址
 * @src: 源地址
 * @n:   字节数
 * 返回: dst
 */
extern void* memmove(void* dst, const void* src, size_t n);

/**
 * memcmp - 比较两块内存
 * @a: 第一块内存
 * @b: 第二块内存
 * @n: 字节数
 * 返回: <0 / 0 / >0
 */
extern int memcmp(const void* a, const void* b, size_t n);

/* ============================================================================
 * fs_alloc 静态池 bump 分配器（裸机零依赖分配器）
 *
 * 内存来源: BSS 段静态池（大小由 FS_ALLOC_POOL_SIZE 宏控制，默认 64KB）
 * 特点:     bump 分配 + 整体重置，无 free list，无碎片，无系统调用
 * 注意:     fs_free 为空操作，内存通过 fs_alloc_reset 批量回收
 * ============================================================================ */

#ifndef FS_ALLOC_POOL_SIZE
#define FS_ALLOC_POOL_SIZE (64 * 1024) // 64KB，可用 -DFS_ALLOC_POOL_SIZE=xxx 覆盖
#endif

/**
 * fs_alloc - 从静态池分配内存
 * @size: 字节数
 * 返回: 8 字节对齐指针；池满返回 NULL
 */
extern void* fs_alloc(size_t size);

/**
 * fs_calloc - 从静态池分配并清零
 * @count: 元素个数
 * @size:  元素大小
 * 返回: 清零后的指针；失败返回 NULL
 */
extern void* fs_calloc(size_t count, size_t size);

/**
 * fs_free - 释放（bump 分配器为空操作）
 * @ptr: 无意义，可传 NULL
 */
extern void fs_free(void* ptr);

/**
 * fs_alloc_reset - 重置分配器（回收全部内存）
 * 警告: 重置后之前所有分配均失效
 */
extern void fs_alloc_reset(void);

/**
 * fs_alloc_usage - 已分配字节数
 */
extern size_t fs_alloc_usage(void);

/**
 * fs_alloc_available - 剩余可用字节数
 */
extern size_t fs_alloc_available(void);

/**
 * fs_alloc_capacity - 池总容量（字节）
 */
extern size_t fs_alloc_capacity(void);

#endif /* KAULA_FREESTANDING_MEMORY_H */
