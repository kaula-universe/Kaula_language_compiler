/*
 * Kaula 并发内核 · IoDriver shard 竞态 / 高压测试（v1.2 sharded epoll）
 *
 * 覆盖 sharded epoll 的结构性风险点：
 *   - 多 shard 线程同时收事件 / 批量注入：READ 并发风暴下 fd→shard
 *     哈希路由不串 fd、任务恰好完成一次（无丢、无重复唤醒）。
 *   - SLEEP 轮转路由：定时器分散到各 shard，全部按时完成。
 *   - 混合场景：READ + SLEEP 并发，验证两类请求在 shard 间不互相干扰。
 *
 * 规模自适应 fd hard limit：目标是 1000+ 并发连接，本地 hard=2048 时
 * 跑 (hard-256)/2 ≈ 896 连接（2000 fd 超限会直接崩，不能硬顶）。
 */

#include "kaula_conc_test.h"
#include "kaula_conc.h"

#include <string.h>
#include <stdint.h>
#include <stdio.h>

#if !KAULA_PLATFORM_WINDOWS
    #include <sys/resource.h>
    #include <unistd.h>
    #include <fcntl.h>

    #define KAULA_TEST_PIPE_ASYNC(fds)  pipe(fds)
    #define KAULA_TEST_WRITE(fd, p, n)  (ssize_t)write(fd, (p), (size_t)(n))
    #define KAULA_TEST_CLOSE_FD(fd)     close(fd)
#else
    /* Windows：命名管道 overlapped 适配复杂，storm 用例直接跳过 */
    #define KAULA_TEST_PIPE_ASYNC(fds)  (-1)
    #define KAULA_TEST_WRITE(fd, p, n)  (-1)
    #define KAULA_TEST_CLOSE_FD(fd)     ((void)0)
#endif

/* ========== 读任务（与 test_iodriver.c 同构） ========== */

typedef struct {
    int          pc;
    KaulaIoFd    fd;
    char*        buf;
    size_t       buf_len;
    int          got;
    _Atomic(int) resume_calls;
} ReadFrame;

static KaulaResumeResult read_resume(void* fp) {
    ReadFrame* f = (ReadFrame*)fp;
    atomic_fetch_add(&f->resume_calls, 1);

    if (f->pc == 0) {
        f->pc = 1;
        KaulaIoRequest req = {0};
        req.type = KAULA_IO_READ;
        req.fd = f->fd;
        req.buf = f->buf;
        req.buf_len = f->buf_len;
        return (KaulaResumeResult){ .status = KAULA_RESUME_SUSPEND,
                                    .io_request = req };
    }
    size_t len = strlen(f->buf);
    f->got = (int)len;
    return (KaulaResumeResult){ .status = KAULA_RESUME_DONE,
                                .value = (void*)(intptr_t)len };
}

/* ========== 定时任务 ========== */

typedef struct {
    int          pc;
    uint64_t     delay_ns;
    _Atomic(int) resume_calls;
} TimerFrame;

static KaulaResumeResult timer_resume(void* fp) {
    TimerFrame* f = (TimerFrame*)fp;
    atomic_fetch_add(&f->resume_calls, 1);

    if (f->pc == 0) {
        f->pc = 1;
        KaulaIoRequest req = {0};
        req.type = KAULA_IO_SLEEP;
        req.timeout_ns = f->delay_ns;
        return (KaulaResumeResult){ .status = KAULA_RESUME_SUSPEND,
                                    .io_request = req };
    }
    return (KaulaResumeResult){ .status = KAULA_RESUME_DONE, .value = (void*)7 };
}

/* ========== fd 上限自适应 ========== */

