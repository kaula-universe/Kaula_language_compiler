/* Standalone tests linked against the real Kaula runtime, no allocator mocks. */
#include "../std/format/format.h"
#include <assert.h>
#include <float.h>
#include <limits.h>
#include <math.h>
#include <stdio.h>
#include <string.h>

static unsigned checks;
#define CHECK(expr) do { ++checks; assert(expr); } while (0)
#define TEXT(call, expected) do { int n = (call); CHECK(n == (int)strlen(expected)); CHECK(strcmp(b, expected) == 0); } while (0)

static void truncation(void) {
    const char* expected = "-0X000002A";
    FormatOptions o = {0};
    o.width = 10; o.zero_pad = true; o.show_base = true; o.uppercase = true;
    for (size_t capacity = 1; capacity <= 12; ++capacity) {
        unsigned char guarded[32];
        memset(guarded, 0xA5, sizeof(guarded));
        char* b = (char*)guarded + 1;
        int n = format_int_opts(b, capacity, -42, 16, &o);
        size_t want = strlen(expected) < capacity - 1 ? strlen(expected) : capacity - 1;
        CHECK(n == (int)want);
        CHECK(memcmp(b, expected, want) == 0);
        CHECK(b[want] == 0);
        CHECK(guarded[0] == 0xA5 && guarded[capacity + 1] == 0xA5);
    }
    char b[64];
    TEXT(format_int(b, 4, -123456, 10), "-12");
    TEXT(format_uint(b, 4, UINT64_MAX, 10), "184");
    TEXT(format_float(b, 5, 12345.67, 2), "1234");
    TEXT(format_printf(b, 4, "%s", "abcdef"), "abc");
    char single[2] = {'x', 'z'};
    CHECK(format_char(single, 1, 'a') == 0 && single[0] == 0 && single[1] == 'z');
    o.width = INT_MAX;
    TEXT(format_int_opts(b, 4, -42, 16, &o), "-0X");
    CHECK(format_int_opts(NULL, 4, 1, 10, &o) == -1);
    CHECK(format_int_opts(b, 0, 1, 10, &o) == -1);
    CHECK(format_int_opts(b, sizeof(b), 1, 37, &o) == -1 && b[0] == 0);
}

static void integers(void) {
    char b[256];
    FormatOptions o = {0};
    TEXT(format_int(b, sizeof(b), INT64_MIN, 10), "-9223372036854775808");
    TEXT(format_uint(b, sizeof(b), UINT64_MAX, 10), "18446744073709551615");
    TEXT(format_uint(b, sizeof(b), 255, 16), "FF");
    TEXT(format_uint_opts(b, sizeof(b), 255, 16, &o), "ff");
    TEXT(format_int_opts(b, sizeof(b), 35, 36, &o), "z");
    o.show_base = true; o.show_sign = true;
    TEXT(format_int_opts(b, sizeof(b), 42, 16, &o), "+0x2a");
    TEXT(format_uint_opts(b, sizeof(b), 5, 2, &o), "+0b101");
    TEXT(format_int_opts(b, sizeof(b), -8, 8, &o), "-0o10");
    o.uppercase = true;
    TEXT(format_uint_opts(b, sizeof(b), 0, 16, &o), "+0X0");
    o.width = 10; o.zero_pad = true; o.pad_char = '.';
    TEXT(format_int_opts(b, sizeof(b), -42, 16, &o), "-0X000002A");
    o.left_align = true;
    TEXT(format_int_opts(b, sizeof(b), -42, 16, &o), "-0X2A.....");
    o.left_align = false; o.zero_pad = false;
    TEXT(format_int_opts(b, sizeof(b), -42, 16, &o), ".....-0X2A");
    o.width = 0; o.thousands = true;
    TEXT(format_int_opts(b, sizeof(b), INT64_MIN, 10, &o), "-9,223,372,036,854,775,808");
    TEXT(format_uint_opts(b, sizeof(b), UINT64_MAX, 10, &o), "+18,446,744,073,709,551,615");
    TEXT(format_uint_opts(b, sizeof(b), 65535, 16, &o), "+0XFFFF");
    o.show_sign = false; o.show_base = false;
    TEXT(format_int_opts(b, sizeof(b), INT64_MIN, 2, &o), "-1000000000000000000000000000000000000000000000000000000000000000");
    for (int base = 2; base <= 36; ++base) {
        int n = format_uint_opts(b, sizeof(b), UINT64_MAX, base, NULL);
        CHECK(n > 0);
        for (size_t cap = 1; cap <= (size_t)n + 1; ++cap) {
            char tiny[128];
            int got = format_uint_opts(tiny, cap, UINT64_MAX, base, NULL);
            CHECK(got == (int)cap - 1 && memcmp(tiny, b, cap - 1) == 0 && tiny[cap - 1] == 0);
        }
    }
}

