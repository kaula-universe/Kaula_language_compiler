#include "archive.h"
#include "../memory/memory.h"
#include "../string/string.h"
#include "../fs/fs.h"
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <time.h>

struct Archive {
    FILE* file;
    ArchiveFormat format;
    bool_t writing;
};

struct ArchiveEntry {
    char* name;
    u64 size;
    u64 mtime;
    bool_t is_directory;
};

Archive* archive_open_read(const char* path, ArchiveFormat format) {
    Archive* arch = (Archive*)kmm_v4_malloc(sizeof(Archive));
    if (!arch) return NULL;
    
    arch->file = fopen(path, "rb");
    if (!arch->file) {
        kmm_v4_free(arch);
        return NULL;
    }
    
    arch->format = format;
    arch->writing = false;
    
    return arch;
}

Archive* archive_open_write(const char* path, ArchiveFormat format) {
    Archive* arch = (Archive*)kmm_v4_malloc(sizeof(Archive));
    if (!arch) return NULL;
    
    arch->file = fopen(path, "wb");
    if (!arch->file) {
        kmm_v4_free(arch);
        return NULL;
    }
    
    arch->format = format;
    arch->writing = true;
    
    return arch;
}

void archive_close(Archive* arch) {
    if (!arch) return;
    if (arch->file) fclose(arch->file);
    kmm_v4_free(arch);
}

bool_t archive_next_entry(Archive* arch, ArchiveEntry* entry) {
    if (!arch || !entry || arch->writing) return false;
    
    char line[256];
    if (!fgets(line, sizeof(line), arch->file)) return false;
    
    size_t len = strlen(line);
    if (len > 0 && line[len - 1] == '\n') line[len - 1] = '\0';
    
    entry->name = kmm_v4_strdup(line);
    entry->size = 0;
    entry->mtime = (u64)time(NULL);
    entry->is_directory = line[len - 1] == '/';
    
    return true;
}

bool_t archive_read_entry(Archive* arch, void* buffer, size_t* size) {
    if (!arch || !buffer || !size || arch->writing) return false;
    
    *size = 0;
    return true;
}

bool_t archive_write_entry(Archive* arch, const ArchiveEntry* entry, const void* data, size_t size) {
    if (!arch || !entry || !data || !arch->writing) return false;
    
    fprintf(arch->file, "%s\n", entry->name);
    if (!entry->is_directory) {
        fwrite(data, 1, size, arch->file);
    }
    
    return true;
}

const char* archive_entry_name(const ArchiveEntry* entry) {
    return entry ? entry->name : NULL;
}

u64 archive_entry_size(const ArchiveEntry* entry) {
    return entry ? entry->size : 0;
}

u64 archive_entry_mtime(const ArchiveEntry* entry) {
    return entry ? entry->mtime : 0;
}

bool_t archive_entry_is_directory(const ArchiveEntry* entry) {
    return entry ? entry->is_directory : false;
}

void archive_entry_set_name(ArchiveEntry* entry, const char* name) {
    if (!entry) return;
    kmm_v4_free(entry->name);
    entry->name = kmm_v4_strdup(name);
}

void archive_entry_set_size(ArchiveEntry* entry, u64 size) {
    if (entry) entry->size = size;
}

void archive_entry_set_mtime(ArchiveEntry* entry, u64 mtime) {
    if (entry) entry->mtime = mtime;
}

void archive_entry_set_is_directory(ArchiveEntry* entry, bool_t is_dir) {
    if (entry) entry->is_directory = is_dir;
}

bool_t archive_extract(Archive* arch, const char* dest_dir) {
    if (!arch || !dest_dir || arch->writing) return false;
    
    fs_create_dir(dest_dir);
    
    ArchiveEntry entry;
    while (archive_next_entry(arch, &entry)) {
        char path[1024];
        snprintf(path, sizeof(path), "%s/%s", dest_dir, entry.name);
        
        if (entry.is_directory) {
            fs_create_dir(path);
        } else {
            size_t size = 0;
            void* data = kmm_v4_malloc(1);
            archive_read_entry(arch, data, &size);
            
            FILE* file = fopen(path, "wb");
            if (file) {
                fwrite(data, 1, size, file);
                fclose(file);
            }
            kmm_v4_free(data);
        }
        
        kmm_v4_free(entry.name);
    }
    
    return true;
}

bool_t archive_extract_entry(Archive* arch, const ArchiveEntry* entry, const char* dest_path) {
    if (!arch || !entry || !dest_path || arch->writing) return false;
    
    if (entry->is_directory) {
        fs_create_dir(dest_path);
        return true;
    } else {
        size_t size = 0;
        void* data = kmm_v4_malloc(1);
        archive_read_entry(arch, data, &size);
        
        FILE* file = fopen(dest_path, "wb");
        if (!file) {
            kmm_v4_free(data);
            return false;
        }
        
        fwrite(data, 1, size, file);
        fclose(file);
        kmm_v4_free(data);
        
        return true;
    }
}

bool_t archive_add_file(Archive* arch, const char* file_path, const char* archive_path) {
    if (!arch || !file_path || !archive_path || !arch->writing) return false;
    
    FILE* file = fopen(file_path, "rb");
    if (!file) return false;
    
    fseek(file, 0, SEEK_END);
    size_t size = (size_t)ftell(file);
    fseek(file, 0, SEEK_SET);
    
    void* data = kmm_v4_malloc(size);
    fread(data, 1, size, file);
    fclose(file);
    
    ArchiveEntry entry;
    entry.name = kmm_v4_strdup(archive_path);
    entry.size = size;
    entry.mtime = (u64)time(NULL);
    entry.is_directory = false;
    
    bool_t result = archive_write_entry(arch, &entry, data, size);
    
    kmm_v4_free(entry.name);
    kmm_v4_free(data);
    
    return result;
}

bool_t archive_add_directory(Archive* arch, const char* dir_path, const char* archive_path) {
    if (!arch || !dir_path || !archive_path || !arch->writing) return false;
    
    ArchiveEntry entry;
    char name[1024];
    snprintf(name, sizeof(name), "%s/", archive_path);
    entry.name = kmm_v4_strdup(name);
    entry.size = 0;
    entry.mtime = (u64)time(NULL);
    entry.is_directory = true;
    
    bool_t result = archive_write_entry(arch, &entry, NULL, 0);
    kmm_v4_free(entry.name);
    
    return result;
}

bool_t archive_compress_file(const char* source, const char* dest, ArchiveFormat format) {
    FILE* src = fopen(source, "rb");
    if (!src) return false;
    
    FILE* dst = fopen(dest, "wb");
    if (!dst) {
        fclose(src);
        return false;
    }
    
    char buffer[4096];
    size_t read;
    while ((read = fread(buffer, 1, sizeof(buffer), src)) > 0) {
        fwrite(buffer, 1, read, dst);
    }
    
    fclose(src);
    fclose(dst);
    
    return true;
}

bool_t archive_decompress_file(const char* source, const char* dest, ArchiveFormat format) {
    FILE* src = fopen(source, "rb");
    if (!src) return false;
    
    FILE* dst = fopen(dest, "wb");
    if (!dst) {
        fclose(src);
        return false;
    }
    
    char buffer[4096];
    size_t read;
    while ((read = fread(buffer, 1, sizeof(buffer), src)) > 0) {
        fwrite(buffer, 1, read, dst);
    }
    
    fclose(src);
    fclose(dst);
    
    return true;
}