static int shard_test_conn_count(void) {
#if KAULA_PLATFORM_WINDOWS
    return 384;   /* Windows 管道句柄也算 fd，保守 */
#else
    struct rlimit rl;
    if (getrlimit(RLIMIT_NOFILE, &rl) == 0) {
        /* soft 提到 hard（不需要特权），按 hard 留余量定连接数 */
        struct rlimit up = { rl.rlim_max, rl.rlim_max };
        setrlimit(RLIMIT_NOFILE, &up);
        long hard = (long)rl.rlim_max;
        int n = (int)((hard - 256) / 2);
        if (n > 1000) n = 1000;
        if (n < 64) n = 64;
        return n;
    }
    return 384;
#endif
}

/* ========== 用例 1：READ 并发风暴 ========== */

static void test_shard_read_storm(void) {
    int N = shard_test_conn_count();
    char label[96];
    snprintf(label, sizeof(label), "shard 竞态：%d 连接并发 READ（fd→shard 路由不串）", N);
    TEST_CASE(label);

    int fds[1000][2];
    char* bufs[1000];
    ReadFrame* frames[1000];
    KaulaHandle hs[1000];

    KaulaRuntimeConfig cfg = {0};
    cfg.worker_count = 4;
    ASSERT_EQ(kaula_runtime_init(&cfg), 0, "初始化应成功");

    for (int i = 0; i < N; i++) {
        if (KAULA_TEST_PIPE_ASYNC(fds[i]) != 0) {
            TEST_FAIL("创建 pipe %d 失败", i);
            return;
        }
    }

    for (int i = 0; i < N; i++) {
        bufs[i] = (char*)kmm_v4_alloc_global(64);
        memset(bufs[i], 0, 64);
        frames[i] = (ReadFrame*)kmm_v4_alloc_global(sizeof(ReadFrame));
        memset(frames[i], 0, sizeof(ReadFrame));
        frames[i]->fd = fds[i][0];
        frames[i]->buf = bufs[i];
        frames[i]->buf_len = 63;
        hs[i] = kaula_spawn(read_resume, frames[i], sizeof(ReadFrame));
        ASSERT_FALSE(kaula_handle_is_null(hs[i]), "spawn 应成功");
    }

    /* 全部挂起后再写数据：事件并发到达各 shard */
    for (int i = 0; i < N; i++) {
        char token[32];
        snprintf(token, sizeof(token), "tok-%04d", i);
        ASSERT_EQ(KAULA_TEST_WRITE(fds[i][1], token, strlen(token)),
                  (int)strlen(token), "写入 %d");
    }

    for (int i = 0; i < N; i++) {
        void* r = kaula_handle_await(hs[i]);
        ASSERT_EQ((int)(intptr_t)r, 8, "连接应读全 token");
        char want[32];
        snprintf(want, sizeof(want), "tok-%04d", i);
        ASSERT_EQ(memcmp(bufs[i], want, 8), 0, "连接 %d token 不应串");
        ASSERT_EQ(atomic_load(&frames[i]->resume_calls), 2, "应 resume 两次");
    }

    for (int i = 0; i < N; i++) {
        KAULA_TEST_CLOSE_FD(fds[i][0]);
        KAULA_TEST_CLOSE_FD(fds[i][1]);
    }
    kaula_runtime_shutdown();
}

/* ========== 用例 2：SLEEP 并发风暴 ========== */

static void test_shard_timer_storm(void) {
    enum { N = 1024 };
    TEST_CASE("shard 竞态：1024 并发 SLEEP（轮转路由）全部按时完成");

    KaulaRuntimeConfig cfg = {0};
    cfg.worker_count = 4;
    ASSERT_EQ(kaula_runtime_init(&cfg), 0, "初始化应成功");

    TimerFrame* frames[N];
    KaulaHandle hs[N];
    for (int i = 0; i < N; i++) {
        frames[i] = (TimerFrame*)kmm_v4_alloc_global(sizeof(TimerFrame));
        memset(frames[i], 0, sizeof(TimerFrame));
        /* 5~12ms 分散：不齐射，考验各 shard 定时器链的独立 fire */
        frames[i]->delay_ns = (uint64_t)(5 + (i % 8)) * 1000000ULL;
        hs[i] = kaula_spawn(timer_resume, frames[i], sizeof(TimerFrame));
        ASSERT_FALSE(kaula_handle_is_null(hs[i]), "spawn 应成功");
    }

    for (int i = 0; i < N; i++) {
        void* r = kaula_handle_await(hs[i]);
        ASSERT_EQ((int)(intptr_t)r, 7, "sleep 应完成");
        ASSERT_EQ(atomic_load(&frames[i]->resume_calls), 2, "应 resume 两次");
    }

    kaula_runtime_shutdown();
}

