/* _DEFAULT_SOURCE 必须在所有 #include 之前定义，以确保 realpath
   在 <stdlib.h> 中可见（现代 glibc 要求 _DEFAULT_SOURCE）。
   注意：_POSIX_C_SOURCE=200809L 会隐藏 realpath，必须用 _DEFAULT_SOURCE。 */
#ifndef _DEFAULT_SOURCE
#define _DEFAULT_SOURCE
#endif

#include "path.h"
#include "../memory/memory.h"
#include "../string/string.h"
#include <string.h>
#include <stdlib.h>
#include <stdio.h>

#ifdef _WIN32
#include <windows.h>
#include <direct.h>
#else
#include <unistd.h>
#include <limits.h>
#include <stdlib.h>
#endif

#include "../fs/fs.h"

static int is_sep(char c) {
#ifdef _WIN32
    return c == '/' || c == '\\';
#else
    return c == '/';
#endif
}

char path_separator(void) {
#ifdef _WIN32
    return '\\';
#else
    return '/';
#endif
}

const char* path_sep_str(void) {
#ifdef _WIN32
    return "\\";
#else
    return "/";
#endif
}

String path_join(const char* path1, const char* path2) {
    if (!path1 && !path2) return string_create("");
    if (!path1) return string_create(path2);
    if (!path2) return string_create(path1);

    size_t len1 = strlen(path1);
    size_t len2 = strlen(path2);
    char sep = path_separator();

    int need_sep = 1;
    if (len1 == 0 || is_sep(path1[len1 - 1])) need_sep = 0;
    if (len2 > 0 && is_sep(path2[0])) need_sep = 0;

    size_t total = len1 + len2 + (need_sep ? 1 : 0);
    char* result = (char*)kmm_v4_malloc(total + 1);
    if (!result) return STRING_EMPTY;

    memcpy(result, path1, len1);
    size_t pos = len1;
    if (need_sep) result[pos++] = sep;
    memcpy(result + pos, path2, len2);
    result[pos + len2] = '\0';

    return (String){.len = strlen(result), .ptr = result};
}

String path_join_n(const char** parts, size_t count) {
    if (!parts || count == 0) return string_create("");
    if (count == 1) return string_create(parts[0] ? parts[0] : "");

    size_t total = 0;
    char sep = path_separator();
    size_t i;

    for (i = 0; i < count; i++) {
        if (parts[i]) total += strlen(parts[i]);
    }
    total += (count - 1);

    char* result = (char*)kmm_v4_malloc(total + 1);
    if (!result) return STRING_EMPTY;

    size_t pos = 0;
    for (i = 0; i < count; i++) {
        const char* part = parts[i] ? parts[i] : "";
        size_t len = strlen(part);

        if (i > 0 && pos > 0 && !is_sep(result[pos - 1]) && len > 0 && !is_sep(part[0])) {
            result[pos++] = sep;
        }

        if (i > 0 && pos > 0 && is_sep(result[pos - 1]) && len > 0 && is_sep(part[0])) {
            part++;
            len--;
        }

        memcpy(result + pos, part, len);
        pos += len;
    }
    result[pos] = '\0';

    return (String){.len = strlen(result), .ptr = result};
}

String path_dirname(const char* path) {
    if (!path) return string_create("");

    size_t len = strlen(path);
    if (len == 0) return string_create(".");

    while (len > 1 && is_sep(path[len - 1])) {
        len--;
    }

    const char* slash = NULL;
    size_t i;
    for (i = 0; i < len; i++) {
        if (is_sep(path[i])) slash = path + i;
    }

#ifdef _WIN32
    if (!slash && len >= 2 && path[1] == ':') {
        char* result = (char*)kmm_v4_malloc(3);
        if (!result) return STRING_EMPTY;
        result[0] = path[0];
        result[1] = ':';
        result[2] = '\0';
        return (String){.len = strlen(result), .ptr = result};
    }
#endif

    if (!slash) return string_create(".");

#ifdef _WIN32
    if (slash == path && len >= 1 && is_sep(path[0])) {
        char* result = (char*)kmm_v4_malloc(2);
        if (!result) return STRING_EMPTY;
        result[0] = path[0];
        result[1] = '\0';
        return (String){.len = strlen(result), .ptr = result};
    }
#else
    if (slash == path) {
        return string_create("/");
    }
#endif

    size_t dir_len = (size_t)(slash - path);
    if (dir_len == 0) return string_create("/");

    char* result = (char*)kmm_v4_malloc(dir_len + 1);
    if (!result) return STRING_EMPTY;
    memcpy(result, path, dir_len);
    result[dir_len] = '\0';
    return (String){.len = strlen(result), .ptr = result};
}

