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
//   「放弃并报错」，regex_aborted() 可以区分「无匹配」与「超预算放弃」。
//
// 为什么不用递归写匹配器：像 `a*` 匹配十万字符的串，递归深度会顶爆 C 栈。
// 这里改成显式回溯栈，深度只受堆内存限制。
// ============================================================================

#include "regex.h"

#include "../memory/memory.h"

#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

// 单个模式的指令数上限（防止 {n,m} 展开把程序撑爆）
#define RX_MAX_INST 8192
// 错误描述长度
#define RX_ERR_LEN 192

// ============================================================================
// 1. 256 位字符集工具
// ============================================================================

static void rx_bs_set(unsigned char* b, int c) {
    b[(c >> 3) & 31] |= (unsigned char)(1u << (c & 7));
}

static int rx_bs_get(const unsigned char* b, int c) {
    return (b[(c >> 3) & 31] >> (c & 7)) & 1;
}

static void rx_bs_range(unsigned char* b, int lo, int hi) {
    int i;
    for (i = lo; i <= hi; i++) rx_bs_set(b, i);
}

static void rx_bs_or(unsigned char* dst, const unsigned char* src) {
    int i;
    for (i = 0; i < 32; i++) dst[i] |= src[i];
}

static void rx_bs_not(unsigned char* b) {
    int i;
    for (i = 0; i < 32; i++) b[i] = (unsigned char)~b[i];
}

static void rx_bs_clear(unsigned char* b) {
    memset(b, 0, 32);
}

// \d / \w / \s 三族简写类
static void rx_bs_named(unsigned char* b, int c) {
    rx_bs_clear(b);
    switch (c) {
        case 'd':
            rx_bs_range(b, '0', '9');
            break;
        case 'w':
            rx_bs_range(b, 'a', 'z');
            rx_bs_range(b, 'A', 'Z');
            rx_bs_range(b, '0', '9');
            rx_bs_set(b, '_');
            break;
        case 's':
            rx_bs_set(b, ' ');
            rx_bs_set(b, '\t');
            rx_bs_set(b, '\n');
            rx_bs_set(b, '\r');
            rx_bs_set(b, '\f');
            rx_bs_set(b, '\v');
            break;
        default:
            break;
    }
}

