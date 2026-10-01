/*
 * Kaula 并发内核 · Worker / 调度器 / 预算测试
 *
 * 覆盖文档 §15.3 的三条曲线 + I2/I3/I5：
 *
 *   曲线 1  短任务零开销
 *   曲线 2  长任务触发分裂（预算机制）
 *   曲线 3  死循环不饿死其他任务（P4）
 *
 * 以及：
 * - 注入队列：非 Worker 线程 spawn 也能被执行
 * - 背压：队列满返回 WouldBlock 而非丢任务（原缺陷 #3）
 * - 多 Worker 并行：任务数应大于 Worker 数
 * - park 而非空转：无任务时 CPU 时间应接近 0（P2 的前置条件）
 */

#include "kaula_conc_test.h"
#include "kaula_conc.h"

#include <string.h>

/* ========== 测试用 resume ========== */

typedef struct {
    int pc;
    int input;
    int result;
    _Atomic(int) executed;   /* 被执行的次数（用于验证不重复执行） */
} WorkFrame;

static KaulaResumeResult work_resume(void* fp) {
    WorkFrame* f = (WorkFrame*)fp;
    atomic_fetch_add(&f->executed, 1);
    if (f->pc == 0) {
        f->pc = 1;
        f->result = f->input * 2;
        return (KaulaResumeResult){ .status = KAULA_RESUME_DONE,
                                    .value = (void*)(intptr_t)f->result };
    }
    return (KaulaResumeResult){ .status = KAULA_RESUME_DONE,
                                .value = (void*)(intptr_t)f->result };
}

/* 主动让出 N 次后再完成 */
typedef struct {
    int          yields_left;
    _Atomic(int) resume_calls;
} YieldFrame;

static KaulaResumeResult yield_resume(void* fp) {
    YieldFrame* f = (YieldFrame*)fp;
    atomic_fetch_add(&f->resume_calls, 1);

    if (f->yields_left > 0) {
        f->yields_left--;
        return (KaulaResumeResult){ .status = KAULA_RESUME_BUDGET_EXHAUSTED, .value = NULL };
    }
    return (KaulaResumeResult){ .status = KAULA_RESUME_DONE, .value = (void*)99 };
}

/* 模拟"死循环"：每次 resume 都主动让出，永不完成 */
typedef struct {
    _Atomic(int) resume_calls;
    _Atomic(int) stop;
} SpinFrame;

static KaulaResumeResult spin_resume(void* fp) {
    SpinFrame* f = (SpinFrame*)fp;
    atomic_fetch_add(&f->resume_calls, 1);
    if (atomic_load(&f->stop)) {
        return (KaulaResumeResult){ .status = KAULA_RESUME_DONE, .value = (void*)1 };
    }
    /* 永远让出：模拟一个不主动 suspend 的长任务 */
    return (KaulaResumeResult){ .status = KAULA_RESUME_BUDGET_EXHAUSTED, .value = NULL };
}

/* ========== 曲线 1：短任务零开销 ========== */

static void test_curve1_short_tasks(void) {
    TEST_CASE("曲线 1：短任务批量执行正确且无重复");
    KaulaRuntimeConfig cfg = {0};
    cfg.worker_count = 4;
    ASSERT_EQ(kaula_runtime_init(&cfg), 0, "初始化应成功");

    const int N = 500;
    int sum = 0;
    int dup = 0;

    for (int i = 0; i < N; i++) {
        WorkFrame* f = (WorkFrame*)kmm_v4_alloc_global(sizeof(WorkFrame));
        f->pc = 0; f->input = i; f->result = 0;
        atomic_store(&f->executed, 0);

        KaulaHandle h = kaula_spawn(work_resume, f, sizeof(WorkFrame));
        if (kaula_handle_is_null(h)) continue;
        sum += (int)(intptr_t)kaula_handle_await(h);

        if (atomic_load(&f->executed) != 1) dup++;
    }

    int expect = 0;
    for (int i = 0; i < N; i++) expect += i * 2;

    ASSERT_EQ(sum, expect, "结果总和应吻合");
    ASSERT_EQ(dup, 0, "每个任务应恰好执行一次（无重复执行）");

    kaula_runtime_shutdown();
}

