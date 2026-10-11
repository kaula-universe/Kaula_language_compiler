/* path_tokenizer.c — 点分路径分段迭代器实现 */
#include <stddef.h>
#include "path_tokenizer.h"

char* kaula_path_next_segment(char* path, char** next) {
    if (next == NULL) return NULL;

    if (path != NULL) {
        *next = path;  // 开始一次新的迭代
    }

    char* cur = *next;
    if (cur == NULL || *cur == '\0') {
        return NULL;
    }

    // 跳过前导分隔符（处理 "..a" / "a..b" 间的空段）
    while (*cur == '.') {
        cur++;
    }
    if (*cur == '\0') {
        *next = NULL;
        return NULL;
    }

    char* seg_start = cur;
    while (*cur != '\0' && *cur != '.') {
        cur++;
    }

    if (*cur == '.') {
        *cur = '\0';
        *next = cur + 1;
    } else {
        *next = NULL;  // 到达末尾，下一轮返回 NULL
    }

    return seg_start;
}