// POSIX 字符类 [:name:]
static int rx_bs_posix(unsigned char* b, const char* name) {
    rx_bs_clear(b);
    if (strcmp(name, "alpha") == 0) {
        rx_bs_range(b, 'a', 'z');
        rx_bs_range(b, 'A', 'Z');
    } else if (strcmp(name, "digit") == 0) {
        rx_bs_range(b, '0', '9');
    } else if (strcmp(name, "alnum") == 0) {
        rx_bs_range(b, 'a', 'z');
        rx_bs_range(b, 'A', 'Z');
        rx_bs_range(b, '0', '9');
    } else if (strcmp(name, "upper") == 0) {
        rx_bs_range(b, 'A', 'Z');
    } else if (strcmp(name, "lower") == 0) {
        rx_bs_range(b, 'a', 'z');
    } else if (strcmp(name, "space") == 0) {
        rx_bs_set(b, ' ');
        rx_bs_set(b, '\t');
        rx_bs_set(b, '\n');
        rx_bs_set(b, '\r');
        rx_bs_set(b, '\f');
        rx_bs_set(b, '\v');
    } else if (strcmp(name, "blank") == 0) {
        rx_bs_set(b, ' ');
        rx_bs_set(b, '\t');
    } else if (strcmp(name, "punct") == 0) {
        rx_bs_range(b, '!', '/');
        rx_bs_range(b, ':', '@');
        rx_bs_range(b, '[', '`');
        rx_bs_range(b, '{', '~');
    } else if (strcmp(name, "print") == 0) {
        rx_bs_range(b, ' ', '~');
    } else if (strcmp(name, "graph") == 0) {
        rx_bs_range(b, '!', '~');
    } else if (strcmp(name, "cntrl") == 0) {
        rx_bs_range(b, 0, 31);
        rx_bs_set(b, 127);
    } else if (strcmp(name, "xdigit") == 0) {
        rx_bs_range(b, '0', '9');
        rx_bs_range(b, 'a', 'f');
        rx_bs_range(b, 'A', 'F');
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

#define RX_ARENA_CHUNK_SIZE 4096
// 头部补齐到 16 字节，保证节点数据区 16 字节对齐
#define RX_ARENA_HDR ((sizeof(RxArenaChunk) + 15u) & ~(size_t)15u)

typedef struct {
    RxArenaChunk* head;
} RxArena;

static void* rx_arena_alloc(RxArena* a, size_t n) {
    n = (n + 15u) & ~(size_t)15u;
    if (n == 0) n = 16;
    if (a->head == NULL || a->head->cap - a->head->used < n) {
        size_t cap = RX_ARENA_CHUNK_SIZE;
        RxArenaChunk* c;
        if (cap < n) cap = n;
        c = (RxArenaChunk*)kmm_v4_malloc(RX_ARENA_HDR + cap);
        if (c == NULL) return NULL;
        c->next = a->head;
        c->used = 0;
        c->cap = cap;
        a->head = c;
    }
    {
        char* base = (char*)a->head + RX_ARENA_HDR;
        void* p = base + a->head->used;
        a->head->used += n;
        return p;
    }
}

static void rx_arena_free(RxArena* a) {
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

typedef struct RxNode {
    RxNodeType type;
    char ch;               // RXN_CHAR
    unsigned char set[32]; // RXN_CLASS
    struct RxNode* a;
    struct RxNode* b;
    int min;
    int max;               // RXN_REPEAT；<0 表示无上界
    bool_t lazy;           // RXN_REPEAT：惰性
    int group;             // RXN_GROUP：组号（1 起）
    u32 flags;             // 建节点时的生效标志（内联开关靠它逐节点生效）
} RxNode;

typedef struct {
    const char* pat;
    size_t pos;
    size_t len;
    RxArena arena;
    u32 flags;
    int group_count;
    char group_names[REGEX_MAX_GROUPS][REGEX_MAX_NAME_LEN];
    char err[RX_ERR_LEN];
} RxParser;

static RxNode* rx_parse_alt(RxParser* p);

static void rx_fail(RxParser* p, const char* fmt, ...) {
    va_list ap;
    if (p->err[0] != '\0') return; // 只保留第一个错误
    va_start(ap, fmt);
    vsnprintf(p->err, sizeof(p->err), fmt, ap);
    va_end(ap);
}

static RxNode* rx_node(RxParser* p, RxNodeType type) {
    RxNode* n = (RxNode*)rx_arena_alloc(&p->arena, sizeof(RxNode));
    if (n == NULL) {
        rx_fail(p, "内存不足");
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

static void rx_skip_ws(RxParser* p) {
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

static int rx_peek(RxParser* p) {
    rx_skip_ws(p);
    if (p->pos >= p->len) return -1;
    return (int)(unsigned char)p->pat[p->pos];
}

static int rx_peek_raw(RxParser* p) {
    if (p->pos >= p->len) return -1;
    return (int)(unsigned char)p->pat[p->pos];
}

static int rx_take(RxParser* p) {
    int c;
    rx_skip_ws(p);
    if (p->pos >= p->len) return -1;
    c = (int)(unsigned char)p->pat[p->pos];
    p->pos++;
    return c;
}

static int rx_take_raw(RxParser* p) {
    int c;
    if (p->pos >= p->len) return -1;
    c = (int)(unsigned char)p->pat[p->pos];
    p->pos++;
    return c;
}

static int rx_is_alnum(int c) {
    return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z');
}

// \xHH —— 解析十六进制转义；失败返回 -1
static int rx_hex(RxParser* p) {
    int v = 0;
    int i;
    for (i = 0; i < 2; i++) {
        int c;
        if (p->pos >= p->len) {
            rx_fail(p, "\\x 后缺少两位十六进制数字");
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
            rx_fail(p, "\\x 后的 '%c' 不是十六进制数字", c);
            return -1;
        }
        p->pos++;
    }
    return v & 0xff;
}

// 单字符转义的字面量值（不含 \d 一类简写类与 \b）
static int rx_escape_char(RxParser* p, int e) {
    switch (e) {
        case 'n': return '\n';
        case 't': return '\t';
        case 'r': return '\r';
        case 'f': return '\f';
        case 'v': return '\v';
        case 'a': return '\a';
        case 'e': return 0x1b;
        case '0': return '\0';
        case 'x': return rx_hex(p);
        default: return e;
    }
}

// 字符类内部：读一个元素。简写类直接并入 set 并返回 -1；
// 否则返回字符码点。
static int rx_class_elem(RxParser* p, unsigned char* set) {
    int c = rx_take_raw(p);
    if (c < 0) return -2; // 串尾
    if (c != '\\') return c;
    {
        int e = rx_take_raw(p);
        if (e < 0) {
            rx_fail(p, "字符类中反斜杠后无内容");
            return -2;
        }
        if (e == 'd' || e == 'w' || e == 's') {
            rx_bs_named(set, e);
            return -1;
        }
        if (e == 'D' || e == 'W' || e == 'S') {
            unsigned char tmp[32];
            rx_bs_named(tmp, (e == 'D') ? 'd' : (e == 'W' ? 'w' : 's'));
            rx_bs_not(tmp);
            rx_bs_or(set, tmp);
            return -1;
        }
        return rx_escape_char(p, e);
    }
}

static RxNode* rx_parse_class(RxParser* p) {
    unsigned char set[32];
    int negated = 0;
    int first = 1;
    RxNode* n;

    rx_bs_clear(set);
    if (rx_peek_raw(p) == '^') {
        negated = 1;
        p->pos++;
    }
    for (;;) {
        int c = rx_peek_raw(p);
        int lo;
        if (c < 0) {
            rx_fail(p, "字符类缺少右方括号 ']'");
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
                if (!rx_bs_posix(set, name)) {
                    rx_fail(p, "未知的 POSIX 字符类 [:%s:]", name);
                    return NULL;
                }
                p->pos = q + 2;
                continue;
            }
        }

        lo = rx_class_elem(p, set);
        if (lo == -2) return NULL;
        if (lo < 0) continue; // 简写类已并入 set

        // 区间 lo-hi
        if (p->pos < p->len && p->pat[p->pos] == '-' && p->pos + 1 < p->len &&
            p->pat[p->pos + 1] != ']') {
            int hi;
            p->pos++; // 吃掉 '-'
            hi = rx_class_elem(p, set);
            if (hi == -2) return NULL;
            if (hi < 0) {
                rx_fail(p, "字符类区间端点不能是简写类");
                return NULL;
            }
            if (hi < lo) {
                rx_fail(p, "字符类区间逆序");
                return NULL;
            }
            rx_bs_range(set, lo, hi);
        } else {
            rx_bs_set(set, lo);
        }
    }

    if (negated) rx_bs_not(set);
    n = rx_node(p, RXN_CLASS);
    if (n == NULL) return NULL;
    memcpy(n->set, set, 32);
    return n;
}

// 字符类之外的转义 \x
static RxNode* rx_parse_escape(RxParser* p) {
    int e = rx_take(p);
    RxNode* n;

    if (e < 0) {
        rx_fail(p, "模式串以孤立的反斜杠结尾");
        return NULL;
    }
    if (e == 'd' || e == 'D' || e == 'w' || e == 'W' || e == 's' || e == 'S') {
        unsigned char set[32];
        int neg = (e == 'D' || e == 'W' || e == 'S');
        rx_bs_named(set, neg ? (e == 'D' ? 'd' : (e == 'W' ? 'w' : 's')) : e);
        if (neg) rx_bs_not(set);
        n = rx_node(p, RXN_CLASS);
        if (n == NULL) return NULL;
        memcpy(n->set, set, 32);
        return n;
    }
    switch (e) {
        case 'b': return rx_node(p, RXN_WORDB);
        case 'B': return rx_node(p, RXN_NWORDB);
        // \A / \z 复用锚点节点（受 MULTILINE 影响的语义见头文件说明）
        case 'A': return rx_node(p, RXN_BOL);
        case 'z':
        case 'Z': return rx_node(p, RXN_EOL);
        default: break;
    }
    if (e == 'x') {
        int v = rx_hex(p);
        if (v < 0) return NULL;
        n = rx_node(p, RXN_CHAR);
        if (n == NULL) return NULL;
        n->ch = (char)v;
        return n;
    }
    if (rx_is_alnum(e)) {
        rx_fail(p, "未知转义序列 \\%c", e);
        return NULL;
    }
    n = rx_node(p, RXN_CHAR);
    if (n == NULL) return NULL;
    n->ch = (char)e;
    return n;
}

static RxNode* rx_parse_group(RxParser* p) {
    int capture = 1;
    int gidx = -1;
    char name[REGEX_MAX_NAME_LEN];
    int have_name = 0;
    RxNode* body;
    RxNode* n;

    name[0] = '\0';

    if (rx_peek(p) == '?') {
        int c1 = (p->pos + 1 < p->len) ? (int)(unsigned char)p->pat[p->pos + 1] : -1;

        if (c1 == ':') {
            capture = 0;
            p->pos += 2;
        } else if (c1 == '=' || c1 == '!') {
            rx_fail(p, "不支持环视断言 (?%c)", c1);
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
                rx_fail(p, "不支持环视断言");
                return NULL;
            }
            while (q < p->len && p->pat[q] != closer && nlen + 1 < sizeof(name)) {
                name[nlen++] = p->pat[q++];
            }
            name[nlen] = '\0';
            if (q >= p->len || p->pat[q] != closer || nlen == 0) {
                rx_fail(p, "具名组语法错误（需要 (?<name>...) 或 (?'name'...)）");
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
                rx_fail(p, "无法识别的分组语法 '(?...'");
                return NULL;
            }
            if (p->pat[q] == ')') {
                // (?flags) 不是分组，只是设置开关后继续解析余下模式。
                // 这里必须直接返回空节点：若让控制流落到下面按普通分组处理，
                // '(' 已经被消费掉，后续 rx_parse_alt 会一路读到模式串末尾，
                // 报出误导性的「分组缺少右括号」。
                p->pos = q + 1;
                p->flags = f;
                return rx_node(p, RXN_EMPTY);
            } else {
                // (?flags: —— 只作用到本组内
                u32 outer = p->flags;
                p->pos = q + 1;
                p->flags = f;
                body = rx_parse_alt(p);
                p->flags = outer;
                if (body == NULL) return NULL;
                if (rx_peek(p) != ')') {
                    rx_fail(p, "分组缺少右括号 ')'");
                    return NULL;
                }
                p->pos++;
                return body;
            }
        }
    }

    if (capture) {
        if (p->group_count + 1 >= REGEX_MAX_GROUPS) {
            rx_fail(p, "捕获组数量超过上限 %d", REGEX_MAX_GROUPS - 1);
            return NULL;
        }
        gidx = ++p->group_count;
        if (have_name) {
            memcpy(p->group_names[gidx], name, strlen(name) + 1);
        }
    }

    body = rx_parse_alt(p);
    if (body == NULL) return NULL;
    if (rx_peek(p) != ')') {
        rx_fail(p, "分组缺少右括号 ')'");
        return NULL;
    }
    p->pos++;

    if (!capture) return body;
    n = rx_node(p, RXN_GROUP);
    if (n == NULL) return NULL;
    n->a = body;
    n->group = gidx;
    return n;
}

// 尝试解析 {n} / {n,} / {n,m}。返回 1 成功（已推进 pos），
// 0 表示不是合法的重复次数（'{' 留给上层当字面量），并可能已写入错误。
static int rx_try_bounds(RxParser* p, int* pmin, int* pmax) {
    size_t i = p->pos + 1; // 跳过 '{'
    long lo = 0;
    long hi;
    int digits = 0;

    if (i >= p->len || p->pat[i] < '0' || p->pat[i] > '9') return 0;
    while (i < p->len && p->pat[i] >= '0' && p->pat[i] <= '9') {
        lo = lo * 10 + (p->pat[i] - '0');
        if (lo > 65535) {
            rx_fail(p, "重复次数过大");
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
                    rx_fail(p, "重复次数过大");
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
        rx_fail(p, "重复次数上界小于下界");
        return 0;
    }
    p->pos = i + 1;
    *pmin = (int)lo;
    *pmax = (int)hi;
    return 1;
}

static RxNode* rx_parse_atom(RxParser* p) {
    int c = rx_take(p);
    RxNode* n;

    switch (c) {
        case -1:
            return rx_node(p, RXN_EMPTY);
        case '(':
            return rx_parse_group(p);
        case '[':
            return rx_parse_class(p);
        case '.':
            return rx_node(p, RXN_ANY);
        case '^':
            return rx_node(p, RXN_BOL);
        case '$':
            return rx_node(p, RXN_EOL);
        case '*':
        case '+':
        case '?':
            rx_fail(p, "量词 '%c' 前面没有可重复的项", c);
            return NULL;
        case '\\':
            return rx_parse_escape(p);
        default:
            n = rx_node(p, RXN_CHAR);
            if (n == NULL) return NULL;
            n->ch = (char)c;
            return n;
    }
}

static RxNode* rx_parse_repeat(RxParser* p) {
    RxNode* atom = rx_parse_atom(p);
    if (atom == NULL) return NULL;

    for (;;) {
        int c = rx_peek(p);
        int min;
        int max;
        RxNode* rep;

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
        } else if (c == '{' && rx_try_bounds(p, &min, &max)) {
            /* rx_try_bounds 已推进 pos */
        } else {
            if (p->err[0] != '\0') return NULL;
            break;
        }

        rep = rx_node(p, RXN_REPEAT);
        if (rep == NULL) return NULL;
        rep->a = atom;
        rep->min = min;
        rep->max = max;
        if (rx_peek(p) == '?') {
            rep->lazy = true;
            p->pos++;
        }
        atom = rep;
    }
    return atom;
}

static RxNode* rx_parse_concat(RxParser* p) {
    RxNode* cat = NULL;
    for (;;) {
        int c = rx_peek(p);
        RxNode* item;
        if (c < 0 || c == '|' || c == ')') break;
        item = rx_parse_repeat(p);
        if (item == NULL) return NULL;
        if (cat == NULL) {
            cat = item;
        } else {
            RxNode* n = rx_node(p, RXN_CAT);
            if (n == NULL) return NULL;
            n->a = cat;
            n->b = item;
            cat = n;
        }
    }
    if (cat == NULL) cat = rx_node(p, RXN_EMPTY);
    return cat;
}

static RxNode* rx_parse_alt(RxParser* p) {
    RxNode* left = rx_parse_concat(p);
    if (left == NULL) return NULL;
    for (;;) {
        if (rx_peek(p) != '|') break;
        p->pos++;
        {
            RxNode* right = rx_parse_concat(p);
            RxNode* n;
            if (right == NULL) return NULL;
            n = rx_node(p, RXN_ALT);
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
    RX_OP_CHAR = 0,
    RX_OP_ANY,
    RX_OP_CLASS,
    RX_OP_SPLIT,
    RX_OP_JMP,
    RX_OP_SAVE,
    RX_OP_MATCH,
    RX_OP_BOL,
    RX_OP_EOL,
    RX_OP_WORDB,
    RX_OP_NWORDB
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
} RxInst;

// 回溯帧：kind=0 是 SPLIT 的备选分支，kind=1 是捕获槽回滚
typedef struct {
    int kind;
    int pc;
    size_t pos;
    int slot;
    long long old;
} RxFrame;

typedef struct {
    RxInst* code;
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

    RxFrame* stack;
    size_t stack_len;
    size_t stack_cap;

    RxArena arena;
    char err[RX_ERR_LEN];
} RxProgram;

static void rx_pr_fail(RxProgram* pr, const char* fmt, ...) {
    va_list ap;
    if (pr->err[0] != '\0') return;
    va_start(ap, fmt);
    vsnprintf(pr->err, sizeof(pr->err), fmt, ap);
    va_end(ap);
}

static int rx_emit(RxProgram* pr, int op) {
    RxInst* in;
    if (pr->oom) return 0;
    if (pr->code_len >= RX_MAX_INST) {
        rx_pr_fail(pr, "模式过于复杂（指令数超过 %d）", RX_MAX_INST);
        pr->oom = true;
        return 0;
    }
    if (pr->code_len == pr->code_cap) {
        size_t ncap = pr->code_cap ? pr->code_cap * 2 : 64;
        RxInst* n = (RxInst*)kmm_v4_malloc(ncap * sizeof(RxInst));
        if (n == NULL) {
            rx_pr_fail(pr, "内存不足");
            pr->oom = true;
            return 0;
        }
        if (pr->code != NULL) {
            memcpy(n, pr->code, pr->code_len * sizeof(RxInst));
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

static void rx_emit_node(RxProgram* pr, RxNode* n);

// 让 sp 这条 SPLIT 优先走 first_trial
static void rx_set_split(RxProgram* pr, int sp, int first_trial, int second_trial,
                         bool_t lazy) {
    if (lazy) {
        pr->code[sp].x = second_trial;
        pr->code[sp].y = first_trial;
    } else {
        pr->code[sp].x = first_trial;
        pr->code[sp].y = second_trial;
    }
}

static void rx_emit_repeat(RxProgram* pr, RxNode* n) {
    int i;
    int min = n->min;
    int max = n->max;

    if (max < 0) {
        // 无上界：必须是「先分裂、再消费」的形状。
        // 早先把 SPLIT 放在 body 之后，`a*` 匹配 "b" 时 body 消费失败，
        // 而跳过分支在 body 后面根本走不到 —— 空匹配直接丢。
        if (min == 0) {
            // L: SPLIT(body, OUT); body; JMP L; OUT:
            int sp = rx_emit(pr, RX_OP_SPLIT);
            int body;
            int jmp;
            int out;
            if (pr->oom) return;
            body = (int)pr->code_len;
            rx_emit_node(pr, n->a);
            jmp = rx_emit(pr, RX_OP_JMP);
            out = (int)pr->code_len;
            if (pr->oom) return;
            pr->code[jmp].x = sp;
            rx_set_split(pr, sp, body, out, n->lazy);
        } else {
            // 先铺 min-1 份，再 L: body; SPLIT(L, OUT)
            for (i = 0; i < min - 1; i++) {
                rx_emit_node(pr, n->a);
                if (pr->oom) return;
            }
            {
                int body = (int)pr->code_len;
                int sp;
                int out;
                rx_emit_node(pr, n->a);
                sp = rx_emit(pr, RX_OP_SPLIT);
                out = (int)pr->code_len;
                if (pr->oom) return;
                rx_set_split(pr, sp, body, out, n->lazy);
            }
        }
        return;
    }

    // 必重复部分
    for (i = 0; i < min; i++) {
        rx_emit_node(pr, n->a);
        if (pr->oom) return;
    }
    // 可选部分：嵌套的 SPLIT，每层跳到本层之后
    for (i = min; i < max; i++) {
        int sp = rx_emit(pr, RX_OP_SPLIT);
        int bstart;
        int after;
        if (pr->oom) return;
        bstart = sp + 1;
        rx_emit_node(pr, n->a);
        after = (int)pr->code_len;
        if (pr->oom) return;
        rx_set_split(pr, sp, bstart, after, n->lazy);
    }
}

static void rx_emit_node(RxProgram* pr, RxNode* n) {
    int i;
    if (n == NULL || pr->oom) return;
    switch (n->type) {
        case RXN_EMPTY:
            break;
        case RXN_CHAR:
            i = rx_emit(pr, RX_OP_CHAR);
            if (i >= 0) {
                pr->code[i].ch = n->ch;
                pr->code[i].iflags = n->flags;
            }
            break;
        case RXN_ANY:
            i = rx_emit(pr, RX_OP_ANY);
            if (i >= 0) pr->code[i].iflags = n->flags;
            break;
        case RXN_CLASS:
            i = rx_emit(pr, RX_OP_CLASS);
            if (i >= 0) memcpy(pr->code[i].set, n->set, 32);
            break;
        case RXN_BOL:
            i = rx_emit(pr, RX_OP_BOL);
            if (i >= 0) pr->code[i].iflags = n->flags;
            break;
        case RXN_EOL:
            i = rx_emit(pr, RX_OP_EOL);
            if (i >= 0) pr->code[i].iflags = n->flags;
            break;
        case RXN_WORDB:
            rx_emit(pr, RX_OP_WORDB);
            break;
        case RXN_NWORDB:
            rx_emit(pr, RX_OP_NWORDB);
            break;
        case RXN_CAT:
            rx_emit_node(pr, n->a);
            rx_emit_node(pr, n->b);
            break;
        case RXN_ALT: {
            int sp = rx_emit(pr, RX_OP_SPLIT);
            int jend;
            int bstart;
            int end;
            rx_emit_node(pr, n->a);
            jend = rx_emit(pr, RX_OP_JMP);
            bstart = (int)pr->code_len;
            rx_emit_node(pr, n->b);
            end = (int)pr->code_len;
            if (pr->oom) return;
            pr->code[sp].x = sp + 1;
            pr->code[sp].y = bstart;
            pr->code[jend].x = end;
            break;
        }
        case RXN_GROUP:
            i = rx_emit(pr, RX_OP_SAVE);
            if (i >= 0) pr->code[i].slot = n->group * 2;
            rx_emit_node(pr, n->a);
            i = rx_emit(pr, RX_OP_SAVE);
            if (i >= 0) pr->code[i].slot = n->group * 2 + 1;
            break;
        case RXN_REPEAT:
            rx_emit_repeat(pr, n);
            break;
        default:
            break;
    }
}

// ============================================================================
// 5. 回溯虚拟机
// ============================================================================

static unsigned char rx_lower(unsigned char c) {
    return (c >= 'A' && c <= 'Z') ? (unsigned char)(c + 32) : c;
}

static int rx_char_eq(unsigned char a, unsigned char b, int icase) {
    if (a == b) return 1;
    if (!icase) return 0;
    return rx_lower(a) == rx_lower(b);
}

static int rx_is_word(int c) {
    return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
           c == '_';
}

static int rx_push(RxProgram* pr, int kind, int pc, size_t pos, int slot, long long old) {
    if (pr->stack_len == pr->stack_cap) {
        size_t ncap = pr->stack_cap ? pr->stack_cap * 2 : 128;
        RxFrame* n = (RxFrame*)kmm_v4_malloc(ncap * sizeof(RxFrame));
        if (n == NULL) return 0;
        if (pr->stack != NULL) {
            memcpy(n, pr->stack, pr->stack_len * sizeof(RxFrame));
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
static bool_t rx_run(RxProgram* pr, const char* s, size_t len, size_t start) {
    int pc = 0;
    size_t pos = start;

    pr->stack_len = 0;
    pr->match_end = start;

    for (;;) {
        const RxInst* in;
        if (pr->step_limit != 0 && ++pr->steps > pr->step_limit) {
            pr->aborted = true;
            break;
        }
        in = &pr->code[pc];
        switch (in->op) {
            case RX_OP_CHAR:
                if (pos < len &&
                    rx_char_eq((unsigned char)s[pos], (unsigned char)in->ch,
                               (in->iflags & REGEX_FLAG_IGNORECASE) != 0)) {
                    pc++;
                    pos++;
                } else {
                    goto backtrack;
                }
                break;
            case RX_OP_ANY:
                if (pos < len && ((in->iflags & REGEX_FLAG_DOTALL) || s[pos] != '\n')) {
                    pc++;
                    pos++;
                } else {
                    goto backtrack;
                }
                break;
            case RX_OP_CLASS:
                if (pos < len && rx_bs_get(in->set, (int)(unsigned char)s[pos])) {
                    pc++;
                    pos++;
                } else {
                    goto backtrack;
                }
                break;
            case RX_OP_SPLIT:
                if (!rx_push(pr, 0, in->lazy ? in->x : in->y, pos, 0, 0)) {
                    pr->aborted = true;
                    goto done;
                }
                pc = in->lazy ? in->y : in->x;
                break;
            case RX_OP_JMP:
                pc = in->x;
                break;
            case RX_OP_SAVE: {
                int slot = in->slot;
                if (slot < 0 || (size_t)slot >= pr->caps_len) {
                    pc++;
                    break;
                }
                if (!rx_push(pr, 1, 0, 0, slot, pr->caps[slot])) {
                    pr->aborted = true;
                    goto done;
                }
                pr->caps[slot] = (long long)pos;
                pc++;
                break;
            }
            case RX_OP_BOL:
                if (pos == 0 ||
                    ((in->iflags & REGEX_FLAG_MULTILINE) && pos > 0 && s[pos - 1] == '\n')) {
                    pc++;
                } else {
                    goto backtrack;
                }
                break;
            case RX_OP_EOL:
                if (pos == len ||
                    ((in->iflags & REGEX_FLAG_MULTILINE) && pos < len && s[pos] == '\n')) {
                    pc++;
                } else {
                    goto backtrack;
                }
                break;
            case RX_OP_WORDB:
            case RX_OP_NWORDB: {
                int before = (pos > 0) ? rx_is_word((int)(unsigned char)s[pos - 1]) : 0;
                int after = (pos < len) ? rx_is_word((int)(unsigned char)s[pos]) : 0;
                int boundary = (before != after);
                int want = (in->op == RX_OP_WORDB) ? boundary : !boundary;
                if (want) {
                    pc++;
                } else {
                    goto backtrack;
                }
                break;
            }
            case RX_OP_MATCH:
                pr->match_end = pos;
                pr->stack_len = 0;
                return true;
            default:
                goto done;
        }
        continue;

    backtrack:
        for (;;) {
            RxFrame f;
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

static void rx_caps_reset(RxProgram* pr) {
    size_t i;
    for (i = 0; i < pr->caps_len; i++) pr->caps[i] = -1;
    pr->steps = 0;
    pr->aborted = false;
}

// 每个候选起点都要重置步数：预算是「单次匹配」的上限，
// 而 rx_search 会从 from 到 len 逐点尝试，若不在每次尝试前清零，
// 长串上累计步数会早早越过预算，后续起点全被 aborted 短路成「无匹配」。
static void rx_caps_reset_try(RxProgram* pr) {
    size_t i;
    for (i = 0; i < pr->caps_len; i++) pr->caps[i] = -1;
    pr->steps = 0;
}

// 从 from 起做非锚定搜索（最左优先）
static bool_t rx_search(RxProgram* pr, const char* s, size_t len, size_t from) {
    size_t start;
    rx_caps_reset(pr);
    for (start = from; start <= len; start++) {
        rx_caps_reset_try(pr);
        if (rx_run(pr, s, len, start)) return true;
        if (pr->aborted) return false;
    }
    return false;
}

// 整串匹配
static bool_t rx_match_full(RxProgram* pr, const char* s, size_t len) {
    rx_caps_reset(pr);
    if (!rx_run(pr, s, len, 0)) return false;
    return pr->match_end == len;
}

// ============================================================================
// 6. 公开 API
// ============================================================================

static char* rx_strdup(const char* s) {
    size_t n = strlen(s);
    char* buf = (char*)kmm_v4_malloc(n + 1);
    if (buf == NULL) return NULL;
    memcpy(buf, s, n + 1);
    return buf;
}

static RxProgram* rx_prog_new(u32 flags) {
    RxProgram* pr = (RxProgram*)kmm_v4_malloc(sizeof(RxProgram));
    if (pr == NULL) return NULL;
    memset(pr, 0, sizeof(*pr));
    pr->flags = flags;
    pr->step_limit = REGEX_DEFAULT_STEP_LIMIT;
    return pr;
}

static void rx_prog_free(RxProgram* pr) {
    if (pr == NULL) return;
    rx_arena_free(&pr->arena);
    if (pr->code != NULL) kmm_v4_free(pr->code);
    if (pr->caps != NULL) kmm_v4_free(pr->caps);
    if (pr->stack != NULL) kmm_v4_free(pr->stack);
    kmm_v4_free(pr);
}

Regex* regex_create_ex(const char* pattern, u32 flags) {
    Regex* re;
    RxProgram* pr;
    RxParser p;
    RxNode* root;

    if (pattern == NULL) return NULL;

    re = (Regex*)kmm_v4_malloc(sizeof(Regex));
    if (re == NULL) return NULL;
    memset(re, 0, sizeof(Regex));

    pr = rx_prog_new(flags);
    if (pr == NULL) {
        kmm_v4_free(re);
        return NULL;
    }
    re->compiled = pr;

    re->pattern = rx_strdup(pattern);
    if (re->pattern == NULL) {
        rx_prog_free(pr);
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

    root = rx_parse_alt(&p);

    if (root == NULL && p.err[0] == '\0') {
        rx_fail(&p, "模式解析失败");
    }
    // 分组未闭合时解析停在中途，err 已记录；未记录则说明结构异常
    if (root != NULL && p.err[0] == '\0' && p.pos < p.len) {
        int c = p.pat[p.pos];
        if (c == ')') rx_fail(&p, "多余的右括号 ')'");
    }

    if (root == NULL || p.err[0] != '\0') {
        snprintf(pr->err, sizeof(pr->err), "%s", p.err[0] ? p.err : "模式解析失败");
        rx_arena_free(&p.arena);
        re->valid = false;
        return re;
    }

    // 整体匹配 0 号组：SAVE 0 ... SAVE 1 ... MATCH
    {
        int s0 = rx_emit(pr, RX_OP_SAVE);
        int s1;
        int m;
        if (s0 >= 0) pr->code[s0].slot = 0;
        rx_emit_node(pr, root);
        s1 = rx_emit(pr, RX_OP_SAVE);
        if (s1 >= 0) pr->code[s1].slot = 1;
        m = rx_emit(pr, RX_OP_MATCH);
        if (m < 0) {
            rx_pr_fail(pr, "模式过于复杂");
        }
    }

    rx_arena_free(&p.arena);

    if (pr->oom) {
        rx_pr_fail(pr, "模式过于复杂或内存不足");
    }

    pr->group_count = (size_t)p.group_count;
    memcpy(pr->group_names, p.group_names, sizeof(pr->group_names));

    pr->caps_len = (pr->group_count + 1) * 2;
    pr->caps = (long long*)kmm_v4_malloc(sizeof(long long) * (pr->caps_len ? pr->caps_len : 2));
    if (pr->caps == NULL) {
        rx_pr_fail(pr, "内存不足");
        re->valid = false;
        return re;
    }
    rx_caps_reset(pr);

    re->valid = (pr->err[0] == '\0');
    return re;
}

Regex* regex_create(const char* pattern) {
    return regex_create_ex(pattern, REGEX_FLAG_NONE);
}

void regex_destroy(Regex* regex) {
    if (regex == NULL) return;
    if (regex->compiled != NULL) rx_prog_free((RxProgram*)regex->compiled);
    if (regex->pattern != NULL) kmm_v4_free((char*)regex->pattern);
    regex->compiled = NULL;
    regex->pattern = NULL;
    kmm_v4_free(regex);
}

bool_t regex_is_valid(const Regex* regex) {
    if (regex == NULL) return false;
    return regex->valid;
}

const char* regex_error(const Regex* regex) {
    RxProgram* pr;
    if (regex == NULL || regex->compiled == NULL) return NULL;
    pr = (RxProgram*)regex->compiled;
    if (pr->err[0] == '\0') return NULL;
    return pr->err;
}

const char* regex_pattern(const Regex* regex) {
    if (regex == NULL) return NULL;
    return regex->pattern;
}

u32 regex_flags(const Regex* regex) {
    RxProgram* pr;
    if (regex == NULL || regex->compiled == NULL) return REGEX_FLAG_NONE;
    pr = (RxProgram*)regex->compiled;
    return pr->flags;
}

size_t regex_group_count(const Regex* regex) {
    RxProgram* pr;
    if (regex == NULL || regex->compiled == NULL) return 0;
    pr = (RxProgram*)regex->compiled;
    return pr->group_count;
}

const char* regex_group_name(const Regex* regex, size_t index) {
    RxProgram* pr;
    if (regex == NULL || regex->compiled == NULL) return NULL;
    pr = (RxProgram*)regex->compiled;
    if (index == 0 || index > pr->group_count) return NULL;
    if (pr->group_names[index][0] == '\0') return NULL;
    return pr->group_names[index];
}

i64 regex_group_index(const Regex* regex, const char* name) {
    RxProgram* pr;
    size_t i;
    if (regex == NULL || regex->compiled == NULL || name == NULL) return -1;
    pr = (RxProgram*)regex->compiled;
    for (i = 1; i <= pr->group_count; i++) {
        if (pr->group_names[i][0] != '\0' && strcmp(pr->group_names[i], name) == 0) {
            return (i64)i;
        }
    }
    return -1;
}

void regex_set_step_limit(Regex* regex, size_t limit) {
    RxProgram* pr;
    if (regex == NULL || regex->compiled == NULL) return;
    pr = (RxProgram*)regex->compiled;
    pr->step_limit = limit;
}

bool_t regex_aborted(const Regex* regex) {
    RxProgram* pr;
    if (regex == NULL || regex->compiled == NULL) return false;
    pr = (RxProgram*)regex->compiled;
    return pr->aborted;
}

bool_t regex_match(const Regex* regex, const char* str) {
    RxProgram* pr;
    if (regex == NULL || !regex->valid || str == NULL) return false;
    pr = (RxProgram*)regex->compiled;
    return rx_match_full(pr, str, strlen(str));
}

bool_t regex_full_match(const Regex* regex, const char* str) {
    return regex_match(regex, str);
}

bool_t regex_search(const Regex* regex, const char* str) {
    RxProgram* pr;
    if (regex == NULL || !regex->valid || str == NULL) return false;
    pr = (RxProgram*)regex->compiled;
    return rx_search(pr, str, strlen(str), 0);
}

bool_t regex_find_first(const Regex* regex, const char* str, size_t from, size_t* start,
                        size_t* end) {
    RxProgram* pr;
    size_t len;
    if (regex == NULL || !regex->valid || str == NULL) return false;
    pr = (RxProgram*)regex->compiled;
    len = strlen(str);
    if (from > len) return false;
    if (!rx_search(pr, str, len, from)) return false;
    if (start != NULL) *start = (size_t)pr->caps[0];
    if (end != NULL) *end = (size_t)pr->caps[1];
    return true;
}

static String rx_slice(const char* s, long long a, long long b) {
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

RegexMatch* regex_find_all(const Regex* regex, const char* str, size_t* count) {
    RxProgram* pr;
    size_t len;
    size_t cap = 8;
    size_t n = 0;
    size_t pos = 0;
    RegexMatch* out;

    if (count != NULL) *count = 0;
    if (regex == NULL || !regex->valid || str == NULL) return NULL;
    pr = (RxProgram*)regex->compiled;
    len = strlen(str);

    out = (RegexMatch*)kmm_v4_malloc(cap * sizeof(RegexMatch));
    if (out == NULL) return NULL;

    while (pos <= len) {
        if (!rx_search(pr, str, len, pos)) break;
        {
            long long s0 = pr->caps[0];
            long long e0 = pr->caps[1];
            if (e0 < s0) break;
            if (n == cap) {
                RegexMatch* bigger;
                cap *= 2;
                bigger = (RegexMatch*)kmm_v4_malloc(cap * sizeof(RegexMatch));
                if (bigger == NULL) {
                    regex_free_matches(out, n);
                    return NULL;
                }
                memcpy(bigger, out, n * sizeof(RegexMatch));
                kmm_v4_free(out);
                out = bigger;
            }
            out[n].start = (size_t)s0;
            out[n].end = (size_t)e0;
            out[n].text = rx_slice(str, s0, e0);
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

RegexMatch* regex_find(const Regex* regex, const char* str, size_t* count) {
    return regex_find_all(regex, str, count);
}

RegexCapture* regex_captures(const Regex* regex, const char* str, size_t* count) {
    RxProgram* pr;
    size_t len;
    size_t g;
    size_t n;
    RegexCapture* caps;

    if (count != NULL) *count = 0;
    if (regex == NULL || !regex->valid || str == NULL) return NULL;
    pr = (RxProgram*)regex->compiled;
    len = strlen(str);

    if (!rx_search(pr, str, len, 0)) return NULL;

    n = pr->group_count + 1;
    caps = (RegexCapture*)kmm_v4_malloc(n * sizeof(RegexCapture));
    if (caps == NULL) return NULL;

    for (g = 0; g < n; g++) {
        long long a = pr->caps[2 * g];
        long long b = pr->caps[2 * g + 1];
        int ok = (a >= 0 && b >= 0 && b >= a);
        caps[g].matched = ok ? true : false;
        caps[g].start = ok ? (size_t)a : 0;
        caps[g].end = ok ? (size_t)b : 0;
        caps[g].text = ok ? rx_slice(str, a, b) : STRING_EMPTY;
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

static void rx_buf_free(RxBuf* b) {
    if (b->data != NULL) kmm_v4_free(b->data);
    b->data = NULL;
    b->len = 0;
    b->cap = 0;
}

static int rx_buf_reserve(RxBuf* b, size_t extra) {
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

static int rx_buf_put(RxBuf* b, const char* p, size_t n) {
    if (n == 0) return 1;
    if (!rx_buf_reserve(b, n)) return 0;
    memcpy(b->data + b->len, p, n);
    b->len += n;
    b->data[b->len] = '\0';
    return 1;
}

static int rx_buf_putc(RxBuf* b, char c) {
    if (!rx_buf_reserve(b, 1)) return 0;
    b->data[b->len++] = c;
    b->data[b->len] = '\0';
    return 1;
}

// 按组号取一次匹配的文本并写入 buf。g < 0 表示整体匹配（$&）。
static void rx_expand_group(RxProgram* pr, RxBuf* buf, const char* s, long long g) {
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
    rx_buf_put(buf, s + (size_t)a, (size_t)(b - a));
}

// 按名字查组号；未找到返回 -1
static long long rx_find_group_by_name(RxProgram* pr, const char* name, size_t nlen) {
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
static void rx_expand_repl(RxProgram* pr, RxBuf* buf, const char* s, const char* rep) {
    const char* r = rep;
    while (*r != '\0') {
        if (*r == '$' && r[1] != '\0') {
            const char* q = r + 1;
            if (*q == '$') {
                rx_buf_putc(buf, '$');
                r = q + 1;
                continue;
            }
            if (*q == '&') {
                rx_expand_group(pr, buf, s, -1);
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
                        rx_expand_group(pr, buf, s, g);
                    } else {
                        rx_expand_group(pr, buf, s, rx_find_group_by_name(pr, q + 1, nlen));
                    }
                    r = close + 1;
                    continue;
                }
            } else if (*q >= '0' && *q <= '9') {
                rx_expand_group(pr, buf, s, (long long)(*q - '0'));
                r = q + 1;
                continue;
            }
        }
        rx_buf_putc(buf, *r);
        r++;
    }
}

static String rx_replace_range(RxProgram* pr, const char* s, size_t len, const char* rep,
                               bool all) {
    RxBuf buf;
    size_t pos = 0;

    memset(&buf, 0, sizeof(buf));
    if (!rx_buf_reserve(&buf, len)) return STRING_EMPTY;

    if (!rx_search(pr, s, len, pos)) {
        // 无匹配：原样返回（string_wrap_n 接管 buf，长串零拷贝）
        rx_buf_put(&buf, s, len);
        if (buf.oom) {
            rx_buf_free(&buf);
            return STRING_EMPTY;
        }
        return string_wrap_n(buf.data, buf.len);
    }

    while (pos <= len) {
        size_t ms = (size_t)pr->caps[0];
        size_t me = (size_t)pr->caps[1];
        if (me < ms) break;
        rx_buf_put(&buf, s + pos, ms - pos);
        rx_expand_repl(pr, &buf, s, rep);
        // 零宽匹配必须前进一位，否则死循环
        pos = (me > ms) ? me : me + 1;
        if (!all) break;
        if (!rx_search(pr, s, len, pos)) break;
    }
    if (pos <= len) rx_buf_put(&buf, s + pos, len - pos);

    if (buf.oom) {
        rx_buf_free(&buf);
        return STRING_EMPTY;
    }
    return string_wrap_n(buf.data, buf.len);
}


String regex_replace(const Regex* regex, const char* str, const char* replacement) {
    RxProgram* pr;
    if (regex == NULL || !regex->valid || str == NULL || replacement == NULL) {
        if (str != NULL) return string_create(str);
        return STRING_EMPTY;
    }
    pr = (RxProgram*)regex->compiled;
    return rx_replace_range(pr, str, strlen(str), replacement, true);
}

String regex_replace_first(const Regex* regex, const char* str, const char* replacement) {
    RxProgram* pr;
    if (regex == NULL || !regex->valid || str == NULL || replacement == NULL) {
        if (str != NULL) return string_create(str);
        return STRING_EMPTY;
    }
    pr = (RxProgram*)regex->compiled;
    return rx_replace_range(pr, str, strlen(str), replacement, false);
}

String* regex_split(const Regex* regex, const char* str, size_t max_parts, size_t* count) {
    RegexMatch* ms;
    size_t n = 0;
    size_t i;
    size_t part_count = 0;
    size_t cap;
    String* parts;
    size_t pos = 0;

    if (count != NULL) *count = 0;
    if (regex == NULL || !regex->valid || str == NULL) return NULL;

    ms = regex_find_all(regex, str, &n);
    cap = n + 2;
    parts = (String*)kmm_v4_malloc(sizeof(String) * cap);
    if (parts == NULL) {
        regex_free_matches(ms, n);
        return NULL;
    }

    for (i = 0; i < n; i++) {
        if (max_parts != 0 && part_count + 1 >= max_parts) break;
        if (ms[i].start < pos) continue;
        parts[part_count++] = rx_slice(str, (long long)pos, (long long)ms[i].start);
        pos = ms[i].end;
    }
    {
        size_t len = strlen(str);
        if (pos <= len) {
            parts[part_count++] = rx_slice(str, (long long)pos, (long long)len);
        }
    }

    regex_free_matches(ms, n);

    if (part_count == 0) {
        kmm_v4_free(parts);
        return NULL;
    }
    if (count != NULL) *count = part_count;
    return parts;
}

String regex_quote(const char* str) {
    static const char meta[] = ".^$*+?()[]{}|\\/-# \t\n\r\f\v";
    RxBuf buf;
    const char* p;

    if (str == NULL) return STRING_EMPTY;
    memset(&buf, 0, sizeof(buf));
    if (!rx_buf_reserve(&buf, strlen(str))) return STRING_EMPTY;

    for (p = str; *p != '\0'; p++) {
        if (strchr(meta, *p) != NULL) rx_buf_putc(&buf, '\\');
        rx_buf_putc(&buf, *p);
    }
    if (buf.oom) {
        rx_buf_free(&buf);
        return STRING_EMPTY;
    }
    return string_wrap_n(buf.data, buf.len);
}

void regex_free_matches(RegexMatch* matches, size_t count) {
    size_t i;
    if (matches == NULL) return;
    for (i = 0; i < count; i++) {
        if (matches[i].text.len > STRING_SSO_CAPACITY && matches[i].text.ptr != NULL) {
            kmm_v4_free(matches[i].text.ptr);
        }
    }
    kmm_v4_free(matches);
}

void regex_free_strings(String* strings, size_t count) {
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

bool_t regex_match_simple(const char* pattern, const char* str) {
    Regex* re = regex_create(pattern);
    bool_t r;
    if (re == NULL) return false;
    r = regex_match(re, str);
    regex_destroy(re);
    return r;
}

bool_t regex_search_simple(const char* pattern, const char* str) {
    Regex* re = regex_create(pattern);
    bool_t r;
    if (re == NULL) return false;
    r = regex_search(re, str);
    regex_destroy(re);
    return r;
}

String regex_replace_simple(const char* pattern, const char* str, const char* replacement) {
    Regex* re = regex_create(pattern);
    String r;
    if (re == NULL) return STRING_EMPTY;
    r = regex_replace(re, str, replacement);
    regex_destroy(re);
    return r;
}

RegexMatch* regex_find_simple(const char* pattern, const char* str, size_t* count) {
    Regex* re = regex_create(pattern);
    RegexMatch* r;
    if (re == NULL) {
        if (count != NULL) *count = 0;
        return NULL;
    }
    r = regex_find_all(re, str, count);
    regex_destroy(re);
    return r;
}