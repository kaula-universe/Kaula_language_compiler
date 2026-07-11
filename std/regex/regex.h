#pragma once
#include "../base/types.h"
#include "../string/string.h"

typedef struct {
    const char* pattern;
    void* compiled;
    bool_t valid;
} Regex;

typedef struct {
    size_t start;
    size_t end;
    String text;
} RegexMatch;

Regex* regex_create(const char* pattern);
void regex_destroy(Regex* regex);
bool_t regex_is_valid(const Regex* regex);
const char* regex_error(const Regex* regex);

bool_t regex_match(const Regex* regex, const char* str);
RegexMatch* regex_find(const Regex* regex, const char* str, size_t* count);
RegexMatch* regex_find_all(const Regex* regex, const char* str, size_t* count);
String regex_replace(const Regex* regex, const char* str, const char* replacement);
String* regex_split(const Regex* regex, const char* str, size_t* count);

RegexMatch* regex_capture_groups(const Regex* regex, const char* str, size_t* count);

bool_t regex_match_simple(const char* pattern, const char* str);
String regex_replace_simple(const char* pattern, const char* str, const char* replacement);
RegexMatch* regex_find_simple(const char* pattern, const char* str, size_t* count);
