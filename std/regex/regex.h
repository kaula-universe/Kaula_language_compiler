#pragma once

// ============================================================================
// std/regex/regex.h - Kaula 标准库正则模块
//
// 自带引擎实现（递归下降解析 + 回溯匹配），不依赖 libc <regex.h>，
// 因此 Windows / Linux / macOS / freestanding 行为一致。
//
// 支持语法：
//   .            任意字符（不含换行，REGEX_FLAG_DOTALL 下含）
//   ^  $         行首 / 行尾锚点（REGEX_FLAG_MULTILINE 下逐行生效）
//   [abc] [^a-z] 字符类，支持范围、\d \w \s \D \W \S 与 POSIX [:alpha:] 等
//   ( )          捕获组，(?: ) 非捕获组，(?'name' ) / (?<name> ) 具名组
//   |            交替
//   * + ?        贪婪量词；追加 ? 变惰性（*? +? ??）
//   {n} {n,} {n,m}  重复次数，{n,m}? 为惰性
//   \b \B        单词边界 / 非单词边界
//   \n \t \r \f \v \0 \xHH  转义序列；\A 串首、\z 串尾
//   (?flags)     内联开关：i 忽略大小写、m 多行、s 点号含换行、x 忽略空白
//   (?:...)      分组同前
//
// 使用方式（C / Kaula 通用）：
//   Regex* re = regex_create("\\d+");
//   if (regex_is_valid(re)) {
//       String out = regex_replace(re, "a123", "#");
//   }
//   regex_destroy(re);
//
// 线程安全：Regex 编译后只读，可被多线程共享；但匹配过程使用 Regex 内部的
// 步数计数器与回溯栈，**同一个 Regex 实例不可被多线程同时用于匹配**。
// 并发场景请为每个线程单独 regex_create。
// ============================================================================

#include "../base/types.h"
#include "../string/string.h"

// ---- 匹配标志 --------------------------------------------------------------

#define REGEX_FLAG_NONE 0u        // 无标志
#define REGEX_FLAG_IGNORECASE 1u  // i：忽略 ASCII 大小写
#define REGEX_FLAG_MULTILINE 2u   // m：^ $ 匹配每行首尾
#define REGEX_FLAG_DOTALL 4u      // s：. 也能匹配换行
#define REGEX_FLAG_EXTENDED 8u    // x：忽略模式中的空白与 # 注释

// ---- 容量上限 --------------------------------------------------------------

#define REGEX_MAX_GROUPS 64       // 捕获组数量上限（含 0 号整体匹配）
#define REGEX_MAX_NAME_LEN 32     // 具名组名长度上限（含结尾 '\0'）
#define REGEX_DEFAULT_STEP_LIMIT 2000000u  // 单次匹配的默认步数预算

// ---- 类型 ------------------------------------------------------------------

// 编译后的正则。不透明句柄：只能通过本模块函数访问其字段。
typedef struct {
    const char* pattern;  // 模式串副本（regex_create 时拷贝，调用方可释放原串）
    void* compiled;       // 内部 RxProgram*
    bool_t valid;         // 编译是否成功
} Regex;

// 单个匹配区间（find_all / split 的产物）
typedef struct {
    size_t start;  // 起始下标
    size_t end;    // 结束下标（不含）
    String text;   // 匹配文本；需 regex_free_matches 释放
} RegexMatch;

// 单次匹配的捕获结果。captures[0] 是整体匹配，captures[1..n] 是各捕获组。
typedef struct {
    size_t start;   // 起始下标
    size_t end;     // 结束下标（不含）
    String text;    // 组文本；未参与匹配的组为 STRING_EMPTY 且 matched 为 false
    bool_t matched; // 该组是否参与了本次匹配
} RegexCapture;

// ---- 编译与生命周期 --------------------------------------------------------

// 编译模式串。失败时返回的 Regex 仍然有效（regex_is_valid 为 false），
// 可用 regex_error 取错误描述；仅当 pattern 为 NULL 或内存不足时返回 NULL。
extern Regex* regex_create(const char* pattern);

// 带标志编译（flags 为 REGEX_FLAG_* 的按位或）
extern Regex* regex_create_ex(const char* pattern, u32 flags);

extern void regex_destroy(Regex* regex);

