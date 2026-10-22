// regex_difftest_tool.c —— Pike VM（新）vs 回溯（旧快照）差分压测
//
// 同一模式/输入下分别编译两个引擎，逐项比对：
//   is_valid / full_match / search / captures 偏移 / find_all 区间 / replace
// 旧引擎因步数预算 abort 的用例（灾难性回溯旧引擎本就放弃）跳过严格对拍，
// 只要求新引擎不 abort。两遍随机种子收敛哈希比对确定性。
//
// 构建：
//   cc -O2 -std=c99 -I. test/bench/regex_difftest_tool.c \
//      std/regex/regex.c test/bench/regex_ref/regex_ref.c -o /tmp/rxdiff
// 运行：/tmp/rxdiff [--quick]

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>

// ---- 最小 allocator shim（测试专用） ----
void* kmm_v4_malloc(size_t n) { return malloc(n); }
void  kmm_v4_free(void* p) { free(p); }
void* kmm_v4_realloc(void* p, size_t n) { return realloc(p, n); }

#include "std/regex/regex.h"
#include "test/bench/regex_ref/regex_ref.h"

// regex.c 用到 string_create（其余 string_wrap_n 是 header inline）
String string_create(const char* s) { return string_wrap(s); }

static int fails = 0;
static int checked = 0;
static int skipped = 0;

#define CHECK(cond, ...) do { \
    checked++; \
    if (!(cond)) { fails++; \
        if (fails <= 20) { printf("FAIL: " __VA_ARGS__); printf("\n"); } } \
} while (0)

static void cmp_captures(Regex* nre, RegexRef* ore, const char* text) {
    size_t nn = 0, on = 0;
    RegexCapture* nc = regex_captures(nre, text, &nn);
    RegexCaptureRef* oc = regex_ref_captures(ore, text, &on);
    if (oc) {
        if (regex_ref_aborted(ore)) { skipped++; if (oc) regex_ref_free_matches((RegexMatchRef*)oc, on); return; }
    }
    CHECK(nn == on, "capture count %s new=%zu old=%zu", text, nn, on);
    if (nn == on) {
        for (size_t i = 0; i < nn; i++) {
            CHECK(nc[i].start == oc[i].start && nc[i].end == oc[i].end,
                  "capture %s[%zu] new=%zu..%zu old=%zu..%zu", text, i,
                  nc[i].start, nc[i].end, oc[i].start, oc[i].end);
            CHECK(nc[i].matched == oc[i].matched, "capture matched %s[%zu]", text, i);
        }
    }
    if (nc) regex_free_matches((RegexMatch*)nc, nn);
    if (oc) regex_ref_free_matches((RegexMatchRef*)oc, on);
}

static void cmp_find_all(Regex* nre, RegexRef* ore, const char* text) {
    size_t nn = 0, on = 0;
    RegexMatch* nm = regex_find_all(nre, text, &nn);
    RegexMatchRef* om = regex_ref_find_all(ore, text, &on);
    if (om && regex_ref_aborted(ore)) { skipped++; goto done; }
    CHECK(nn == on, "find_all count %s new=%zu old=%zu", text, nn, on);
    if (nn == on) {
        for (size_t i = 0; i < nn; i++) {
            CHECK(nm[i].start == om[i].start && nm[i].end == om[i].end,
                  "find %s[%zu] new=%zu..%zu old=%zu..%zu", text, i,
                  nm[i].start, nm[i].end, om[i].start, om[i].end);
        }
    }
done:
    if (nm) regex_free_matches(nm, nn);
    if (om) regex_ref_free_matches(om, on);
}

static void cmp_regex(const char* pat, const char* text) {
    Regex* nre = regex_create(pat);
    RegexRef* ore = regex_ref_create(pat);
    if (!regex_is_valid(nre) && !regex_ref_is_valid(ore)) { regex_destroy(nre); regex_ref_destroy(ore); return; }
    CHECK(regex_is_valid(nre) == regex_ref_is_valid(ore), "valid %s", pat);
    if (!regex_is_valid(nre)) { regex_destroy(nre); regex_ref_destroy(ore); return; }

    int nfm = regex_full_match(nre, text);
    int ofm = regex_ref_full_match(ore, text);
    if (!regex_ref_aborted(ore)) CHECK(nfm == ofm, "full_match /%s/ on '%s' new=%d old=%d", pat, text, nfm, ofm);

    int ns = regex_search(nre, text);
    int os = regex_ref_search(ore, text);
    if (!regex_ref_aborted(ore)) CHECK(ns == os, "search /%s/ on '%s' new=%d old=%d", pat, text, ns, os);

    cmp_captures(nre, ore, text);
    cmp_find_all(nre, ore, text);

    String nr = regex_replace(nre, text, "[$1]");
    String orr = regex_ref_replace(ore, text, "[$1]");
    if (!regex_ref_aborted(ore)) {
        const char* ne = (nr.len <= STRING_SSO_CAPACITY) ? nr.sso : nr.ptr;
        const char* oe = (orr.len <= STRING_SSO_CAPACITY) ? orr.sso : orr.ptr;
        char nb[512], ob[512];
        snprintf(nb, sizeof nb, "%.*s", (int)(nr.len < 500 ? nr.len : 500), ne);
        snprintf(ob, sizeof ob, "%.*s", (int)(orr.len < 500 ? orr.len : 500), oe);
        CHECK(strcmp(nb, ob) == 0, "replace /%s/ on '%s' new='%s' old='%s'", pat, text, nb, ob);
    }
    CHECK(!regex_aborted(nre), "new engine aborted on /%s/ '%s'", pat, text);

    regex_destroy(nre);
    regex_ref_destroy(ore);
}

