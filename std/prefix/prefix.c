#include "prefix.h"
#include <stdlib.h>

// 前缀系统函数
PrefixSystem* create_prefix_system();
void destroy_prefix_system(PrefixSystem* system);
int enter_prefix(PrefixSystem* system, const char* name);
int exit_prefix(PrefixSystem* system);
void set_prefix_data(PrefixSystem* system, void* data);
void* get_prefix_data(PrefixSystem* system);
PrefixNode* find_prefix(PrefixSystem* system, const char* path);

// 全局前缀系统实例
static PrefixSystem* global_prefix_system = NULL;

// 初始化前缀系统
static void prefix_system_init() {
    if (!global_prefix_system) {
        global_prefix_system = create_prefix_system();
    }
}

// Prefix函数
PrefixSystem* prefix_system_create() {
    return create_prefix_system();
}

void prefix_system_destroy(PrefixSystem* system) {
    destroy_prefix_system(system);
}

int prefix_enter(const char* name) {
    prefix_system_init();
    return enter_prefix(global_prefix_system, name);
}

int prefix_leave() {
    prefix_system_init();
    return exit_prefix(global_prefix_system);
}

void prefix_set_data(void* data) {
    prefix_system_init();
    set_prefix_data(global_prefix_system, data);
}

void* prefix_get_data() {
    prefix_system_init();
    return get_prefix_data(global_prefix_system);
}

PrefixNode* prefix_find(const char* path) {
    prefix_system_init();
    return find_prefix(global_prefix_system, path);
}

PrefixSystem* prefix_system_get() {
    prefix_system_init();
    return global_prefix_system;
}
