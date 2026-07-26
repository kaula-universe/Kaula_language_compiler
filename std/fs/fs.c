/* _GNU_SOURCE 必须在所有 #include 之前定义，以确保 DT_DIR/DT_REG 等
   GNU 扩展在 <dirent.h> 中可见（Linux glibc 要求） */
#ifndef _GNU_SOURCE
#define _GNU_SOURCE
#endif

#include "fs.h"
#include "../memory/memory.h"
#include "../string/string.h"
#include <string.h>
#include <stdlib.h>
#include <stdio.h>

#ifdef _WIN32
#include <windows.h>
#include <io.h>
#include <direct.h>
#else
#include <sys/types.h>
#include <sys/stat.h>
#include <sys/statvfs.h>
#include <dirent.h>
#include <unistd.h>
#include <fcntl.h>
#include <errno.h>
#include <fnmatch.h>
#include <limits.h>
#include <time.h>
#endif

struct DirIterator {
#ifdef _WIN32
    HANDLE handle;
    WIN32_FIND_DATAA find_data;
    bool_t first;
    char* base_path;
#else
    DIR* dir;
    char* base_path;
#endif
};

#ifdef _WIN32
static i64 file_time_to_unix(FILETIME ft) {
    ULARGE_INTEGER uli;
    uli.LowPart = ft.dwLowDateTime;
    uli.HighPart = ft.dwHighDateTime;
    return (i64)(uli.QuadPart / 10000000 - 11644473600LL);
}
#endif

bool_t fs_exists(const char* path) {
    if (!path) return 0;
#ifdef _WIN32
    DWORD attr = GetFileAttributesA(path);
    return (attr != INVALID_FILE_ATTRIBUTES);
#else
    struct stat st;
    return (stat(path, &st) == 0);
#endif
}

bool_t fs_is_file(const char* path) {
    if (!path) return 0;
#ifdef _WIN32
    DWORD attr = GetFileAttributesA(path);
    if (attr == INVALID_FILE_ATTRIBUTES) return 0;
    return !(attr & FILE_ATTRIBUTE_DIRECTORY);
#else
    struct stat st;
    if (stat(path, &st) != 0) return 0;
    return S_ISREG(st.st_mode);
#endif
}

bool_t fs_is_dir(const char* path) {
    if (!path) return 0;
#ifdef _WIN32
    DWORD attr = GetFileAttributesA(path);
    if (attr == INVALID_FILE_ATTRIBUTES) return 0;
    return (attr & FILE_ATTRIBUTE_DIRECTORY) != 0;
#else
    struct stat st;
    if (stat(path, &st) != 0) return 0;
    return S_ISDIR(st.st_mode);
#endif
}

bool_t fs_is_symlink(const char* path) {
    if (!path) return 0;
#ifdef _WIN32
    DWORD attr = GetFileAttributesA(path);
    if (attr == INVALID_FILE_ATTRIBUTES) return 0;
    return (attr & FILE_ATTRIBUTE_REPARSE_POINT) != 0;
#else
    struct stat st;
    if (lstat(path, &st) != 0) return 0;
    return S_ISLNK(st.st_mode);
#endif
}

u64 fs_file_size(const char* path) {
    if (!path) return 0;
#ifdef _WIN32
    WIN32_FILE_ATTRIBUTE_DATA fad;
    if (!GetFileAttributesExA(path, GetFileExInfoStandard, &fad)) return 0;
    ULARGE_INTEGER uli;
    uli.LowPart = fad.nFileSizeLow;
    uli.HighPart = fad.nFileSizeHigh;
    return (u64)uli.QuadPart;
#else
    struct stat st;
    if (stat(path, &st) != 0) return 0;
    return (u64)st.st_size;
#endif
}

i64 fs_modified_time(const char* path) {
    if (!path) return 0;
#ifdef _WIN32
    WIN32_FILE_ATTRIBUTE_DATA fad;
    if (!GetFileAttributesExA(path, GetFileExInfoStandard, &fad)) return 0;
    return file_time_to_unix(fad.ftLastWriteTime);
#else
    struct stat st;
    if (stat(path, &st) != 0) return 0;
    return (i64)st.st_mtime;
#endif
}

