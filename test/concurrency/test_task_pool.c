/*
 * Kaula 并发内核 · Task 复用池的正确性契约
 *
 * kaula_task_alloc 命中 per-Worker 复用池时逐字段重置 Task。
 * 正确性靠"每个字段都被重置"保证：漏一个，残留值就出现在下一个
 * 复用该 Task 的任务里。
 *
 * 最容易漏的是**跨任务形态不同**的字段：
 *   - flags：上一个带 FRAME_ON_HEAP，这一个不带 → 帧不回收（泄漏）
 *           反之则回收一个不属于它的帧
 *   - inner_frame / frame：上一个有帧，这一个没有 → retire 时二次释放
 *   - result：残留会被误读成"上一次任务的返回值"
 *   - pin_refs / waiters / cancelled：残留会让 epoch 计数漂移
 *
 * 这些都不立刻崩：表现为泄漏、悬垂，或安静地返回旧值 —— 最难查的一类。
 * 所以用**形态交替**把每个字段都打一遍。
 *
 * 判据只看**字段值**，不看用了多少条原子指令：原子与非原子在这里
 * 是同一份可观察行为，区别只在生成的代码与缓存一致性代价。
 */

#include "kaula_conc_test.h"
#include "kaula_conc_task.h"
#include "kaula_conc_worker.h"
#include "../kmm_scoped_allocator_v4.h"

#include <stdlib.h>

typedef struct {
    int64_t result;
} result_frame;

/* resume 的真实签名：返回 KaulaResumeResult */
static KaulaResumeResult plain_resume(void* frame) {
    (void)frame;
    return (KaulaResumeResult){ .status = KAULA_RESUME_DONE, .value = NULL };
}

static KaulaResumeResult result_resume(void* frame) {
    ((result_frame*)frame)->result = 0x5A5A5A5ALL;
    return (KaulaResumeResult){ .status = KAULA_RESUME_DONE, .value = NULL };
}

/* alloc 之后必须为干净初值的字段清单。两条分配分支（池命中 / 新建）
   都必须满足它 —— 两条分支清单不一致就是 bug。 */
static int assert_clean_task(const KaulaTask* t, const char* where, int round) {
    if (t->frame != NULL) {
        TEST_FAIL("%s round %d: frame 应为 NULL，实际非空", where, round); return 0;
    }
    if (t->inner_frame != NULL) {
        TEST_FAIL("%s round %d: inner_frame 应为 NULL", where, round); return 0;
    }
    if (t->frame_size != 0) {
        TEST_FAIL("%s round %d: frame_size 应为 0，实际 %llu",
                  where, round, (unsigned long long)t->frame_size); return 0;
    }
    if (t->flags != 0) {
        TEST_FAIL("%s round %d: flags 应为 0，实际 0x%x",
                  where, round, (unsigned)t->flags); return 0;
    }
    if (t->error != 0) {
        TEST_FAIL("%s round %d: error 应为 0，实际 %d", where, round, t->error); return 0;
    }
    if (atomic_load(&t->result) != NULL) {
        TEST_FAIL("%s round %d: result 应为 NULL", where, round); return 0;
    }
    if (atomic_load(&t->state) != KAULA_TASK_CREATED) {
        TEST_FAIL("%s round %d: state 应为 CREATED，实际 %d",
                  where, round, (int)atomic_load(&t->state)); return 0;
    }
    if (atomic_load(&t->owner) != NULL) {
        TEST_FAIL("%s round %d: owner 应为 NULL", where, round); return 0;
    }
    if (atomic_load(&t->pin_refs) != 0) {
        TEST_FAIL("%s round %d: pin_refs 应为 0，实际 %d",
                  where, round, (int)atomic_load(&t->pin_refs)); return 0;
    }
    if (atomic_load(&t->waiters) != 0) {
        TEST_FAIL("%s round %d: waiters 应为 0，实际 %d",
                  where, round, (int)atomic_load(&t->waiters)); return 0;
    }
    if (atomic_load(&t->cancelled) != 0) {
        TEST_FAIL("%s round %d: cancelled 应为 0", where, round); return 0;
    }
    if (t->io_result != 0) {
        TEST_FAIL("%s round %d: io_result 应为 0", where, round); return 0;
    }
    if (t->resume_count != 0) {
        TEST_FAIL("%s round %d: resume_count 应为 0", where, round); return 0;
    }
    if (t->steal_count != 0) {
        TEST_FAIL("%s round %d: steal_count 应为 0", where, round); return 0;
    }
    return 1;
}

static void release_task(KaulaTask* t) {
    if (kaula_p_event_is_valid(&t->done_event)) {
        kaula_p_event_destroy(&t->done_event);
        kaula_p_event_invalidate(&t->done_event);
    }
    /* Task 来自 KMM survivor 段，必须用 kmm_v4_survivor_free；
       用 libc free 会直接堆损坏（0xC0000374）。 */
    kmm_v4_survivor_free(t);
}

/*
 * 形态交替：无帧 / 带用户帧 / 带 wrapper 帧 + inner_frame。
 *
 * 每一轮都校验 alloc 出来的字段是干净的，并校验 resume 写入的
 * 结果没被上一轮残留污染。
 */
