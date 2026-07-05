/**
 * std/memory/memory.h - Kaula 标准库内存模块
 *
 * 提供 KMM V4 分配器的高级接口，替代 std_malloc
 * KMM V4 是一个作用域感知的 bump allocator，比 malloc/free 快 5-10x
 *
 * 使用方式:
 *   import std.memory
 *   let ptr = std.memory.kmm_v4_alloc(1024)
 *   // ... 使用 ptr ...
 *   // 作用域退出时自动回收，无需手动 free
 *
 * 性能对比:
 *   - kmm_v4_alloc: ~10ns (bump pointer)
 *   - std_malloc:   ~50-100ns (系统 malloc)
 *   - 快 5-10x
 */

#ifndef KAULA_STD_MEMORY_H
#define KAULA_STD_MEMORY_H

#include <stddef.h>
#include <stdint.h>

/* ============================================================================
 * KMM V4 核心分配函数
 * ============================================================================ */

/**
 * kmm_v4_alloc - 从 KMM V4 池中分配内存
 * @size: 要分配的字节数
 *
 * 返回: 分配的内存指针，失败返回 NULL
 *
 * 特点:
 *   - 从线程本地 bump pool 分配，无需系统调用
 *   - 分配的内存在作用域退出时自动回收
 *   - 不支持单独 free（bump allocator 设计）
 *   - 8 字节对齐
 *
 * 示例:
 *   int* arr = kmm_v4_alloc(100 * sizeof(int));
 */
void* kmm_v4_alloc(size_t size);

/**
 * kmm_v4_calloc - 从 KMM V4 池中分配并清零内存
 * @count: 元素数量
 * @size: 每个元素的字节数
 *
 * 返回: 分配的内存指针（已清零），失败返回 NULL
 *
 * 示例:
 *   int* arr = kmm_v4_calloc(100, sizeof(int));  // 分配 100 个 int，全部为 0
 */
void* kmm_v4_calloc(size_t count, size_t size);

/**
 * kmm_v4_realloc - 重新分配内存（有限支持）
 * @ptr: 原内存指针
 * @new_size: 新的大小
 *
 * 返回: 新的内存指针，失败返回 NULL
 *
 * 注意: bump allocator 不支持真正的 realloc
 * 如果 new_size > old_size，会分配新内存并复制
 * 如果 new_size <= old_size，返回原指针（不缩小）
 */
void* kmm_v4_realloc(void* ptr, size_t new_size);

/**
 * kmm_v4_free - 释放内存（KMM V4 中为空操作）
 * @ptr: 要释放的内存指针
 *
 * 注意: KMM V4 使用 bump allocator，内存通过作用域退出批量回收
 * 此函数存在是为了 API 兼容性，实际上什么都不做
 *
 * 你不需要调用此函数 - 作用域退出时会自动回收
 */
void kmm_v4_free(void* ptr);

/**
 * kmm_v4_malloc - kmm_v4_alloc 的别名（兼容旧 API）
 * @size: 要分配的字节数
 *
 * 等价于 kmm_v4_alloc
 */
void* kmm_v4_malloc(size_t size);

/* ============================================================================
 * 对齐分配
 * ============================================================================ */

/**
 * kmm_v4_alloc_aligned - 分配指定对齐的内存
 * @size: 要分配的字节数
 * @alignment: 对齐要求（必须是 2 的幂）
 *
 * 返回: 对齐的内存指针，失败返回 NULL
 *
 * 示例:
 *   // 分配 64 字节对齐的内存（用于 SIMD）
 *   void* buf = kmm_v4_alloc_aligned(1024, 64);
 */
void* kmm_v4_alloc_aligned(size_t size, size_t alignment);

/* ============================================================================
 * 作用域管理
 * ============================================================================ */

/**
 * kmm_v4_scope_enter - 进入新的内存作用域
 *
 * 保存当前分配偏移量，之后的所有分配都在这个作用域内
 * 作用域退出时，该作用域内分配的所有内存会被自动回收
 *
 * 典型用法（由 KMM_V4_SCOPE_START/END 宏自动管理）:
 *   KMM_V4_SCOPE_START {
 *       void* p1 = kmm_v4_alloc(100);
 *       void* p2 = kmm_v4_alloc(200);
 *       // ... 使用 p1, p2 ...
 *   } KMM_V4_SCOPE_END;
 *   // p1, p2 指向的内存已自动回收
 */
void kmm_v4_scope_enter(void);

/**
 * kmm_v4_scope_exit - 退出当前内存作用域
 *
 * 回收该作用域内分配的所有内存
 * 必须与 kmm_v4_scope_enter 配对使用
 */
void kmm_v4_scope_exit(void);

/* ============================================================================
 * 状态查询
 * ============================================================================ */

/**
 * kmm_v4_usage - 查询当前内存使用量
 *
 * 返回: 已使用的字节数
 */
size_t kmm_v4_usage(void);

/**
 * kmm_v4_available - 查询剩余可用内存
 *
 * 返回: 剩余可用的字节数
 */
size_t kmm_v4_available(void);

/**
 * kmm_v4_capacity - 查询总池容量
 *
 * 返回: 总池大小（字节）
 */
size_t kmm_v4_capacity(void);

/**
 * kmm_v4_reset - 重置分配器（回收所有内存）
 *
 * 警告: 这会重置整个分配器，所有之前的分配都将失效
 * 仅在确定不再需要任何已分配内存时使用
 */