i64 fs_created_time(const char* path) {
    if (!path) return 0;
#ifdef _WIN32
    WIN32_FILE_ATTRIBUTE_DATA fad;
    if (!GetFileAttributesExA(path, GetFileExInfoStandard, &fad)) return 0;
    return file_time_to_unix(fad.ftCreationTime);
#else
    struct stat st;
    if (stat(path, &st) != 0) return 0;
    return (i64)st.st_ctime;
#endif
}

bool_t fs_create_file(const char* path) {
    if (!path) return 0;
#ifdef _WIN32
    HANDLE h = CreateFileA(path, GENERIC_WRITE, 0, NULL, CREATE_NEW,
                           FILE_ATTRIBUTE_NORMAL, NULL);
    if (h == INVALID_HANDLE_VALUE) {
        if (GetLastError() == ERROR_FILE_EXISTS) return 1;
        return 0;
    }
    CloseHandle(h);
    return 1;
#else
    int fd = open(path, O_CREAT | O_WRONLY | O_EXCL, 0644);
    if (fd < 0) {
        if (errno == EEXIST) return 1;
        return 0;
    }
    close(fd);
    return 1;
#endif
}

bool_t fs_create_dir(const char* path) {
    if (!path) return 0;
#ifdef _WIN32
    if (!CreateDirectoryA(path, NULL)) {
        if (GetLastError() == ERROR_ALREADY_EXISTS) return 1;
        return 0;
    }
    return 1;
#else
    if (mkdir(path, 0755) != 0) {
        if (errno == EEXIST) return 1;
        return 0;
    }
    return 1;
#endif
}

bool_t fs_create_dir_all(const char* path) {
    if (!path) return 0;
    if (fs_is_dir(path)) return 1;

    size_t len = strlen(path);
    char* tmp = (char*)kmm_v4_malloc(len + 1);
    if (!tmp) return 0;
    memcpy(tmp, path, len + 1);

    size_t i;
    for (i = 1; i < len; i++) {
        if (tmp[i] == '/' || tmp[i] == '\\') {
            char c = tmp[i];
            tmp[i] = '\0';
            if (!fs_create_dir(tmp)) {
                kmm_v4_free(tmp);
                return 0;
            }
            tmp[i] = c;
        }
    }

    bool_t result = fs_create_dir(tmp);
    kmm_v4_free(tmp);
    return result;
}

bool_t fs_remove(const char* path) {
    if (!path) return 0;
#ifdef _WIN32
    DWORD attr = GetFileAttributesA(path);
    if (attr == INVALID_FILE_ATTRIBUTES) return 0;
    if (attr & FILE_ATTRIBUTE_DIRECTORY) {
        return RemoveDirectoryA(path) != 0;
    } else {
        return DeleteFileA(path) != 0;
    }
#else
    struct stat st;
    if (stat(path, &st) != 0) return 0;
    if (S_ISDIR(st.st_mode)) {
        return rmdir(path) == 0;
    } else {
        return unlink(path) == 0;
    }
#endif
}

static bool_t remove_all_recursive(const char* path) {
    if (!path) return 0;
    if (!fs_exists(path)) return 1;

    if (fs_is_dir(path) && !fs_is_symlink(path)) {
        DirIterator* it = fs_dir_open(path);
        if (!it) return 0;

        FileInfo info;
        while (fs_dir_next(it, &info)) {
            if (strcmp(info.name.ptr, ".") == 0 || strcmp(info.name.ptr, "..") == 0)
                continue;

            char* full = (char*)kmm_v4_malloc(strlen(path) + 1 + info.name.len + 1);
            if (!full) {
                fs_dir_close(it);
                return 0;
            }
            sprintf(full, "%s%c%s", path,
#ifdef _WIN32
                    '\\',
#else
                    '/',
#endif
                    info.name.ptr);

            bool_t ok = remove_all_recursive(full);
            kmm_v4_free(full);
            if (!ok) {
                fs_dir_close(it);
                return 0;
            }
        }
        fs_dir_close(it);
    }

    return fs_remove(path);
}

