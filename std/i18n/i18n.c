#include "i18n.h"
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <time.h>

#if STD_PLATFORM_WINDOWS
    #include <windows.h>
#else
    #include <locale.h>
#endif

// ==================== 全局状态 ====================
static Language g_current_lang = LANG_AUTO;
static TextEncoding g_current_encoding = ENCODING_UTF8;
static TranslationTable* g_translation_table = NULL;

// ==================== 语言代码与名称 ====================
static const char* s_language_codes[] = {
    "",           // LANG_AUTO
    "en-US",      // LANG_EN_US
    "zh-CN",      // LANG_ZH_CN
    "zh-TW",      // LANG_ZH_TW
    "ja-JP",      // LANG_JA_JP
    "ko-KR",      // LANG_KO_KR
    "fr-FR",      // LANG_FR_FR
    "de-DE",      // LANG_DE_DE
    "es-ES",      // LANG_ES_ES
    "pt-BR",      // LANG_PT_BR
    "ru-RU",      // LANG_RU_RU
    "ar-SA",      // LANG_AR_SA
    "hi-IN",      // LANG_HI_IN
    "th-TH",      // LANG_TH_TH
    "vi-VN",      // LANG_VI_VN
    "it-IT",      // LANG_IT_IT
    "tr-TR",      // LANG_TR_TR
    "pl-PL",      // LANG_PL_PL
    "nl-NL",      // LANG_NL_NL
    "id-ID",      // LANG_ID_ID
    "ms-MY",      // LANG_MS_MY
};

static const char* s_language_names[] = {
    "Auto Detect",
    "English (US)",
    "Chinese (Simplified)",
    "Chinese (Traditional)",
    "Japanese",
    "Korean",
    "French",
    "German",
    "Spanish",
    "Portuguese (Brazil)",
    "Russian",
    "Arabic",
    "Hindi",
    "Thai",
    "Vietnamese",
    "Italian",
    "Turkish",
    "Polish",
    "Dutch",
    "Indonesian",
    "Malay",
};

// ==================== RTL 语言判断 ====================
bool i18n_is_rtl_language(Language lang) {
    return lang == LANG_AR_SA;
}

// ==================== 编码名称 ====================
static const char* s_encoding_names[] = {
    "Auto",
    "UTF-8",
    "UTF-16",
    "UTF-32",
    "GBK",
    "GB2312",
    "Big5",
    "Shift-JIS",
    "EUC-KR",
    "ISO-8859-1",
    "Windows-1252",
};

const char* i18n_get_encoding_name(TextEncoding encoding) {
    if (encoding < 0 || encoding >= 11) return "Unknown";
    return s_encoding_names[encoding];
}

// ==================== 初始化与清理 ====================
void i18n_init(void) {
    if (!g_translation_table) {
        g_translation_table = (TranslationTable*)malloc(sizeof(TranslationTable));
        if (g_translation_table) {
            g_translation_table->count = 0;
            g_translation_table->capacity = 256;
            g_translation_table->entries = (TranslationEntry*)malloc(
                sizeof(TranslationEntry) * g_translation_table->capacity);
        }
    }
    
    // 自动检测系统语言
    if (g_current_lang == LANG_AUTO) {
#if STD_PLATFORM_WINDOWS
        LANGID lang_id = GetUserDefaultUILanguage();
        switch (PRIMARYLANGID(lang_id)) {
            case LANG_CHINESE:
                g_current_lang = (SUBLANGID(lang_id) == SUBLANG_CHINESE_SIMPLIFIED) 
                    ? LANG_ZH_CN : LANG_ZH_TW;
                break;
            case LANG_JAPANESE: g_current_lang = LANG_JA_JP; break;
            case LANG_KOREAN: g_current_lang = LANG_KO_KR; break;
            case LANG_FRENCH: g_current_lang = LANG_FR_FR; break;
            case LANG_GERMAN: g_current_lang = LANG_DE_DE; break;
            case LANG_SPANISH: g_current_lang = LANG_ES_ES; break;
            case LANG_PORTUGUESE: g_current_lang = LANG_PT_BR; break;
            case LANG_RUSSIAN: g_current_lang = LANG_RU_RU; break;
            case LANG_ARABIC: g_current_lang = LANG_AR_SA; break;
            default: g_current_lang = LANG_EN_US; break;
        }
#else
        const char* locale = setlocale(LC_ALL, NULL);
        if (locale) {
            if (strstr(locale, "zh_CN")) g_current_lang = LANG_ZH_CN;
            else if (strstr(locale, "zh_TW")) g_current_lang = LANG_ZH_TW;
            else if (strstr(locale, "ja")) g_current_lang = LANG_JA_JP;
            else if (strstr(locale, "ko")) g_current_lang = LANG_KO_KR;
            else if (strstr(locale, "fr")) g_current_lang = LANG_FR_FR;
            else if (strstr(locale, "de")) g_current_lang = LANG_DE_DE;
            else if (strstr(locale, "es")) g_current_lang = LANG_ES_ES;
            else if (strstr(locale, "ru")) g_current_lang = LANG_RU_RU;
            else if (strstr(locale, "ar")) g_current_lang = LANG_AR_SA;
            else g_current_lang = LANG_EN_US;
        } else {
            g_current_lang = LANG_EN_US;
        }
#endif
    }
}

