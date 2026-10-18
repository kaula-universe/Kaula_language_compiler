#include "unicode.h"
#include "../memory/memory.h"
#include "../string/string.h"
#include <string.h>
#include <stdlib.h>

/* ============================================================
 * Unicode implementation
 * Pure C, no external Unicode library dependency.
 * Covers ASCII (0-127), Latin-1 Supplement (128-255),
 * Latin Extended-A/B, Greek, and common ranges.
 * ============================================================ */

#define REPLACEMENT_CHAR 0xFFFDu

/* ============================================================
 * ASCII category table (0-127)
 * ============================================================ */
static const UnicodeCategory ascii_category[128] = {
    /* 0x00 */ UNICODE_CATEGORY_CC, UNICODE_CATEGORY_CC,
    /* 0x02 */ UNICODE_CATEGORY_CC, UNICODE_CATEGORY_CC,
    /* 0x04 */ UNICODE_CATEGORY_CC, UNICODE_CATEGORY_CC,
    /* 0x06 */ UNICODE_CATEGORY_CC, UNICODE_CATEGORY_CC,
    /* 0x08 */ UNICODE_CATEGORY_CC, UNICODE_CATEGORY_CC,
    /* 0x0A */ UNICODE_CATEGORY_CC, UNICODE_CATEGORY_CC,
    /* 0x0C */ UNICODE_CATEGORY_CC, UNICODE_CATEGORY_CC,
    /* 0x0E */ UNICODE_CATEGORY_CC, UNICODE_CATEGORY_CC,
    /* 0x10 */ UNICODE_CATEGORY_CC, UNICODE_CATEGORY_CC,
    /* 0x12 */ UNICODE_CATEGORY_CC, UNICODE_CATEGORY_CC,
    /* 0x14 */ UNICODE_CATEGORY_CC, UNICODE_CATEGORY_CC,
    /* 0x16 */ UNICODE_CATEGORY_CC, UNICODE_CATEGORY_CC,
    /* 0x18 */ UNICODE_CATEGORY_CC, UNICODE_CATEGORY_CC,
    /* 0x1A */ UNICODE_CATEGORY_CC, UNICODE_CATEGORY_CC,
    /* 0x1C */ UNICODE_CATEGORY_CC, UNICODE_CATEGORY_CC,
    /* 0x1E */ UNICODE_CATEGORY_CC, UNICODE_CATEGORY_CC,
    /* 0x20 */ UNICODE_CATEGORY_ZS, UNICODE_CATEGORY_PO,
    /* 0x22 */ UNICODE_CATEGORY_PO, UNICODE_CATEGORY_PO,
    /* 0x24 */ UNICODE_CATEGORY_SC, UNICODE_CATEGORY_PO,
    /* 0x26 */ UNICODE_CATEGORY_PO, UNICODE_CATEGORY_PO,
    /* 0x28 */ UNICODE_CATEGORY_PS, UNICODE_CATEGORY_PE,
    /* 0x2A */ UNICODE_CATEGORY_PO, UNICODE_CATEGORY_SM,
    /* 0x2C */ UNICODE_CATEGORY_PO, UNICODE_CATEGORY_PD,
    /* 0x2E */ UNICODE_CATEGORY_PO, UNICODE_CATEGORY_PO,
    /* 0x30 */ UNICODE_CATEGORY_ND, UNICODE_CATEGORY_ND,
    /* 0x32 */ UNICODE_CATEGORY_ND, UNICODE_CATEGORY_ND,
    /* 0x34 */ UNICODE_CATEGORY_ND, UNICODE_CATEGORY_ND,
    /* 0x36 */ UNICODE_CATEGORY_ND, UNICODE_CATEGORY_ND,
    /* 0x38 */ UNICODE_CATEGORY_ND, UNICODE_CATEGORY_ND,
    /* 0x3A */ UNICODE_CATEGORY_PO, UNICODE_CATEGORY_PO,
    /* 0x3C */ UNICODE_CATEGORY_SM, UNICODE_CATEGORY_SM,
    /* 0x3E */ UNICODE_CATEGORY_SM, UNICODE_CATEGORY_PO,
    /* 0x40 */ UNICODE_CATEGORY_PO, UNICODE_CATEGORY_LU,
    /* 0x42 */ UNICODE_CATEGORY_LU, UNICODE_CATEGORY_LU,
    /* 0x44 */ UNICODE_CATEGORY_LU, UNICODE_CATEGORY_LU,
    /* 0x46 */ UNICODE_CATEGORY_LU, UNICODE_CATEGORY_LU,
    /* 0x48 */ UNICODE_CATEGORY_LU, UNICODE_CATEGORY_LU,
    /* 0x4A */ UNICODE_CATEGORY_LU, UNICODE_CATEGORY_LU,
    /* 0x4C */ UNICODE_CATEGORY_LU, UNICODE_CATEGORY_LU,
    /* 0x4E */ UNICODE_CATEGORY_LU, UNICODE_CATEGORY_LU,
    /* 0x50 */ UNICODE_CATEGORY_LU, UNICODE_CATEGORY_LU,
    /* 0x52 */ UNICODE_CATEGORY_LU, UNICODE_CATEGORY_LU,
    /* 0x54 */ UNICODE_CATEGORY_LU, UNICODE_CATEGORY_LU,
    /* 0x56 */ UNICODE_CATEGORY_LU, UNICODE_CATEGORY_LU,
    /* 0x58 */ UNICODE_CATEGORY_LU, UNICODE_CATEGORY_LU,
    /* 0x5A */ UNICODE_CATEGORY_LU, UNICODE_CATEGORY_PS,
    /* 0x5C */ UNICODE_CATEGORY_PO, UNICODE_CATEGORY_PE,
    /* 0x5E */ UNICODE_CATEGORY_SK, UNICODE_CATEGORY_PC,
    /* 0x60 */ UNICODE_CATEGORY_SK, UNICODE_CATEGORY_LL,
    /* 0x62 */ UNICODE_CATEGORY_LL, UNICODE_CATEGORY_LL,
    /* 0x64 */ UNICODE_CATEGORY_LL, UNICODE_CATEGORY_LL,
    /* 0x66 */ UNICODE_CATEGORY_LL, UNICODE_CATEGORY_LL,
    /* 0x68 */ UNICODE_CATEGORY_LL, UNICODE_CATEGORY_LL,
    /* 0x6A */ UNICODE_CATEGORY_LL, UNICODE_CATEGORY_LL,
    /* 0x6C */ UNICODE_CATEGORY_LL, UNICODE_CATEGORY_LL,
    /* 0x6E */ UNICODE_CATEGORY_LL, UNICODE_CATEGORY_LL,
    /* 0x70 */ UNICODE_CATEGORY_LL, UNICODE_CATEGORY_LL,
    /* 0x72 */ UNICODE_CATEGORY_LL, UNICODE_CATEGORY_LL,
    /* 0x74 */ UNICODE_CATEGORY_LL, UNICODE_CATEGORY_LL,
    /* 0x76 */ UNICODE_CATEGORY_LL, UNICODE_CATEGORY_LL,
    /* 0x78 */ UNICODE_CATEGORY_LL, UNICODE_CATEGORY_LL,
    /* 0x7A */ UNICODE_CATEGORY_LL, UNICODE_CATEGORY_PS,
    /* 0x7C */ UNICODE_CATEGORY_SM, UNICODE_CATEGORY_PE,
    /* 0x7E */ UNICODE_CATEGORY_SK, UNICODE_CATEGORY_CC
};

