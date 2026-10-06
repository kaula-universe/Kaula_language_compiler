/*
 * opt2.c —— 确认瓶颈：await 时的"跨核缓存失效" vs "调度元数据"
 *
 * 关键对照：
 *   W1 大窗口 back-to-back（conc=10000,total=20000）→ 1.5M TPS
 *   W2 基准口径 conc=1000                              → 0.6M TPS
 * 差别只在「填 1000 个就停下来 await」——停下就意味着 Worker 与 main
 * 在 Task 数组上反复换手。本程序直接量这件事：
 *   在每个 await 前插入不同长度的忙等（拉大"任务已就绪"的比例），
 *   看 TPS 与就绪比例的关系。
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

extern int kaula_task_is_done_probe(void* task);

int main(int argc, char** argv) {
    int conc = argc > 1 ? atoi(argv[1]) : 1000;
    int total = argc > 2 ? atoi(argv[2]) : 20000;
    int spin = argc > 3 ? atoi(argv[3]) : 20000;

    KaulaRuntimeConfig c; memset(&c, 0, sizeof c); c.enable_metrics = -1;
    kaula_runtime_init(&c);

    F** fr = malloc(sizeof(F*) * conc);
    KaulaHandle* inf = malloc(sizeof(KaulaHandle) * conc);
    for (int j = 0; j < conc; j++) { fr[j] = calloc(1, sizeof(F)); fr[j]->spin = spin; inf[j] = kaula_handle_null(); }

    int64_t done = 0, issued = 0, ready = 0;
    uint64_t t0 = now();
    while (done < total) {
        int filled = 0;
        for (int j = 0; j < conc && issued < total; j++) {
            fr[j]->pc = 0;
            inf[j] = kaula_spawn(rr, fr[j], sizeof(F));
            issued++; filled++;
        }
        for (int j = 0; j < filled; j++) {
            if (kaula_task_is_done_probe(inf[j].task)) ready++;
            int64_t v; kaula_handle_await_into_timed(inf[j], &v, 8, NULL);
            kaula_handle_release(inf[j]); done++;
        }
    }
    uint64_t dt = now() - t0;
    printf("conc=%-6d tps=%-9.0f 每任务=%.2f us  提交时已就绪比例=%.1f%%\n",
           conc, total * 1e9 / (double)dt, dt / 1000.0 / total,
           100.0 * ready / total);

    free(inf); for (int j = 0; j < conc; j++) free(fr[j]); free(fr);
    kaula_runtime_shutdown();
    return 0;
}