bool_t fs_remove_all(const char* path) {
    return remove_all_recursive(path);
}

bool_t fs_copy(const char* src, const char* dst) {
    if (!src || !dst) return 0;

    size_t size;
    char* data = fs_read_file(src, &size);
    if (!data) return 0;

    bool_t ok = fs_write_file(dst, data, size);
    kmm_v4_free(data);
    return ok;
}

bool_t fs_rename(const char* old_path, const char* new_path) {
    if (!old_path || !new_path) return 0;
#ifdef _WIN32
    return MoveFileA(old_path, new_path) != 0;
#else
    return rename(old_path, new_path) == 0;
#endif
}

bool_t fs_move(const char* src, const char* dst) {
    if (!src || !dst) return 0;
    if (fs_rename(src, dst)) return 1;

    if (!fs_copy(src, dst)) return 0;
    if (!fs_remove_all(src)) {
        fs_remove_all(dst);
        return 0;
    }
    return 1;
}

char* fs_read_file(const char* path, size_t* out_size) {
    if (out_size) *out_size = 0;
    if (!path) return NULL;

#ifdef _WIN32
    HANDLE h = CreateFileA(path, GENERIC_READ, FILE_SHARE_READ, NULL,
                           OPEN_EXISTING, FILE_ATTRIBUTE_NORMAL, NULL);
    if (h == INVALID_HANDLE_VALUE) return NULL;

    LARGE_INTEGER li;
    if (!GetFileSizeEx(h, &li)) {
        CloseHandle(h);
        return NULL;
    }
    size_t size = (size_t)li.QuadPart;

    char* buf = (char*)kmm_v4_malloc(size + 1);
    if (!buf) {
        CloseHandle(h);
        return NULL;
    }

    DWORD bytes_read;
    if (!ReadFile(h, buf, (DWORD)size, &bytes_read, NULL)) {
        CloseHandle(h);
        kmm_v4_free(buf);
        return NULL;
    }
    buf[bytes_read] = '\0';
    CloseHandle(h);

    if (out_size) *out_size = (size_t)bytes_read;
    return buf;
#else
    FILE* f = fopen(path, "rb");
    if (!f) return NULL;

    fseek(f, 0, SEEK_END);
    long size = ftell(f);
    fseek(f, 0, SEEK_SET);

    if (size < 0) {
        fclose(f);
        return NULL;
    }

    char* buf = (char*)kmm_v4_malloc((size_t)size + 1);
    if (!buf) {
        fclose(f);
        return NULL;
    }

    size_t nread = fread(buf, 1, (size_t)size, f);
    buf[nread] = '\0';
    fclose(f);

    if (out_size) *out_size = nread;
    return buf;
#endif
}

bool_t fs_write_file(const char* path, const char* data, size_t size) {
    if (!path || (!data && size > 0)) return 0;

#ifdef _WIN32
    HANDLE h = CreateFileA(path, GENERIC_WRITE, 0, NULL, CREATE_ALWAYS,
                           FILE_ATTRIBUTE_NORMAL, NULL);
    if (h == INVALID_HANDLE_VALUE) return 0;

    DWORD bytes_written;
    bool_t ok = (WriteFile(h, data, (DWORD)size, &bytes_written, NULL) != 0) &&
                (size_t)bytes_written == size;
    CloseHandle(h);
    return ok;
#else
    FILE* f = fopen(path, "wb");
    if (!f) return 0;

    size_t nwritten = fwrite(data, 1, size, f);
    fclose(f);
    return nwritten == size;
#endif
}

bool_t fs_append_file(const char* path, const char* data, size_t size) {
    if (!path || (!data && size > 0)) return 0;

#ifdef _WIN32
    HANDLE h = CreateFileA(path, FILE_APPEND_DATA, 0, NULL, OPEN_ALWAYS,
                           FILE_ATTRIBUTE_NORMAL, NULL);
    if (h == INVALID_HANDLE_VALUE) return 0;

    SetFilePointer(h, 0, NULL, FILE_END);

    DWORD bytes_written;
    bool_t ok = (WriteFile(h, data, (DWORD)size, &bytes_written, NULL) != 0) &&
                (size_t)bytes_written == size;
    CloseHandle(h);
    return ok;
#else
    FILE* f = fopen(path, "ab");
    if (!f) return 0;

    size_t nwritten = fwrite(data, 1, size, f);
    fclose(f);
    return nwritten == size;
#endif
}

