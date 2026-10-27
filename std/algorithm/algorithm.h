#pragma once
#include "../base/types.h"
#include "../string/string.h"

typedef int (*CompareFunc)(const void* a, const void* b);
typedef bool_t (*PredicateFunc)(const void* item, void* ctx);
typedef void (*MapFunc)(void* item, void* ctx);
typedef void (*ForEachFunc)(const void* item, size_t index, void* ctx);

void algo_sort(void* array, size_t count, size_t elem_size, CompareFunc cmp);
void algo_sort_desc(void* array, size_t count, size_t elem_size, CompareFunc cmp);
bool_t algo_is_sorted(const void* array, size_t count, size_t elem_size, CompareFunc cmp);

size_t algo_binary_search(const void* key, const void* array, size_t count,
                          size_t elem_size, CompareFunc cmp);
size_t algo_lower_bound(const void* key, const void* array, size_t count,
                        size_t elem_size, CompareFunc cmp);
size_t algo_upper_bound(const void* key, const void* array, size_t count,
                        size_t elem_size, CompareFunc cmp);

size_t algo_find(const void* item, const void* array, size_t count,
                 size_t elem_size, CompareFunc cmp);
size_t algo_find_if(const void* array, size_t count, size_t elem_size,
                    PredicateFunc pred, void* ctx);
size_t algo_count(const void* item, const void* array, size_t count,
                  size_t elem_size, CompareFunc cmp);
size_t algo_count_if(const void* array, size_t count, size_t elem_size,
                     PredicateFunc pred, void* ctx);

void algo_reverse(void* array, size_t count, size_t elem_size);
void algo_swap(void* a, void* b, size_t elem_size);

void algo_min_max(const void* array, size_t count, size_t elem_size,
                  CompareFunc cmp, size_t* min_idx, size_t* max_idx);
size_t algo_min_element(const void* array, size_t count, size_t elem_size,
                        CompareFunc cmp);
size_t algo_max_element(const void* array, size_t count, size_t elem_size,
                        CompareFunc cmp);

void algo_fill(void* array, size_t count, size_t elem_size, const void* value);
void algo_copy(const void* src, void* dst, size_t count, size_t elem_size);

void algo_for_each(const void* array, size_t count, size_t elem_size,
                   ForEachFunc func, void* ctx);

bool_t algo_all_of(const void* array, size_t count, size_t elem_size,
                   PredicateFunc pred, void* ctx);
bool_t algo_any_of(const void* array, size_t count, size_t elem_size,
                   PredicateFunc pred, void* ctx);
bool_t algo_none_of(const void* array, size_t count, size_t elem_size,
                    PredicateFunc pred, void* ctx);

void algo_unique(void* array, size_t* count, size_t elem_size, CompareFunc cmp);

int algo_compare_int(const void* a, const void* b);
int algo_compare_int_desc(const void* a, const void* b);
int algo_compare_float(const void* a, const void* b);
int algo_compare_string(const void* a, const void* b);