extern bool_t regex_is_valid(const Regex* regex);

// 编译错误描述；无错误时返回 NULL
extern const char* regex_error(const Regex* regex);

// 模式串副本（Regex 持有，不是调用方原串）
extern const char* regex_pattern(const Regex* regex);

// 编译时生效的标志
extern u32 regex_flags(const Regex* regex);

// ---- 捕获组信息 ------------------------------------------------------------

// 捕获组数量（不含 0 号整体匹配）
extern size_t regex_group_count(const Regex* regex);

// 第 index 组的名字；无名组返回 NULL，index 越界返回 NULL
extern const char* regex_group_name(const Regex* regex, size_t index);

// 按名字查组号（0 号整体匹配）；未找到返回 -1
extern i64 regex_group_index(const Regex* regex, const char* name);

// ---- 匹配 ------------------------------------------------------------------

// 整串匹配：模式必须从位置 0 开始且消费到串尾
extern bool_t regex_match(const Regex* regex, const char* str);

// regex_match 的别名。Kaula 里 `match` 是保留字（match 表达式），
// 写成 std.regex.match(...) 会被解析成 match 表达式而不是函数调用 ——
// 生成的 C 里会出现 `check(std.regex, switch (re) {}, ...)` 这种垃圾。
// 因此 Kaula 侧走 regex_full_match。
extern bool_t regex_full_match(const Regex* regex, const char* str);

// 非锚定搜索：串内任意位置存在匹配即 true
extern bool_t regex_search(const Regex* regex, const char* str);

// 首个匹配的区间（从 from 起向左搜索）；命中返回 true 并写出 [start, end)
extern bool_t regex_find_first(const Regex* regex, const char* str, size_t from,
                               size_t* start, size_t* end);

// 所有非重叠匹配。返回的数组需 regex_free_matches 释放；
// 无匹配时返回 NULL 且 *count 为 0。
extern RegexMatch* regex_find_all(const Regex* regex, const char* str, size_t* count);

// 所有非重叠匹配（regex_find_all 的别名，保留历史名）
extern RegexMatch* regex_find(const Regex* regex, const char* str, size_t* count);

// 首个匹配的各捕获组。返回数组长度 *count（至少 1，含 0 号整体匹配），
// 需 regex_free_matches 释放；无匹配返回 NULL 且 *count 为 0。
extern RegexCapture* regex_captures(const Regex* regex, const char* str, size_t* count);

// ---- 替换与拆分 ------------------------------------------------------------

// 替换所有匹配。replacement 支持 $0 / $1..$9 / $&（整体匹配）/
// ${name}（具名组）/ $$（字面 $）；未参与匹配的组展开为空串。
extern String regex_replace(const Regex* regex, const char* str, const char* replacement);

// 只替换首个匹配
extern String regex_replace_first(const Regex* regex, const char* str, const char* replacement);

// 按匹配切分。max_parts 为 0 表示不限段数；结果需 regex_free_strings 释放。
// 若 str 以分隔匹配结尾，末尾会保留一个空串。
extern String* regex_split(const Regex* regex, const char* str, size_t max_parts,
                           size_t* count);

// 把 str 中的正则元字符转义成字面量，可安全用作模式串
extern String regex_quote(const char* str);

// ---- 释放 ------------------------------------------------------------------

extern void regex_free_matches(RegexMatch* matches, size_t count);
extern void regex_free_strings(String* strings, size_t count);

// ---- 步数预算（防止灾难性回溯）--------------------------------------------

// 设置单次匹配的步数预算；0 表示不限。默认 REGEX_DEFAULT_STEP_LIMIT。
extern void regex_set_step_limit(Regex* regex, size_t limit);

// 上一次匹配是否因超出步数预算而提前放弃。
// 返回 true 表示结果不可信（视为无匹配），应调大 regex_set_step_limit 重试。
extern bool_t regex_aborted(const Regex* regex);

// ---- 一次性便捷接口（内部临时编译，用完即销毁）------------------------------

extern bool_t regex_match_simple(const char* pattern, const char* str);
extern bool_t regex_search_simple(const char* pattern, const char* str);
extern String regex_replace_simple(const char* pattern, const char* str,
                                   const char* replacement);
extern RegexMatch* regex_find_simple(const char* pattern, const char* str, size_t* count);