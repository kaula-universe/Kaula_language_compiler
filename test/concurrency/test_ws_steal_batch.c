/*
 * Kaula 并发内核 · 批量 work-stealing 测试（DESIGN 8.1 第 1 条）
 *
 * 覆盖两条正确性支柱 + 全部边界：
 *   - 空队列返回 0；仅 1 项时 batch 返回 0 且单任务 steal 仍正确；
 *   - 批量恰好偷到 k 个最老任务、FIFO 顺序正确、剩余数正确；
 *   - size 在 k 与 2k 之间的「偷一半」边界（n = size-1，留 1）；
 *   - size ≤ k 时恰好留 1；
 *   - 多 batch thief 并发 + owner 同时 push/pop：所有任务恰好消费一次，
 *     无丢失、无重复（checksum + consumed[] 双保险）。
 */

#include "kaula_conc_test.h"
#include "kaula_conc_ws_deque.h"

typedef struct { int value; } dummy_task;

/* ========== 单线程语义 ========== */

static void test_empty_returns_zero(void) {
    TEST_CASE("空队列批量窃取返回 0");
    KaulaWSDeque q;
    kaula_ws_deque_init(&q);
    dummy_task* out[8];
    ASSERT_EQ(kaula_ws_deque_steal_batch(&q, (struct KaulaTask**)out, 8), 0,
              "空队列 batch 返回 0");
}

static void test_single_item_no_batch(void) {
    TEST_CASE("仅 1 项时 batch 返回 0，单任务 steal 仍正确");
    KaulaWSDeque q;
    kaula_ws_deque_init(&q);

    dummy_task a = {1};
    kaula_ws_deque_push_owner(&q, (struct KaulaTask*)&a);

    dummy_task* out[8];
    ASSERT_EQ(kaula_ws_deque_steal_batch(&q, (struct KaulaTask**)out, 8), 0,
              "size=1 时 batch 必须返回 0（留单偷仲裁）");
    ASSERT_EQ(kaula_ws_deque_size(&q), 1, "队列不应被 batch 动过");
    /* 末元素仍由单任务 steal/owner pop 仲裁 */
    ASSERT_TRUE(kaula_ws_deque_steal(&q) == (struct KaulaTask*)&a,
                "单偷仍正确拿到唯一元素");
    ASSERT_EQ(kaula_ws_deque_size(&q), 0, "队列已空");
}

static void test_batch_exactly_k_fifo(void) {
    TEST_CASE("批量恰好偷 k 个最老任务，FIFO 顺序正确、剩余数正确");
    KaulaWSDeque q;
    kaula_ws_deque_init(&q);

    dummy_task a = {1}, b = {2}, c = {3}, d = {4}, e = {5};
    kaula_ws_deque_push_owner(&q, (struct KaulaTask*)&a);
    kaula_ws_deque_push_owner(&q, (struct KaulaTask*)&b);
    kaula_ws_deque_push_owner(&q, (struct KaulaTask*)&c);
    kaula_ws_deque_push_owner(&q, (struct KaulaTask*)&d);
    kaula_ws_deque_push_owner(&q, (struct KaulaTask*)&e);
    /* size=5, max_n=3 -> n=min(3, 5-1)=3，偷 a,b,c，留 d,e */

    dummy_task* out[8];
    int got = kaula_ws_deque_steal_batch(&q, (struct KaulaTask**)out, 3);
    ASSERT_EQ(got, 3, "应恰好偷 3 个");
    ASSERT_TRUE(out[0] == (struct KaulaTask*)&a &&
                out[1] == (struct KaulaTask*)&b &&
                out[2] == (struct KaulaTask*)&c,
                "批量窃取必须按 FIFO 拿最老的 a,b,c");
    ASSERT_EQ(kaula_ws_deque_size(&q), 2, "应剩余 d,e 两项");
    /* 剩下两项按 FIFO 继续由单偷/owner 消费 */
    ASSERT_TRUE(kaula_ws_deque_steal(&q) == (struct KaulaTask*)&d, "接下来偷到 d");
    ASSERT_TRUE(kaula_ws_deque_steal(&q) == (struct KaulaTask*)&e, "最后偷到 e");
}

