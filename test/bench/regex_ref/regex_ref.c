// ============================================================================
// std/regex/regex.c - Kaula 标准库正则引擎
//
// 结构：
//   1. 位图工具        —— 256 位字符集
//   2. 内存池          —— AST 节点批量分配/释放
//   3. 递归下降解析器  —— 模式串 → AST
//   4. 指令生成器      —— AST → 扁平指令数组（Thompson 风格）
//   5. 回溯虚拟机      —— 迭代式执行，带步数预算
//   6. 公开 API
//
// 为什么用「回溯 + 显式栈」而不是递归 NFA 模拟：
//   回溯天然支持惰性量词与非贪婪交替，且捕获组语义直白；代价是最坏情况
//   指数级。这里用「步数预算」（默认 200 万步）把最坏情况从「挂死」降级为
//   「放弃并报错」，regex_ref_aborted() 可以区分「无匹配」与「超预算放弃」。
//
// 为什么不用递归写匹配器：像 `a*` 匹配十万字符的串，递归深度会顶爆 C 栈。
// 这里改成显式回溯栈，深度只受堆内存限制。
// ============================================================================

#include "regex_ref.h"

#include "../../../std/memory/memory.h"

#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

// 单个模式的指令数上限（防止 {n,m} 展开把程序撑爆）
#define RXR_MAX_INST 8192
// 错误描述长度
#define RXR_ERR_LEN 192

// ============================================================================
// 1. 256 位字符集工具
// ============================================================================

static void rxr_bs_set(unsigned char* b, int c) {
    b[(c >> 3) & 31] |= (unsigned char)(1u << (c & 7));
}

static int rxr_bs_get(const unsigned char* b, int c) {
    return (b[(c >> 3) & 31] >> (c & 7)) & 1;
}

static void rxr_bs_range(unsigned char* b, int lo, int hi) {
    int i;
    for (i = lo; i <= hi; i++) rxr_bs_set(b, i);
}

static void rxr_bs_or(unsigned char* dst, const unsigned char* src) {
    int i;
    for (i = 0; i < 32; i++) dst[i] |= src[i];
}

static void rxr_bs_not(unsigned char* b) {
    int i;
    for (i = 0; i < 32; i++) b[i] = (unsigned char)~b[i];
}

static void rxr_bs_clear(unsigned char* b) {
    memset(b, 0, 32);
}

// \d / \w / \s 三族简写类
static void rxr_bs_named(unsigned char* b, int c) {
    rxr_bs_clear(b);
    switch (c) {
        case 'd':
            rxr_bs_range(b, '0', '9');
            break;
        case 'w':
            rxr_bs_range(b, 'a', 'z');
            rxr_bs_range(b, 'A', 'Z');
            rxr_bs_range(b, '0', '9');
            rxr_bs_set(b, '_');
            break;
        case 's':
            rxr_bs_set(b, ' ');
            rxr_bs_set(b, '\t');
            rxr_bs_set(b, '\n');
            rxr_bs_set(b, '\r');
            rxr_bs_set(b, '\f');
            rxr_bs_set(b, '\v');
            break;
        default:
            break;
    }
}

// POSIX 字符类 [:name:]
static int rxr_bs_posix(unsigned char* b, const char* name) {
    rxr_bs_clear(b);
    if (strcmp(name, "alpha") == 0) {
        rxr_bs_range(b, 'a', 'z');
        rxr_bs_range(b, 'A', 'Z');
    } else if (strcmp(name, "digit") == 0) {
        rxr_bs_range(b, '0', '9');
    } else if (strcmp(name, "alnum") == 0) {
        rxr_bs_range(b, 'a', 'z');
        rxr_bs_range(b, 'A', 'Z');
        rxr_bs_range(b, '0', '9');
    } else if (strcmp(name, "upper") == 0) {
        rxr_bs_range(b, 'A', 'Z');
    } else if (strcmp(name, "lower") == 0) {
        rxr_bs_range(b, 'a', 'z');
    } else if (strcmp(name, "space") == 0) {
        rxr_bs_set(b, ' ');
        rxr_bs_set(b, '\t');
        rxr_bs_set(b, '\n');
        rxr_bs_set(b, '\r');
        rxr_bs_set(b, '\f');
        rxr_bs_set(b, '\v');
    } else if (strcmp(name, "blank") == 0) {
        rxr_bs_set(b, ' ');
        rxr_bs_set(b, '\t');
    } else if (strcmp(name, "punct") == 0) {
        rxr_bs_range(b, '!', '/');
        rxr_bs_range(b, ':', '@');
        rxr_bs_range(b, '[', '`');
        rxr_bs_range(b, '{', '~');
    } else if (strcmp(name, "print") == 0) {
        rxr_bs_range(b, ' ', '~');
    } else if (strcmp(name, "graph") == 0) {
        rxr_bs_range(b, '!', '~');
    } else if (strcmp(name, "cntrl") == 0) {
        rxr_bs_range(b, 0, 31);
        rxr_bs_set(b, 127);
    } else if (strcmp(name, "xdigit") == 0) {
        rxr_bs_range(b, '0', '9');
        rxr_bs_range(b, 'a', 'f');
        rxr_bs_range(b, 'A', 'F');
    } else {
        return 0;
    }
    return 1;
}

// ============================================================================
// 2. AST 内存池
// ============================================================================

typedef struct RxArenaChunk {
    struct RxArenaChunk* next;
    size_t used;
    size_t cap;
} RxArenaChunk;

#define RXR_ARENA_CHUNK_SIZE 4096
// 头部补齐到 16 字节，保证节点数据区 16 字节对齐
#define RXR_ARENA_HDR ((sizeof(RxArenaChunk) + 15u) & ~(size_t)15u)

typedef struct {
    RxArenaChunk* head;
} RxrArena;

static void* rxr_arena_alloc(RxrArena* a, size_t n) {
    n = (n + 15u) & ~(size_t)15u;
    if (n == 0) n = 16;
    if (a->head == NULL || a->head->cap - a->head->used < n) {
        size_t cap = RXR_ARENA_CHUNK_SIZE;
        RxArenaChunk* c;
        if (cap < n) cap = n;
        c = (RxArenaChunk*)kmm_v4_malloc(RXR_ARENA_HDR + cap);
        if (c == NULL) return NULL;
        c->next = a->head;
        c->used = 0;
        c->cap = cap;
        a->head = c;
    }
    {
        char* base = (char*)a->head + RXR_ARENA_HDR;
        void* p = base + a->head->used;
        a->head->used += n;
        return p;
    }
}

static void rxr_arena_free(RxrArena* a) {
    RxArenaChunk* c = a->head;
    while (c != NULL) {
        RxArenaChunk* next = c->next;
        kmm_v4_free(c);
        c = next;
    }
    a->head = NULL;
}

// ============================================================================
// 3. AST 与递归下降解析器
// ============================================================================

typedef enum {
    RXN_EMPTY = 0,  // 空（匹配零宽）
    RXN_CHAR,       // 单字符
    RXN_ANY,        // .
    RXN_CLASS,      // 字符类（已含取反结果）
    RXN_CAT,        // 连接
    RXN_ALT,        // 交替
    RXN_REPEAT,     // 重复 {min,max}，max<0 为无上界
    RXN_GROUP,      // 捕获组
    RXN_BOL,        // ^ / \A
    RXN_EOL,        // $ / \z
    RXN_WORDB,      // \b
    RXN_NWORDB      // \B
} RxNodeType;

typedef struct RxrNode {
    RxNodeType type;
    char ch;               // RXN_CHAR
    unsigned char set[32]; // RXN_CLASS
    struct RxrNode* a;
    struct RxrNode* b;
    int min;
    int max;               // RXN_REPEAT；<0 表示无上界
    bool_t lazy;           // RXN_REPEAT：惰性
    int group;             // RXN_GROUP：组号（1 起）
    u32 flags;             // 建节点时的生效标志（内联开关靠它逐节点生效）
} RxrNode;

