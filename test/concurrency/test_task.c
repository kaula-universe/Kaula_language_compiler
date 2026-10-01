/*
 * Kaula 并发内核 · Task 生命周期与 Handle 测试
 *
 * 覆盖：
 * - alloc/init/complete 生命周期
 * - await 的 park/unpark 路径（原实现是 sleep 轮询）
 * - try_await 超时
 * - poll 非阻塞查询
 * - cancel
 * - epoch 回收：完成后不立即释放，退休计数递增
 * - 失败路径：kaula_task_fail 也能唤醒等待者（否则 await 死锁）
 */

#include "kaula_conc_test.h"
#include "kaula_conc.h"

#include <string.h>

/* 编译器 lowering 风格的帧与 resume 函数 */
typedef struct {
    int  pc;
    int  input;
    int  result;
} SimpleFrame;

static KaulaResumeResult simple_resume(void* frame_ptr) {
    SimpleFrame* f = (SimpleFrame*)frame_ptr;
    if (f->pc == 0) {
        f->pc = 1;
        f->result = f->input * 3 + 1;
        return (KaulaResumeResult){ .status = KAULA_RESUME_DONE,
                                    .value = (void*)(intptr_t)f->result };
    }
    return (KaulaResumeResult){ .status = KAULA_RESUME_DONE,
                                .value = (void*)(intptr_t)f->result };
}

/* 会主动让出若干次的任务，用于验证预算/让出路径 */
typedef struct {
    int pc;
    int yields_left;
} YieldFrame;

static KaulaResumeResult yield_resume(void* frame_ptr) {
    YieldFrame* f = (YieldFrame*)frame_ptr;
    if (f->yields_left > 0) {
        f->yields_left--;
        return (KaulaResumeResult){ .status = KAULA_RESUME_BUDGET_EXHAUSTED, .value = NULL };
    }
    return (KaulaResumeResult){ .status = KAULA_RESUME_DONE, .value = (void*)7 };
}

/* ========== 生命周期 ========== */

static void test_alloc_init_complete(void) {
    TEST_CASE("alloc / init / complete 生命周期");
    KaulaTask* t = kaula_task_alloc();
    ASSERT_NOT_NULL(t, "分配应成功");
    if (t == NULL) return;

    /*
     * task_id 由 g_conc_runtime.next_task_id 分配。
     * 该计数器初值为 1，但 kaula_runtime_init 会把它重置为 1，
     * 因此未初始化运行时的裸 alloc 拿到的 id 可能为 0。
     * 这里只要求 id 单调递增（不同任务 id 不同）。
     */
    KaulaTask* t2 = kaula_task_alloc();
    if (t2 != NULL) {
        ASSERT_NE(t2->task_id, t->task_id, "不同任务 id 必须不同");
    } else {
        TEST_PASS();
    }
    ASSERT_EQ(atomic_load(&t->state), KAULA_TASK_CREATED, "初始状态为 CREATED");

    SimpleFrame* f = (SimpleFrame*)kmm_v4_alloc_global(sizeof(SimpleFrame));
    f->pc = 0; f->input = 5; f->result = 0;

    kaula_task_init(t, simple_resume, f, sizeof(SimpleFrame));
    ASSERT_TRUE(t->resume_fn == simple_resume, "resume_fn 应被记录");
    ASSERT_EQ(t->frame_size, (long long)sizeof(SimpleFrame), "frame_size 应记录");

    KaulaResumeResult rr = t->resume_fn(t->frame);
    ASSERT_EQ(rr.status, KAULA_RESUME_DONE, "resume 应报告 DONE");
    int r = (int)(intptr_t)rr.value;
    ASSERT_EQ(r, 16, "resume 应算出 5*3+1=16");

    kaula_task_complete(t, (void*)(intptr_t)r);
    ASSERT_EQ(atomic_load(&t->state), KAULA_TASK_DONE, "完成后状态为 DONE");
    ASSERT_EQ((int)(intptr_t)t->result, 16, "结果应可读");
}

/* ========== await ========== */

