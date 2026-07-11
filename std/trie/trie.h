#pragma once
#include "../base/types.h"
#include "../memory/memory.h"

typedef struct TrieNode {
    struct TrieNode* children[256];
    bool_t is_end;
    void* value;
} TrieNode;

typedef struct Trie {
    TrieNode* root;
    size_t size;
} Trie;

Trie* trie_create(void);
void trie_destroy(Trie* trie);

void trie_insert(Trie* trie, const char* key, void* value);
void* trie_search(const Trie* trie, const char* key);
bool_t trie_delete(Trie* trie, const char* key);
bool_t trie_contains(const Trie* trie, const char* key);

bool_t trie_has_prefix(const Trie* trie, const char* prefix);
void trie_prefix_search(const Trie* trie, const char* prefix,
                        void (*callback)(const char* key, void* value, void* ctx), void* ctx);

size_t trie_size(const Trie* trie);
bool_t trie_is_empty(const Trie* trie);

void trie_clear(Trie* trie);
void trie_traverse(const Trie* trie, void (*callback)(const char* key, void* value, void* ctx), void* ctx);