typedef struct {
    const char* pat;
    size_t pos;
    size_t len;
    RxrArena arena;
    u32 flags;
    int group_count;
    char group_names[REGEX_MAX_GROUPS][REGEX_MAX_NAME_LEN];
    char err[RXR_ERR_LEN];
} RxrParser;

static RxrNode* rxr_parse_alt(RxrParser* p);

static void rxr_fail(RxrParser* p, const char* fmt, ...) {
    va_list ap;
    if (p->err[0] != '\0') return; // 只保留第一个错误
    va_start(ap, fmt);
    vsnprintf(p->err, sizeof(p->err), fmt, ap);
    va_end(ap);
}

static RxrNode* rxr_node(RxrParser* p, RxNodeType type) {
    RxrNode* n = (RxrNode*)rxr_arena_alloc(&p->arena, sizeof(RxrNode));
    if (n == NULL) {
        rxr_fail(p, "内存不足");
        return NULL;
    }
    memset(n, 0, sizeof(*n));
    n->type = type;
    n->max = -1;
    n->flags = p->flags;
    return n;
}

// --- 词法小工具 -------------------------------------------------------------
// 模式主体里在 EXTENDED 模式下跳过空白与 # 注释；字符类内部不跳。

static void rxr_skip_ws(RxrParser* p) {
    if ((p->flags & REGEX_FLAG_EXTENDED) == 0) return;
    for (;;) {
        while (p->pos < p->len) {
            char c = p->pat[p->pos];
            if (c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v') {
                p->pos++;
            } else {
                break;
            }
        }
        if (p->pos < p->len && p->pat[p->pos] == '#') {
            while (p->pos < p->len && p->pat[p->pos] != '\n') p->pos++;
            continue;
        }
        return;
    }
}

static int rxr_peek(RxrParser* p) {
    rxr_skip_ws(p);
    if (p->pos >= p->len) return -1;
    return (int)(unsigned char)p->pat[p->pos];
}

static int rxr_peek_raw(RxrParser* p) {
    if (p->pos >= p->len) return -1;
    return (int)(unsigned char)p->pat[p->pos];
}

static int rxr_take(RxrParser* p) {
    int c;
    rxr_skip_ws(p);
    if (p->pos >= p->len) return -1;
    c = (int)(unsigned char)p->pat[p->pos];
    p->pos++;
    return c;
}

static int rxr_take_raw(RxrParser* p) {
    int c;
    if (p->pos >= p->len) return -1;
    c = (int)(unsigned char)p->pat[p->pos];
    p->pos++;
    return c;
}

static int rxr_is_alnum(int c) {
    return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z');
}

// \xHH —— 解析十六进制转义；失败返回 -1
static int rxr_hex(RxrParser* p) {
    int v = 0;
    int i;
    for (i = 0; i < 2; i++) {
        int c;
        if (p->pos >= p->len) {
            rxr_fail(p, "\\x 后缺少两位十六进制数字");
            return -1;
        }
        c = (int)(unsigned char)p->pat[p->pos];
        if (c >= '0' && c <= '9') {
            v = v * 16 + (c - '0');
        } else if (c >= 'a' && c <= 'f') {
            v = v * 16 + (c - 'a' + 10);
        } else if (c >= 'A' && c <= 'F') {
            v = v * 16 + (c - 'A' + 10);
        } else {
            rxr_fail(p, "\\x 后的 '%c' 不是十六进制数字", c);
            return -1;
        }
        p->pos++;
    }
    return v & 0xff;
}

// 单字符转义的字面量值（不含 \d 一类简写类与 \b）
static int rxr_escape_char(RxrParser* p, int e) {
    switch (e) {
        case 'n': return '\n';
        case 't': return '\t';
        case 'r': return '\r';
        case 'f': return '\f';
        case 'v': return '\v';
        case 'a': return '\a';
        case 'e': return 0x1b;
        case '0': return '\0';
        case 'x': return rxr_hex(p);
        default: return e;
    }
}

// 字符类内部：读一个元素。简写类直接并入 set 并返回 -1；
// 否则返回字符码点。
static int rxr_class_elem(RxrParser* p, unsigned char* set) {
    int c = rxr_take_raw(p);
    if (c < 0) return -2; // 串尾
    if (c != '\\') return c;
    {
        int e = rxr_take_raw(p);
        if (e < 0) {
            rxr_fail(p, "字符类中反斜杠后无内容");
            return -2;
        }
        if (e == 'd' || e == 'w' || e == 's') {
            rxr_bs_named(set, e);
            return -1;
        }
        if (e == 'D' || e == 'W' || e == 'S') {
            unsigned char tmp[32];
            rxr_bs_named(tmp, (e == 'D') ? 'd' : (e == 'W' ? 'w' : 's'));
            rxr_bs_not(tmp);
            rxr_bs_or(set, tmp);
            return -1;
        }
        return rxr_escape_char(p, e);
    }
}

static RxrNode* rxr_parse_class(RxrParser* p) {
    unsigned char set[32];
    int negated = 0;
    int first = 1;
    RxrNode* n;

    rxr_bs_clear(set);
    if (rxr_peek_raw(p) == '^') {
        negated = 1;
        p->pos++;
    }
    for (;;) {
        int c = rxr_peek_raw(p);
        int lo;
        if (c < 0) {
            rxr_fail(p, "字符类缺少右方括号 ']'");
            return NULL;
        }
        // ']' 作为首字符时是字面量
        if (c == ']' && !first) {
            p->pos++;
            break;
        }
        first = 0;

        // POSIX 类 [:alpha:]
        if (c == '[' && p->pos + 1 < p->len && p->pat[p->pos + 1] == ':') {
            size_t q = p->pos + 2;
            while (q < p->len && p->pat[q] != ':') q++;
            if (q + 1 < p->len && p->pat[q] == ':' && p->pat[q + 1] == ']') {
                char name[24];
                size_t nlen = q - (p->pos + 2);
                if (nlen >= sizeof(name)) nlen = sizeof(name) - 1;
                memcpy(name, p->pat + p->pos + 2, nlen);
                name[nlen] = '\0';
                if (!rxr_bs_posix(set, name)) {
                    rxr_fail(p, "未知的 POSIX 字符类 [:%s:]", name);
                    return NULL;
                }
                p->pos = q + 2;
                continue;
            }
        }

        lo = rxr_class_elem(p, set);
        if (lo == -2) return NULL;
        if (lo < 0) continue; // 简写类已并入 set

        // 区间 lo-hi
        if (p->pos < p->len && p->pat[p->pos] == '-' && p->pos + 1 < p->len &&
            p->pat[p->pos + 1] != ']') {
            int hi;
            p->pos++; // 吃掉 '-'
            hi = rxr_class_elem(p, set);
            if (hi == -2) return NULL;
            if (hi < 0) {
                rxr_fail(p, "字符类区间端点不能是简写类");
                return NULL;
            }
            if (hi < lo) {
                rxr_fail(p, "字符类区间逆序");
                return NULL;
            }
            rxr_bs_range(set, lo, hi);
        } else {
            rxr_bs_set(set, lo);
        }
    }

    if (negated) rxr_bs_not(set);
    n = rxr_node(p, RXN_CLASS);
    if (n == NULL) return NULL;
    memcpy(n->set, set, 32);
    return n;
}

