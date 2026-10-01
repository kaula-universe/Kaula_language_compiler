/*
 * Kaula 并发内核 · epoch 回收 与 完成队列 测试
 *
 * 这两块是内存安全的底（设计说明书 P5、文档 §8 / §10.3）。
 *
 * 核心风险：KaulaHandle 里存的是裸 Task 指针。
 * 任务完成时若立即 free，任何正在 await 的线程都会读到已释放内存（UAF）。
 * epoch 保证：只有当所有活跃 Worker 都离开后，对象才真正释放。
 */

#include "kaula_conc_test.h"
#include "kaula_conc.h"
#include "kaula_conc_epoch.h"
#include "kaula_conc_completion.h"

#include <string.h>

typedef struct { int pc; int v; } DummyFrame;

static KaulaResumeResult dummy_resume(void* fp) {
    DummyFrame* f = (DummyFrame*)fp;
    f->v = 1;
    return (KaulaResumeResult){ .status = KAULA_RESUME_DONE, .value = (void*)1 };
}

/* ========== epoch 基础 ========== */

static void test_epoch_enter_exit(void) {
    TEST_CASE("epoch enter/exit 不崩溃");
    KaulaRuntimeConfig cfg = {0};
    cfg.worker_count = 1;
    ASSERT_EQ(kaula_runtime_init(&cfg), 0, "初始化应成功");

    kaula_epoch_enter();
    kaula_epoch_exit();
    TEST_PASS();

    kaula_runtime_shutdown();
}

static void test_epoch_retire_not_immediate(void) {
    TEST_CASE("retire 后对象在 epoch 内仍可读（非 UAF）");
    KaulaRuntimeConfig cfg = {0};
    cfg.worker_count = 1;
    ASSERT_EQ(kaula_runtime_init(&cfg), 0, "初始化应成功");

    uint64_t r0 = kaula_epoch_retired_total();

    KaulaTask* t = kaula_task_alloc();
    ASSERT_NOT_NULL(t, "分配应成功");
    if (t == NULL) { kaula_runtime_shutdown(); return; }

    kaula_task_complete(t, (void*)0xABCD);

    /*
     * 关键断言：完成后对象仍然可读。
     * 若 complete 路径改成立即 free，这里就是 UAF。
     * 我们读 task_id（不该被清零）与 result。
     */
    ASSERT_EQ((int)(intptr_t)t->result, 0xABCD, "完成后结果仍可读");
    ASSERT_NE(t->task_id, 0, "完成后 task_id 仍可读（未被清零）");

    ASSERT_TRUE(kaula_epoch_retired_total() > r0, "退休计数应递增");

    kaula_runtime_shutdown();
}

static void test_epoch_reclaims_on_shutdown(void) {
    TEST_CASE("shutdown 后全部退休对象被回收");
    KaulaRuntimeConfig cfg = {0};
    cfg.worker_count = 2;
    ASSERT_EQ(kaula_runtime_init(&cfg), 0, "初始化应成功");

    for (int i = 0; i < 50; i++) {
        KaulaTask* t = kaula_task_alloc();
        if (t == NULL) break;
        kaula_task_complete(t, (void*)(intptr_t)i);
    }

    uint64_t retired = kaula_epoch_retired_total();
    ASSERT_MSG(retired >= 50, "应有 50 个退休对象，实际 %llu",
              (unsigned long long)retired);

    kaula_runtime_shutdown();

    uint64_t reclaimed = kaula_epoch_reclaimed_total();
    ASSERT_MSG(reclaimed >= 50, "shutdown 后应全部回收，实际 %llu",
              (unsigned long long)reclaimed);
    ASSERT_TRUE(reclaimed >= retired, "回收数不应少于退休数（无泄漏）");
}

/* ========== await 与 epoch 的配合 ========== */

typedef struct {
    _Atomic(int) stop;
} RaceFrame;

static KaulaResumeResult race_resume(void* fp) {
    RaceFrame* f = (RaceFrame*)fp;
    if (atomic_load(&f->stop)) {
        return (KaulaResumeResult){ .status = KAULA_RESUME_DONE, .value = (void*)5 };
    }
    return (KaulaResumeResult){ .status = KAULA_RESUME_BUDGET_EXHAUSTED, .value = NULL };
}

static void test_await_races_with_completion(void) {
    TEST_CASE("await 与 complete 并发：不得读到已释放内存");
    KaulaRuntimeConfig cfg = {0};
    cfg.worker_count = 4;
    ASSERT_EQ(kaula_runtime_init(&cfg), 0, "初始化应成功");

    /*
     * 跑多轮，制造"await 刚要注册 / complete 刚好发生"的窗口。
     * 这是 P5 最容易崩的地方：一轮撞不上不代表没有。
     */
    const int ROUNDS = 300;
    int completed = 0;

    for (int r = 0; r < ROUNDS; r++) {
        RaceFrame f;
        atomic_store(&f.stop, 0);

        KaulaTask* t = NULL;

        /*
         * 走 spawn 而不是手工 post：spawn 会递增 active_task_count，
         * complete 会递减 —— 手工 post 会漏掉前者，
         * 计数会变成负数（实测 -300）。
         */
        KaulaHandle hh = kaula_spawn(race_resume, &f, sizeof(f));
        if (kaula_handle_is_null(hh)) break;
        t = hh.task;

        /* 主线程立刻 await，与 Worker 上的执行形成竞争 */
        if (r % 2 == 0) {
            atomic_store(&f.stop, 1);          /* 让下一次 resume 完成 */
            if (kaula_task_await(t, 2000000000ULL)) {
                completed++;
            }
        } else {
            /* 另一路：先让它自然完成，再 await（已完成的快速路径） */
            atomic_store(&f.stop, 1);
            kaula_p_sleep_ns(1000);
            if (kaula_task_await(t, 2000000000ULL)) completed++;
        }
    }

    ASSERT_EQ(completed, ROUNDS, "所有任务都应完成");
    ASSERT_EQ(kaula_conc_active_tasks(), 0, "结束后不应有活跃任务残留");

    kaula_runtime_shutdown();
}