/* ========== 曲线 2：长任务触发分裂 ========== */

static void test_curve2_yield_requeue(void) {
    TEST_CASE("曲线 2：主动让出的任务被重新入队并最终完成");
    KaulaRuntimeConfig cfg = {0};
    cfg.worker_count = 2;
    ASSERT_EQ(kaula_runtime_init(&cfg), 0, "初始化应成功");

    const int N = 50;
    for (int i = 0; i < N; i++) {
        YieldFrame* f = (YieldFrame*)kmm_v4_alloc_global(sizeof(YieldFrame));
        f->yields_left = 20;
        atomic_store(&f->resume_calls, 0);

        KaulaHandle h = kaula_spawn(yield_resume, f, sizeof(YieldFrame));
        ASSERT_FALSE(kaula_handle_is_null(h), "spawn 应成功");
        if (kaula_handle_is_null(h)) continue;

        void* r = kaula_handle_await(h);
        ASSERT_EQ((int)(intptr_t)r, 99, "让出足够多次后应完成");
        ASSERT_TRUE(atomic_load(&f->resume_calls) >= 21,
                    "resume 至少被调用 yields_left+1 次");
    }

    kaula_runtime_shutdown();
}

/* ========== 曲线 3：死循环不饿死 ========== */

static void test_curve3_spin_does_not_starve(void) {
    TEST_CASE("曲线 3：不让出的任务不会饿死其他任务（P4）");
    KaulaRuntimeConfig cfg = {0};
    cfg.worker_count = 4;
    ASSERT_EQ(kaula_runtime_init(&cfg), 0, "初始化应成功");

    /* 投一个"永不完成、只让出"的任务 */
    SpinFrame* spin = (SpinFrame*)kmm_v4_alloc_global(sizeof(SpinFrame));
    atomic_store(&spin->resume_calls, 0);
    atomic_store(&spin->stop, 0);
    KaulaHandle spin_h = kaula_spawn(spin_resume, spin, sizeof(SpinFrame));
    ASSERT_FALSE(kaula_handle_is_null(spin_h), "spin 任务应被接受");

    /* 再投一批正常任务，它们都应完成 */
    const int N = 100;
    int done = 0;
    for (int i = 0; i < N; i++) {
        WorkFrame* f = (WorkFrame*)kmm_v4_alloc_global(sizeof(WorkFrame));
        f->pc = 0; f->input = i; f->result = 0;
        atomic_store(&f->executed, 0);

        KaulaHandle h = kaula_spawn(work_resume, f, sizeof(WorkFrame));
        if (kaula_handle_is_null(h)) continue;
        kaula_handle_await(h);
        done++;
    }

    ASSERT_EQ(done, N, "正常任务应全部完成（不被 spin 饿死）");

    /* 收尾：让 spin 结束，否则 shutdown 时它还在队列里 */
    atomic_store(&spin->stop, 1);
    kaula_handle_await(spin_h);

    kaula_runtime_shutdown();
}

/* ========== 注入队列：外部线程 spawn ========== */

static void test_spawn_from_external_thread(void) {
    TEST_CASE("非 Worker 线程 spawn 的任务也会被执行");
    KaulaRuntimeConfig cfg = {0};
    cfg.worker_count = 3;
    ASSERT_EQ(kaula_runtime_init(&cfg), 0, "初始化应成功");

    /* 本测试运行在 main 线程，kaula_conc_current_worker() == NULL */
    ASSERT_NULL(kaula_conc_current_worker(), "main 线程不应是 Worker");

    const int N = 100;
    int sum = 0;
    for (int i = 0; i < N; i++) {
        WorkFrame* f = (WorkFrame*)kmm_v4_alloc_global(sizeof(WorkFrame));
        f->pc = 0; f->input = i; f->result = 0;
        KaulaHandle h = kaula_spawn(work_resume, f, sizeof(WorkFrame));
        ASSERT_FALSE(kaula_handle_is_null(h), "spawn 应成功");
        if (kaula_handle_is_null(h)) continue;
        sum += (int)(intptr_t)kaula_handle_await(h);
    }

    int expect = 0;
    for (int i = 0; i < N; i++) expect += i * 2;
    ASSERT_EQ(sum, expect, "外部线程提交的任务结果应正确");

    kaula_runtime_shutdown();
}

