/*
 * Kaula 并发内核 · complete 打点测试
 *
 * 验证 submit_ns / complete_ns 这对时间戳。
 *
 * 这个打点存在的理由：延迟统计不能用调用方掐表。并发窗口里按
 * 「调用方何时轮到取结果」量到的是排队 + 顺序取回，与服务时间能差
 * 两个数量级 —— 实测未修时 win=1000 的 p50=3.15s 而整轮只用 26ms，
 * p50 大于总耗时，那个数根本不是延迟。
 *
 * 所以判据不是「延迟是多少」，而是「延迟与取回顺序无关」。
 *
 * 契约提醒：await_into 会从 task->result memcpy dst_size 字节，
 * 所以 result 必须是**真实地址**。用 (void*)(intptr_t)N 假指针
 * 就是段错误（kaula_handle_await 只返回地址不解引用，才可以用假指针）。
 */

#include "kaula_conc_test.h"
#include "kaula_conc.h"

#include <string.h>
#include <stdint.h>

/* 固定 4ms 定时：服务时间的下界已知，便于交叉验证 */
typedef struct {
    int      pc;
    int      calls;
    int64_t  out;      /* 结果的真实存放处 —— result 指向它 */
} StampFrame;

static KaulaResumeResult stamp_resume(void* frame_ptr) {
    StampFrame* f = (StampFrame*)frame_ptr;

    if (f->calls == 0) {
        f->calls = 1;
        f->pc = 1;

        KaulaIoRequest req;
        memset(&req, 0, sizeof(req));
        req.type = KAULA_IO_SLEEP;
        req.timeout_ns = 4000000ULL;   /* 4ms */

        KaulaResumeResult r;
        memset(&r, 0, sizeof(r));
        r.status = KAULA_RESUME_SUSPEND;
        r.io_request = req;
        r.value = NULL;
        return r;
    }

    f->out = 42;

    KaulaResumeResult r;
    memset(&r, 0, sizeof(r));
    r.status = KAULA_RESUME_DONE;
    r.value = &f->out;        /* 真实地址，不是 (void*)42 */
    return r;
}

void test_complete_timestamp(void) {
    TEST_CASE("complete 打点：非零、不早于定时器本身、与取回顺序无关");
    KaulaRuntimeConfig cfg = {0};
    cfg.worker_count = 2;
    ASSERT_EQ(kaula_runtime_init(&cfg), 0, "初始化应成功");

    const int N = 8;
    KaulaHandle hs[8];
    uint64_t lat[8];

    /* 一次性全部提交：它们并发跑，每个只睡 4ms */
    for (int i = 0; i < N; i++) {
        StampFrame* f = (StampFrame*)kmm_v4_alloc_global(sizeof(StampFrame));
        memset(f, 0, sizeof(*f));
        hs[i] = kaula_spawn(stamp_resume, f, sizeof(StampFrame));
        ASSERT_FALSE(kaula_handle_is_null(hs[i]), "spawn 应成功");
    }

    /*
     * 倒序取回：与提交顺序相反。
     *
     * 若时间戳取自调用方，倒序取回会让测出的延迟与顺序强相关；
     * 取自内核则每个任务只反映自己的服务时间。
     */
    for (int i = N - 1; i >= 0; i--) {
        int64_t out = 0;
        uint64_t svc = 0;
        kaula_handle_await_into_timed(hs[i], &out, sizeof(out), &svc);
        lat[i] = svc;
        ASSERT_EQ((int)out, 42, "任务结果应为 42");
        /* 无参访问器必须给出同一个数：基准走的就是这条路 */
        ASSERT_EQ((long long)kaula_conc_last_latency_ns(), (long long)svc,
                  "无参访问器应与 await_into_timed 给出相同的延迟");
    }

    uint64_t min_lat = ~0ULL, max_lat = 0;
    for (int i = 0; i < N; i++) {
        if (lat[i] == 0) {
            TEST_FAIL("任务 %d 的 complete 打点为 0（未打点或早于 submit）", i);
        }
        if (lat[i] < min_lat) min_lat = lat[i];
        if (lat[i] > max_lat) max_lat = lat[i];
    }
    printf("  8 个并发 4ms 任务的服务时间：min=%llu us  max=%llu us\n",
           (unsigned long long)(min_lat / 1000),
           (unsigned long long)(max_lat / 1000));

    /* 下界：不可能早于定时器本身的 4ms（否则说明时间戳跨错了任务） */
    ASSERT_TRUE(min_lat >= 4000000ULL,
                "服务时间不应短于定时器本身的 4ms");

    /*
     * 上界：并发执行下，8 个各睡 4ms 的任务应都落在几十毫秒内。
     * 取 50ms —— 远大于「4ms + 一次调度」，却远小于顺序取回算法
     * 会产生的量级（那种算法在本机给出过秒级）。
     */
    ASSERT_TRUE(max_lat < 50000000ULL,
                "并发任务服务时间应都在 50ms 内；若出现秒级落差，"
                "说明打点取错了来源");

    kaula_runtime_shutdown();
}
int main(void) {
    setvbuf(stdout, NULL, _IONBF, 0);
    setvbuf(stderr, NULL, _IONBF, 0);
    TEST_SECTION("complete 打点");
    test_complete_timestamp();
    return test_report("completion_stamp");
}