void i18n_cleanup(void) {
    if (g_translation_table) {
        for (size_t i = 0; i < g_translation_table->count; i++) {
            TranslationEntry* entry = &g_translation_table->entries[i];
            if (entry->translations) {
                for (int j = 0; j < LANG_COUNT; j++) {
                    if (entry->translations[j]) {
                        free((void*)entry->translations[j]);
                    }
                }
                free(entry->translations);
            }
            if (entry->key) {
                free((void*)entry->key);
            }
        }
        free(g_translation_table->entries);
        free(g_translation_table);
        g_translation_table = NULL;
    }
}

// ==================== 语言设置 ====================
void i18n_set_language(Language lang) {
    if (lang >= 0 && lang < LANG_COUNT) {
        g_current_lang = lang;
    }
}

Language i18n_get_language(void) {
    return g_current_lang;
}

const char* i18n_get_language_code(Language lang) {
    if (lang < 0 || lang >= LANG_COUNT) return "";
    return s_language_codes[lang];
}

const char* i18n_get_language_name(Language lang) {
    if (lang < 0 || lang >= LANG_COUNT) return "Unknown";
    return s_language_names[lang];
}

Language i18n_parse_language_code(const char* code) {
    if (!code) return LANG_EN_US;
    
    for (int i = 1; i < LANG_COUNT; i++) {
        if (strcmp(s_language_codes[i], code) == 0) {
            return (Language)i;
        }
    }
    
    // 忽略大小写匹配
    char lower_code[16];
    strncpy(lower_code, code, sizeof(lower_code) - 1);
    lower_code[sizeof(lower_code) - 1] = '\0';
    for (char* p = lower_code; *p; p++) {
        if (*p >= 'A' && *p <= 'Z') *p += 32;
    }
    
    for (int i = 1; i < LANG_COUNT; i++) {
        char cmp[16];
        strncpy(cmp, s_language_codes[i], sizeof(cmp) - 1);
        cmp[sizeof(cmp) - 1] = '\0';
        for (char* p = cmp; *p; p++) {
            if (*p >= 'A' && *p <= 'Z') *p += 32;
        }
        if (strcmp(lower_code, cmp) == 0) {
            return (Language)i;
        }
    }
    
    return LANG_EN_US;
}

// ==================== 编码设置 ====================
void i18n_set_encoding(TextEncoding encoding) {
    if (encoding >= 0 && encoding < 11) {
        g_current_encoding = encoding;
    }
}

TextEncoding i18n_get_encoding(void) {
    return g_current_encoding;
}

void i18n_set_console_encoding(void) {
#if STD_PLATFORM_WINDOWS
    if (g_current_lang == LANG_ZH_CN || g_current_lang == LANG_ZH_TW) {
        SetConsoleOutputCP(CP_UTF8);
        SetConsoleCP(CP_UTF8);
    } else {
        UINT cp = GetACP();
        SetConsoleOutputCP(cp);
        SetConsoleCP(cp);
    }
#else
    const char* locale_str;
    switch (g_current_lang) {
        case LANG_ZH_CN: locale_str = "zh_CN.UTF-8"; break;
        case LANG_ZH_TW: locale_str = "zh_TW.UTF-8"; break;
        case LANG_JA_JP: locale_str = "ja_JP.UTF-8"; break;
        case LANG_KO_KR: locale_str = "ko_KR.UTF-8"; break;
        default: locale_str = "C.UTF-8"; break;
    }
    setlocale(LC_ALL, locale_str);
#endif
}

