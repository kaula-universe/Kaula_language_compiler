/*
 * Kaula 并发内核 · WSDeque（Chase-Lev 无锁工作窃取队列）测试
 *
 * 覆盖：单生产者 FIFO/LIFO 语义、满队列背压、owner pop 与 thief steal
 * 的竞争、外部线程并发 push（缺陷 #2 的回归测试）。
 */

#include "kaula_conc_test.h"
#include "kaula_conc_ws_deque.h"

typedef struct { int value; } dummy_task;

/* ========== 单线程语义 ========== */

static void test_push_pop_lifo(void) {
    TEST_CASE("owner push + pop 为 LIFO");
    KaulaWSDeque q;
    kaula_ws_deque_init(&q);

    dummy_task a = {1}, b = {2}, c = {3};
    ASSERT_EQ(kaula_ws_deque_push_owner(&q, (struct KaulaTask*)&a), 1, "push a");
    ASSERT_EQ(kaula_ws_deque_push_owner(&q, (struct KaulaTask*)&b), 1, "push b");
    ASSERT_EQ(kaula_ws_deque_push_owner(&q, (struct KaulaTask*)&c), 1, "push c");

    ASSERT_EQ(kaula_ws_deque_size(&q), 3, "size 应为 3");
    ASSERT_TRUE(kaula_ws_deque_pop(&q) == (struct KaulaTask*)&c, "先弹出 c（LIFO）");
    ASSERT_TRUE(kaula_ws_deque_pop(&q) == (struct KaulaTask*)&b, "再弹出 b");
    ASSERT_TRUE(kaula_ws_deque_pop(&q) == (struct KaulaTask*)&a, "最后弹出 a");
    ASSERT_NULL(kaula_ws_deque_pop(&q), "空队列 pop 返回 NULL");
}

static void test_steal_is_fifo(void) {
    TEST_CASE("thief steal 为 FIFO");
    KaulaWSDeque q;
    kaula_ws_deque_init(&q);

    dummy_task a = {1}, b = {2}, c = {3};
    kaula_ws_deque_push_owner(&q, (struct KaulaTask*)&a);
    kaula_ws_deque_push_owner(&q, (struct KaulaTask*)&b);
    kaula_ws_deque_push_owner(&q, (struct KaulaTask*)&c);

    ASSERT_TRUE(kaula_ws_deque_steal(&q) == (struct KaulaTask*)&a, "steal 拿到 a（FIFO）");
    ASSERT_TRUE(kaula_ws_deque_steal(&q) == (struct KaulaTask*)&b, "steal 拿到 b");
    ASSERT_TRUE(kaula_ws_deque_steal(&q) == (struct KaulaTask*)&c, "steal 拿到 c");
    ASSERT_NULL(kaula_ws_deque_steal(&q), "空队列 steal 返回 NULL");
}

/* ========== 批量 steal（P0） ========== */

/*
 * 批量 steal 的契约（见 kaula_conc_ws_deque.h 的 v1.3 重写说明）：
 *   - 一次调用最多拿 max_n 个最老任务（FIFO），不做「一半」启发式；
 *   - 永远给 owner 留至少 1 个：不足两项时返回 0，已拿到 ≥1 个后遇末元素也停。
 * 这两条让批量偷不会把队列清空，末元素仍走单偷/owner pop 的 CAS 仲裁。
 */
