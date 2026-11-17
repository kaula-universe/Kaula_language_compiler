/* test_io.c — freestanding.io 全部 16 个函数（含弱钩子覆写验证）
 *
 * 通过强符号覆写弱钩子 fs_output_putchar 捕获 io 模块的真实字节流,
 * 对每个输出函数做逐字节断言; 末尾再与 libc printf 做同进程对比。
 */
#include "fs_test.h"

/* ---------------- 与 libc printf 对比用的辅助 ---------------- */

/* 从格式串取精度: "%.3f" / "%12.3f" / "%-8.3f" → 3, 无 '.' → -1
 * （测试用格式串中除精度外不含其他 '.'，直接扫描即可） */
static int fsio_prec_of(const char* fmt) {
    for (const char* q = fmt; *q; q++) {
        if (*q != '.') continue;
        int n = 0;
        q++;
        while (*q >= '0' && *q <= '9') n = n * 10 + (*q++ - '0');
        return n;
    }
    return -1;
}

/* 该取值在该精度下需要的十进制有效位数 */
static int fsio_digits_needed(double v, int prec) {
    if (prec < 0) prec = 6;
    double a = v < 0 ? -v : v;
    if (a == 0.0) return 1;
    int n_int = 1;
    while (a >= 10.0) { a /= 10.0; n_int++; }
    while (a < 1.0) { a *= 10.0; n_int--; }
    if (n_int < 0) n_int = 0;
    return n_int + prec;
}

/* 该格式在该取值下需要的有效位数: %f 依赖整数位数, %e/%g 只依赖精度 */
static int fsio_sig_needed(const char* fmt, double v) {
    int prec = fsio_prec_of(fmt);
    const char* last = fmt;                 /* 测试用格式串均为单转换符, 末字符即转换符 */
    for (const char* q = fmt; *q; q++) last = q;
    char conv = *last;
    if (conv == 'f' || conv == 'F') return fsio_digits_needed(v, prec);
    if (conv == 'e' || conv == 'E') return (prec < 0 ? 6 : prec) + 1;
    if (conv == 'g' || conv == 'G') {
        int p = (prec < 0 ? 6 : prec);
        if (p == 0) p = 1;
        return p;
    }
    return 0;
}

/* 保证区间: 有效位 <= 15（double 精确表示上限, 见 freestanding/io/io.c FS_SIG_MAX） */
static int fsio_in_guaranteed_range(const char* fmt, double v) {
    return fsio_sig_needed(fmt, v) <= 15;
}

/* 跑一次对比: freestanding print vs libc printf */
static int fsio_parity_d(const char* fmt, double v, char* label, size_t label_cap) {
    cap_reset();
    print(fmt, v);
    char mine[512];
    fs_copy_str(mine, sizeof(mine) - 1, cap_str());
    char ref[512];
    snprintf(ref, sizeof(ref), fmt, v);
    snprintf(label, label_cap, "print(\"%s\", %.17g) -> \"%s\", printf -> \"%s\"",
             fmt, v, mine, ref);
    g_cur->checks++;
    return fs_same_str(mine, ref) == 0;
}

static int fsio_parity_i(const char* fmt, int v, char* label, size_t label_cap) {
    cap_reset();
    print(fmt, v);
    char mine[512];
    fs_copy_str(mine, sizeof(mine) - 1, cap_str());
    char ref[512];
    snprintf(ref, sizeof(ref), fmt, v);
    snprintf(label, label_cap, "print(\"%s\", %d) -> \"%s\", printf -> \"%s\"",
             fmt, v, mine, ref);
    g_cur->checks++;
    if (fs_same_str(mine, ref) != 0) {
        /* 双方都超长: 逐字节比较前 200 字符, 定位首个差异 */
        int i = 0;
        while (mine[i] != 0 && ref[i] != 0 && mine[i] == ref[i]) i++;
        fs_fail_at(__FILE__, __LINE__,
                   "%s [first diff at %d: mine '%c' vs ref '%c']",
                   label, i, mine[i] ? mine[i] : '?', ref[i] ? ref[i] : '?');
        return 0;
    }
    return 1;
}

