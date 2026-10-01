/*
 * Kaula 并发内核 · 阶段三测试
 * 验证阻塞车道 + Worker 撤离机制
 *
 * 测试场景：
 * 1. 阻塞任务不影响 Worker
 * 2. #[hint(blocking)] 属性
 * 3. Worker 撤离（长计算不饿死其他任务）
 */

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <assert.h>
#include <stdint.h>
#include <stdatomic.h>
#include <unistd.h>

#include "../../src/runtime/kaula_runtime_public.h"

/* ========== 辅助函数 ========== */

static uint64_t get_ticks_ns(void) {
    return kaula_get_ticks();
}

/* ========== 测试 1：阻塞任务不影响 Worker ========== */

typedef struct {
    int sleep_ms;       /* 阻塞睡眠时间 */
    int result;
} BlockingTaskFrame;

static KaulaResumeResult blocking_task_resume(void* frame_ptr) {
    BlockingTaskFrame* f = (BlockingTaskFrame*)frame_ptr;

    /* 模拟阻塞操作（如 FFI 调用） */
    usleep(f->sleep_ms * 1000);
    f->result = f->sleep_ms * 2;

    return (KaulaResumeResult){
        .status = KAULA_RESUME_BLOCKED,
        .value = (void*)(intptr_t)f->result,
    };
}

static void test_blocking_task_isolation(void) {
    printf("=== 测试 1：阻塞任务不影响 Worker ===\n");

    /* spawn 一个阻塞任务 */
    BlockingTaskFrame* frame = (BlockingTaskFrame*)kmm_v4_alloc_global(sizeof(BlockingTaskFrame));
    if (frame == NULL) {
        printf("  [FAIL] frame 分配失败\n");
        assert(0);
    }
    frame->sleep_ms = 100; /* 阻塞 100ms */
    frame->result = 0;

    printf("  spawn 阻塞任务（100ms）...\n");
    KaulaHandle h = kaula_spawn(blocking_task_resume, frame, sizeof(BlockingTaskFrame));
    if (kaula_handle_is_null(h)) {
        printf("  [FAIL] spawn 返回 null\n");
        assert(0);
    }

    /* 同时 spawn 一个正常任务 */
    typedef struct { int x; } NormalFrame;
    NormalFrame* nf = (NormalFrame*)kmm_v4_alloc_global(sizeof(NormalFrame));
    nf->x = 42;

    KaulaHandle nh = kaula_spawn(
        (KaulaResumeFn)({
            KaulaResumeResult fn(void* p) {
                NormalFrame* f = (NormalFrame*)p;
                return (KaulaResumeResult){
                    .status = KAULA_RESUME_DONE,
                    .value = (void*)(intptr_t)(f->x * 2),
                };
            }
            fn;
        }),
        nf, sizeof(NormalFrame)
    );

    /* 等待正常任务完成（应该很快） */
    printf("  await 正常任务...\n");
    uint64_t start = get_ticks_ns();
    void* nresult = kaula_handle_await(nh);
    uint64_t elapsed = get_ticks_ns() - start;

    int nval = (int)(intptr_t)nresult;
    printf("  正常任务结果: %d, 耗时 %lu ms\n", nval, (unsigned long)(elapsed / 1000000));

    if (nval == 84 && elapsed < 50000000) { /* < 50ms */
        printf("  [PASS] 正常任务未被阻塞任务影响\n");
    } else {
        printf("  [FAIL] 正常任务被阻塞或结果错误\n");
        assert(0);
    }

    /* 等待阻塞任务完成 */
    printf("  await 阻塞任务...\n");
    void* bresult = kaula_handle_await(h);
    int bval = (int)(intptr_t)bresult;
    printf("  阻塞任务结果: %d\n", bval);

    if (bval == 200) {
        printf("  [PASS] 阻塞任务正确完成\n");
    } else {
        printf("  [FAIL] 阻塞任务结果错误\n");
        assert(0);
    }
}

/* ========== 测试 2：#[hint(blocking)] 属性 ========== */

typedef struct {
    int id;
    int result;
} HintBlockingFrame;

static KaulaResumeResult hint_blocking_resume(void* frame_ptr) {
    HintBlockingFrame* f = (HintBlockingFrame*)frame_ptr;

    /* 模拟阻塞操作 */
    usleep(50000); /* 50ms */
    f->result = f->id * 10;

    return (KaulaResumeResult){
        .status = KAULA_RESUME_BLOCKED,
        .value = (void*)(intptr_t)f->result,
    };
}

#define HINT_BLOCKING_COUNT 10