static void test_steal_batch_takes_oldest_leaves_one(void) {
    TEST_CASE("批量 steal 拿最老 N-1 个，末元素留给 owner");
    KaulaWSDeque q;
    kaula_ws_deque_init(&q);

    dummy_task a = {1}, b = {2}, c = {3}, d = {4}, e = {5}, f = {6};
    kaula_ws_deque_push_owner(&q, (struct KaulaTask*)&a);
    kaula_ws_deque_push_owner(&q, (struct KaulaTask*)&b);
    kaula_ws_deque_push_owner(&q, (struct KaulaTask*)&c);
    kaula_ws_deque_push_owner(&q, (struct KaulaTask*)&d);
    kaula_ws_deque_push_owner(&q, (struct KaulaTask*)&e);
    kaula_ws_deque_push_owner(&q, (struct KaulaTask*)&f);

    struct KaulaTask* batch[KAULA_STEAL_BATCH_MAX];
    int nb = kaula_ws_deque_steal_batch(&q, batch, KAULA_STEAL_BATCH_MAX);
    ASSERT_EQ(nb, 5, "6 个最多偷 5 个，留 1 个给 owner");
    ASSERT_TRUE(batch[0] == (struct KaulaTask*)&a &&
                batch[1] == (struct KaulaTask*)&b &&
                batch[2] == (struct KaulaTask*)&c &&
                batch[3] == (struct KaulaTask*)&d &&
                batch[4] == (struct KaulaTask*)&e, "FIFO 拿最老 a b c d e");

    /* 剩余末元素 f 归 owner：pop 仍是 LIFO */
    ASSERT_TRUE(kaula_ws_deque_pop(&q) == (struct KaulaTask*)&f, "owner 剩 f（LIFO）");
    ASSERT_NULL(kaula_ws_deque_pop(&q), "空队列 pop 返回 NULL");
}

static void test_steal_batch_leaves_last_for_single_steal(void) {
    TEST_CASE("只剩 1 个时批量返回 0，末元素留给单偷");
    KaulaWSDeque q;
    kaula_ws_deque_init(&q);

    dummy_task a = {1};
    kaula_ws_deque_push_owner(&q, (struct KaulaTask*)&a);

    struct KaulaTask* batch[KAULA_STEAL_BATCH_MAX];
    int nb = kaula_ws_deque_steal_batch(&q, batch, KAULA_STEAL_BATCH_MAX);
    ASSERT_EQ(nb, 0, "不足两项时批量不偷，返回 0");
    ASSERT_TRUE(kaula_ws_deque_steal(&q) == (struct KaulaTask*)&a,
                "单偷走末元素 CAS 仲裁，拿到 a");
    ASSERT_EQ(kaula_ws_deque_steal_batch(&q, batch, KAULA_STEAL_BATCH_MAX), 0,
              "空队列返回 0");
}

static void test_steal_batch_cap(void) {
    TEST_CASE("批量上限截断 max_out");
    KaulaWSDeque q;
    kaula_ws_deque_init(&q);

    dummy_task t = {0};
    for (int i = 0; i < 10; i++) {
        kaula_ws_deque_push_owner(&q, (struct KaulaTask*)&t);
    }
    struct KaulaTask* batch[2];
    int nb = kaula_ws_deque_steal_batch(&q, batch, 2);
    ASSERT_EQ(nb, 2, "max_out=2 截断批量");
    ASSERT_EQ(kaula_ws_deque_size(&q), 8, "剩余 8 个");
}

/* ========== 满队列背压（I3） ========== */

static void test_push_returns_false_when_full(void) {
    TEST_CASE("满队列 push 返回 0（触发背压，不静默丢弃）");
    KaulaWSDeque q;
    kaula_ws_deque_init(&q);

    dummy_task t = {0};
    int accepted = 0;
    for (int i = 0; i < KAULA_WS_DEQUE_CAPACITY; i++) {
        if (kaula_ws_deque_push_owner(&q, (struct KaulaTask*)&t)) accepted++;
    }
    ASSERT_EQ(accepted, KAULA_WS_DEQUE_CAPACITY, "应恰好接受容量个元素");

    /* 再推一个必须失败 —— 这是缺陷 #3 的核心断言 */
    ASSERT_EQ(kaula_ws_deque_push_owner(&q, (struct KaulaTask*)&t), 0,
              "满时 push_owner 必须返回 0");
    ASSERT_EQ(kaula_ws_deque_push_external(&q, (struct KaulaTask*)&t), 0,
              "满时 push_external 也必须返回 0");
    ASSERT_EQ(kaula_ws_deque_size(&q), KAULA_WS_DEQUE_CAPACITY, "size 不应越界");
}

static void test_slot_reusable_after_pop(void) {
    TEST_CASE("pop 后槽位可复用");
    KaulaWSDeque q;
    kaula_ws_deque_init(&q);

    dummy_task t = {0};
    int all_ok = 1;
    for (int i = 0; i < KAULA_WS_DEQUE_CAPACITY + 100; i++) {
        if (!kaula_ws_deque_push_owner(&q, (struct KaulaTask*)&t)) all_ok = 0;
        if (!kaula_ws_deque_pop(&q)) all_ok = 0;
    }
    ASSERT_TRUE(all_ok, "反复 push/pop 不应失败");
    ASSERT_EQ(kaula_ws_deque_size(&q), 0, "最终 size 应为 0");
}