// ==================== 翻译表管理 ====================
static int find_translation_entry(const char* key) {
    if (!g_translation_table || !key) return -1;
    
    for (size_t i = 0; i < g_translation_table->count; i++) {
        if (strcmp(g_translation_table->entries[i].key, key) == 0) {
            return (int)i;
        }
    }
    return -1;
}

static void ensure_translation_table_capacity(void) {
    if (!g_translation_table) return;
    
    if (g_translation_table->count >= g_translation_table->capacity) {
        size_t new_capacity = g_translation_table->capacity * 2;
        TranslationEntry* new_entries = (TranslationEntry*)realloc(
            g_translation_table->entries,
            sizeof(TranslationEntry) * new_capacity);
        if (new_entries) {
            g_translation_table->entries = new_entries;
            g_translation_table->capacity = new_capacity;
        }
    }
}

void i18n_register_translation(const char* key, Language lang, const char* value) {
    if (!g_translation_table || !key || lang < 0 || lang >= LANG_COUNT) return;
    
    int idx = find_translation_entry(key);
    if (idx >= 0) {
        // 更新已有条目
        TranslationEntry* entry = &g_translation_table->entries[idx];
        if (!entry->translations) {
            entry->translations = (const char**)calloc(LANG_COUNT, sizeof(char*));
        }
        if (entry->translations[lang]) {
            free((void*)entry->translations[lang]);
        }
        entry->translations[lang] = strdup(value ? value : "");
        return;
    }
    
    // 添加新条目
    ensure_translation_table_capacity();
    TranslationEntry* entry = &g_translation_table->entries[g_translation_table->count];
    entry->key = strdup(key);
    entry->translations = (const char**)calloc(LANG_COUNT, sizeof(char*));
    entry->translations[lang] = strdup(value ? value : "");
    g_translation_table->count++;
}

void i18n_register_translations(const TranslationEntry* entries, size_t count) {
    if (!entries) return;
    
    for (size_t i = 0; i < count; i++) {
        for (int lang = 0; lang < LANG_COUNT; lang++) {
            if (entries[i].translations && entries[i].translations[lang]) {
                i18n_register_translation(entries[i].key, (Language)lang, entries[i].translations[lang]);
            }
        }
    }
}

// ==================== 翻译功能 ====================
const char* i18n_translate(const char* key) {
    if (!key) return "";
    if (!g_translation_table) return key;
    
    int idx = find_translation_entry(key);
    if (idx < 0) return key;
    
    TranslationEntry* entry = &g_translation_table->entries[idx];
    if (!entry->translations) return key;
    
    // 优先使用当前语言
    if (g_current_lang > 0 && entry->translations[g_current_lang]) {
        return entry->translations[g_current_lang];
    }
    
    // 回退到英语
    if (entry->translations[LANG_EN_US]) {
        return entry->translations[LANG_EN_US];
    }
    
    // 返回第一个可用翻译
    for (int i = 1; i < LANG_COUNT; i++) {
        if (entry->translations[i]) {
            return entry->translations[i];
        }
    }
    
    return key;
}

const char* i18n_t(const char* key) {
    return i18n_translate(key);
}

const char* i18n_translate_args(const char* key, const char** args, size_t arg_count) {
    const char* template_str = i18n_translate(key);
    if (!template_str || !args || arg_count == 0) return template_str;
    
    // 简单参数替换: {0}, {1}, {2}, ...
    static char buffer[4096];
    char* dst = buffer;
    const char* src = template_str;
    size_t buffer_size = sizeof(buffer);
    size_t total_len = 0;
    
    while (*src && total_len < buffer_size - 1) {
        if (src[0] == '{' && src[1] >= '0' && src[1] <= '9') {
            int idx = src[1] - '0';
            if (src[2] == '}') {
                if ((size_t)idx < arg_count && args[idx]) {
                    size_t arg_len = strlen(args[idx]);
                    if (total_len + arg_len < buffer_size - 1) {
                        memcpy(dst, args[idx], arg_len);
                        dst += arg_len;
                        total_len += arg_len;
                    }
                }
                src += 3;
                continue;
            }
        }
        *dst++ = *src++;
        total_len++;
    }
    *dst = '\0';
    
    return buffer;
}

