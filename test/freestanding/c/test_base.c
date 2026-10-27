/* test_base.c — freestanding.base 全部 29 个函数 */
#include "fs_test.h"

void suite_base(void) {
    /* ---------------- 饱和转换 to_i8 / to_i16 / to_i32 / to_i64 ---------------- */
    FS_FN("base", "to_i8");
    CHECK_INT(to_i8(0), 0, "to_i8(0)");
    CHECK_INT(to_i8(127), 127, "to_i8(127)");
    CHECK_INT(to_i8(128), 127, "to_i8(128) saturate high");
    CHECK_INT(to_i8(1000), 127, "to_i8(1000) saturate high");
    CHECK_INT(to_i8(-1), -1, "to_i8(-1)");
    CHECK_INT(to_i8(-128), -128, "to_i8(-128)");
    CHECK_INT(to_i8(-129), -128, "to_i8(-129) saturate low");
    CHECK_INT(to_i8(9223372036854775807LL), 127, "to_i8(INT64_MAX) saturate");
    CHECK_INT(to_i8(-9223372036854775807LL - 1), -128, "to_i8(INT64_MIN) saturate");

    FS_FN("base", "to_i16");
    CHECK_INT(to_i16(0), 0, "to_i16(0)");
    CHECK_INT(to_i16(32767), 32767, "to_i16(32767)");
    CHECK_INT(to_i16(32768), 32767, "to_i16(32768) saturate high");
    CHECK_INT(to_i16(-32768), -32768, "to_i16(-32768)");
    CHECK_INT(to_i16(-32769), -32768, "to_i16(-32769) saturate low");
    CHECK_INT(to_i16(2147483647), 32767, "to_i16(INT32_MAX) saturate");

    FS_FN("base", "to_i32");
    CHECK_INT(to_i32(0), 0, "to_i32(0)");
    CHECK_INT(to_i32(2147483647), 2147483647, "to_i32(INT32_MAX)");
    CHECK_INT(to_i32(2147483648LL), 2147483647, "to_i32(INT32_MAX+1) saturate");
    CHECK_INT(to_i32(-2147483648LL), -2147483648, "to_i32(INT32_MIN)");
    CHECK_INT(to_i32(-2147483649LL), -2147483648, "to_i32(INT32_MIN-1) saturate");
    CHECK_INT(to_i32(9223372036854775807LL), 2147483647, "to_i32(INT64_MAX) saturate");

    FS_FN("base", "to_i64");
    CHECK_INT(to_i64(0), 0, "to_i64(0)");
    CHECK_INT(to_i64(123456789), 123456789, "to_i64(123456789)");
    CHECK_INT(to_i64(9223372036854775807LL), 9223372036854775807LL, "to_i64(INT64_MAX) identity");
    CHECK_INT(to_i64(-9223372036854775807LL - 1), -9223372036854775807LL - 1, "to_i64(INT64_MIN) identity");

    /* ---------------- 无符号饱和转换 to_u8 / to_u16 / to_u32 / to_u64 ---------------- */
    FS_FN("base", "to_u8");
    CHECK_UINT(to_u8(0), 0, "to_u8(0)");
    CHECK_UINT(to_u8(255), 255, "to_u8(255)");
    CHECK_UINT(to_u8(256), 255, "to_u8(256) saturate");
    CHECK_UINT(to_u8(4096), 255, "to_u8(4096) saturate");

    FS_FN("base", "to_u16");
    CHECK_UINT(to_u16(0), 0, "to_u16(0)");
    CHECK_UINT(to_u16(65535), 65535, "to_u16(65535)");
    CHECK_UINT(to_u16(65536), 65535, "to_u16(65536) saturate");

    FS_FN("base", "to_u32");
    CHECK_UINT(to_u32(0), 0, "to_u32(0)");
    CHECK_UINT(to_u32(4294967295ULL), 4294967295ULL, "to_u32(U32_MAX)");
    CHECK_UINT(to_u32(4294967296ULL), 4294967295ULL, "to_u32(U32_MAX+1) saturate");

    FS_FN("base", "to_u64");
    CHECK_UINT(to_u64(0), 0, "to_u64(0)");
    CHECK_UINT(to_u64(12345678901234567890ULL), 12345678901234567890ULL, "to_u64 large identity");
    CHECK_UINT(to_u64(18446744073709551615ULL), 18446744073709551615ULL, "to_u64(U64_MAX) identity");

    /* ---------------- 浮点/布尔/字符转换 ---------------- */
    FS_FN("base", "to_f32");
    CHECK_F64(to_f32(1.5), 1.5, 1e-9, "to_f32(1.5)");
    CHECK_F64(to_f32(-0.25), -0.25, 1e-9, "to_f32(-0.25)");
    CHECK_F64(to_f32(0.0), 0.0, 1e-9, "to_f32(0.0)");

    FS_FN("base", "to_f64");
    CHECK_F64(to_f64(1.0), 1.0, 0, "to_f64(1.0)");
    CHECK_F64(to_f64(-12345.678), -12345.678, 0, "to_f64(-12345.678) identity");
    CHECK_F64(to_f64(0.1), 0.1, 0, "to_f64(0.1) identity");

    FS_FN("base", "to_bool");
    CHECK(to_bool(0) == false, "to_bool(0) == false");
    CHECK(to_bool(1) == true, "to_bool(1) == true");
    CHECK(to_bool(-5) == true, "to_bool(-5) == true");
    CHECK(to_bool(256) == true, "to_bool(256) == true");

    FS_FN("base", "to_char");
    CHECK_INT(to_char(65), 65, "to_char(65) == 'A'");
    CHECK_INT(to_char(321), 65, "to_char(321) truncates to 'A'");
    CHECK_INT(to_char(0), 0, "to_char(0)");

    /* ---------------- 比较函数（有符号/无符号/浮点/布尔/字符） ---------------- */
    FS_FN("base", "compare_i8");
    CHECK_INT(compare_i8(1, 2), -1, "compare_i8(1,2) < 0");
    CHECK_INT(compare_i8(2, 1), 1, "compare_i8(2,1) > 0");
    CHECK_INT(compare_i8(-5, -5), 0, "compare_i8(-5,-5) == 0");
    CHECK_INT(compare_i8(-128, 127), -1, "compare_i8(-128,127) < 0");

    FS_FN("base", "compare_i16");
    CHECK_INT(compare_i16(1, 2), -1, "compare_i16(1,2)");
    CHECK_INT(compare_i16(2, 1), 1, "compare_i16(2,1)");
    CHECK_INT(compare_i16(-32768, 32767), -1, "compare_i16(-32768,32767)");

    FS_FN("base", "compare_i32");
    CHECK_INT(compare_i32(1, 2), -1, "compare_i32(1,2)");
    CHECK_INT(compare_i32(2147483647, -2147483647 - 1), 1, "compare_i32(INT32_MAX,INT32_MIN)");
    CHECK_INT(compare_i32(7, 7), 0, "compare_i32(7,7)");

    FS_FN("base", "compare_i64");
    CHECK_INT(compare_i64(1, 2), -1, "compare_i64(1,2)");
    CHECK_INT(compare_i64(9223372036854775807LL, -9223372036854775807LL - 1), 1, "compare_i64(MAX,MIN)");
    CHECK_INT(compare_i64(0, 0), 0, "compare_i64(0,0)");

    FS_FN("base", "compare_u8");
    CHECK_INT(compare_u8(1, 255), -1, "compare_u8(1,255)");
    CHECK_INT(compare_u8(255, 1), 1, "compare_u8(255,1)");
    CHECK_INT(compare_u8(0, 0), 0, "compare_u8(0,0)");

    FS_FN("base", "compare_u16");
    CHECK_INT(compare_u16(1, 65535), -1, "compare_u16(1,65535)");
    CHECK_INT(compare_u16(65535, 65535), 0, "compare_u16(65535,65535)");

    FS_FN("base", "compare_u32");
    CHECK_INT(compare_u32(1, 4294967295ULL), -1, "compare_u32(1,U32_MAX)");
    CHECK_INT(compare_u32(4294967295ULL, 1), 1, "compare_u32(U32_MAX,1)");

    FS_FN("base", "compare_u64");
    CHECK_INT(compare_u64(1, 18446744073709551615ULL), -1, "compare_u64(1,U64_MAX)");
    CHECK_INT(compare_u64(18446744073709551615ULL, 0), 1, "compare_u64(U64_MAX,0)");
    CHECK_INT(compare_u64(9, 9), 0, "compare_u64(9,9)");

    FS_FN("base", "compare_f32");
    CHECK_INT(compare_f32(1.0f, 2.0f), -1, "compare_f32(1,2)");
    CHECK_INT(compare_f32(2.0f, 1.0f), 1, "compare_f32(2,1)");
    CHECK_INT(compare_f32(-0.5f, -0.5f), 0, "compare_f32(-0.5,-0.5)");

    FS_FN("base", "compare_f64");
    CHECK_INT(compare_f64(1.0, 2.0), -1, "compare_f64(1,2)");
    CHECK_INT(compare_f64(2.0, 1.0), 1, "compare_f64(2,1)");
    CHECK_INT(compare_f64(0.1, 0.1), 0, "compare_f64(0.1,0.1)");

    FS_FN("base", "compare_bool");
    CHECK_INT(compare_bool(false, true), -1, "compare_bool(false,true)");
    CHECK_INT(compare_bool(true, false), 1, "compare_bool(true,false)");
    CHECK_INT(compare_bool(true, true), 0, "compare_bool(true,true)");

    FS_FN("base", "compare_char");
    CHECK_INT(compare_char('a', 'b'), -1, "compare_char('a','b')");
    CHECK_INT(compare_char('b', 'a'), 1, "compare_char('b','a')");
    CHECK_INT(compare_char('A', 'A'), 0, "compare_char('A','A')");
    /* 有符号 char: 0x80 < 0 */
    CHECK_INT(compare_char((char)0x80, (char)0x01) < 0 ? 1 : 0, 1, "compare_char(0x80,0x01) signed");

    /* ---------------- 类型检查 ---------------- */
    FS_FN("base", "is_integer");
    CHECK(is_integer(0) == true, "is_integer(0)");
    CHECK(is_integer(-1) == true, "is_integer(-1)");
    CHECK(is_integer(9223372036854775807LL) == true, "is_integer(INT64_MAX)");

    FS_FN("base", "is_unsigned");
    CHECK(is_unsigned(0) == true, "is_unsigned(0)");
    CHECK(is_unsigned(18446744073709551615ULL) == true, "is_unsigned(U64_MAX)");

    FS_FN("base", "is_float");
    CHECK(is_float(0.0) == true, "is_float(0.0)");
    CHECK(is_float(-1.5e300) == true, "is_float(-1.5e300)");

    FS_FN("base", "is_bool");
    CHECK(is_bool(false) == true, "is_bool(false)");
    CHECK(is_bool(true) == true, "is_bool(true)");

    FS_FN("base", "is_char");
    CHECK(is_char('x') == true, "is_char('x')");
    CHECK(is_char(0) == true, "is_char(0)");
}