String* fs_read_lines(const char* path, size_t* out_count) {
    if (out_count) *out_count = 0;
    if (!path) return NULL;

    size_t size;
    char* content = fs_read_file(path, &size);
    if (!content) return NULL;

    size_t capacity = 16;
    size_t count = 0;
    String* lines = (String*)kmm_v4_malloc(capacity * sizeof(String));
    if (!lines) {
        kmm_v4_free(content);
        return NULL;
    }

    size_t line_start = 0;
    size_t i;
    for (i = 0; i <= size; i++) {
        if (i == size || content[i] == '\n') {
            size_t line_len = i - line_start;
            if (line_len > 0 && content[i - 1] == '\r') {
                line_len--;
            }

            if (count >= capacity) {
                capacity *= 2;
                String* new_lines = (String*)kmm_v4_malloc(capacity * sizeof(String));
                if (!new_lines) {
                    size_t j;
                    for (j = 0; j < count; j++) string_free(lines[j]);
                    kmm_v4_free(lines);
                    kmm_v4_free(content);
                    return NULL;
                }
                memcpy(new_lines, lines, count * sizeof(String));
                kmm_v4_free(lines);
                lines = new_lines;
            }

            char* line_buf = (char*)kmm_v4_malloc(line_len + 1);
            if (line_buf) {
                memcpy(line_buf, content + line_start, line_len);
                line_buf[line_len] = '\0';
                lines[count] = (String){.len = line_len, .ptr = line_buf};
            } else {
                lines[count] = STRING_EMPTY;
            }
            count++;
            line_start = i + 1;
        }
    }

    kmm_v4_free(content);
    if (out_count) *out_count = count;
    return lines;
}

bool_t fs_write_lines(const char* path, const char** lines, size_t count) {
    if (!path || (!lines && count > 0)) return 0;

    size_t total = 0;
    size_t i;
    for (i = 0; i < count; i++) {
        if (lines[i]) total += strlen(lines[i]);
        total += 1;
    }

    char* buf = (char*)kmm_v4_malloc(total + 1);
    if (!buf) return 0;

    size_t pos = 0;
    for (i = 0; i < count; i++) {
        if (lines[i]) {
            size_t len = strlen(lines[i]);
            memcpy(buf + pos, lines[i], len);
            pos += len;
        }
        buf[pos++] = '\n';
    }
    buf[pos] = '\0';

    bool_t ok = fs_write_file(path, buf, pos);
    kmm_v4_free(buf);
    return ok;
}

DirIterator* fs_dir_open(const char* path) {
    if (!path) return NULL;

    DirIterator* it = (DirIterator*)kmm_v4_malloc(sizeof(DirIterator));
    if (!it) return NULL;
    memset(it, 0, sizeof(DirIterator));

    String base = string_create(path);
    it->base_path = base.ptr;

#ifdef _WIN32
    size_t len = strlen(path);
    char* wildcard = (char*)kmm_v4_malloc(len + 3);
    if (!wildcard) {
        kmm_v4_free(it->base_path);
        kmm_v4_free(it);
        return NULL;
    }
    sprintf(wildcard, "%s\\*", path);

    it->handle = FindFirstFileA(wildcard, &it->find_data);
    kmm_v4_free(wildcard);

    if (it->handle == INVALID_HANDLE_VALUE) {
        kmm_v4_free(it->base_path);
        kmm_v4_free(it);
        return NULL;
    }
    it->first = 1;
#else
    it->dir = opendir(path);
    if (!it->dir) {
        kmm_v4_free(it->base_path);
        kmm_v4_free(it);
        return NULL;
    }
#endif

    return it;
}

