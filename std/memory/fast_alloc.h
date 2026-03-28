#ifndef FAST_ALLOC_H
#define FAST_ALLOC_H

#include <stddef.h>

#ifdef __cplusplus
extern "C" {
#endif

/**
 * 初始化快速分配器
 * 应该在程序启动时调用一次
 */
void fast_allocator_init(void);

/**
 * 快速分配函数
 * @param size 分配的字节数
 * @return 分配的内存指针，失败返回 NULL
 */
void* fast_alloc(size_t size);

/**
 * 快速批量分配函数（清零）
 * @param num 元素个数
 * @param size 每个元素的大小
 * @return 分配的内存指针，失败返回 NULL
 */
void* fast_calloc(size_t num, size_t size);

/**
 * 释放所有快速分配的内存
 * 注意：这个函数会释放所有通过 fast_alloc 分配的内存
 */
void fast_free_all(void);

#ifdef __cplusplus
}
#endif

#endif // FAST_ALLOC_H