// ==================== 文件加载/保存 ====================
bool i18n_load_translation_file(const char* filepath) {
    if (!filepath) return false;
    
    FILE* file = fopen(filepath, "r");
    if (!file) return false;
    
    char line[2048];
    while (fgets(line, sizeof(line), file)) {
        // 跳过注释和空行
        if (line[0] == '#' || line[0] == '\n' || line[0] == '\r') continue;
        
        // 格式: [language_code] key = value
        char* bracket_start = strchr(line, '[');
        char* bracket_end = strchr(line, ']');
        if (!bracket_start || !bracket_end) continue;
        
        *bracket_end = '\0';
        Language lang = i18n_parse_language_code(bracket_start + 1);
        
        char* rest = bracket_end + 1;
        char* eq = strchr(rest, '=');
        if (!eq) continue;
        
        *eq = '\0';
        // 去除 key 的空格
        char* key = rest;
        while (*key == ' ' || *key == '\t') key++;
        char* key_end = eq - 1;
        while (key_end > key && (*key_end == ' ' || *key_end == '\t')) *key_end-- = '\0';
        
        char* value = eq + 1;
        while (*value == ' ' || *value == '\t') value++;
        // 去除尾部换行
        size_t value_len = strlen(value);
        while (value_len > 0 && (value[value_len-1] == '\n' || value[value_len-1] == '\r')) {
            value[--value_len] = '\0';
        }
        
        i18n_register_translation(key, lang, value);
    }
    
    fclose(file);
    return true;
}

bool i18n_save_translation_file(const char* filepath) {
    if (!filepath || !g_translation_table) return false;
    
    FILE* file = fopen(filepath, "w");
    if (!file) return false;
    
    fprintf(file, "# Translation File\n");
    fprintf(file, "# Format: [language_code] key = value\n\n");
    
    for (size_t i = 0; i < g_translation_table->count; i++) {
        TranslationEntry* entry = &g_translation_table->entries[i];
        if (!entry->translations) continue;
        
        for (int lang = 1; lang < LANG_COUNT; lang++) {
            if (entry->translations[lang]) {
                fprintf(file, "[%s] %s = %s\n", 
                        s_language_codes[lang], 
                        entry->key, 
                        entry->translations[lang]);
            }
        }
    }
    
    fclose(file);
    return true;
}

void i18n_clear_translations(void) {
    if (!g_translation_table) return;
    
    for (size_t i = 0; i < g_translation_table->count; i++) {
        TranslationEntry* entry = &g_translation_table->entries[i];
        if (entry->translations) {
            for (int j = 0; j < LANG_COUNT; j++) {
                if (entry->translations[j]) {
                    free((void*)entry->translations[j]);
                }
            }
            free(entry->translations);
            entry->translations = NULL;
        }
        if (entry->key) {
            free((void*)entry->key);
            entry->key = NULL;
        }
    }
    g_translation_table->count = 0;
}

// ==================== UTF-8 编码解码 ====================
int i18n_utf8_encode(wchar_t codepoint, char* out_buffer) {
    if (!out_buffer) return 0;
    
    if (codepoint < 0x80) {
        out_buffer[0] = (char)codepoint;
        return 1;
    } else if (codepoint < 0x800) {
        out_buffer[0] = (char)(0xC0 | (codepoint >> 6));
        out_buffer[1] = (char)(0x80 | (codepoint & 0x3F));
        return 2;
    } else if (codepoint < 0x10000) {
        out_buffer[0] = (char)(0xE0 | (codepoint >> 12));
        out_buffer[1] = (char)(0x80 | ((codepoint >> 6) & 0x3F));
        out_buffer[2] = (char)(0x80 | (codepoint & 0x3F));
        return 3;
    } else if (codepoint < 0x110000) {
        out_buffer[0] = (char)(0xF0 | (codepoint >> 18));
        out_buffer[1] = (char)(0x80 | ((codepoint >> 12) & 0x3F));
        out_buffer[2] = (char)(0x80 | ((codepoint >> 6) & 0x3F));
        out_buffer[3] = (char)(0x80 | (codepoint & 0x3F));
        return 4;
    }
    return 0;
}