/* ============================================================
 * Latin-1 Supplement category table (128-255)
 * ============================================================ */
static UnicodeCategory latin1_category(u32 cp) {
    switch (cp) {
        case 0x00A0: return UNICODE_CATEGORY_ZS;
        case 0x00A1: return UNICODE_CATEGORY_PO;
        case 0x00A2: case 0x00A3: case 0x00A4: case 0x00A5:
        case 0x00A6: case 0x00A7: case 0x00A8: case 0x00A9:
        case 0x00AA: case 0x00AB: case 0x00AC: case 0x00AD:
        case 0x00AE: case 0x00AF:
            return UNICODE_CATEGORY_SO;
        case 0x00B0: case 0x00B1: case 0x00B2: case 0x00B3:
        case 0x00B4: case 0x00B5: case 0x00B6: case 0x00B7:
        case 0x00B8: case 0x00B9: case 0x00BA: case 0x00BB:
        case 0x00BC: case 0x00BD: case 0x00BE:
            return UNICODE_CATEGORY_SO;
        case 0x00BF: return UNICODE_CATEGORY_PO;
        case 0x00D7: return UNICODE_CATEGORY_SM;
        case 0x00F7: return UNICODE_CATEGORY_SM;
        default: break;
    }
    /* Uppercase letters */
    if ((cp >= 0x00C0 && cp <= 0x00C5) ||
        (cp >= 0x00C8 && cp <= 0x00CF) ||
        cp == 0x00D0 || cp == 0x00DE ||
        cp == 0x00C7 || cp == 0x00D1) {
        return UNICODE_CATEGORY_LU;
    }
    /* Lowercase letters */
    if ((cp >= 0x00E0 && cp <= 0x00E5) ||
        (cp >= 0x00E8 && cp <= 0x00EF) ||
        cp == 0x00F0 || cp == 0x00FE ||
        cp == 0x00E7 || cp == 0x00F1 ||
        (cp >= 0x00F2 && cp <= 0x00F6) ||
        (cp >= 0x00F8 && cp <= 0x00FF) ||
        cp == 0x00DF) {
        return UNICODE_CATEGORY_LL;
    }
    /* Titlecase letter eth/thorn handled above */
    return UNICODE_CATEGORY_SO;
}

/* ============================================================
 * Latin Extended-A case mapping (0x0100-0x017F)
 * ============================================================ */
typedef struct {
    u32 upper;
    u32 lower;
} CasePair;

static const CasePair latin_ext_a[] = {
    {0x0100, 0x0101}, {0x0102, 0x0103}, {0x0104, 0x0105},
    {0x0106, 0x0107}, {0x0108, 0x0109}, {0x010A, 0x010B},
    {0x010C, 0x010D}, {0x010E, 0x010F}, {0x0110, 0x0111},
    {0x0112, 0x0113}, {0x0114, 0x0115}, {0x0116, 0x0117},
    {0x0118, 0x0119}, {0x011A, 0x011B}, {0x011C, 0x011D},
    {0x011E, 0x011F}, {0x0120, 0x0121}, {0x0122, 0x0123},
    {0x0124, 0x0125}, {0x0126, 0x0127}, {0x0128, 0x0129},
    {0x012A, 0x012B}, {0x012C, 0x012D}, {0x012E, 0x012F},
    {0x0130, 0x0069}, {0x0132, 0x0133}, {0x0134, 0x0135},
    {0x0136, 0x0137}, {0x0139, 0x013A}, {0x013B, 0x013C},
    {0x013D, 0x013E}, {0x013F, 0x0140}, {0x0141, 0x0142},
    {0x0143, 0x0144}, {0x0145, 0x0146}, {0x0147, 0x0148},
    {0x014A, 0x014B}, {0x014C, 0x014D}, {0x014E, 0x014F},
    {0x0150, 0x0151}, {0x0152, 0x0153}, {0x0154, 0x0155},
    {0x0156, 0x0157}, {0x0158, 0x0159}, {0x015A, 0x015B},
    {0x015C, 0x015D}, {0x015E, 0x015F}, {0x0160, 0x0161},
    {0x0162, 0x0163}, {0x0164, 0x0165}, {0x0166, 0x0167},
    {0x0168, 0x0169}, {0x016A, 0x016B}, {0x016C, 0x016D},
    {0x016E, 0x016F}, {0x0170, 0x0171}, {0x0172, 0x0173},
    {0x0174, 0x0175}, {0x0176, 0x0177}, {0x0178, 0x00FF},
    {0x0179, 0x017A}, {0x017B, 0x017C}, {0x017D, 0x017E},
    {0x017F, 0x017F}
};

static const size_t latin_ext_a_count =
    sizeof(latin_ext_a) / sizeof(latin_ext_a[0]);

/* ============================================================
 * Greek case mapping (0x0391-0x03C9)
 * ============================================================ */
static const CasePair greek_case[] = {
    {0x0391, 0x03B1}, {0x0392, 0x03B2}, {0x0393, 0x03B3},
    {0x0394, 0x03B4}, {0x0395, 0x03B5}, {0x0396, 0x03B6},
    {0x0397, 0x03B7}, {0x0398, 0x03B8}, {0x0399, 0x03B9},
    {0x039A, 0x03BA}, {0x039B, 0x03BB}, {0x039C, 0x03BC},
    {0x039D, 0x03BD}, {0x039E, 0x03BE}, {0x039F, 0x03BF},
    {0x03A0, 0x03C0}, {0x03A1, 0x03C1}, {0x03A3, 0x03C3},
    {0x03A4, 0x03C4}, {0x03A5, 0x03C5}, {0x03A6, 0x03C6},
    {0x03A7, 0x03C7}, {0x03A8, 0x03C8}, {0x03A9, 0x03C9}
};

static const size_t greek_case_count =
    sizeof(greek_case) / sizeof(greek_case[0]);

static u32 lookup_to_upper(u32 cp, const CasePair* table, size_t n) {
    size_t i;
    for (i = 0; i < n; i++) {
        if (table[i].lower == cp) return table[i].upper;
    }
    return cp;
}

static u32 lookup_to_lower(u32 cp, const CasePair* table, size_t n) {
    size_t i;
    for (i = 0; i < n; i++) {
        if (table[i].upper == cp) return table[i].lower;
    }
    return cp;
}