bool_t fs_dir_next(DirIterator* it, FileInfo* info) {
    if (!it || !info) return 0;

#ifdef _WIN32
    if (it->handle == INVALID_HANDLE_VALUE) return 0;
    if (!it->first) {
        if (!FindNextFileA(it->handle, &it->find_data)) return 0;
    }
    it->first = 0;

    memset(info, 0, sizeof(FileInfo));
    info->name = string_create(it->find_data.cFileName);

    size_t base_len = strlen(it->base_path);
    size_t name_len = strlen(it->find_data.cFileName);
    char* path_buf = (char*)kmm_v4_malloc(base_len + 1 + name_len + 1);
    if (path_buf) {
        sprintf(path_buf, "%s\\%s", it->base_path, it->find_data.cFileName);
        info->path = (String){.len = base_len + 1 + name_len, .ptr = path_buf};
    } else {
        info->path = STRING_EMPTY;
    }

    info->is_dir = (it->find_data.dwFileAttributes & FILE_ATTRIBUTE_DIRECTORY) != 0;
    info->is_file = !info->is_dir;
    info->is_symlink = (it->find_data.dwFileAttributes & FILE_ATTRIBUTE_REPARSE_POINT) != 0;

    ULARGE_INTEGER uli;
    uli.LowPart = it->find_data.nFileSizeLow;
    uli.HighPart = it->find_data.nFileSizeHigh;
    info->size = (u64)uli.QuadPart;

    info->created = file_time_to_unix(it->find_data.ftCreationTime);
    info->modified = file_time_to_unix(it->find_data.ftLastWriteTime);
    info->accessed = file_time_to_unix(it->find_data.ftLastAccessTime);
    info->mode = (u32)it->find_data.dwFileAttributes;

    return 1;
#else
    struct dirent* entry = readdir(it->dir);
    if (!entry) return 0;

    memset(info, 0, sizeof(FileInfo));
    info->name = string_create(entry->d_name);

    size_t base_len = strlen(it->base_path);
    size_t name_len = strlen(entry->d_name);
    char* path_buf = (char*)kmm_v4_malloc(base_len + 1 + name_len + 1);
    if (path_buf) {
        sprintf(path_buf, "%s/%s", it->base_path, entry->d_name);
        info->path = (String){.len = base_len + 1 + name_len, .ptr = path_buf};
    } else {
        info->path = STRING_EMPTY;
    }

    struct stat st;
    if (stat(info->path.ptr, &st) == 0) {
        info->is_dir = S_ISDIR(st.st_mode);
        info->is_file = S_ISREG(st.st_mode);
        info->is_symlink = S_ISLNK(st.st_mode);
        info->size = (u64)st.st_size;
        info->created = (i64)st.st_ctime;
        info->modified = (i64)st.st_mtime;
        info->accessed = (i64)st.st_atime;
        info->mode = (u32)st.st_mode;
    } else {
        info->is_dir = (entry->d_type == DT_DIR);
        info->is_file = (entry->d_type == DT_REG);
        info->is_symlink = (entry->d_type == DT_LNK);
    }

    return 1;
#endif
}

void fs_dir_close(DirIterator* it) {
    if (!it) return;
#ifdef _WIN32
    if (it->handle != INVALID_HANDLE_VALUE) {
        FindClose(it->handle);
    }
#else
    if (it->dir) closedir(it->dir);
#endif
    if (it->base_path) kmm_v4_free(it->base_path);
    kmm_v4_free(it);
}

#ifdef _WIN32
static bool_t glob_match(const char* pattern, const char* str) {
    while (*pattern && *str) {
        if (*pattern == '*') {
            while (*pattern == '*') pattern++;
            if (!*pattern) return 1;
            while (*str) {
                if (glob_match(pattern, str)) return 1;
                str++;
            }
            return 0;
        } else if (*pattern == '?') {
            pattern++;
            str++;
        } else {
            if (*pattern != *str) return 0;
            pattern++;
            str++;
        }
    }
    while (*pattern == '*') pattern++;
    return !*pattern && !*str;
}
#endif

