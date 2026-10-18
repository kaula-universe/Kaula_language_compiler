#pragma once
#include "../base/types.h"
#include "../string/string.h"

typedef enum {
    UNICODE_CATEGORY_LU,  // Letter, uppercase
    UNICODE_CATEGORY_LL,  // Letter, lowercase
    UNICODE_CATEGORY_LT,  // Letter, titlecase
    UNICODE_CATEGORY_LM,  // Letter, modifier
    UNICODE_CATEGORY_LO,  // Letter, other
    UNICODE_CATEGORY_MN,  // Mark, nonspacing
    UNICODE_CATEGORY_MC,  // Mark, spacing combining
    UNICODE_CATEGORY_ME,  // Mark, enclosing
    UNICODE_CATEGORY_ND,  // Number, decimal digit
    UNICODE_CATEGORY_NL,  // Number, letter
    UNICODE_CATEGORY_NO,  // Number, other
    UNICODE_CATEGORY_PC,  // Punctuation, connector
    UNICODE_CATEGORY_PD,  // Punctuation, dash
    UNICODE_CATEGORY_PS,  // Punctuation, open
    UNICODE_CATEGORY_PE,  // Punctuation, close
    UNICODE_CATEGORY_PI,  // Punctuation, initial quote
    UNICODE_CATEGORY_PF,  // Punctuation, final quote
    UNICODE_CATEGORY_PO,  // Punctuation, other
    UNICODE_CATEGORY_SM,  // Symbol, math
    UNICODE_CATEGORY_SC,  // Symbol, currency
    UNICODE_CATEGORY_SK,  // Symbol, modifier
    UNICODE_CATEGORY_SO,  // Symbol, other
    UNICODE_CATEGORY_ZS,  // Separator, space
    UNICODE_CATEGORY_ZL,  // Separator, line
    UNICODE_CATEGORY_ZP,  // Separator, paragraph
    UNICODE_CATEGORY_CC,  // Other, control
    UNICODE_CATEGORY_CF,  // Other, format
    UNICODE_CATEGORY_CS,  // Other, surrogate
    UNICODE_CATEGORY_CO,  // Other, private use
    UNICODE_CATEGORY_CN   // Other, not assigned
} UnicodeCategory;

typedef enum {
    UNICODE_NFC,
    UNICODE_NFD,
    UNICODE_NFKC,
    UNICODE_NFKD
} UnicodeNormalization;

// Codepoint properties
UnicodeCategory unicode_category(u32 codepoint);
bool_t unicode_is_alpha(u32 cp);
bool_t unicode_is_digit(u32 cp);
bool_t unicode_is_alnum(u32 cp);
bool_t unicode_is_space(u32 cp);
bool_t unicode_is_upper(u32 cp);
bool_t unicode_is_lower(u32 cp);
bool_t unicode_is_print(u32 cp);
bool_t unicode_is_punct(u32 cp);
bool_t unicode_is_control(u32 cp);
bool_t unicode_is_letter(u32 cp);
bool_t unicode_is_number(u32 cp);
bool_t unicode_is_symbol(u32 cp);
bool_t unicode_is_separator(u32 cp);

// Case conversion
u32 unicode_to_upper(u32 cp);
u32 unicode_to_lower(u32 cp);
u32 unicode_to_title(u32 cp);

// String-level Unicode operations
String unicode_str_to_upper(const char* str);
String unicode_str_to_lower(const char* str);
String unicode_str_to_title(const char* str);
String unicode_str_swapcase(const char* str);
String unicode_str_capitalize(const char* str);

// Normalization
String unicode_normalize(const char* str, UnicodeNormalization form);
String unicode_normalize_nfc(const char* str);
String unicode_normalize_nfd(const char* str);
String unicode_normalize_nfkc(const char* str);
String unicode_normalize_nfkd(const char* str);

// String info
size_t unicode_strlen(const char* str);
size_t unicode_char_count(const char* str);
u32    unicode_char_at(const char* str, size_t index);
String unicode_substr(const char* str, size_t start, size_t count);
String unicode_reverse(const char* str);

// Codepoint iteration
typedef struct {
    const char* str;
    size_t pos;
    size_t len;
} UnicodeIter;

UnicodeIter unicode_iter_create(const char* str);
bool_t      unicode_iter_next(UnicodeIter* iter, u32* out_cp);
size_t      unicode_iter_pos(const UnicodeIter* iter);

// Validation
bool_t unicode_is_valid(const char* str);
bool_t unicode_is_valid_n(const char* str, size_t len);

// Encoding/Decoding
int unicode_encode_utf8(u32 cp, char* out_buf);
int unicode_decode_utf8(const char* str, u32* out_cp);
int unicode_utf8_seq_len(unsigned char first_byte);

// Misc
const char* unicode_category_name(UnicodeCategory cat);
const char* unicode_version(void);
u32 unicode_replacement_char(void);