/* ============================================================
 * Codepoint properties
 * ============================================================ */

UnicodeCategory unicode_category(u32 codepoint) {
    if (codepoint < 128) {
        return ascii_category[codepoint];
    }
    if (codepoint <= 0x00FF) {
        return latin1_category(codepoint);
    }
    /* Latin Extended-A */
    if (codepoint >= 0x0100 && codepoint <= 0x017F) {
        if (codepoint % 2 == 0) return UNICODE_CATEGORY_LU;
        return UNICODE_CATEGORY_LL;
    }
    /* Latin Extended-B */
    if (codepoint >= 0x0180 && codepoint <= 0x024F) {
        return UNICODE_CATEGORY_LL;
    }
    /* IPA Extensions */
    if (codepoint >= 0x0250 && codepoint <= 0x02AF) {
        return UNICODE_CATEGORY_LL;
    }
    /* Spacing Modifier Letters */
    if (codepoint >= 0x02B0 && codepoint <= 0x02FF) {
        return UNICODE_CATEGORY_LM;
    }
    /* Combining Diacritical Marks */
    if (codepoint >= 0x0300 && codepoint <= 0x036F) {
        return UNICODE_CATEGORY_MN;
    }
    /* Greek and Coptic */
    if (codepoint >= 0x0370 && codepoint <= 0x03FF) {
        if (codepoint >= 0x0391 && codepoint <= 0x03A9) {
            if (codepoint == 0x03A2) return UNICODE_CATEGORY_CN;
            return UNICODE_CATEGORY_LU;
        }
        if (codepoint >= 0x03B1 && codepoint <= 0x03C9) {
            return UNICODE_CATEGORY_LL;
        }
        if (codepoint == 0x037A || codepoint == 0x037E ||
            codepoint == 0x0387) {
            return UNICODE_CATEGORY_PO;
        }
        if (codepoint == 0x0374 || codepoint == 0x0375) {
            return UNICODE_CATEGORY_SK;
        }
        if (codepoint >= 0x0386 && codepoint <= 0x03CE) {
            return UNICODE_CATEGORY_LL;
        }
        return UNICODE_CATEGORY_SO;
    }
    /* Cyrillic */
    if (codepoint >= 0x0400 && codepoint <= 0x04FF) {
        if (codepoint >= 0x0410 && codepoint <= 0x042F) {
            return UNICODE_CATEGORY_LU;
        }
        if (codepoint >= 0x0430 && codepoint <= 0x044F) {
            return UNICODE_CATEGORY_LL;
        }
        if (codepoint <= 0x040F) return UNICODE_CATEGORY_LU;
        return UNICODE_CATEGORY_LL;
    }
    /* General Punctuation */
    if (codepoint >= 0x2000 && codepoint <= 0x200B) {
        return UNICODE_CATEGORY_ZS;
    }
    if (codepoint >= 0x200C && codepoint <= 0x200F) {
        return UNICODE_CATEGORY_CF;
    }
    if (codepoint >= 0x2010 && codepoint <= 0x2027) {
        return UNICODE_CATEGORY_PD;
    }
    if (codepoint >= 0x2030 && codepoint <= 0x203E) {
        return UNICODE_CATEGORY_PO;
    }
    /* CJK */
    if (codepoint >= 0x4E00 && codepoint <= 0x9FFF) {
        return UNICODE_CATEGORY_LO;
    }
    if (codepoint >= 0x3040 && codepoint <= 0x309F) {
        return UNICODE_CATEGORY_LO;
    }
    if (codepoint >= 0x30A0 && codepoint <= 0x30FF) {
        return UNICODE_CATEGORY_LO;
    }
    if (codepoint >= 0xAC00 && codepoint <= 0xD7AF) {
        return UNICODE_CATEGORY_LO;
    }
    /* Surrogates */
    if (codepoint >= 0xD800 && codepoint <= 0xDFFF) {
        return UNICODE_CATEGORY_CS;
    }
    /* Private Use */
    if (codepoint >= 0xE000 && codepoint <= 0xF8FF) {
        return UNICODE_CATEGORY_CO;
    }
    /* Noncharacters */
    if (codepoint >= 0xFFF0 && codepoint <= 0xFFFF) {
        return UNICODE_CATEGORY_CN;
    }
    if (codepoint > 0x10FFFF) {
        return UNICODE_CATEGORY_CN;
    }
    return UNICODE_CATEGORY_CN;
}

bool_t unicode_is_alpha(u32 cp) {
    UnicodeCategory c = unicode_category(cp);
    return (c == UNICODE_CATEGORY_LU || c == UNICODE_CATEGORY_LL ||
            c == UNICODE_CATEGORY_LT || c == UNICODE_CATEGORY_LM ||
            c == UNICODE_CATEGORY_LO) ? 1 : 0;
}

bool_t unicode_is_digit(u32 cp) {
    return unicode_category(cp) == UNICODE_CATEGORY_ND ? 1 : 0;
}

bool_t unicode_is_alnum(u32 cp) {
    UnicodeCategory c = unicode_category(cp);
    return (c == UNICODE_CATEGORY_LU || c == UNICODE_CATEGORY_LL ||
            c == UNICODE_CATEGORY_LT || c == UNICODE_CATEGORY_LM ||
            c == UNICODE_CATEGORY_LO || c == UNICODE_CATEGORY_ND ||
            c == UNICODE_CATEGORY_NL || c == UNICODE_CATEGORY_NO) ? 1 : 0;
}

bool_t unicode_is_space(u32 cp) {
    if (cp == 0x20 || cp == 0x09 || cp == 0x0A ||
        cp == 0x0B || cp == 0x0C || cp == 0x0D) {
        return 1;
    }
    UnicodeCategory c = unicode_category(cp);
    return (c == UNICODE_CATEGORY_ZS || c == UNICODE_CATEGORY_ZL ||
            c == UNICODE_CATEGORY_ZP) ? 1 : 0;
}

bool_t unicode_is_upper(u32 cp) {
    UnicodeCategory c = unicode_category(cp);
    return (c == UNICODE_CATEGORY_LU || c == UNICODE_CATEGORY_LT) ? 1 : 0;
}

bool_t unicode_is_lower(u32 cp) {
    return unicode_category(cp) == UNICODE_CATEGORY_LL ? 1 : 0;
}

bool_t unicode_is_print(u32 cp) {
    UnicodeCategory c = unicode_category(cp);
    if (c == UNICODE_CATEGORY_CC || c == UNICODE_CATEGORY_CF ||
        c == UNICODE_CATEGORY_CS || c == UNICODE_CATEGORY_CO ||
        c == UNICODE_CATEGORY_CN) {
        return 0;
    }
    return 1;
}

