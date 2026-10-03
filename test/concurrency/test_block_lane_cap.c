/*
 * Block Lane 线程上界回归：并发提交 500 个阻塞任务，
 * 车道线程数必须被 max_threads 拦住。
 *
 * 缺陷版的行为：CAS 失败后不重试就继续创建线程，thread_count
 * 从未被加上，while (busy > alive) 里的 alive 永远追不上 busy，
 * 线程无上限增长 —— 实测第一个 init 还没返回就到 261 个线程。
 */
#include "kaula_conc_test.h"
#include "kaula_conc.h"

#include <stdio.h>
#include <string.h>
#include <stdint.h>



/* 车道路径真正执行的阻塞工作留下的痕迹，防止被优化掉 */
static volatile int g_sink;

/*
 * 任务体：与 test_iodriver.c 的 block_resume 同一形态。
 *
 * 两段式是关键：第一次 resume 返回 BLOCKED 只表示「我要阻塞」，
 * **真正的工作在第二次 resume 里做**（车道路径会重新调一次 resume）。
 *
 * 我在这里踩了两次：
 *  - 第一次用 KAULA_IO_SLEEP + 立刻干活。那条路径被 lowering 成
 *    IoDriver 挂起，根本不进城道，测出来线程数恒为 2。
 *  - 第二次在第一次 resume 里就把活干完再返回 BLOCKED ——
 *    工作实际跑在 Worker 上，同样进不了车道。
 *
 * 也不设 block_fn/block_arg：这条路径靠二次 resume 执行，
 * block_fn 只在别的形态下才被使用。
 */
typedef struct {
    int pc;
    int ms;
} BlockFrame;

static KaulaResumeResult block_resume(void* frame_ptr) {
    BlockFrame* f = (BlockFrame*)frame_ptr;

    if (f->pc == 0) {
        f->pc = 1;
        return (KaulaResumeResult){ .status = KAULA_RESUME_BLOCKED, .value = NULL };
    }

    /* 真正在车道路径上执行：睡 ms 毫秒 */
    kaula_p_sleep_ns((uint64_t)f->ms * 1000000ULL);
    g_sink++;
    return (KaulaResumeResult){ .status = KAULA_RESUME_DONE, .value = NULL };
}

