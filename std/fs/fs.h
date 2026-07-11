#pragma once
#include "../base/types.h"
#include "../string/string.h"

typedef struct {
    String path;
    String name;
    bool_t is_dir;
    bool_t is_file;
    bool_t is_symlink;
    u64    size;
    i64    created;
    i64    modified;
    i64    accessed;
    u32    mode;
} FileInfo;

typedef struct DirIterator DirIterator;

bool_t fs_exists(const char* path);
bool_t fs_is_file(const char* path);
bool_t fs_is_dir(const char* path);
bool_t fs_is_symlink(const char* path);
u64    fs_file_size(const char* path);
i64    fs_modified_time(const char* path);
i64    fs_created_time(const char* path);

bool_t fs_create_file(const char* path);
bool_t fs_create_dir(const char* path);
bool_t fs_create_dir_all(const char* path);
bool_t fs_remove(const char* path);
bool_t fs_remove_all(const char* path);
bool_t fs_copy(const char* src, const char* dst);
bool_t fs_rename(const char* old_path, const char* new_path);
bool_t fs_move(const char* src, const char* dst);

char*  fs_read_file(const char* path, size_t* out_size);
bool_t fs_write_file(const char* path, const char* data, size_t size);
bool_t fs_append_file(const char* path, const char* data, size_t size);

String* fs_read_lines(const char* path, size_t* out_count);
bool_t  fs_write_lines(const char* path, const char** lines, size_t count);

DirIterator* fs_dir_open(const char* path);
bool_t       fs_dir_next(DirIterator* it, FileInfo* info);
void         fs_dir_close(DirIterator* it);

String* fs_glob(const char* pattern, size_t* out_count);

bool_t fs_get_info(const char* path, FileInfo* info);

i64    fs_free_space(const char* path);
i64    fs_total_space(const char* path);

String fs_temp_dir(void);
String fs_temp_file(const char* prefix);