// 字符类之外的转义 \x
static RxrNode* rxr_parse_escape(RxrParser* p) {
    int e = rxr_take(p);
    RxrNode* n;

    if (e < 0) {
        rxr_fail(p, "模式串以孤立的反斜杠结尾");
        return NULL;
    }
    if (e == 'd' || e == 'D' || e == 'w' || e == 'W' || e == 's' || e == 'S') {
        unsigned char set[32];
        int neg = (e == 'D' || e == 'W' || e == 'S');
        rxr_bs_named(set, neg ? (e == 'D' ? 'd' : (e == 'W' ? 'w' : 's')) : e);
        if (neg) rxr_bs_not(set);
        n = rxr_node(p, RXN_CLASS);
        if (n == NULL) return NULL;
        memcpy(n->set, set, 32);
        return n;
    }
    switch (e) {
        case 'b': return rxr_node(p, RXN_WORDB);
        case 'B': return rxr_node(p, RXN_NWORDB);
        // \A / \z 复用锚点节点（受 MULTILINE 影响的语义见头文件说明）
        case 'A': return rxr_node(p, RXN_BOL);
        case 'z':
        case 'Z': return rxr_node(p, RXN_EOL);
        default: break;
    }
    if (e == 'x') {
        int v = rxr_hex(p);
        if (v < 0) return NULL;
        n = rxr_node(p, RXN_CHAR);
        if (n == NULL) return NULL;
        n->ch = (char)v;
        return n;
    }
    if (rxr_is_alnum(e)) {
        rxr_fail(p, "未知转义序列 \\%c", e);
        return NULL;
    }
    n = rxr_node(p, RXN_CHAR);
    if (n == NULL) return NULL;
    n->ch = (char)e;
    return n;
}

static RxrNode* rxr_parse_group(RxrParser* p) {
    int capture = 1;
    int gidx = -1;
    char name[REGEX_MAX_NAME_LEN];
    int have_name = 0;
    RxrNode* body;
    RxrNode* n;

    name[0] = '\0';

    if (rxr_peek(p) == '?') {
        int c1 = (p->pos + 1 < p->len) ? (int)(unsigned char)p->pat[p->pos + 1] : -1;

        if (c1 == ':') {
            capture = 0;
            p->pos += 2;
        } else if (c1 == '=' || c1 == '!') {
            rxr_fail(p, "不支持环视断言 (?%c)", c1);
            return NULL;
        } else if (c1 == '<' || c1 == '\'' || (c1 == 'P' && p->pos + 2 < p->len &&
                                              p->pat[p->pos + 2] == '<')) {
            char closer;
            size_t q;
            size_t nlen = 0;
            if (c1 == 'P') {
                closer = '>';
                q = p->pos + 3;
            } else {
                closer = (c1 == '\'') ? '\'' : '>';
                q = p->pos + 2;
            }
            if (q < p->len && p->pat[q] == '=') {
                rxr_fail(p, "不支持环视断言");
                return NULL;
            }
            while (q < p->len && p->pat[q] != closer && nlen + 1 < sizeof(name)) {
                name[nlen++] = p->pat[q++];
            }
            name[nlen] = '\0';
            if (q >= p->len || p->pat[q] != closer || nlen == 0) {
                rxr_fail(p, "具名组语法错误（需要 (?<name>...) 或 (?'name'...)）");
                return NULL;
            }
            p->pos = q + 1;
            have_name = 1;
        } else {
            // 内联开关 (?imsx) 或 (?imsx:
            u32 f = 0;
            int any = 0;
            size_t q = p->pos + 1;
            while (q < p->len) {
                char fc = p->pat[q];
                if (fc == 'i') f |= REGEX_FLAG_IGNORECASE;
                else if (fc == 'm') f |= REGEX_FLAG_MULTILINE;
                else if (fc == 's') f |= REGEX_FLAG_DOTALL;
                else if (fc == 'x') f |= REGEX_FLAG_EXTENDED;
                else if (fc == '-' || fc == 'U') { /* '-' 取反与 U 暂不支持 */ }
                else break;
                any = 1;
                q++;
            }
            if (!any || q >= p->len || (p->pat[q] != ')' && p->pat[q] != ':')) {
                rxr_fail(p, "无法识别的分组语法 '(?...'");
                return NULL;
            }
            if (p->pat[q] == ')') {
                // (?flags) 不是分组，只是设置开关后继续解析余下模式。
                // 这里必须直接返回空节点：若让控制流落到下面按普通分组处理，
                // '(' 已经被消费掉，后续 rxr_parse_alt 会一路读到模式串末尾，
                // 报出误导性的「分组缺少右括号」。
                p->pos = q + 1;
                p->flags = f;
                return rxr_node(p, RXN_EMPTY);
            } else {
                // (?flags: —— 只作用到本组内
                u32 outer = p->flags;
                p->pos = q + 1;
                p->flags = f;
                body = rxr_parse_alt(p);
                p->flags = outer;
                if (body == NULL) return NULL;
                if (rxr_peek(p) != ')') {
                    rxr_fail(p, "分组缺少右括号 ')'");
                    return NULL;
                }
                p->pos++;
                return body;
            }
        }
    }

    if (capture) {
        if (p->group_count + 1 >= REGEX_MAX_GROUPS) {
            rxr_fail(p, "捕获组数量超过上限 %d", REGEX_MAX_GROUPS - 1);
            return NULL;
        }
        gidx = ++p->group_count;
        if (have_name) {
            memcpy(p->group_names[gidx], name, strlen(name) + 1);
        }
    }

    body = rxr_parse_alt(p);
    if (body == NULL) return NULL;
    if (rxr_peek(p) != ')') {
        rxr_fail(p, "分组缺少右括号 ')'");
        return NULL;
    }
    p->pos++;

    if (!capture) return body;
    n = rxr_node(p, RXN_GROUP);
    if (n == NULL) return NULL;
    n->a = body;
    n->group = gidx;
    return n;
}

// 尝试解析 {n} / {n,} / {n,m}。返回 1 成功（已推进 pos），
// 0 表示不是合法的重复次数（'{' 留给上层当字面量），并可能已写入错误。
static int rxr_try_bounds(RxrParser* p, int* pmin, int* pmax) {
    size_t i = p->pos + 1; // 跳过 '{'
    long lo = 0;
    long hi;
    int digits = 0;

    if (i >= p->len || p->pat[i] < '0' || p->pat[i] > '9') return 0;
    while (i < p->len && p->pat[i] >= '0' && p->pat[i] <= '9') {
        lo = lo * 10 + (p->pat[i] - '0');
        if (lo > 65535) {
            rxr_fail(p, "重复次数过大");
            return 0;
        }
        i++;
        digits++;
    }
    if (digits == 0) return 0;

    hi = lo;
    if (i < p->len && p->pat[i] == ',') {
        i++;
        if (i < p->len && p->pat[i] >= '0' && p->pat[i] <= '9') {
            hi = 0;
            while (i < p->len && p->pat[i] >= '0' && p->pat[i] <= '9') {
                hi = hi * 10 + (p->pat[i] - '0');
                if (hi > 65535) {
                    rxr_fail(p, "重复次数过大");
                    return 0;
                }
                i++;
            }
        } else {
            hi = -1;
        }
    }
    if (i >= p->len || p->pat[i] != '}') return 0;
    if (hi >= 0 && hi < lo) {
        rxr_fail(p, "重复次数上界小于下界");
        return 0;
    }
    p->pos = i + 1;
    *pmin = (int)lo;
    *pmax = (int)hi;
    return 1;
}

static RxrNode* rxr_parse_atom(RxrParser* p) {
    int c = rxr_take(p);
    RxrNode* n;

    switch (c) {
        case -1:
            return rxr_node(p, RXN_EMPTY);
        case '(':
            return rxr_parse_group(p);
        case '[':
            return rxr_parse_class(p);
        case '.':
            return rxr_node(p, RXN_ANY);
        case '^':
            return rxr_node(p, RXN_BOL);
        case '$':
            return rxr_node(p, RXN_EOL);
        case '*':
        case '+':
        case '?':
            rxr_fail(p, "量词 '%c' 前面没有可重复的项", c);
            return NULL;
        case '\\':
            return rxr_parse_escape(p);
        default:
            n = rxr_node(p, RXN_CHAR);
            if (n == NULL) return NULL;
            n->ch = (char)c;
            return n;
    }
}