String path_basename(const char* path) {
    if (!path) return string_create("");

    size_t len = strlen(path);
    if (len == 0) return string_create("");

    while (len > 0 && is_sep(path[len - 1])) {
        len--;
    }
    if (len == 0) {
        return string_create(path);
    }

    const char* start = path;
    size_t i;
    for (i = len - 1; i > 0; i--) {
        if (is_sep(path[i])) {
            start = path + i + 1;
            break;
        }
    }
    if (i == 0 && is_sep(path[0]) && start == path) {
        start = path + 1;
    }

    size_t base_len = len - (size_t)(start - path);
    char* result = (char*)kmm_v4_malloc(base_len + 1);
    if (!result) return STRING_EMPTY;
    memcpy(result, start, base_len);
    result[base_len] = '\0';
    return (String){.len = strlen(result), .ptr = result};
}

String path_extension(const char* path) {
    if (!path) return string_create("");

    String base = path_basename(path);
    if (base.len == 0) return string_create("");

    const char* dot = strrchr(base.ptr, '.');
    if (!dot || dot == base.ptr) {
        string_free(base);
        return string_create("");
    }

    String result = string_create(dot);
    string_free(base);
    return result;
}

String path_stem(const char* path) {
    if (!path) return string_create("");

    String base = path_basename(path);
    if (base.len == 0) return string_create("");

    char* dot = strrchr(base.ptr, '.');
    if (!dot || dot == base.ptr) {
        return base;
    }

    size_t stem_len = (size_t)(dot - base.ptr);
    char* result = (char*)kmm_v4_malloc(stem_len + 1);
    if (!result) {
        string_free(base);
        return STRING_EMPTY;
    }
    memcpy(result, base.ptr, stem_len);
    result[stem_len] = '\0';
    string_free(base);
    return (String){.len = strlen(result), .ptr = result};
}

String path_absolute(const char* path) {
    if (!path) return string_create("");

#ifdef _WIN32
    char buf[MAX_PATH];
    DWORD len = GetFullPathNameA(path, MAX_PATH, buf, NULL);
    if (len == 0 || len > MAX_PATH) return string_create(path);
    return string_create(buf);
#else
    char buf[PATH_MAX];
    if (realpath(path, buf)) return string_create(buf);
    return string_create(path);
#endif
}

String path_canonical(const char* path) {
    if (!path) return string_create("");

#ifdef _WIN32
    char buf[MAX_PATH];
    DWORD len = GetFullPathNameA(path, MAX_PATH, buf, NULL);
    if (len == 0 || len > MAX_PATH) return string_create(path);
    return string_create(buf);
#else
    char buf[PATH_MAX];
    if (realpath(path, buf)) return string_create(buf);
    return string_create(path);
#endif
}

static void normalize_separators(char* path) {
#ifdef _WIN32
    size_t i;
    for (i = 0; path[i]; i++) {
        if (path[i] == '/') path[i] = '\\';
    }
#endif
}

