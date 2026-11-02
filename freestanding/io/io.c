// freestanding/io/io.c — 输入输出模块实现
// 无 <stdio.h>/libc 依赖：输出经 fs_output_putchar 弱钩子（默认空操作），
// 内置迷你 printf，与 libc printf 行为对齐：
//   转换符: %d %i %u %x %X %o %b %c %s %f %F %e %E %g %G %p %%
//   标志:   '-' 左对齐, '0' 零填充, '+' 正号, ' ' 空格, '#' 交替形式
//   宽度:   十进制数字或 '*'
//   精度:   '.' + 十进制数字 或 '.*'
//   长度修饰: l/ll/h/z/j/t/L 解析并忽略（整数按 64 位读取，与 Kaula i64 约定一致）
// 符号 FS_WEAK

#ifndef KAULA_FREESTANDING_IO_C
#define KAULA_FREESTANDING_IO_C

#include "io.h"
#include "../base/fs_common.h"
#include "../string/string.h"
#include "../math/math.h"
#include <stdarg.h>
#include <stdint.h>

// ==================== 输出钩子（默认空操作，用户可覆写为强符号） ====================

FS_WEAK void fs_output_putchar(char c) {
    (void)c; // 默认丢弃输出；裸机覆写为 UART，托管覆写为控制台
}

// ==================== 底层输出 ====================

FS_WEAK void fs_putchar(char c) {
    fs_output_putchar(c);
}

FS_WEAK void fs_puts(const char* s) {
    if (s == NULL) return;
    while (*s != 0) {
        fs_output_putchar(*s);
        s++;
    }
}

FS_WEAK void fs_putint(i64 value) {
    char buf[24];
    fs_puts(fs_itoa(value, buf));
}

FS_WEAK void fs_putuint(u64 value) {
    char buf[24];
    fs_puts(fs_utoa(value, buf));
}

FS_WEAK void fs_puthex_noprefix(u64 value) {
    char buf[20];
    fs_puts(fs_itoa_hex(value, buf, false));
}

FS_WEAK void fs_puthex(u64 value) {
    fs_puts("0x");
    fs_puthex_noprefix(value);
}

FS_WEAK void fs_putbin(u64 value) {
    fs_puts("0b");
    for (int i = 63; i >= 0; i--) {
        fs_output_putchar(((value >> i) & 1ULL) ? '1' : '0');
    }
}

// ==================== 格式化引擎（printf 兼容子集） ====================

#define FS_FMT_BUF   1024   /* 单个转换单元的组包缓冲 */
#define FS_FMT_MAXW  512    /* 宽度上限（printf 无上限，此处夹取避免缓冲越界） */
#define FS_SIG_MAX   17     /* double 可精确表示的十进制有效位上限（超出部分补 0） */

typedef struct {
    bool left_align;   /* '-' */
    bool zero_pad;     /* '0' */
    bool plus_sign;    /* '+' */
    bool space_sign;   /* ' ' */
    bool alt_form;     /* '#' */
    int  width;        /* 默认 0 */
    int  prec;         /* -1: 未指定 */
} fs_spec;

/* 组包缓冲：先把一个转换单元拼好（含宽度填充），再一次输出 */
static char fs_fbuf[FS_FMT_BUF];
static int  fs_flen;

static void fs_fclear(void) {
    fs_flen = 0;
    fs_fbuf[0] = '\0';
}

static void fs_fputc(char c) {
    if (fs_flen < FS_FMT_BUF - 1) fs_fbuf[fs_flen++] = c;
    fs_fbuf[fs_flen] = '\0';
}

static void fs_fputs(const char* s) {
    while (*s != 0) fs_fputc(*s++);
}

static void fs_fflush(void) {
    fs_puts(fs_fbuf);
    fs_fclear();
}

/* 输出 pre + body, 按标志/宽度填充
 *   left_align:  填充在 body 之后（空格）
 *   zero_pad:    填充在 pre 之后、body 之前（'0'），且 left_align 优先
 *   其他/无标志:  填充在 pre 之前（空格）
 */
