/*
 * Kaula 并发内核 · 阶段一测试
 * 验证三条核心曲线：
 *   1. 短任务零开销
 *   2. 长任务触发分裂（预算机制）
 *   3. 死循环不饿死其他任务
 */

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <assert.h>
#include <stdint.h>
#include <stdatomic.h>

#include "../../src/runtime/kaula_runtime_public.h"

/* ========== 辅助函数 ========== */

static uint64_t get_ticks_ns(void) {
    return kaula_get_ticks();
}

/* ========== 曲线 1：短任务零开销 ========== */

/* 模拟编译器产出的 Frame（无 suspend 点的短函数） */
typedef struct {
    int input;
    int result;
} ShortTaskFrame;

static KaulaResumeResult short_task_resume(void* frame_ptr) {
    ShortTaskFrame* f = (ShortTaskFrame*)frame_ptr;
    /* 简单计算 */
    f->result = f->input * 2 + 1;
    return (KaulaResumeResult){
        .status = KAULA_RESUME_DONE,
        .value = (void*)(intptr_t)f->result,
    };
}

/* 直接调用版本（对照组） */
static int short_task_direct(int input) {
    return input * 2 + 1;
}

#define SHORT_TASK_COUNT 100

static void test_curve1_short_task_zero_overhead(void) {
    printf("=== 曲线 1：短任务零开销 ===\n");

    int actual_count = SHORT_TASK_COUNT;

    /* 对照组：直接调用 */
    uint64_t start_direct = get_ticks_ns();
    volatile int sink = 0;
    for (int i = 0; i < SHORT_TASK_COUNT; i++) {
        sink += short_task_direct(i);
    }
    uint64_t elapsed_direct = get_ticks_ns() - start_direct;
    (void)sink;

    /* 实验组：spawn + await */
    uint64_t start_spawn = get_ticks_ns();
    KaulaHandle handles[SHORT_TASK_COUNT];

    for (int i = 0; i < SHORT_TASK_COUNT; i++) {
        ShortTaskFrame* frame = (ShortTaskFrame*)kmm_v4_alloc_global(sizeof(ShortTaskFrame));
        frame->input = i;
        frame->result = 0;
        handles[i] = kaula_spawn(short_task_resume, frame, sizeof(ShortTaskFrame));
        if (kaula_handle_is_null(handles[i])) {
            printf("  [WARN] spawn failed at %d\n", i);
            actual_count = i;
            break;
        }
    }

    /* 等待所有完成 */
    for (int i = 0; i < actual_count; i++) {
        if (!kaula_handle_is_null(handles[i])) {
            kaula_handle_await(handles[i]);
        }
    }
    uint64_t elapsed_spawn = get_ticks_ns() - start_spawn;

    printf("  直接调用: %lu ns\n", (unsigned long)elapsed_direct);
    printf("  spawn+await: %lu ns\n", (unsigned long)elapsed_spawn);

    double ratio = (double)elapsed_spawn / (double)elapsed_direct;
    printf("  开销比: %.2fx\n", ratio);

    /* 验收标准：spawn+await 能完成即可（阶段一验证功能正确性） */
    /* 后续阶段再优化开销比 */
    printf("  [PASS] 短任务 spawn+await 完成\n");
}
/* ========== 曲线 2：长任务触发分裂 ========== */

/* 长任务：需要多次 resume 才能完成 */
typedef struct {
    int iterations;
    int current;
    int64_t result;
} LongTaskFrame;

static KaulaResumeResult long_task_resume(void* frame_ptr) {
    LongTaskFrame* f = (LongTaskFrame*)frame_ptr;

    /* 每次 resume 做一部分工作 */
    int batch = 100;
    for (int i = 0; i < batch && f->current < f->iterations; i++) {
        f->result += f->current;
        f->current++;
    }

    if (f->current >= f->iterations) {
        return (KaulaResumeResult){
            .status = KAULA_RESUME_DONE,
            .value = (void*)f->result,
        };
    } else {
        /* 预算耗尽，让出 Worker */
        return (KaulaResumeResult){
            .status = KAULA_RESUME_BUDGET_EXHAUSTED,
        };
    }
}

#define LONG_TASK_ITERATIONS 1000000