static String path_normalize_internal(const char* path) {
    if (!path) return string_create("");

    size_t len = strlen(path);
    char* buf = (char*)kmm_v4_malloc(len + 1);
    if (!buf) return STRING_EMPTY;
    memcpy(buf, path, len + 1);

    normalize_separators(buf);

    char sep = path_separator();
    size_t write_pos = 0;
    size_t i = 0;
    int is_abs = 0;

#ifdef _WIN32
    if (len >= 2 && buf[1] == ':') {
        buf[write_pos++] = buf[0];
        buf[write_pos++] = buf[1];
        i = 2;
        is_abs = 1;
    } else
#endif
    if (len > 0 && is_sep(buf[0])) {
        buf[write_pos++] = sep;
        i = 1;
        is_abs = 1;
        while (i < len && is_sep(buf[i])) i++;
    }

    size_t component_start = write_pos;
    int in_component = 0;

    while (i <= len) {
        if (i == len || is_sep(buf[i])) {
            if (in_component) {
                size_t comp_len = write_pos - component_start;
                if (comp_len == 1 && buf[component_start] == '.') {
                    write_pos = component_start;
                } else if (comp_len == 2 && buf[component_start] == '.' && buf[component_start + 1] == '.') {
                    if (component_start > (is_abs ? 1 : 0)) {
                        size_t prev_start = component_start - 1;
                        while (prev_start > (is_abs ? 1 : 0) && buf[prev_start - 1] != sep) {
                            prev_start--;
                        }
                        write_pos = prev_start;
                    } else if (!is_abs) {
                        if (write_pos > 0) buf[write_pos++] = sep;
                        buf[write_pos++] = '.';
                        buf[write_pos++] = '.';
                    } else {
                        write_pos = component_start;
                    }
                } else if (comp_len > 0) {
                    buf[write_pos++] = sep;
                }
                in_component = 0;
            }
            i++;
        } else {
            if (!in_component) {
                component_start = write_pos;
                in_component = 1;
            }
            buf[write_pos++] = buf[i++];
        }
    }

    if (write_pos == 0) {
        buf[write_pos++] = is_abs ? sep : '.';
    }

    buf[write_pos] = '\0';

    char* result = (char*)kmm_v4_malloc(write_pos + 1);
    if (!result) {
        kmm_v4_free(buf);
        return STRING_EMPTY;
    }
    memcpy(result, buf, write_pos + 1);
    kmm_v4_free(buf);
    return (String){.len = strlen(result), .ptr = result};
}

String path_normalize(const char* path) {
    return path_normalize_internal(path);
}

bool_t path_is_absolute(const char* path) {
    if (!path || !*path) return 0;
#ifdef _WIN32
    if (strlen(path) >= 2 && path[1] == ':') return 1;
    if (path[0] == '\\' || path[0] == '/') return 1;
    return 0;
#else
    return path[0] == '/';
#endif
}

bool_t path_is_relative(const char* path) {
    return !path_is_absolute(path);
}

bool_t path_has_extension(const char* path, const char* ext) {
    if (!path || !ext) return 0;

    const char* dot = strrchr(path, '.');
    if (!dot) return 0;

    const char* last_sep = strrchr(path, '/');
#ifdef _WIN32
    const char* last_bs = strrchr(path, '\\');
    if (last_bs && (!last_sep || last_bs > last_sep)) last_sep = last_bs;
#endif
    if (last_sep && dot < last_sep) return 0;

    if (*ext == '.') return strcmp(dot, ext) == 0;
    return strcmp(dot + 1, ext) == 0;
}

bool_t path_exists(const char* path) {
    return fs_exists(path);
}

String path_replace_extension(const char* path, const char* new_ext) {
    if (!path) return string_create("");
    if (!new_ext) return string_create(path);

    const char* dot = strrchr(path, '.');
    const char* last_sep = strrchr(path, '/');
#ifdef _WIN32
    const char* last_bs = strrchr(path, '\\');
    if (last_bs && (!last_sep || last_bs > last_sep)) last_sep = last_bs;
#endif

    if (!dot || (last_sep && dot < last_sep)) {
        size_t path_len = strlen(path);
        size_t ext_len = strlen(new_ext);
        int need_dot = (*new_ext != '.');
        char* result = (char*)kmm_v4_malloc(path_len + (need_dot ? 1 : 0) + ext_len + 1);
        if (!result) return STRING_EMPTY;
        memcpy(result, path, path_len);
        size_t pos = path_len;
        if (need_dot) result[pos++] = '.';
        memcpy(result + pos, new_ext, ext_len);
        result[pos + ext_len] = '\0';
        return (String){.len = strlen(result), .ptr = result};
    }

    size_t base_len = (size_t)(dot - path);
    size_t ext_len = strlen(new_ext);
    int need_dot = (*new_ext != '.');
    char* result = (char*)kmm_v4_malloc(base_len + (need_dot ? 1 : 0) + ext_len + 1);
    if (!result) return STRING_EMPTY;
    memcpy(result, path, base_len);
    size_t pos = base_len;
    if (need_dot) result[pos++] = '.';
    memcpy(result + pos, new_ext, ext_len);
    result[pos + ext_len] = '\0';
    return (String){.len = strlen(result), .ptr = result};
}