static void glob_recursive(const char* pattern, const char* base_dir,
                           String** results, size_t* count, size_t* capacity) {
    const char* slash = strchr(pattern, '/');
#ifdef _WIN32
    const char* bslash = strchr(pattern, '\\');
    if (bslash && (!slash || bslash < slash)) slash = bslash;
#endif

    char current[1024];
    char remaining[1024];

    if (slash) {
        size_t seg_len = (size_t)(slash - pattern);
        if (seg_len >= sizeof(current)) return;
        memcpy(current, pattern, seg_len);
        current[seg_len] = '\0';
        strncpy(remaining, slash + 1, sizeof(remaining) - 1);
        remaining[sizeof(remaining) - 1] = '\0';
    } else {
        strncpy(current, pattern, sizeof(current) - 1);
        current[sizeof(current) - 1] = '\0';
        remaining[0] = '\0';
    }

    int has_wildcard = (strchr(current, '*') != NULL || strchr(current, '?') != NULL);

    if (!has_wildcard && !slash) {
        char full_path[2048];
        if (base_dir && base_dir[0]) {
            snprintf(full_path, sizeof(full_path), "%s%c%s", base_dir,
#ifdef _WIN32
                     '\\',
#else
                     '/',
#endif
                     current);
        } else {
            strncpy(full_path, current, sizeof(full_path) - 1);
            full_path[sizeof(full_path) - 1] = '\0';
        }
        if (fs_exists(full_path)) {
            if (*count >= *capacity) {
                size_t new_cap = *capacity * 2;
                String* new_arr = (String*)kmm_v4_malloc(new_cap * sizeof(String));
                if (!new_arr) return;
                memcpy(new_arr, *results, *count * sizeof(String));
                kmm_v4_free(*results);
                *results = new_arr;
                *capacity = new_cap;
            }
            (*results)[*count] = string_create(full_path);
            (*count)++;
        }
        return;
    }

    if (!has_wildcard && slash) {
        char new_base[2048];
        if (base_dir && base_dir[0]) {
            snprintf(new_base, sizeof(new_base), "%s%c%s", base_dir,
#ifdef _WIN32
                     '\\',
#else
                     '/',
#endif
                     current);
        } else {
            strncpy(new_base, current, sizeof(new_base) - 1);
            new_base[sizeof(new_base) - 1] = '\0';
        }
        if (fs_is_dir(new_base)) {
            glob_recursive(remaining, new_base, results, count, capacity);
        }
        return;
    }

    DirIterator* it = fs_dir_open(base_dir && base_dir[0] ? base_dir : ".");
    if (!it) return;

    FileInfo info;
    while (fs_dir_next(it, &info)) {
        if (strcmp(info.name.ptr, ".") == 0 || strcmp(info.name.ptr, "..") == 0) {
            string_free(info.path);
            string_free(info.name);
            continue;
        }

#ifdef _WIN32
        bool_t matched = glob_match(current, info.name.ptr);
#else
        bool_t matched = (fnmatch(current, info.name.ptr, 0) == 0);
#endif

        if (matched) {
            if (!slash) {
                if (*count >= *capacity) {
                    size_t new_cap = *capacity * 2;
                    String* new_arr = (String*)kmm_v4_malloc(new_cap * sizeof(String));
                    if (!new_arr) {
                        string_free(info.path);
                        string_free(info.name);
                        fs_dir_close(it);
                        return;
                    }
                    memcpy(new_arr, *results, *count * sizeof(String));
                    kmm_v4_free(*results);
                    *results = new_arr;
                    *capacity = new_cap;
                }
                (*results)[*count] = string_copy(info.path);
                (*count)++;
            } else if (info.is_dir) {
                glob_recursive(remaining, info.path.ptr, results, count, capacity);
            }
        }

        string_free(info.path);
        string_free(info.name);
    }
    fs_dir_close(it);
}

String* fs_glob(const char* pattern, size_t* out_count) {
    if (out_count) *out_count = 0;
    if (!pattern) return NULL;

    size_t capacity = 64;
    size_t count = 0;
    String* results = (String*)kmm_v4_malloc(capacity * sizeof(String));
    if (!results) return NULL;

    glob_recursive(pattern, "", &results, &count, &capacity);

    if (out_count) *out_count = count;
    return results;
}