static void fs_femit(const char* pre, const char* body, const fs_spec* sp) {
    int pre_len = (int)strlen(pre);
    int body_len = (int)strlen(body);
    int total = pre_len + body_len;
    int pad = sp->width > total ? sp->width - total : 0;
    if (pad > FS_FMT_MAXW) pad = FS_FMT_MAXW;

    if (sp->left_align) {
        fs_fputs(pre);
        fs_fputs(body);
        for (int i = 0; i < pad; i++) fs_fputc(' ');
    } else if (sp->zero_pad) {
        fs_fputs(pre);
        for (int i = 0; i < pad; i++) fs_fputc('0');
        fs_fputs(body);
    } else {
        for (int i = 0; i < pad; i++) fs_fputc(' ');
        fs_fputs(pre);
        fs_fputs(body);
    }
    fs_fflush();
}

// ==================== 整数格式化 ====================

/* u64 → 数字字符串（不含前缀/符号）, 返回长度, out 需 >= 70 字节 */
static int fs_u64_to_str(u64 mag, unsigned base, bool upper, char* out) {
    const char* table = upper ? "0123456789ABCDEF" : "0123456789abcdef";
    char rev[70];
    int n = 0;
    if (mag == 0) {
        rev[n++] = '0';
    } else {
        while (mag > 0 && n < (int)sizeof(rev)) {
            rev[n++] = table[mag % base];
            mag /= base;
        }
    }
    for (int i = 0; i < n; i++) out[i] = rev[n - 1 - i];
    out[n] = '\0';
    return n;
}

static void fs_emit_int(u64 mag, unsigned base, bool upper, bool negative, const fs_spec* sp) {
    char rev[72];
    int n = fs_u64_to_str(mag, base, upper, rev);

    /* 精度指定最小位数（不足前补 0） */
    int min_digits = (sp->prec > 0) ? sp->prec : 1;
    int lead = min_digits - n;
    if (lead < 0) lead = 0;

    /* 符号前缀（'-' / '+' / ' '） */
    char pre[4];
    int pre_len = 0;
    if (negative) pre[pre_len++] = '-';
    else if (sp->plus_sign) pre[pre_len++] = '+';
    else if (sp->space_sign) pre[pre_len++] = ' ';
    /* '#' 交替形式: 16/2 进制加前缀(值非 0 时), 8 进制首位已是 0 时不加 */
    if (sp->alt_form) {
        if (base == 16 && mag != 0) {
            pre[pre_len++] = '0';
            pre[pre_len++] = upper ? 'X' : 'x';
        } else if (base == 2 && mag != 0) {
            pre[pre_len++] = '0';
            pre[pre_len++] = 'b';
        } else if (base == 8 && rev[0] != '0') {
            pre[pre_len++] = '0';
        }
    }
    pre[pre_len] = '\0';

    /* body = 精度补零 + 数字 */
    char body[96];
    int bl = 0;
    for (int i = 0; i < lead && bl < (int)sizeof(body) - 1; i++) body[bl++] = '0';
    for (int i = 0; i < n && bl < (int)sizeof(body) - 1; i++) body[bl++] = rev[i];
    body[bl] = '\0';

    /* 精度已保证最小位数时, '0' 标志不再补零（与 libc 一致） */
    fs_spec sp2 = *sp;
    if (sp->prec >= 0) sp2.zero_pad = false;
    fs_femit(pre, body, &sp2);
}

// ==================== 浮点格式化 ====================

/* 归一化: 返回 e 使 mant ∈ [1, 10), 满足 v ≈ mant × 10^e
 * v <= 0 → 返回 0, *mant = 0
 */
static int fs_norm_exp(f64 v, f64* mant) {
    f64 a = v;
    int e = 0;
    if (a >= 1.0) {
        while (a >= 10.0) { a /= 10.0; e++; }
        while (a < 1.0) { a *= 10.0; e--; }
    } else if (a > 0.0) {
        while (a < 1.0) { a *= 10.0; e--; }
        while (a >= 10.0) { a /= 10.0; e++; }
    }
    *mant = a;
    return e;
}

/* 提取 |v| 的十进制有效数字（不做舍入）
 *   dig[0..ndig)  数字字符
 *   *sticky       第 ndig 位之后是否还有非零数字（决定 5.x 的舍入方向）
 *   *exp10        v = 0.dig[0]dig[1]... × 10^(*exp10)
 * 说明: 第 i 位数字对应 10^(exp10-1+i) 位; v == 0 → 全 '0', *exp10 = 0
 */
