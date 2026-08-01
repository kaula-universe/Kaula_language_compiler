#ifndef STD_LIMIT_LIMIT_H
#define STD_LIMIT_LIMIT_H

#include "../base/types.h"
#include <limits.h>
#include <float.h>

// 整数类型极值（有符号）
i8 limit_max_i8(void);
i16 limit_max_i16(void);
i32 limit_max_i32(void);
i64 limit_max_i64(void);
i8 limit_min_i8(void);
i16 limit_min_i16(void);
i32 limit_min_i32(void);
i64 limit_min_i64(void);

// 整数类型极值（无符号）
u8 limit_max_u8(void);
u16 limit_max_u16(void);
u32 limit_max_u32(void);
u64 limit_max_u64(void);
u8 limit_min_u8(void);
u16 limit_min_u16(void);
u32 limit_min_u32(void);
u64 limit_min_u64(void);

// 浮点极限值（正数方向）
//   limit_max = 最大正值 (接近 +∞),  limit_min = 最小正数 (接近 +0)
f32 limit_max_f32(void);
f64 limit_max_f64(void);
f32 limit_min_f32(void);
f64 limit_min_f64(void);

// 浮点负数方向极值 (neg = negative)
//   neg_max = 最大负数 (接近 -0),  neg_min = 最负值 (接近 -∞)
f32 neg_max_f32(void);
f64 neg_max_f64(void);
f32 neg_min_f32(void);
f64 neg_min_f64(void);

#endif // STD_LIMIT_LIMIT_H
