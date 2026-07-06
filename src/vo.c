#include "kaula.h"
#include <stdio.h>

static inline void vo_lru_remove(VOModule* vo, int index) {
    VOData* data = &vo->data_cache[index];
    if (data->lru_prev >= 0) {
        vo->data_cache[data->lru_prev].lru_next = data->lru_next;
    } else {
        vo->lru_head = data->lru_next;
    }
    if (data->lru_next >= 0) {
        vo->data_cache[data->lru_next].lru_prev = data->lru_prev;
    } else {
        vo->lru_tail = data->lru_prev;
    }
    data->lru_prev = -1;
    data->lru_next = -1;
}

static inline void vo_lru_push_front(VOModule* vo, int index) {
    VOData* data = &vo->data_cache[index];
    data->lru_prev = -1;
    data->lru_next = vo->lru_head;
    if (vo->lru_head >= 0) {
        vo->data_cache[vo->lru_head].lru_prev = index;
    } else {
        vo->lru_tail = index;
    }
    vo->lru_head = index;
}

static inline int vo_lru_pop_back(VOModule* vo) {
    int victim = vo->lru_tail;
    if (victim >= 0) {
        vo_lru_remove(vo, victim);
    }
    return victim;
}

VOModule* vo_create(int cache_max) {
    VOModule* vo = (VOModule*)fast_alloc(sizeof(VOModule));
    vo->cache_max = cache_max;
    vo->data_cache = (VOData*)fast_calloc(cache_max + 1, sizeof(VOData));
    vo->code_cache = (void* (**)(void*))fast_calloc(cache_max + 1, sizeof(void*));
    vo->lru_head = -1;
    vo->lru_tail = -1;
    vo->access_counter = 0;
    for (int i = 0; i <= cache_max; i++) {
        vo->data_cache[i].lru_prev = -1;
        vo->data_cache[i].lru_next = -1;
        vo->data_cache[i].has_code = 0;
        vo->data_cache[i].code_index = -1;
    }
    return vo;
}

void vo_destroy(VOModule* vo) {
    if (vo->data_cache) {
        fast_free(vo->data_cache);
    }
    fast_free(vo);
}

void vo_data_load(VOModule* vo, int index, void* value) {
    if (index >= 0 && index <= vo->cache_max) {
        VOData* data = &vo->data_cache[index];
        if (data->has_code || data->value != NULL) {
            vo_lru_remove(vo, index);
        }
        data->value = value;
        data->has_code = 0;
        data->code_index = -1;
        vo_lru_push_front(vo, index);
    } else {
        int evict_index = vo_lru_pop_back(vo);
        if (evict_index >= 0) {
            VOData* data = &vo->data_cache[evict_index];
            data->value = value;
            data->has_code = 0;
            data->code_index = -1;
            vo_lru_push_front(vo, evict_index);
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
    
    VOData* data = &vo->data_cache[data_index];
    data->code = vo->code_cache[-code_index];
    data->has_code = 1;
    data->code_index = code_index;
}

void* vo_access(VOModule* vo, int index) {
    if (!vo) {
        return NULL;
    }
    
    if (index > 0 && index <= vo->cache_max) {
        VOData* data = &vo->data_cache[index];
        vo_lru_remove(vo, index);
        vo_lru_push_front(vo, index);
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