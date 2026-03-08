#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#ifdef STANDALONE_TEST
int main() {
    test_prefix_system();
    return 0;
}
#endif

// 前缀系统：上下文管理

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

// 创建前缀系统
PrefixSystem* create_prefix_system() {
    PrefixSystem* system = (PrefixSystem*)malloc(sizeof(PrefixSystem));
    if (!system) return NULL;
    
    // 创建根节点
    system->root = (PrefixNode*)malloc(sizeof(PrefixNode));
    if (!system->root) {
        free(system);
        return NULL;
    }
    
    system->root->name = strdup("");
    system->root->data = NULL;
    system->root->parent = NULL;
    system->root->children = NULL;
    system->root->next = NULL;
    
    system->current = system->root;
    return system;
}

// 释放前缀节点
void free_prefix_node(PrefixNode* node) {
    if (!node) return;
    
    // 释放子节点
    PrefixNode* child = node->children;
    while (child) {
        PrefixNode* next = child->next;
        free_prefix_node(child);
        child = next;
    }
    
    free(node->name);
    free(node);
}

// 销毁前缀系统
void destroy_prefix_system(PrefixSystem* system) {
    if (!system) return;
    
    free_prefix_node(system->root);
    free(system);
}

// 查找子节点
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

// 创建子节点
PrefixNode* create_child(PrefixNode* parent, const char* name) {
    if (!parent || !name) return NULL;
    
    // 检查是否已存在
    PrefixNode* existing = find_child(parent, name);
    if (existing) return existing;
    
    // 创建新节点
    PrefixNode* node = (PrefixNode*)malloc(sizeof(PrefixNode));
    if (!node) return NULL;
    
    node->name = strdup(name);
    node->data = NULL;
    node->parent = parent;
    node->children = NULL;
    node->next = NULL;
    
    // 添加到子节点列表
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

// 进入前缀
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

// 退出前缀
int exit_prefix(PrefixSystem* system) {
    if (!system || !system->current->parent) return 0;
    
    system->current = system->current->parent;
    return 1;
}

// 设置当前前缀数据
void set_prefix_data(PrefixSystem* system, void* data) {
    if (!system) return;
    system->current->data = data;
}

// 获取当前前缀数据
void* get_prefix_data(PrefixSystem* system) {
    if (!system) return NULL;
    return system->current->data;
}

// 查找指定路径的前缀
PrefixNode* find_prefix(PrefixSystem* system, const char* path) {
    if (!system || !path) return NULL;
    
    PrefixNode* node = system->root;
    char* path_copy = strdup(path);
    if (!path_copy) return NULL;
    
    char* token = strtok(path_copy, ".");
    while (token) {
        node = find_child(node, token);
        if (!node) {
            free(path_copy);
            return NULL;
        }
        token = strtok(NULL, ".");
    }
    
    free(path_copy);
    return node;
}

// 打印前缀系统
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

// 测试前缀系统
void test_prefix_system() {
    printf("=== 前缀系统测试 ===\n");
    
    PrefixSystem* system = create_prefix_system();
    if (!system) {
        printf("创建前缀系统失败\n");
        return;
    }
    
    // 测试进入前缀
    printf("进入前缀: a\n");
    enter_prefix(system, "a");
    
    printf("进入前缀: b\n");
    enter_prefix(system, "b");
    
    // 设置数据
    int data = 42;
    set_prefix_data(system, &data);
    printf("设置当前前缀数据: %d\n", *((int*)get_prefix_data(system)));
    
    // 退出前缀
    printf("退出前缀\n");
    exit_prefix(system);
    
    printf("进入前缀: c\n");
    enter_prefix(system, "c");
    
    // 打印前缀系统
    printf("\n前缀系统结构:\n");
    print_prefix_system(system->root, 0);
    
    // 测试查找前缀
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
