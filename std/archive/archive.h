#ifndef STD_ARCHIVE_ARCHIVE_H
#define STD_ARCHIVE_ARCHIVE_H

#include "../base/types.h"

typedef struct Archive Archive;
typedef struct ArchiveEntry ArchiveEntry;

typedef enum {
    ARCHIVE_TAR,
    ARCHIVE_ZIP,
    ARCHIVE_GZIP,
    ARCHIVE_BZIP2
} ArchiveFormat;

Archive* archive_open_read(const char* path, ArchiveFormat format);
Archive* archive_open_write(const char* path, ArchiveFormat format);
void archive_close(Archive* arch);

bool_t archive_next_entry(Archive* arch, ArchiveEntry* entry);
bool_t archive_read_entry(Archive* arch, void* buffer, size_t* size);
bool_t archive_write_entry(Archive* arch, const ArchiveEntry* entry, const void* data, size_t size);

const char* archive_entry_name(const ArchiveEntry* entry);
u64 archive_entry_size(const ArchiveEntry* entry);
u64 archive_entry_mtime(const ArchiveEntry* entry);
bool_t archive_entry_is_directory(const ArchiveEntry* entry);

void archive_entry_set_name(ArchiveEntry* entry, const char* name);
void archive_entry_set_size(ArchiveEntry* entry, u64 size);
void archive_entry_set_mtime(ArchiveEntry* entry, u64 mtime);
void archive_entry_set_is_directory(ArchiveEntry* entry, bool_t is_dir);

bool_t archive_extract(Archive* arch, const char* dest_dir);
bool_t archive_extract_entry(Archive* arch, const ArchiveEntry* entry, const char* dest_path);

bool_t archive_add_file(Archive* arch, const char* file_path, const char* archive_path);
bool_t archive_add_directory(Archive* arch, const char* dir_path, const char* archive_path);

bool_t archive_compress_file(const char* source, const char* dest, ArchiveFormat format);
bool_t archive_decompress_file(const char* source, const char* dest, ArchiveFormat format);

#endif
