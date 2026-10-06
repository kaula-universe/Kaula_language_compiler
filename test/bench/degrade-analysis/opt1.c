/*
 * opt1.c —— 验证「填满整窗后再统一 await」的优化幅度
 *
 * 现状（基准口径，逐窗口填满→逐个 await）：conc 越大越亏，
 *   因为「填」这段时间里 Worker 已经跑完了一批任务，await 提前返回，
 *   而提前返回之后 main 要重新填窗口 —— 形成「一小批、一小批」的涡流，
 *   调度元数据（Task 分配、epoch retire/reclaim、唤醒）被反复支付。
 *
 * 本程序对比三种驱动方式（事务本体完全相同，仍走内核）：
 *   V1 逐窗口    —— 现基准口径（填 win → await win）
 *   V2 背靠背    —— 一次性把 total 全部 spawn（受 max_tasks 限），再全 await
 *   V3 主动填满  —— 始终维持 win 个在途：await 一个立刻补一个
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
    int conc = argc > 1 ? atoi(argv[1]) : 10000;
    int total = argc > 2 ? atoi(argv[2]) : 20000;
    int spin = argc > 3 ? atoi(argv[3]) : 20000;

    KaulaRuntimeConfig c; memset(&c, 0, sizeof c); c.enable_metrics = -1;
    kaula_runtime_init(&c);

    F** fr = malloc(sizeof(F*) * total);
    KaulaHandle* inf = malloc(sizeof(KaulaHandle) * total);
    for (int j = 0; j < total; j++) { fr[j] = calloc(1, sizeof(F)); fr[j]->spin = spin; inf[j] = kaula_handle_null(); }

    /* ---------- V1 逐窗口（基准口径） ---------- */
    {
        int64_t done = 0, issued = 0;
        uint64_t t0 = now();
        while (done < total) {
            int filled = 0;
            for (int j = 0; j < conc && issued < total; j++) {
                fr[j]->pc = 0;
                inf[j] = kaula_spawn(rr, fr[j], sizeof(F));
                issued++; filled++;
            }
            for (int j = 0; j < filled; j++) {
                int64_t v; kaula_handle_await_into_timed(inf[j], &v, 8, NULL);
                kaula_handle_release(inf[j]); done++;
            }
        }
        uint64_t dt = now() - t0;
        printf("V1 逐窗口(基准口径) conc=%-6d tps=%-9.0f 每任务=%.2f us\n",
               conc, total * 1e9 / (double)dt, dt / 1000.0 / total);
    }

    /* ---------- V2 一次性全部 spawn（分批绕过 max_tasks） ---------- */
    {
        int64_t done = 0, issued = 0;
        uint64_t t0 = now();
        while (issued < total) {
            int batch = 0;
            for (int j = 0; issued < total && j < conc; j++, issued++) {
                fr[j]->pc = 0;
                inf[j] = kaula_spawn(rr, fr[j], sizeof(F));
                batch++;
            }
            for (int j = 0; j < batch; j++) {
                int64_t v; kaula_handle_await_into_timed(inf[j], &v, 8, NULL);
                kaula_handle_release(inf[j]); done++;
            }
        }
        uint64_t dt = now() - t0;
        printf("V2 全量提交 conc=%-6d tps=%-9.0f 每任务=%.2f us\n",
               conc, total * 1e9 / (double)dt, dt / 1000.0 / total);
    }

    /* ---------- V3 维持窗口恒满：await 一个立刻补一个 ---------- */
    {
        int64_t issued = 0, done = 0;
        /* 预填 */
        for (int j = 0; j < conc && issued < total; j++, issued++) {
            fr[j]->pc = 0;
            inf[j] = kaula_spawn(rr, fr[j], sizeof(F));
        }
        /* 滑动：await j 之后立刻在 j 位置补一个新的 */
        int slot = 0;
        uint64_t t0 = now();
        while (done < total) {
            int64_t v; kaula_handle_await_into_timed(inf[slot], &v, 8, NULL);
            kaula_handle_release(inf[slot]); done++;
            if (issued < total) {
                fr[slot]->pc = 0;
                inf[slot] = kaula_spawn(rr, fr[slot], sizeof(F));
                issued++;
            }
            slot = (slot + 1) % conc;
        }
        uint64_t dt = now() - t0;
        printf("V3 窗口恒满(滑动) conc=%-6d tps=%-9.0f 每任务=%.2f us\n",
               conc, total * 1e9 / (double)dt, dt / 1000.0 / total);
    }

    free(inf); for (int j = 0; j < total; j++) free(fr[j]); free(fr);
    kaula_runtime_shutdown();
    return 0;
}