/* ========== 完成队列 ========== */

static void test_completion_push_take(void) {
    TEST_CASE("完成队列 push / take");
    kaula_completion_init();

    /* 构造一个"已完成"的任务并入队 */
    KaulaTask* t = kaula_task_alloc();
    if (t == NULL) { TEST_FAIL("Task 分配失败"); return; }

    uint64_t id = t->task_id;
    ASSERT_EQ(kaula_completion_push(t), 1, "入队应成功");
    ASSERT_EQ(kaula_completion_count(), 1, "完成计数应为 1");

    KaulaTask* got = kaula_completion_take(id);
    ASSERT_TRUE(got == t, "take 应取回同一个任务");

    kaula_completion_shutdown();
}

static void test_completion_sharding(void) {
    TEST_CASE("完成队列按 task_id 分片");
    kaula_completion_init();

    /*
     * 连续 id 若都落到同一片，说明散列失效。
     * 至少应有多个不同的片被用到。
     */
    int shards_hit[KAULA_COMPLETION_SHARDS];
    memset(shards_hit, 0, sizeof(shards_hit));

    KaulaTask* tasks[200];
    for (int i = 0; i < 200; i++) {
        tasks[i] = kaula_task_alloc();
        if (tasks[i] == NULL) break;
        kaula_completion_push(tasks[i]);
    }

    /* 用快照统计各片用量 */
    int used = 0;
    for (uint32_t s = 0; s < KAULA_COMPLETION_SHARDS; s++) {
        uint64_t head = atomic_load(&g_conc_completion.shards[s].head);
        uint64_t tail = atomic_load(&g_conc_completion.shards[s].tail);
        if (tail > head) used++;
        (void)shards_hit;
    }

    ASSERT_MSG(used >= KAULA_COMPLETION_SHARDS / 2,
              "200 个任务应散布到多个分片，实际只用到 %d 片", used);

    kaula_completion_shutdown();
}

static void test_completion_full_no_overwrite(void) {
    TEST_CASE("完成队列满时拒绝而非覆盖（I3）");
    kaula_completion_init();

    /*
     * 用同一个 task 反复 push：它总是落到同一片，
     * 于是正好填满那一片（上限 CAPACITY-1，保留一个空槽）。
     */
    int accepted = 0;
    int rejected = 0;
    KaulaTask* t = kaula_task_alloc();
    if (t == NULL) { kaula_completion_shutdown(); TEST_FAIL("分配失败"); return; }

    for (int i = 0; i < KAULA_COMPLETION_CAPACITY_PER_SHARD * 2; i++) {
        if (kaula_completion_push(t)) accepted++; else rejected++;
    }

    ASSERT_EQ(accepted, KAULA_COMPLETION_CAPACITY_PER_SHARD - 1,
              "单片应恰好接受 CAPACITY-1 个（留一个空槽）");
    ASSERT_TRUE(rejected > 0, "超容量应被拒绝");
    ASSERT_EQ(kaula_completion_drops(), (uint64_t)rejected, "丢弃计数应吻合");

    kaula_completion_shutdown();
}

/* ========== 边界：分配失败 ========== */

static void test_task_alloc_null_safe(void) {
    TEST_CASE("对空指针的防御");
    ASSERT_EQ(kaula_task_await(NULL, 1000), 1, "await(NULL) 视为已完成");
    ASSERT_EQ(kaula_handle_poll(kaula_handle_null()), 0, "poll(空 handle) 应为 0");
    ASSERT_NULL(kaula_handle_await(kaula_handle_null()), "await(空 handle) 应返回 NULL");
    ASSERT_NULL(kaula_handle_try_await(kaula_handle_null(), 1000), "try_await(空) 应返回 NULL");
    kaula_handle_cancel(kaula_handle_null());   /* 不应崩溃 */
    ASSERT_EQ(kaula_handle_error(kaula_handle_null()), 0, "空 handle 错误码为 0");
    TEST_PASS();
}

int main(void) {
    setvbuf(stdout, NULL, _IONBF, 0);
    setvbuf(stderr, NULL, _IONBF, 0);

    TEST_SECTION("epoch");
    test_epoch_enter_exit();
    test_epoch_retire_not_immediate();
    test_epoch_reclaims_on_shutdown();
    TEST_SECTION("await 与回收");
    test_await_races_with_completion();
    TEST_SECTION("完成队列");
    test_completion_push_take();
    test_completion_sharding();
    test_completion_full_no_overwrite();
    TEST_SECTION("边界");
    test_task_alloc_null_safe();
    return test_report("epoch");
}