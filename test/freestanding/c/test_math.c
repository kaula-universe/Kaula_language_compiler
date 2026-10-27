/* test_math.c — freestanding.math 全部 35 个函数 */
#include "fs_test.h"

void suite_math(void) {
    /* ==================== 浮点基础 ==================== */
    FS_FN("math", "math_abs");
    CHECK_F64(math_abs(3.5), 3.5, 0, "math_abs(3.5)");
    CHECK_F64(math_abs(-3.5), 3.5, 0, "math_abs(-3.5)");
    CHECK_F64(math_abs(0.0), 0.0, 0, "math_abs(0.0)");
    CHECK_F64(math_abs(-0.0), 0.0, 0, "math_abs(-0.0) clears sign");
    CHECK_F64(math_abs(-1e300), 1e300, 0, "math_abs(-1e300)");

    FS_FN("math", "math_fabs");
    CHECK_F64(math_fabs(2.25), 2.25, 0, "math_fabs(2.25)");
    CHECK_F64(math_fabs(-2.25), 2.25, 0, "math_fabs(-2.25)");
    CHECK_F64(math_fabs(-0.0), 0.0, 0, "math_fabs(-0.0)");

    FS_FN("math", "math_min");
    CHECK_F64(math_min(1.0, 2.0), 1.0, 0, "math_min(1,2)");
    CHECK_F64(math_min(2.0, 1.0), 1.0, 0, "math_min(2,1)");
    CHECK_F64(math_min(-5.0, 5.0), -5.0, 0, "math_min(-5,5)");
    CHECK_F64(math_min(1.0, 1.0), 1.0, 0, "math_min equal");
    CHECK(math_isnan(math_min(fs_nan(), 3.0)) == false, "math_min(NaN,3) == 3 (NaN-propagating to arg)");

    FS_FN("math", "math_max");
    CHECK_F64(math_max(1.0, 2.0), 2.0, 0, "math_max(1,2)");
    CHECK_F64(math_max(2.0, 1.0), 2.0, 0, "math_max(2,1)");
    CHECK_F64(math_max(-5.0, 5.0), 5.0, 0, "math_max(-5,5)");
    CHECK(math_isnan(math_max(fs_nan(), 3.0)) == false, "math_max(NaN,3) == 3");

    FS_FN("math", "math_signbit");
    CHECK_INT(math_signbit(1.0), 0, "math_signbit(+1.0) == 0");
    CHECK_INT(math_signbit(-1.0), 1, "math_signbit(-1.0) == 1");
    CHECK_INT(math_signbit(0.0), 0, "math_signbit(+0.0) == 0");
    CHECK_INT(math_signbit(-0.0), 1, "math_signbit(-0.0) == 1");
    CHECK_INT(math_signbit(fs_nan()), 0, "math_signbit(+NaN) == 0");

    FS_FN("math", "math_isnan");
    CHECK(math_isnan(fs_nan()) == true, "math_isnan(NaN)");
    CHECK(math_isnan(-fs_nan()) == true, "math_isnan(-NaN)");
    CHECK(math_isnan(0.0 / 0.0) == true, "math_isnan(0.0/0.0)");
    CHECK(math_isnan(1.0) == false, "math_isnan(1.0) false");
    CHECK(math_isnan(0.0) == false, "math_isnan(0.0) false");
    /* 1e308 * 10 → +inf, 不是 NaN */
    CHECK(math_isnan(1e308 * 10) == false, "overflow to inf is not NaN");

    FS_FN("math", "math_isinf");
    CHECK(math_isinf(1e308 * 10) == true, "math_isinf(+inf)");
    CHECK(math_isinf(-(1e308 * 10)) == true, "math_isinf(-inf)");
    CHECK(math_isinf(1.0) == false, "math_isinf(1.0) false");
    CHECK(math_isinf(fs_nan()) == false, "math_isinf(NaN) false");
    CHECK(math_isinf(1e308) == false, "math_isinf(1e308) false");

    FS_FN("math", "math_isfinite");
    CHECK(math_isfinite(1.0) == true, "math_isfinite(1.0)");
    CHECK(math_isfinite(0.0) == true, "math_isfinite(0.0)");
    CHECK(math_isfinite(1e308) == true, "math_isfinite(1e308)");
    CHECK(math_isfinite(fs_nan()) == false, "math_isfinite(NaN) false");
    CHECK(math_isfinite(1e308 * 10) == false, "math_isfinite(inf) false");

    FS_FN("math", "math_floor");
    CHECK_F64(math_floor(1.5), 1.0, 0, "math_floor(1.5)");
    CHECK_F64(math_floor(-1.5), -2.0, 0, "math_floor(-1.5)");
    CHECK_F64(math_floor(2.0), 2.0, 0, "math_floor(2.0)");
    CHECK_F64(math_floor(0.5), 0.0, 0, "math_floor(0.5)");
    CHECK_F64(math_floor(-0.5), -1.0, 0, "math_floor(-0.5)");
    CHECK_F64(math_floor(0.0), 0.0, 0, "math_floor(0.0)");
    CHECK_F64(math_floor(-0.0), -0.0, 0, "math_floor(-0.0)");
    CHECK(math_isnan(math_floor(fs_nan())), "math_floor(NaN) -> NaN");
    CHECK(math_isinf(math_floor(1e308 * 10)) && math_floor(1e308 * 10) > 0, "math_floor(+inf) -> +inf");
    /* 超出 int64 表示范围的有限值: 强转 int64 是 UB, 实现必须显式夹取 */
    CHECK_F64(math_floor(1e19), 1e19, 0, "math_floor(1e19) == 1e19 (beyond int64 range)");
    CHECK_F64(math_floor(-1e19), -1e19, 0, "math_floor(-1e19) == -1e19 (beyond int64 range)");
    CHECK_F64(math_floor(1e300), 1e300, 0, "math_floor(1e300) == 1e300 (beyond int64 range)");
    CHECK_F64(math_floor(9.2233720368547758e18), 9.2233720368547758e18, 0,
              "math_floor(2^63-1ulp) stays put");
    CHECK_F64(math_floor(4503599627370496.0), 4503599627370496.0, 0, "math_floor(2^52) exact");

    FS_FN("math", "math_ceil");
    CHECK_F64(math_ceil(1.5), 2.0, 0, "math_ceil(1.5)");
    CHECK_F64(math_ceil(-1.5), -1.0, 0, "math_ceil(-1.5)");
    CHECK_F64(math_ceil(2.0), 2.0, 0, "math_ceil(2.0)");
    CHECK_F64(math_ceil(0.5), 1.0, 0, "math_ceil(0.5)");
    CHECK_F64(math_ceil(-0.5), -0.0, 0, "math_ceil(-0.5) == -0.0");
    CHECK_F64(math_ceil(0.0), 0.0, 0, "math_ceil(0.0)");
    CHECK(math_isnan(math_ceil(fs_nan())), "math_ceil(NaN) -> NaN");
    CHECK(math_isinf(math_ceil(-(1e308 * 10))) && math_ceil(-(1e308 * 10)) < 0, "math_ceil(-inf) -> -inf");
    CHECK_F64(math_ceil(1e19), 1e19, 0, "math_ceil(1e19) == 1e19 (beyond int64 range)");
    CHECK_F64(math_ceil(-1e19), -1e19, 0, "math_ceil(-1e19) == -1e19 (beyond int64 range)");
    CHECK_F64(math_ceil(1e300), 1e300, 0, "math_ceil(1e300) == 1e300 (beyond int64 range)");

    FS_FN("math", "math_trunc");
    CHECK_F64(math_trunc(1.9), 1.0, 0, "math_trunc(1.9)");
    CHECK_F64(math_trunc(-1.9), -1.0, 0, "math_trunc(-1.9)");
    CHECK_F64(math_trunc(2.0), 2.0, 0, "math_trunc(2.0)");
    CHECK_F64(math_trunc(-2.0), -2.0, 0, "math_trunc(-2.0)");
    CHECK_F64(math_trunc(0.9), 0.0, 0, "math_trunc(0.9)");
    CHECK_F64(math_trunc(0.0), 0.0, 0, "math_trunc(0.0)");
    CHECK(math_isnan(math_trunc(fs_nan())), "math_trunc(NaN) -> NaN");
    CHECK(math_isinf(math_trunc(1e308 * 10)), "math_trunc(+inf) -> +inf");
    CHECK_F64(math_trunc(1e19), 1e19, 0, "math_trunc(1e19) == 1e19 (beyond int64 range)");
    CHECK_F64(math_trunc(-1e19), -1e19, 0, "math_trunc(-1e19) == -1e19 (beyond int64 range)");
    CHECK_F64(math_trunc(1e300), 1e300, 0, "math_trunc(1e300) == 1e300 (beyond int64 range)");
    CHECK_F64(math_trunc(1.9e19), 1.9e19, 0, "math_trunc(1.9e19) == 1.9e19 (beyond int64 range)");

    /* ==================== 整数数学 ==================== */
    FS_FN("math", "math_abs_i64");
    CHECK_INT(math_abs_i64(5), 5, "math_abs_i64(5)");
    CHECK_INT(math_abs_i64(-5), 5, "math_abs_i64(-5)");
    CHECK_INT(math_abs_i64(0), 0, "math_abs_i64(0)");
    CHECK_INT(math_abs_i64(9223372036854775807LL), 9223372036854775807LL, "math_abs_i64(INT64_MAX)");
    CHECK_INT(math_abs_i64(-9223372036854775807LL - 1), 9223372036854775807LL,
              "math_abs_i64(INT64_MIN) saturates to INT64_MAX");

    FS_FN("math", "math_abs_i32");
    CHECK_INT(math_abs_i32(5), 5, "math_abs_i32(5)");
    CHECK_INT(math_abs_i32(-5), 5, "math_abs_i32(-5)");
    CHECK_INT(math_abs_i32(0), 0, "math_abs_i32(0)");
    CHECK_INT(math_abs_i32(2147483647), 2147483647, "math_abs_i32(INT32_MAX)");
    CHECK_INT(math_abs_i32(-2147483647 - 1), 2147483647,
              "math_abs_i32(INT32_MIN) saturates to INT32_MAX");

    FS_FN("math", "math_min_i64");
    CHECK_INT(math_min_i64(1, 2), 1, "math_min_i64(1,2)");
    CHECK_INT(math_min_i64(-9223372036854775807LL - 1, 0), -9223372036854775807LL - 1, "math_min_i64(INT64_MIN,0)");

    FS_FN("math", "math_max_i64");
    CHECK_INT(math_max_i64(1, 2), 2, "math_max_i64(1,2)");
    CHECK_INT(math_max_i64(0, 9223372036854775807LL), 9223372036854775807LL, "math_max_i64(0,INT64_MAX)");

    FS_FN("math", "math_min_i32");
    CHECK_INT(math_min_i32(1, 2), 1, "math_min_i32(1,2)");
    CHECK_INT(math_min_i32(-2147483647 - 1, 0), -2147483647 - 1, "math_min_i32(INT32_MIN,0)");

    FS_FN("math", "math_max_i32");
    CHECK_INT(math_max_i32(1, 2), 2, "math_max_i32(1,2)");
    CHECK_INT(math_max_i32(0, 2147483647), 2147483647, "math_max_i32(0,INT32_MAX)");

    FS_FN("math", "math_min_u64");
    CHECK_UINT(math_min_u64(1, 2), 1, "math_min_u64(1,2)");
    CHECK_UINT(math_min_u64(0, 18446744073709551615ULL), 0, "math_min_u64(0,U64_MAX)");

    FS_FN("math", "math_max_u64");
    CHECK_UINT(math_max_u64(1, 2), 2, "math_max_u64(1,2)");
    CHECK_UINT(math_max_u64(0, 18446744073709551615ULL), 18446744073709551615ULL, "math_max_u64(0,U64_MAX)");

    FS_FN("math", "math_min_u32");
    CHECK_UINT(math_min_u32(1, 2), 1, "math_min_u32(1,2)");
    CHECK_UINT(math_min_u32(0, 4294967295U), 0, "math_min_u32(0,U32_MAX)");

    FS_FN("math", "math_max_u32");
    CHECK_UINT(math_max_u32(1, 2), 2, "math_max_u32(1,2)");
    CHECK_UINT(math_max_u32(0, 4294967295U), 4294967295U, "math_max_u32(0,U32_MAX)");

    /* ==================== 位运算辅助 ==================== */
    FS_FN("math", "math_clz");
    CHECK_UINT(math_clz(0), 64, "math_clz(0) == 64");
    CHECK_UINT(math_clz(1), 63, "math_clz(1) == 63");
    CHECK_UINT(math_clz(2), 62, "math_clz(2) == 62");
    CHECK_UINT(math_clz(0x8000000000000000ULL), 0, "math_clz(MSB only) == 0");
    CHECK_UINT(math_clz(0xFF), 56, "math_clz(0xFF) == 56");
    CHECK_UINT(math_clz(0xFFFFFFFFFFFFFFFFULL), 0, "math_clz(U64_MAX) == 0");
    CHECK_UINT(math_clz(0x0000000100000000ULL), 31, "math_clz(1<<32) == 31");

    FS_FN("math", "math_ctz");
    CHECK_UINT(math_ctz(0), 64, "math_ctz(0) == 64");
    CHECK_UINT(math_ctz(1), 0, "math_ctz(1) == 0");
    CHECK_UINT(math_ctz(2), 1, "math_ctz(2) == 1");
    CHECK_UINT(math_ctz(0x8000000000000000ULL), 63, "math_ctz(MSB only) == 63");
    CHECK_UINT(math_ctz(0x100ULL), 8, "math_ctz(0x100) == 8");
    CHECK_UINT(math_ctz(0xFFFFFFFFFFFFFFFFULL), 0, "math_ctz(U64_MAX) == 0");

    FS_FN("math", "math_popcount");
    CHECK_UINT(math_popcount(0), 0, "math_popcount(0)");
    CHECK_UINT(math_popcount(1), 1, "math_popcount(1)");
    CHECK_UINT(math_popcount(3), 2, "math_popcount(3)");
    CHECK_UINT(math_popcount(0xFF), 8, "math_popcount(0xFF) == 8");
    CHECK_UINT(math_popcount(0xFFFFFFFFFFFFFFFFULL), 64, "math_popcount(U64_MAX) == 64");
    CHECK_UINT(math_popcount(0x8000000000000000ULL), 1, "math_popcount(MSB only) == 1");

    FS_FN("math", "math_is_pow2");
    CHECK(math_is_pow2(1) == true, "math_is_pow2(1)");
    CHECK(math_is_pow2(2) == true, "math_is_pow2(2)");
    CHECK(math_is_pow2(256) == true, "math_is_pow2(256)");
    CHECK(math_is_pow2(0x8000000000000000ULL) == true, "math_is_pow2(1<<63)");
    CHECK(math_is_pow2(0) == false, "math_is_pow2(0) == false");
    CHECK(math_is_pow2(3) == false, "math_is_pow2(3) == false");
    CHECK(math_is_pow2(100) == false, "math_is_pow2(100) == false");

    FS_FN("math", "math_round_up");
    CHECK_UINT(math_round_up(0, 8), 0, "math_round_up(0,8) == 0");
    CHECK_UINT(math_round_up(1, 8), 8, "math_round_up(1,8) == 8");
    CHECK_UINT(math_round_up(8, 8), 8, "math_round_up(8,8) == 8");
    CHECK_UINT(math_round_up(9, 16), 16, "math_round_up(9,16) == 16");
    CHECK_UINT(math_round_up(17, 16), 32, "math_round_up(17,16) == 32");
    CHECK_UINT(math_round_up(100, 1), 100, "math_round_up(100,1) == 100");
    CHECK_UINT(math_round_up(100, 0), 100, "math_round_up alignment 0 -> identity");

    FS_FN("math", "math_round_down");
    CHECK_UINT(math_round_down(0, 8), 0, "math_round_down(0,8)");
    CHECK_UINT(math_round_down(1, 8), 0, "math_round_down(1,8) == 0");
    CHECK_UINT(math_round_down(8, 8), 8, "math_round_down(8,8) == 8");
    CHECK_UINT(math_round_down(17, 16), 16, "math_round_down(17,16) == 16");
    CHECK_UINT(math_round_down(31, 16), 16, "math_round_down(31,16) == 16");
    CHECK_UINT(math_round_down(100, 0), 100, "math_round_down alignment 0 -> identity");

    FS_FN("math", "math_div_round_up");
    CHECK_UINT(math_div_round_up(10, 3), 4, "math_div_round_up(10,3) == 4");
    CHECK_UINT(math_div_round_up(9, 3), 3, "math_div_round_up(9,3) == 3");
    CHECK_UINT(math_div_round_up(0, 3), 0, "math_div_round_up(0,3) == 0");
    CHECK_UINT(math_div_round_up(1, 1), 1, "math_div_round_up(1,1)");
    CHECK_UINT(math_div_round_up(7, 0), 0, "math_div_round_up(7,0) == 0 (no div by zero)");

    FS_FN("math", "math_gcd");
    CHECK_UINT(math_gcd(48, 36), 12, "math_gcd(48,36) == 12");
    CHECK_UINT(math_gcd(36, 48), 12, "math_gcd(36,48) == 12");
    CHECK_UINT(math_gcd(17, 5), 1, "math_gcd(17,5) == 1");
    CHECK_UINT(math_gcd(0, 5), 5, "math_gcd(0,5) == 5");
    CHECK_UINT(math_gcd(5, 0), 5, "math_gcd(5,0) == 5");
    CHECK_UINT(math_gcd(7, 7), 7, "math_gcd(7,7) == 7");
    CHECK_UINT(math_gcd(1000000, 250000), 250000, "math_gcd large");

    /* ==================== 随机数 ==================== */
    FS_FN("math", "math_srand");
    {
        /* 同种子 → 同序列 (确定性) */
        math_srand(12345);
        int a1 = math_rand(), a2 = math_rand(), a3 = math_rand();
        math_srand(12345);
        int b1 = math_rand(), b2 = math_rand(), b3 = math_rand();
        CHECK(a1 == b1 && a2 == b2 && a3 == b3, "math_srand makes math_rand deterministic");
        CHECK(a1 != a2 && a2 != a3, "math_rand sequence is not constant");
        /* 不同种子 → 不同序列 */
        math_srand(1);
        int c1 = math_rand();
        math_srand(2);
        int c2 = math_rand();
        CHECK(c1 != c2, "different seeds give different sequences");
        /* seed 0 不退化为全零状态 */
        math_srand(0);
        int z1 = math_rand(), z2 = math_rand();
        CHECK(z1 != 0 || z2 != 0, "math_srand(0) does not degenerate");
    }

    FS_FN("math", "math_rand");
    {
        math_srand(777);
        int ok = 1;
        int seen_zero = 0, distinct = 0, prev = math_rand();
        for (int i = 0; i < 5000; i++) {
            int r = math_rand();
            if (r < 0 || r > 2147483647) ok = 0;   /* 必须落在 [0, INT32_MAX] */
            if (r != prev) distinct++;
            prev = r;
            if (r == 0) seen_zero++;
        }
        CHECK(ok, "math_rand always in [0, INT32_MAX]");
        CHECK(distinct > 4900, "math_rand produces distinct values");
        (void)seen_zero;
    }

    FS_FN("math", "math_randf");
    {
        math_srand(31337);
        int ok = 1;
        double sum = 0;
        for (int i = 0; i < 20000; i++) {
            double r = math_randf();
            if (!(r >= 0.0 && r < 1.0)) ok = 0;
            sum += r;
        }
        CHECK(ok, "math_randf in [0,1)");
        double mean = sum / 20000.0;
        CHECK_F64(mean, 0.5, 0.05, "math_randf mean ~0.5 (uniform)");
    }

    FS_FN("math", "math_rand_range");
    {
        math_srand(4242);
        int ok = 1;
        for (int i = 0; i < 20000; i++) {
            double r = math_rand_range(-5.0, 5.0);
            if (!(r >= -5.0 && r <= 5.0)) ok = 0;
        }
        CHECK(ok, "math_rand_range in [min,max]");
        for (int i = 0; i < 1000; i++) {
            double r = math_rand_range(10.0, 10.0);
            if (r != 10.0) { CHECK(0, "math_rand_range(min==max) returns min"); break; }
        }
        double v = math_rand_range(0.0, 100.0);
        CHECK(v >= 0.0 && v <= 100.0, "math_rand_range(0,100) in range");
    }

    /* ==================== 角度转换 ==================== */
    FS_FN("math", "math_deg_to_rad");
    CHECK_F64(math_deg_to_rad(0.0), 0.0, 1e-15, "math_deg_to_rad(0)");
    CHECK_F64(math_deg_to_rad(180.0), FS_PI, 1e-12, "math_deg_to_rad(180) == PI");
    CHECK_F64(math_deg_to_rad(90.0), FS_PI / 2, 1e-12, "math_deg_to_rad(90) == PI/2");
    CHECK_F64(math_deg_to_rad(-90.0), -FS_PI / 2, 1e-12, "math_deg_to_rad(-90)");
    CHECK_F64(math_deg_to_rad(360.0), 2 * FS_PI, 1e-12, "math_deg_to_rad(360) == 2PI");

    FS_FN("math", "math_rad_to_deg");
    CHECK_F64(math_rad_to_deg(0.0), 0.0, 1e-12, "math_rad_to_deg(0)");
    CHECK_F64(math_rad_to_deg(FS_PI), 180.0, 1e-10, "math_rad_to_deg(PI) == 180");
    CHECK_F64(math_rad_to_deg(FS_PI / 2), 90.0, 1e-10, "math_rad_to_deg(PI/2) == 90");
    CHECK_F64(math_rad_to_deg(math_deg_to_rad(37.0)), 37.0, 1e-10, "deg->rad->deg roundtrip 37");
    CHECK_F64(math_rad_to_deg(math_deg_to_rad(-123.456)), -123.456, 1e-9, "roundtrip -123.456");
}
