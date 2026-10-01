/*
 * Kaula 并发内核 · 平台抽象层测试
 *
 * 覆盖：时钟、休眠、线程、可唤醒事件原语、IO 等待句柄。
 */

#include "kaula_conc_test.h"
#include "../src/concurrency/kaula_conc_platform.h"

static void thread_add(void* arg) {
    long long* p = (long long*)arg;
    *p = 42;
}

static _Atomic(int) g_event_flag = 0;

static void thread_set_flag(void* arg) {
    (void)arg;
    atomic_store(&g_event_flag, 1);
}

static void thread_signal_event(void* arg) {
    kaula_p_event* e = (kaula_p_event*)arg;
    kaula_p_sleep_ns(10000000); /* 10ms */
    kaula_p_event_signal(e);
}

static void test_clock_monotonic(void) {
    TEST_CASE("时钟单调递增");
    uint64_t a = kaula_p_now_ns();
    uint64_t b = kaula_p_now_ns();
    ASSERT_TRUE(b >= a, "第二次读数应 >= 第一次");
    ASSERT_TRUE(a > 0, "时钟读数应 > 0");
}

static void test_sleep_advances_clock(void) {
    TEST_CASE("休眠推进时钟");
    uint64_t before = kaula_p_now_ns();
    kaula_p_sleep_ns(20000000); /* 20ms */
    uint64_t after = kaula_p_now_ns();
    uint64_t delta = after - before;
    /* 允许调度误差：至少 10ms，至多 2s */
    ASSERT_TRUE(delta >= 10000000ULL, "休眠后应至少推进 10ms");
    ASSERT_TRUE(delta < 2000000000ULL, "休眠不应超过 2s");
}

static void test_thread_create_join(void) {
    TEST_CASE("线程创建与 join");
    long long v = 0;
    kaula_p_thread_t t;
    ASSERT_EQ(kaula_p_thread_create(&t, thread_add, &v), 0, "创建线程应成功");
    ASSERT_EQ(kaula_p_thread_join(t), 0, "join 应成功");
    ASSERT_EQ(v, 42, "线程写入的值应可读");
}

static void test_event_timeout_returns_zero(void) {
    TEST_CASE("事件超时返回 0");
    kaula_p_event e;
    ASSERT_EQ(kaula_p_event_create(&e), 0, "创建事件应成功");
    uint64_t t0 = kaula_p_now_ns();
    int r = kaula_p_event_wait(&e, 20000000); /* 20ms */
    uint64_t dt = kaula_p_now_ns() - t0;
    ASSERT_EQ(r, 0, "无人 signal 时应超时返回 0");
    ASSERT_TRUE(dt >= 10000000ULL, "应真的等了约 20ms");
    kaula_p_event_destroy(&e);
}

static void test_event_signal_wakes_waiter(void) {
    TEST_CASE("事件 signal 唤醒等待者");
    kaula_p_event e;
    ASSERT_EQ(kaula_p_event_create(&e), 0, "创建事件应成功");

    kaula_p_thread_t sig;
    ASSERT_EQ(kaula_p_thread_create(&sig, thread_signal_event, &e), 0, "创建 signal 线程");

    uint64_t t0 = kaula_p_now_ns();
    int r = kaula_p_event_wait(&e, 5000000000ULL); /* 5s 上限 */
    uint64_t dt = kaula_p_now_ns() - t0;

    ASSERT_EQ(r, 1, "应被 signal 唤醒返回 1");
    ASSERT_TRUE(dt < 1000000000ULL, "应远早于 5s 超时被唤醒");

    kaula_p_thread_join(sig);
    kaula_p_event_destroy(&e);
}

static void test_event_reusable(void) {
    TEST_CASE("事件可重复使用（signal 累积）");
    kaula_p_event e;
    ASSERT_EQ(kaula_p_event_create(&e), 0, "创建事件应成功");

    /* signal 两次，等待两次 */
    kaula_p_event_signal(&e);
    kaula_p_event_signal(&e);
    ASSERT_EQ(kaula_p_event_wait(&e, 100000000ULL), 1, "第一次等待应成功");
    ASSERT_EQ(kaula_p_event_wait(&e, 100000000ULL), 1, "第二次等待应成功");
    ASSERT_EQ(kaula_p_event_wait(&e, 10000000ULL), 0, "第三次应超时");

    kaula_p_event_destroy(&e);
}

static void test_io_wait_handle(void) {
    TEST_CASE("IO 复用等待句柄");
    kaula_p_io_wait_t w;
    int rc = kaula_p_io_wait_create(&w);
    if (rc != 0) {
        TEST_FAIL("创建 IO 等待句柄失败（当前平台可能不支持）");
        return;
    }
    TEST_PASS();

    /* 未注册任何 fd 时 wake 不应崩溃 */
    ASSERT_EQ(kaula_p_io_wake(w), 0, "唤醒应成功");

    kaula_p_io_wait_destroy(&w);
    TEST_PASS();
}

int main(void) {
    TEST_SECTION("平台抽象");
    test_clock_monotonic();
    test_sleep_advances_clock();
    test_thread_create_join();
    TEST_SECTION("可唤醒事件");
    test_event_timeout_returns_zero();
    test_event_signal_wakes_waiter();
    test_event_reusable();
    TEST_SECTION("IO 复用");
    test_io_wait_handle();
    return test_report("platform");
}