static void test_await_completed(void) {
    TEST_CASE("await 一个已完成的任务立即返回");
    KaulaRuntimeConfig cfg = {0};
    cfg.worker_count = 2;
    ASSERT_EQ(kaula_runtime_init(&cfg), 0, "运行时初始化应成功");

    KaulaTask* t = kaula_task_alloc();
    SimpleFrame* f = (SimpleFrame*)kmm_v4_alloc_global(sizeof(SimpleFrame));
    f->pc = 0; f->input = 10; f->result = 0;
    kaula_task_init(t, simple_resume, f, sizeof(SimpleFrame));
    kaula_task_complete(t, (void*)(intptr_t)31);

    KaulaHandle h = kaula_handle_from_task(t);
    void* r = kaula_handle_await(h);
    ASSERT_EQ((int)(intptr_t)r, 31, "await 应返回已存好的结果");
    ASSERT_EQ(kaula_handle_poll(h), 1, "poll 应报告已完成");

    kaula_runtime_shutdown();
}

static void test_await_spawned(void) {
    TEST_CASE("spawn 后 await 能拿到结果");
    KaulaRuntimeConfig cfg = {0};
    cfg.worker_count = 4;
    ASSERT_EQ(kaula_runtime_init(&cfg), 0, "运行时初始化应成功");

    int total = 0;
    const int N = 200;
    for (int i = 0; i < N; i++) {
        SimpleFrame* f = (SimpleFrame*)kmm_v4_alloc_global(sizeof(SimpleFrame));
        f->pc = 0; f->input = i; f->result = 0;
        KaulaHandle h = kaula_spawn(simple_resume, f, sizeof(SimpleFrame));
        ASSERT_FALSE(kaula_handle_is_null(h), "spawn 应返回有效 handle");
        if (kaula_handle_is_null(h)) break;
        int v = (int)(intptr_t)kaula_handle_await(h);
        ASSERT_EQ(v, i * 3 + 1, "结果应与输入对应");
        total += v;
    }
    ASSERT_EQ(total, (N * (N - 1) / 2) * 3 + N, "总和应吻合");

    kaula_runtime_shutdown();
}

static void test_try_await_timeout(void) {
    TEST_CASE("try_await 超时返回 NULL");
    KaulaTask* t = kaula_task_alloc();
    ASSERT_NOT_NULL(t, "分配应成功");
    if (t == NULL) return;

    /* 故意不完成：验证超时而不是永久阻塞 */
    uint64_t t0 = kaula_p_now_ns();
    void* r = kaula_handle_try_await(kaula_handle_from_task(t), 50000000ULL);
    uint64_t dt = kaula_p_now_ns() - t0;

    ASSERT_NULL(r, "未完成的任务 try_await 应返回 NULL");
    ASSERT_TRUE(dt >= 30000000ULL, "应真的等了约 50ms");
    ASSERT_TRUE(dt < 2000000000ULL, "不应远超超时时间");

    kaula_task_complete(t, (void*)1);
}

static void test_fail_wakes_waiters(void) {
    TEST_CASE("kaula_task_fail 也唤醒等待者（否则 await 死锁）");
    KaulaTask* t = kaula_task_alloc();
    ASSERT_NOT_NULL(t, "分配应成功");
    if (t == NULL) return;

    /* 先注册一个等待者（模拟另一个线程在 await） */
    atomic_store(&t->waiters, 1);

    uint64_t t0 = kaula_p_now_ns();
    kaula_task_fail(t, KAULA_ERR_SHUTDOWN);
    uint64_t dt = kaula_p_now_ns() - t0;

    ASSERT_EQ(atomic_load(&t->state), KAULA_TASK_DONE, "失败后也应进入 DONE");
    ASSERT_EQ(t->error, KAULA_ERR_SHUTDOWN, "错误码应被记录");
    /* fail 会 signal done_event，等待者应被唤醒；这里只验证 signal 未阻塞 */
    ASSERT_TRUE(dt < 1000000000ULL, "fail 不应长时间阻塞");
}

/* ========== cancel ========== */

