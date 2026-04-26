#ifndef STD_I18N_I18N_H
#define STD_I18N_I18N_H

#include "../base/types.h"

// ==================== 语言标识 ====================
typedef enum {
    LANG_AUTO = 0,        // 自动检测
    LANG_EN_US,           // 英语(美国)
    LANG_ZH_CN,           // 中文(简体)
    LANG_ZH_TW,           // 中文(繁体)
    LANG_JA_JP,           // 日语
    LANG_KO_KR,           // 韩语
    LANG_FR_FR,           // 法语
    LANG_DE_DE,           // 德语
    LANG_ES_ES,           // 西班牙语
    LANG_PT_BR,           // 葡萄牙语(巴西)
    LANG_RU_RU,           // 俄语
    LANG_AR_SA,           // 阿拉伯语
    LANG_HI_IN,           // 印地语
    LANG_TH_TH,           // 泰语
    LANG_VI_VN,           // 越南语
    LANG_IT_IT,           // 意大利语
    LANG_TR_TR,           // 土耳其语
    LANG_PL_PL,           // 波兰语
    LANG_NL_NL,           // 荷兰语
    LANG_ID_ID,           // 印尼语
    LANG_MS_MY,           // 马来语
    LANG_COUNT
} Language;

// ==================== 字符编码 ====================
typedef enum {
    ENCODING_AUTO = 0,    // 自动检测
    ENCODING_UTF8,        // UTF-8 (默认)
    ENCODING_UTF16,       // UTF-16
    ENCODING_UTF32,       // UTF-32
    ENCODING_GBK,         // GBK (中文Windows)
    ENCODING_GB2312,      // GB2312
    ENCODING_BIG5,        // Big5 (繁体中文)
    ENCODING_SHIFT_JIS,   // Shift-JIS (日文)
    ENCODING_EUC_KR,      // EUC-KR (韩文)
    ENCODING_ISO_8859_1,  // Latin-1
    ENCODING_WINDOWS_1252 // Windows-1252
} TextEncoding;

// ==================== 翻译条目 ====================
typedef struct {
    const char* key;          // 翻译键
    const char** translations; // 各语言翻译 [LANG_COUNT]
} TranslationEntry;

// ==================== 翻译表 ====================
typedef struct {
    TranslationEntry* entries;
    size_t count;
    size_t capacity;
} TranslationTable;

// ==================== 初始化与清理 ====================
extern void i18n_init(void);
extern void i18n_cleanup(void);

// ==================== 语言设置 ====================
extern void i18n_set_language(Language lang);
extern Language i18n_get_language(void);
extern const char* i18n_get_language_code(Language lang);
extern const char* i18n_get_language_name(Language lang);
extern Language i18n_parse_language_code(const char* code);
extern bool i18n_is_rtl_language(Language lang);

// ==================== 编码设置 ====================
extern void i18n_set_encoding(TextEncoding encoding);
extern TextEncoding i18n_get_encoding(void);
extern void i18n_set_console_encoding(void);
extern const char* i18n_get_encoding_name(TextEncoding encoding);

// ==================== 翻译功能 ====================
extern const char* i18n_translate(const char* key);
extern const char* i18n_t(const char* key);
extern const char* i18n_translate_args(const char* key, const char** args, size_t arg_count);

// ==================== 翻译表管理 ====================
extern void i18n_register_translation(const char* key, Language lang, const char* value);
extern void i18n_register_translations(const TranslationEntry* entries, size_t count);
extern bool i18n_load_translation_file(const char* filepath);
extern bool i18n_save_translation_file(const char* filepath);
extern void i18n_clear_translations(void);

// ==================== 字符串编码转换 ====================
extern char* i18n_convert_encoding(const char* input, TextEncoding from, TextEncoding to);
extern char* i18n_to_utf8(const char* input, TextEncoding from_encoding);
extern char* i18n_from_utf8(const char* input, TextEncoding to_encoding);
extern bool i18n_is_utf8(const char* input);
extern bool i18n_detect_encoding(const char* input, TextEncoding* out_encoding);

// ==================== Unicode 工具函数 ====================
extern size_t i18n_utf8_char_count(const char* str);
extern size_t i18n_utf8_byte_length(const char* str, size_t char_index);
extern char* i18n_utf8_substring(const char* str, size_t start, size_t count);
extern size_t i18n_utf8_strlen(const char* str);
extern int i18n_utf8_encode(wchar_t codepoint, char* out_buffer);
extern int i18n_utf8_decode(const char* input, wchar_t* out_codepoint);
extern char* i18n_utf8_normalize(const char* input);

// ==================== 本地化数字和日期 ====================
extern char* i18n_format_number(double value);
extern char* i18n_format_currency(double amount, Language lang);
extern char* i18n_format_date(time_t timestamp, const char* format);
extern char* i18n_format_date_time(time_t timestamp, const char* format);
extern char i18n_get_decimal_separator(Language lang);
extern char i18n_get_thousands_separator(Language lang);

// ==================== 平台适配 ====================
extern void i18n_setup_console(void);
extern void i18n_fix_console_output(void);

#endif // STD_I18N_I18N_H