static void fs_extract(f64 v, int ndig, char* dig, bool* sticky, int* exp10) {
    int i;
    for (i = 0; i < ndig; i++) dig[i] = '0';
    *sticky = false;
    if (!(v > 0.0)) { *exp10 = 0; return; }

    f64 a = 0.0;
    int e = fs_norm_exp(v, &a);

    int n = 0;
    while (n < ndig) {
        int d = (int)a;
        if (d < 0) d = 0;
        if (d > 9) d = 9;
        dig[n++] = (char)('0' + d);
        a = (a - (f64)d) * 10.0;
        if (a <= 0.0) { *exp10 = e + 1; return; }   /* 剩余全零 */
    }
    *sticky = (a > 0.0);                              /* 尾部还有非零数字 */
    *exp10 = e + 1;
}

/* 在 dig 的下标 r 处按 guard + sticky 四舍五入（平局取偶）, 进位向高位传播
 * 最高位溢出时: dig = "1" + 全 0, *exp10++
 * 超出 ndig 的部分视为 0（double 有效位边界, 见 FS_SIG_MAX）
 */
static void fs_round_at(char* dig, int ndig, int r, char guard, bool sticky, int* exp10) {
    if (r < 0) return;                     /* 调用方自行处理前导零区 */
    if (r >= ndig) return;                 /* 打印位超出有效位: 视为 0, 不进位 */
    bool up = false;
    if (guard > '5') up = true;
    else if (guard == '5') {
        if (sticky) up = true;                         /* 严格大于半 */
        else if (((dig[r] - '0') & 1) != 0) up = true; /* 平局 → 取偶 */
    }
    if (!up) return;
    int j = r;
    while (j >= 0) {
        if (dig[j] != '9') { dig[j]++; return; }
        dig[j] = '0';
        j--;
    }
    for (int i = 0; i < ndig; i++) dig[i] = '0';
    dig[0] = '1';
    (*exp10)++;
}

/* 夹取精度到 double 可表示范围 */
static int fs_clamp_prec(int prec) {
    if (prec < 0) prec = 6;
    if (prec > FS_SIG_MAX - 2) prec = FS_SIG_MAX - 2;
    return prec;
}

/* 特殊值处理: 返回 true 表示已输出（非有限数） */
static bool fs_emit_special(f64 val, const fs_spec* sp, bool upper) {
    if (math_isnan(val)) {
        fs_fclear();
        fs_femit("", upper ? "NAN" : "nan", sp);
        return true;
    }
    if (math_isinf(val)) {
        const char* s = math_signbit(val) ? (upper ? "-INF" : "-inf")
                                         : (upper ? "INF" : "inf");
        fs_fclear();
        fs_femit("", s, sp);
        return true;
    }
    return false;
}

/* guard 位的取值与"其后是否还有非零位"（sticky 只在 guard 是最后一位提取位时生效） */
static char fs_guard_of(const char* dig, int ndig, int g, bool sticky, bool* out_sticky) {
    *out_sticky = false;
    if (g < 0) return '0';
    if (g >= ndig) return '0';            /* 超出有效位: 视作 0（FS_SIG_MAX 边界） */
    if (g == ndig - 1) *out_sticky = sticky;
    return dig[g];
}

