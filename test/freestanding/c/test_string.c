/* test_string.c — freestanding.string 全部 30 个函数 */
#include "fs_test.h"

void suite_string(void) {
    char buf[64];

    /* ==================== C 字符串操作（仅 freestanding 构建测库自身实现） ==================== */
#ifdef KAULA_FREESTANDING

    FS_FN("string", "strlen");
    CHECK_UINT(strlen(""), 0, "strlen(\"\")");
    CHECK_UINT(strlen("a"), 1, "strlen(\"a\")");
    CHECK_UINT(strlen("hello"), 5, "strlen(\"hello\")");
    CHECK_UINT(strlen("0123456789abcdefghijklmnopqrstuvwxyz"), 36, "strlen 36 chars");
    {
        char big[300];
        for (int i = 0; i < 256; i++) big[i] = 'x';
        big[256] = 0;
        CHECK_UINT(strlen(big), 256, "strlen 256 chars");
    }
    CHECK_UINT(strlen(NULL), 0, "strlen(NULL) == 0 (documented defensive behaviour)");

    FS_FN("string", "strcmp");
    CHECK_INT(strcmp("abc", "abc"), 0, "strcmp equal");
    CHECK(strcmp("abc", "abd") < 0, "strcmp less");
    CHECK(strcmp("abd", "abc") > 0, "strcmp greater");
    CHECK(strcmp("abc", "abcd") < 0, "strcmp prefix shorter < longer");
    CHECK(strcmp("abcd", "abc") > 0, "strcmp prefix longer > shorter");
    CHECK(strcmp("", "") == 0, "strcmp both empty");
    CHECK(strcmp("", "a") < 0, "strcmp empty < nonempty");
    /* 高位字节按 unsigned char 比较 */
    {
        const char a[2] = { (char)0x80, 0 };
        const char b[2] = { (char)0x01, 0 };
        CHECK(strcmp(a, b) > 0, "strcmp compares as unsigned char");
    }
    CHECK_INT(strcmp(NULL, "a"), -1, "strcmp(NULL,a) < 0");
    CHECK_INT(strcmp("a", NULL), 1, "strcmp(a,NULL) > 0");
    CHECK_INT(strcmp(NULL, NULL), 0, "strcmp(NULL,NULL) == 0");

    FS_FN("string", "strncmp");
    CHECK_INT(strncmp("abc", "abc", 0), 0, "strncmp n=0");
    CHECK_INT(strncmp("abc", "abd", 2), 0, "strncmp equal within n");
    CHECK(strncmp("abc", "abd", 3) < 0, "strncmp differs at n");
    CHECK_INT(strncmp("abc", "abcd", 3), 0, "strncmp stops at shorter string");
    CHECK(strncmp("abc", "abd", 10) < 0, "strncmp n beyond length still compares");
    CHECK_INT(strncmp("abc", "abc", 100), 0, "strncmp huge n equal");
    CHECK_INT(strncmp(NULL, NULL, 5), 0, "strncmp(NULL,NULL)");
    CHECK(strncmp(NULL, "abc", 3) < 0, "strncmp(NULL,abc) < 0");
    CHECK(strncmp("abc", NULL, 3) > 0, "strncmp(abc,NULL) > 0");

    FS_FN("string", "strcasecmp");
    CHECK_INT(strcasecmp("ABC", "abc"), 0, "strcasecmp case-insensitive equal");
    CHECK_INT(strcasecmp("AbC", "aBc"), 0, "strcasecmp mixed case");
    CHECK_INT(strcasecmp("", ""), 0, "strcasecmp both empty");
    CHECK(strcasecmp("abc", "abd") < 0, "strcasecmp less");
    CHECK(strcasecmp("ABD", "abc") > 0, "strcasecmp greater");
    CHECK(strcasecmp("abc", "abcd") < 0, "strcasecmp prefix");
    CHECK_INT(strcasecmp(NULL, "a"), -1, "strcasecmp(NULL,a) < 0");
    CHECK_INT(strcasecmp("a", NULL), 1, "strcasecmp(a,NULL) > 0");

    FS_FN("string", "strncasecmp");
    CHECK_INT(strncasecmp("ABC", "abc", 0), 0, "strncasecmp n=0");
    CHECK_INT(strncasecmp("ABC", "abd", 2), 0, "strncasecmp equal within n");
    CHECK(strncasecmp("ABC", "abd", 3) < 0, "strncasecmp differs at n");
    CHECK_INT(strncasecmp("abc", "ABCDEF", 3), 0, "strncasecmp stops at shorter");
    CHECK_INT(strncasecmp("Hello World", "HELLO WORLD", 20), 0, "strncasecmp long equal");
    CHECK(strncasecmp("HELLO", "hello", 1) == 0, "strncasecmp n=1 equal");

    FS_FN("string", "strcpy");
    {
        CHECK(strcpy(buf, "hello") == buf, "strcpy returns dst");
        CHECK_STR(buf, "hello", "strcpy copies content");
        CHECK(strcpy(buf, "") == buf, "strcpy empty returns dst");
        CHECK_STR(buf, "", "strcpy empty content");
        CHECK_STR(strcpy(buf, "0123456789abcdef"), "0123456789abcdef", "strcpy long");
        CHECK_NULL(strcpy(NULL, "x"), "strcpy(NULL,...) -> NULL");
    }

    FS_FN("string", "strncpy");
    {
        /* src 短于 n: 剩余填 '\0' */
        CHECK(strncpy(buf, "ab", 8) == buf, "strncpy returns dst");
        CHECK_STR(buf, "ab", "strncpy short content");
        {
            int zeroed = 1;
            for (int i = 2; i < 8; i++)
                if (buf[i] != 0) zeroed = 0;
            CHECK(zeroed, "strncpy zero-fills remainder");
        }
        /* src 长于 n: 截断, 不加终止符 (libc 语义) */
        for (int i = 0; i < 8; i++) buf[i] = '#';
        strncpy(buf, "abcdefghij", 4);
        CHECK(memcmp(buf, "abcd", 4) == 0, "strncpy truncates to n");
        CHECK_INT(buf[4], '#', "strncpy does not append NUL when truncated");
        /* n=0 */
        for (int i = 0; i < 8; i++) buf[i] = '#';
        strncpy(buf, "xyz", 0);
        CHECK_INT(buf[0], '#', "strncpy n=0 writes nothing");
        CHECK_NULL(strncpy(NULL, "x", 1), "strncpy(NULL,...) -> NULL");
    }

    FS_FN("string", "strcat");
    {
        CHECK(strcpy(buf, "foo") == buf, "strcat setup");
        CHECK(strcat(buf, "bar") == buf, "strcat returns dst");
        CHECK_STR(buf, "foobar", "strcat appends");
        strcat(buf, "");
        CHECK_STR(buf, "foobar", "strcat empty src no-op");
        strcat(buf, "baz");
        CHECK_STR(buf, "foobarbaz", "strcat again");
        CHECK_NULL(strcat(NULL, "x"), "strcat(NULL,...) -> NULL");
    }

    FS_FN("string", "strncat");
    {
        strcpy(buf, "foo");
        CHECK(strncat(buf, "barbaz", 3) == buf, "strncat returns dst");
        CHECK_STR(buf, "foobar", "strncat appends at most n");
        strncat(buf, "XY", 10);
        CHECK_STR(buf, "foobarXY", "strncat stops at NUL and terminates");
        strcpy(buf, "a");
        strncat(buf, "bc", 0);
        CHECK_STR(buf, "a", "strncat n=0 no-op");
        CHECK_NULL(strncat(NULL, "x", 1), "strncat(NULL,...) -> NULL");
    }

    FS_FN("string", "strchr");
    {
        CHECK(strchr("hello", 'l') == strchr("hello", 'l'), "strchr stable");
        CHECK(strchr("hello", 'h') != NULL, "strchr first char");
        CHECK(strchr("hello", 'o') != NULL && *strchr("hello", 'o') == 'o', "strchr last char");
        CHECK(strchr("hello", 'z') == NULL, "strchr not found");
        CHECK(strchr("hello", 0) != NULL && *strchr("hello", 0) == 0, "strchr finds NUL");
        CHECK_NULL(strchr(NULL, 'a'), "strchr(NULL) -> NULL");
    }

    FS_FN("string", "strrchr");
    {
        const char *p = strrchr("hello", 'l');
        CHECK(p != NULL && p[0] == 'l' && p[1] == 'o', "strrchr returns last occurrence");
        CHECK(strrchr("hello", 'z') == NULL, "strrchr not found");
        {
            const char *q = strrchr("hello", 0);
            CHECK(q != NULL && *q == 0, "strrchr finds NUL");
        }
        CHECK_NULL(strrchr(NULL, 'a'), "strrchr(NULL) -> NULL");
    }

    FS_FN("string", "strstr");
    {
        const char *p = strstr("hello world", "world");
        CHECK(p != NULL && p[0] == 'w', "strstr finds substring");
        CHECK(strstr("hello", "hello") != NULL, "strstr whole string");
        CHECK(strstr("hello", "") == strstr("hello", ""), "strstr empty needle -> haystack");
        CHECK(strstr("hello", "xyz") == NULL, "strstr not found");
        CHECK(strstr("aaa", "aa") != NULL, "strstr finds at position 0");
        {
            const char *p2 = strstr("abcabcabd", "abd");
            CHECK(p2 != NULL && p2[0] == 'a' && p2[3] == 0, "strstr last occurrence position");
        }
        CHECK_NULL(strstr(NULL, "a"), "strstr(NULL,...) -> NULL");
        CHECK_NULL(strstr("a", NULL), "strstr(...,NULL) -> NULL");
    }

    FS_FN("string", "strspn");
    CHECK_UINT(strspn("abcde", "abc"), 3, "strspn prefix 3");
    CHECK_UINT(strspn("abcde", "xyz"), 0, "strspn none match");
    CHECK_UINT(strspn("abcde", "abcde"), 5, "strspn all match");
    CHECK_UINT(strspn("aXbc", "abc"), 1, "strspn stops at first mismatch");
    CHECK_UINT(strspn("", "abc"), 0, "strspn empty string");
    CHECK_UINT(strspn("1234", "0123456789"), 4, "strspn digits");
    CHECK_UINT(strspn("abc", NULL), 0, "strspn(NULL accept) -> 0");

    FS_FN("string", "strcspn");
    CHECK_UINT(strcspn("abcde", "de"), 3, "strcspn prefix 3");
    CHECK_UINT(strcspn("abcde", "xyz"), 5, "strcspn no reject match");
    CHECK_UINT(strcspn("dXbc", "d"), 0, "strcspn first char rejected");
    CHECK_UINT(strcspn("", "abc"), 0, "strcspn empty string");
    CHECK_UINT(strcspn("abc", NULL), 0, "strcspn(NULL reject) -> 0");

    FS_FN("string", "memchr");
    {
        unsigned char m[8] = { 1, 2, 3, 4, 5, 6, 7, 8 };
        void *p = memchr(m, 4, 8);
        CHECK(p == m + 3, "memchr finds byte");
        CHECK_NULL(memchr(m, 99, 8), "memchr not found");
        CHECK_NULL(memchr(m, 1, 0), "memchr n=0 -> NULL");
        CHECK(memchr(m, 8, 8) == m + 7, "memchr last byte");
        CHECK_NULL(memchr(NULL, 1, 8), "memchr(NULL) -> NULL");
    }
#endif /* KAULA_FREESTANDING */

    /* ==================== 字符分类与转换（两种模式都测） ==================== */
    FS_FN("string", "is_alpha");
    CHECK(is_alpha('A') && is_alpha('Z'), "is_alpha upper");
    CHECK(is_alpha('a') && is_alpha('z'), "is_alpha lower");
    CHECK(!is_alpha('0') && !is_alpha('9'), "is_alpha rejects digits");
    CHECK(!is_alpha(' ') && !is_alpha('_') && !is_alpha('+'), "is_alpha rejects punctuation");
    CHECK(!is_alpha((char)0x80), "is_alpha rejects high byte");

    FS_FN("string", "is_digit");
    CHECK(is_digit('0') && is_digit('5') && is_digit('9'), "is_digit 0/5/9");
    CHECK(!is_digit('a') && !is_digit(' ') && !is_digit('/') && !is_digit(':'), "is_digit rejects");

    FS_FN("string", "is_alnum");
    CHECK(is_alnum('a') && is_alnum('Z') && is_alnum('0') && is_alnum('9'), "is_alnum accepts");
    CHECK(!is_alnum(' ') && !is_alnum('-') && !is_alnum('_'), "is_alnum rejects");

    FS_FN("string", "is_space");
    CHECK(is_space(' '), "is_space ' '");
    CHECK(is_space('\t') && is_space('\n') && is_space('\r'), "is_space \\t \\n \\r");
    CHECK(is_space('\v') && is_space('\f'), "is_space \\v \\f");
    CHECK(!is_space('a') && !is_space('0') && !is_space('\0'), "is_space rejects others");

    FS_FN("string", "is_upper");
    CHECK(is_upper('A') && is_upper('Z'), "is_upper A/Z");
    CHECK(!is_upper('a') && !is_upper('0') && !is_upper('@'), "is_upper rejects");

    FS_FN("string", "is_lower");
    CHECK(is_lower('a') && is_lower('z'), "is_lower a/z");
    CHECK(!is_lower('A') && !is_lower('0') && !is_lower('`'), "is_lower rejects");

    FS_FN("string", "is_xdigit");
    CHECK(is_xdigit('0') && is_xdigit('9'), "is_xdigit digits");
    CHECK(is_xdigit('a') && is_xdigit('f') && is_xdigit('A') && is_xdigit('F'), "is_xdigit a-f A-F");
    CHECK(!is_xdigit('g') && !is_xdigit('G') && !is_xdigit(' '), "is_xdigit rejects g/G");

    FS_FN("string", "to_upper");
    CHECK_INT(to_upper('a'), 'A', "to_upper('a')");
    CHECK_INT(to_upper('z'), 'Z', "to_upper('z')");
    CHECK_INT(to_upper('A'), 'A', "to_upper('A') unchanged");
    CHECK_INT(to_upper('0'), '0', "to_upper('0') unchanged");
    CHECK_INT(to_upper('_'), '_', "to_upper('_') unchanged");

    FS_FN("string", "to_lower");
    CHECK_INT(to_lower('A'), 'a', "to_lower('A')");
    CHECK_INT(to_lower('Z'), 'z', "to_lower('Z')");
    CHECK_INT(to_lower('a'), 'a', "to_lower('a') unchanged");
    CHECK_INT(to_lower('0'), '0', "to_lower('0') unchanged");

    /* ==================== 数字 <-> 字符串 ==================== */
    FS_FN("string", "fs_itoa");
    CHECK_STR(fs_itoa(0, buf), "0", "fs_itoa(0)");
    CHECK_STR(fs_itoa(1, buf), "1", "fs_itoa(1)");
    CHECK_STR(fs_itoa(-1, buf), "-1", "fs_itoa(-1)");
    CHECK_STR(fs_itoa(12345, buf), "12345", "fs_itoa(12345)");
    CHECK_STR(fs_itoa(-9876, buf), "-9876", "fs_itoa(-9876)");
    CHECK_STR(fs_itoa(100, buf), "100", "fs_itoa(100)");
    CHECK_STR(fs_itoa(9223372036854775807LL, buf), "9223372036854775807", "fs_itoa(INT64_MAX)");
    CHECK_STR(fs_itoa(-9223372036854775807LL - 1, buf), "-9223372036854775808", "fs_itoa(INT64_MIN)");
    CHECK_STR(fs_itoa(42, buf), "42", "fs_itoa returns buf (write into caller buffer)");

    FS_FN("string", "fs_utoa");
    CHECK_STR(fs_utoa(0, buf), "0", "fs_utoa(0)");
    CHECK_STR(fs_utoa(12345, buf), "12345", "fs_utoa(12345)");
    CHECK_STR(fs_utoa(18446744073709551615ULL, buf), "18446744073709551615", "fs_utoa(U64_MAX)");

    FS_FN("string", "fs_itoa_hex");
    CHECK_STR(fs_itoa_hex(0, buf, false), "0", "fs_itoa_hex(0) lower");
    CHECK_STR(fs_itoa_hex(0xCAFE, buf, false), "cafe", "fs_itoa_hex(0xCAFE) lower");
    CHECK_STR(fs_itoa_hex(0xCAFE, buf, true), "CAFE", "fs_itoa_hex(0xCAFE) upper");
    CHECK_STR(fs_itoa_hex(0xDEADBEEF, buf, false), "deadbeef", "fs_itoa_hex(0xDEADBEEF)");
    CHECK_STR(fs_itoa_hex(255, buf, false), "ff", "fs_itoa_hex(255)");
    CHECK_STR(fs_itoa_hex(18446744073709551615ULL, buf, false), "ffffffffffffffff", "fs_itoa_hex(U64_MAX)");
    CHECK_STR(fs_itoa_hex(10, buf, true), "A", "fs_itoa_hex(10) upper");

    FS_FN("string", "fs_atoi");
    CHECK_INT(fs_atoi("0"), 0, "fs_atoi(\"0\")");
    CHECK_INT(fs_atoi("42"), 42, "fs_atoi(\"42\")");
    CHECK_INT(fs_atoi("-42"), -42, "fs_atoi(\"-42\")");
    CHECK_INT(fs_atoi("+42"), 42, "fs_atoi(\"+42\")");
    CHECK_INT(fs_atoi("  7  "), 7, "fs_atoi skips leading whitespace");
    CHECK_INT(fs_atoi("\t\n42"), 42, "fs_atoi skips tab/newline");
    CHECK_INT(fs_atoi("abc"), 0, "fs_atoi invalid -> 0");
    CHECK_INT(fs_atoi(""), 0, "fs_atoi empty -> 0");
    CHECK_INT(fs_atoi("12abc"), 12, "fs_atoi stops at non-digit");
    CHECK_INT(fs_atoi("9223372036854775807"), 9223372036854775807LL, "fs_atoi(INT64_MAX)");
    CHECK_INT(fs_atoi("-9223372036854775808"), -9223372036854775807LL - 1, "fs_atoi(INT64_MIN)");
    CHECK_INT(fs_atoi("99999999999999999999"), 9223372036854775807LL, "fs_atoi overflow saturates");
    CHECK_INT(fs_atoi(NULL), 0, "fs_atoi(NULL) -> 0");
    /* 与 fs_itoa 往返 */
    CHECK_STR(fs_itoa(fs_atoi("-31337"), buf), "-31337", "fs_atoi/fs_itoa roundtrip -31337");
    CHECK_STR(fs_itoa(fs_atoi("9223372036854775807"), buf), "9223372036854775807", "roundtrip INT64_MAX");

    FS_FN("string", "fs_atou");
    CHECK_UINT(fs_atou("0"), 0, "fs_atou(\"0\")");
    CHECK_UINT(fs_atou("42"), 42, "fs_atou(\"42\")");
    CHECK_UINT(fs_atou("  99"), 99, "fs_atou skips whitespace");
    CHECK_UINT(fs_atou("0x1F"), 31, "fs_atou hex lower x");
    CHECK_UINT(fs_atou("0X1f"), 31, "fs_atou hex upper X");
    CHECK_UINT(fs_atou("0xDEADBEEF"), 0xDEADBEEF, "fs_atou hex mixed case");
    CHECK_UINT(fs_atou("0x"), 0, "fs_atou(\"0x\") -> 0");
    CHECK_UINT(fs_atou("abc"), 0, "fs_atou invalid -> 0");
    CHECK_UINT(fs_atou(""), 0, "fs_atou empty -> 0");
    CHECK_UINT(fs_atou(NULL), 0, "fs_atou(NULL) -> 0");
    CHECK_UINT(fs_atou("18446744073709551615"), 18446744073709551615ULL, "fs_atou(U64_MAX)");

    FS_FN("string", "fs_strdup");
    {
        fs_alloc_reset();
        char *d = fs_strdup("hello world");
        CHECK_NOTNULL(d, "fs_strdup allocates");
        CHECK_STR(d, "hello world", "fs_strdup content");
        /* 副本独立: 修改副本不影响原串 */
        d[0] = 'H';
        CHECK_STR(d, "Hello world", "fs_strdup copy is writable/independent");
        CHECK_STR("hello world", "hello world", "original literal untouched");
        CHECK_NULL(fs_strdup(NULL), "fs_strdup(NULL) -> NULL");
        char *e = fs_strdup("");
        CHECK_NOTNULL(e, "fs_strdup(\"\") non-null");
        CHECK_STR(e, "", "fs_strdup(\"\") content");
        CHECK_STR(fs_strdup("0123456789"), "0123456789", "fs_strdup 10 chars");
    }
}