bool_t unicode_is_punct(u32 cp) {
    UnicodeCategory c = unicode_category(cp);
    return (c == UNICODE_CATEGORY_PC || c == UNICODE_CATEGORY_PD ||
            c == UNICODE_CATEGORY_PS || c == UNICODE_CATEGORY_PE ||
            c == UNICODE_CATEGORY_PI || c == UNICODE_CATEGORY_PF ||
            c == UNICODE_CATEGORY_PO) ? 1 : 0;
}

bool_t unicode_is_control(u32 cp) {
    return unicode_category(cp) == UNICODE_CATEGORY_CC ? 1 : 0;
}

bool_t unicode_is_letter(u32 cp) {
    return unicode_is_alpha(cp);
}

bool_t unicode_is_number(u32 cp) {
    UnicodeCategory c = unicode_category(cp);
    return (c == UNICODE_CATEGORY_ND || c == UNICODE_CATEGORY_NL ||
            c == UNICODE_CATEGORY_NO) ? 1 : 0;
}

bool_t unicode_is_symbol(u32 cp) {
    UnicodeCategory c = unicode_category(cp);
    return (c == UNICODE_CATEGORY_SM || c == UNICODE_CATEGORY_SC ||
            c == UNICODE_CATEGORY_SK || c == UNICODE_CATEGORY_SO) ? 1 : 0;
}

bool_t unicode_is_separator(u32 cp) {
    UnicodeCategory c = unicode_category(cp);
    return (c == UNICODE_CATEGORY_ZS || c == UNICODE_CATEGORY_ZL ||
            c == UNICODE_CATEGORY_ZP) ? 1 : 0;
}

/* ============================================================
 * Case conversion - codepoint level
 * ============================================================ */

u32 unicode_to_upper(u32 cp) {
    if (cp >= 'A' && cp <= 'Z') return cp;
    if (cp >= 'a' && cp <= 'z') return cp - 32;
    if (cp >= 0x00E0 && cp <= 0x00FE && cp != 0x00F7) return cp - 32;
    if (cp == 0x00FF) return 0x0178;
    if (cp >= 0x0100 && cp <= 0x017F) {
        return lookup_to_upper(cp, latin_ext_a, latin_ext_a_count);
    }
    if (cp >= 0x0391 && cp <= 0x03C9) {
        return lookup_to_upper(cp, greek_case, greek_case_count);
    }
    /* Cyrillic a-z -> A-Z */
    if (cp >= 0x0430 && cp <= 0x044F) return cp - 32;
    return cp;
}

u32 unicode_to_lower(u32 cp) {
    if (cp >= 'a' && cp <= 'z') return cp;
    if (cp >= 'A' && cp <= 'Z') return cp + 32;
    if (cp >= 0x00C0 && cp <= 0x00DE && cp != 0x00D7) return cp + 32;
    if (cp >= 0x0100 && cp <= 0x017F) {
        return lookup_to_lower(cp, latin_ext_a, latin_ext_a_count);
    }
    if (cp >= 0x0391 && cp <= 0x03C9) {
        return lookup_to_lower(cp, greek_case, greek_case_count);
    }
    /* Cyrillic A-Z -> a-z */
    if (cp >= 0x0410 && cp <= 0x042F) return cp + 32;
    return cp;
}

u32 unicode_to_title(u32 cp) {
    u32 u = unicode_to_upper(cp);
    /* Most titlecase mappings equal uppercase for common ranges */
    return u;
}

/* ============================================================
 * UTF-8 encoding/decoding (RFC 3629)
 * ============================================================ */

int unicode_utf8_seq_len(unsigned char first_byte) {
    if (first_byte < 0x80) return 1;
    if ((first_byte & 0xE0) == 0xC0) return 2;
    if ((first_byte & 0xF0) == 0xE0) return 3;
    if ((first_byte & 0xF8) == 0xF0) return 4;
    return -1;
}

int unicode_encode_utf8(u32 cp, char* out_buf) {
    if (!out_buf) return -1;
    if (cp > 0x10FFFF || (cp >= 0xD800 && cp <= 0xDFFF)) {
        cp = REPLACEMENT_CHAR;
    }
    if (cp < 0x80) {
        out_buf[0] = (char)cp;
        return 1;
    }
    if (cp < 0x800) {
        out_buf[0] = (char)(0xC0 | (cp >> 6));
        out_buf[1] = (char)(0x80 | (cp & 0x3F));
        return 2;
    }
    if (cp < 0x10000) {
        out_buf[0] = (char)(0xE0 | (cp >> 12));
        out_buf[1] = (char)(0x80 | ((cp >> 6) & 0x3F));
        out_buf[2] = (char)(0x80 | (cp & 0x3F));
        return 3;
    }
    out_buf[0] = (char)(0xF0 | (cp >> 18));
    out_buf[1] = (char)(0x80 | ((cp >> 12) & 0x3F));
    out_buf[2] = (char)(0x80 | ((cp >> 6) & 0x3F));
    out_buf[3] = (char)(0x80 | (cp & 0x3F));
    return 4;
}

int unicode_decode_utf8(const char* str, u32* out_cp) {
    if (!str || !out_cp) return -1;
    unsigned char b0 = (unsigned char)str[0];
    if (b0 < 0x80) {
        *out_cp = b0;
        return 1;
    }
    if ((b0 & 0xE0) == 0xC0) {
        unsigned char b1 = (unsigned char)str[1];
        if ((b1 & 0xC0) != 0x80) return -1;
        u32 cp = ((u32)(b0 & 0x1F) << 6) | (u32)(b1 & 0x3F);
        if (cp < 0x80) return -1;
        *out_cp = cp;
        return 2;
    }
    if ((b0 & 0xF0) == 0xE0) {
        unsigned char b1 = (unsigned char)str[1];
        unsigned char b2 = (unsigned char)str[2];
        if ((b1 & 0xC0) != 0x80 || (b2 & 0xC0) != 0x80) return -1;
        u32 cp = ((u32)(b0 & 0x0F) << 12) |
                 ((u32)(b1 & 0x3F) << 6) |
                 (u32)(b2 & 0x3F);
        if (cp < 0x800) return -1;
        if (cp >= 0xD800 && cp <= 0xDFFF) return -1;
        *out_cp = cp;
        return 3;
    }
    if ((b0 & 0xF8) == 0xF0) {
        unsigned char b1 = (unsigned char)str[1];
        unsigned char b2 = (unsigned char)str[2];
        unsigned char b3 = (unsigned char)str[3];
        if ((b1 & 0xC0) != 0x80 ||
            (b2 & 0xC0) != 0x80 ||
            (b3 & 0xC0) != 0x80) return -1;
        u32 cp = ((u32)(b0 & 0x07) << 18) |
                 ((u32)(b1 & 0x3F) << 12) |
                 ((u32)(b2 & 0x3F) << 6) |
                 (u32)(b3 & 0x3F);
        if (cp < 0x10000 || cp > 0x10FFFF) return -1;
        *out_cp = cp;
        return 4;
    }
    return -1;
}