/* ========== 用例 3：混合风暴 ========== */

static void test_shard_mixed_storm(void) {
    int N = shard_test_conn_count();
    enum { TS = 512 };
    char label[96];
    snprintf(label, sizeof(label),
             "shard 竞态：%d READ + %d SLEEP 混合", N, TS);
    TEST_CASE(label);

    int fds[1000][2];
    char* bufs[1000];
    ReadFrame* frames[1000];
    KaulaHandle hs[1000 + 512];
    TimerFrame* tframes[512];

    KaulaRuntimeConfig cfg = {0};
    cfg.worker_count = 4;
    ASSERT_EQ(kaula_runtime_init(&cfg), 0, "初始化应成功");

    int total = 0;
    for (int i = 0; i < N; i++) {
        if (KAULA_TEST_PIPE_ASYNC(fds[i]) != 0) {
            TEST_FAIL("创建 pipe %d 失败", i);
            return;
        }
        bufs[i] = (char*)kmm_v4_alloc_global(64);
        memset(bufs[i], 0, 64);
        frames[i] = (ReadFrame*)kmm_v4_alloc_global(sizeof(ReadFrame));
        memset(frames[i], 0, sizeof(ReadFrame));
        frames[i]->fd = fds[i][0];
        frames[i]->buf = bufs[i];
        frames[i]->buf_len = 63;
        hs[total++] = kaula_spawn(read_resume, frames[i], sizeof(ReadFrame));
    }
    for (int i = 0; i < TS; i++) {
        tframes[i] = (TimerFrame*)kmm_v4_alloc_global(sizeof(TimerFrame));
        memset(tframes[i], 0, sizeof(TimerFrame));
        tframes[i]->delay_ns = (uint64_t)(3 + (i % 10)) * 1000000ULL;
        hs[total++] = kaula_spawn(timer_resume, tframes[i], sizeof(TimerFrame));
    }

    /* READ 与 SLEEP 混合到达：写数据制造并发完成事件 */
    for (int i = 0; i < N; i++) {
        char token[32];
        snprintf(token, sizeof(token), "tok-%04d", i);
        ASSERT_EQ(KAULA_TEST_WRITE(fds[i][1], token, strlen(token)),
                  (int)strlen(token), "写入 %d");
    }

    for (int i = 0; i < N; i++) {
        void* r = kaula_handle_await(hs[i]);
        ASSERT_EQ((int)(intptr_t)r, 8, "连接应读全 token");
        char want[32];
        snprintf(want, sizeof(want), "tok-%04d", i);
        ASSERT_EQ(memcmp(bufs[i], want, 8), 0, "连接 %d token 不应串");
    }
    for (int i = N; i < total; i++) {
        void* r = kaula_handle_await(hs[i]);
        ASSERT_EQ((int)(intptr_t)r, 7, "sleep 应完成");
    }

    for (int i = 0; i < N; i++) {
        KAULA_TEST_CLOSE_FD(fds[i][0]);
        KAULA_TEST_CLOSE_FD(fds[i][1]);
    }
    kaula_runtime_shutdown();
}

/* ========== 主入口 ========== */

int main(void) {
    setvbuf(stdout, NULL, _IONBF, 0);
    setvbuf(stderr, NULL, _IONBF, 0);

    TEST_SECTION("IoDriver shard 竞态");
    test_shard_read_storm();
    test_shard_timer_storm();
    test_shard_mixed_storm();
    return test_report("iodriver_shard");
}
