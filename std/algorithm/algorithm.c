#include "algorithm.h"
#include "../memory/memory.h"
#include "../string/string.h"
#include <string.h>

void algo_swap(void* a, void* b, size_t elem_size) {
    unsigned char* pa = (unsigned char*)a;
    unsigned char* pb = (unsigned char*)b;
    unsigned char tmp;
    size_t i;
    for (i = 0; i < elem_size; i++) {
        tmp = pa[i];
        pa[i] = pb[i];
        pb[i] = tmp;
    }
}

void algo_reverse(void* array, size_t count, size_t elem_size) {
    unsigned char* arr = (unsigned char*)array;
    size_t i, j;
    if (!array || count < 2) return;
    for (i = 0, j = count - 1; i < j; i++, j--) {
        algo_swap(arr + i * elem_size, arr + j * elem_size, elem_size);
    }
}

static size_t partition(void* array, size_t low, size_t high,
                        size_t elem_size, CompareFunc cmp) {
    unsigned char* arr = (unsigned char*)array;
    void* pivot = arr + high * elem_size;
    size_t i = low;
    size_t j;
    for (j = low; j < high; j++) {
        if (cmp(arr + j * elem_size, pivot) < 0) {
            if (i != j) {
                algo_swap(arr + i * elem_size, arr + j * elem_size, elem_size);
            }
            i++;
        }
    }
    if (i != high) {
        algo_swap(arr + i * elem_size, arr + high * elem_size, elem_size);
    }
    return i;
}

static size_t* grow_stack(size_t* old_stack, size_t old_size, size_t new_size) {
    size_t* new_stack = (size_t*)kmm_v4_malloc(new_size * sizeof(size_t) * 2);
    if (!new_stack) return NULL;
    memcpy(new_stack, old_stack, old_size * sizeof(size_t) * 2);
    kmm_v4_free(old_stack);
    return new_stack;
}

static void quick_sort(void* array, size_t low, size_t high,
                       size_t elem_size, CompareFunc cmp) {
    unsigned char* arr = (unsigned char*)array;
    size_t stack_size = 64;
    size_t* stack = (size_t*)kmm_v4_malloc(stack_size * sizeof(size_t) * 2);
    size_t top = 0;
    size_t p;
    if (!stack) return;

    stack[top++] = low;
    stack[top++] = high;

    while (top > 0) {
        high = stack[--top];
        low = stack[--top];

        if (low < high) {
            p = partition(arr, low, high, elem_size, cmp);
            if (p > 0 && p - 1 >= low) {
                if (top + 2 > stack_size * 2) {
                    size_t new_size = stack_size * 2;
                    size_t* new_stack = grow_stack(stack, stack_size, new_size);
                    if (!new_stack) {
                        kmm_v4_free(stack);
                        return;
                    }
                    stack = new_stack;
                    stack_size = new_size;
                }
                stack[top++] = low;
                stack[top++] = p - 1;
            }
            if (p + 1 <= high) {
                if (top + 2 > stack_size * 2) {
                    size_t new_size = stack_size * 2;
                    size_t* new_stack = grow_stack(stack, stack_size, new_size);
                    if (!new_stack) {
                        kmm_v4_free(stack);
                        return;
                    }
                    stack = new_stack;
                    stack_size = new_size;
                }
                stack[top++] = p + 1;
                stack[top++] = high;
            }
        }
    }
    kmm_v4_free(stack);
}

void algo_sort(void* array, size_t count, size_t elem_size, CompareFunc cmp) {
    if (!array || count < 2 || elem_size == 0 || !cmp) return;
    quick_sort(array, 0, count - 1, elem_size, cmp);
}

void algo_sort_desc(void* array, size_t count, size_t elem_size, CompareFunc cmp) {
    unsigned char* arr = (unsigned char*)array;
    size_t i, j;
    if (!array || count < 2 || elem_size == 0 || !cmp) return;
    quick_sort(array, 0, count - 1, elem_size, cmp);
    for (i = 0, j = count - 1; i < j; i++, j--) {
        algo_swap(arr + i * elem_size, arr + j * elem_size, elem_size);
    }
}

bool_t algo_is_sorted(const void* array, size_t count, size_t elem_size, CompareFunc cmp) {
    const unsigned char* arr = (const unsigned char*)array;
    size_t i;
    if (!array || count < 2 || !cmp) return true;
    for (i = 1; i < count; i++) {
        if (cmp(arr + (i - 1) * elem_size, arr + i * elem_size) > 0) {
            return false;
        }
    }
    return true;
}

