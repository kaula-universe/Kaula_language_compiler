/*
 * Kaula 并发内核 · 探针 2：分配重叠检查
 * 打印 hs 基址、全部 Task/Frame 指针，检查是否有地址重叠（双分配）。
 */
#include "kaula_conc_test.h"
#include "kaula_conc.h"

#include <stdio.h>
#include <stdlib.h>

typedef struct { int input; int result; int64_t out; } NF;

static KaulaResumeResult n_resume(void* fp) {
    NF* f = (NF*)fp;
    /*
     * result.value 的契约是「指向返回数据的指针」（await 复制的是它
     * 指向的内容，见 kaula_task_complete / await_copy_out）。把标量
     * 直接强转成指针会令 await memcpy 去读地址 5 —— 确定性 SIGSEGV。
     * 与 test_completion_stamp 一致：先落进帧字段，再交地址。
     */
    f->out = f->input;
    return (KaulaResumeResult){ .status = KAULA_RESUME_DONE,
                                .value = &f->out };
}

int main(void) {
    setvbuf(stdout, NULL, _IONBF, 0);
    KaulaRuntimeConfig cfg = {0};
    if (kaula_runtime_init(&cfg) != 0) {
        printf("P2 init fail\n");
        return 1;
    }

    int N = 20000;
    const char* env_n = getenv("PROBE_N");
    if (env_n != NULL) N = atoi(env_n);

    KaulaHandle* hs = (KaulaHandle*)kmm_v4_alloc_global(N * sizeof(KaulaHandle));
    void** frames = (void**)kmm_v4_alloc_global(N * sizeof(void*));
    if (hs == NULL || frames == NULL) {
        printf("P2 hs/frames alloc fail\n");
        kaula_runtime_shutdown();
        return 1;
    }
    printf("P2 hs_base=%p frames_base=%p N=%d\n", (void*)hs, (void*)frames, N);

    int nulls = 0;
    for (int i = 0; i < N; i++) {
        NF* f = (NF*)kmm_v4_alloc_global(sizeof(NF));
        frames[i] = f;
        if (f == NULL) { printf("P2 frame alloc fail at %d\n", i); break; }
        f->input = i; f->result = 0;
        hs[i] = kaula_spawn_enter(n_resume, n_resume, f, sizeof(NF));
        if (kaula_handle_is_null(hs[i])) nulls++;
    }
    printf("P2 spawn done nulls=%d\n", nulls);

    /* 去重检查：task 指针与 frame 指针是否与 hs 槽位重叠 */
    for (int i = 0; i < N; i++) {
        char* t = (char*)hs[i].task;
        char* f = (char*)frames[i];
        if (hs[i].task != NULL &&
            (t >= (char*)hs && t < (char*)hs + N * sizeof(KaulaHandle))) {
            printf("P2 OVERLAP hs[%d].task=%p inside hs array!\n", i, (void*)hs[i].task);
        }
        if (f != NULL &&
            (f >= (char*)hs && f < (char*)hs + N * sizeof(KaulaHandle))) {
            printf("P2 OVERLAP frame[%d]=%p inside hs array!\n", i, (void*)f);
        }
        /* 两个 task 指针重复？ */
        for (int j = 0; j < i; j++) {
            if (hs[i].task != NULL && hs[i].task == hs[j].task) {
                printf("P2 DUP task %d == %d : %p\n", j, i, (void*)hs[i].task);
            }
            if (frames[i] != NULL && frames[i] == frames[j]) {
                printf("P2 DUP frame %d == %d : %p\n", j, i, (void*)frames[i]);
            }
        }
    }

    /* await 全部 */
    int bad = 0;
    for (int i = 0; i < N; i++) {
        int64_t r = -7777;
        kaula_handle_await_into(hs[i], &r, sizeof(r));
        if (r != i) { bad++; }
    }
    printf("P2 await done bad=%d\n", bad);

    kaula_runtime_shutdown();
    return 0;
}