static RxrNode* rxr_parse_repeat(RxrParser* p) {
    RxrNode* atom = rxr_parse_atom(p);
    if (atom == NULL) return NULL;

    for (;;) {
        int c = rxr_peek(p);
        int min;
        int max;
        RxrNode* rep;

        if (c == '*') {
            p->pos++;
            min = 0;
            max = -1;
        } else if (c == '+') {
            p->pos++;
            min = 1;
            max = -1;
        } else if (c == '?') {
            p->pos++;
            min = 0;
            max = 1;
        } else if (c == '{' && rxr_try_bounds(p, &min, &max)) {
            /* rxr_try_bounds 已推进 pos */
        } else {
            if (p->err[0] != '\0') return NULL;
            break;
        }

        rep = rxr_node(p, RXN_REPEAT);
        if (rep == NULL) return NULL;
        rep->a = atom;
        rep->min = min;
        rep->max = max;
        if (rxr_peek(p) == '?') {
            rep->lazy = true;
            p->pos++;
        }
        atom = rep;
    }
    return atom;
}

static RxrNode* rxr_parse_concat(RxrParser* p) {
    RxrNode* cat = NULL;
    for (;;) {
        int c = rxr_peek(p);
        RxrNode* item;
        if (c < 0 || c == '|' || c == ')') break;
        item = rxr_parse_repeat(p);
        if (item == NULL) return NULL;
        if (cat == NULL) {
            cat = item;
        } else {
            RxrNode* n = rxr_node(p, RXN_CAT);
            if (n == NULL) return NULL;
            n->a = cat;
            n->b = item;
            cat = n;
        }
    }
    if (cat == NULL) cat = rxr_node(p, RXN_EMPTY);
    return cat;
}

static RxrNode* rxr_parse_alt(RxrParser* p) {
    RxrNode* left = rxr_parse_concat(p);
    if (left == NULL) return NULL;
    for (;;) {
        if (rxr_peek(p) != '|') break;
        p->pos++;
        {
            RxrNode* right = rxr_parse_concat(p);
            RxrNode* n;
            if (right == NULL) return NULL;
            n = rxr_node(p, RXN_ALT);
            if (n == NULL) return NULL;
            n->a = left;
            n->b = right;
            left = n;
        }
    }
    return left;
}

// ============================================================================
// 4. 指令数组与生成器
// ============================================================================

typedef enum {
    RXR_OP_CHAR = 0,
    RXR_OP_ANY,
    RXR_OP_CLASS,
    RXR_OP_SPLIT,
    RXR_OP_JMP,
    RXR_OP_SAVE,
    RXR_OP_MATCH,
    RXR_OP_BOL,
    RXR_OP_EOL,
    RXR_OP_WORDB,
    RXR_OP_NWORDB
} RxOp;

typedef struct {
    int op;
    int x;
    int y;
    int slot;
    bool_t lazy;
    char ch;
    unsigned char set[32];
    // 本指令的生效标志。CHAR 看 IGNORECASE、ANY 看 DOTALL、BOL/EOL 看 MULTILINE。
    // 必须逐指令下发：内联开关 (?i)/(?m)/(?s) 只对部分节点生效，
    // 匹配期若统一读 pr->flags，内联开关就等于没写。
    u32 iflags;
} RxrInst;

// 回溯帧：kind=0 是 SPLIT 的备选分支，kind=1 是捕获槽回滚
typedef struct {
    int kind;
    int pc;
    size_t pos;
    int slot;
    long long old;
} RxrFrame;

typedef struct {
    RxrInst* code;
    size_t code_len;
    size_t code_cap;
    bool_t oom;

    u32 flags;
    size_t group_count;
    char group_names[REGEX_MAX_GROUPS][REGEX_MAX_NAME_LEN];

    size_t step_limit;
    size_t steps;
    bool_t aborted;
    size_t match_end;

    long long* caps;   // 2*(group_count+1) 个槽位，-1 表示未参与
    size_t caps_len;

    RxrFrame* stack;
    size_t stack_len;
    size_t stack_cap;

    RxrArena arena;
    char err[RXR_ERR_LEN];
} RxrProgram;

static void rxr_pr_fail(RxrProgram* pr, const char* fmt, ...) {
    va_list ap;
    if (pr->err[0] != '\0') return;
    va_start(ap, fmt);
    vsnprintf(pr->err, sizeof(pr->err), fmt, ap);
    va_end(ap);
}

static int rxr_emit(RxrProgram* pr, int op) {
    RxrInst* in;
    if (pr->oom) return 0;
    if (pr->code_len >= RXR_MAX_INST) {
        rxr_pr_fail(pr, "模式过于复杂（指令数超过 %d）", RXR_MAX_INST);
        pr->oom = true;
        return 0;
    }
    if (pr->code_len == pr->code_cap) {
        size_t ncap = pr->code_cap ? pr->code_cap * 2 : 64;
        RxrInst* n = (RxrInst*)kmm_v4_malloc(ncap * sizeof(RxrInst));
        if (n == NULL) {
            rxr_pr_fail(pr, "内存不足");
            pr->oom = true;
            return 0;
        }
        if (pr->code != NULL) {
            memcpy(n, pr->code, pr->code_len * sizeof(RxrInst));
            kmm_v4_free(pr->code);
        }
        pr->code = n;
        pr->code_cap = ncap;
    }
    in = &pr->code[pr->code_len];
    memset(in, 0, sizeof(*in));
    in->op = op;
    in->iflags = pr->flags;
    return (int)pr->code_len++;
}

static void rxr_emit_node(RxrProgram* pr, RxrNode* n);

// 让 sp 这条 SPLIT 优先走 first_trial
static void rxr_set_split(RxrProgram* pr, int sp, int first_trial, int second_trial,
                         bool_t lazy) {
    if (lazy) {
        pr->code[sp].x = second_trial;
        pr->code[sp].y = first_trial;
    } else {
        pr->code[sp].x = first_trial;
        pr->code[sp].y = second_trial;
    }
}

static void rxr_emit_repeat(RxrProgram* pr, RxrNode* n) {
    int i;
    int min = n->min;
    int max = n->max;

    if (max < 0) {
        // 无上界：必须是「先分裂、再消费」的形状。
        // 早先把 SPLIT 放在 body 之后，`a*` 匹配 "b" 时 body 消费失败，
        // 而跳过分支在 body 后面根本走不到 —— 空匹配直接丢。
        if (min == 0) {
            // L: SPLIT(body, OUT); body; JMP L; OUT:
            int sp = rxr_emit(pr, RXR_OP_SPLIT);
            int body;
            int jmp;
            int out;
            if (pr->oom) return;
            body = (int)pr->code_len;
            rxr_emit_node(pr, n->a);
            jmp = rxr_emit(pr, RXR_OP_JMP);
            out = (int)pr->code_len;
            if (pr->oom) return;
            pr->code[jmp].x = sp;
            rxr_set_split(pr, sp, body, out, n->lazy);
        } else {
            // 先铺 min-1 份，再 L: body; SPLIT(L, OUT)
            for (i = 0; i < min - 1; i++) {
                rxr_emit_node(pr, n->a);
                if (pr->oom) return;
            }
            {
                int body = (int)pr->code_len;
                int sp;
                int out;
                rxr_emit_node(pr, n->a);
                sp = rxr_emit(pr, RXR_OP_SPLIT);
                out = (int)pr->code_len;
                if (pr->oom) return;
                rxr_set_split(pr, sp, body, out, n->lazy);
            }
        }
        return;
    }

    // 必重复部分
    for (i = 0; i < min; i++) {
        rxr_emit_node(pr, n->a);
        if (pr->oom) return;
    }
    // 可选部分：嵌套的 SPLIT，每层跳到本层之后
    for (i = min; i < max; i++) {
        int sp = rxr_emit(pr, RXR_OP_SPLIT);
        int bstart;
        int after;
        if (pr->oom) return;
        bstart = sp + 1;
        rxr_emit_node(pr, n->a);
        after = (int)pr->code_len;
        if (pr->oom) return;
        rxr_set_split(pr, sp, bstart, after, n->lazy);
    }
}

