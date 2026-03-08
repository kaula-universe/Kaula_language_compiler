#ifndef STD_VO_VO_H
#define STD_VO_VO_H

#include "../base/types.h"
#include "../../src/kaula.h"

// VO类型 - 使用src目录中的高性能实现
typedef VOModule VO;
typedef VOData VOData;

// VO函数
extern VO* std_vo_create(int cache_max);
extern void std_vo_destroy(VO* vo);
extern void std_vo_data_load(VO* vo, int index, void* value);
extern void std_vo_code_load(VO* vo, int index, void* (*func)(void*));
extern void std_vo_associate(VO* vo, int data_index, int code_index);
extern void* std_vo_access(VO* vo, int index);
extern int std_vo_get_cache_max(VO* vo);

#endif // STD_VO_VO_H