/* %f / %F 定点格式 */
static void fs_format_fixed(f64 val, const fs_spec* sp, bool upper) {
    if (fs_emit_special(val, sp, upper)) return;

    int prec = fs_clamp_prec(sp->prec);
    bool negative = math_signbit(val);
    f64 a = math_abs(val);

    /* 小数点前位数（可为负: 0.001 → -2）与"最后打印位"对应的数字下标 */
    f64 mant = 0.0;
    int e = fs_norm_exp(a, &mant);
    int exp10 = e + 1;
    int r = prec + exp10 - 1;              /* 第 prec 位小数 ↔ dig[r] */

    int ndig;
    if (r >= 0) {
        ndig = r + 2;                      /* 打印位 + 1 位 guard */
    } else {
        ndig = 1;                          /* 全部落在前导零区, 只需首个有效数字 */
    }
    if (ndig > FS_SIG_MAX) ndig = FS_SIG_MAX;

    char dig[FS_SIG_MAX + 2];
    bool sticky = false;
    fs_extract(a, ndig, dig, &sticky, &exp10);

    char body[FS_FMT_BUF];
    int bl = 0;
    if (r < 0) {
        /* 打印位全部落在前导零区: 末位 guard 若为首个有效数字则决定是否进 1 */
        char lead = '0';
        if (r + 1 == 0) {
            bool gs = false;
            char gd = fs_guard_of(dig, ndig, 0, sticky, &gs);
            if (gd > '5') lead = '1';
            else if (gd == '5' && gs) lead = '1';
            /* 精确平局: 目标位是 0（偶数）→ 保持 0 */
        }
        body[bl++] = '0';                       /* exp10 <= 0 → 整数位为 0 */
        if (prec > 0) {
            body[bl++] = '.';
            for (int i = 1; i < prec; i++) body[bl++] = '0';
            body[bl++] = lead;
        } else if (sp->alt_form) {
            body[bl++] = '.';
        }
        body[bl] = '\0';
        goto emit;
    }

    {
        bool gs = false;
        char guard = fs_guard_of(dig, ndig, r + 1, sticky, &gs);
        fs_round_at(dig, ndig, r, guard, gs, &exp10);
    }

    /* 小数点前的整数位（exp10 可为负 → 只输出 "0"） */
    if (exp10 <= 0) {
        body[bl++] = '0';
    } else {
        for (int i = 0; i < exp10; i++)
            body[bl++] = (i < ndig) ? dig[i] : '0';
    }
    if (prec > 0) {
        body[bl++] = '.';
        for (int i = 0; i < prec; i++) {
            int idx = exp10 + i;
            body[bl++] = (idx >= 0 && idx < ndig) ? dig[idx] : '0';
        }
    } else if (sp->alt_form) {
        body[bl++] = '.';
    }
    body[bl] = '\0';

emit:
    {
        char pre[2];
        pre[0] = '\0';
        if (negative) pre[0] = '-';
        else if (sp->plus_sign) pre[0] = '+';
        else if (sp->space_sign) pre[0] = ' ';
        pre[1] = '\0';
        fs_femit(pre, body, sp);
    }
}

/* %e / %E 科学计数格式 */
static void fs_format_exp(f64 val, const fs_spec* sp, bool upper) {
    if (fs_emit_special(val, sp, upper)) return;

    int prec = fs_clamp_prec(sp->prec);
    bool negative = math_signbit(val);
    f64 a = math_abs(val);

    int ndig = prec + 2;                   /* 前导 1 位 + prec 位小数 + 1 位 guard */
    if (ndig > FS_SIG_MAX) ndig = FS_SIG_MAX;
    if (ndig < 2) ndig = 2;

    char dig[FS_SIG_MAX + 2];
    bool sticky = false;
    int exp10 = 0;
    fs_extract(a, ndig, dig, &sticky, &exp10);
    {
        bool gs = false;
        char guard = fs_guard_of(dig, ndig, prec + 1, sticky, &gs);
        fs_round_at(dig, ndig, prec, guard, gs, &exp10);
    }
    int e10 = (a == 0.0) ? 0 : exp10 - 1;   /* 科学计数法指数（0 输出 e+00） */

    char body[FS_FMT_BUF];
    int bl = 0;
    body[bl++] = dig[0];
    if (prec > 0 || sp->alt_form) {
        body[bl++] = '.';
        for (int i = 0; i < prec; i++) {
            int idx = 1 + i;
            body[bl++] = (idx < ndig) ? dig[idx] : '0';
        }
    }
    body[bl++] = upper ? 'E' : 'e';
    body[bl++] = (e10 < 0) ? '-' : '+';
    {
        unsigned ae = (unsigned)(e10 < 0 ? -e10 : e10);
        if (ae >= 100) body[bl++] = (char)('0' + (ae / 100) % 10);
        if (ae >= 10) body[bl++] = (char)('0' + (ae / 10) % 10);
        else body[bl++] = '0';                    /* 指数至少 2 位 */
        body[bl++] = (char)('0' + ae % 10);
    }
    body[bl] = '\0';

    char pre[2];
    pre[0] = '\0';
    if (negative) pre[0] = '-';
    else if (sp->plus_sign) pre[0] = '+';
    else if (sp->space_sign) pre[0] = ' ';
    pre[1] = '\0';
    fs_femit(pre, body, sp);
}