static void rxr_emit_node(RxrProgram* pr, RxrNode* n) {
    int i;
    if (n == NULL || pr->oom) return;
    switch (n->type) {
        case RXN_EMPTY:
            break;
        case RXN_CHAR:
            i = rxr_emit(pr, RXR_OP_CHAR);
            if (i >= 0) {
                pr->code[i].ch = n->ch;
                pr->code[i].iflags = n->flags;
            }
            break;
        case RXN_ANY:
            i = rxr_emit(pr, RXR_OP_ANY);
            if (i >= 0) pr->code[i].iflags = n->flags;
            break;
        case RXN_CLASS:
            i = rxr_emit(pr, RXR_OP_CLASS);
            if (i >= 0) memcpy(pr->code[i].set, n->set, 32);
            break;
        case RXN_BOL:
            i = rxr_emit(pr, RXR_OP_BOL);
            if (i >= 0) pr->code[i].iflags = n->flags;
            break;
        case RXN_EOL:
            i = rxr_emit(pr, RXR_OP_EOL);
            if (i >= 0) pr->code[i].iflags = n->flags;
            break;
        case RXN_WORDB:
            rxr_emit(pr, RXR_OP_WORDB);
            break;
        case RXN_NWORDB:
            rxr_emit(pr, RXR_OP_NWORDB);
            break;
        case RXN_CAT:
            rxr_emit_node(pr, n->a);
            rxr_emit_node(pr, n->b);
            break;
        case RXN_ALT: {
            int sp = rxr_emit(pr, RXR_OP_SPLIT);
            int jend;
            int bstart;
            int end;
            rxr_emit_node(pr, n->a);
            jend = rxr_emit(pr, RXR_OP_JMP);
            bstart = (int)pr->code_len;
            rxr_emit_node(pr, n->b);
            end = (int)pr->code_len;
            if (pr->oom) return;
            pr->code[sp].x = sp + 1;
            pr->code[sp].y = bstart;
            pr->code[jend].x = end;
            break;
        }
        case RXN_GROUP:
            i = rxr_emit(pr, RXR_OP_SAVE);
            if (i >= 0) pr->code[i].slot = n->group * 2;
            rxr_emit_node(pr, n->a);
            i = rxr_emit(pr, RXR_OP_SAVE);
            if (i >= 0) pr->code[i].slot = n->group * 2 + 1;
            break;
        case RXN_REPEAT:
            rxr_emit_repeat(pr, n);
            break;
        default:
            break;
    }
}

// ============================================================================
// 5. 回溯虚拟机
// ============================================================================

static unsigned char rxr_lower(unsigned char c) {
    return (c >= 'A' && c <= 'Z') ? (unsigned char)(c + 32) : c;
}

static int rxr_char_eq(unsigned char a, unsigned char b, int icase) {
    if (a == b) return 1;
    if (!icase) return 0;
    return rxr_lower(a) == rxr_lower(b);
}

static int rxr_is_word(int c) {
    return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
           c == '_';
}

static int rxr_push(RxrProgram* pr, int kind, int pc, size_t pos, int slot, long long old) {
    if (pr->stack_len == pr->stack_cap) {
        size_t ncap = pr->stack_cap ? pr->stack_cap * 2 : 128;
        RxrFrame* n = (RxrFrame*)kmm_v4_malloc(ncap * sizeof(RxrFrame));
        if (n == NULL) return 0;
        if (pr->stack != NULL) {
            memcpy(n, pr->stack, pr->stack_len * sizeof(RxrFrame));
            kmm_v4_free(pr->stack);
        }
        pr->stack = n;
        pr->stack_cap = ncap;
    }
    pr->stack[pr->stack_len].kind = kind;
    pr->stack[pr->stack_len].pc = pc;
    pr->stack[pr->stack_len].pos = pos;
    pr->stack[pr->stack_len].slot = slot;
    pr->stack[pr->stack_len].old = old;
    pr->stack_len++;
    return 1;
}

// 从 start 处锚定执行一次匹配。捕获结果写入 pr->caps。
// 前置条件：调用方已把 caps 全部置 -1 并重置 steps/aborted。
static bool_t rxr_run(RxrProgram* pr, const char* s, size_t len, size_t start) {
    int pc = 0;
    size_t pos = start;

    pr->stack_len = 0;
    pr->match_end = start;

    for (;;) {
        const RxrInst* in;
        if (pr->step_limit != 0 && ++pr->steps > pr->step_limit) {
            pr->aborted = true;
            break;
        }
        in = &pr->code[pc];
        switch (in->op) {
            case RXR_OP_CHAR:
                if (pos < len &&
                    rxr_char_eq((unsigned char)s[pos], (unsigned char)in->ch,
                               (in->iflags & REGEX_FLAG_IGNORECASE) != 0)) {
                    pc++;
                    pos++;
                } else {
                    goto backtrack;
                }
                break;
            case RXR_OP_ANY:
                if (pos < len && ((in->iflags & REGEX_FLAG_DOTALL) || s[pos] != '\n')) {
                    pc++;
                    pos++;
                } else {
                    goto backtrack;
                }
                break;
            case RXR_OP_CLASS:
                if (pos < len && rxr_bs_get(in->set, (int)(unsigned char)s[pos])) {
                    pc++;
                    pos++;
                } else {
                    goto backtrack;
                }
                break;
            case RXR_OP_SPLIT:
                if (!rxr_push(pr, 0, in->lazy ? in->x : in->y, pos, 0, 0)) {
                    pr->aborted = true;
                    goto done;
                }
                pc = in->lazy ? in->y : in->x;
                break;
            case RXR_OP_JMP:
                pc = in->x;
                break;
            case RXR_OP_SAVE: {
                int slot = in->slot;
                if (slot < 0 || (size_t)slot >= pr->caps_len) {
                    pc++;
                    break;
                }
                if (!rxr_push(pr, 1, 0, 0, slot, pr->caps[slot])) {
                    pr->aborted = true;
                    goto done;
                }
                pr->caps[slot] = (long long)pos;
                pc++;
                break;
            }
            case RXR_OP_BOL:
                if (pos == 0 ||
                    ((in->iflags & REGEX_FLAG_MULTILINE) && pos > 0 && s[pos - 1] == '\n')) {
                    pc++;
                } else {
                    goto backtrack;
                }
                break;
            case RXR_OP_EOL:
                if (pos == len ||
                    ((in->iflags & REGEX_FLAG_MULTILINE) && pos < len && s[pos] == '\n')) {
                    pc++;
                } else {
                    goto backtrack;
                }
                break;
            case RXR_OP_WORDB:
            case RXR_OP_NWORDB: {
                int before = (pos > 0) ? rxr_is_word((int)(unsigned char)s[pos - 1]) : 0;
                int after = (pos < len) ? rxr_is_word((int)(unsigned char)s[pos]) : 0;
                int boundary = (before != after);
                int want = (in->op == RXR_OP_WORDB) ? boundary : !boundary;
                if (want) {
                    pc++;
                } else {
                    goto backtrack;
                }
                break;
            }
            case RXR_OP_MATCH:
                pr->match_end = pos;
                pr->stack_len = 0;
                return true;
            default:
                goto done;
        }
        continue;

    backtrack:
        for (;;) {
            RxrFrame f;
            if (pr->stack_len == 0) goto done;
            f = pr->stack[--pr->stack_len];
            if (f.kind == 1) {
                pr->caps[f.slot] = f.old;
                continue;
            }
            pc = f.pc;
            pos = f.pos;
            break;
        }
    }

done:
    pr->stack_len = 0;
    return false;
}

static void rxr_caps_reset(RxrProgram* pr) {
    size_t i;
    for (i = 0; i < pr->caps_len; i++) pr->caps[i] = -1;
    pr->steps = 0;
    pr->aborted = false;
}

