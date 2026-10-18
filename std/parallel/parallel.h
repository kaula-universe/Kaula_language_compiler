#pragma once
#include "../base/types.h"
#include "../memory/memory.h"

typedef void (*ParallelFunc)(void* data, size_t index);
typedef i64 (*ReduceFunc)(i64 left, i64 right);

void parallel_for(size_t start, size_t end, ParallelFunc func, void* data);
void parallel_for_chunked(size_t start, size_t end, size_t chunk_size, ParallelFunc func, void* data);

i64 parallel_reduce(i64* array, size_t count, ReduceFunc reducer, i64 initial);

void parallel_sort(i64* array, size_t count);
void parallel_sort_with_compare(void* array, size_t count, size_t elem_size,
                                int (*compare)(const void*, const void*));

void parallel_map(i64* input, i64* output, size_t count, i64 (*transform)(i64));
void parallel_filter(i64* input, i64* output, size_t count, size_t* out_count,
                     bool_t (*predicate)(i64));

void parallel_prefix_sum(i64* array, size_t count);

void parallel_copy(void* src, void* dst, size_t count, size_t elem_size);
void parallel_fill(void* array, size_t count, size_t elem_size, const void* value);

size_t parallel_count_if(i64* array, size_t count, bool_t (*predicate)(i64));
i64 parallel_find_first(i64* array, size_t count, bool_t (*predicate)(i64));

void parallel_scan(i64* input, i64* output, size_t count, ReduceFunc reducer, i64 initial);

void parallel_for_each(i64* array, size_t count, void (*func)(i64*));

bool_t parallel_all_of(i64* array, size_t count, bool_t (*predicate)(i64));
bool_t parallel_any_of(i64* array, size_t count, bool_t (*predicate)(i64));
bool_t parallel_none_of(i64* array, size_t count, bool_t (*predicate)(i64));

void parallel_init(void);
void parallel_shutdown(void);
size_t parallel_get_thread_count(void);
void parallel_set_thread_count(size_t count);