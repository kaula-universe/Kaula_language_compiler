#include <stdio.h>
#include <stdlib.h>
#include <string.h>

// 树系统：层次化数据结构

// 树节点类型
typedef enum TreeNodeType {
    NODE_TYPE_VALUE,
    NODE_TYPE_OBJECT,
    NODE_TYPE_ARRAY
} TreeNodeType;

// 树节点结构
typedef struct TreeNode {
    char* key;
    TreeNodeType type;
    union {
        void* value;
        struct TreeNode* children;
    } data;
    struct TreeNode* next;
    struct TreeNode* parent;
} TreeNode;

// 树系统结构
typedef struct TreeSystem {
    TreeNode* root;
} TreeSystem;

// 创建树系统
TreeSystem* create_tree_system() {
    TreeSystem* system = (TreeSystem*)malloc(sizeof(TreeSystem));
    if (!system) return NULL;
    
    // 创建根节点
    system->root = (TreeNode*)malloc(sizeof(TreeNode));
    if (!system->root) {
        free(system);
        return NULL;
    }
    
    system->root->key = strdup("");
    system->root->type = NODE_TYPE_OBJECT;
    system->root->data.children = NULL;
    system->root->next = NULL;
    system->root->parent = NULL;
    
    return system;
}

// 释放树节点
void free_tree_node(TreeNode* node) {
    if (!node) return;
    
    // 释放子节点
    if (node->type == NODE_TYPE_OBJECT || node->type == NODE_TYPE_ARRAY) {
        TreeNode* child = node->data.children;
        while (child) {
            TreeNode* next = child->next;
            free_tree_node(child);
            child = next;
        }
    } else if (node->type == NODE_TYPE_VALUE) {
        free(node->data.value);
    }
    
    free(node->key);
    free(node);
}

// 销毁树系统
void destroy_tree_system(TreeSystem* system) {
    if (!system) return;
    
    free_tree_node(system->root);
    free(system);
}

// 查找子节点
TreeNode* find_tree_child(TreeNode* parent, const char* key) {
    if (!parent || !key || (parent->type != NODE_TYPE_OBJECT && parent->type != NODE_TYPE_ARRAY)) {
        return NULL;
    }
    
    TreeNode* child = parent->data.children;
    while (child) {
        if (strcmp(child->key, key) == 0) {
            return child;
        }
        child = child->next;
    }
    
    return NULL;
}

// 创建树节点
TreeNode* create_tree_node(const char* key, TreeNodeType type) {
    TreeNode* node = (TreeNode*)malloc(sizeof(TreeNode));
    if (!node) return NULL;
    
    node->key = strdup(key);
    node->type = type;
    
    if (type == NODE_TYPE_OBJECT || type == NODE_TYPE_ARRAY) {
        node->data.children = NULL;
    } else {
        node->data.value = NULL;
    }
    
    node->next = NULL;
    node->parent = NULL;
    
    return node;
}

// 添加子节点
int add_tree_child(TreeNode* parent, TreeNode* child) {
    if (!parent || !child || (parent->type != NODE_TYPE_OBJECT && parent->type != NODE_TYPE_ARRAY)) {
        return 0;
    }
    
    child->parent = parent;
    
    if (!parent->data.children) {
        parent->data.children = child;
    } else {
        TreeNode* last = parent->data.children;
        while (last->next) {
            last = last->next;
        }
        last->next = child;
    }
    
    return 1;
}

// 设置节点值
int set_tree_node_value(TreeNode* node, void* value) {
    if (!node || node->type != NODE_TYPE_VALUE) {
        return 0;
    }
    
    if (node->data.value) {
        free(node->data.value);
    }
    
    node->data.value = value;
    return 1;
}

// 获取节点值
void* get_tree_node_value(TreeNode* node) {
    if (!node || node->type != NODE_TYPE_VALUE) {
        return NULL;
    }
    
    return node->data.value;
}