// 每个候选起点都要重置步数：预算是「单次匹配」的上限，
// 而 rxr_search 会从 from 到 len 逐点尝试，若不在每次尝试前清零，
// 长串上累计步数会早早越过预算，后续起点全被 aborted 短路成「无匹配」。
static void rxr_caps_reset_try(RxrProgram* pr) {
    size_t i;
    for (i = 0; i < pr->caps_len; i++) pr->caps[i] = -1;
    pr->steps = 0;
}

// 从 from 起做非锚定搜索（最左优先）
static bool_t rxr_search(RxrProgram* pr, const char* s, size_t len, size_t from) {
    size_t start;
    rxr_caps_reset(pr);
    for (start = from; start <= len; start++) {
        rxr_caps_reset_try(pr);
        if (rxr_run(pr, s, len, start)) return true;
        if (pr->aborted) return false;
    }
    return false;
}

// 整串匹配
static bool_t rxr_match_full(RxrProgram* pr, const char* s, size_t len) {
    rxr_caps_reset(pr);
    if (!rxr_run(pr, s, len, 0)) return false;
    return pr->match_end == len;
}

// ============================================================================
// 6. 公开 API
// ============================================================================

static char* rxr_strdup(const char* s) {
    size_t n = strlen(s);
    char* buf = (char*)kmm_v4_malloc(n + 1);
    if (buf == NULL) return NULL;
    memcpy(buf, s, n + 1);
    return buf;
}

static RxrProgram* rxr_prog_new(u32 flags) {
    RxrProgram* pr = (RxrProgram*)kmm_v4_malloc(sizeof(RxrProgram));
    if (pr == NULL) return NULL;
    memset(pr, 0, sizeof(*pr));
    pr->flags = flags;
    pr->step_limit = REGEX_DEFAULT_STEP_LIMIT;
    return pr;
}

static void rxr_prog_free(RxrProgram* pr) {
    if (pr == NULL) return;
    rxr_arena_free(&pr->arena);
    if (pr->code != NULL) kmm_v4_free(pr->code);
    if (pr->caps != NULL) kmm_v4_free(pr->caps);
    if (pr->stack != NULL) kmm_v4_free(pr->stack);
    kmm_v4_free(pr);
}

RegexRef* regex_ref_create_ex(const char* pattern, u32 flags) {
    RegexRef* re;
    RxrProgram* pr;
    RxrParser p;
    RxrNode* root;

    if (pattern == NULL) return NULL;

    re = (RegexRef*)kmm_v4_malloc(sizeof(RegexRef));
    if (re == NULL) return NULL;
    memset(re, 0, sizeof(RegexRef));

    pr = rxr_prog_new(flags);
    if (pr == NULL) {
        kmm_v4_free(re);
        return NULL;
    }
    re->compiled = pr;

    re->pattern = rxr_strdup(pattern);
    if (re->pattern == NULL) {
        rxr_prog_free(pr);
        re->compiled = NULL;
        kmm_v4_free(re);
        return NULL;
    }

    memset(&p, 0, sizeof(p));
    p.pat = pattern;
    p.pos = 0;
    p.len = strlen(pattern);
    p.flags = flags;
    p.group_count = 0;

    root = rxr_parse_alt(&p);

    if (root == NULL && p.err[0] == '\0') {
        rxr_fail(&p, "模式解析失败");
    }
    // 分组未闭合时解析停在中途，err 已记录；未记录则说明结构异常
    if (root != NULL && p.err[0] == '\0' && p.pos < p.len) {
        int c = p.pat[p.pos];
        if (c == ')') rxr_fail(&p, "多余的右括号 ')'");
    }

    if (root == NULL || p.err[0] != '\0') {
        snprintf(pr->err, sizeof(pr->err), "%s", p.err[0] ? p.err : "模式解析失败");
        rxr_arena_free(&p.arena);
        re->valid = false;
        return re;
    }

    // 整体匹配 0 号组：SAVE 0 ... SAVE 1 ... MATCH
    {
        int s0 = rxr_emit(pr, RXR_OP_SAVE);
        int s1;
        int m;
        if (s0 >= 0) pr->code[s0].slot = 0;
        rxr_emit_node(pr, root);
        s1 = rxr_emit(pr, RXR_OP_SAVE);
        if (s1 >= 0) pr->code[s1].slot = 1;
        m = rxr_emit(pr, RXR_OP_MATCH);
        if (m < 0) {
            rxr_pr_fail(pr, "模式过于复杂");
        }
    }

    rxr_arena_free(&p.arena);

    if (pr->oom) {
        rxr_pr_fail(pr, "模式过于复杂或内存不足");
    }

    pr->group_count = (size_t)p.group_count;
    memcpy(pr->group_names, p.group_names, sizeof(pr->group_names));

    pr->caps_len = (pr->group_count + 1) * 2;
    pr->caps = (long long*)kmm_v4_malloc(sizeof(long long) * (pr->caps_len ? pr->caps_len : 2));
    if (pr->caps == NULL) {
        rxr_pr_fail(pr, "内存不足");
        re->valid = false;
        return re;
    }
    rxr_caps_reset(pr);

    re->valid = (pr->err[0] == '\0');
    return re;
}

RegexRef* regex_ref_create(const char* pattern) {
    return regex_ref_create_ex(pattern, REGEX_FLAG_NONE);
}

void regex_ref_destroy(RegexRef* regex) {
    if (regex == NULL) return;
    if (regex->compiled != NULL) rxr_prog_free((RxrProgram*)regex->compiled);
    if (regex->pattern != NULL) kmm_v4_free((char*)regex->pattern);
    regex->compiled = NULL;
    regex->pattern = NULL;
    kmm_v4_free(regex);
}

bool_t regex_ref_is_valid(const RegexRef* regex) {
    if (regex == NULL) return false;
    return regex->valid;
}

const char* regex_ref_error(const RegexRef* regex) {
    RxrProgram* pr;
    if (regex == NULL || regex->compiled == NULL) return NULL;
    pr = (RxrProgram*)regex->compiled;
    if (pr->err[0] == '\0') return NULL;
    return pr->err;
}

const char* regex_ref_pattern(const RegexRef* regex) {
    if (regex == NULL) return NULL;
    return regex->pattern;
}

u32 regex_ref_flags(const RegexRef* regex) {
    RxrProgram* pr;
    if (regex == NULL || regex->compiled == NULL) return REGEX_FLAG_NONE;
    pr = (RxrProgram*)regex->compiled;
    return pr->flags;
}

size_t regex_ref_group_count(const RegexRef* regex) {
    RxrProgram* pr;
    if (regex == NULL || regex->compiled == NULL) return 0;
    pr = (RxrProgram*)regex->compiled;
    return pr->group_count;
}

const char* regex_ref_group_name(const RegexRef* regex, size_t index) {
    RxrProgram* pr;
    if (regex == NULL || regex->compiled == NULL) return NULL;
    pr = (RxrProgram*)regex->compiled;
    if (index == 0 || index > pr->group_count) return NULL;
    if (pr->group_names[index][0] == '\0') return NULL;
    return pr->group_names[index];
}

i64 regex_ref_group_index(const RegexRef* regex, const char* name) {
    RxrProgram* pr;
    size_t i;
    if (regex == NULL || regex->compiled == NULL || name == NULL) return -1;
    pr = (RxrProgram*)regex->compiled;
    for (i = 1; i <= pr->group_count; i++) {
        if (pr->group_names[i][0] != '\0' && strcmp(pr->group_names[i], name) == 0) {
            return (i64)i;
        }
    }
    return -1;
}

void regex_ref_set_step_limit(RegexRef* regex, size_t limit) {
    RxrProgram* pr;
    if (regex == NULL || regex->compiled == NULL) return;
    pr = (RxrProgram*)regex->compiled;
    pr->step_limit = limit;
}

