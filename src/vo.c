#include "kaula.h"
#include <stdio.h>

VOModule* vo_create(int cache_max) {
    VOModule* vo = (VOModule*)fast_alloc(sizeof(VOModule));
    vo->cache_max = cache_max;
    vo->data_cache = (VOData*)fast_calloc(cache_max + 1, sizeof(VOData));
    vo->code_cache = (void* (*)(void*))fast_calloc(cache_max + 1, sizeof(void*));
    // 初始化访问时间
    for (int i = 0; i <= cache_max; i++) {
        vo->data_cache[i].last_access = 0;
        vo->data_cache[i].code_index = -1;
    }
    return vo;
}

static inline uint64_t get_current_time_ns() {
    LARGE_INTEGER freq, counter;
    QueryPerformanceFrequency(&freq);
    QueryPerformanceCounter(&counter);
    return (uint64_t)((double)counter.QuadPart * 1000000000.0 / (double)freq.QuadPart);
}

static int find_lru_victim(VOModule* vo) {
    uint64_t min_access = (uint64_t)-1;
    int victim_index = -1;
    for (int i = 0; i <= vo->cache_max; i++) {
        if (vo->data_cache[i].last_access < min_access) {
            min_access = vo->data_cache[i].last_access;
            victim_index = i;
        }
    }
    return victim_index;
}

void vo_data_load(VOModule* vo, int index, void* value) {
    if (index >= 0 && index <= vo->cache_max) {
        vo->data_cache[index].value = value;
        vo->data_cache[index].has_code = 0;
        vo->data_cache[index].last_access = get_current_time_ns();
        vo->data_cache[index].code_index = -1;
    } else {
        // 缓存已满，需要LRU淘汰
        int evict_index = find_lru_victim(vo);
        if (evict_index >= 0) {
            vo->data_cache[evict_index].value = value;
            vo->data_cache[evict_index].has_code = 0;
            vo->data_cache[evict_index].last_access = get_current_time_ns();
            vo->data_cache[evict_index].code_index = -1;
        }
    }
}

void vo_code_load(VOModule* vo, int index, void* (*func)(void*)) {
    if (!vo || !func) {
        return;
    }
    
    if (index < -VO_CACHE_SIZE || index >= 0) {
        fprintf(stderr, "Error: Invalid code cache index %d\n", index);
        return;
    }
    
    vo->code_cache[-index] = func;
}

void vo_associate(VOModule* vo, int data_index, int code_index) {
    if (!vo) {
        return;
    }
    
    if (data_index < 0 || data_index > vo->cache_max) {
        fprintf(stderr, "Error: Invalid data index %d\n", data_index);
        return;
    }
    
    if (code_index < -VO_CACHE_SIZE || code_index >= 0) {
        fprintf(stderr, "Error: Invalid code index %d\n", code_index);
        return;
    }
    
    vo->data_cache[data_index].code = vo->code_cache[-code_index];
    vo->data_cache[data_index].has_code = 1;
    vo->data_cache[data_index].code_index = code_index;
}

void* vo_access(VOModule* vo, int index) {
    if (!vo) {
        return NULL;
    }
    
    if (index > 0 && index <= vo->cache_max) {
        VOData* data = &vo->data_cache[index];
        data->last_access = get_current_time_ns();
        if (data->has_code) {
            return data->code(data->value);
        }
        return data->value;
    } else if (index < 0 && index >= -VO_CACHE_SIZE) {
        return (void*)vo->code_cache[-index];
    } else {
        fprintf(stderr, "Error: Invalid VO access index %d\n", index);
        return NULL;
    }
}