/* ============================================================
 * String info
 * ============================================================ */

size_t unicode_strlen(const char* str) {
    if (!str) return 0;
    return strlen(str);
}

size_t unicode_char_count(const char* str) {
    if (!str) return 0;
    size_t count = 0;
    size_t i = 0;
    while (str[i]) {
        unsigned char b = (unsigned char)str[i];
        if (b < 0x80) {
            i += 1;
        } else if ((b & 0xE0) == 0xC0) {
            i += 2;
        } else if ((b & 0xF0) == 0xE0) {
            i += 3;
        } else if ((b & 0xF8) == 0xF0) {
            i += 4;
        } else {
            i += 1;
        }
        count++;
    }
    return count;
}

u32 unicode_char_at(const char* str, size_t index) {
    if (!str) return 0;
    size_t count = 0;
    size_t i = 0;
    while (str[i]) {
        if (count == index) {
            u32 cp = 0;
            if (unicode_decode_utf8(str + i, &cp) > 0) return cp;
            return 0;
        }
        unsigned char b = (unsigned char)str[i];
        if (b < 0x80) {
            i += 1;
        } else if ((b & 0xE0) == 0xC0) {
            i += 2;
        } else if ((b & 0xF0) == 0xE0) {
            i += 3;
        } else if ((b & 0xF8) == 0xF0) {
            i += 4;
        } else {
            i += 1;
        }
        count++;
    }
    return 0;
}

String unicode_substr(const char* str, size_t start, size_t count) {
    if (!str) return string_create("");
    size_t char_count = unicode_char_count(str);
    if (start >= char_count) return string_create("");
    if (start + count > char_count) {
        count = char_count - start;
    }
    /* Find byte offset for start */
    size_t c = 0;
    size_t byte_pos = 0;
    while (str[byte_pos] && c < start) {
        int len = unicode_utf8_seq_len((unsigned char)str[byte_pos]);
        if (len < 1) len = 1;
        byte_pos += len;
        c++;
    }
    /* Find byte length */
    size_t byte_start = byte_pos;
    c = 0;
    while (str[byte_pos] && c < count) {
        int len = unicode_utf8_seq_len((unsigned char)str[byte_pos]);
        if (len < 1) len = 1;
        byte_pos += len;
        c++;
    }
    size_t byte_len = byte_pos - byte_start;
    char* result = (char*)kmm_v4_malloc(byte_len + 1);
    if (!result) return STRING_EMPTY;
    memcpy(result, str + byte_start, byte_len);
    result[byte_len] = '\0';
    return string_create(result);
}

String unicode_reverse(const char* str) {
    if (!str) return string_create("");
    size_t byte_len = strlen(str);
    if (byte_len == 0) return string_create("");
    /* Collect codepoint byte ranges */
    size_t char_count = unicode_char_count(str);
    if (char_count == 0) return string_create("");
    char* result = (char*)kmm_v4_malloc(byte_len + 1);
    if (!result) return STRING_EMPTY;
    size_t out_pos = byte_len;
    result[out_pos] = '\0';
    size_t i = 0;
    while (i < byte_len) {
        int len = unicode_utf8_seq_len((unsigned char)str[i]);
        if (len < 1 || i + len > byte_len) len = 1;
        out_pos -= len;
        memcpy(result + out_pos, str + i, len);
        i += len;
    }
    return (String){.len = byte_len, .ptr = result};
}

/* ============================================================
 * String-level case conversion helpers
 * ============================================================ */

static String str_case_convert(const char* str,
                               u32 (*conv)(u32)) {
    if (!str) return string_create("");
    size_t byte_len = strlen(str);
    /* Worst case: each codepoint expands to 4 bytes */
    char* buf = (char*)kmm_v4_malloc(byte_len * 4 + 1);
    if (!buf) return STRING_EMPTY;
    size_t i = 0;
    size_t out = 0;
    while (i < byte_len) {
        u32 cp = 0;
        int len = unicode_decode_utf8(str + i, &cp);
        if (len < 1) {
            buf[out++] = str[i];
            i++;
            continue;
        }
        u32 new_cp = conv(cp);
        int enc_len = unicode_encode_utf8(new_cp, buf + out);
        if (enc_len < 1) {
            buf[out++] = '?';
        } else {
            out += enc_len;
        }
        i += len;
    }
    buf[out] = '\0';
    /* Shrink to fit */
    char* result = (char*)kmm_v4_malloc(out + 1);
    if (!result) {
        return (String){.len = out, .ptr = buf};
    }
    memcpy(result, buf, out + 1);
    kmm_v4_free(buf);
    return (String){.len = out, .ptr = result};
}

String unicode_str_to_upper(const char* str) {
    return str_case_convert(str, unicode_to_upper);
}

String unicode_str_to_lower(const char* str) {
    return str_case_convert(str, unicode_to_lower);
}

String unicode_str_to_title(const char* str) {
    if (!str) return string_create("");
    size_t byte_len = strlen(str);
    char* buf = (char*)kmm_v4_malloc(byte_len * 4 + 1);
    if (!buf) return STRING_EMPTY;
    size_t i = 0;
    size_t out = 0;
    bool_t next_upper = 1;
    while (i < byte_len) {
        u32 cp = 0;
        int len = unicode_decode_utf8(str + i, &cp);
        if (len < 1) {
            buf[out++] = str[i];
            i++;
            continue;
        }
        u32 new_cp;
        if (next_upper && unicode_is_alpha(cp)) {
            new_cp = unicode_to_title(cp);
            next_upper = 0;
        } else if (unicode_is_space(cp)) {
            new_cp = cp;
            next_upper = 1;
        } else {
            new_cp = unicode_to_lower(cp);
        }
        int enc_len = unicode_encode_utf8(new_cp, buf + out);
        if (enc_len < 1) {
            buf[out++] = '?';
        } else {
            out += enc_len;
        }
        i += len;
    }
    buf[out] = '\0';
    char* result = (char*)kmm_v4_malloc(out + 1);
    if (!result) {
        return (String){.len = out, .ptr = buf};
    }
    memcpy(result, buf, out + 1);
    kmm_v4_free(buf);
    return (String){.len = out, .ptr = result};
}

String unicode_str_swapcase(const char* str) {
    if (!str) return string_create("");
    size_t byte_len = strlen(str);
    char* buf = (char*)kmm_v4_malloc(byte_len * 4 + 1);
    if (!buf) return STRING_EMPTY;
    size_t i = 0;
    size_t out = 0;
    while (i < byte_len) {
        u32 cp = 0;
        int len = unicode_decode_utf8(str + i, &cp);
        if (len < 1) {
            buf[out++] = str[i];
            i++;
            continue;
        }
        u32 new_cp;
        if (unicode_is_upper(cp)) {
            new_cp = unicode_to_lower(cp);
        } else if (unicode_is_lower(cp)) {
            new_cp = unicode_to_upper(cp);
        } else {
            new_cp = cp;
        }
        int enc_len = unicode_encode_utf8(new_cp, buf + out);
        if (enc_len < 1) {
            buf[out++] = '?';
        } else {
            out += enc_len;
        }
        i += len;
    }
    buf[out] = '\0';
    char* result = (char*)kmm_v4_malloc(out + 1);
    if (!result) {
        return (String){.len = out, .ptr = buf};
    }
    memcpy(result, buf, out + 1);
    kmm_v4_free(buf);
    return (String){.len = out, .ptr = result};
}

