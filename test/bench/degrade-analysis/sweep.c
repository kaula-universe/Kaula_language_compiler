/*
 * sweep.c —— 决定性对照：同一进程内交错测量，消除跨进程噪声
 *
 * 用法: sweep <conc> <total> <spin> <rounds>
 *
 * 输出每种驱动方式的中位数（消除单次抖动），并在同一进程内重复 R 轮，
 * 让所有变体共同经历同一段系统噪声。
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

typedef struct { F** fr; KaulaHandle* inf; int conc; int total; int spin; } Ctx;

/* V1 逐窗口（基准口径） */
static double run_v1(Ctx* c) {
    int64_t done = 0, issued = 0;
    uint64_t t0 = now();
    while (done < c->total) {
        int filled = 0;
        for (int j = 0; j < c->conc && issued < c->total; j++) {
            c->fr[j]->pc = 0;
            c->inf[j] = kaula_spawn(rr, c->fr[j], sizeof(F));
            issued++; filled++;
        }
        for (int j = 0; j < filled; j++) {
            int64_t v; kaula_handle_await_into_timed(c->inf[j], &v, 8, NULL);
            kaula_handle_release(c->inf[j]); done++;
        }
    }
    uint64_t dt = now() - t0;
    return c->total * 1e9 / (double)dt;
}

/* V2 堆叠提交 */
static double run_v2(Ctx* c) {
    int64_t done = 0, issued = 0;
    uint64_t t0 = now();
    while (issued < c->total) {
        int batch = 0;
        for (int j = 0; issued < c->total && j < c->conc; j++, issued++) {
            c->fr[j]->pc = 0;
            c->inf[j] = kaula_spawn(rr, c->fr[j], sizeof(F));
            batch++;
        }
        for (int j = 0; j < batch; j++) {
            int64_t v; kaula_handle_await_into_timed(c->inf[j], &v, 8, NULL);
            kaula_handle_release(c->inf[j]); done++;
        }
    }
    uint64_t dt = now() - t0;
    return c->total * 1e9 / (double)dt;
}

/* V3 窗口恒满（滑动） */
static double run_v3(Ctx* c) {
    int64_t issued = 0, done = 0;
    for (int j = 0; j < c->conc && issued < c->total; j++, issued++) {
        c->fr[j]->pc = 0;
        c->inf[j] = kaula_spawn(rr, c->fr[j], sizeof(F));
    }
    int slot = 0;
    uint64_t t0 = now();
    while (done < c->total) {
        int64_t v; kaula_handle_await_into_timed(c->inf[slot], &v, 8, NULL);
        kaula_handle_release(c->inf[slot]); done++;
        if (issued < c->total) {
            c->fr[slot]->pc = 0;
            c->inf[slot] = kaula_spawn(rr, c->fr[slot], sizeof(F));
            issued++;
        }
        slot = (slot + 1) % c->conc;
    }
    uint64_t dt = now() - t0;
    return c->total * 1e9 / (double)dt;
}

static int cmp_d(const void* a, const void* b) {
    double x = *(const double*)a, y = *(const double*)b;
    return (x < y) ? -1 : (x > y);
}

int main(int argc, char** argv) {
    int conc = argc > 1 ? atoi(argv[1]) : 1000;
    int total = argc > 2 ? atoi(argv[2]) : 20000;
    int spin = argc > 3 ? atoi(argv[3]) : 20000;
    int rounds = argc > 4 ? atoi(argv[4]) : 7;

    KaulaRuntimeConfig c; memset(&c, 0, sizeof c); c.enable_metrics = -1;
    kaula_runtime_init(&c);

    Ctx x;
    x.conc = conc; x.total = total; x.spin = spin;
    x.fr = malloc(sizeof(F*) * conc);
    x.inf = malloc(sizeof(KaulaHandle) * conc);
    for (int j = 0; j < conc; j++) { x.fr[j] = calloc(1, sizeof(F)); x.fr[j]->spin = spin; x.inf[j] = kaula_handle_null(); }

    double s1[64], s2[64], s3[64];
    for (int r = 0; r < rounds; r++) {
        s1[r] = run_v1(&x);
        s2[r] = run_v2(&x);
        s3[r] = run_v3(&x);
    }
    qsort(s1, rounds, sizeof(double), cmp_d);
    qsort(s2, rounds, sizeof(double), cmp_d);
    qsort(s3, rounds, sizeof(double), cmp_d);

    printf("conc=%-6d spin=%-6d 中位数(%d轮):  V1逐窗口=%-9.0f  V2堆叠=%-9.0f  V3滑动=%-9.0f\n",
           conc, spin, rounds, s1[rounds / 2], s2[rounds / 2], s3[rounds / 2]);
    printf("                          最小/最大:  V1 %.0f/%.0f  V2 %.0f/%.0f  V3 %.0f/%.0f\n",
           s1[0], s1[rounds - 1], s2[0], s2[rounds - 1], s3[0], s3[rounds - 1]);

    free(x.inf); for (int j = 0; j < conc; j++) free(x.fr[j]); free(x.fr);
    kaula_runtime_shutdown();
    return 0;
}