int i18n_utf8_decode(const char* input, wchar_t* out_codepoint) {
    if (!input || !out_codepoint) return 0;
    
    unsigned char c = (unsigned char)input[0];
    
    if (c < 0x80) {
        *out_codepoint = c;
        return 1;
    } else if ((c & 0xE0) == 0xC0) {
        if ((input[1] & 0xC0) != 0x80) return 0;
        *out_codepoint = ((wchar_t)(c & 0x1F) << 6) | ((wchar_t)(input[1] & 0x3F));
        return 2;
    } else if ((c & 0xF0) == 0xE0) {
        if ((input[1] & 0xC0) != 0x80 || (input[2] & 0xC0) != 0x80) return 0;
        *out_codepoint = ((wchar_t)(c & 0x0F) << 12) | 
                         ((wchar_t)(input[1] & 0x3F) << 6) | 
                         ((wchar_t)(input[2] & 0x3F));
        return 3;
    } else if ((c & 0xF8) == 0xF0) {
        if ((input[1] & 0xC0) != 0x80 || (input[2] & 0xC0) != 0x80 || 
            (input[3] & 0xC0) != 0x80) return 0;
        *out_codepoint = ((wchar_t)(c & 0x07) << 18) | 
                         ((wchar_t)(input[1] & 0x3F) << 12) | 
                         ((wchar_t)(input[2] & 0x3F) << 6) | 
                         ((wchar_t)(input[3] & 0x3F));
        return 4;
    }
    
    return 0;
}

// ==================== UTF-8 字符串操作 ====================
size_t i18n_utf8_strlen(const char* str) {
    if (!str) return 0;
    
    size_t count = 0;
    while (*str) {
        unsigned char c = (unsigned char)*str;
        if ((c & 0x80) == 0) {
            str += 1;
        } else if ((c & 0xE0) == 0xC0) {
            str += 2;
        } else if ((c & 0xF0) == 0xE0) {
            str += 3;
        } else if ((c & 0xF8) == 0xF0) {
            str += 4;
        } else {
            str += 1;
        }
        count++;
    }
    return count;
}

size_t i18n_utf8_char_count(const char* str) {
    return i18n_utf8_strlen(str);
}

size_t i18n_utf8_byte_length(const char* str, size_t char_index) {
    if (!str) return 0;
    
    size_t idx = 0;
    while (*str && idx < char_index) {
        unsigned char c = (unsigned char)*str;
        if ((c & 0x80) == 0) str += 1;
        else if ((c & 0xE0) == 0xC0) str += 2;
        else if ((c & 0xF0) == 0xE0) str += 3;
        else if ((c & 0xF8) == 0xF0) str += 4;
        else str += 1;
        idx++;
    }
    
    if (!*str) return 0;
    
    unsigned char c = (unsigned char)*str;
    if ((c & 0x80) == 0) return 1;
    else if ((c & 0xE0) == 0xC0) return 2;
    else if ((c & 0xF0) == 0xE0) return 3;
    else if ((c & 0xF8) == 0xF0) return 4;
    return 1;
}

char* i18n_utf8_substring(const char* str, size_t start, size_t count) {
    if (!str) return NULL;
    
    // 找到起始位置
    size_t idx = 0;
    const char* start_ptr = str;
    while (*start_ptr && idx < start) {
        unsigned char c = (unsigned char)*start_ptr;
        if ((c & 0x80) == 0) start_ptr += 1;
        else if ((c & 0xE0) == 0xC0) start_ptr += 2;
        else if ((c & 0xF0) == 0xE0) start_ptr += 3;
        else if ((c & 0xF8) == 0xF0) start_ptr += 4;
        else start_ptr += 1;
        idx++;
    }
    
    if (!*start_ptr) return strdup("");
    
    // 找到结束位置
    const char* end_ptr = start_ptr;
    idx = 0;
    while (*end_ptr && idx < count) {
        unsigned char c = (unsigned char)*end_ptr;
        if ((c & 0x80) == 0) end_ptr += 1;
        else if ((c & 0xE0) == 0xC0) end_ptr += 2;
        else if ((c & 0xF0) == 0xE0) end_ptr += 3;
        else if ((c & 0xF8) == 0xF0) end_ptr += 4;
        else end_ptr += 1;
        idx++;
    }
    
    size_t byte_len = end_ptr - start_ptr;
    char* result = (char*)malloc(byte_len + 1);
    if (result) {
        memcpy(result, start_ptr, byte_len);
        result[byte_len] = '\0';
    }
    return result;
}

