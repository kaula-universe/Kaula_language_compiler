#include "trie.h"
#include "../string/string.h"
#include <stdlib.h>
#include <string.h>

Trie* trie_create(void) {
    Trie* t = (Trie*)kmm_v4_malloc(sizeof(Trie));
    if (!t) return NULL;
    t->root = (TrieNode*)kmm_v4_malloc(sizeof(TrieNode));
    if (!t->root) { kmm_v4_free(t); return NULL; }
    memset(t->root, 0, sizeof(TrieNode));
    t->size = 0;
    return t;
}

static void trie_node_destroy(TrieNode* node) {
    if (!node) return;
    for (size_t i = 0; i < 256; i++) {
        trie_node_destroy(node->children[i]);
    }
    kmm_v4_free(node);
}

void trie_destroy(Trie* trie) {
    if (!trie) return;
    trie_node_destroy(trie->root);
    kmm_v4_free(trie);
}

void trie_insert(Trie* trie, const char* key, void* value) {
    if (!trie || !key) return;
    
    TrieNode* curr = trie->root;
    for (size_t i = 0; key[i]; i++) {
        unsigned char c = (unsigned char)key[i];
        if (!curr->children[c]) {
            curr->children[c] = (TrieNode*)kmm_v4_malloc(sizeof(TrieNode));
            if (!curr->children[c]) return;
            memset(curr->children[c], 0, sizeof(TrieNode));
        }
        curr = curr->children[c];
    }
    
    if (!curr->is_end) trie->size++;
    curr->is_end = true;
    curr->value = value;
}

void* trie_search(const Trie* trie, const char* key) {
    if (!trie || !key) return NULL;
    
    TrieNode* curr = trie->root;
    for (size_t i = 0; key[i]; i++) {
        unsigned char c = (unsigned char)key[i];
        if (!curr->children[c]) return NULL;
        curr = curr->children[c];
    }
    
    return curr->is_end ? curr->value : NULL;
}

bool_t trie_delete(Trie* trie, const char* key) {
    if (!trie || !key) return false;
    
    TrieNode** path[256];
    size_t depth = 0;
    TrieNode* curr = trie->root;
    
    for (size_t i = 0; key[i]; i++) {
        unsigned char c = (unsigned char)key[i];
        if (!curr->children[c]) return false;
        path[depth++] = &curr->children[c];
        curr = curr->children[c];
    }
    
    if (!curr->is_end) return false;
    curr->is_end = false;
    curr->value = NULL;
    trie->size--;
    
    for (ssize_t i = depth - 1; i >= 0; i--) {
        curr = *path[i];
        bool_t has_child = false;
        for (size_t j = 0; j < 256; j++) {
            if (curr->children[j]) { has_child = true; break; }
        }
        if (has_child || curr->is_end) break;
        kmm_v4_free(curr);
        *path[i] = NULL;
    }
    
    return true;
}

bool_t trie_contains(const Trie* trie, const char* key) {
    return trie_search(trie, key) != NULL;
}

bool_t trie_has_prefix(const Trie* trie, const char* prefix) {
    if (!trie || !prefix) return false;
    
    TrieNode* curr = trie->root;
    for (size_t i = 0; prefix[i]; i++) {
        unsigned char c = (unsigned char)prefix[i];
        if (!curr->children[c]) return false;
        curr = curr->children[c];
    }
    return true;
}

static void trie_prefix_search_recursive(const TrieNode* node, char* prefix, size_t len,
                                         void (*callback)(const char* key, void* value, void* ctx), void* ctx) {
    if (!node) return;
    
    if (node->is_end) {
        prefix[len] = '\0';
        callback(prefix, node->value, ctx);
    }
    
    for (size_t i = 0; i < 256; i++) {
        if (node->children[i]) {
            prefix[len] = (char)i;
            trie_prefix_search_recursive(node->children[i], prefix, len + 1, callback, ctx);
        }
    }
}

void trie_prefix_search(const Trie* trie, const char* prefix,
                        void (*callback)(const char* key, void* value, void* ctx), void* ctx) {
    if (!trie || !prefix || !callback) return;
    
    TrieNode* curr = trie->root;
    for (size_t i = 0; prefix[i]; i++) {
        unsigned char c = (unsigned char)prefix[i];
        if (!curr->children[c]) return;
        curr = curr->children[c];
    }
    
    size_t len = strlen(prefix);
    char* buffer = (char*)kmm_v4_malloc(len + 256);
    if (!buffer) return;
    strcpy(buffer, prefix);
    
    trie_prefix_search_recursive(curr, buffer, len, callback, ctx);
    kmm_v4_free(buffer);
}

size_t trie_size(const Trie* trie) {
    return trie ? trie->size : 0;
}

bool_t trie_is_empty(const Trie* trie) {
    return trie && trie->size == 0;
}

void trie_clear(Trie* trie) {
    if (!trie) return;
    trie_node_destroy(trie->root);
    trie->root = (TrieNode*)kmm_v4_malloc(sizeof(TrieNode));
    memset(trie->root, 0, sizeof(TrieNode));
    trie->size = 0;
}

void trie_traverse(const Trie* trie, void (*callback)(const char* key, void* value, void* ctx), void* ctx) {
    trie_prefix_search(trie, "", callback, ctx);
}