/*
 * Kaula 并发内核 · spawn 完成态不被覆盖（回归）
 *
 * 缺陷：投递与置 READY 的顺序反了。
 *   kaula_conc_worker_post(...)          把 task 交给 Worker —— 此刻已可见
 *   atomic_store(&task->state, READY)    Worker 可能已经跑完并写了 DONE，
 *                                        这一发把 DONE 覆盖回 READY
 * 完成被抹掉之后没有补救：唤醒信号是在完成那一刻发的，那时还没有等待者；
 * 等 await 注册上去看到的是 READY 不是 DONE，于是 park 到 done_event 再也醒不来。
 *
 * 判据用的是**行为**（await 能不能按时拿到结果），不去读任务帧：
 * 帧在任务退休后会被 epoch 回收并复用，读它得到的是随机值，
 * 那种"检测"会把自己变成一个假阳性制造机。
 */

#include "kaula_conc_test.h"
#include "kaula_conc.h"
#include "kaula_conc_scheduler.h"

#include <string.h>

#ifdef KAULA_PLATFORM_WINDOWS
    #ifndef WIN32_LEAN_AND_MEAN
        #define WIN32_LEAN_AND_MEAN
    #endif
    #include <windows.h>
    #include <tlhelp32.h>
#endif

typedef struct {
    int pc;
    int input;
    int result;
} Frame;

/* 任务体越短，「投递 → Worker 跑完 → 投递方置状态」这个窗口越容易被命中 */
static KaulaResumeResult fast_resume(void* fp) {
    Frame* f = (Frame*)fp;
    if (f->pc == 0) {
        f->pc = 1;
        f->result = f->input * 3 + 1;
    }
    return (KaulaResumeResult){
        .status = KAULA_RESUME_DONE,
        .value = (void*)(intptr_t)f->result
    };
}

static void* shim_call(void* fp) {
    Frame* f = (Frame*)fp;
    return (void*)(intptr_t)(f->input * 3 + 1);
}

static Frame* frame_new(int input) {
    Frame* f = (Frame*)kmm_v4_alloc_global(sizeof(Frame));
    f->pc = 0; f->input = input; f->result = -1;
    return f;
}

/*
 * 提交一批任务并逐个取回结果，返回"没按时拿到结果"的个数。
 *
 * await 超时取 200ms：正常路径是微秒级返回，200ms 只在真的丢失完成时才会用到，
 * 因此不会把"慢"误判成"丢"，也不会让用例本身被拖死。
 */
static int stranded(uint32_t rounds, uint32_t workers, int simple_form) {
    (void)workers;
    int bad = 0;
    for (uint32_t i = 0; i < rounds; i++) {
        Frame* f = frame_new((int)i);
        KaulaHandle h = simple_form
            ? kaula_spawn_simple(shim_call, f, sizeof(Frame))
            : kaula_spawn(fast_resume, f, sizeof(Frame));
        if (kaula_handle_is_null(h)) continue;

        void* r = kaula_handle_try_await(h, 200000000ULL);
        if ((int)(intptr_t)r != (int)i * 3 + 1) bad++;

        kaula_handle_release(h);
    }
    return bad;
}

static void test_completion_not_erased(void) {
    TEST_CASE("投递后 state 不得把 DONE 覆盖回 READY（await 死锁回归）");
    KaulaRuntimeConfig cfg = {0};
    cfg.worker_count = 4;
    ASSERT_EQ(kaula_runtime_init(&cfg), 0, "运行时初始化应成功");

    /* 缺陷是概率事件：实测缺陷版本在 10 万次里命中约 100 次 */    int bad = stranded(100000, cfg.worker_count, 0);
    ASSERT_EQ(bad, 0, "不应有任何一次 await 拿不到结果");

    kaula_runtime_shutdown();
}

static void test_completion_not_erased_single_form(void) {
    TEST_CASE("单一形态 spawn 同样不得抹掉 DONE");
    KaulaRuntimeConfig cfg = {0};
    cfg.worker_count = 4;
    ASSERT_EQ(kaula_runtime_init(&cfg), 0, "运行时初始化应成功");

    int bad = stranded(50000, cfg.worker_count, 1);
    ASSERT_EQ(bad, 0, "kaula_spawn_simple 不应让 await 落空");

    kaula_runtime_shutdown();
}

/*
 * 硬挂起点：把缺陷版本丢进来，这里会永久阻塞，
 * 由 run_tests.py 的超时兜底报成死锁（正是最初的现象）。
 */
static void test_await_never_strands(void) {
    TEST_CASE("spawn 后逐个 await：不得有任何一个永久等待");
    KaulaRuntimeConfig cfg = {0};
    cfg.worker_count = 4;
    ASSERT_EQ(kaula_runtime_init(&cfg), 0, "运行时初始化应成功");

    const int N = 20000;
    int wrong = 0;
    for (int i = 0; i < N; i++) {
        Frame* f = frame_new(i);
        KaulaHandle h = kaula_spawn(fast_resume, f, sizeof(Frame));
        if (kaula_handle_is_null(h)) continue;
        void* r = kaula_handle_await(h);
        if ((int)(intptr_t)r != i * 3 + 1) wrong++;
        kaula_handle_release(h);
    }
    ASSERT_EQ(wrong, 0, "每个 await 都应拿到与输入对应的结果");

    kaula_runtime_shutdown();
}

#ifdef KAULA_PLATFORM_WINDOWS
static int own_thread_count(void) {
    HANDLE s = CreateToolhelp32Snapshot(TH32CS_SNAPTHREAD, 0);
    if (s == INVALID_HANDLE_VALUE) return -1;
    DWORD me = GetCurrentProcessId();
    THREADENTRY32 e; e.dwSize = sizeof(e);
    int n = 0;
    if (Thread32First(s, &e)) {
        do { if (e.th32OwnerProcessID == me) n++; } while (Thread32Next(s, &e));
    }
    CloseHandle(s);
    return n;
}
#endif

/*
 * 关停必须把后台线程都等掉。
 *
 * 监控线程的 join 一旦漏掉（或它自己判错条件而空转），上一轮的线程就会
 * 活过本轮 shutdown —— 而它还在扫 g_conc_workers[] 并可能扩容 Worker，
 * 也就是在读正在被拆掉的运行时状态。
 */
static void test_shutdown_joins_background_threads(void) {
    TEST_CASE("多轮 init/shutdown 后不得残留后台线程");

#ifdef KAULA_PLATFORM_WINDOWS
    Sleep(50);
    int before = own_thread_count();
#endif

    for (int i = 0; i < 8; i++) {
        KaulaRuntimeConfig cfg = {0};
        cfg.worker_count = 2;
        ASSERT_EQ(kaula_runtime_init(&cfg), 0, "初始化应成功");
        kaula_runtime_shutdown();
    }

#ifdef KAULA_PLATFORM_WINDOWS
    Sleep(100);
    int after = own_thread_count();
    ASSERT_TRUE(after <= before + 1,
                "8 轮 init/shutdown 后线程数不应增长（关停没有等掉后台线程）");
#else
    TEST_PASS();
#endif
}

int main(void) {
    TEST_SECTION("完成态");
    test_completion_not_erased();
    test_completion_not_erased_single_form();

    TEST_SECTION("await");
    test_await_never_strands();

    TEST_SECTION("关停");
    test_shutdown_joins_background_threads();

    return test_report("spawn_completion");
}