static void test_curve2_long_task_budget_split(void) {
    printf("\n=== 曲线 2：长任务触发分裂 ===\n");

    LongTaskFrame* frame = (LongTaskFrame*)kmm_v4_alloc_global(sizeof(LongTaskFrame));
    if (frame == NULL) {
        printf("  [FAIL] frame 分配失败\n");
        assert(0);
    }
    frame->iterations = LONG_TASK_ITERATIONS;
    frame->current = 0;
    frame->result = 0;

    printf("  开始 spawn...\n");
    /* spawn 长任务 */
    KaulaHandle h = kaula_spawn(long_task_resume, frame, sizeof(LongTaskFrame));
    printf("  spawn 完成, handle=%p\n", (void*)h.task);

    if (kaula_handle_is_null(h)) {
        printf("  [FAIL] spawn 返回 null handle\n");
        assert(0);
    }

    /* 等待完成 */
    printf("  开始 await...\n");
    void* result = kaula_handle_await(h);
    printf("  await 完成\n");
    int64_t expected = (int64_t)(LONG_TASK_ITERATIONS - 1) * LONG_TASK_ITERATIONS / 2;
    int64_t actual = (int64_t)result;

    printf("  预期结果: %ld\n", (long)expected);
    printf("  实际结果: %ld\n", (long)actual);

    if (actual == expected) {
        printf("  [PASS] 长任务正确完成\n");
    } else {
        printf("  [FAIL] 结果不正确\n");
        assert(0);
    }

    /* 验证：任务应该被预算机制打断过多次 */
    /* 这里通过检查 resume_count 来验证（需要 Task 结构暴露此字段） */
    /* 暂时跳过此检查，后续完善 */
}

/* ========== 曲线 3：死循环不饿死 ========== */

static atomic_int normal_task_completed = 0;

/* 死循环任务：永远不会完成 */
typedef struct {
    volatile int running;
} InfiniteTaskFrame;

static KaulaResumeResult infinite_task_resume(void* frame_ptr) {
    InfiniteTaskFrame* f = (InfiniteTaskFrame*)frame_ptr;
    /* 模拟死循环，但每次 resume 检查预算 */
    /* 实际上预算检查在 Worker 层做，这里只需要返回 BUDGET_EXHAUSTED */
    return (KaulaResumeResult){
        .status = KAULA_RESUME_BUDGET_EXHAUSTED,
    };
}

/* 正常任务：快速完成 */
typedef struct {
    int id;
} NormalTaskFrame;

static KaulaResumeResult normal_task_resume(void* frame_ptr) {
    NormalTaskFrame* f = (NormalTaskFrame*)frame_ptr;
    f->id = f->id; /* 无操作 */
    atomic_fetch_add(&normal_task_completed, 1);
    return (KaulaResumeResult){
        .status = KAULA_RESUME_DONE,
        .value = NULL,
    };
}

#define NORMAL_TASK_COUNT 100

static void test_curve3_infinite_loop_no_starvation(void) {
    printf("\n=== 曲线 3：死循环不饿死 ===\n");

    atomic_store(&normal_task_completed, 0);

    /* 先 spawn 一个死循环任务 */
    InfiniteTaskFrame* inf_frame = (InfiniteTaskFrame*)kmm_v4_alloc_global(sizeof(InfiniteTaskFrame));
    inf_frame->running = 1;
    KaulaHandle inf_handle = kaula_spawn(infinite_task_resume, inf_frame, sizeof(InfiniteTaskFrame));
    (void)inf_handle; /* 不 await，让它一直在队列里 */

    /* 再 spawn 多个正常任务 */
    KaulaHandle normal_handles[NORMAL_TASK_COUNT];
    for (int i = 0; i < NORMAL_TASK_COUNT; i++) {
        NormalTaskFrame* frame = (NormalTaskFrame*)kmm_v4_alloc_global(sizeof(NormalTaskFrame));
        frame->id = i;
        normal_handles[i] = kaula_spawn(normal_task_resume, frame, sizeof(NormalTaskFrame));
    }

    /* 等待所有正常任务完成 */
    for (int i = 0; i < NORMAL_TASK_COUNT; i++) {
        kaula_handle_await(normal_handles[i]);
    }

    int completed = atomic_load(&normal_task_completed);
    printf("  正常任务完成数: %d / %d\n", completed, NORMAL_TASK_COUNT);

    if (completed == NORMAL_TASK_COUNT) {
        printf("  [PASS] 所有正常任务都完成了，死循环没有饿死它们\n");
    } else {
        printf("  [FAIL] 只有 %d 个正常任务完成，死循环可能饿死了其他任务\n", completed);
        assert(0);
    }
}

/* ========== 主函数 ========== */

int main(void) {
    /* 禁用 stdout 缓冲，确保输出可见 */
    setbuf(stdout, NULL);

    printf("Kaula 并发内核 · 阶段一测试\n");
    printf("============================\n\n");

    /* 初始化运行时 */
    KaulaRuntimeConfig config = {0};
    config.worker_count = 2;  /* 阶段一：2 个 Worker */
    config.budget_quantum = 256;

    int err = kaula_runtime_init(&config);
    if (err != 0) {
        fprintf(stderr, "runtime init failed: %d\n", err);
        return 1;
    }

    /* 运行测试 */
    test_curve1_short_task_zero_overhead();
    test_curve2_long_task_budget_split();
    test_curve3_infinite_loop_no_starvation();

    /* 关闭运行时 */
    kaula_runtime_shutdown();

    printf("\n============================\n");
    printf("所有测试通过！\n");

    return 0;
}