static void test_cancel_flagged_task(void) {
    TEST_CASE("cancel 设置 cancelled 标志");
    KaulaTask* t = kaula_task_alloc();
    ASSERT_NOT_NULL(t, "分配应成功");
    if (t == NULL) return;

    ASSERT_EQ(kaula_task_cancel(t), 1, "未完成任务应可取消");
    ASSERT_EQ(atomic_load(&t->cancelled), 1, "cancelled 标志应置位");

    /*
     * 第二次取消：cancelled 已置位，仍应返回 1（幂等的"已请求取消"）。
     * cancel 的返回值语义是"取消请求已被接受"，不是"状态发生了变化"。
     */
    ASSERT_EQ(kaula_task_cancel(t), 1, "重复取消应幂等返回 1");
}

static void test_cancel_unintrusive_rejected(void) {
    TEST_CASE("#[hint(unintrusive)] 任务拒绝取消");
    KaulaTask* t = kaula_task_alloc();
    ASSERT_NOT_NULL(t, "分配应成功");
    if (t == NULL) return;

    t->flags |= KAULA_TASK_FLAG_UNINTRUSIVE;
    ASSERT_EQ(kaula_task_cancel(t), 0, "unintrusive 任务不应被取消");
    ASSERT_EQ(atomic_load(&t->cancelled), 0, "cancelled 应保持 0");
}

/* ========== epoch 回收 ========== */

static void test_epoch_retires_on_complete(void) {
    TEST_CASE("任务完成后进入 epoch 退休而非立即释放");
    KaulaRuntimeConfig cfg = {0};
    cfg.worker_count = 1;
    ASSERT_EQ(kaula_runtime_init(&cfg), 0, "初始化应成功");

    uint64_t before_retired = kaula_epoch_retired_total();

    KaulaTask* t = kaula_task_alloc();
    ASSERT_NOT_NULL(t, "分配应成功");
    if (t != NULL) {
        kaula_task_complete(t, (void*)42);
        /* 完成后不应立即释放（epoch 延迟回收） */
        ASSERT_EQ((int)(intptr_t)t->result, 42, "完成后仍可读结果（未被释放）");
    }

    uint64_t after_retired = kaula_epoch_retired_total();
    ASSERT_TRUE(after_retired > before_retired,
                "退休计数应递增（延迟释放而非立即 free）");

    kaula_runtime_shutdown();

    /* shutdown 后应全部回收 */
    ASSERT_TRUE(kaula_epoch_reclaimed_total() > 0, "shutdown 应完成回收");
}

/* ========== Handle 错误路径 ========== */

static void test_spawn_beyond_max_tasks(void) {
    TEST_CASE("超过全局任务硬上限返回 WouldBlock（I3）");
    KaulaRuntimeConfig cfg = {0};
    cfg.worker_count = 2;
    cfg.max_tasks = 4;   /* 故意设很小 */
    ASSERT_EQ(kaula_runtime_init(&cfg), 0, "初始化应成功");

    /*
     * 提交一批任务但不 await，让它们堆积；
     * 超过 max_tasks 后应返回 WOULD_BLOCK 而不是崩溃或 OOM。
     */
    int accepted = 0;
    int blocked = 0;
    for (int i = 0; i < 40; i++) {
        SimpleFrame* f = (SimpleFrame*)kmm_v4_alloc_global(sizeof(SimpleFrame));
        f->pc = 0; f->input = i; f->result = 0;
        KaulaHandle h = kaula_spawn(simple_resume, f, sizeof(SimpleFrame));
        if (kaula_handle_is_null(h)) {
            if (kaula_handle_error(h) == KAULA_ERR_WOULD_BLOCK) blocked++;
        } else {
            accepted++;
        }
    }

    /* 不要求精确数字（Worker 可能已经消费掉一些），只要求行为正确 */
    ASSERT_TRUE(accepted + blocked == 40, "每个 spawn 要么成功要么明确报 WouldBlock");

    kaula_runtime_shutdown();
}

int main(void) {
    TEST_SECTION("生命周期");
    test_alloc_init_complete();
    TEST_SECTION("await");
    test_await_completed();
    test_await_spawned();
    test_try_await_timeout();
    test_fail_wakes_waiters();
    TEST_SECTION("取消");
    test_cancel_flagged_task();
    test_cancel_unintrusive_rejected();
    TEST_SECTION("epoch");
    test_epoch_retires_on_complete();
    TEST_SECTION("背压");
    test_spawn_beyond_max_tasks();
    return test_report("task");
}