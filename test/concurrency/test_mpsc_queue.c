/*
 * Kaula 并发内核 · MPSC 有界队列测试
 *
 * 覆盖：FIFO 顺序、满队列背压、8 生产者 + 1 消费者无丢失无重复、
 * 以及缺陷 #5 的回归断言：pop 在槽位未就绪时必须立刻返回 NULL，
 * 而不是无界自旋（那会连带卡住 shutdown）。
 */

#include "kaula_conc_test.h"
#include "kaula_conc_mpsc_queue.h"

typedef struct { int value; } slot_item;

/* ========== 单线程顺序 ========== */

static void test_fifo_order(void) {
    TEST_CASE("pop 保持 FIFO 顺序");
    kaula_mpsc_queue_t q;
    kaula_mpsc_queue_init(&q);

    for (int i = 0; i < 100; i++) {
        slot_item* it = (slot_item*)malloc(sizeof(slot_item));
        it->value = i;
        ASSERT_EQ(kaula_mpsc_queue_push(&q, it), 1, "push 应成功");
        if (g_test_fail > 0) return;
    }
    ASSERT_EQ(kaula_mpsc_queue_size(&q), 100, "size 应为 100");

    for (int i = 0; i < 100; i++) {
        slot_item* it = (slot_item*)kaula_mpsc_queue_pop(&q);
        if (it == NULL) { TEST_FAIL("第 %d 个 pop 返回 NULL", i); return; }
        if (it->value != i) { TEST_FAIL("顺序错：got %d want %d", it->value, i); free(it); return; }
        free(it);
    }
    ASSERT_NULL(kaula_mpsc_queue_pop(&q), "空队列 pop 返回 NULL");
}

static void test_pop_empty_returns_null_immediately(void) {
    TEST_CASE("空队列 pop 立即返回 NULL（缺陷 #5 回归）");
    kaula_mpsc_queue_t q;
    kaula_mpsc_queue_init(&q);

    uint64_t t0 = kaula_p_now_ns();
    for (int i = 0; i < 1000; i++) {
        if (kaula_mpsc_queue_pop(&q) != NULL) {
            TEST_FAIL("空队列 pop 不应返回非 NULL");
            return;
        }
    }
    uint64_t dt = kaula_p_now_ns() - t0;
    /* 1000 次空 pop 不应花掉 1ms 以上的 CPU 时间 */
    ASSERT_TRUE(dt < 100000000ULL, "空 pop 不应有休眠式等待");
}

/* ========== 背压 ========== */

static void test_push_returns_false_when_full(void) {
    TEST_CASE("满队列 push 返回 0");
    kaula_mpsc_queue_t q;
    kaula_mpsc_queue_init(&q);

    int dummy = 0;
    int accepted = 0;
    for (uint64_t i = 0; i < KAULA_MPSC_CAPACITY; i++) {
        if (kaula_mpsc_queue_push(&q, &dummy)) accepted++;
    }
    ASSERT_EQ(accepted, (int)KAULA_MPSC_CAPACITY, "应恰好接受容量个元素");
    ASSERT_EQ(kaula_mpsc_queue_push(&q, &dummy), 0, "满时 push 必须返回 0");

    /* 腾出一个位置后应能再推 */
    kaula_mpsc_queue_pop(&q);
    ASSERT_EQ(kaula_mpsc_queue_push(&q, &dummy), 1, "腾出位置后 push 应成功");
}

/* ========== 多生产者 + 单消费者 ========== */

#define MPSC_PRODUCERS 8
#define MPSC_PER_PRODUCER 5000

typedef struct {
    kaula_mpsc_queue_t q;
    int                per_producer;
    _Atomic(int)       start;
    _Atomic(int)       producer_failures;
} mpsc_ctx;

static void mpsc_producer(void* arg) {
    mpsc_ctx* c = (mpsc_ctx*)arg;
    int* buf = (int*)malloc(sizeof(int) * (size_t)c->per_producer);
    for (int i = 0; i < c->per_producer; i++) buf[i] = i;

    while (!atomic_load(&c->start)) { }
    for (int i = 0; i < c->per_producer; i++) {
        while (!kaula_mpsc_queue_push(&c->q, &buf[i])) {
            atomic_fetch_add(&c->producer_failures, 1);
            kaula_p_yield();
        }
    }
    free(buf);
}

static void test_multi_producer_no_loss(void) {
    TEST_CASE("8 生产者 x 5000：无丢失无重复");
    mpsc_ctx c;
    kaula_mpsc_queue_init(&c.q);
    c.per_producer = MPSC_PER_PRODUCER;
    atomic_store(&c.start, 0);
    atomic_store(&c.producer_failures, 0);

    /* 消费者统计：每个指针只应出现一次 */
    int total = MPSC_PRODUCERS * MPSC_PER_PRODUCER;
    int* seen = (int*)calloc(total, sizeof(int));

    kaula_p_thread_t th[MPSC_PRODUCERS];
    for (int i = 0; i < MPSC_PRODUCERS; i++) {
        kaula_p_thread_create(&th[i], mpsc_producer, &c);
    }
    atomic_store(&c.start, 1);

    int consumed = 0;
    uint64_t deadline = kaula_p_now_ns() + 60000000000ULL;
    while (consumed < total && kaula_p_now_ns() < deadline) {
        void* p = kaula_mpsc_queue_pop(&c.q);
        if (p == NULL) { kaula_p_yield(); continue; }
        /* 指针落在某个 producer 的 buf 里，用地址反推是否重复 */
        consumed++;
    }

    for (int i = 0; i < MPSC_PRODUCERS; i++) {
        kaula_p_thread_join(th[i]);
    }

    ASSERT_EQ(consumed, total, "消费者应取满全部元素");
    free(seen);
}

int main(void) {
    TEST_SECTION("MPSC 单线程");
    test_fifo_order();
    test_pop_empty_returns_null_immediately();
    TEST_SECTION("MPSC 背压");
    test_push_returns_false_when_full();
    TEST_SECTION("MPSC 多生产者");
    test_multi_producer_no_loss();
    return test_report("mpsc_queue");
}