String unicode_str_capitalize(const char* str) {
    if (!str) return string_create("");
    size_t byte_len = strlen(str);
    char* buf = (char*)kmm_v4_malloc(byte_len * 4 + 1);
    if (!buf) return STRING_EMPTY;
    size_t i = 0;
    size_t out = 0;
    bool_t first_char = 1;
    while (i < byte_len) {
        u32 cp = 0;
        int len = unicode_decode_utf8(str + i, &cp);
        if (len < 1) {
            buf[out++] = str[i];
            i++;
            continue;
        }
        u32 new_cp;
        if (first_char && unicode_is_alpha(cp)) {
            new_cp = unicode_to_upper(cp);
            first_char = 0;
        } else {
            new_cp = unicode_to_lower(cp);
            if (unicode_is_alpha(cp)) first_char = 0;
        }
        int enc_len = unicode_encode_utf8(new_cp, buf + out);
        if (enc_len < 1) {
            buf[out++] = '?';
        } else {
            out += enc_len;
        }
        i += len;
    }
    buf[out] = '\0';
    char* result = (char*)kmm_v4_malloc(out + 1);
    if (!result) {
        return (String){.len = out, .ptr = buf};
    }
    memcpy(result, buf, out + 1);
    kmm_v4_free(buf);
    return (String){.len = out, .ptr = result};
}

/* ============================================================
 * Normalization - decomposition/composition tables
 * ============================================================ */

typedef struct {
    u32 composed;
    u32 base;
    u32 combining;
} DecompEntry;

/* Latin-1 Supplement and common decomposition entries */
static const DecompEntry decomp_table[] = {
    {0x00C0, 0x0041, 0x0300}, {0x00C1, 0x0041, 0x0301},
    {0x00C2, 0x0041, 0x0302}, {0x00C3, 0x0041, 0x0303},
    {0x00C4, 0x0041, 0x0308}, {0x00C5, 0x0041, 0x030A},
    {0x00C7, 0x0043, 0x0327}, {0x00C8, 0x0045, 0x0300},
    {0x00C9, 0x0045, 0x0301}, {0x00CA, 0x0045, 0x0302},
    {0x00CB, 0x0045, 0x0308}, {0x00CC, 0x0049, 0x0300},
    {0x00CD, 0x0049, 0x0301}, {0x00CE, 0x0049, 0x0302},
    {0x00CF, 0x0049, 0x0308}, {0x00D1, 0x004E, 0x0303},
    {0x00D2, 0x004F, 0x0300}, {0x00D3, 0x004F, 0x0301},
    {0x00D4, 0x004F, 0x0302}, {0x00D5, 0x004F, 0x0303},
    {0x00D6, 0x004F, 0x0308}, {0x00D9, 0x0055, 0x0300},
    {0x00DA, 0x0055, 0x0301}, {0x00DB, 0x0055, 0x0302},
    {0x00DC, 0x0055, 0x0308}, {0x00DD, 0x0059, 0x0301},
    {0x00E0, 0x0061, 0x0300}, {0x00E1, 0x0061, 0x0301},
    {0x00E2, 0x0061, 0x0302}, {0x00E3, 0x0061, 0x0303},
    {0x00E4, 0x0061, 0x0308}, {0x00E5, 0x0061, 0x030A},
    {0x00E7, 0x0063, 0x0327}, {0x00E8, 0x0065, 0x0300},
    {0x00E9, 0x0065, 0x0301}, {0x00EA, 0x0065, 0x0302},
    {0x00EB, 0x0065, 0x0308}, {0x00EC, 0x0069, 0x0300},
    {0x00ED, 0x0069, 0x0301}, {0x00EE, 0x0069, 0x0302},
    {0x00EF, 0x0069, 0x0308}, {0x00F1, 0x006E, 0x0303},
    {0x00F2, 0x006F, 0x0300}, {0x00F3, 0x006F, 0x0301},
    {0x00F4, 0x006F, 0x0302}, {0x00F5, 0x006F, 0x0303},
    {0x00F6, 0x006F, 0x0308}, {0x00F9, 0x0075, 0x0300},
    {0x00FA, 0x0075, 0x0301}, {0x00FB, 0x0075, 0x0302},
    {0x00FC, 0x0075, 0x0308}, {0x00FD, 0x0079, 0x0301},
    {0x00FF, 0x0079, 0x0308},
    /* Latin Extended-A common */
    {0x0100, 0x0041, 0x0304}, {0x0101, 0x0061, 0x0304},
    {0x0102, 0x0041, 0x0306}, {0x0103, 0x0061, 0x0306},
    {0x0104, 0x0041, 0x0328}, {0x0105, 0x0061, 0x0328},
    {0x0106, 0x0043, 0x0301}, {0x0107, 0x0063, 0x0301},
    {0x0108, 0x0043, 0x0302}, {0x0109, 0x0063, 0x0302},
    {0x010A, 0x0043, 0x0307}, {0x010B, 0x0063, 0x0307},
    {0x010C, 0x0043, 0x030C}, {0x010D, 0x0063, 0x030C},
    {0x010E, 0x0044, 0x030C}, {0x010F, 0x0064, 0x030C},
    {0x0112, 0x0045, 0x0304}, {0x0113, 0x0065, 0x0304},
    {0x0114, 0x0045, 0x0306}, {0x0115, 0x0065, 0x0306},
    {0x0116, 0x0045, 0x0307}, {0x0117, 0x0065, 0x0307},
    {0x0118, 0x0045, 0x0328}, {0x0119, 0x0065, 0x0328},
    {0x011A, 0x0045, 0x030C}, {0x011B, 0x0065, 0x030C},
    {0x011C, 0x0047, 0x0302}, {0x011D, 0x0067, 0x0302},
    {0x011E, 0x0047, 0x0306}, {0x011F, 0x0067, 0x0306},
    {0x0120, 0x0047, 0x0307}, {0x0121, 0x0067, 0x0307},
    {0x0122, 0x0047, 0x0327}, {0x0123, 0x0067, 0x0327},
    {0x0124, 0x0048, 0x0302}, {0x0125, 0x0068, 0x0302},
    {0x0128, 0x0049, 0x0303}, {0x0129, 0x0069, 0x0303},
    {0x012A, 0x0049, 0x0304}, {0x012B, 0x0069, 0x0304},
    {0x012C, 0x0049, 0x0306}, {0x012D, 0x0069, 0x0306},
    {0x012E, 0x0049, 0x0328}, {0x012F, 0x0069, 0x0328},
    {0x0134, 0x004A, 0x0302}, {0x0135, 0x006A, 0x0302},
    {0x0136, 0x004B, 0x0327}, {0x0137, 0x006B, 0x0327},
    {0x0139, 0x004C, 0x0301}, {0x013A, 0x006C, 0x0301},
    {0x013B, 0x004C, 0x0327}, {0x013C, 0x006C, 0x0327},
    {0x013D, 0x004C, 0x030C}, {0x013E, 0x006C, 0x030C},
    {0x0143, 0x004E, 0x0301}, {0x0144, 0x006E, 0x0301},
    {0x0145, 0x004E, 0x0327}, {0x0146, 0x006E, 0x0327},
    {0x0147, 0x004E, 0x030C}, {0x0148, 0x006E, 0x030C},
    {0x014C, 0x004F, 0x0304}, {0x014D, 0x006F, 0x0304},
    {0x014E, 0x004F, 0x0306}, {0x014F, 0x006F, 0x0306},
    {0x0150, 0x004F, 0x030B}, {0x0151, 0x006F, 0x030B},
    {0x0154, 0x0052, 0x0301}, {0x0155, 0x0072, 0x0301},
    {0x0156, 0x0052, 0x0327}, {0x0157, 0x0072, 0x0327},
    {0x0158, 0x0052, 0x030C}, {0x0159, 0x0072, 0x030C},
    {0x015A, 0x0053, 0x0301}, {0x015B, 0x0073, 0x0301},
    {0x015C, 0x0053, 0x0302}, {0x015D, 0x0073, 0x0302},
    {0x015E, 0x0053, 0x0327}, {0x015F, 0x0073, 0x0327},
    {0x0160, 0x0053, 0x030C}, {0x0161, 0x0073, 0x030C},
    {0x0162, 0x0054, 0x0327}, {0x0163, 0x0074, 0x0327},
    {0x0164, 0x0054, 0x030C}, {0x0165, 0x0074, 0x030C},
    {0x0168, 0x0055, 0x0303}, {0x0169, 0x0075, 0x0303},
    {0x016A, 0x0055, 0x0304}, {0x016B, 0x0075, 0x0304},
    {0x016C, 0x0055, 0x0306}, {0x016D, 0x0075, 0x0306},
    {0x016E, 0x0055, 0x030A}, {0x016F, 0x0075, 0x030A},
    {0x0170, 0x0055, 0x030B}, {0x0171, 0x0075, 0x030B},
    {0x0172, 0x0055, 0x0328}, {0x0173, 0x0075, 0x0328},
    {0x0174, 0x0057, 0x0302}, {0x0175, 0x0077, 0x0302},
    {0x0176, 0x0059, 0x0302}, {0x0177, 0x0079, 0x0302},
    {0x0179, 0x005A, 0x0301}, {0x017A, 0x007A, 0x0301},
    {0x017B, 0x005A, 0x0307}, {0x017C, 0x007A, 0x0307},
    {0x017D, 0x005A, 0x030C}, {0x017E, 0x007A, 0x030C}
};