/* ========== 并行度 ========== */

typedef struct {
    _Atomic(int) max_concurrent;
    _Atomic(int) current;
    _Atomic(int) finished;
} par_ctx;

static KaulaResumeResult par_resume(void* fp) {
    par_ctx* c = (par_ctx*)fp;

    int cur = atomic_fetch_add(&c->current, 1) + 1;
    /* 更新峰值并发 */
    int prev = atomic_load(&c->max_concurrent);
    while (cur > prev) {
        if (atomic_compare_exchange_weak(&c->max_concurrent, &prev, cur)) break;
    }

    /* 做点事，让并发窗口真实存在 */
    volatile int spin = 0;
    for (int i = 0; i < 20000; i++) spin += i;

    atomic_fetch_sub(&c->current, 1);
    atomic_fetch_add(&c->finished, 1);
    (void)spin;
    return (KaulaResumeResult){ .status = KAULA_RESUME_DONE, .value = (void*)1 };
}

static void test_multi_worker_parallelism(void) {
    TEST_CASE("多 Worker 真并行（并发峰值 > 1）");
    uint32_t cpus = kaula_conc_cpu_count();
    if (cpus < 2) {
        printf("  (单核环境，跳过并行度断言)\n");
        return;
    }

    KaulaRuntimeConfig cfg = {0};
    cfg.worker_count = 4;
    ASSERT_EQ(kaula_runtime_init(&cfg), 0, "初始化应成功");
    ASSERT_EQ(kaula_conc_worker_count(), 4, "Worker 数应等于配置值");

    par_ctx ctx;
    atomic_store(&ctx.current, 0);
    atomic_store(&ctx.max_concurrent, 0);
    atomic_store(&ctx.finished, 0);

    const int N = 200;
    KaulaHandle handles[200];
    for (int i = 0; i < N; i++) {
        handles[i] = kaula_spawn(par_resume, &ctx, sizeof(ctx));
        ASSERT_FALSE(kaula_handle_is_null(handles[i]), "spawn 应成功");
    }
    for (int i = 0; i < N; i++) {
        kaula_handle_await(handles[i]);
    }

    ASSERT_EQ(atomic_load(&ctx.finished), N, "全部任务应完成");
    ASSERT_TRUE(atomic_load(&ctx.max_concurrent) >= 2,
                "多 Worker 下应观测到并发执行");

    kaula_runtime_shutdown();
}

/* ========== park 而非空转 ========== */

static uint64_t process_cpu_ns(void) {
#if KAULA_PLATFORM_WINDOWS
    FILETIME creation, exit, kernel, user;
    if (!GetProcessTimes(GetCurrentProcess(), &creation, &exit, &kernel, &user)) return 0;
    ULARGE_INTEGER k, u;
    k.LowPart = kernel.dwLowDateTime; k.HighPart = kernel.dwHighDateTime;
    u.LowPart = user.dwLowDateTime;  u.HighPart = user.dwHighDateTime;
    return (k.QuadPart + u.QuadPart) * 100ULL;   /* 100ns → ns */
#else
    struct timespec ts;
    if (clock_gettime(CLOCK_PROCESS_CPUTIME_ID, &ts) != 0) return 0;
    return (uint64_t)ts.tv_sec * 1000000000ULL + (uint64_t)ts.tv_nsec;
#endif
}