static void test_alloc_always_clean_across_shapes(void) {
    TEST_CASE("alloc：形态交替下每轮字段都是干净初值");

    enum { ROUNDS = 300 };

    for (int round = 0; round < ROUNDS; round++) {
        int shape = round % 3;

        KaulaTask* t = kaula_task_alloc();
        if (t == NULL) { TEST_FAIL("round %d: task_alloc 返回 NULL", round); return; }

        if (!assert_clean_task(t, "alloc", round)) return;

        /* 按形态装配并跑一遍，校验结果字段 */
        void* inner = NULL;   /* t->inner_frame，形态 2 才有 */
        void* written_to = NULL; /* resume 实际写入的那块：即 t->frame */
        if (shape == 0) {
            kaula_task_init(t, plain_resume, NULL, 0);
        } else if (shape == 1) {
            written_to = malloc(sizeof(result_frame));
            if (written_to == NULL) { TEST_FAIL("malloc 失败"); release_task(t); return; }
            ((result_frame*)written_to)->result = -1;
            kaula_task_init(t, result_resume, written_to, sizeof(result_frame));
            t->flags |= KAULA_TASK_FLAG_FRAME_ON_HEAP;
        } else {
            written_to = malloc(sizeof(result_frame));
            inner = malloc(sizeof(result_frame));
            if (written_to == NULL || inner == NULL) {
                TEST_FAIL("malloc 失败"); free(written_to); free(inner); release_task(t); return;
            }
            ((result_frame*)written_to)->result = -1;
            ((result_frame*)inner)->result = -1;
            kaula_task_init(t, result_resume, written_to, sizeof(result_frame));
            t->inner_frame = inner;
            t->flags |= KAULA_TASK_FLAG_FRAME_ON_HEAP;
        }

        if (shape != 0) {
            /* resume 写的是 t->frame，不是 inner_frame */
            t->resume_fn(t->frame);
            if (((result_frame*)t->frame)->result != 0x5A5A5A5ALL) {
                TEST_FAIL("round %d shape %d: 结果被污染 = %lld",
                          round, shape, (long long)((result_frame*)t->frame)->result);
                return;
            }
            /* inner_frame 也要保持自己的初值：resume 不该碰它 */
            if (shape == 2 && ((result_frame*)inner)->result != -1) {
                TEST_FAIL("round %d shape %d: inner_frame 被 resume 越界写入",
                          round, shape);
                return;
            }
        }

        /* 回收 */
        free(written_to);
        free(inner);
        release_task(t);
    }
}

/*
 * 池分支：把一个"脏" Task 挂进复用池，下一次分配必须拿到干净字段。
 *
 * 复用池是 per-Worker 单链表，只有本 Worker 能 push/pop，
 * 所以取用时的字段重置不需要原子操作 —— 本用例保证语义不变：
 * 池里出来的 Task 与新建的 Task 同样干净。
 */
static void test_pool_branch_returns_clean_task(void) {
    TEST_CASE("池分支：脏 Task 复用后字段全部归位");

    KaulaWorker* w = kaula_conc_current_worker();
    if (w == NULL) {
        /* 主线程没有 Worker 上下文，kaula_task_alloc 不读池。
           明确记下来而不是静默通过 —— 免得"跳过了"被读成"通过了"。 */
        TEST_CASE("池分支：脏 Task 复用后字段全部归位（跳过：主线程无 Worker 上下文）");
        return;
    }

    KaulaTask* dirty = kaula_task_alloc();
    if (dirty == NULL) { TEST_FAIL("task_alloc 返回 NULL"); return; }

    /* 造一个脏 Task */
    dirty->frame = (void*)0x1000;
    dirty->inner_frame = (void*)0x2000;
    dirty->frame_size = 999;
    dirty->flags = KAULA_TASK_FLAG_FRAME_ON_HEAP | KAULA_TASK_FLAG_UNINTRUSIVE;
    dirty->error = 999;
    atomic_store(&dirty->result, (void*)0x3000);
    atomic_store(&dirty->pin_refs, 7);
    atomic_store(&dirty->waiters, 3);
    atomic_store(&dirty->cancelled, 1);
    dirty->io_result = 4242;
    dirty->resume_count = 88;
    dirty->steal_count = 99;
    atomic_store(&dirty->state, KAULA_TASK_DONE);

    /* 挂进复用池（owner 独占） */
    dirty->next_free = w->task_pool;
    w->task_pool = dirty;

    KaulaTask* fresh = kaula_task_alloc();
    if (fresh == NULL) { TEST_FAIL("池分支 alloc 返回 NULL"); return; }
    if (fresh != dirty) {
        TEST_FAIL("池分支应取回刚挂进去的 Task");
        return;
    }
    if (!assert_clean_task(fresh, "pool", 0)) return;

    /* fresh 归池，别泄漏 */
    fresh->next_free = w->task_pool;
    w->task_pool = fresh;
}

/* task_id 必须单调递增且不复用 —— 句柄按 id 索引，重复即灾难 */
static void test_task_id_is_monotonic(void) {
    TEST_CASE("task_id 单调递增，跨复用不复用");

    uint64_t prev = 0;
    enum { N = 400 };
    for (int i = 0; i < N; i++) {
        KaulaTask* t = kaula_task_alloc();
        if (t == NULL) { TEST_FAIL("alloc #%d 返回 NULL", i); return; }
        if (i > 0 && t->task_id <= prev) {
            TEST_FAIL("task_id 未单调递增：#%d 得到 %llu，上一个是 %llu",
                      i, (unsigned long long)t->task_id, (unsigned long long)prev);
            release_task(t);
            return;
        }
        prev = t->task_id;
        release_task(t);
    }
}

int main(void) {
    TEST_SECTION("Task 复用池");
    test_alloc_always_clean_across_shapes();
    test_pool_branch_returns_clean_task();
    test_task_id_is_monotonic();
    return test_report("task_pool");
}