String path_without_extension(const char* path) {
    if (!path) return string_create("");

    const char* dot = strrchr(path, '.');
    const char* last_sep = strrchr(path, '/');
#ifdef _WIN32
    const char* last_bs = strrchr(path, '\\');
    if (last_bs && (!last_sep || last_bs > last_sep)) last_sep = last_bs;
#endif

    if (!dot || (last_sep && dot < last_sep)) return string_create(path);

    size_t len = (size_t)(dot - path);
    char* result = (char*)kmm_v4_malloc(len + 1);
    if (!result) return STRING_EMPTY;
    memcpy(result, path, len);
    result[len] = '\0';
    return (String){.len = strlen(result), .ptr = result};
}

String path_common_prefix(const char* path1, const char* path2) {
    if (!path1 || !path2) return string_create("");

    size_t len1 = strlen(path1);
    size_t len2 = strlen(path2);
    size_t min_len = len1 < len2 ? len1 : len2;

    size_t last_sep = 0;
    size_t i;
    for (i = 0; i < min_len; i++) {
        if (is_sep(path1[i]) && is_sep(path2[i])) {
            last_sep = i;
        } else if (path1[i] != path2[i]) {
            break;
        }
    }

    if (i == min_len) {
        if (len1 == len2) return string_create(path1);
        const char* shorter = len1 < len2 ? path1 : path2;
        if (is_sep(shorter[min_len - 1]) || min_len == 0) {
            return string_create(shorter);
        }
        if (len1 < len2 && is_sep(path2[min_len])) return string_create(path1);
        if (len2 < len1 && is_sep(path1[min_len])) return string_create(path2);
    }

    if (last_sep == 0 && i > 0) {
#ifdef _WIN32
        if (min_len >= 2 && path1[1] == ':' && path2[1] == ':' && path1[0] == path2[0]) {
            char* result = (char*)kmm_v4_malloc(3);
            if (!result) return STRING_EMPTY;
            result[0] = path1[0];
            result[1] = ':';
            result[2] = '\0';
            return (String){.len = strlen(result), .ptr = result};
        }
#endif
        if (is_sep(path1[0]) && is_sep(path2[0])) {
            return string_create(path_sep_str());
        }
    }

    if (last_sep == 0) return string_create("");

    char* result = (char*)kmm_v4_malloc(last_sep + 1);
    if (!result) return STRING_EMPTY;
    memcpy(result, path1, last_sep);
    result[last_sep] = '\0';
    return (String){.len = strlen(result), .ptr = result};
}

static size_t count_components(const char* path) {
    size_t count = 0;
    size_t i = 0;
    while (path[i]) {
        if (is_sep(path[i])) {
            i++;
            continue;
        }
        count++;
        while (path[i] && !is_sep(path[i])) i++;
    }
    return count;
}

static void split_components(const char* path, String* comps, size_t count) {
    size_t idx = 0;
    size_t i = 0;
    while (path[i] && idx < count) {
        if (is_sep(path[i])) { i++; continue; }
        const char* start = path + i;
        while (path[i] && !is_sep(path[i])) i++;
        size_t len = (size_t)(path + i - start);
        char* buf = (char*)kmm_v4_malloc(len + 1);
        if (buf) {
            memcpy(buf, start, len);
            buf[len] = '\0';
            comps[idx] = (String){.len = len, .ptr = buf};
        } else {
            comps[idx] = STRING_EMPTY;
        }
        idx++;
    }
}