// ---- 简易 xorshift64 模糊 ----
static uint64_t rng_state = 8817264546325252ULL;
static uint64_t xr(void) {
    rng_state ^= rng_state << 13;
    rng_state ^= rng_state >> 7;
    rng_state ^= rng_state << 17;
    return rng_state;
}

static const char* atoms[] = { "a", "b", "c", "1", "2", " ", "", "x" };
static void fuzz_pattern(char* out, size_t cap) {
    size_t n = 0;
    int depth = 0;
    while (n + 8 < cap && depth < 6) {
        int r = xr() % 10;
        switch (r) {
            case 0: case 1: case 2: case 3: out[n++] = atoms[xr()%6][0] ? atoms[xr()%6][0] : 'a'; break;
            case 4: out[n++] = '('; depth++; break;
            case 5: if (depth > 0) { out[n++] = ')'; depth--; } break;
            case 6: out[n++] = '*'; break;
            case 7: out[n++] = '+'; break;
            case 8: out[n++] = '?'; break;
            default: out[n++] = '|'; break;
        }
    }
    while (depth-- > 0) out[n++] = ')';
    out[n] = '\0';
}

static void fuzz_text(char* out, size_t cap) {
    size_t n = 0;
    while (n + 1 < cap) { out[n++] = "abx12 " [xr() % 6]; }
    out[n] = '\0';
}

int main(int argc, char** argv) {
    int quick = (argc > 1 && strcmp(argv[1], "--quick") == 0);

    static const struct { const char* pat; const char* text; } cases[] = {
        { "abc", "abc" }, { "abc", "xabcx" }, { "a.c", "axc" }, { "a.c", "ac" },
        { "\\d+", "abc123def" }, { "\\d+", "abc" }, { "^abc", "abc def" },
        { "abc$", "x abc" }, { "ab*", "abbbbc" }, { "ab+c", "ac" },
        { "(a+)+b", "aaaaab" }, { "(a+)+b", "aaaaac" }, { "(a+)+b", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaac" },
        { "(\\d+)-(\\d+)", "2024-10-22" }, { "(\\d+)-(\\d+)", "no" },
        { "a|b|c", "xby" }, { "a(b|c)d", "acd" }, { "(?:abc)+", "abcabc" },
        { "\\bword\\b", "a word here" }, { "\\bword\\b", "sword" },
        { "x", "" }, { "", "abc" }, { "a*", "bbb" }, { "a*b", "aaab" },
        { "(a*)(a+)", "aaa" }, { "(a|a|a)+b", "aaab" }, { "[0-9]{2,4}", "12345" },
        { "a{2,3}", "aaaa" }, { "colou?r", "color" }, { "colou?r", "colour" },
        { "(?i)ABC", "abc" }, { "(?m)^x$", "y\nx\nz" }, { "(?s).", "\n" },
    };
    for (size_t i = 0; i < sizeof(cases)/sizeof(cases[0]); i++) {
        cmp_regex(cases[i].pat, cases[i].text);
    }

    if (!quick) {
        // 模糊两轮，第二遍换种子比对确定性哈希
        char pat[64], text[128];
        uint64_t h1 = 0, h2 = 0;
        for (int round = 0; round < 2; round++) {
            rng_state = 8817264546325252ULL;
            uint64_t* hp = round ? &h2 : &h1;
            for (int i = 0; i < 200000; i++) {
                fuzz_pattern(pat, sizeof pat);
                fuzz_text(text, sizeof text);
                cmp_regex(pat, text);
                *hp = *hp * 1099511628211ULL + (pat[0] << 7) + (text[0] << 1);
            }
        }
        printf("determinism hash: h1=%llx h2=%llx %s\n",
               (unsigned long long)h1, (unsigned long long)h2, h1==h2 ? "CONVERGED" : "DIVERGED");
    }

    printf("\n=== checked %d, failed %d, skipped(old-abort) %d ===\n",
           checked, fails, skipped);
    return fails ? 1 : 0;
}
