/*
 * diag2.c —— 验证「conc 越小越慢」的成因
 *
 * 假设：窗口 W 太小时，填满窗口的这段时间里 Worker 已经跑完一部分任务，
 * 于是 await 时大量任务已 DONE（快路径），但每次 await 都要进内核、
 * 加锁、检查事件；而窗口大时 Worker 被摊薄，任务还在跑，await 走 park
 * 快路径（不持锁）。
 *
 * 验证手段：把「填窗口」与「取结果」的耗时分开计时。
 * 另外测一种极端：窗口 W 但每轮只 await 一个（保持窗口满）。
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
    uint64_t fill_ns = 0, drain_ns = 0;
    uint64_t t0 = now();

    while (done < total) {
        int filled = 0;
        uint64_t a = now();
        for (int j = 0; j < conc && issued < total; j++) {
            fr[j]->pc = 0;
            inf[j] = kaula_spawn(rr, fr[j], sizeof(F));
            issued++; filled++;
        }
        uint64_t b = now();
        fill_ns += b - a;

        for (int j = 0; j < filled; j++) {
            int64_t v; uint64_t l;
            kaula_handle_await_into_timed(inf[j], &v, 8, &l);
            kaula_handle_release(inf[j]);
            done++;
        }
        drain_ns += now() - b;
    }
    uint64_t dt = now() - t0;

    printf("conc=%-6d wall=%-8.1f ms  tps=%-9.0f 每任务=%.2f us | "
           "填窗口=%.1f ms (%.0f%%)  取结果=%.1f ms (%.0f%%)\n",
           conc, dt / 1e6, total * 1e9 / (double)dt, dt / 1000.0 / total,
           fill_ns / 1e6, 100.0 * fill_ns / dt,
           drain_ns / 1e6, 100.0 * drain_ns / dt);

    free(inf); for (int j = 0; j < conc; j++) free(fr[j]); free(fr);
    kaula_runtime_shutdown();
    return 0;
}