static void floats(void) {
    char b[1024], expected[1024];
    FormatOptions o = {0};
    o.precision = 2; o.thousands = true; o.show_sign = true;
    TEXT(format_float_opts(b, sizeof(b), 1234567.125, &o), "+1,234,567.12");
    TEXT(format_float_opts(b, sizeof(b), -0.0, &o), "-0.00");
    o.scientific = true; o.uppercase = true;
    TEXT(format_float_opts(b, sizeof(b), 1250.0, &o), "+1.25E+03");
    TEXT(format_float_opts(b, sizeof(b), INFINITY, &o), "+INF");
    TEXT(format_float_opts(b, sizeof(b), -INFINITY, &o), "-INF");
    CHECK(format_float_opts(b, sizeof(b), NAN, &o) > 0 && strstr(b, "NAN"));
    o.uppercase = false; o.show_sign = false;
    TEXT(format_float_opts(b, sizeof(b), 0.00125, &o), "1.25e-03");
    o.scientific = false; o.thousands = false; o.precision = -1;
    TEXT(format_float_opts(b, sizeof(b), 1.25, &o), "1.250000");
    o.precision = 100;
    TEXT(format_float_opts(b, sizeof(b), 1.25, &o), "1.25000000000000000000");
    for (int grouped = 0; grouped < 2; ++grouped) {
        o.thousands = grouped; o.precision = 20;
        int full = format_float_opts(expected, sizeof(expected), -DBL_MAX, &o);
        CHECK(full > 310 && full == (int)strlen(expected));
        for (size_t cap = 1; cap <= (size_t)full + 1; ++cap) {
            memset(b, '!', sizeof(b));
            int n = format_float_opts(b, cap, -DBL_MAX, &o);
            CHECK(n == (int)cap - 1 && memcmp(b, expected, cap - 1) == 0);
            CHECK(b[cap - 1] == 0 && b[cap] == '!');
        }
    }
    o.thousands = false;
    snprintf(expected, sizeof(expected), "%.20f", DBL_MAX);
    CHECK(format_float_opts(b, sizeof(b), DBL_MAX, &o) == (int)strlen(expected));
    CHECK(strcmp(b, expected) == 0);
    FormatBuilder* fb = format_builder_create(8);
    CHECK(fb != NULL);
    format_builder_append_float(fb, DBL_MAX, 20);
    CHECK(strcmp(format_builder_get(fb), expected) == 0);
    CHECK(format_builder_length(fb) == strlen(expected));
    format_builder_destroy(fb);
}

static void kaula_api(void) {
    FormatOptions* o = format_options_create();
    CHECK(o != NULL && o->precision == 6);
    format_options_set_layout(o, 12, false, true, 46);
    format_options_set_flags(o, true, true, true, false, false);
    CHECK(strcmp(format_uint_text(255, 16, o), "+0X0000000FF") == 0);
    CHECK(strcmp(format_int_text(-255, 16, o), "-0X0000000FF") == 0);
    format_options_set_layout(o, 0, false, false, 0);
    format_options_set_precision(o, 2);
    format_options_set_flags(o, false, false, false, true, false);
    CHECK(strcmp(format_float_text(1234.5, o), "1,234.50") == 0);
    CHECK(format_int_text(1, 1, o) == NULL);
    format_options_set_layout(NULL, 1, false, false, 32);
    format_options_set_precision(NULL, 3);
    format_options_set_flags(NULL, false, false, false, false, false);
}

int main(void) {
    truncation(); integers(); floats(); kaula_api();
    printf("format options: %u checks passed\n", checks);
    return 0;
}