static void test_batch_leaves_one_when_size_le_k(void) {
    TEST_CASE("size ≤ k 时批量恰好留 1 项（n = size-1）");
    KaulaWSDeque q;
    kaula_ws_deque_init(&q);

    dummy_task a = {1}, b = {2}, c = {3}, d = {4};
    kaula_ws_deque_push_owner(&q, (struct KaulaTask*)&a);
    kaula_ws_deque_push_owner(&q, (struct KaulaTask*)&b);
    kaula_ws_deque_push_owner(&q, (struct KaulaTask*)&c);
    kaula_ws_deque_push_owner(&q, (struct KaulaTask*)&d);
    /* size=4, max_n=8 -> n=min(8, 4-1)=3，偷 a,b,c，留最后 1 项 d */

    dummy_task* out[8];
    int got = kaula_ws_deque_steal_batch(&q, (struct KaulaTask**)out, 8);
    ASSERT_EQ(got, 3, "size=4 -> n=3，绝不偷整队");
    ASSERT_TRUE(out[0] == (struct KaulaTask*)&a &&
                out[1] == (struct KaulaTask*)&b &&
                out[2] == (struct KaulaTask*)&c, "FIFO 取前 3");
    ASSERT_EQ(kaula_ws_deque_size(&q), 1, "必须恰好留最后 1 项 d");
    /* 留下的 1 项由单偷收尾（batch 此时 size=1 会返回 0） */
    ASSERT_TRUE(kaula_ws_deque_steal(&q) == (struct KaulaTask*)&d, "末项 d 可单偷");
}

static void test_batch_min_capped(void) {
    TEST_CASE("size=2 时批量只偷 1、留 1");
    KaulaWSDeque q;
    kaula_ws_deque_init(&q);

    dummy_task a = {1}, b = {2};
    kaula_ws_deque_push_owner(&q, (struct KaulaTask*)&a);
    kaula_ws_deque_push_owner(&q, (struct KaulaTask*)&b);

    dummy_task* out[8];
    int got = kaula_ws_deque_steal_batch(&q, (struct KaulaTask**)out, 8);
    ASSERT_EQ(got, 1, "size=2 -> n=1");
    ASSERT_TRUE(out[0] == (struct KaulaTask*)&a, "偷到最老的 a");
    ASSERT_EQ(kaula_ws_deque_size(&q), 1, "留 b 不偷整队");
    ASSERT_TRUE(kaula_ws_deque_steal(&q) == (struct KaulaTask*)&b, "末项 b 可单偷");
}

/* ========== 多 thief 并发 + owner push/pop 压测（checksum） ========== */

#define STRESS_TASKS 200000
#define N_THIEVES    4

typedef struct {
    KaulaWSDeque   q;
    dummy_task     tasks[STRESS_TASKS];
    _Atomic(int)   consumed[STRESS_TASKS];  /* 0=未取, 1=已取；CAS 检测重复取 */
    _Atomic(int)   double_take;             /* 发现重复取任务计数 */
    _Atomic(int)   total_taken;             /* owner pop + thief 偷的总数 */
    _Atomic(int)   start;
    _Atomic(int)   owner_done;
} batch_race_ctx;

static _Atomic(int) dbg_printed;

static void mark_taken(batch_race_ctx* c, struct KaulaTask* t, const char* who) {
    int idx = (int)(((dummy_task*)t) - c->tasks);
    int tag = (who[0] == 'o') ? 1 : (strchr(who, 'b') ? 2 : 3); /* 1=owner 2=thief-batch 3=thief-single */
    int expected = 0;
    if (!atomic_compare_exchange_strong(&c->consumed[idx], &expected, tag)) {
        atomic_fetch_add(&c->double_take, 1);
        int p = atomic_fetch_add(&dbg_printed, 1);
        if (p < 12) {
            const char* first =
                expected == 1 ? "owner" :
                expected == 2 ? "thief-batch" :
                expected == 3 ? "thief-single" : "?";
            fprintf(stderr, "[DBGDOUBLE] task[%d] 首次=%s 二次=%s\n", idx, first, who);
        }
    }
    atomic_fetch_add(&c->total_taken, 1);
}