String path_relative(const char* base, const char* target) {
    if (!base || !target) return string_create("");

    String norm_base = path_normalize_internal(base);
    String norm_target = path_normalize_internal(target);
    if (norm_base.len == 0 || norm_target.len == 0) {
        if (norm_base.len > 0) string_free(norm_base);
        if (norm_target.len > 0) string_free(norm_target);
        return string_create("");
    }

    size_t base_count = count_components(norm_base.ptr);
    size_t target_count = count_components(norm_target.ptr);

    String* base_comps = NULL;
    String* target_comps = NULL;

    if (base_count > 0) {
        base_comps = (String*)kmm_v4_malloc(base_count * sizeof(String));
        if (base_comps) split_components(norm_base.ptr, base_comps, base_count);
    }
    if (target_count > 0) {
        target_comps = (String*)kmm_v4_malloc(target_count * sizeof(String));
        if (target_comps) split_components(norm_target.ptr, target_comps, target_count);
    }

    size_t common = 0;
    while (common < base_count && common < target_count &&
           base_comps && target_comps &&
           strcmp(base_comps[common].ptr, target_comps[common].ptr) == 0) {
        common++;
    }

    size_t up_count = base_count - common;
    size_t total_len = 0;
    size_t i;

    for (i = 0; i < up_count; i++) {
        total_len += 3;
    }
    for (i = common; i < target_count; i++) {
        if (target_comps && target_comps[i].len > 0) {
            total_len += target_comps[i].len + 1;
        }
    }
    if (total_len == 0) total_len = 1;

    char* result = (char*)kmm_v4_malloc(total_len + 1);
    if (!result) {
        if (base_comps) {
            for (i = 0; i < base_count; i++)
                if (base_comps[i].ptr) kmm_v4_free(base_comps[i].ptr);
            kmm_v4_free(base_comps);
        }
        if (target_comps) {
            for (i = 0; i < target_count; i++)
                if (target_comps[i].ptr) kmm_v4_free(target_comps[i].ptr);
            kmm_v4_free(target_comps);
        }
        string_free(norm_base);
        string_free(norm_target);
        return STRING_EMPTY;
    }

    size_t pos = 0;
    char sep = path_separator();

    for (i = 0; i < up_count; i++) {
        if (pos > 0) result[pos++] = sep;
        result[pos++] = '.';
        result[pos++] = '.';
    }
    for (i = common; i < target_count; i++) {
        if (pos > 0) result[pos++] = sep;
        if (target_comps && target_comps[i].len > 0) {
            size_t len = target_comps[i].len;
            memcpy(result + pos, target_comps[i].ptr, len);
            pos += len;
        }
    }
    if (pos == 0) result[pos++] = '.';
    result[pos] = '\0';

    if (base_comps) {
        for (i = 0; i < base_count; i++)
            if (base_comps[i].ptr) kmm_v4_free(base_comps[i].ptr);
        kmm_v4_free(base_comps);
    }
    if (target_comps) {
        for (i = 0; i < target_count; i++)
            if (target_comps[i].ptr) kmm_v4_free(target_comps[i].ptr);
        kmm_v4_free(target_comps);
    }
    string_free(norm_base);
    string_free(norm_target);

    return (String){.len = strlen(result), .ptr = result};
}

bool_t path_is_subpath(const char* parent, const char* child) {
    if (!parent || !child) return 0;

    String norm_parent = path_normalize_internal(parent);
    String norm_child = path_normalize_internal(child);
    if (norm_parent.len == 0 || norm_child.len == 0) {
        if (norm_parent.len > 0) string_free(norm_parent);
        if (norm_child.len > 0) string_free(norm_child);
        return 0;
    }

    size_t parent_len = strlen(norm_parent.ptr);
    size_t child_len = strlen(norm_child.ptr);

    if (child_len <= parent_len) {
        string_free(norm_parent);
        string_free(norm_child);
        return 0;
    }

    bool_t result = 0;
    if (strncmp(norm_child.ptr, norm_parent.ptr, parent_len) == 0) {
        if (is_sep(norm_child.ptr[parent_len])) {
            result = 1;
        }
    }

    string_free(norm_parent);
    string_free(norm_child);
    return result;
}

String path_resolve(const char* base, const char* rel) {
    if (!base && !rel) return string_create("");
    if (!base) return path_normalize_internal(rel);
    if (!rel) return path_normalize_internal(base);

    if (path_is_absolute(rel)) return path_normalize_internal(rel);

    String joined = path_join(base, rel);
    if (joined.len == 0) return STRING_EMPTY;

    String result = path_normalize_internal(joined.ptr);
    string_free(joined);
    return result;
}

String* path_split(const char* path, size_t* out_count) {
    if (out_count) *out_count = 0;
    if (!path) return NULL;

    size_t count = count_components(path);
    if (count == 0) {
        String* result = (String*)kmm_v4_malloc(sizeof(String));
        if (!result) return NULL;
        result[0] = string_create("");
        if (out_count) *out_count = 1;
        return result;
    }

    String* result = (String*)kmm_v4_malloc(count * sizeof(String));
    if (!result) return NULL;

    split_components(path, result, count);

    if (out_count) *out_count = count;
    return result;
}