// ==================== UTF-8 验证和检测 ====================
bool i18n_is_utf8(const char* input) {
    if (!input) return true;
    
    while (*input) {
        unsigned char c = (unsigned char)*input;
        
        if ((c & 0x80) == 0) {
            input += 1;
        } else if ((c & 0xE0) == 0xC0) {
            if ((input[1] & 0xC0) != 0x80) return false;
            input += 2;
        } else if ((c & 0xF0) == 0xE0) {
            if ((input[1] & 0xC0) != 0x80 || (input[2] & 0xC0) != 0x80) return false;
            input += 3;
        } else if ((c & 0xF8) == 0xF0) {
            if ((input[1] & 0xC0) != 0x80 || (input[2] & 0xC0) != 0x80 || 
                (input[3] & 0xC0) != 0x80) return false;
            input += 4;
        } else {
            return false;
        }
    }
    return true;
}

bool i18n_detect_encoding(const char* input, TextEncoding* out_encoding) {
    if (!input || !out_encoding) return false;
    
    if (i18n_is_utf8(input)) {
        *out_encoding = ENCODING_UTF8;
        return true;
    }
    
    // 检查是否包含高字节 (可能为 GBK/Big5/Shift-JIS 等)
    const char* p = input;
    while (*p) {
        unsigned char c = (unsigned char)*p;
        if (c > 0x80) {
            // 简单启发式检测
            if (c >= 0xA1 && c <= 0xF7 && p[1] && p[1] >= 0xA1) {
                *out_encoding = ENCODING_GB2312;
                return true;
            }
            if (c >= 0x81 && c <= 0xFE && p[1]) {
                *out_encoding = ENCODING_GBK;
                return true;
            }
            break;
        }
        p++;
    }
    
    *out_encoding = ENCODING_AUTO;
    return false;
}

// ==================== 编码转换 ====================
char* i18n_convert_encoding(const char* input, TextEncoding from, TextEncoding to) {
    if (!input) return NULL;
    if (from == to) return strdup(input);
    
#if STD_PLATFORM_WINDOWS
    // 使用 Windows API 进行编码转换
    UINT from_cp, to_cp;
    
    switch (from) {
        case ENCODING_UTF8: from_cp = CP_UTF8; break;
        case ENCODING_GBK: case ENCODING_GB2312: from_cp = 936; break;
        case ENCODING_BIG5: from_cp = 950; break;
        case ENCODING_SHIFT_JIS: from_cp = 932; break;
        case ENCODING_EUC_KR: from_cp = 949; break;
        case ENCODING_WINDOWS_1252: from_cp = 1252; break;
        case ENCODING_ISO_8859_1: from_cp = 28591; break;
        default: from_cp = CP_UTF8; break;
    }
    
    switch (to) {
        case ENCODING_UTF8: to_cp = CP_UTF8; break;
        case ENCODING_GBK: case ENCODING_GB2312: to_cp = 936; break;
        case ENCODING_BIG5: to_cp = 950; break;
        case ENCODING_SHIFT_JIS: to_cp = 932; break;
        case ENCODING_EUC_KR: to_cp = 949; break;
        case ENCODING_WINDOWS_1252: to_cp = 1252; break;
        case ENCODING_ISO_8859_1: to_cp = 28591; break;
        default: to_cp = CP_UTF8; break;
    }
    
    // 先转换到 UTF-16
    int wide_len = MultiByteToWideChar(from_cp, 0, input, -1, NULL, 0);
    if (wide_len <= 0) return strdup(input);
    
    wchar_t* wide_str = (wchar_t*)malloc(sizeof(wchar_t) * wide_len);
    if (!wide_str) return NULL;
    
    MultiByteToWideChar(from_cp, 0, input, -1, wide_str, wide_len);
    
    // 再从 UTF-16 转换到目标编码
    int new_len = WideCharToMultiByte(to_cp, 0, wide_str, -1, NULL, 0, NULL, NULL);
    char* result = (char*)malloc(new_len);
    if (result) {
        WideCharToMultiByte(to_cp, 0, wide_str, -1, result, new_len, NULL, NULL);
    }
    
    free(wide_str);
    return result;
#else
    // POSIX 系统简化实现
    if (to == ENCODING_UTF8) return strdup(input);
    return strdup(input);
#endif
}

