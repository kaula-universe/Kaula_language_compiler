#pragma once
#include "../base/types.h"
#include "../string/string.h"

char path_separator(void);
const char* path_sep_str(void);

String path_join(const char* path1, const char* path2);
String path_join_n(const char** parts, size_t count);

String path_dirname(const char* path);
String path_basename(const char* path);
String path_extension(const char* path);
String path_stem(const char* path);

String path_absolute(const char* path);
String path_canonical(const char* path);
String path_normalize(const char* path);

bool_t path_is_absolute(const char* path);
bool_t path_is_relative(const char* path);
bool_t path_has_extension(const char* path, const char* ext);
bool_t path_exists(const char* path);

String path_replace_extension(const char* path, const char* new_ext);
String path_without_extension(const char* path);

String path_common_prefix(const char* path1, const char* path2);
String path_relative(const char* base, const char* target);

bool_t path_is_subpath(const char* parent, const char* child);
String path_resolve(const char* base, const char* rel);

String* path_split(const char* path, size_t* out_count);