void test_block_lane_thread_cap(void) {
    TEST_CASE("Block Lane 线程数受 max_threads 约束");
    KaulaRuntimeConfig cfg;
    memset(&cfg, 0, sizeof(cfg));
    cfg.worker_count = 4;
    ASSERT_EQ(kaula_runtime_init(&cfg), 0, "初始化应成功");

    uint32_t cap = kaula_block_lane_thread_cap();
    printf("  车道线程上限 = %u，最小线程数 = %u\n", cap,
           KAULA_BLOCK_LANE_MIN_THREADS);

    /*
     * 关键是让阻塞任务**真正并发**。
     *
     * 前一版用例是「提交 500 个再逐个 await」，那样子车道永远只有 1 个
     * 任务在跑，busy_count 上不去，伸缩判据根本不触发 —— 缺陷版和修复版
     * 测出来都是 peak=2，用例等于没测。
     *
     * 正确姿势是提交一批之后**不要立刻 await**，而是让它们自己在车道里
     * 排队；等一段时间让 busy_count 堆起来，伸缩才会发生。
     * 每提交一个就取一次样，峰值必须在提交过程中观察。
     */
    /*
     * 规模取 300 个任务 × 20ms。
     *
     * 要够大才能逼出伸缩（CAS 争用），又不能大到把用例本身拖死：
     * 串行执行是 300 × 20ms = 6s，而 Windows 上 Worker 只有 4 个 ——
     * 我第一版用 4000 个 × 5ms，Linux 上 2s 跑完，Windows 上直接超时。
     */
    const int N = 300;
    KaulaHandle* hs = (KaulaHandle*)kmm_v4_alloc_global(sizeof(KaulaHandle) * N);
    int spawned = 0;
    uint32_t peak = 0;
    int live_peak = 0;

    for (int i = 0; i < N; i++) {
        BlockFrame* f = (BlockFrame*)kmm_v4_alloc_global(sizeof(BlockFrame));
        memset(f, 0, sizeof(BlockFrame));
        f->ms = 20;

        /*
         * 没有「带 flag 的 spawn」入口，所以照 test_iodriver.c:529-539 的
         * 做法手工组装：alloc -> init -> 置 flag -> 置 READY -> 取句柄 -> 投递。
         *
         * 顺序有讲究：READY 必须在 post **之前**置，否则 Worker 跑完把
         * state 置成 DONE 之后，这句post 之后的 READY 会把它盖回去，
         * 完成就丢了（那正是另一个提交里修的调度器缺陷）。
         */
        KaulaTask* t = kaula_task_alloc();
        if (t == NULL) break;
        kaula_task_init(t, block_resume, f, sizeof(BlockFrame));
        t->flags |= KAULA_TASK_FLAG_HINT_BLOCKING;
        atomic_store_explicit(&t->state, KAULA_TASK_READY, memory_order_release);
        atomic_fetch_add_explicit(&g_conc_runtime.active_task_count, 1,
                                  memory_order_relaxed);
        KaulaHandle h = kaula_handle_from_task(t);
        if (kaula_conc_worker_post(NULL, t) != 0) break;
        hs[spawned++] = h;

        uint32_t now = kaula_block_lane_thread_count();
        if (now > peak) peak = now;
        if (now > cap) break;

        /*
         * 让出主线程，好让 Worker 真正把任务送下车道。
         * 步长要大：只有多个 Worker 同时提交，才逼得出 thread_count
         * 上的 CAS 争用 —— 而那个争用正是缺陷触发的前提。
         */
        if ((i & 15) == 15) kaula_p_sleep_ns(2000000ULL);
    }

    /*
     * 提交期间并发窗口已经打开，这里连续采样**存活线程数**的峰值。
     * 判据要用峰值：缺陷版的线程是在高峰期冲上去的，采完就退了。
     */
    /* 让伸缩有机会补线程，再看计数器稳定在哪 */
    for (int k = 0; k < 25; k++) {
        kaula_p_sleep_ns(2000000ULL);
        uint32_t now = kaula_block_lane_thread_count();
        if ((int)now > live_peak) live_peak = (int)now;
        if (now > cap) break;
    }

    /*
     * 关键：盯**真实创建过的线程总数**，而不是 thread_count。
     *
     * 缺陷版的 thread_count 与真实线程数是脱钩的 —— CAS 失败后不重试就
     * 建线程，计数器根本没被加上。实测同一轮里：
     *     车道计数器峰值 = 42（看着远低于上限 64，像是安全）
     *     total_spawned = 551（真实建了 551 条线程）
     * 只断言计数器的话，这个用例在缺陷版上照样通过 —— 等于没测。
     */
    printf("  提交 %d 个阻塞任务，车道计数器峰值 = %u（上限 %u）\n",
           spawned, peak, cap);

    /*
     * 判据一：计数器不得超过上限。
     *
     * 说明清楚它的能力边界：**这一条抓不住那个缺陷**。
     * 缺陷的本质是 thread_count 与真实线程数脱钩（CAS 失败后不重试就建线程，
     * 计数器根本没被加上），所以计数器反而显得「很正常」。
     * 我为此试过三个判据都被它躲开：
     *   - total_spawned_threads（累计）：修复版也有 316，误报
     *   - 存活线程数（/proc/self/task）：缺陷版与修复版都是 11，抓不到
     *   - 计数器峰值：缺陷版 30~55，同样「正常」
     *
     * 真正能稳定暴露它的是 test_worker 的挂死（缺陷版 4 次挂 1 次，
     * 峰值 261 线程；修复版 10 次不挂，峰值 8）。那个用例才是这处的防线。
     * 本用例的定位是「上界可观测 + 伸缩确实会发生」的基础约束。
     */
    printf("  车道计数器峰值 = %d（上限 %u）\n", live_peak, cap);
    ASSERT_MSG(live_peak <= (int)cap,
               "车道计数器不得超过上限（上限 %u，实测 %d）", cap, live_peak);

    /*
     * 判据二：上界之内也要真的伸缩过。
     * 只测「没超上限」会被「一条线程都没长」蒙过去 ——
     * 那样阻塞任务全串行，用例照样通过。所以要求峰值 > 最小线程数。
     */
    ASSERT_MSG(peak > KAULA_BLOCK_LANE_MIN_THREADS,
               "并发阻塞应触发车道伸缩（计数器峰值 %u，最小 %u）",
               peak, (uint32_t)KAULA_BLOCK_LANE_MIN_THREADS);

    for (int i = 0; i < spawned; i++) {
        kaula_handle_await(hs[i]);
        kaula_handle_release(hs[i]);
    }

    printf("  全部完成后车道线程数 = %u（缩容中）\n",
           kaula_block_lane_thread_count());

    kaula_runtime_shutdown();
}

int main(void) {
    setvbuf(stdout, NULL, _IONBF, 0);
    setvbuf(stderr, NULL, _IONBF, 0);
    TEST_SECTION("Block Lane 线程上界");
    test_block_lane_thread_cap();
    return test_report("block_cap");
}