char* i18n_to_utf8(const char* input, TextEncoding from_encoding) {
    return i18n_convert_encoding(input, from_encoding, ENCODING_UTF8);
}

char* i18n_from_utf8(const char* input, TextEncoding to_encoding) {
    return i18n_convert_encoding(input, ENCODING_UTF8, to_encoding);
}

char* i18n_utf8_normalize(const char* input) {
    if (!input) return NULL;
    
    // 简化实现：确保字符串是有效的 UTF-8
    // 完整的 Unicode 规范化需要 NFC/NFD 算法
    return strdup(input);
}

// ==================== 本地化数字格式 ====================
char i18n_get_decimal_separator(Language lang) {
    switch (lang) {
        case LANG_FR_FR: case LANG_DE_DE: case LANG_ES_ES:
        case LANG_IT_IT: case LANG_PT_BR: case LANG_RU_RU:
        case LANG_NL_NL: case LANG_PL_PL: case LANG_TR_TR:
            return ',';
        default:
            return '.';
    }
}

char i18n_get_thousands_separator(Language lang) {
    switch (lang) {
        case LANG_FR_FR:
            return ' ';
        case LANG_DE_DE: case LANG_ES_ES: case LANG_IT_IT:
        case LANG_PT_BR: case LANG_RU_RU: case LANG_NL_NL:
        case LANG_PL_PL: case LANG_TR_TR:
            return '.';
        case LANG_ZH_CN: case LANG_ZH_TW: case LANG_JA_JP:
        case LANG_KO_KR: case LANG_HI_IN:
            return ',';
        case LANG_AR_SA:
            return ',';
        default:
            return ',';
    }
}

char* i18n_format_number(double value) {
    Language lang = (g_current_lang > 0) ? g_current_lang : LANG_EN_US;
    
    char buffer[128];
    char decimal_sep = i18n_get_decimal_separator(lang);
    char thousands_sep = i18n_get_thousands_separator(lang);
    
    // 分离整数和小数部分
    long long int_part = (long long)value;
    double frac_part = value - int_part;
    if (frac_part < 0) frac_part = -frac_part;
    
    // 格式化整数部分
    char int_str[64];
    char* dst = int_str + sizeof(int_str) - 1;
    *dst = '\0';
    
    if (int_part == 0) {
        *--dst = '0';
    } else {
        long long temp = int_part < 0 ? -int_part : int_part;
        int digits = 0;
        while (temp > 0) {
            if (digits > 0 && digits % 3 == 0) {
                *--dst = thousands_sep;
            }
            *--dst = '0' + (temp % 10);
            temp /= 10;
            digits++;
        }
    }
    
    if (int_part < 0) {
        *--dst = '-';
    }
    
    snprintf(buffer, sizeof(buffer), "%s", dst);
    
    // 添加小数部分
    if (frac_part > 0.0000001) {
        char frac_str[32];
        snprintf(frac_str, sizeof(frac_str), "%c%02g", decimal_sep, frac_part);
        strncat(buffer, frac_str, sizeof(buffer) - strlen(buffer) - 1);
    }
    
    return strdup(buffer);
}

char* i18n_format_currency(double amount, Language lang) {
    static char buffer[128];
    char* num_str = i18n_format_number(amount);
    if (!num_str) return strdup("");
    
    switch (lang) {
        case LANG_ZH_CN: case LANG_ZH_TW:
            snprintf(buffer, sizeof(buffer), "\xC2\xA5%s", num_str);
            break;
        case LANG_JA_JP:
            snprintf(buffer, sizeof(buffer), "\xC2\xA5%s", num_str);
            break;
        case LANG_KO_KR:
            snprintf(buffer, sizeof(buffer), "\xE2\x82\xA9%s", num_str);
            break;
        case LANG_EN_US: case LANG_ES_ES: case LANG_PT_BR:
            snprintf(buffer, sizeof(buffer), "$%s", num_str);
            break;
        case LANG_FR_FR: case LANG_DE_DE: case LANG_IT_IT:
        case LANG_NL_NL: case LANG_TR_TR:
            snprintf(buffer, sizeof(buffer), "%s\xE2\x82\xAC", num_str);
            break;
        case LANG_RU_RU:
            snprintf(buffer, sizeof(buffer), "%s\xE2\x82\xBD", num_str);
            break;
        case LANG_AR_SA:
            snprintf(buffer, sizeof(buffer), "\xD9\x80%s", num_str);
            break;
        case LANG_HI_IN:
            snprintf(buffer, sizeof(buffer), "\xE2\x82\xB9%s", num_str);
            break;
        default:
            snprintf(buffer, sizeof(buffer), "%s", num_str);
            break;
    }
    
    free(num_str);
    return strdup(buffer);
}

