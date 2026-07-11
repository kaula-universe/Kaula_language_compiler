#pragma once
#include "../base/types.h"
#include "../string/string.h"
#include <stddef.h>

// Type kind enumeration
typedef enum {
    TRAIT_KIND_VOID,
    TRAIT_KIND_BOOL,
    TRAIT_KIND_INT8, TRAIT_KIND_INT16, TRAIT_KIND_INT32, TRAIT_KIND_INT64,
    TRAIT_KIND_UINT8, TRAIT_KIND_UINT16, TRAIT_KIND_UINT32, TRAIT_KIND_UINT64,
    TRAIT_KIND_FLOAT32, TRAIT_KIND_FLOAT64,
    TRAIT_KIND_CHAR,
    TRAIT_KIND_PTR,
    TRAIT_KIND_ARRAY,
    TRAIT_KIND_STRUCT,
    TRAIT_KIND_ENUM,
    TRAIT_KIND_FUNC,
    TRAIT_KIND_UNKNOWN
} TraitKind;

// Type info structure
typedef struct {
    const char* name;
    TraitKind kind;
    size_t size;
    size_t align;
    bool_t is_signed;
    bool_t is_floating;
    bool_t is_const;
    bool_t is_volatile;
} TypeInfo;

// Compile-time type info (zero overhead, resolved at compile time)
#define TYPE_INFO(type) ((TypeInfo){ \
    .name = #type, \
    .kind = _TRAIT_KIND(type), \
    .size = sizeof(type), \
    .align = _Alignof(type), \
    .is_signed = _TRAIT_IS_SIGNED(type), \
    .is_floating = _TRAIT_IS_FLOATING(type), \
    .is_const = false, \
    .is_volatile = false \
})

// Type kind detection (using _Generic)
#define _TRAIT_KIND(type) _Generic((type), \
    bool: TRAIT_KIND_BOOL, \
    char: TRAIT_KIND_CHAR, \
    signed char: TRAIT_KIND_INT8, \
    unsigned char: TRAIT_KIND_UINT8, \
    short: TRAIT_KIND_INT16, \
    unsigned short: TRAIT_KIND_UINT16, \
    int: TRAIT_KIND_INT32, \
    unsigned int: TRAIT_KIND_UINT32, \
    long: TRAIT_KIND_INT64, \
    unsigned long: TRAIT_KIND_UINT64, \
    long long: TRAIT_KIND_INT64, \
    unsigned long long: TRAIT_KIND_UINT64, \
    float: TRAIT_KIND_FLOAT32, \
    double: TRAIT_KIND_FLOAT64, \
    default: _TRAIT_PTR_CHECK(type) \
)

#define _TRAIT_PTR_CHECK(type) _Generic((type), \
    char*: TRAIT_KIND_PTR, \
    const char*: TRAIT_KIND_PTR, \
    void*: TRAIT_KIND_PTR, \
    const void*: TRAIT_KIND_PTR, \
    int*: TRAIT_KIND_PTR, \
    default: TRAIT_KIND_UNKNOWN \
)

#define _TRAIT_IS_SIGNED(type) _Generic((type), \
    signed char: true, short: true, int: true, long: true, long long: true, \
    default: false \
)

#define _TRAIT_IS_FLOATING(type) _Generic((type), \
    float: true, double: true, long double: true, \
    default: false \
)

// Type properties
#define type_size(type) sizeof(type)
#define type_align(type) _Alignof(type)
#define type_name(type) #type
#define type_is_ptr(type) _Generic((type), default: false, \
    char*: true, void*: true, int*: true, const char*: true, const void*: true)
#define type_is_int(type) _Generic((type), \
    char: true, signed char: true, unsigned char: true, \
    short: true, unsigned short: true, \
    int: true, unsigned int: true, \
    long: true, unsigned long: true, \
    long long: true, unsigned long long: true, \
    default: false)
#define type_is_float(type) _Generic((type), \
    float: true, double: true, long double: true, \
    default: false)
#define type_is_arithmetic(type) (type_is_int(type) || type_is_float(type))

// Same type check
#define types_same(a, b) _Generic((a), _Generic((b), default: true, default: false))

// Runtime type info table
typedef struct {
    const char* type_name;
    TypeInfo info;
} TypeRegistryEntry;

void     traits_register(const char* name, const TypeInfo* info);
const TypeInfo* traits_lookup(const char* name);
void     traits_list_all(void (*callback)(const TypeRegistryEntry*));

// Conversion helpers
const char* trait_kind_name(TraitKind kind);
bool_t      trait_is_numeric(TraitKind kind);
bool_t      trait_is_integer(TraitKind kind);
bool_t      trait_is_floating(TraitKind kind);
bool_t      trait_is_pointer(TraitKind kind);

// Safe casting
#define safe_cast(type, value) ((type)(value))
#define type_check(type, value) (_Generic((value), type: true, default: false))
