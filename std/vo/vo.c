#include "vo.h"
#include <stdlib.h>

// VO函数 - 使用src目录中的高性能实现
VO* std_vo_create(int cache_max) {
    // 直接调用src目录中的vo_create函数
    return (VO*)vo_create(cache_max);
}

void std_vo_destroy(VO* vo) {
    // VO使用fast_alloc分配，不需要单独释放
    (void)vo;
}

void std_vo_data_load(VO* vo, int index, void* value) {
    // 直接调用src目录中的vo_data_load函数
    vo_data_load((VOModule*)vo, index, value);
}

void std_vo_code_load(VO* vo, int index, void* (*func)(void*)) {
    // 直接调用src目录中的vo_code_load函数
    vo_code_load((VOModule*)vo, index, func);
}

void std_vo_associate(VO* vo, int data_index, int code_index) {
    // 直接调用src目录中的vo_associate函数
    vo_associate((VOModule*)vo, data_index, code_index);
}

void* std_vo_access(VO* vo, int index) {
    // 直接调用src目录中的vo_access函数
    return vo_access((VOModule*)vo, index);
}

int std_vo_get_cache_max(VO* vo) {
    if (vo) {
        VOModule* vo_impl = (VOModule*)vo;
        return vo_impl->cache_max;
    }
    return 0;
}