static void test_hint_blocking_attribute(void) {
    printf("\n=== 测试 2：#[hint(blocking)] 属性 ===\n");

    KaulaHandle handles[HINT_BLOCKING_COUNT];
    HintBlockingFrame* frames[HINT_BLOCKING_COUNT];

    printf("  spawn %d 个阻塞任务...\n", HINT_BLOCKING_COUNT);
    uint64_t start = get_ticks_ns();

    for (int i = 0; i < HINT_BLOCKING_COUNT; i++) {
        frames[i] = (HintBlockingFrame*)kmm_v4_alloc_global(sizeof(HintBlockingFrame));
        frames[i]->id = i;
        frames[i]->result = 0;

        handles[i] = kaula_spawn(hint_blocking_resume, frames[i], sizeof(HintBlockingFrame));
        if (kaula_handle_is_null(handles[i])) {
            printf("  [FAIL] spawn %d 返回 null\n", i);
            assert(0);
        }
    }

    printf("  await all...\n");
    int success_count = 0;
    for (int i = 0; i < HINT_BLOCKING_COUNT; i++) {
        void* result = kaula_handle_await(handles[i]);
        int val = (int)(intptr_t)result;
        if (val == i * 10) {
            success_count++;
        }
    }

    uint64_t elapsed = get_ticks_ns() - start;
    printf("  完成 %d/%d 个阻塞任务, 耗时 %lu ms\n",
           success_count, HINT_BLOCKING_COUNT, (unsigned long)(elapsed / 1000000));

    if (success_count == HINT_BLOCKING_COUNT) {
        printf("  [PASS] 所有阻塞任务正确完成\n");
    } else {
        printf("  [FAIL] 只有 %d 个完成\n", success_count);
        assert(0);
    }
}

/* ========== 测试 3：Worker 撤离（长计算不饿死） ========== */

static atomic_int long_task_iterations = 0;

typedef struct {
    volatile int running;
} LongComputeFrame;

static KaulaResumeResult long_compute_resume(void* frame_ptr) {
    LongComputeFrame* f = (LongComputeFrame*)frame_ptr;

    /* 模拟长计算：每次 resume 做大量工作 */
    for (int i = 0; i < 1000000 && f->running; i++) {
        atomic_fetch_add(&long_task_iterations, 1);
    }

    /* 主动让出 */
    return (KaulaResumeResult){
        .status = KAULA_RESUME_BUDGET_EXHAUSTED,
    };
}

typedef struct {
    int id;
} QuickTaskFrame;

static KaulaResumeResult quick_task_resume(void* frame_ptr) {
    QuickTaskFrame* f = (QuickTaskFrame*)frame_ptr;
    return (KaulaResumeResult){
        .status = KAULA_RESUME_DONE,
        .value = (void*)(intptr_t)(f->id * 100),
    };
}

#define QUICK_TASK_COUNT 50

static void test_worker_eviction(void) {
    printf("\n=== 测试 3：Worker 撤离（长计算不饿死） ===\n");

    atomic_store(&long_task_iterations, 0);

    /* spawn 一个长计算任务 */
    LongComputeFrame* lcf = (LongComputeFrame*)kmm_v4_alloc_global(sizeof(LongComputeFrame));
    lcf->running = 1;

    printf("  spawn 长计算任务...\n");
    KaulaHandle lch = kaula_spawn(long_compute_resume, lcf, sizeof(LongComputeFrame));
    (void)lch; /* 不等待，让它一直在队列里 */

    /* 等待 100ms，让长计算任务运行 */
    usleep(100000);

    /* spawn 多个快速任务 */
    printf("  spawn %d 个快速任务...\n", QUICK_TASK_COUNT);
    KaulaHandle handles[QUICK_TASK_COUNT];
    QuickTaskFrame* frames[QUICK_TASK_COUNT];

    uint64_t start = get_ticks_ns();

    for (int i = 0; i < QUICK_TASK_COUNT; i++) {
        frames[i] = (QuickTaskFrame*)kmm_v4_alloc_global(sizeof(QuickTaskFrame));
        frames[i]->id = i;

        handles[i] = kaula_spawn(quick_task_resume, frames[i], sizeof(QuickTaskFrame));
        if (kaula_handle_is_null(handles[i])) {
            printf("  [FAIL] spawn %d 返回 null\n", i);
            assert(0);
        }
    }

    printf("  await all quick tasks...\n");
    int success_count = 0;
    for (int i = 0; i < QUICK_TASK_COUNT; i++) {
        void* result = kaula_handle_await(handles[i]);
        int val = (int)(intptr_t)result;
        if (val == i * 100) {
            success_count++;
        }
    }

    uint64_t elapsed = get_ticks_ns() - start;
    printf("  完成 %d/%d 个快速任务, 耗时 %lu ms\n",
           success_count, QUICK_TASK_COUNT, (unsigned long)(elapsed / 1000000));

    int iterations = atomic_load(&long_task_iterations);
    printf("  长计算任务迭代次数: %d\n", iterations);

    if (success_count == QUICK_TASK_COUNT && elapsed < 1000000000) { /* < 1s */
        printf("  [PASS] 快速任务未被长计算饿死\n");
    } else {
        printf("  [FAIL] 快速任务被饿死或超时\n");
        assert(0);
    }

    /* 停止长计算任务 */
    lcf->running = 0;
}

/* ========== 主函数 ========== */

int main(void) {
    setbuf(stdout, NULL);

    printf("Kaula 并发内核 · 阶段三测试\n");
    printf("============================\n\n");

    /* 初始化运行时 */
    KaulaRuntimeConfig config = {0};
    config.worker_count = 2;
    config.budget_quantum = 256;

    int err = kaula_runtime_init(&config);
    if (err != 0) {
        fprintf(stderr, "runtime init failed: %d\n", err);
        return 1;
    }

    /* 运行测试 */
    test_blocking_task_isolation();
    test_hint_blocking_attribute();
    test_worker_eviction();

    /* 关闭运行时 */
    kaula_runtime_shutdown();

    printf("\n============================\n");
    printf("所有测试通过！\n");

    return 0;
}
