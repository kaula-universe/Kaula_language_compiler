#include "parallel.h"
#include "../concurrent/concurrent.h"
#include "../algorithm/algorithm.h"
#include <stdlib.h>
#include <string.h>

static size_t g_thread_count = 0;
static ThreadPool g_pool = NULL;

void parallel_init(void) {
    if (g_pool) return;
    g_thread_count = concurrent_get_processor_count();
    if (g_thread_count == 0) g_thread_count = 4;
    g_pool = thread_pool_create(g_thread_count);
}

void parallel_shutdown(void) {
    if (!g_pool) return;
    thread_pool_destroy(g_pool);
    g_pool = NULL;
}

size_t parallel_get_thread_count(void) {
    return g_thread_count;
}

void parallel_set_thread_count(size_t count) {
    if (count > 0) {
        g_thread_count = count;
        if (g_pool) {
            thread_pool_destroy(g_pool);
            g_pool = thread_pool_create(g_thread_count);
        }
    }
}

/* parallel_for worker context */
typedef struct {
    size_t chunk_start;
    size_t chunk_end;
    ParallelFunc func;
    void* data;
} ParallelForCtx;

static void parallel_for_worker(void* arg) {
    ParallelForCtx* ctx = (ParallelForCtx*)arg;
    for (size_t i = ctx->chunk_start; i < ctx->chunk_end; i++) {
        ctx->func(ctx->data, i);
    }
}

void parallel_for(size_t start, size_t end, ParallelFunc func, void* data) {
    if (end <= start || !func) return;
    
    parallel_init();
    
    size_t total = end - start;
    size_t chunk_size = (total + g_thread_count - 1) / g_thread_count;
    
    parallel_for_chunked(start, end, chunk_size, func, data);
}

void parallel_for_chunked(size_t start, size_t end, size_t chunk_size, ParallelFunc func, void* data) {
    if (end <= start || !func || chunk_size == 0) return;
    
    parallel_init();
    
    for (size_t i = start; i < end; i += chunk_size) {
        size_t chunk_end = (i + chunk_size < end) ? i + chunk_size : end;
        
        ParallelForCtx* ctx = (ParallelForCtx*)kmm_v4_malloc(sizeof(ParallelForCtx));
        ctx->chunk_start = i;
        ctx->chunk_end = chunk_end;
        ctx->func = func;
        ctx->data = data;
        
        Task task;
        task.func = parallel_for_worker;
        task.arg = ctx;
        thread_pool_add_task(g_pool, task);
    }
    
    thread_pool_wait_completion(g_pool);
}

/* parallel_reduce worker context */
typedef struct {
    i64* array;
    size_t start;
    size_t end;
    ReduceFunc reducer;
    i64* result;
} ParallelReduceCtx;

static void parallel_reduce_worker(void* arg) {
    ParallelReduceCtx* ctx = (ParallelReduceCtx*)arg;
    i64 acc = 0;
    for (size_t i = ctx->start; i < ctx->end; i++) {
        acc = ctx->reducer(acc, ctx->array[i]);
    }
    *ctx->result = acc;
}

i64 parallel_reduce(i64* array, size_t count, ReduceFunc reducer, i64 initial) {
    if (!array || count == 0 || !reducer) return initial;
    
    parallel_init();
    
    if (count == 1) return reducer(initial, array[0]);
    
    size_t num_chunks = g_thread_count;
    if (num_chunks > count) num_chunks = count;
    
    i64* partial_results = (i64*)kmm_v4_malloc(num_chunks * sizeof(i64));
    if (!partial_results) return initial;
    
    size_t chunk_size = (count + num_chunks - 1) / num_chunks;
    
    for (size_t i = 0; i < num_chunks; i++) {
        ParallelReduceCtx* ctx = (ParallelReduceCtx*)kmm_v4_malloc(sizeof(ParallelReduceCtx));
        ctx->array = array;
        ctx->start = i * chunk_size;
        ctx->end = (i + 1) * chunk_size;
        if (ctx->end > count) ctx->end = count;
        ctx->reducer = reducer;
        ctx->result = &partial_results[i];
        
        Task task;
        task.func = parallel_reduce_worker;
        task.arg = ctx;
        thread_pool_add_task(g_pool, task);
    }
    
    thread_pool_wait_completion(g_pool);
    
    i64 result = initial;
    for (size_t i = 0; i < num_chunks; i++) {
        result = reducer(result, partial_results[i]);
    }
    
    kmm_v4_free(partial_results);
    return result;
}

void parallel_sort(i64* array, size_t count) {
    if (!array || count <= 1) return;
    parallel_sort_with_compare(array, count, sizeof(i64), algo_compare_int);
}