static void batch_race_owner(void* arg) {
    batch_race_ctx* c = (batch_race_ctx*)arg;
    while (!atomic_load(&c->start)) { }

    int pushed = 0;
    for (;;) {
        /* 尽量推 */
        while (pushed < STRESS_TASKS &&
               kaula_ws_deque_push_owner(&c->q,
                       (struct KaulaTask*)&c->tasks[pushed])) {
            pushed++;
        }
        /* 再尽量从 bottom pop（LIFO） */
        struct KaulaTask* t = kaula_ws_deque_pop(&c->q);
        if (t) { mark_taken(c, t, "owner"); continue; }
        if (pushed >= STRESS_TASKS) {
            while ((t = kaula_ws_deque_pop(&c->q)) != NULL) {
                mark_taken(c, t, "owner");
            }
            break;
        }
        kaula_p_yield();
    }
    atomic_store(&c->owner_done, 1);
}

static void batch_race_thief(void* arg) {
    batch_race_ctx* c = (batch_race_ctx*)arg;
    while (!atomic_load(&c->start)) { }

    for (;;) {
        struct KaulaTask* out[8];
        int got = kaula_ws_deque_steal_batch(&c->q, out, 8);
        if (got > 0) {
            for (int i = 0; i < got; i++) {
                mark_taken(c, out[i], "thief-batch");
            }
            continue;
        }
        /* batch 拿不到（空/不足两项/CAS 失败）：退回单任务 steal */
        struct KaulaTask* t = kaula_ws_deque_steal(&c->q);
        if (t) { mark_taken(c, t, "thief-single"); continue; }
        if (atomic_load(&c->owner_done)) break;
        kaula_p_yield();
    }
}

static void test_batch_race_no_loss_no_dup(void) {
    TEST_CASE("多 batch thief + owner push/pop：任务恰好消费一次");
    batch_race_ctx c;
    kaula_ws_deque_init(&c.q);
    for (int i = 0; i < STRESS_TASKS; i++) {
        c.tasks[i].value = i;
        atomic_store(&c.consumed[i], 0);
    }
    atomic_store(&c.double_take, 0);
    atomic_store(&c.total_taken, 0);
    atomic_store(&c.start, 0);
    atomic_store(&c.owner_done, 0);

    kaula_p_thread_t thieves[N_THIEVES], owner;
    kaula_p_thread_create(&owner, batch_race_owner, &c);
    for (int i = 0; i < N_THIEVES; i++) {
        kaula_p_thread_create(&thieves[i], batch_race_thief, &c);
    }

    atomic_store(&c.start, 1);
    kaula_p_thread_join(owner);
    for (int i = 0; i < N_THIEVES; i++) {
        kaula_p_thread_join(thieves[i]);
    }

    ASSERT_EQ(atomic_load(&c.double_take), 0, "绝不能双重取任务");
    ASSERT_EQ(atomic_load(&c.total_taken), STRESS_TASKS,
              "全部任务应恰好被消费一次（无丢失）");
    ASSERT_EQ(kaula_ws_deque_size(&c.q), 0, "结束后队列应为空");
}

int main(void) {
    TEST_SECTION("WSDeque 批量窃取单线程语义");
    test_empty_returns_zero();
    test_single_item_no_batch();
    test_batch_exactly_k_fifo();
    test_batch_leaves_one_when_size_le_k();
    test_batch_min_capped();
    TEST_SECTION("WSDeque 批量窃取并发压测");
    test_batch_race_no_loss_no_dup();
    return test_report("ws_steal_batch");
}