static const size_t decomp_table_count =
    sizeof(decomp_table) / sizeof(decomp_table[0]);

static const DecompEntry* find_decomp(u32 cp) {
    size_t i;
    for (i = 0; i < decomp_table_count; i++) {
        if (decomp_table[i].composed == cp) return &decomp_table[i];
    }
    return NULL;
}

static u32 find_composed(u32 base, u32 combining) {
    size_t i;
    for (i = 0; i < decomp_table_count; i++) {
        if (decomp_table[i].base == base &&
            decomp_table[i].combining == combining) {
            return decomp_table[i].composed;
        }
    }
    return 0;
}

/* ============================================================
 * Normalization - NFD (decompose)
 * ============================================================ */

static String normalize_nfd_impl(const char* str) {
    if (!str) return string_create("");
    size_t byte_len = strlen(str);
    /* Worst case: each codepoint becomes 2 codepoints (4 bytes each) */
    char* buf = (char*)kmm_v4_malloc(byte_len * 8 + 1);
    if (!buf) return STRING_EMPTY;
    size_t i = 0;
    size_t out = 0;
    while (i < byte_len) {
        u32 cp = 0;
        int len = unicode_decode_utf8(str + i, &cp);
        if (len < 1) {
            buf[out++] = str[i];
            i++;
            continue;
        }
        const DecompEntry* d = find_decomp(cp);
        if (d) {
            int e1 = unicode_encode_utf8(d->base, buf + out);
            if (e1 > 0) out += e1;
            int e2 = unicode_encode_utf8(d->combining, buf + out);
            if (e2 > 0) out += e2;
        } else {
            int e = unicode_encode_utf8(cp, buf + out);
            if (e > 0) out += e;
        }
        i += len;
    }
    buf[out] = '\0';
    char* result = (char*)kmm_v4_malloc(out + 1);
    if (!result) {
        return (String){.len = out, .ptr = buf};
    }
    memcpy(result, buf, out + 1);
    kmm_v4_free(buf);
    return (String){.len = out, .ptr = result};
}

/* ============================================================
 * Normalization - NFC (decompose then recompose)
 * ============================================================ */

static String normalize_nfc_impl(const char* str) {
    /* First decompose */
    String decomposed = normalize_nfd_impl(str);
    if (decomposed.len == 0) return STRING_EMPTY;
    size_t byte_len = decomposed.len;
    char* buf = (char*)kmm_v4_malloc(byte_len + 1);
    if (!buf) {
        return decomposed;
    }
    size_t i = 0;
    size_t out = 0;
    u32 prev_cp = 0;
    int have_prev = 0;
    while (i < byte_len) {
        u32 cp = 0;
        int len = unicode_decode_utf8(decomposed.ptr + i, &cp);
        if (len < 1) {
            if (have_prev) {
                int e = unicode_encode_utf8(prev_cp, buf + out);
                if (e > 0) out += e;
                have_prev = 0;
            }
            buf[out++] = decomposed.ptr[i];
            i++;
            continue;
        }
        if (have_prev) {
            /* Check if prev + cp can be composed */
            u32 composed = find_composed(prev_cp, cp);
            if (composed) {
                prev_cp = composed;
                /* Try chaining: the new composed char might compose again */
                u32 chained;
                while (i + len < byte_len) {
                    u32 next_cp = 0;
                    int next_len = unicode_decode_utf8(
                        decomposed.ptr + i + len, &next_cp);
                    if (next_len < 1) break;
                    chained = find_composed(prev_cp, next_cp);
                    if (chained) {
                        prev_cp = chained;
                        len += next_len;
                    } else {
                        break;
                    }
                }
            } else {
                int e = unicode_encode_utf8(prev_cp, buf + out);
                if (e > 0) out += e;
                prev_cp = cp;
            }
        } else {
            prev_cp = cp;
            have_prev = 1;
        }
        i += len;
    }
    if (have_prev) {
        int e = unicode_encode_utf8(prev_cp, buf + out);
        if (e > 0) out += e;
    }
    buf[out] = '\0';
    kmm_v4_free(decomposed.ptr);
    char* result = (char*)kmm_v4_malloc(out + 1);
    if (!result) {
        return (String){.len = out, .ptr = buf};
    }
    memcpy(result, buf, out + 1);
    kmm_v4_free(buf);
    return (String){.len = out, .ptr = result};
}