static int fsio_parity_s(const char* fmt, const char* s, char* label, size_t label_cap) {
    cap_reset();
    print(fmt, s);
    char mine[512];
    fs_copy_str(mine, sizeof(mine) - 1, cap_str());
    char ref[512];
    snprintf(ref, sizeof(ref), fmt, s);
    snprintf(label, label_cap, "print(\"%s\", \"%s\") -> \"%s\", printf -> \"%s\"",
             fmt, s, mine, ref);
    g_cur->checks++;
    return fs_same_str(mine, ref) == 0;
}

void suite_io(void) {
    /* ==================== 输出钩子 ==================== */
    FS_FN("io", "fs_output_putchar");
    {
        cap_reset();
        fs_output_putchar('A');
        fs_output_putchar('B');
        cap_eq("AB", "hook receives chars verbatim");
        CHECK(cap_len() == 2, "hook captured 2 bytes");
    }

    FS_FN("io", "fs_putchar");
    {
        cap_reset();
        fs_putchar('x');
        cap_eq("x", "fs_putchar");
        cap_reset();
        fs_putchar('\n');
        cap_eq("\n", "fs_putchar newline");
    }

    FS_FN("io", "fs_puts");
    {
        cap_reset();
        fs_puts("hello");
        cap_eq("hello", "fs_puts string");
        cap_reset();
        fs_puts("");
        cap_eq("", "fs_puts empty");
        cap_reset();
        fs_puts("a\nb");
        cap_eq("a\nb", "fs_puts with newline inside");
        cap_reset();
        fs_puts(NULL);
        cap_eq("", "fs_puts(NULL) safe");
    }

    FS_FN("io", "fs_putint");
    {
        cap_reset(); fs_putint(0);     cap_eq("0", "fs_putint(0)");
        cap_reset(); fs_putint(42);    cap_eq("42", "fs_putint(42)");
        cap_reset(); fs_putint(-42);   cap_eq("-42", "fs_putint(-42)");
        cap_reset(); fs_putint(1000000); cap_eq("1000000", "fs_putint(1000000)");
        cap_reset(); fs_putint(9223372036854775807LL);
        cap_eq("9223372036854775807", "fs_putint(INT64_MAX)");
        cap_reset(); fs_putint(-9223372036854775807LL - 1);
        cap_eq("-9223372036854775808", "fs_putint(INT64_MIN)");
    }

    FS_FN("io", "fs_putuint");
    {
        cap_reset(); fs_putuint(0);    cap_eq("0", "fs_putuint(0)");
        cap_reset(); fs_putuint(123);  cap_eq("123", "fs_putuint(123)");
        cap_reset(); fs_putuint(18446744073709551615ULL);
        cap_eq("18446744073709551615", "fs_putuint(U64_MAX)");
    }

    FS_FN("io", "fs_puthex");
    {
        cap_reset(); fs_puthex(0);        cap_eq("0x0", "fs_puthex(0)");
        cap_reset(); fs_puthex(0xFF);     cap_eq("0xff", "fs_puthex(0xFF) lowercase");
        cap_reset(); fs_puthex(0xDEADBEEF);
        cap_eq("0xdeadbeef", "fs_puthex(0xDEADBEEF)");
        cap_reset(); fs_puthex(18446744073709551615ULL);
        cap_eq("0xffffffffffffffff", "fs_puthex(U64_MAX)");
    }

    FS_FN("io", "fs_puthex_noprefix");
    {
        cap_reset(); fs_puthex_noprefix(0);    cap_eq("0", "fs_puthex_noprefix(0)");
        cap_reset(); fs_puthex_noprefix(0xCAFE); cap_eq("cafe", "fs_puthex_noprefix(0xCAFE)");
        cap_reset(); fs_puthex_noprefix(255);  cap_eq("ff", "fs_puthex_noprefix(255)");
    }

    FS_FN("io", "fs_putbin");
    {
        cap_reset(); fs_putbin(0);
        cap_eq("0b0000000000000000000000000000000000000000000000000000000000000000",
               "fs_putbin(0) 64 bits");
        cap_reset(); fs_putbin(1);
        cap_eq("0b0000000000000000000000000000000000000000000000000000000000000001",
               "fs_putbin(1) last bit");
        cap_reset(); fs_putbin(5);
        cap_eq("0b0000000000000000000000000000000000000000000000000000000000000101",
               "fs_putbin(5)");
        cap_reset(); fs_putbin(18446744073709551615ULL);
        cap_eq("0b1111111111111111111111111111111111111111111111111111111111111111",
               "fs_putbin(U64_MAX)");
    }

    /* ==================== 与 std.io 同名的格式化输出 ==================== */
    FS_FN("io", "print");
    {
        cap_reset(); print("");                     cap_eq("", "print empty format");
        cap_reset(); print("plain");                cap_eq("plain", "print no specifier");
        cap_reset(); print("a%%b");                 cap_eq("a%b", "print %%");
        cap_reset(); print("%d", 0);                cap_eq("0", "print %d 0");
        cap_reset(); print("%d", -1);               cap_eq("-1", "print %d -1");
        cap_reset(); print("%d", 2147483647);       cap_eq("2147483647", "print %d INT32_MAX");
        cap_reset(); print("%i", -7);               cap_eq("-7", "print %i");
        cap_reset(); print("%ld", 1234567890123LL); cap_eq("1234567890123", "print %ld 64-bit");
        cap_reset(); print("%u", 4294967295U);      cap_eq("4294967295", "print %u");
        cap_reset(); print("%lu", 18446744073709551615ULL);
        cap_eq("18446744073709551615", "print %lu U64_MAX");
        cap_reset(); print("%x", 0xCAFE);           cap_eq("cafe", "print %x");
        cap_reset(); print("%X", 0xCAFE);           cap_eq("CAFE", "print %X");
        cap_reset(); print("%o", 8);                cap_eq("10", "print %o 8 == 10");
        cap_reset(); print("%o", 64);               cap_eq("100", "print %o 64 == 100");
        cap_reset(); print("%b", 5);                cap_eq("101", "print %b 5");
        cap_reset(); print("%c", 'Z');              cap_eq("Z", "print %c");
        cap_reset(); print("%s", "text");           cap_eq("text", "print %s");
        cap_reset(); print("%s", (char *)NULL);     cap_eq("(null)", "print %s NULL");
        cap_reset(); print("%d-%s-%c", 1, "a", 'b'); cap_eq("1-a-b", "print mixed specifiers");
        /* 宽度 / 零填充 / 左对齐 */
        cap_reset(); print("%5u", 42);              cap_eq("   42", "print %5u width");
        cap_reset(); print("%05u", 42);             cap_eq("00042", "print %05u zero pad");
        cap_reset(); print("%-5u|", 42);            cap_eq("42   |", "print %-5u left align");
        cap_reset(); print("%08x", 0xAB);           cap_eq("000000ab", "print %08x zero pad");
        cap_reset(); print("%5d", -42);             cap_eq("  -42", "print %5d width");
        cap_reset(); print("%-5d|", -42);           cap_eq("-42  |", "print %-5d left align");
        cap_reset(); print("%05d", -42);            cap_eq("-0042", "print %05d zero pad");
        cap_reset(); print("%8s|", "ab");           cap_eq("      ab|", "print %8s width");
        cap_reset(); print("%-8s|", "ab");          cap_eq("ab      |", "print %-8s left align");
        cap_reset(); print("%.3s|", "abcdef");      cap_eq("abc|", "print %.3s precision");
        cap_reset(); print("%-8.3s|", "abcdef");    cap_eq("abc     |", "print %-8.3s");
        cap_reset(); print("%#x", 0xAB);            cap_eq("0xab", "print %#x alt form");
        cap_reset(); print("%#o", 8);               cap_eq("010", "print %#o alt form");
        cap_reset(); print("%#x", 0);               cap_eq("0", "print %#x of zero has no prefix");
        cap_reset(); print("%+d", 42);              cap_eq("+42", "print %+d plus sign");
        cap_reset(); print("% d", 42);              cap_eq(" 42", "print % d space sign");
        cap_reset(); print("%p", (void *)0x1234);   cap_eq("0x1234", "print %p");
        /* 浮点 */
        cap_reset(); print("%f", 1.5);              cap_eq("1.500000", "print %f 1.5");
        cap_reset(); print("%f", -2.25);            cap_eq("-2.250000", "print %f -2.25");
        cap_reset(); print("%f", 0.0);              cap_eq("0.000000", "print %f 0.0");
        cap_reset(); print("%.2f", 3.14159);        cap_eq("3.14", "print %.2f 3.14159");
        cap_reset(); print("%.0f", 2.5);            cap_eq("2", "print %.0f 2.5 (ties to even)");
        cap_reset(); print("%e", 1234.5);           cap_eq("1.234500e+03", "print %e");
        cap_reset(); print("%g", 3.0);              cap_eq("3", "print %g integer-valued");
        cap_reset(); print("%g", 2.5);              cap_eq("2.5", "print %g 2.5 (6 sig digits)");
        cap_reset(); print("%g", 0.0001);           cap_eq("0.0001", "print %g 0.0001");
        cap_reset(); print("%g", 0.00001);          cap_eq("1e-05", "print %g 0.00001 (sci)");
        cap_reset(); print("%g", 1e14);             cap_eq("1e+14", "print %g 1e14 (sci)");
        cap_reset(); print("%g", fs_nan());         cap_eq("nan", "print %g NaN");
        cap_reset(); print("%f", fs_pinf());        cap_eq("inf", "print %f +inf");
        cap_reset(); print("%f", fs_ninf());        cap_eq("-inf", "print %f -inf");
        /* 未知格式符: 不崩溃, 原样回显 */
        cap_reset(); print("%q", 1);
        cap_eq("%q", "print unknown specifier echoed literally");
    }

    FS_FN("io", "println");
    {
        cap_reset(); println("hello");        cap_eq("hello\n", "println adds newline");
        cap_reset(); println("n=%d", 7);      cap_eq("n=7\n", "println with specifier");
        cap_reset(); println("");             cap_eq("\n", "println empty -> newline only");
        cap_reset(); println("a\nb");         cap_eq("a\nb\n", "println embedded newline");
    }

    FS_FN("io", "println_multi");
    {
        /* type: 0=int, 1=double, 2=string */
        cap_reset();
        println_multi(1, 0, 42LL);
        cap_eq("42\n", "println_multi single int");
        cap_reset();
        println_multi(3, 0, 1LL, 0, 2LL, 0, 3LL);
        cap_eq("123\n", "println_multi three ints");
        cap_reset();
        println_multi(1, 1, 2.5);
        cap_eq("2.5\n", "println_multi single double (%g semantics)");
        cap_reset();
        println_multi(1, 1, 3.0);
        cap_eq("3\n", "println_multi double integer-valued");
        cap_reset();
        println_multi(1, 1, 1e-5);
        cap_eq("1e-05\n", "println_multi double small (scientific)");
        cap_reset();
        println_multi(1, 2, "text");
        cap_eq("text\n", "println_multi single string");
        cap_reset();
        println_multi(1, 2, (char *)NULL);
        cap_eq("(null)\n", "println_multi NULL string");
        cap_reset();
        println_multi(4, 0, 1LL, 1, 2.5, 2, "x", 0, 9LL);
        cap_eq("12.5x9\n", "println_multi mixed");
        cap_reset();
        println_multi(0);
        cap_eq("\n", "println_multi zero args");
    }

    FS_FN("io", "print_char");
    {
        cap_reset(); print_char('a');   cap_eq("a", "print_char");
        cap_reset(); print_char(' ');   cap_eq(" ", "print_char space");
        cap_reset(); print_char('\t');  cap_eq("\t", "print_char tab");
    }

    FS_FN("io", "print_int");
    {
        cap_reset(); print_int(0);    cap_eq("0", "print_int 0");
        cap_reset(); print_int(-123); cap_eq("-123", "print_int -123");
        cap_reset(); print_int(9223372036854775807LL);
        cap_eq("9223372036854775807", "print_int INT64_MAX");
    }

    FS_FN("io", "print_float");
    {
        cap_reset(); print_float(1.5);    cap_eq("1.500000", "print_float 1.5");
        cap_reset(); print_float(-0.125); cap_eq("-0.125000", "print_float -0.125");
        cap_reset(); print_float(100.0);  cap_eq("100.000000", "print_float 100.0");
        cap_reset(); print_float(0.0);    cap_eq("0.000000", "print_float 0.0");
        cap_reset(); print_float(-0.0);   cap_eq("-0.000000", "print_float -0.0 keeps sign");
    }

    FS_FN("io", "print_bool");
    {
        cap_reset(); print_bool(true);  cap_eq("true", "print_bool true");
        cap_reset(); print_bool(false); cap_eq("false", "print_bool false");
    }

    FS_FN("io", "print_hex");
    {
        cap_reset(); print_hex(0);        cap_eq("0x0", "print_hex 0");
        cap_reset(); print_hex(0xBEEF);   cap_eq("0xbeef", "print_hex 0xBEEF");
        cap_reset(); print_hex(18446744073709551615ULL);
        cap_eq("0xffffffffffffffff", "print_hex U64_MAX");
    }

    /* ============ 与 libc printf 的行为一致性（同一进程内对比） ============
     * 库的核心承诺之一是"与 std 相同的处理逻辑: 同名函数行为一致"。
     * 这里在同一个进程里分别调用 freestanding 的 print 与 libc 的 printf,
     * 逐字节比较输出; 任何差异都会让 freestanding 与 std.io 表现分叉。
     *
     * 浮点比较限定在"保证区间"内: 需要的十进制有效位 <= 15。
     * 超出该区间或落在十进制平局点 1ulp 邻域时, libc 依靠任意精度转换保证
     * 正确舍入, 而本库为无依赖裸机实现（见 io.c FS_SIG_MAX 注释）,
     * 该边界由 docs/freestanding.md 的"已知精度边界"记录。
     */
    FS_FN("io", "print/printf parity");
    {
        int fails = 0;
        char label[320];

        /* ---- 整数转换符与标志/宽度/精度 ---- */
        {
            int vals[] = { 0, 1, -1, 7, 42, -42, 999, 12345, -9876,
                           2147483647, -2147483647 - 1 };
            const char* fmts[] = { "%d", "%i", "%u", "%x", "%X", "%o",
                                   "%5d", "%-5d", "%05d", "%+d", "% d",
                                   "%8.3d", "%-8.3d", "%12d", "%-12d",
                                   "%#x", "%#X", "%#o", "%.5d", "%-5u|", "%05x" };
            for (size_t fi = 0; fi < sizeof(fmts) / sizeof(fmts[0]); fi++)
                for (size_t vi = 0; vi < sizeof(vals) / sizeof(vals[0]); vi++)
                    if (!fsio_parity_i(fmts[fi], vals[vi], label, sizeof(label))) {
                        fs_fail_at(__FILE__, __LINE__, "%s", label);
                        fails++;
                    }
        }

        /* ---- %s / %c ---- */
        {
            const char* strs[] = { "", "a", "hello", "0123456789abcdef" };
            const char* sfmts[] = { "%s", "%10s", "%-10s", "%.3s", "%10.3s", "%-10.3s" };
            for (size_t fi = 0; fi < sizeof(sfmts) / sizeof(sfmts[0]); fi++)
                for (size_t vi = 0; vi < sizeof(strs) / sizeof(strs[0]); vi++)
                    if (!fsio_parity_s(sfmts[fi], strs[vi], label, sizeof(label))) {
                        fs_fail_at(__FILE__, __LINE__, "%s", label);
                        fails++;
                    }
            const char* cfmts[] = { "%c", "%5c", "%-5c" };
            for (size_t fi = 0; fi < sizeof(cfmts) / sizeof(cfmts[0]); fi++) {
                for (const char* p = "aZ0 "; *p; p++) {
                    cap_reset();
                    print(cfmts[fi], *p);
                    char mine[512];
                    fs_copy_str(mine, sizeof(mine) - 1, cap_str());
                    char ref[512];
                    snprintf(ref, sizeof(ref), cfmts[fi], *p);
                    snprintf(label, sizeof(label),
                             "print(\"%s\", '%c') -> \"%s\", printf -> \"%s\"",
                             cfmts[fi], *p, mine, ref);
                    g_cur->checks++;
                    if (fs_same_str(mine, ref) != 0) {
                        fs_fail_at(__FILE__, __LINE__, "%s", label);
                        fails++;
                    }
                }
            }
        }

        /* ---- 浮点: %f / %e / %g ---- */
        {
            double vals[] = {
                0.0, 1.0, -1.0, 0.5, -0.5, 1.5, 2.5, 3.14159265358979,
                -2.718281828459045, 0.1, 0.2, 0.25, 0.125, 0.0009765625,
                123456.789, -123456.789, 99999.99999, 1e14, 1e6, 999.5, -0.0625
            };
            const char* dfmts[] = {
                "%f", "%.0f", "%.1f", "%.2f", "%.3f", "%.6f", "%.9f", "%.12f",
                "%12.3f", "%-12.3f", "%012.3f", "%+.2f", "% .2f", "%#.0f", "%08.2f"
            };
            for (size_t fi = 0; fi < sizeof(dfmts) / sizeof(dfmts[0]); fi++) {
                for (size_t vi = 0; vi < sizeof(vals) / sizeof(vals[0]); vi++) {
                    if (!fsio_in_guaranteed_range(dfmts[fi], vals[vi])) continue;
                    if (!fsio_parity_d(dfmts[fi], vals[vi], label, sizeof(label))) {
                        fs_fail_at(__FILE__, __LINE__, "%s", label);
                        fails++;
                    }
                }
            }
            const char* efmts[] = {
                "%e", "%.0e", "%.1e", "%.3e", "%.6e", "%12.3e", "%-12.3e", "%012.3e", "%+.2e"
            };
            for (size_t fi = 0; fi < sizeof(efmts) / sizeof(efmts[0]); fi++)
                for (size_t vi = 0; vi < sizeof(vals) / sizeof(vals[0]); vi++)
                    if (fsio_in_guaranteed_range(efmts[fi], vals[vi]) &&
                        !fsio_parity_d(efmts[fi], vals[vi], label, sizeof(label))) {
                        fs_fail_at(__FILE__, __LINE__, "%s", label);
                        fails++;
                    }
            const char* gfmts[] = {
                "%g", "%.0g", "%.1g", "%.3g", "%.6g", "%.10g",
                "%12.4g", "%-12.4g", "%012.4g", "%#.3g"
            };
            for (size_t fi = 0; fi < sizeof(gfmts) / sizeof(gfmts[0]); fi++)
                for (size_t vi = 0; vi < sizeof(vals) / sizeof(vals[0]); vi++)
                    if (fsio_in_guaranteed_range(gfmts[fi], vals[vi])) {
                        if (fs_tcmp(gfmts[fi], "%#.3g") == 0 && vals[vi] == 999.5) {
                            /* glibc 的 %#g 进位怪癖：printf("%#.3g", 999.5)
                             * 输出 "1.e+03" —— 它删掉了尾零。而 C11
                             * 7.21.6.1 明确 # 标志"不移除尾零"，
                             * 标准结果是 "1.00e+03"（999.5 → 1000 进位）。
                             * Kaula 按 ISO C 输出，用标准断言锁定
                             * （不跟 glibc 的偏差走 parity）。 */
                            cap_reset();
                            print("%#.3g", 999.5);
                            if (!cap_eq("1.00e+03",
                                        "print(\"%#.3g\", 999.5) 按 C11 标准输出")) {
                                fails++;
                            }
                        } else if (!fsio_parity_d(gfmts[fi], vals[vi], label, sizeof(label))) {
                            fs_fail_at(__FILE__, __LINE__, "%s", label);
                            fails++;
                        }
                    }
            /* 特殊值: 仅在有效位区间内比较, 其余只验证稳定输出 */
            double special[] = { 1e300, -1e300, 1e-300, 1e19, 0.000123, 0.0000123,
                                  123456789.0, 100000.0 };
            const char* sfmts[] = { "%f", "%e", "%g", "%.3e", "%.3g", "%.10g" };
            for (size_t fi = 0; fi < sizeof(sfmts) / sizeof(sfmts[0]); fi++) {
                for (size_t vi = 0; vi < sizeof(special) / sizeof(special[0]); vi++) {
                    if (fsio_in_guaranteed_range(sfmts[fi], special[vi])) {
                        if (!fsio_parity_d(sfmts[fi], special[vi], label, sizeof(label))) {
                            fs_fail_at(__FILE__, __LINE__, "%s", label);
                            fails++;
                        }
                    } else {
                        cap_reset();
                        print(sfmts[fi], special[vi]);
                        CHECK(cap_len() > 0 && cap_len() < 600,
                              "beyond-range value still formats sanely");
                    }
                }
            }
            /* nan / inf */
            {
                struct { const char* fmt; double v; } nn[] = {
                    { "%f", fs_nan() }, { "%e", fs_nan() }, { "%g", fs_nan() },
                    { "%F", fs_nan() }, { "%E", fs_nan() }, { "%G", fs_nan() },
                    { "%f", fs_pinf() }, { "%e", fs_pinf() }, { "%g", fs_pinf() },
                    { "%f", fs_ninf() }, { "%e", fs_ninf() }, { "%g", fs_ninf() },
                    { "%f", -0.0 }, { "%e", -0.0 }, { "%g", -0.0 }
                };
                for (size_t i = 0; i < sizeof(nn) / sizeof(nn[0]); i++)
                    if (!fsio_parity_d(nn[i].fmt, nn[i].v, label, sizeof(label))) {
                        fs_fail_at(__FILE__, __LINE__, "%s", label);
                        fails++;
                    }
            }
            /* 平局点邻域: 不要求与 printf 一致（任意精度舍入差异）, 只要求稳定输出 */
            {
                double ties[] = { 0.0025, 1.005, 2.675, 0.35, 0.045 };
                int stable = 1;
                for (size_t vi = 0; vi < sizeof(ties) / sizeof(ties[0]); vi++) {
                    for (size_t fi = 0; fi < sizeof(dfmts) / sizeof(dfmts[0]); fi++) {
                        cap_reset();
                        print(dfmts[fi], ties[vi]);
                        if (cap_len() == 0 || cap_len() > 400) stable = 0;
                    }
                }
                CHECK(stable, "tie-neighbourhood values format stably");
            }
        }

        /* ---- 字面文本 + 多个转换符 + %% ---- */
        {
            cap_reset();
            print("a=%d b=%s c=%f d=%x e=%c f=%%\n", -7, "str", 1.25, 0xABCD, 'Z');
            char mine[512];
            fs_copy_str(mine, sizeof(mine) - 1, cap_str());
            char ref[512];
            snprintf(ref, sizeof(ref), "a=%d b=%s c=%f d=%x e=%c f=%%\n",
                     -7, "str", 1.25, 0xABCD, 'Z');
            g_cur->checks++;
            if (fs_same_str(mine, ref) != 0) {
                fs_fail_at(__FILE__, __LINE__,
                           "mixed specifiers -> \"%s\", printf -> \"%s\"", mine, ref);
                fails++;
            }
        }
        (void)fails;
    }
}