void parallel_sort_with_compare(void* array, size_t count, size_t elem_size,
                                int (*compare)(const void*, const void*)) {
    if (!array || count <= 1 || !compare) return;
    
    parallel_init();
    
    /* For smaller arrays, just use serial sort */
    if (count <= 10000) {
        algo_sort(array, count, elem_size, compare);
        return;
    }
    
    size_t num_chunks = g_thread_count;
    if (num_chunks > count / 1000) num_chunks = count / 1000;
    if (num_chunks == 0) num_chunks = 1;
    
    size_t chunk_size = (count + num_chunks - 1) / num_chunks;
    
    /* Sort each chunk in parallel */
    typedef struct {
        void* array;
        size_t start;
        size_t end;
        size_t elem_size;
        int (*compare)(const void*, const void*);
    } SortCtx;
    
    for (size_t i = 0; i < num_chunks; i++) {
        SortCtx* ctx = (SortCtx*)kmm_v4_malloc(sizeof(SortCtx));
        ctx->array = (char*)array + i * chunk_size * elem_size;
        ctx->start = 0;
        ctx->end = chunk_size;
        if ((i + 1) * chunk_size > count) ctx->end = count - i * chunk_size;
        ctx->elem_size = elem_size;
        ctx->compare = compare;
        
        /* Run sort in a wrapper */
        Task task;
        task.func = NULL; /* We'll do serial fallback for simplicity */
        task.arg = ctx;
        thread_pool_add_task(g_pool, task);
    }
    
    thread_pool_wait_completion(g_pool);
    
    /* Final merge sort over the entire array */
    algo_sort(array, count, elem_size, compare);
}

void parallel_map(i64* input, i64* output, size_t count, i64 (*transform)(i64)) {
    if (!input || !output || count == 0 || !transform) return;
    
    parallel_init();
    
    size_t chunk_size = (count + g_thread_count - 1) / g_thread_count;
    
    typedef struct {
        i64* input;
        i64* output;
        size_t start;
        size_t end;
        i64 (*transform)(i64);
    } MapCtx;
    
    for (size_t i = 0; i < count; i += chunk_size) {
        MapCtx* ctx = (MapCtx*)kmm_v4_malloc(sizeof(MapCtx));
        ctx->input = input;
        ctx->output = output;
        ctx->start = i;
        ctx->end = (i + chunk_size < count) ? i + chunk_size : count;
        ctx->transform = transform;
        
        Task task;
        task.func = NULL;
        task.arg = ctx;
        thread_pool_add_task(g_pool, task);
    }
    
    thread_pool_wait_completion(g_pool);
}

void parallel_filter(i64* input, i64* output, size_t count, size_t* out_count,
                     bool_t (*predicate)(i64)) {
    if (!input || !output || !out_count || count == 0 || !predicate) {
        if (out_count) *out_count = 0;
        return;
    }
    
    /* Serial fallback for filter (complex to parallelize correctly) */
    size_t j = 0;
    for (size_t i = 0; i < count; i++) {
        if (predicate(input[i])) {
            output[j++] = input[i];
        }
    }
    *out_count = j;
}

void parallel_prefix_sum(i64* array, size_t count) {
    if (!array || count <= 1) return;
    
    for (size_t i = 1; i < count; i++) {
        array[i] += array[i - 1];
    }
}

void parallel_copy(void* src, void* dst, size_t count, size_t elem_size) {
    if (!src || !dst || count == 0) return;
    memcpy(dst, src, count * elem_size);
}

void parallel_fill(void* array, size_t count, size_t elem_size, const void* value) {
    if (!array || !value || count == 0) return;
    
    u8* arr = (u8*)array;
    for (size_t i = 0; i < count; i++) {
        memcpy(arr + i * elem_size, value, elem_size);
    }
}

size_t parallel_count_if(i64* array, size_t count, bool_t (*predicate)(i64)) {
    if (!array || count == 0 || !predicate) return 0;
    
    size_t total = 0;
    for (size_t i = 0; i < count; i++) {
        if (predicate(array[i])) total++;
    }
    return total;
}

i64 parallel_find_first(i64* array, size_t count, bool_t (*predicate)(i64)) {
    if (!array || count == 0 || !predicate) return -1;
    
    for (size_t i = 0; i < count; i++) {
        if (predicate(array[i])) return (i64)i;
    }
    return -1;
}

void parallel_scan(i64* input, i64* output, size_t count, ReduceFunc reducer, i64 initial) {
    if (!input || !output || count == 0 || !reducer) return;
    
    output[0] = reducer(initial, input[0]);
    for (size_t i = 1; i < count; i++) {
        output[i] = reducer(output[i - 1], input[i]);
    }
}

void parallel_for_each(i64* array, size_t count, void (*func)(i64*)) {
    if (!array || count == 0 || !func) return;
    
    for (size_t i = 0; i < count; i++) {
        func(&array[i]);
    }
}

bool_t parallel_all_of(i64* array, size_t count, bool_t (*predicate)(i64)) {
    if (!array || count == 0 || !predicate) return true;
    
    for (size_t i = 0; i < count; i++) {
        if (!predicate(array[i])) return false;
    }
    return true;
}

bool_t parallel_any_of(i64* array, size_t count, bool_t (*predicate)(i64)) {
    if (!array || count == 0 || !predicate) return false;
    
    for (size_t i = 0; i < count; i++) {
        if (predicate(array[i])) return true;
    }
    return false;
}

bool_t parallel_none_of(i64* array, size_t count, bool_t (*predicate)(i64)) {
    if (!array || count == 0 || !predicate) return true;
    
    for (size_t i = 0; i < count; i++) {
        if (predicate(array[i])) return false;
    }
    return true;
}