String unicode_normalize(const char* str, UnicodeNormalization form) {
    switch (form) {
        case UNICODE_NFC:  return normalize_nfc_impl(str);
        case UNICODE_NFD:  return normalize_nfd_impl(str);
        case UNICODE_NFKC: return normalize_nfc_impl(str);
        case UNICODE_NFKD: return normalize_nfd_impl(str);
        default:           return string_create(str);
    }
}

String unicode_normalize_nfc(const char* str) {
    return normalize_nfc_impl(str);
}

String unicode_normalize_nfd(const char* str) {
    return normalize_nfd_impl(str);
}

String unicode_normalize_nfkc(const char* str) {
    return normalize_nfc_impl(str);
}

String unicode_normalize_nfkd(const char* str) {
    return normalize_nfd_impl(str);
}

/* ============================================================
 * Codepoint iteration
 * ============================================================ */

UnicodeIter unicode_iter_create(const char* str) {
    UnicodeIter iter;
    iter.str = str;
    iter.pos = 0;
    iter.len = str ? strlen(str) : 0;
    return iter;
}

bool_t unicode_iter_next(UnicodeIter* iter, u32* out_cp) {
    if (!iter || !out_cp) return 0;
    if (!iter->str || iter->pos >= iter->len) return 0;
    int len = unicode_decode_utf8(iter->str + iter->pos, out_cp);
    if (len < 1) {
        *out_cp = REPLACEMENT_CHAR;
        iter->pos += 1;
        return 1;
    }
    iter->pos += len;
    return 1;
}

size_t unicode_iter_pos(const UnicodeIter* iter) {
    if (!iter) return 0;
    return iter->pos;
}

/* ============================================================
 * Validation
 * ============================================================ */

bool_t unicode_is_valid(const char* str) {
    if (!str) return 1;
    return unicode_is_valid_n(str, strlen(str));
}

bool_t unicode_is_valid_n(const char* str, size_t len) {
    if (!str) return 1;
    size_t i = 0;
    while (i < len) {
        unsigned char b = (unsigned char)str[i];
        if (b < 0x80) {
            i++;
            continue;
        }
        if ((b & 0xE0) == 0xC0) {
            if (i + 1 >= len) return 0;
            unsigned char b1 = (unsigned char)str[i + 1];
            if ((b1 & 0xC0) != 0x80) return 0;
            u32 cp = ((u32)(b & 0x1F) << 6) | (u32)(b1 & 0x3F);
            if (cp < 0x80) return 0;
            i += 2;
        } else if ((b & 0xF0) == 0xE0) {
            if (i + 2 >= len) return 0;
            unsigned char b1 = (unsigned char)str[i + 1];
            unsigned char b2 = (unsigned char)str[i + 2];
            if ((b1 & 0xC0) != 0x80 || (b2 & 0xC0) != 0x80) return 0;
            u32 cp = ((u32)(b & 0x0F) << 12) |
                     ((u32)(b1 & 0x3F) << 6) |
                     (u32)(b2 & 0x3F);
            if (cp < 0x800) return 0;
            if (cp >= 0xD800 && cp <= 0xDFFF) return 0;
            i += 3;
        } else if ((b & 0xF8) == 0xF0) {
            if (i + 3 >= len) return 0;
            unsigned char b1 = (unsigned char)str[i + 1];
            unsigned char b2 = (unsigned char)str[i + 2];
            unsigned char b3 = (unsigned char)str[i + 3];
            if ((b1 & 0xC0) != 0x80 ||
                (b2 & 0xC0) != 0x80 ||
                (b3 & 0xC0) != 0x80) return 0;
            u32 cp = ((u32)(b & 0x07) << 18) |
                     ((u32)(b1 & 0x3F) << 12) |
                     ((u32)(b2 & 0x3F) << 6) |
                     (u32)(b3 & 0x3F);
            if (cp < 0x10000 || cp > 0x10FFFF) return 0;
            i += 4;
        } else {
            return 0;
        }
    }
    return 1;
}

/* ============================================================
 * Misc
 * ============================================================ */

const char* unicode_category_name(UnicodeCategory cat) {
    switch (cat) {
        case UNICODE_CATEGORY_LU: return "Lu";
        case UNICODE_CATEGORY_LL: return "Ll";
        case UNICODE_CATEGORY_LT: return "Lt";
        case UNICODE_CATEGORY_LM: return "Lm";
        case UNICODE_CATEGORY_LO: return "Lo";
        case UNICODE_CATEGORY_MN: return "Mn";
        case UNICODE_CATEGORY_MC: return "Mc";
        case UNICODE_CATEGORY_ME: return "Me";
        case UNICODE_CATEGORY_ND: return "Nd";
        case UNICODE_CATEGORY_NL: return "Nl";
        case UNICODE_CATEGORY_NO: return "No";
        case UNICODE_CATEGORY_PC: return "Pc";
        case UNICODE_CATEGORY_PD: return "Pd";
        case UNICODE_CATEGORY_PS: return "Ps";
        case UNICODE_CATEGORY_PE: return "Pe";
        case UNICODE_CATEGORY_PI: return "Pi";
        case UNICODE_CATEGORY_PF: return "Pf";
        case UNICODE_CATEGORY_PO: return "Po";
        case UNICODE_CATEGORY_SM: return "Sm";
        case UNICODE_CATEGORY_SC: return "Sc";
        case UNICODE_CATEGORY_SK: return "Sk";
        case UNICODE_CATEGORY_SO: return "So";
        case UNICODE_CATEGORY_ZS: return "Zs";
        case UNICODE_CATEGORY_ZL: return "Zl";
        case UNICODE_CATEGORY_ZP: return "Zp";
        case UNICODE_CATEGORY_CC: return "Cc";
        case UNICODE_CATEGORY_CF: return "Cf";
        case UNICODE_CATEGORY_CS: return "Cs";
        case UNICODE_CATEGORY_CO: return "Co";
        case UNICODE_CATEGORY_CN: return "Cn";
        default: return "Unknown";
    }
}

const char* unicode_version(void) {
    return "15.0.0";
}

u32 unicode_replacement_char(void) {
    return REPLACEMENT_CHAR;
}
