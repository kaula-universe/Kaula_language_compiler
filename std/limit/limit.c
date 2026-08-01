#include "limit.h"

// 整数类型极值（有符号）: INT8_MIN/MAX 等来自 <limits.h>
i8 limit_max_i8(void)  { return INT8_MAX; }
i16 limit_max_i16(void) { return INT16_MAX; }
i32 limit_max_i32(void) { return INT32_MAX; }
i64 limit_max_i64(void) { return INT64_MAX; }
i8 limit_min_i8(void)  { return INT8_MIN; }
i16 limit_min_i16(void) { return INT16_MIN; }
i32 limit_min_i32(void) { return INT32_MIN; }
i64 limit_min_i64(void) { return INT64_MIN; }

// 整数类型极值（无符号）: UINT8_MAX 等
u8 limit_max_u8(void)  { return UINT8_MAX; }
u16 limit_max_u16(void) { return UINT16_MAX; }
u32 limit_max_u32(void) { return UINT32_MAX; }
u64 limit_max_u64(void) { return UINT64_MAX; }
u8 limit_min_u8(void)  { return 0; }
u16 limit_min_u16(void) { return 0; }
u32 limit_min_u32(void) { return 0; }
u64 limit_min_u64(void) { return 0; }

// 浮点极限值（正数方向）: FLT_MAX/DBL_MAX 与 FLT_MIN/DBL_MIN 来自 <float.h>
//   limit_max = 最大正值 (接近 +∞),  limit_min = 最小正数 (接近 +0)
f32 limit_max_f32(void) { return FLT_MAX; }
f64 limit_max_f64(void) { return DBL_MAX; }
f32 limit_min_f32(void) { return FLT_MIN; }
f64 limit_min_f64(void) { return DBL_MIN; }

// 浮点负数方向极值: 与正数方向对称
//   neg_max = 最大负数 (接近 -0),  neg_min = 最负值 (接近 -∞)
f32 neg_max_f32(void) { return -FLT_MIN; }
f64 neg_max_f64(void) { return -DBL_MIN; }
f32 neg_min_f32(void) { return -FLT_MAX; }
f64 neg_min_f64(void) { return -DBL_MAX; }