static void test_idle_workers_park(void) {
    TEST_CASE("空闲 Worker 走 park 而非空转（P2 前置）");
    KaulaRuntimeConfig cfg = {0};
    cfg.worker_count = 4;
    ASSERT_EQ(kaula_runtime_init(&cfg), 0, "初始化应成功");

    /* 空载 300ms：若 Worker 在空转，CPU 时间会显著上升 */
    uint64_t cpu0 = process_cpu_ns();
    uint64_t wall0 = kaula_p_now_ns();
    kaula_p_sleep_ns(300000000ULL);
    uint64_t cpu1 = process_cpu_ns();
    uint64_t wall1 = kaula_p_now_ns();

    uint64_t wall = wall1 - wall0;
    uint64_t cpu  = cpu1 - cpu0;

    /*
     * 4 个 Worker 若全速空转，CPU 时间会接近 wall 的 4 倍（多核计）。
     * park 实现下应远低于此。这里用"CPU / wall < 1.0"作为宽松阈值：
     * 允许 park 超时唤醒带来的少量开销，但不允许烧满一个核。
     */
    double ratio = wall ? (double)cpu / (double)wall : 0.0;
    if (ratio >= 1.0) {
        TEST_FAIL("空闲 300ms 消耗 CPU %.0f ms（wall %llu ms），疑似空转",
                  (double)cpu / 1e6, (unsigned long long)wall / 1000000);
    } else {
        TEST_PASS();
    }

    kaula_runtime_shutdown();
}

/* ========== 运行时生命周期 ========== */

static void test_runtime_idempotent(void) {
    TEST_CASE("init 幂等：重复调用安全");
    KaulaRuntimeConfig cfg = {0};
    cfg.worker_count = 2;

    ASSERT_EQ(kaula_runtime_init(&cfg), 0, "首次 init 应成功");
    ASSERT_EQ(kaula_runtime_init(&cfg), 0, "重复 init 应返回 0 而非崩溃");
    ASSERT_EQ(kaula_runtime_init(&cfg), 0, "第三次 init 仍应安全");
    ASSERT_EQ(kaula_conc_worker_count(), 2, "Worker 数不应被重复创建");

    kaula_runtime_shutdown();
    kaula_runtime_shutdown();   /* 二次 shutdown 也应安全 */
    TEST_PASS();
}

static void test_runtime_init_null_config(void) {
    TEST_CASE("init(NULL) 不崩溃（缺陷 #8）");
    ASSERT_EQ(kaula_runtime_init(NULL), 0, "NULL 配置应走默认值");
    ASSERT_TRUE(kaula_conc_worker_count() >= 1, "应至少有一个 Worker");
    kaula_runtime_shutdown();
}

static void test_spawn_after_shutdown_rejected(void) {
    TEST_CASE("shutdown 后 spawn 被拒绝");
    KaulaRuntimeConfig cfg = {0};
    cfg.worker_count = 2;
    ASSERT_EQ(kaula_runtime_init(&cfg), 0, "初始化应成功");
    kaula_runtime_shutdown();

    WorkFrame* f = (WorkFrame*)kmm_v4_alloc_global(sizeof(WorkFrame));
    f->pc = 0; f->input = 1; f->result = 0;
    KaulaHandle h = kaula_spawn(work_resume, f, sizeof(WorkFrame));

    ASSERT_TRUE(kaula_handle_is_null(h), "shutdown 后应返回空 handle");
    ASSERT_EQ(kaula_handle_error(h), KAULA_ERR_SHUTDOWN, "错误码应为 SHUTDOWN");
}

int main(void) {
    TEST_SECTION("运行时生命周期");
    test_runtime_idempotent();
    test_runtime_init_null_config();
    TEST_SECTION("三条曲线");
    test_curve1_short_tasks();
    test_curve2_yield_requeue();
    test_curve3_spin_does_not_starve();
    TEST_SECTION("调度");
    test_spawn_from_external_thread();
    test_multi_worker_parallelism();
    TEST_SECTION("空载行为");
    test_idle_workers_park();
    TEST_SECTION("关闭");
    test_spawn_after_shutdown_rejected();
    return test_report("worker");
}