/* ========== owner pop vs thief steal 竞争 ========== */

typedef struct {
    KaulaWSDeque q;
    int          total;          /* 入队总数 */
    _Atomic(int) popped;
    _Atomic(int) stolen;
    _Atomic(int) start;
    _Atomic(int) owner_done;     /* owner 推完并排干 */
} race_ctx;

/*
 * owner 线程：同时 push 与 pop —— 这才是 Chase-Lev 的真实用法。
 *
 * deque 只有一个 owner，它既往 bottom 端投 Task，也从 bottom 端取。
 * 早期版本把 push 与 pop 分给两个线程，等于造出"两个 owner"，
 * 违反使用契约 —— 那种情况下的测试失败说明的是测试写错了，
 * 不是实现错了。
 */
static void race_owner(void* arg) {
    race_ctx* c = (race_ctx*)arg;
    dummy_task t = {0};
    int pushed = 0;

    while (!atomic_load(&c->start)) { /* 等起跑 */ }

    for (;;) {
        /* 先尽量推 */
        while (pushed < c->total &&
               kaula_ws_deque_push_owner(&c->q, (struct KaulaTask*)&t)) {
            pushed++;
        }
        /* 再尽量取 */
        if (kaula_ws_deque_pop(&c->q)) {
            atomic_fetch_add(&c->popped, 1);
            continue;
        }
        if (pushed >= c->total) {
            /* 推完：排干残留后收工 */
            while (kaula_ws_deque_pop(&c->q)) {
                atomic_fetch_add(&c->popped, 1);
            }
            break;
        }
        /* 队列满且取不出：让 thief 帮忙 */
        kaula_p_yield();
    }
    atomic_store(&c->owner_done, 1);
}

static void race_thief(void* arg) {
    race_ctx* c = (race_ctx*)arg;
    while (!atomic_load(&c->start)) { }
    for (;;) {
        if (kaula_ws_deque_steal(&c->q)) {
            atomic_fetch_add(&c->stolen, 1);
            continue;
        }
        /*
         * thief 看到空队列时无法区分"真空"与"owner 正在推"，
         * 因此退出条件只能是 owner 宣告收工。
         */
        if (atomic_load(&c->owner_done)) break;
        kaula_p_yield();
    }
}

/* 批量 steal 的 thief：一次拿一半，stolen 按任务数累加 */
static void race_batch_thief(void* arg) {
    race_ctx* c = (race_ctx*)arg;
    while (!atomic_load(&c->start)) { }
    for (;;) {
        struct KaulaTask* batch[KAULA_STEAL_BATCH_MAX];
        int nb = kaula_ws_deque_steal_batch(&c->q, batch, KAULA_STEAL_BATCH_MAX);
        if (nb > 0) {
            atomic_fetch_add(&c->stolen, nb);
            continue;
        }
        if (atomic_load(&c->owner_done)) break;
        kaula_p_yield();
    }
}

static void test_owner_pop_vs_thief_race(void) {
    TEST_CASE("owner pop 与 thief steal 竞争不重复不丢失");
    race_ctx c;
    kaula_ws_deque_init(&c.q);
    c.total = 20000;
    atomic_store(&c.popped, 0);
    atomic_store(&c.stolen, 0);
    atomic_store(&c.start, 0);
    atomic_store(&c.owner_done, 0);

    kaula_p_thread_t thief, owner;
    kaula_p_thread_create(&owner, race_owner, &c);
    kaula_p_thread_create(&thief, race_thief, &c);

    atomic_store(&c.start, 1);
    kaula_p_thread_join(owner);
    kaula_p_thread_join(thief);

    int got = atomic_load(&c.popped) + atomic_load(&c.stolen);
    ASSERT_EQ(got, c.total, "popped + stolen 应等于总数（无重复无丢失）");
    ASSERT_EQ(kaula_ws_deque_size(&c.q), 0, "结束后队列应为空");
}