size_t algo_binary_search(const void* key, const void* array, size_t count,
                          size_t elem_size, CompareFunc cmp) {
    const unsigned char* arr = (const unsigned char*)array;
    size_t low = 0;
    size_t high = count;
    size_t mid;
    int c;
    if (!array || !key || !cmp || count == 0) return count;
    while (low < high) {
        mid = low + (high - low) / 2;
        c = cmp(arr + mid * elem_size, key);
        if (c == 0) {
            return mid;
        } else if (c < 0) {
            low = mid + 1;
        } else {
            high = mid;
        }
    }
    return count;
}

size_t algo_lower_bound(const void* key, const void* array, size_t count,
                        size_t elem_size, CompareFunc cmp) {
    const unsigned char* arr = (const unsigned char*)array;
    size_t low = 0;
    size_t high = count;
    size_t mid;
    int c;
    if (!array || !key || !cmp || count == 0) return count;
    while (low < high) {
        mid = low + (high - low) / 2;
        c = cmp(arr + mid * elem_size, key);
        if (c < 0) {
            low = mid + 1;
        } else {
            high = mid;
        }
    }
    return low;
}

size_t algo_upper_bound(const void* key, const void* array, size_t count,
                        size_t elem_size, CompareFunc cmp) {
    const unsigned char* arr = (const unsigned char*)array;
    size_t low = 0;
    size_t high = count;
    size_t mid;
    int c;
    if (!array || !key || !cmp || count == 0) return count;
    while (low < high) {
        mid = low + (high - low) / 2;
        c = cmp(arr + mid * elem_size, key);
        if (c <= 0) {
            low = mid + 1;
        } else {
            high = mid;
        }
    }
    return low;
}

size_t algo_find(const void* item, const void* array, size_t count,
                 size_t elem_size, CompareFunc cmp) {
    const unsigned char* arr = (const unsigned char*)array;
    size_t i;
    if (!array || !item || !cmp) return count;
    for (i = 0; i < count; i++) {
        if (cmp(arr + i * elem_size, item) == 0) {
            return i;
        }
    }
    return count;
}

size_t algo_find_if(const void* array, size_t count, size_t elem_size,
                    PredicateFunc pred, void* ctx) {
    const unsigned char* arr = (const unsigned char*)array;
    size_t i;
    if (!array || !pred) return count;
    for (i = 0; i < count; i++) {
        if (pred(arr + i * elem_size, ctx)) {
            return i;
        }
    }
    return count;
}

size_t algo_count(const void* item, const void* array, size_t count,
                  size_t elem_size, CompareFunc cmp) {
    const unsigned char* arr = (const unsigned char*)array;
    size_t i, cnt = 0;
    if (!array || !item || !cmp) return 0;
    for (i = 0; i < count; i++) {
        if (cmp(arr + i * elem_size, item) == 0) {
            cnt++;
        }
    }
    return cnt;
}

size_t algo_count_if(const void* array, size_t count, size_t elem_size,
                     PredicateFunc pred, void* ctx) {
    const unsigned char* arr = (const unsigned char*)array;
    size_t i, cnt = 0;
    if (!array || !pred) return 0;
    for (i = 0; i < count; i++) {
        if (pred(arr + i * elem_size, ctx)) {
            cnt++;
        }
    }
    return cnt;
}

void algo_min_max(const void* array, size_t count, size_t elem_size,
                  CompareFunc cmp, size_t* min_idx, size_t* max_idx) {
    const unsigned char* arr = (const unsigned char*)array;
    size_t i;
    size_t mn = 0, mx = 0;
    if (!array || count == 0 || !cmp) {
        if (min_idx) *min_idx = 0;
        if (max_idx) *max_idx = 0;
        return;
    }
    for (i = 1; i < count; i++) {
        if (cmp(arr + i * elem_size, arr + mn * elem_size) < 0) {
            mn = i;
        }
        if (cmp(arr + i * elem_size, arr + mx * elem_size) > 0) {
            mx = i;
        }
    }
    if (min_idx) *min_idx = mn;
    if (max_idx) *max_idx = mx;
}