bool_t fs_get_info(const char* path, FileInfo* info) {
    if (!path || !info) return 0;
    if (!fs_exists(path)) return 0;

    memset(info, 0, sizeof(FileInfo));
    info->path = string_create(path);

    const char* name = strrchr(path, '/');
#ifdef _WIN32
    const char* bname = strrchr(path, '\\');
    if (bname && (!name || bname > name)) name = bname;
#endif
    if (name) {
        info->name = string_create(name + 1);
    } else {
        info->name = string_create(path);
    }

    info->is_dir = fs_is_dir(path);
    info->is_file = fs_is_file(path);
    info->is_symlink = fs_is_symlink(path);
    info->size = fs_file_size(path);
    info->modified = fs_modified_time(path);
    info->created = fs_created_time(path);

#ifdef _WIN32
    DWORD attr = GetFileAttributesA(path);
    info->mode = (attr != INVALID_FILE_ATTRIBUTES) ? (u32)attr : 0;

    WIN32_FILE_ATTRIBUTE_DATA fad;
    if (GetFileAttributesExA(path, GetFileExInfoStandard, &fad)) {
        info->accessed = file_time_to_unix(fad.ftLastAccessTime);
    }
#else
    struct stat st;
    if (stat(path, &st) == 0) {
        info->accessed = (i64)st.st_atime;
        info->mode = (u32)st.st_mode;
    }
#endif

    return 1;
}

i64 fs_free_space(const char* path) {
    if (!path) return 0;
#ifdef _WIN32
    ULARGE_INTEGER free_bytes;
    if (!GetDiskFreeSpaceExA(path, &free_bytes, NULL, NULL)) return 0;
    return (i64)free_bytes.QuadPart;
#else
    struct statvfs st;
    if (statvfs(path, &st) != 0) return 0;
    return (i64)(st.f_bavail * st.f_frsize);
#endif
}

i64 fs_total_space(const char* path) {
    if (!path) return 0;
#ifdef _WIN32
    ULARGE_INTEGER total_bytes;
    if (!GetDiskFreeSpaceExA(path, NULL, &total_bytes, NULL)) return 0;
    return (i64)total_bytes.QuadPart;
#else
    struct statvfs st;
    if (statvfs(path, &st) != 0) return 0;
    return (i64)(st.f_blocks * st.f_frsize);
#endif
}

String fs_temp_dir(void) {
#ifdef _WIN32
    char buf[MAX_PATH];
    DWORD len = GetTempPathA(MAX_PATH, buf);
    if (len == 0 || len > MAX_PATH) return string_create(".");
    if (len > 0 && buf[len - 1] == '\\') buf[len - 1] = '\0';
    return string_create(buf);
#else
    const char* tmp = getenv("TMPDIR");
    if (!tmp) tmp = getenv("TMP");
    if (!tmp) tmp = getenv("TEMP");
    if (!tmp) tmp = "/tmp";
    return string_create(tmp);
#endif
}

String fs_temp_file(const char* prefix) {
    String tmpdir = fs_temp_dir();
    if (tmpdir.len == 0) return STRING_EMPTY;

    size_t prefix_len = prefix ? strlen(prefix) : 0;
    size_t dir_len = tmpdir.len;
    size_t total = dir_len + 1 + prefix_len + 16;

    char* tmpl = (char*)kmm_v4_malloc(total + 1);
    if (!tmpl) {
        string_free(tmpdir);
        return STRING_EMPTY;
    }

    sprintf(tmpl, "%s%c%sXXXXXX", tmpdir.ptr,
#ifdef _WIN32
            '\\',
#else
            '/',
#endif
            prefix ? prefix : "");

    string_free(tmpdir);

#ifdef _WIN32
    if (_mktemp_s(tmpl, total + 1) != 0) {
        kmm_v4_free(tmpl);
        return STRING_EMPTY;
    }
    return string_create(tmpl);
#else
    int fd = mkstemp(tmpl);
    if (fd < 0) {
        kmm_v4_free(tmpl);
        return STRING_EMPTY;
    }
    close(fd);
    return string_create(tmpl);
#endif
}