void kmm_v4_reset(void);

/* ============================================================================
 * 类型安全的便捷宏
 * ============================================================================ */

/**
 * KMM_V4_ALLOC(type) - 类型安全的单对象分配
 *
 * 示例:
 *   int* p = KMM_V4_ALLOC(int);           // 分配 1 个 int
 *   Point* pt = KMM_V4_ALLOC(Point);      // 分配 1 个 Point
 */
#define KMM_V4_ALLOC(type) \
    ((type*)kmm_v4_alloc(sizeof(type)))

/**
 * KMM_V4_ALLOC_ZERO(type) - 类型安全的零初始化分配
 *
 * 示例:
 *   int* p = KMM_V4_ALLOC_ZERO(int);      // 分配 1 个 int，初始化为 0
 *   Point* pt = KMM_V4_ALLOC_ZERO(Point); // 分配 1 个 Point，所有字段为 0
 */
#define KMM_V4_ALLOC_ZERO(type) \
    ((type*)kmm_v4_calloc(1, sizeof(type)))

/**
 * KMM_V4_ALLOC_ARRAY(type, count) - 类型安全的数组分配
 *
 * 示例:
 *   int* arr = KMM_V4_ALLOC_ARRAY(int, 100);       // 分配 100 个 int
 *   Point* pts = KMM_V4_ALLOC_ARRAY(Point, 10);    // 分配 10 个 Point
 */
#define KMM_V4_ALLOC_ARRAY(type, count) \
    ((type*)kmm_v4_alloc(sizeof(type) * (count)))

/**
 * KMM_V4_ALLOC_ARRAY_ZERO(type, count) - 零初始化的数组分配
 *
 * 示例:
 *   int* arr = KMM_V4_ALLOC_ARRAY_ZERO(int, 100);  // 分配 100 个 int，全部为 0
 */
#define KMM_V4_ALLOC_ARRAY_ZERO(type, count) \
    ((type*)kmm_v4_calloc(count, sizeof(type)))

/* ============================================================================
 * 与 std_malloc 的对比 API
 * ============================================================================ */

/**
 * std_malloc - 系统 malloc 的包装（兼容旧代码）
 * @size: 要分配的字节数
 *
 * 返回: 分配的内存指针，失败返回 NULL
 *
 * 注意: 对于新代码，建议使用 kmm_v4_alloc 替代
 * std_malloc 使用系统 malloc，每次分配/释放都有系统调用开销
 * kmm_v4_alloc 使用 bump allocator，快 5-10x
 */
void* std_malloc(size_t size);

/**
 * std_free - 系统 free 的包装（兼容旧代码）
 * @ptr: 要释放的内存指针
 *
 * 注意: 对于新代码，使用 kmm_v4_alloc 时不需要调用 std_free
 * 只有通过 std_malloc 分配的内存才需要 std_free
 */
void std_free(void* ptr);

/* ============================================================================
 * SOR 所有权操作
 * ============================================================================ */

/**
 * std_yeide - 所有权转移（SOR 语义）
 * @src: 源所有权指针
 * @dst: 目标指针
 *
 * 返回: dst 指针
 *
 * 编译器在释放模式下展开为简单的赋值+清零
 * src 失效，dst 获得所有权
 */
void* std_yeide(void* src, void* dst);

/**
 * std_release - 所有权分发（SOR 语义）
 * @src: 源所有权指针
 *
 * 返回: src 指针（供后续访问）
 *
 * 将只读访问权分发给多个持有者
 * 编译器在释放模式下展开为零开销操作
 */
void* std_release(void* src);

/* ============================================================================
 * 快速分配器（兼容旧 API）
 * ============================================================================ */

/**
 * fast_alloc - 快速分配（基于栈的 bump allocator）
 * @size: 要分配的字节数
 *
 * 返回: 分配的内存指针
 *
 * 注意: 这是一个更早期的分配器，建议使用 kmm_v4_alloc 替代
 */
void* fast_alloc(size_t size);

/**
 * fast_calloc - 快速零初始化分配
 * @num: 元素数量
 * @size: 每个元素的字节数
 *
 * 返回: 分配的内存指针（已清零）
 */
void* fast_calloc(size_t num, size_t size);

/**
 * fast_free - 快速释放
 * @ptr: 要释放的内存指针
 */
void fast_free(void* ptr);

/* ============================================================================
 * 作用域分配（兼容旧 API）
 * ============================================================================ */

/**
 * kaula_scope_enter - 进入作用域（兼容旧 API）
 *
 * 等价于 kmm_v4_scope_enter
 */
void kaula_scope_enter(void);

/**
 * kaula_scope_exit - 退出作用域（兼容旧 API）
 *
 * 等价于 kmm_v4_scope_exit
 */
void kaula_scope_exit(void);

/**
 * kaula_scope_alloc - 作用域内分配（兼容旧 API）
 * @size: 要分配的字节数
 *
 * 等价于 kmm_v4_alloc
 */
void* kaula_scope_alloc(size_t size);

/**
 * kaula_scope_free - 作用域内释放（兼容旧 API）
 * @ptr: 要释放的内存指针
 *
 * 等价于 kmm_v4_free（空操作）
 */
void kaula_scope_free(void* ptr);

#endif /* KAULA_STD_MEMORY_H */
