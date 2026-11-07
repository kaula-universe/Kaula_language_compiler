/*
 * diag.c —— degrade 瓶颈逐层金丝雀
 *
 * 思路：用同一份 C 基准的 resume 函数，但把"提交/回收"链路上的每一环
 * 单独拿出来计时，找出 12.3us 的纯计算为何在稳态只剩 ~1/50 的吞吐。
 *
 * 每个场景都跑 20000 个任务（与基准一致），报告：
 *   wall_ns   —— 从首次 spawn 到最后一个 await 返回的墙钟
 *   tps       —— 20000 / wall
 *   avg_us    —— 每任务平均墙钟（含调度）
 *
 * 场景：
 *   A 理论下界    —— 单线程直接调用 resume（不进内核），即纯计算成本
 *   B 全链路      —— 正常 spawn + await（对标基准）
 *   C 无回收      —— 正常路径，但把 GLOBAL_RECLAIM_THRESHOLD 拉到天文数字
 *                    （需要源码补丁，这里用 env 探针替代：见变体）
 *   D 串行不进内核—— 单线程反复调用真实的 kaula_task_complete/alloc 组合
 */
#include "kaula/concurrency/kaula_conc.h"
#include "kaula/concurrency/kaula_conc_worker.h"
#include "kaula/concurrency/kaula_conc_epoch.h"
#include "kaula/concurrency/kaula_conc_task.h"

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

static void banner(const char* s) { printf("\n===== %s =====\n", s); }

int main(int argc, char** argv) {
    int conc = argc > 1 ? atoi(argv[1]) : 1000;
    int total = argc > 2 ? atoi(argv[2]) : 20000;
    int spin = argc > 3 ? atoi(argv[3]) : 20000;

    KaulaRuntimeConfig c; memset(&c, 0, sizeof c); c.enable_metrics = -1;

    /* ---------- A 纯计算下界（完全不进内核） ---------- */
    banner("A 纯计算下界（单线程直调 resume，不进内核）");
    {
        F* f = calloc(1, sizeof(F)); f->spin = spin;
        uint64_t t0 = now();
        for (int i = 0; i < total; i++) { f->pc = 0; (void)rr(f); }
        uint64_t dt = now() - t0;
        printf("  wall=%.1f ms  tps=%.0f  每任务=%.2f us\n",
               dt / 1e6, total * 1e9 / (double)dt, dt / 1000.0 / total);
        printf("  (32 核理论上限 ≈ %.2f M TPS)\n",
               (total * 1e9 / (double)dt) * 32 / 1e6);
        free(f);
    }

    /* ---------- B 全链路 ---------- */
    banner("B 全链路（spawn + await，对标基准）");
    kaula_runtime_init(&c);
    {
        F** fr = malloc(sizeof(F*) * conc);
        KaulaHandle* inf = malloc(sizeof(KaulaHandle) * conc);
        for (int j = 0; j < conc; j++) { fr[j] = calloc(1, sizeof(F)); fr[j]->spin = spin; inf[j] = kaula_handle_null(); }
        int64_t done = 0, issued = 0, sf = 0;
        uint64_t t0 = now();
        while (done < total) {
            int filled = 0;
            for (int j = 0; j < conc && issued < total; j++) {
                fr[j]->pc = 0;
                inf[j] = kaula_spawn(rr, fr[j], sizeof(F));
                if (kaula_handle_is_null(inf[j])) { sf++; done++; }
                issued++; filled++;
            }
            for (int j = 0; j < filled; j++) {
                if (kaula_handle_is_null(inf[j])) continue;
                int64_t v; uint64_t l;
                kaula_handle_await_into_timed(inf[j], &v, 8, &l);
                kaula_handle_release(inf[j]); done++;
            }
        }
        uint64_t dt = now() - t0;
        printf("  wall=%.1f ms  tps=%.0f  每任务=%.2f us  spawn_failed=%lld\n",
               dt / 1e6, total * 1e9 / (double)dt, dt / 1000.0 / total, (long long)sf);
        free(inf); for (int j = 0; j < conc; j++) free(fr[j]); free(fr);
    }
    kaula_runtime_shutdown();

    /* ---------- C 只测 spawn（不等） ---------- */
    banner("C 只 spawn 不等（隔离提交路径成本）");
    memset(&c, 0, sizeof c); c.enable_metrics = -1;
    kaula_runtime_init(&c);
    {
        F** fr = malloc(sizeof(F*) * conc);
        KaulaHandle* inf = malloc(sizeof(KaulaHandle) * conc);
        for (int j = 0; j < conc; j++) { fr[j] = calloc(1, sizeof(F)); fr[j]->spin = spin; inf[j] = kaula_handle_null(); }
        uint64_t t0 = now();
        for (int i = 0; i < total; i++) {
            int j = i % conc;
            if (!kaula_handle_is_null(inf[j])) kaula_handle_release(inf[j]);
            fr[j]->pc = 0;
            inf[j] = kaula_spawn(rr, fr[j], sizeof(F));
        }
        uint64_t dt = now() - t0;
        printf("  spawn %d 个耗时 %.1f ms → 每 spawn=%.2f us  (tps=%.0f)\n",
               total, dt / 1e6, dt / 1000.0 / total, total * 1e9 / (double)dt);
        for (int j = 0; j < conc; j++) if (!kaula_handle_is_null(inf[j])) kaula_handle_release(inf[j]);
        free(inf); for (int j = 0; j < conc; j++) free(fr[j]); free(fr);
    }
    kaula_runtime_shutdown();

    /* ---------- D 串行 await 单个任务（close loop） ---------- */
    banner("D 闭环单任务（spawn 一个立刻 await，无并发窗口）");
    memset(&c, 0, sizeof c); c.enable_metrics = -1;
    kaula_runtime_init(&c);
    {
        F* f = calloc(1, sizeof(F)); f->spin = spin;
        uint64_t t0 = now();
        for (int i = 0; i < total; i++) {
            f->pc = 0;
            KaulaHandle h = kaula_spawn(rr, f, sizeof(F));
            int64_t v; kaula_handle_await_into_timed(h, &v, 8, NULL);
            kaula_handle_release(h);
        }
        uint64_t dt = now() - t0;
        printf("  wall=%.1f ms  每任务=%.2f us  tps=%.0f\n",
               dt / 1e6, dt / 1000.0 / total, total * 1e9 / (double)dt);
        free(f);
    }
    kaula_runtime_shutdown();

    return 0;
}
