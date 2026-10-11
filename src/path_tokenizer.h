/* path_tokenizer.h — 点分路径分段迭代器
 *
 * prefix_system.c / tree_system.c 依赖的路径分段工具。
 * 将 "user.skills.0" 之类的点分路径按 '.' 切分为逐段迭代。
 *
 * 用法：
 *   char* next = NULL;
 *   char* seg = kaula_path_next_segment(path_copy, &next);  // 首次：传路径
 *   while (seg) {
 *       ...使用 seg...
 *       seg = kaula_path_next_segment(NULL, &next);          // 后续：传 NULL
 *   }
 *
 * 注意：path 会被就地修改（' . ' -> '\0'），必须以可写缓冲区传入。
 */
#ifndef KAULA_PATH_TOKENIZER_H
#define KAULA_PATH_TOKENIZER_H

#ifdef __cplusplus
extern "C" {
#endif

/*
 * 取出下一个路径段并返回；*next 维护迭代状态（首次调用须传非 NULL path）。
 * 返回 NULL 表示迭代结束。连续分隔符（如 "a..b"）产生的空段会被跳过。
 */
char* kaula_path_next_segment(char* path, char** next);

#ifdef __cplusplus
}
#endif

#endif /* KAULA_PATH_TOKENIZER_H */