#include "kaula.h"
#include "path_tokenizer.h"
#include <stdio.h>
#include <string.h>

typedef enum TreeNodeType {
    NODE_TYPE_VALUE,
    NODE_TYPE_OBJECT,
    NODE_TYPE_ARRAY
} TreeNodeType;

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

typedef struct TreeSystem {
    TreeNode* root;
} TreeSystem;

TreeSystem* create_tree_system() {
    TreeSystem* system = (TreeSystem*)kmm_v4_malloc(sizeof(TreeSystem));
    if (!system) return NULL;
    
    system->root = (TreeNode*)kmm_v4_malloc(sizeof(TreeNode));
    if (!system->root) {
        kmm_v4_free(system);
        return NULL;
    }
    
    system->root->key = kmm_v4_strdup("");
    system->root->type = NODE_TYPE_OBJECT;
    system->root->data.children = NULL;
    system->root->next = NULL;
    system->root->parent = NULL;
    
    return system;
}

void free_tree_node(TreeNode* node) {
    if (!node) return;

    if (node->type == NODE_TYPE_OBJECT || node->type == NODE_TYPE_ARRAY) {
        TreeNode* child = node->data.children;
        while (child) {
            TreeNode* next = child->next;
            free_tree_node(child);
            child = next;
        }
    } else if (node->type == NODE_TYPE_VALUE) {
        (void)node->data.value;
    }

    kmm_v4_free(node->key);
    kmm_v4_free(node);
}

void destroy_tree_system(TreeSystem* system) {
    if (!system) return;
    free_tree_node(system->root);
    kmm_v4_free(system);
}

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

TreeNode* create_tree_node(const char* key, TreeNodeType type) {
    TreeNode* node = (TreeNode*)kmm_v4_malloc(sizeof(TreeNode));
    if (!node) return NULL;
    
    node->key = kmm_v4_strdup(key);
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

int set_tree_node_value(TreeNode* node, void* value) {
    if (!node || node->type != NODE_TYPE_VALUE) {
        return 0;
    }

    node->data.value = value;
    return 1;
}

void* get_tree_node_value(TreeNode* node) {
    if (!node || node->type != NODE_TYPE_VALUE) {
        return NULL;
    }

    return node->data.value;
}

// ==================== 公开 API thin wrapper（kaula.h 声明名转发到内部实现） ====================
// find_tree_node 定义在本块之后，这里前向声明以满足 C89 旧式隐式声明规则
TreeNode* find_tree_node(TreeNode* root, const char* path);

Tree* tree_create(void) {
    return (Tree*)create_tree_system();
}

void tree_destroy(Tree* tree) {
    destroy_tree_system((TreeSystem*)tree);
}

void tree_set_root(Tree* tree, void* value) {
    TreeSystem* sys = (TreeSystem*)tree;
    if (!sys || !sys->root) return;
    set_tree_node_value(sys->root, value);
}

void tree_add_node(Tree* tree, const char* parent_name, const char* node_name, void* value) {
    TreeSystem* sys = (TreeSystem*)tree;
    if (!sys || !node_name) return;

    TreeNode* parent = sys->root;
    if (parent_name && parent_name[0] != '\0') {
        parent = find_tree_node(sys->root, parent_name);
    }
    if (!parent) return;

    /*
     * value == NULL 视为容器(OBJECT)节点：之后可在其下继续 add 子节点；
     * value != NULL 视为叶子(VALUE)节点。底层 add_tree_child 只允许
     * OBJECT/ARRAY 作父节点，故容器必须建为 OBJECT——此前一律建 VALUE
     * 导致挂不上子节点、只能产出扁平树。
     */
    TreeNodeType type = (value == NULL) ? NODE_TYPE_OBJECT : NODE_TYPE_VALUE;
    TreeNode* child = create_tree_node(node_name, type);
    if (!child) return;
    add_tree_child(parent, child);
    if (type == NODE_TYPE_VALUE) {
        set_tree_node_value(child, value);
    }
}

void* tree_get_node(Tree* tree, const char* node_name) {
    TreeSystem* sys = (TreeSystem*)tree;
    if (!sys || !node_name) return NULL;

    TreeNode* node = find_tree_node(sys->root, node_name);
    return node ? get_tree_node_value(node) : NULL;
}

TreeNode* find_tree_node(TreeNode* root, const char* path) {
    if (!root || !path) return NULL;

    if (strcmp(path, "") == 0) {
        return root;
    }

    char* path_copy = kmm_v4_strdup(path);
    if (!path_copy) return NULL;

    char* next = NULL;
    char* segment = kaula_path_next_segment(path_copy, &next);
    TreeNode* current = root;

    while (segment) {
        current = find_tree_child(current, segment);
        if (!current) {
            kmm_v4_free(path_copy);
            return NULL;
        }
        segment = kaula_path_next_segment(NULL, &next);
    }

    kmm_v4_free(path_copy);
    return current;
}

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

void test_tree_system() {
    printf("=== 树系统测试 ===\n");
    
    TreeSystem* system = create_tree_system();
    if (!system) {
        printf("创建树系统失败\n");
        return;
    }
    
    TreeNode* user = create_tree_node("user", NODE_TYPE_OBJECT);
    add_tree_child(system->root, user);
    
    TreeNode* name = create_tree_node("name", NODE_TYPE_VALUE);
    set_tree_node_value(name, kmm_v4_strdup("Kaula"));
    add_tree_child(user, name);
    
    TreeNode* age = create_tree_node("age", NODE_TYPE_VALUE);
    set_tree_node_value(age, kmm_v4_strdup("1"));
    add_tree_child(user, age);
    
    TreeNode* skills = create_tree_node("skills", NODE_TYPE_ARRAY);
    add_tree_child(user, skills);
    
    TreeNode* skill1 = create_tree_node("0", NODE_TYPE_VALUE);
    set_tree_node_value(skill1, kmm_v4_strdup("Programming"));
    add_tree_child(skills, skill1);
    
    TreeNode* skill2 = create_tree_node("1", NODE_TYPE_VALUE);
    set_tree_node_value(skill2, kmm_v4_strdup("Compiling"));
    add_tree_child(skills, skill2);
    
    printf("\n树系统结构:\n");
    print_tree_node(system->root, 0);
    
    printf("\n查找节点: user.name\n");
    TreeNode* node = find_tree_node(system->root, "user.name");
    if (node) {
        printf("找到节点: %s, 值: %s\n", node->key, (char*)get_tree_node_value(node));
    } else {
        printf("未找到节点\n");
    }
    
    printf("\n修改节点值: user.age = 2\n");
    node = find_tree_node(system->root, "user.age");
    if (node) {
        set_tree_node_value(node, kmm_v4_strdup("2"));
        printf("修改后的值: %s\n", (char*)get_tree_node_value(node));
    }
    
    printf("\n修改后的树系统:\n");
    print_tree_node(system->root, 0);
    
    destroy_tree_system(system);
    printf("\n树系统测试完成\n");
}