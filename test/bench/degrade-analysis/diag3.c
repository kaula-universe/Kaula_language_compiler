/*
 * diag3.c —— 定位「取结果」环节的真实成本
 *
 * 现象：spawn 一个任务只要 0.8us，但「取结果」（await + release）在
 * 小窗口下每次要 2 微秒以上。本程序把取结果拆成三段各自计时：
 *   T_ready  : task 已 DONE，await 只做快速返回
 *   T_wait   : task 未完成，await 要等
 *   T_rel    : release
 * 并回答一个关键问题：把 conc 撑大，是不是仅仅因为「摊薄」了 drain 循环
 * 的固定开销。
 */
#include "kaula/concurrency/kaula_conc.h"

#include <stdio.h>
#include <string.h>
#include <stdint.h>
#include <stdlib.h>
#include <time.h>

#define MULT 1103515245LL
#define ADD 12345LL
#define MASK30 1073741824LL
#define MOD_P 9223372036854775807LL

typedef struct { int32_t pc, _p; int64_t result, spin; } F;

static int64_t parse(void) {
    int64_t s = 2545, c = 0;
    for (int k = 0; k < 256; k++) { s = ((s % 1048576) * MULT + ADD) % MOD_P; c = ((c % MASK30) * 31 + (s % 256)) % MOD_P; }
    return c;
}
static KaulaResumeResult rr(void* f_) {
    F* f = f_;
    int64_t a = 1;
    for (int64_t r = 0; r < f->spin; r++) a = ((a % MASK30) * MULT + ADD) % MOD_P;
    f->result = a ^ parse();
    KaulaResumeResult x; memset(&x, 0, sizeof x);
    x.status = KAULA_RESUME_DONE; x.value = &f->result;
    return x;
}
static uint64_t now(void) {
    struct timespec t; clock_gettime(CLOCK_MONOTONIC, &t);
    return t.tv_sec * 1000000000ULL + t.tv_nsec;
}

/* 简单延时：用忙等保证纯用户态，不受调度器影响 */
static void spin_ns(uint64_t ns) {
    uint64_t t0 = now();
    while (now() - t0 < ns) { }
}

int main(int argc, char** argv) {
    int conc = argc > 1 ? atoi(argv[1]) : 1000;
    int total = argc > 2 ? atoi(argv[2]) : 20000;
    int spin = argc > 3 ? atoi(argv[3]) : 20000;

    KaulaRuntimeConfig c; memset(&c, 0, sizeof c); c.enable_metrics = -1;
    kaula_runtime_init(&c);

    F** fr = malloc(sizeof(F*) * conc);
    KaulaHandle* inf = malloc(sizeof(KaulaHandle) * conc);
    for (int j = 0; j < conc; j++) { fr[j] = calloc(1, sizeof(F)); fr[j]->spin = spin; inf[j] = kaula_handle_null(); }

    int64_t done = 0, issued = 0;
    uint64_t t_fill = 0, t_await = 0, t_rel = 0;
    uint64_t n_ready = 0, n_wait = 0;
    uint64_t t0 = now();

    while (done < total) {
        int filled = 0;
        uint64_t a = now();
        for (int j = 0; j < conc && issued < total; j++) {
            fr[j]->pc = 0;
            inf[j] = kaula_spawn(rr, fr[j], sizeof(F));
            issued++; filled++;
        }
        t_fill += now() - a;

        for (int j = 0; j < filled; j++) {
            int64_t v; uint64_t l;
            /* 记录 await 入口时的状态，判断是"已就绪"还是"要等" */
            extern int kaula_task_is_done_probe(void* task);
            int w = kaula_task_is_done_probe(inf[j].task);
            uint64_t a2 = now();
            kaula_handle_await_into_timed(inf[j], &v, 8, &l);
            t_await += now() - a2;
            if (w) n_ready++; else n_wait++;
            uint64_t a3 = now();
            kaula_handle_release(inf[j]);
            t_rel += now() - a3;
            done++;
        }
    }
    uint64_t dt = now() - t0;

    printf("conc=%-6d wall=%-7.1fms tps=%-9.0f | 填=%5.1fms 等=%6.1fms 放=%5.1fms | "
           "已就绪=%lld 需等待=%lld\n",
           conc, dt / 1e6, total * 1e9 / (double)dt,
           t_fill / 1e6, t_await / 1e6, t_rel / 1e6,
           (long long)n_ready, (long long)n_wait);

    free(inf); for (int j = 0; j < conc; j++) free(fr[j]); free(fr);
    kaula_runtime_shutdown();
    return 0;
}
