#include "kaula.h"
#include <stdio.h>
#include <string.h>

typedef struct PrefixNode {
    char* name;
    void* data;
    struct PrefixNode* parent;
    struct PrefixNode* children;
    struct PrefixNode* next;
} PrefixNode;

typedef struct PrefixSystem {
    PrefixNode* root;
    PrefixNode* current;
} PrefixSystem;

PrefixSystem* create_prefix_system() {
    PrefixSystem* system = (PrefixSystem*)kmm_v4_malloc(sizeof(PrefixSystem));
    if (!system) return NULL;
    
    system->root = (PrefixNode*)kmm_v4_malloc(sizeof(PrefixNode));
    if (!system->root) {
        return NULL;
    }
    
    system->root->name = kmm_v4_strdup("");
    system->root->data = NULL;
    system->root->parent = NULL;
    system->root->children = NULL;
    system->root->next = NULL;
    
    system->current = system->root;
    return system;
}

void free_prefix_node(PrefixNode* node) {
    if (!node) return;
    
    PrefixNode* child = node->children;
    while (child) {
        PrefixNode* next = child->next;
        free_prefix_node(child);
        child = next;
    }
}

void destroy_prefix_system(PrefixSystem* system) {
    if (!system) return;
    free_prefix_node(system->root);
}

PrefixNode* find_child(PrefixNode* parent, const char* name) {
    if (!parent || !name) return NULL;
    
    PrefixNode* child = parent->children;
    while (child) {
        if (strcmp(child->name, name) == 0) {
            return child;
        }
        child = child->next;
    }
    
    return NULL;
}

PrefixNode* create_child(PrefixNode* parent, const char* name) {
    if (!parent || !name) return NULL;
    
    PrefixNode* existing = find_child(parent, name);
    if (existing) return existing;
    
    PrefixNode* node = (PrefixNode*)kmm_v4_malloc(sizeof(PrefixNode));
    if (!node) return NULL;
    
    node->name = kmm_v4_strdup(name);
    node->data = NULL;
    node->parent = parent;
    node->children = NULL;
    node->next = NULL;
    
    if (!parent->children) {
        parent->children = node;
    } else {
        PrefixNode* child = parent->children;
        while (child->next) {
            child = child->next;
        }
        child->next = node;
    }
    
    return node;
}

int enter_prefix(PrefixSystem* system, const char* name) {
    if (!system || !name) return 0;
    
    PrefixNode* child = find_child(system->current, name);
    if (!child) {
        child = create_child(system->current, name);
        if (!child) return 0;
    }
    
    system->current = child;
    return 1;
}

int exit_prefix(PrefixSystem* system) {
    if (!system || !system->current->parent) return 0;
    
    system->current = system->current->parent;
    return 1;
}

void set_prefix_data(PrefixSystem* system, void* data) {
    if (!system) return;
    system->current->data = data;
}

void* get_prefix_data(PrefixSystem* system) {
    if (!system) return NULL;
    return system->current->data;
}

PrefixNode* find_prefix(PrefixSystem* system, const char* path) {
    if (!system || !path) return NULL;
    
    PrefixNode* node = system->root;
    char* path_copy = kmm_v4_strdup(path);
    if (!path_copy) return NULL;
    
    char* token = strtok(path_copy, ".");
    while (token) {
        node = find_child(node, token);
        if (!node) {
            return NULL;
        }
        token = strtok(NULL, ".");
    }
    
    return node;
}

void print_prefix_system(PrefixNode* node, int depth) {
    if (!node) return;
    
    for (int i = 0; i < depth; i++) {
        printf("  ");
    }
    
    printf("%s\n", node->name);
    
    PrefixNode* child = node->children;
    while (child) {
        print_prefix_system(child, depth + 1);
        child = child->next;
    }
}

void test_prefix_system() {
    printf("=== 前缀系统测试 ===\n");
    
    PrefixSystem* system = create_prefix_system();
    if (!system) {
        printf("创建前缀系统失败\n");
        return;
    }
    
    printf("进入前缀: a\n");
    enter_prefix(system, "a");
    
    printf("进入前缀: b\n");
    enter_prefix(system, "b");
    
    int data = 42;
    set_prefix_data(system, &data);
    printf("设置当前前缀数据: %d\n", *((int*)get_prefix_data(system)));
    
    printf("退出前缀\n");
    exit_prefix(system);
    
    printf("进入前缀: c\n");
    enter_prefix(system, "c");
    
    printf("\n前缀系统结构:\n");
    print_prefix_system(system->root, 0);
    
    printf("\n查找前缀: a.b\n");
    PrefixNode* node = find_prefix(system, "a.b");
    if (node) {
        printf("找到前缀: %s, 数据: %d\n", node->name, *((int*)node->data));
    } else {
        printf("未找到前缀\n");
    }
    
    destroy_prefix_system(system);
    printf("\n前缀系统测试完成\n");
}