// 递归查找节点
TreeNode* find_tree_node(TreeNode* root, const char* path) {
    if (!root || !path) return NULL;
    
    if (strcmp(path, "") == 0) {
        return root;
    }
    
    char* path_copy = strdup(path);
    if (!path_copy) return NULL;
    
    char* token = strtok(path_copy, ".");
    TreeNode* current = root;
    
    while (token) {
        current = find_tree_child(current, token);
        if (!current) {
            free(path_copy);
            return NULL;
        }
        token = strtok(NULL, ".");
    }
    
    free(path_copy);
    return current;
}

// 打印树系统
void print_tree_node(TreeNode* node, int depth) {
    if (!node) return;
    
    for (int i = 0; i < depth; i++) {
        printf("  ");
    }
    
    if (node->key && strlen(node->key) > 0) {
        printf("%s: ", node->key);
    }
    
    switch (node->type) {
        case NODE_TYPE_VALUE:
            if (node->data.value) {
                printf("%s\n", (char*)node->data.value);
            } else {
                printf("null\n");
            }
            break;
        case NODE_TYPE_OBJECT:
            printf("{\n");
            TreeNode* child = node->data.children;
            while (child) {
                print_tree_node(child, depth + 1);
                child = child->next;
            }
            for (int i = 0; i < depth; i++) {
                printf("  ");
            }
            printf("}\n");
            break;
        case NODE_TYPE_ARRAY:
            printf("[\n");
            child = node->data.children;
            while (child) {
                print_tree_node(child, depth + 1);
                child = child->next;
            }
            for (int i = 0; i < depth; i++) {
                printf("  ");
            }
            printf("]\n");
            break;
    }
}

// 测试树系统
void test_tree_system() {
    printf("=== 树系统测试 ===\n");
    
    TreeSystem* system = create_tree_system();
    if (!system) {
        printf("创建树系统失败\n");
        return;
    }
    
    // 创建对象节点
    TreeNode* user = create_tree_node("user", NODE_TYPE_OBJECT);
    add_tree_child(system->root, user);
    
    // 添加子节点
    TreeNode* name = create_tree_node("name", NODE_TYPE_VALUE);
    set_tree_node_value(name, strdup("Kaula"));
    add_tree_child(user, name);
    
    TreeNode* age = create_tree_node("age", NODE_TYPE_VALUE);
    set_tree_node_value(age, strdup("1"));
    add_tree_child(user, age);
    
    // 创建数组节点
    TreeNode* skills = create_tree_node("skills", NODE_TYPE_ARRAY);
    add_tree_child(user, skills);
    
    // 添加数组元素
    TreeNode* skill1 = create_tree_node("0", NODE_TYPE_VALUE);
    set_tree_node_value(skill1, strdup("Programming"));
    add_tree_child(skills, skill1);
    
    TreeNode* skill2 = create_tree_node("1", NODE_TYPE_VALUE);
    set_tree_node_value(skill2, strdup("Compiling"));
    add_tree_child(skills, skill2);
    
    // 打印树系统
    printf("\n树系统结构:\n");
    print_tree_node(system->root, 0);
    
    // 测试查找节点
    printf("\n查找节点: user.name\n");
    TreeNode* node = find_tree_node(system->root, "user.name");
    if (node) {
        printf("找到节点: %s, 值: %s\n", node->key, (char*)get_tree_node_value(node));
    } else {
        printf("未找到节点\n");
    }
    
    // 测试修改节点值
    printf("\n修改节点值: user.age = 2\n");
    node = find_tree_node(system->root, "user.age");
    if (node) {
        set_tree_node_value(node, strdup("2"));
        printf("修改后的值: %s\n", (char*)get_tree_node_value(node));
    }
    
    // 打印修改后的树
    printf("\n修改后的树系统:\n");
    print_tree_node(system->root, 0);
    
    destroy_tree_system(system);
    printf("\n树系统测试完成\n");
}

int main() {
    test_tree_system();
    return 0;
}