size_t algo_min_element(const void* array, size_t count, size_t elem_size,
                        CompareFunc cmp) {
    const unsigned char* arr = (const unsigned char*)array;
    size_t i, mn = 0;
    if (!array || count == 0 || !cmp) return 0;
    for (i = 1; i < count; i++) {
        if (cmp(arr + i * elem_size, arr + mn * elem_size) < 0) {
            mn = i;
        }
    }
    return mn;
}

size_t algo_max_element(const void* array, size_t count, size_t elem_size,
                        CompareFunc cmp) {
    const unsigned char* arr = (const unsigned char*)array;
    size_t i, mx = 0;
    if (!array || count == 0 || !cmp) return 0;
    for (i = 1; i < count; i++) {
        if (cmp(arr + i * elem_size, arr + mx * elem_size) > 0) {
            mx = i;
        }
    }
    return mx;
}

void algo_fill(void* array, size_t count, size_t elem_size, const void* value) {
    unsigned char* arr = (unsigned char*)array;
    size_t i;
    if (!array || !value || elem_size == 0) return;
    for (i = 0; i < count; i++) {
        memcpy(arr + i * elem_size, value, elem_size);
    }
}

void algo_copy(const void* src, void* dst, size_t count, size_t elem_size) {
    if (!src || !dst || elem_size == 0) return;
    memcpy(dst, src, count * elem_size);
}

void algo_for_each(const void* array, size_t count, size_t elem_size,
                   ForEachFunc func, void* ctx) {
    const unsigned char* arr = (const unsigned char*)array;
    size_t i;
    if (!array || !func) return;
    for (i = 0; i < count; i++) {
        func(arr + i * elem_size, i, ctx);
    }
}

bool_t algo_all_of(const void* array, size_t count, size_t elem_size,
                   PredicateFunc pred, void* ctx) {
    const unsigned char* arr = (const unsigned char*)array;
    size_t i;
    if (!array || !pred) return true;
    for (i = 0; i < count; i++) {
        if (!pred(arr + i * elem_size, ctx)) {
            return false;
        }
    }
    return true;
}

bool_t algo_any_of(const void* array, size_t count, size_t elem_size,
                   PredicateFunc pred, void* ctx) {
    const unsigned char* arr = (const unsigned char*)array;
    size_t i;
    if (!array || !pred) return false;
    for (i = 0; i < count; i++) {
        if (pred(arr + i * elem_size, ctx)) {
            return true;
        }
    }
    return false;
}

bool_t algo_none_of(const void* array, size_t count, size_t elem_size,
                    PredicateFunc pred, void* ctx) {
    const unsigned char* arr = (const unsigned char*)array;
    size_t i;
    if (!array || !pred) return true;
    for (i = 0; i < count; i++) {
        if (pred(arr + i * elem_size, ctx)) {
            return false;
        }
    }
    return true;
}

void algo_unique(void* array, size_t* count, size_t elem_size, CompareFunc cmp) {
    unsigned char* arr = (unsigned char*)array;
    size_t i, write_idx = 0;
    if (!array || !count || *count < 2 || elem_size == 0 || !cmp) return;
    for (i = 1; i < *count; i++) {
        if (cmp(arr + i * elem_size, arr + write_idx * elem_size) != 0) {
            write_idx++;
            if (write_idx != i) {
                memcpy(arr + write_idx * elem_size, arr + i * elem_size, elem_size);
            }
        }
    }
    *count = write_idx + 1;
}

int algo_compare_int(const void* a, const void* b) {
    int ia = *(const int*)a;
    int ib = *(const int*)b;
    if (ia < ib) return -1;
    if (ia > ib) return 1;
    return 0;
}

int algo_compare_int_desc(const void* a, const void* b) {
    int ia = *(const int*)a;
    int ib = *(const int*)b;
    if (ia > ib) return -1;
    if (ia < ib) return 1;
    return 0;
}

int algo_compare_float(const void* a, const void* b) {
    float fa = *(const float*)a;
    float fb = *(const float*)b;
    if (fa < fb) return -1;
    if (fa > fb) return 1;
    return 0;
}

int algo_compare_string(const void* a, const void* b) {
    const String sa = *(const String*)a;
    const String sb = *(const String*)b;
    if (!sa.ptr && !sb.ptr) return 0;
    if (!sa.ptr) return -1;
    if (!sb.ptr) return 1;
    return strcmp(sa.ptr, sb.ptr);
}