bool_t regex_ref_aborted(const RegexRef* regex) {
    RxrProgram* pr;
    if (regex == NULL || regex->compiled == NULL) return false;
    pr = (RxrProgram*)regex->compiled;
    return pr->aborted;
}

bool_t regex_ref_match(const RegexRef* regex, const char* str) {
    RxrProgram* pr;
    if (regex == NULL || !regex->valid || str == NULL) return false;
    pr = (RxrProgram*)regex->compiled;
    return rxr_match_full(pr, str, strlen(str));
}

bool_t regex_ref_full_match(const RegexRef* regex, const char* str) {
    return regex_ref_match(regex, str);
}

bool_t regex_ref_search(const RegexRef* regex, const char* str) {
    RxrProgram* pr;
    if (regex == NULL || !regex->valid || str == NULL) return false;
    pr = (RxrProgram*)regex->compiled;
    return rxr_search(pr, str, strlen(str), 0);
}

bool_t regex_ref_find_first(const RegexRef* regex, const char* str, size_t from, size_t* start,
                        size_t* end) {
    RxrProgram* pr;
    size_t len;
    if (regex == NULL || !regex->valid || str == NULL) return false;
    pr = (RxrProgram*)regex->compiled;
    len = strlen(str);
    if (from > len) return false;
    if (!rxr_search(pr, str, len, from)) return false;
    if (start != NULL) *start = (size_t)pr->caps[0];
    if (end != NULL) *end = (size_t)pr->caps[1];
    return true;
}

static String rxr_slice(const char* s, long long a, long long b) {
    size_t n;
    char* buf;
    if (a < 0 || b < 0 || b < a) return STRING_EMPTY;
    n = (size_t)(b - a);
    buf = (char*)kmm_v4_malloc(n + 1);
    if (buf == NULL) return STRING_EMPTY;
    memcpy(buf, s + (size_t)a, n);
    buf[n] = '\0';
    return string_wrap_n(buf, n);
}

RegexMatchRef* regex_ref_find_all(const RegexRef* regex, const char* str, size_t* count) {
    RxrProgram* pr;
    size_t len;
    size_t cap = 8;
    size_t n = 0;
    size_t pos = 0;
    RegexMatchRef* out;

    if (count != NULL) *count = 0;
    if (regex == NULL || !regex->valid || str == NULL) return NULL;
    pr = (RxrProgram*)regex->compiled;
    len = strlen(str);

    out = (RegexMatchRef*)kmm_v4_malloc(cap * sizeof(RegexMatchRef));
    if (out == NULL) return NULL;

    while (pos <= len) {
        if (!rxr_search(pr, str, len, pos)) break;
        {
            long long s0 = pr->caps[0];
            long long e0 = pr->caps[1];
            if (e0 < s0) break;
            if (n == cap) {
                RegexMatchRef* bigger;
                cap *= 2;
                bigger = (RegexMatchRef*)kmm_v4_malloc(cap * sizeof(RegexMatchRef));
                if (bigger == NULL) {
                    regex_ref_free_matches(out, n);
                    return NULL;
                }
                memcpy(bigger, out, n * sizeof(RegexMatchRef));
                kmm_v4_free(out);
                out = bigger;
            }
            out[n].start = (size_t)s0;
            out[n].end = (size_t)e0;
            out[n].text = rxr_slice(str, s0, e0);
            n++;
            // 零宽匹配必须前进一位，否则死循环
            pos = (e0 > s0) ? (size_t)e0 : (size_t)e0 + 1;
        }
    }

    if (n == 0) {
        kmm_v4_free(out);
        return NULL;
    }
    if (count != NULL) *count = n;
    return out;
}

RegexMatchRef* regex_ref_find(const RegexRef* regex, const char* str, size_t* count) {
    return regex_ref_find_all(regex, str, count);
}

RegexCaptureRef* regex_ref_captures(const RegexRef* regex, const char* str, size_t* count) {
    RxrProgram* pr;
    size_t len;
    size_t g;
    size_t n;
    RegexCaptureRef* caps;

    if (count != NULL) *count = 0;
    if (regex == NULL || !regex->valid || str == NULL) return NULL;
    pr = (RxrProgram*)regex->compiled;
    len = strlen(str);

    if (!rxr_search(pr, str, len, 0)) return NULL;

    n = pr->group_count + 1;
    caps = (RegexCaptureRef*)kmm_v4_malloc(n * sizeof(RegexCaptureRef));
    if (caps == NULL) return NULL;

    for (g = 0; g < n; g++) {
        long long a = pr->caps[2 * g];
        long long b = pr->caps[2 * g + 1];
        int ok = (a >= 0 && b >= 0 && b >= a);
        caps[g].matched = ok ? true : false;
        caps[g].start = ok ? (size_t)a : 0;
        caps[g].end = ok ? (size_t)b : 0;
        caps[g].text = ok ? rxr_slice(str, a, b) : STRING_EMPTY;
    }
    if (count != NULL) *count = n;
    return caps;
}

// --- 替换 ------------------------------------------------------------------

typedef struct {
    char* data;
    size_t len;
    size_t cap;
    bool_t oom;
} RxBuf;

static void rxr_buf_free(RxBuf* b) {
    if (b->data != NULL) kmm_v4_free(b->data);
    b->data = NULL;
    b->len = 0;
    b->cap = 0;
}

static int rxr_buf_reserve(RxBuf* b, size_t extra) {
    if (b->oom) return 0;
    if (b->len + extra + 1 <= b->cap) return 1;
    {
        size_t ncap = b->cap ? b->cap : 64;
        char* n;
        while (ncap < b->len + extra + 1) ncap *= 2;
        n = (char*)kmm_v4_malloc(ncap);
        if (n == NULL) {
            b->oom = true;
            return 0;
        }
        if (b->data != NULL) {
            memcpy(n, b->data, b->len);
            kmm_v4_free(b->data);
        }
        b->data = n;
        b->cap = ncap;
    }
    return 1;
}

static int rxr_buf_put(RxBuf* b, const char* p, size_t n) {
    if (n == 0) return 1;
    if (!rxr_buf_reserve(b, n)) return 0;
    memcpy(b->data + b->len, p, n);
    b->len += n;
    b->data[b->len] = '\0';
    return 1;
}

static int rxr_buf_putc(RxBuf* b, char c) {
    if (!rxr_buf_reserve(b, 1)) return 0;
    b->data[b->len++] = c;
    b->data[b->len] = '\0';
    return 1;
}

// 按组号取一次匹配的文本并写入 buf。g < 0 表示整体匹配（$&）。
static void rxr_expand_group(RxrProgram* pr, RxBuf* buf, const char* s, long long g) {
    long long a;
    long long b;
    if (g < 0) {
        a = pr->caps[0];
        b = pr->caps[1];
    } else if ((size_t)g * 2 + 1 < pr->caps_len) {
        a = pr->caps[2 * (size_t)g];
        b = pr->caps[2 * (size_t)g + 1];
    } else {
        return;
    }
    // 未参与匹配的组展开为空串
    if (a < 0 || b < 0 || b < a) return;
    rxr_buf_put(buf, s + (size_t)a, (size_t)(b - a));
}

// 按名字查组号；未找到返回 -1
static long long rxr_find_group_by_name(RxrProgram* pr, const char* name, size_t nlen) {
    char tmp[REGEX_MAX_NAME_LEN];
    size_t gi;
    if (nlen == 0 || nlen >= sizeof(tmp)) return -1;
    memcpy(tmp, name, nlen);
    tmp[nlen] = '\0';
    for (gi = 1; gi <= pr->group_count; gi++) {
        if (pr->group_names[gi][0] != '\0' && strcmp(pr->group_names[gi], tmp) == 0) {
            return (long long)gi;
        }
    }
    return -1;
}