/* %g / %G 通用格式: 按指数选择定点/科学, 去尾零 */
static void fs_emit_g(f64 val, const fs_spec* sp, bool upper) {
    if (fs_emit_special(val, sp, upper)) return;

    int prec = sp->prec >= 0 ? sp->prec : 6;
    if (prec == 0) prec = 1;
    if (prec > FS_SIG_MAX - 2) prec = FS_SIG_MAX - 2;

    bool negative = math_signbit(val);
    f64 a = math_abs(val);

    int ndig = prec + 1;                    /* prec 位有效数字 + 1 位 guard */
    if (ndig > FS_SIG_MAX) ndig = FS_SIG_MAX;
    if (ndig < 2) ndig = 2;

    char dig[FS_SIG_MAX + 2];
    bool sticky = false;
    int exp10 = 0;
    fs_extract(a, ndig, dig, &sticky, &exp10);
    {
        bool gs = false;
        char guard = fs_guard_of(dig, ndig, prec, sticky, &gs);
        fs_round_at(dig, ndig, prec - 1, guard, gs, &exp10);
    }
    int X = (a == 0.0) ? 0 : exp10 - 1;

    /* 组装尾数（定点或科学）; 尾数结束位置记入 ml, 之后追加指数 */
    char body[FS_FMT_BUF];
    int bl = 0;
    int ml;
    if (X < -4 || X >= prec) {
        /* 科学计数, 小数位 prec-1 */
        body[bl++] = dig[0];
        if (prec > 1 || sp->alt_form) {
            body[bl++] = '.';
            for (int i = 0; i < prec - 1; i++) {
                int idx = 1 + i;
                body[bl++] = (idx < ndig) ? dig[idx] : '0';
            }
        }
    } else {
        /* 定点: 整数位数 = X+1（X<0 时小数点左侧为 0） */
        int n_int = X + 1;
        int frac = prec - 1 - X;         /* 小数位数 */
        if (n_int <= 0) {
            body[bl++] = '0';
        } else {
            for (int i = 0; i < n_int && bl < (int)sizeof(body) - 20; i++)
                body[bl++] = (i < ndig) ? dig[i] : '0';
        }
        if (frac > 0) {
            body[bl++] = '.';
            for (int i = 0; i < frac && bl < (int)sizeof(body) - 20; i++) {
                int idx = n_int + i;
                body[bl++] = (idx >= 0 && idx < ndig) ? dig[idx] : '0';
            }
        } else if (sp->alt_form) {
            body[bl++] = '.';
        }
    }
    ml = bl;
    body[bl] = '\0';

    /* 去尾零（仅尾数部分, ' #' 标志保留） */
    if (!sp->alt_form) {
        bool has_dot = false;
        for (int i = 0; i < ml; i++) if (body[i] == '.') has_dot = true;
        if (has_dot) {
            int end = ml;
            while (end > 0 && body[end - 1] == '0') end--;
            if (end > 0 && body[end - 1] == '.') end--;
            body[end] = '\0';
            bl = end;
        }
    }

    /* 科学计数: 追加指数 */
    if (X < -4 || X >= prec) {
        body[bl++] = upper ? 'E' : 'e';
        body[bl++] = (X < 0) ? '-' : '+';
        unsigned ae = (unsigned)(X < 0 ? -X : X);
        if (ae >= 100) body[bl++] = (char)('0' + (ae / 100) % 10);
        if (ae >= 10) body[bl++] = (char)('0' + (ae / 10) % 10);
        else body[bl++] = '0';                    /* 指数至少 2 位 */
        body[bl++] = (char)('0' + ae % 10);
    }
    body[bl] = '\0';

    char pre[2];
    int pl = 0;
    pre[0] = '\0';
    if (negative) pre[pl++] = '-';
    else if (sp->plus_sign) pre[pl++] = '+';
    else if (sp->space_sign) pre[pl++] = ' ';
    pre[pl] = '\0';
    fs_femit(pre, body, sp);
}

