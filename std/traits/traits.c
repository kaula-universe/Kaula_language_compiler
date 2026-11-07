#include "traits.h"
#include "../memory/memory.h"
#include "../string/string.h"
#include <string.h>

typedef struct TypeRegistryEntryInternal {
    char* type_name;
    TypeInfo info;
    struct TypeRegistryEntryInternal* next;
} TypeRegistryEntryInternal;

static TypeRegistryEntryInternal* g_registry_head = NULL;
static size_t g_registry_count = 0;

void traits_register(const char* name, const TypeInfo* info) {
    TypeRegistryEntryInternal* entry;
    TypeRegistryEntryInternal* existing;
    if (!name || !info) return;
    existing = g_registry_head;
    while (existing) {
        if (strcmp(existing->type_name, name) == 0) {
            existing->info = *info;
            return;
        }
        existing = (TypeRegistryEntryInternal*)existing->next;
    }
    entry = (TypeRegistryEntryInternal*)kmm_v4_malloc(sizeof(TypeRegistryEntryInternal));
    if (!entry) return;
    entry->type_name = string_create(name).ptr;
    entry->info = *info;
    entry->next = NULL;
    if (!g_registry_head) {
        g_registry_head = entry;
    } else {
        TypeRegistryEntryInternal* cur = g_registry_head;
        while (cur->next) {
            cur = (TypeRegistryEntryInternal*)cur->next;
        }
        cur->next = (struct TypeRegistryEntryInternal*)entry;
    }
    g_registry_count++;
}

const TypeInfo* traits_lookup(const char* name) {
    TypeRegistryEntryInternal* cur;
    if (!name) return NULL;
    cur = g_registry_head;
    while (cur) {
        if (strcmp(cur->type_name, name) == 0) {
            return &cur->info;
        }
        cur = (TypeRegistryEntryInternal*)cur->next;
    }
    return NULL;
}

void traits_list_all(void (*callback)(const TypeRegistryEntry*)) {
    TypeRegistryEntryInternal* cur;
    TypeRegistryEntry public_entry;
    if (!callback) return;
    cur = g_registry_head;
    while (cur) {
        public_entry.type_name = cur->type_name;
        public_entry.info = cur->info;
        callback(&public_entry);
        cur = (TypeRegistryEntryInternal*)cur->next;
    }
}

const char* trait_kind_name(TraitKind kind) {
    switch (kind) {
        case TRAIT_KIND_VOID:    return "void";
        case TRAIT_KIND_BOOL:    return "bool";
        case TRAIT_KIND_INT8:    return "int8";
        case TRAIT_KIND_INT16:   return "int16";
        case TRAIT_KIND_INT32:   return "int32";
        case TRAIT_KIND_INT64:   return "int64";
        case TRAIT_KIND_UINT8:   return "uint8";
        case TRAIT_KIND_UINT16:  return "uint16";
        case TRAIT_KIND_UINT32:  return "uint32";
        case TRAIT_KIND_UINT64:  return "uint64";
        case TRAIT_KIND_FLOAT32: return "float32";
        case TRAIT_KIND_FLOAT64: return "float64";
        case TRAIT_KIND_CHAR:    return "char";
        case TRAIT_KIND_PTR:     return "pointer";
        case TRAIT_KIND_ARRAY:   return "array";
        case TRAIT_KIND_STRUCT:  return "struct";
        case TRAIT_KIND_ENUM:    return "enum";
        case TRAIT_KIND_FUNC:    return "function";
        default:                 return "unknown";
    }
}

bool_t trait_is_numeric(TraitKind kind) {
    return trait_is_integer(kind) || trait_is_floating(kind);
}

bool_t trait_is_integer(TraitKind kind) {
    switch (kind) {
        case TRAIT_KIND_INT8:
        case TRAIT_KIND_INT16:
        case TRAIT_KIND_INT32:
        case TRAIT_KIND_INT64:
        case TRAIT_KIND_UINT8:
        case TRAIT_KIND_UINT16:
        case TRAIT_KIND_UINT32:
        case TRAIT_KIND_UINT64:
        case TRAIT_KIND_CHAR:
        case TRAIT_KIND_BOOL:
            return true;
        default:
            return false;
    }
}

bool_t trait_is_floating(TraitKind kind) {
    return kind == TRAIT_KIND_FLOAT32 || kind == TRAIT_KIND_FLOAT64;
}

bool_t trait_is_pointer(TraitKind kind) {
    return kind == TRAIT_KIND_PTR;
}