// 展开整个替换串。支持 $0..$9 / $& / ${name} / $$
static void rxr_expand_repl(RxrProgram* pr, RxBuf* buf, const char* s, const char* rep) {
    const char* r = rep;
    while (*r != '\0') {
        if (*r == '$' && r[1] != '\0') {
            const char* q = r + 1;
            if (*q == '$') {
                rxr_buf_putc(buf, '$');
                r = q + 1;
                continue;
            }
            if (*q == '&') {
                rxr_expand_group(pr, buf, s, -1);
                r = q + 1;
                continue;
            }
            if (*q == '{') {
                const char* close = strchr(q + 1, '}');
                if (close != NULL) {
                    size_t nlen = (size_t)(close - q - 1);
                    // ${n} 内容全为数字时按组号解释，否则按组名。
                    // 少了这个分支，${1} 会被当成组名 "1" 去查具名组表，
                    // 查不到就退化成整体匹配，替换结果整段重复。
                    size_t d;
                    bool numeric = nlen > 0;
                    for (d = 0; d < nlen; d++) {
                        if (q[1 + d] < '0' || q[1 + d] > '9') {
                            numeric = false;
                            break;
                        }
                    }
                    if (numeric) {
                        long long g = 0;
                        for (d = 0; d < nlen; d++) g = g * 10 + (q[1 + d] - '0');
                        rxr_expand_group(pr, buf, s, g);
                    } else {
                        rxr_expand_group(pr, buf, s, rxr_find_group_by_name(pr, q + 1, nlen));
                    }
                    r = close + 1;
                    continue;
                }
            } else if (*q >= '0' && *q <= '9') {
                rxr_expand_group(pr, buf, s, (long long)(*q - '0'));
                r = q + 1;
                continue;
            }
        }
        rxr_buf_putc(buf, *r);
        r++;
    }
}

static String rxr_replace_range(RxrProgram* pr, const char* s, size_t len, const char* rep,
                               bool all) {
    RxBuf buf;
    size_t pos = 0;

    memset(&buf, 0, sizeof(buf));
    if (!rxr_buf_reserve(&buf, len)) return STRING_EMPTY;

    if (!rxr_search(pr, s, len, pos)) {
        // 无匹配：原样返回（string_wrap_n 接管 buf，长串零拷贝）
        rxr_buf_put(&buf, s, len);
        if (buf.oom) {
            rxr_buf_free(&buf);
            return STRING_EMPTY;
        }
        return string_wrap_n(buf.data, buf.len);
    }

    while (pos <= len) {
        size_t ms = (size_t)pr->caps[0];
        size_t me = (size_t)pr->caps[1];
        if (me < ms) break;
        rxr_buf_put(&buf, s + pos, ms - pos);
        rxr_expand_repl(pr, &buf, s, rep);
        // 零宽匹配必须前进一位，否则死循环
        pos = (me > ms) ? me : me + 1;
        if (!all) break;
        if (!rxr_search(pr, s, len, pos)) break;
    }
    if (pos <= len) rxr_buf_put(&buf, s + pos, len - pos);

    if (buf.oom) {
        rxr_buf_free(&buf);
        return STRING_EMPTY;
    }
    return string_wrap_n(buf.data, buf.len);
}


String regex_ref_replace(const RegexRef* regex, const char* str, const char* replacement) {
    RxrProgram* pr;
    if (regex == NULL || !regex->valid || str == NULL || replacement == NULL) {
        if (str != NULL) return string_create(str);
        return STRING_EMPTY;
    }
    pr = (RxrProgram*)regex->compiled;
    return rxr_replace_range(pr, str, strlen(str), replacement, true);
}

String regex_ref_replace_first(const RegexRef* regex, const char* str, const char* replacement) {
    RxrProgram* pr;
    if (regex == NULL || !regex->valid || str == NULL || replacement == NULL) {
        if (str != NULL) return string_create(str);
        return STRING_EMPTY;
    }
    pr = (RxrProgram*)regex->compiled;
    return rxr_replace_range(pr, str, strlen(str), replacement, false);
}

String* regex_ref_split(const RegexRef* regex, const char* str, size_t max_parts, size_t* count) {
    RegexMatchRef* ms;
    size_t n = 0;
    size_t i;
    size_t part_count = 0;
    size_t cap;
    String* parts;
    size_t pos = 0;

    if (count != NULL) *count = 0;
    if (regex == NULL || !regex->valid || str == NULL) return NULL;

    ms = regex_ref_find_all(regex, str, &n);
    cap = n + 2;
    parts = (String*)kmm_v4_malloc(sizeof(String) * cap);
    if (parts == NULL) {
        regex_ref_free_matches(ms, n);
        return NULL;
    }

    for (i = 0; i < n; i++) {
        if (max_parts != 0 && part_count + 1 >= max_parts) break;
        if (ms[i].start < pos) continue;
        parts[part_count++] = rxr_slice(str, (long long)pos, (long long)ms[i].start);
        pos = ms[i].end;
    }
    {
        size_t len = strlen(str);
        if (pos <= len) {
            parts[part_count++] = rxr_slice(str, (long long)pos, (long long)len);
        }
    }

    regex_ref_free_matches(ms, n);

    if (part_count == 0) {
        kmm_v4_free(parts);
        return NULL;
    }
    if (count != NULL) *count = part_count;
    return parts;
}

String regex_ref_quote(const char* str) {
    static const char meta[] = ".^$*+?()[]{}|\\/-# \t\n\r\f\v";
    RxBuf buf;
    const char* p;

    if (str == NULL) return STRING_EMPTY;
    memset(&buf, 0, sizeof(buf));
    if (!rxr_buf_reserve(&buf, strlen(str))) return STRING_EMPTY;

    for (p = str; *p != '\0'; p++) {
        if (strchr(meta, *p) != NULL) rxr_buf_putc(&buf, '\\');
        rxr_buf_putc(&buf, *p);
    }
    if (buf.oom) {
        rxr_buf_free(&buf);
        return STRING_EMPTY;
    }
    return string_wrap_n(buf.data, buf.len);
}

void regex_ref_free_matches(RegexMatchRef* matches, size_t count) {
    size_t i;
    if (matches == NULL) return;
    for (i = 0; i < count; i++) {
        if (matches[i].text.len > STRING_SSO_CAPACITY && matches[i].text.ptr != NULL) {
            kmm_v4_free(matches[i].text.ptr);
        }
    }
    kmm_v4_free(matches);
}

void regex_ref_free_strings(String* strings, size_t count) {
    size_t i;
    if (strings == NULL) return;
    for (i = 0; i < count; i++) {
        if (strings[i].len > STRING_SSO_CAPACITY && strings[i].ptr != NULL) {
            kmm_v4_free(strings[i].ptr);
        }
    }
    kmm_v4_free(strings);
}

// --- 一次性便捷接口 --------------------------------------------------------

bool_t regex_ref_match_simple(const char* pattern, const char* str) {
    RegexRef* re = regex_ref_create(pattern);
    bool_t r;
    if (re == NULL) return false;
    r = regex_ref_match(re, str);
    regex_ref_destroy(re);
    return r;
}

bool_t regex_ref_search_simple(const char* pattern, const char* str) {
    RegexRef* re = regex_ref_create(pattern);
    bool_t r;
    if (re == NULL) return false;
    r = regex_ref_search(re, str);
    regex_ref_destroy(re);
    return r;
}

String regex_ref_replace_simple(const char* pattern, const char* str, const char* replacement) {
    RegexRef* re = regex_ref_create(pattern);
    String r;
    if (re == NULL) return STRING_EMPTY;
    r = regex_ref_replace(re, str, replacement);
    regex_ref_destroy(re);
    return r;
}

RegexMatchRef* regex_ref_find_simple(const char* pattern, const char* str, size_t* count) {
    RegexRef* re = regex_ref_create(pattern);
    RegexMatchRef* r;
    if (re == NULL) {
        if (count != NULL) *count = 0;
        return NULL;
    }
    r = regex_ref_find_all(re, str, count);
    regex_ref_destroy(re);
    return r;
}