char* i18n_format_date(time_t timestamp, const char* format) {
    if (!format) return strdup("");
    
    struct tm* tm_info = localtime(&timestamp);
    static char buffer[128];
    
    Language lang = (g_current_lang > 0) ? g_current_lang : LANG_EN_US;
    
    // 根据语言选择默认格式
    char fmt[64];
    if (strcmp(format, "") == 0) {
        switch (lang) {
            case LANG_ZH_CN: case LANG_ZH_TW:
                strncpy(fmt, "%Y年%m月%d日", sizeof(fmt));
                break;
            case LANG_JA_JP:
                strncpy(fmt, "%Y年%m月%d日", sizeof(fmt));
                break;
            case LANG_KO_KR:
                strncpy(fmt, "%Y년 %m월 %d일", sizeof(fmt));
                break;
            case LANG_FR_FR:
                strncpy(fmt, "%d/%m/%Y", sizeof(fmt));
                break;
            case LANG_DE_DE:
                strncpy(fmt, "%d.%m.%Y", sizeof(fmt));
                break;
            case LANG_ES_ES: case LANG_IT_IT: case LANG_PT_BR:
                strncpy(fmt, "%d/%m/%Y", sizeof(fmt));
                break;
            default:
                strncpy(fmt, "%Y-%m-%d", sizeof(fmt));
                break;
        }
    } else {
        strncpy(fmt, format, sizeof(fmt));
    }
    
    fmt[sizeof(fmt) - 1] = '\0';
    strftime(buffer, sizeof(buffer), fmt, tm_info);
    return strdup(buffer);
}

char* i18n_format_date_time(time_t timestamp, const char* format) {
    if (!format) return strdup("");
    
    struct tm* tm_info = localtime(&timestamp);
    static char buffer[128];
    
    Language lang = (g_current_lang > 0) ? g_current_lang : LANG_EN_US;
    
    char fmt[64];
    if (strcmp(format, "") == 0) {
        switch (lang) {
            case LANG_ZH_CN: case LANG_ZH_TW:
                strncpy(fmt, "%Y年%m月%d日 %H:%M:%S", sizeof(fmt));
                break;
            case LANG_JA_JP:
                strncpy(fmt, "%Y年%m月%d日 %H:%M:%S", sizeof(fmt));
                break;
            case LANG_KO_KR:
                strncpy(fmt, "%Y년 %m월 %d일 %H:%M:%S", sizeof(fmt));
                break;
            case LANG_FR_FR: case LANG_DE_DE: case LANG_ES_ES:
            case LANG_IT_IT: case LANG_PT_BR:
                strncpy(fmt, "%d/%m/%Y %H:%M:%S", sizeof(fmt));
                break;
            default:
                strncpy(fmt, "%Y-%m-%d %H:%M:%S", sizeof(fmt));
                break;
        }
    } else {
        strncpy(fmt, format, sizeof(fmt));
    }
    
    fmt[sizeof(fmt) - 1] = '\0';
    strftime(buffer, sizeof(buffer), fmt, tm_info);
    return strdup(buffer);
}

// ==================== 平台适配 ====================
void i18n_setup_console(void) {
#if STD_PLATFORM_WINDOWS
    // 设置控制台输出为 UTF-8 编码
    SetConsoleOutputCP(CP_UTF8);
    SetConsoleCP(CP_UTF8);
    
    // 启用虚拟终端处理（支持 ANSI 转义序列）
    HANDLE hOut = GetStdHandle(STD_OUTPUT_HANDLE);
    if (hOut != INVALID_HANDLE_VALUE) {
        DWORD dwMode = 0;
        if (GetConsoleMode(hOut, &dwMode)) {
            dwMode |= ENABLE_VIRTUAL_TERMINAL_PROCESSING;
            SetConsoleMode(hOut, dwMode);
        }
    }
#else
    // Linux/Mac 设置 locale
    setlocale(LC_ALL, "");
#endif
}

void i18n_fix_console_output(void) {
    i18n_setup_console();
}