/* 兼容旧调用点: 输出 f64, 精度 prec 位小数（print_float / println_multi 使用） */
static void fs_format_double(f64 val, int prec) {
    fs_spec sp;
    sp.left_align = false;
    sp.zero_pad = false;
    sp.plus_sign = false;
    sp.space_sign = false;
    sp.alt_form = false;
    sp.width = 0;
    sp.prec = prec;
    fs_format_fixed(val, &sp, false);
}

/* 兼容旧调用点: %g 语义（与 libc printf("%g") 对齐） */
static void fs_format_g(f64 val, int prec) {
    fs_spec sp;
    sp.left_align = false;
    sp.zero_pad = false;
    sp.plus_sign = false;
    sp.space_sign = false;
    sp.alt_form = false;
    sp.width = 0;
    sp.prec = prec;
    fs_emit_g(val, &sp, false);
}

// ==================== 迷你 printf 核心 ====================

static void fs_printf_va(const char* format, va_list ap) {
    if (format == NULL) return;

    while (*format != 0) {
        if (*format != '%') {
            fs_putchar(*format);
            format++;
            continue;
        }
        format++;                          /* 跳过 '%' */

        /* 标志 */
        fs_spec sp;
        sp.left_align = false;
        sp.zero_pad = false;
        sp.plus_sign = false;
        sp.space_sign = false;
        sp.alt_form = false;
        sp.width = 0;
        sp.prec = -1;
        for (;;) {
            if (*format == '-') { sp.left_align = true; format++; }
            else if (*format == '0') { sp.zero_pad = true; format++; }
            else if (*format == '+') { sp.plus_sign = true; format++; }
            else if (*format == ' ') { sp.space_sign = true; format++; }
            else if (*format == '#') { sp.alt_form = true; format++; }
            else break;
        }

        /* 宽度 */
        if (*format == '*') {
            int w = va_arg(ap, int);
            format++;
            if (w < 0) { sp.left_align = true; w = -w; }
            sp.width = w;
        } else {
            while (*format >= '0' && *format <= '9') {
                sp.width = sp.width * 10 + (*format - '0');
                format++;
            }
        }
        if (sp.width > FS_FMT_MAXW) sp.width = FS_FMT_MAXW;

        /* 精度 */
        if (*format == '.') {
            format++;
            sp.prec = 0;
            if (*format == '*') {
                int p = va_arg(ap, int);
                format++;
                sp.prec = p < 0 ? -1 : p;
            } else {
                while (*format >= '0' && *format <= '9') {
                    sp.prec = sp.prec * 10 + (*format - '0');
                    format++;
                }
            }
        }

        /* 长度修饰符: 解析并记录（整数按 64 位读取, 与 Kaula i64 约定一致） */
        bool is_long = false;
        while (*format == 'l' || *format == 'h' || *format == 'z' ||
               *format == 'j' || *format == 't' || *format == 'L') {
            if (*format == 'l') is_long = true;
            format++;
        }

        char conv = *format;
        switch (conv) {
        case '%':
            fs_putchar('%');
            format++;
            break;
        case 'c': {
            char c = (char)va_arg(ap, int);
            char body[2];
            body[0] = c;
            body[1] = '\0';
            fs_spec s2 = sp;
            s2.zero_pad = false;            /* 非数值转换忽略 '0' 标志 */
            fs_fclear();
            fs_femit("", body, &s2);
            format++;
            break;
        }
        case 's': {
            const char* s = va_arg(ap, const char*);
            if (s == NULL) s = "(null)";
            char body[FS_FMT_BUF];
            int bl = 0;
            while (s[bl] != 0) {
                if (sp.prec >= 0 && bl >= sp.prec) break;
                if (bl >= FS_FMT_BUF - 1) break;
                body[bl] = s[bl];
                bl++;
            }
            body[bl] = '\0';
            fs_spec s2 = sp;
            s2.zero_pad = false;
            fs_fclear();
            fs_femit("", body, &s2);
            format++;
            break;
        }
        case 'd':
        case 'i': {
            i64 v = is_long ? (i64)va_arg(ap, long long) : (i64)va_arg(ap, int);
            bool neg = v < 0;
            u64 mag = neg ? (u64)0 - (u64)v : (u64)v;
            fs_emit_int(mag, 10, false, neg, &sp);
            format++;
            break;
        }
        case 'u': {
            u64 v = is_long ? (u64)va_arg(ap, unsigned long long)
                            : (u64)va_arg(ap, unsigned int);
            fs_emit_int(v, 10, false, false, &sp);
            format++;
            break;
        }
        case 'x':
        case 'X': {
            u64 v = is_long ? (u64)va_arg(ap, unsigned long long)
                            : (u64)va_arg(ap, unsigned int);
            fs_emit_int(v, 16, conv == 'X', false, &sp);
            format++;
            break;
        }
        case 'o': {
            u64 v = is_long ? (u64)va_arg(ap, unsigned long long)
                            : (u64)va_arg(ap, unsigned int);
            fs_emit_int(v, 8, false, false, &sp);
            format++;
            break;
        }
        case 'b': {
            u64 v = is_long ? (u64)va_arg(ap, unsigned long long)
                            : (u64)va_arg(ap, unsigned int);
            fs_emit_int(v, 2, false, false, &sp);
            format++;
            break;
        }
        case 'p': {
            void* p = va_arg(ap, void*);
            u64 v = (u64)(uintptr_t)p;
            char digits[72];
            fs_u64_to_str(v, 16, false, digits);
            char body[80];
            body[0] = '0';
            body[1] = 'x';
            int bl = 2;
            for (int i = 0; digits[i] != 0 && bl < (int)sizeof(body) - 1; i++)
                body[bl++] = digits[i];
            body[bl] = '\0';
            fs_spec s2 = sp;
            s2.zero_pad = false;
            fs_fclear();
            fs_femit("", body, &s2);
            format++;
            break;
        }
        case 'f':
        case 'F': {
            f64 d = va_arg(ap, double);
            fs_format_fixed(d, &sp, conv == 'F');
            format++;
            break;
        }
        case 'e':
        case 'E': {
            f64 d = va_arg(ap, double);
            fs_format_exp(d, &sp, conv == 'E');
            format++;
            break;
        }
        case 'g':
        case 'G': {
            f64 d = va_arg(ap, double);
            fs_emit_g(d, &sp, conv == 'G');
            format++;
            break;
        }
        default:
            /* 未知转换符: 原样回显 '%' 与该字符（不吞掉后续字符, 不死循环） */
            fs_putchar('%');
            if (conv != 0) {
                fs_putchar(conv);
                format++;
            }
            break;
        }
    }
}