static void test_owner_pop_vs_batch_thief_race(void) {
    TEST_CASE("owner pop 与批量 steal 竞争不重复不丢失");
    race_ctx c;
    kaula_ws_deque_init(&c.q);
    c.total = 20000;
    atomic_store(&c.popped, 0);
    atomic_store(&c.stolen, 0);
    atomic_store(&c.start, 0);
    atomic_store(&c.owner_done, 0);

    /* 两个批量 thief：与 owner 同时抢，验证双读 bottom 的边界防护 */
    kaula_p_thread_t thief1, thief2, owner;
    kaula_p_thread_create(&owner, race_owner, &c);
    kaula_p_thread_create(&thief1, race_batch_thief, &c);
    kaula_p_thread_create(&thief2, race_batch_thief, &c);

    atomic_store(&c.start, 1);
    kaula_p_thread_join(owner);
    kaula_p_thread_join(thief1);
    kaula_p_thread_join(thief2);

    int got = atomic_load(&c.popped) + atomic_load(&c.stolen);
    ASSERT_EQ(got, c.total, "popped + stolen 应等于总数（无重复无丢失）");
    ASSERT_EQ(kaula_ws_deque_size(&c.q), 0, "结束后队列应为空");
}

/* ========== 外部线程并发 push（缺陷 #2 回归） ========== */

typedef struct {
    KaulaWSDeque q;
    int          per_thread;
    _Atomic(int) start;
    _Atomic(int) drained;   /* drainer 回传取到的数量 */
} xpush_ctx;

static void xpush_thread(void* arg) {
    xpush_ctx* c = (xpush_ctx*)arg;
    dummy_task t = {7};
    while (!atomic_load(&c->start)) { }
    for (int i = 0; i < c->per_thread; i++) {
        /*
         * push_external 返回 0 = 队列满。
         * 这里必须重试而不是丢弃：测试要断言"一个不丢"，
         * 若实现静默丢弃，重试也补不回来，测试会如实失败。
         */
        while (!kaula_ws_deque_push_external(&c->q, (struct KaulaTask*)&t)) {
            kaula_p_yield();
        }
    }
}

static void test_concurrent_external_push(void) {
    TEST_CASE("8 线程并发 push_external 全部可被 steal 取出");
    xpush_ctx c;
    kaula_ws_deque_init(&c.q);
    c.per_thread = 1000;
    atomic_store(&c.start, 0);
    atomic_store(&c.drained, 0);

    kaula_p_thread_t th[8];
    for (int i = 0; i < 8; i++) {
        kaula_p_thread_create(&th[i], xpush_thread, &c);
    }
    atomic_store(&c.start, 1);

    /*
     * 主线程自己当消费者（与生产线程竞争 steal），
     * 不用额外 drainer 线程 —— 少一个线程就少一类同步问题，
     * 且 drainer 与 8 个 producer 抢同一个 deque 反而掩盖竞争。
     */
    int drained = 0;
    uint64_t deadline = kaula_p_now_ns() + 30000000000ULL;
    while (drained < 8000 && kaula_p_now_ns() < deadline) {
        if (kaula_ws_deque_steal(&c.q)) { drained++; continue; }
        kaula_p_yield();
    }

    for (int i = 0; i < 8; i++) {
        kaula_p_thread_join(th[i]);
    }

    /* 生产线程可能刚好在退出前塞进最后几个，补一次 */
    while (drained < 8000) {
        if (kaula_ws_deque_steal(&c.q)) { drained++; continue; }
        break;
    }

    ASSERT_EQ(drained, 8000, "8000 个元素应全部被取出");
    ASSERT_EQ(kaula_ws_deque_size(&c.q), 0, "队列应为空");
}

int main(void) {
    TEST_SECTION("WSDeque 单线程语义");
    test_push_pop_lifo();
    test_steal_is_fifo();
    test_steal_batch_takes_oldest_leaves_one();
    test_steal_batch_leaves_last_for_single_steal();
    test_steal_batch_cap();
    TEST_SECTION("WSDeque 背压");
    test_push_returns_false_when_full();
    test_slot_reusable_after_pop();
    TEST_SECTION("WSDeque 并发");
    test_owner_pop_vs_thief_race();
    test_owner_pop_vs_batch_thief_race();
    test_concurrent_external_push();
    return test_report("ws_deque");
}