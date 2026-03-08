#ifndef STD_PREFIX_PREFIX_H
#define STD_PREFIX_PREFIX_H

#include "../base/types.h"

// 前缀节点结构
typedef struct PrefixNode {
    char* name;
    void* data;
    struct PrefixNode* parent;
    struct PrefixNode* children;
    struct PrefixNode* next;
} PrefixNode;

// 前缀系统结构
typedef struct PrefixSystem {
    PrefixNode* root;
    PrefixNode* current;
} PrefixSystem;

// Prefix函数
extern PrefixSystem* prefix_system_create();
extern void prefix_system_destroy(PrefixSystem* system);
extern int prefix_enter(const char* name);
extern int prefix_leave();
extern void prefix_set_data(void* data);
extern void* prefix_get_data();
extern PrefixNode* prefix_find(const char* path);
extern PrefixSystem* prefix_system_get();

#endif // STD_PREFIX_PREFIX_H