// ==================== 与 std.io 同名的格式化输出 ====================

FS_WEAK void print(const char* format, ...) {
    va_list args;
    va_start(args, format);
    fs_printf_va(format, args);
    va_end(args);
}

FS_WEAK void println(const char* format, ...) {
    va_list args;
    va_start(args, format);
    fs_printf_va(format, args);
    va_end(args);
    fs_putchar('\n');
}

// type: 0=int, 1=double, 2=string（与 std.io 相同调用约定）
FS_WEAK void println_multi(int arg_count, ...) {
    va_list args;
    va_start(args, arg_count);

    for (int i = 0; i < arg_count; i++) {
        int type = va_arg(args, int);
        switch (type) {
        case 0: { // int
            long long v = va_arg(args, long long);
            fs_putint((i64)v);
            break;
        }
        case 1: { // double
            double val = va_arg(args, double);
            fs_format_g(val, 6);            // 与 std.io 的 printf("%g") 对齐
            break;
        }
        case 2: { // string
            const char* s = va_arg(args, const char*);
            if (s == NULL) s = "(null)";
            fs_puts(s);
            break;
        }
        default:
            break;
        }
    }
    va_end(args);
    fs_putchar('\n');
}

FS_WEAK void print_char(char c) {
    fs_putchar(c);
}

FS_WEAK void print_int(i64 value) {
    fs_putint(value);
}

FS_WEAK void print_float(f64 value) {
    fs_format_double(value, 6);             // 与 std.io 的 printf("%f") 对齐
}

FS_WEAK void print_bool(bool value) {
    fs_puts(value ? "true" : "false");
}

FS_WEAK void print_hex(u64 value) {
    fs_puthex(value);
}

#endif /* KAULA_FREESTANDING_IO_C */
