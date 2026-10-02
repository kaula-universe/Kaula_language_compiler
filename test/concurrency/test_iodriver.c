/*
 * Kaula 并发内核 · IoDriver / 阻塞车道 / 撤离 / 预算自适应测试
 *
 * 覆盖：
 * - 定时器（SUSPEND + KAULA_IO_SLEEP）：文档 §6 步骤 4
 *   原实现是空壳（立即完成），现必须是真实延迟
 * - socket 往返：epoll/kqueue/IOCP 的 suspend/resume 全链路（I4）
 * - 同 fd 多等待者：原实现 EPOLL_CTL_MOD 会覆盖掉第一个（缺陷 #6）
 * - 阻塞车道：不可插桩的阻塞操作在独立线程执行，不占用 Worker（I5）
 * - 撤离：停滞 Worker 被标记并扩容（P4）
 * - 预算自适应：量子随执行时长调整（§5.3）
 * - 可观测性：metrics 快照能反映实际发生的事（§12）
 */

#include "kaula_conc_test.h"
#include "kaula_conc.h"

#include <string.h>
#include <stdint.h>

#if KAULA_PLATFORM_WINDOWS
    #include <winsock2.h>
    #include <ws2tcpip.h>
    typedef SOCKET kaula_test_socket_t;
    #define KAULA_TEST_INVALID_SOCK INVALID_SOCKET
    #define KAULA_TEST_CLOSE_SOCK(s) closesocket(s)
#else
    #include <sys/socket.h>
    #include <netinet/in.h>
    #include <arpa/inet.h>
    #include <unistd.h>
    typedef int kaula_test_socket_t;
    #define KAULA_TEST_INVALID_SOCK (-1)
    #define KAULA_TEST_CLOSE_SOCK(s) close(s)
#endif

/* ========== 帧定义 ========== */

/* 定时器任务：挂起 delay_ns 后由 IoDriver 唤醒 */
typedef struct {
    int pc;
    int delay_ns;
    int fired;
    _Atomic(int) resume_calls;
} TimerFrame;

static KaulaResumeResult timer_resume(void* fp) {
    TimerFrame* f = (TimerFrame*)fp;
    atomic_fetch_add(&f->resume_calls, 1);

    if (f->pc == 0) {
        f->pc = 1;
        KaulaIoRequest req = {0};
        req.type = KAULA_IO_SLEEP;
        req.timeout_ns = (uint64_t)f->delay_ns;
        return (KaulaResumeResult){ .status = KAULA_RESUME_SUSPEND,
                                    .io_request = req };
    }
    f->fired = 1;
    return (KaulaResumeResult){ .status = KAULA_RESUME_DONE, .value = (void*)7 };
}

/* socket 读任务：挂起等待可读 */
typedef struct {
    int             pc;
    kaula_test_socket_t sock;
    char*           buf;
    size_t          buf_len;
    int             got;
    _Atomic(int)    resume_calls;
} SockFrame;

static KaulaResumeResult sock_resume(void* fp) {
    SockFrame* f = (SockFrame*)fp;
    atomic_fetch_add(&f->resume_calls, 1);

    if (f->pc == 0) {
        f->pc = 1;
        KaulaIoRequest req = {0};
        req.type = KAULA_IO_READ;
        req.fd = (int)f->sock;
        req.buf = f->buf;
        req.buf_len = f->buf_len;
        return (KaulaResumeResult){ .status = KAULA_RESUME_SUSPEND,
                                    .io_request = req };
    }
    /* 第二次 resume：IoDriver 已把数据读进 buf */
    size_t len = strlen(f->buf);
    f->got = (int)len;
    return (KaulaResumeResult){ .status = KAULA_RESUME_DONE,
                                .value = (void*)(intptr_t)len };
}

/* 不让出的任务：模拟一个不主动 suspend 的长计算（用于撤离测试） */
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
    return (KaulaResumeResult){ .status = KAULA_RESUME_BUDGET_EXHAUSTED, .value = NULL };
}

/*
 * 阻塞任务：模拟不可插桩的 FFI（一个会卡住的 read()）。
 *
 * 关键：第一次 resume 必须"立即"返回 BLOCKED，绝不能自己先睡。
 * 真正的阻塞发生在车道线程调用的第二次 resume 里。
 *
 * 若第一次 resume 就阻塞，那么阻塞发生在 Worker 上，
 * hint 标志根本没机会起作用 —— 测试就会测错东西。
 * （这正是我第一版写错的地方：结果 4 个任务串行成 125ms。）
 */
typedef struct {
    int pc;
} BlockFrame;

static KaulaResumeResult block_resume(void* fp) {
    BlockFrame* f = (BlockFrame*)fp;

    if (f->pc == 0) {
        /* 声明"我要阻塞"，不做任何实际工作，立刻让出 Worker */
        f->pc = 1;
        return (KaulaResumeResult){ .status = KAULA_RESUME_BLOCKED, .value = NULL };
    }

    /* 车道线程在这里真的阻塞 50ms —— 模拟不可插桩的 FFI */
    kaula_p_sleep_ns(50000000ULL);
    return (KaulaResumeResult){ .status = KAULA_RESUME_DONE, .value = (void*)123 };
}

/* ========== 定时器 ========== */

static void test_timer_suspend_resume(void) {
    TEST_CASE("定时器：SUSPEND 真实延迟后被唤醒（§6 步骤 4）");
    KaulaRuntimeConfig cfg = {0};
    cfg.worker_count = 2;
    ASSERT_EQ(kaula_runtime_init(&cfg), 0, "初始化应成功");

    TimerFrame* f = (TimerFrame*)kmm_v4_alloc_global(sizeof(TimerFrame));
    memset(f, 0, sizeof(*f));
    f->pc = 0;
    f->delay_ns = 60000000;   /* 60ms */
    atomic_store(&f->resume_calls, 0);

    uint64_t t0 = kaula_p_now_ns();
    KaulaHandle h = kaula_spawn(timer_resume, f, sizeof(TimerFrame));
    ASSERT_FALSE(kaula_handle_is_null(h), "spawn 应成功");

    void* r = kaula_handle_await(h);
    uint64_t dt = kaula_p_now_ns() - t0;

    ASSERT_EQ((int)(intptr_t)r, 7, "任务应完成");
    ASSERT_EQ(f->fired, 1, "定时器应已触发");
    ASSERT_EQ(atomic_load(&f->resume_calls), 2, "应 resume 两次（挂起 + 恢复）");

    /*
     * 原实现对 KAULA_IO_SLEEP 直接返回"立即完成"，
     * 所以延迟断言是这条用例的核心。
     */
    ASSERT_TRUE(dt >= 50000000ULL, "应真的等了约 60ms（原实现是 0ms）");
    ASSERT_TRUE(dt < 2000000000ULL, "不应远超预期");

    kaula_runtime_shutdown();
}

/*
 * 亚毫秒定时精度：守住「挂起往返的额外开销远小于一个时钟节拍」。
 *
 * 这个 bug 不崩溃、不给错结果，只是让所有定时慢约 20 倍，
 * 所以能悄悄回归 —— 别的用例全绿时它照样红。必须单独钉一条。
 *
 * 根因是 Windows 默认系统时钟粒度 15.625ms：所有带超时的等待都被
 * 向上量化到下一个节拍。修法是进程启动时 timeBeginPeriod(1)，
 * 外加亚毫秒超时折成「不等待、立即重查」。
 */
static void test_subms_timer_precision(void) {
    TEST_CASE("亚毫秒定时精度：额外开销应远小于 15.625ms 时钟节拍");
    KaulaRuntimeConfig cfg = {0};
    cfg.worker_count = 2;
    ASSERT_EQ(kaula_runtime_init(&cfg), 0, "初始化应成功");

    /* 预热：首轮含线程首次调度与缓存冷启动 */
    for (int i = 0; i < 2; i++) {
        TimerFrame* w = (TimerFrame*)kmm_v4_alloc_global(sizeof(TimerFrame));
        memset(w, 0, sizeof(*w));
        w->pc = 0;
        w->delay_ns = 200000;   /* 200us */
        KaulaHandle wh = kaula_spawn(timer_resume, w, sizeof(TimerFrame));
        if (!kaula_handle_is_null(wh)) {
            kaula_handle_await(wh);
        }
    }

    const int N = 20;
    uint64_t t0 = kaula_p_now_ns();
    for (int i = 0; i < N; i++) {
        TimerFrame* f = (TimerFrame*)kmm_v4_alloc_global(sizeof(TimerFrame));
        memset(f, 0, sizeof(*f));
        f->pc = 0;
        f->delay_ns = 200000;   /* 200us */
        KaulaHandle h = kaula_spawn(timer_resume, f, sizeof(TimerFrame));
        if (kaula_handle_is_null(h)) { TEST_FAIL("spawn 应成功"); break; }
        (void)kaula_handle_await(h);
    }
    uint64_t per = (kaula_p_now_ns() - t0) / N;
    uint64_t overhead = (per > 200000) ? (per - 200000) : 0;

    printf("  200us 定时 %d 次，平均 %llu us/次，额外开销 %llu us\n",
           N, (unsigned long long)(per / 1000), (unsigned long long)(overhead / 1000));

    /*
     * 阈值 8ms：15.625ms 的节拍是目标，8ms 留约一半余量给调度抖动，
     * 同时仍远低于一个节拍 —— 回归时必然越线。
     */
    ASSERT_TRUE(overhead < 8000000ULL,
                "亚毫秒定时额外开销应 < 8ms；若接近 15.6ms 量级，"
                "多半是系统时钟分辨率没抬（缺 timeBeginPeriod）"
                "或亚毫秒超时被折成了等待");

    kaula_runtime_shutdown();
}

static void test_many_timers(void) {
    TEST_CASE("多个定时器并发挂起都能按时唤醒");
    KaulaRuntimeConfig cfg = {0};
    cfg.worker_count = 4;
    ASSERT_EQ(kaula_runtime_init(&cfg), 0, "初始化应成功");

    const int N = 100;
    KaulaHandle hs[100];
    TimerFrame* frames[100];

    for (int i = 0; i < N; i++) {
        frames[i] = (TimerFrame*)kmm_v4_alloc_global(sizeof(TimerFrame));
        memset(frames[i], 0, sizeof(*frames[i]));
        frames[i]->pc = 0;
        frames[i]->delay_ns = 50000000ULL + (uint64_t)i * 100000ULL;  /* 50~60ms */
        atomic_store(&frames[i]->resume_calls, 0);
        hs[i] = kaula_spawn(timer_resume, frames[i], sizeof(TimerFrame));
        ASSERT_FALSE(kaula_handle_is_null(hs[i]), "spawn 应成功");
    }

    int done = 0;
    for (int i = 0; i < N; i++) {
        void* r = kaula_handle_await(hs[i]);
        if ((int)(intptr_t)r == 7) done++;
    }

    ASSERT_EQ(done, N, "所有定时器任务都应完成");
    ASSERT_EQ(kaula_iodriver_pending_count(), 0, "结束后不应残留挂起");

    kaula_runtime_shutdown();
}

/* ========== socket 往返 ========== */

/*
 * socket 往返测试。
 *
 * 目前只在 epoll / kqueue 后端上启用：
 * IOCP 是完成式模型，需要真正的 overlapped 投递，
 * 而 kaula_conc_io_backend.c 的 Windows 分支目前是简化实现
 * （只处理唤醒包，未投递 overlapped 读写）。
 * 在那之前跑这条用例只会挂住，没有诊断价值。
 *
 * 接入方式已经打通（KaulaIoRequest 的 fd/buf/buf_len 三元组
 * 与 IoDriver 的挂起表、唤醒路径都与后端无关），
 * 补齐 IOCP 分支后把这里的 #if 放开即可。
 */
#if KAULA_P_IO_EPOLL || KAULA_P_IO_KQUEUE
typedef struct {
    kaula_test_socket_t srv;
    unsigned short      port;
    kaula_test_socket_t accepted;
} accept_ctx;

static void accept_thread(void* arg) {
    accept_ctx* a = (accept_ctx*)arg;
    a->accepted = accept(a->srv, NULL, NULL);
}

static void test_socket_suspend_resume(void) {
    TEST_CASE("socket 读写：I4（提交后 Worker 立即转做别的事）");
#if KAULA_PLATFORM_WINDOWS
    WSADATA wsa;
    if (WSAStartup(MAKEWORD(2, 2), &wsa) != 0) {
        TEST_FAIL("WSAStartup 失败");
        return;
    }
#endif
    KaulaRuntimeConfig cfg = {0};
    cfg.worker_count = 2;
    ASSERT_EQ(kaula_runtime_init(&cfg), 0, "初始化应成功");

    /* 建立一对已连接的 socket */
    kaula_test_socket_t server = socket(AF_INET, SOCK_STREAM, IPPROTO_TCP);
    ASSERT_TRUE(server != KAULA_TEST_INVALID_SOCK, "创建 socket");
    if (server == KAULA_TEST_INVALID_SOCK) { WSACleanup(); return; }

    struct sockaddr_in addr;
    memset(&addr, 0, sizeof(addr));
    addr.sin_family = AF_INET;
    addr.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
    addr.sin_port = 0;

    ASSERT_EQ(bind(server, (struct sockaddr*)&addr, sizeof(addr)), 0, "bind");
    ASSERT_EQ(listen(server, 8), 0, "listen");

    int addrlen = sizeof(addr);
    getsockname(server, (struct sockaddr*)&addr, &addrlen);

    kaula_test_socket_t client = socket(AF_INET, SOCK_STREAM, IPPROTO_TCP);
    ASSERT_TRUE(client != KAULA_TEST_INVALID_SOCK, "创建 client");

    /* 先起 accept 线程（它会阻塞在 accept），再 connect */
    accept_ctx actx;
    actx.srv = server;
    actx.port = ntohs(addr.sin_port);
    actx.accepted = KAULA_TEST_INVALID_SOCK;

    kaula_p_thread_t acc_t;
    kaula_p_thread_create(&acc_t, accept_thread, &actx);

    /* connect 会等 accept 完成（内核阻塞在这里，但这是测试而非内核路径） */
    int rc = connect(client, (struct sockaddr*)&addr, sizeof(addr));
    ASSERT_EQ(rc, 0, "connect 应成功");

    kaula_p_thread_join(acc_t);
    ASSERT_TRUE(actx.accepted != KAULA_TEST_INVALID_SOCK, "accept 应成功");

    if (actx.accepted != KAULA_TEST_INVALID_SOCK) {
        /* 服务端发数据 */
        const char* msg = "hello-kernel";
        ASSERT_TRUE(send(actx.accepted, msg, (int)strlen(msg), 0) > 0, "send");

        /* 起一个读任务：挂起等可读 */
        SockFrame* f = (SockFrame*)kmm_v4_alloc_global(sizeof(SockFrame));
        memset(f, 0, sizeof(*f));
        f->pc = 0;
        f->sock = client;
        f->buf = (char*)kmm_v4_alloc_global(64);
        memset(f->buf, 0, 64);
        f->buf_len = 63;
        atomic_store(&f->resume_calls, 0);

        KaulaHandle h = kaula_spawn(sock_resume, f, sizeof(SockFrame));
        ASSERT_FALSE(kaula_handle_is_null(h), "spawn 应成功");

        void* r = kaula_handle_await(h);
        ASSERT_EQ((int)(intptr_t)r, (int)strlen("hello-kernel"), "应读到完整数据");
        ASSERT_EQ(atomic_load(&f->resume_calls), 2, "应 resume 两次");
        ASSERT_EQ(memcmp(f->buf, msg, strlen(msg)), 0, "读到的内容应一致");

        KAULA_TEST_CLOSE_SOCK(actx.accepted);
    }

    KAULA_TEST_CLOSE_SOCK(client);
    KAULA_TEST_CLOSE_SOCK(server);
    kaula_runtime_shutdown();
#if KAULA_PLATFORM_WINDOWS
    WSACleanup();
#endif
}
#else
static void test_socket_suspend_resume(void) {
    TEST_CASE("socket 读写（当前后端跳过）");
    printf("  (跳过：IOCP 后端尚未投递 overlapped 操作，见文件头注释)\n");
}
#endif

/* ========== 阻塞车道 ========== */

static void test_block_lane_isolation(void) {
    TEST_CASE("阻塞车道：阻塞操作不占用 Worker（I5）");
    KaulaRuntimeConfig cfg = {0};
    cfg.worker_count = 2;
    cfg.max_block_threads = 8;
    ASSERT_EQ(kaula_runtime_init(&cfg), 0, "初始化应成功");

/*
     * 4 个各阻塞 50ms 的任务，共 200ms 串行工作量。
     *
     * 若阻塞发生在 Worker 上，2 个 Worker 只能并行 2 个 → ≥100ms。
     * 走阻塞车道（8 线程）则应接近 50ms。
     *
     * 取多轮采样中的**最小值**而非单次值：
     * 调度抖动与机器负载只会让耗时变长、不会变短，所以 min 反映的是
     * 「系统空闲时的真实能力」。单次采样在 --stress 压测或 CI 负载下
     * 会偶发冲到 90ms 以上而误报（实测无负载稳定 59-64ms，开 12 个
     * CPU 占满进程仍是 60-64ms，可见抖动属环境噪声而非逻辑问题）。
     *
     * 关键点：这不削弱断言的鉴别力。若阻塞真的跑在 Worker 上，
     * 每一轮都会 ≥100ms，取 min 同样 ≥100ms，照样失败。只有
     * 「并行但偶尔被调度拖延」才会被 min 滤掉 —— 而那正是要容忍的噪声。
     */
    const int N = 4;
    const int ATTEMPTS = 5;
    uint64_t dt_best = UINT64_MAX;
    int all_ok = 1;
    int lane_expanded = 0;

    /*
     * 这里不走 kaula_spawn：hint 标志必须在任务被 Worker 取走之前设好，
     *  而 spawn 之后立刻置位是竞态的（Worker 可能已经开跑）。
     *
     * 编译器生成的代码不会有这个问题：#[hint(blocking)] 是静态属性，
     *  在 lowering 期就写进 spawn 的参数里。
     */
    for (int attempt = 0; attempt < ATTEMPTS; attempt++) {
        KaulaHandle hs[4];
        uint64_t spawned_before = g_conc_block_lane.total_spawned_threads;

        int submitted = 0;
        for (int i = 0; i < N; i++) {
            BlockFrame* f = (BlockFrame*)kmm_v4_alloc_global(sizeof(BlockFrame));
            if (f == NULL) { TEST_FAIL("Frame 分配失败"); all_ok = 0; break; }
            memset(f, 0, sizeof(*f));
            f->pc = 0;

            KaulaTask* t = kaula_task_alloc();
            if (t == NULL) { TEST_FAIL("Task 分配失败"); all_ok = 0; break; }
            kaula_task_init(t, block_resume, f, sizeof(BlockFrame));
            t->flags |= KAULA_TASK_FLAG_HINT_BLOCKING;
            atomic_store_explicit(&t->state, KAULA_TASK_READY, memory_order_release);
            atomic_fetch_add_explicit(&g_conc_runtime.active_task_count, 1,
                                      memory_order_relaxed);

            hs[i] = kaula_handle_from_task(t);
            if (kaula_handle_is_null(hs[i])) { TEST_FAIL("handle 应有效"); all_ok = 0; }
            if (kaula_conc_worker_post(NULL, t) != 0) { TEST_FAIL("投递应成功"); all_ok = 0; }
            submitted++;
        }

        uint64_t t0 = kaula_p_now_ns();
        for (int i = 0; i < submitted; i++) {
            void* r = kaula_handle_await(hs[i]);
            if ((int)(intptr_t)r != 123) { TEST_FAIL("阻塞任务应完成"); all_ok = 0; }
        }
        uint64_t dt = kaula_p_now_ns() - t0;

        uint64_t spawned_after = g_conc_block_lane.total_spawned_threads;
        if (spawned_after > spawned_before) lane_expanded = 1;

        if (submitted == N && dt < dt_best) dt_best = dt;
    }

    if (dt_best == UINT64_MAX) {
        TEST_FAIL("没有完成任何一轮完整计时采样");
        kaula_runtime_shutdown();
        return;
    }

    printf("  (4 个 50ms 阻塞任务 %d 轮取最优 %llu ms，串行需 200ms；车道扩容%s)\n",
           ATTEMPTS,
           (unsigned long long)(dt_best / 1000000),
           lane_expanded ? "有" : "无");

    ASSERT_TRUE(all_ok, "所有轮次的阻塞任务都应完成");
    ASSERT_TRUE(dt_best < 90000000ULL,
                "阻塞应并行完成；若阻塞跑在 Worker 上会 ≥100ms");
    ASSERT_TRUE(dt_best >= 50000000ULL, "总耗时不应短于单次阻塞时长");
    ASSERT_TRUE(lane_expanded, "并发阻塞时应动态扩容车道线程");

    /*
     * 车道线程数应随并发阻塞需求增长（动态伸缩，文档 §7.2）。
     *
     * 注意：扩容出来的线程在空闲后会自行缩容（park 超时后复查），
     * 所以断言必须紧跟提交之后做，不能等任务全跑完再看 ——
     * 那时线程可能已经缩回去了。
     */

    kaula_runtime_shutdown();
}

static void test_block_lane_direct_submit(void) {
    TEST_CASE("阻塞车道可直接提交并完成");
    KaulaRuntimeConfig cfg = {0};
    cfg.worker_count = 2;
    cfg.max_block_threads = 8;
    ASSERT_EQ(kaula_runtime_init(&cfg), 0, "初始化应成功");

    ASSERT_TRUE(kaula_block_lane_thread_count() >= KAULA_BLOCK_LANE_MIN_THREADS,
                "车道应至少有最小线程数");

    /*
     * 直接构造一个任务并投递到车道，验证车道自己的执行路径。
     * pc 从 0 开始：车道线程的第一次 resume 会拿到 BLOCKED，
     * 车道会就地继续（阻塞类任务在车道内循环跑到 Done）。
     */
    BlockFrame* f = (BlockFrame*)kmm_v4_alloc_global(sizeof(BlockFrame));
    memset(f, 0, sizeof(*f));
    f->pc = 0;

    KaulaTask* t = kaula_task_alloc();
    ASSERT_NOT_NULL(t, "Task 分配应成功");
    if (t == NULL) { kaula_runtime_shutdown(); return; }

    kaula_task_init(t, block_resume, f, sizeof(BlockFrame));
    t->flags |= KAULA_TASK_FLAG_HINT_BLOCKING;
    atomic_store(&t->state, KAULA_TASK_BLOCKED);

    ASSERT_EQ(kaula_block_lane_submit(t), 0, "车道应接受任务");

    ASSERT_EQ(kaula_task_await(t, 5000000000ULL), 1, "任务应在 5s 内完成");
    ASSERT_EQ((int)(intptr_t)t->result, 123, "结果应正确");

    kaula_runtime_shutdown();
}

/* ========== 撤离 ========== */

static void test_evict_scan(void) {
    TEST_CASE("撤离：停滞 Worker 被标记隔离（I5）");
    KaulaRuntimeConfig cfg = {0};
    cfg.worker_count = 2;
    cfg.stall_threshold_ns = 5000000ULL;   /* 5ms，便于测试 */
    cfg.enable_evict_monitor = 0;          /* 手动调 scan，避免后台线程干扰 */
    ASSERT_EQ(kaula_runtime_init(&cfg), 0, "初始化应成功");

    uint64_t before = kaula_evict_total();
    ASSERT_EQ(before, 0, "初始隔离次数应为 0");

    /* 无任务在跑时 scan 不应误报 */
    kaula_evict_scan();
    ASSERT_EQ(kaula_evict_total(), 0, "空闲时不应触发隔离");

    kaula_runtime_shutdown();
}

static void test_evict_stalled_worker(void) {
    TEST_CASE("撤离：长任务触发隔离并扩容");
    KaulaRuntimeConfig cfg = {0};
    cfg.worker_count = 2;
    cfg.stall_threshold_ns = 2000000ULL;   /* 2ms */
    cfg.enable_evict_monitor = 0;
    ASSERT_EQ(kaula_runtime_init(&cfg), 0, "初始化应成功");

    uint32_t workers_before = kaula_conc_worker_count();

    /*
     * 手工构造一个"停滞"现场：直接写 Worker 的 current_task 与起始时间。
     * 这比真的跑一个卡死循环更可控，也不依赖后台调度时序。
     */
    KaulaWorker* w = &g_conc_workers[0];
    SpinFrame spin;
    memset(&spin, 0, sizeof(spin));
    atomic_store(&spin.resume_calls, 0);
    atomic_store(&spin.stop, 1);

    KaulaTask* t = kaula_task_alloc();
    if (t == NULL) { kaula_runtime_shutdown(); TEST_FAIL("Task 分配失败"); return; }
    kaula_task_init(t, spin_resume, &spin, sizeof(spin));

    w->current_task = t;
    w->current_task_start_ns = kaula_p_now_ns() - 50000000ULL;  /* 50ms 前 */
    atomic_store_explicit(&t->state, KAULA_TASK_RUNNING, memory_order_release);

    kaula_evict_scan();

    ASSERT_EQ(atomic_load(&w->evicted), 1, "停滞 Worker 应被标记隔离");
    ASSERT_TRUE(kaula_evict_total() >= 1, "隔离计数应递增");
    ASSERT_TRUE(kaula_conc_worker_count() > workers_before,
                "应扩容一个替代 Worker");

    /* 清理现场 */
    w->current_task = NULL;
    kaula_task_complete(t, NULL);

    kaula_runtime_shutdown();
}

/* ========== 预算自适应 ========== */

static void test_budget_adaptor_converges(void) {
    TEST_CASE("预算自适应：按执行时长调整量子（§5.3）");
    KaulaBudgetAdaptor a;
    kaula_budget_adaptor_init(&a);

    int32_t initial = kaula_budget_adaptor_quantum(&a);
    ASSERT_EQ(initial, KAULA_DEFAULT_BUDGET_QUANTUM, "初始量子应为默认值");

    /* 反复喂"很快的任务"样本：单 tick 耗时极小，量子应变大 */
    for (int i = 0; i < 200; i++) {
        kaula_budget_adaptor_update(&a, 1);   /* 1ns 样本 */
    }
    int32_t after_fast = kaula_budget_adaptor_quantum(&a);

    /* 反复喂"很慢的任务"样本：量子应变小 */
    KaulaBudgetAdaptor b;
    kaula_budget_adaptor_init(&b);
    for (int i = 0; i < 200; i++) {
        kaula_budget_adaptor_update(&b, 1000000);   /* 1ms 样本 */
    }
    int32_t after_slow = kaula_budget_adaptor_quantum(&b);

    ASSERT_TRUE(after_fast > initial, "快任务应让量子变大（提高吞吐）");
    ASSERT_TRUE(after_slow < initial, "慢任务应让量子变小（降低延迟）");
    ASSERT_TRUE(after_fast > after_slow, "快慢任务的量子应有区分");
}

static void test_budget_quantum_clamped(void) {
    TEST_CASE("预算量子始终被钳制在 [MIN, MAX] 内");
    KaulaBudgetAdaptor a;
    kaula_budget_adaptor_init(&a);

    /* 极端样本：0 纳秒 → 量子应撞到上界 */
    for (int i = 0; i < 500; i++) kaula_budget_adaptor_update(&a, 0);
    int32_t hi = kaula_budget_adaptor_quantum(&a);
    ASSERT_TRUE(hi <= KAULA_BUDGET_MAX_QUANTUM, "量子不应超过 MAX");

    /* 极端样本：超长任务 → 量子应撞到下界 */
    KaulaBudgetAdaptor b;
    kaula_budget_adaptor_init(&b);
    for (int i = 0; i < 500; i++) kaula_budget_adaptor_update(&b, 10000000000ULL);
    int32_t lo = kaula_budget_adaptor_quantum(&b);
    ASSERT_TRUE(lo >= KAULA_BUDGET_MIN_QUANTUM, "量子不应低于 MIN");
}

/* ========== 可观测性 ========== */

static void test_metrics_snapshot(void) {
    TEST_CASE("metrics 快照反映实际发生的事（§12）");
    KaulaRuntimeConfig cfg = {0};
    cfg.worker_count = 2;
    cfg.enable_metrics = 1;
    ASSERT_EQ(kaula_runtime_init(&cfg), 0, "初始化应成功");

    KaulaMetrics m0;
    kaula_metrics_snapshot(&m0);
    ASSERT_EQ(m0.total_spawned, 0, "初始 spawn 数为 0");

    /* 跑 20 个任务 */
    const int N = 20;
    for (int i = 0; i < N; i++) {
        TimerFrame* f = (TimerFrame*)kmm_v4_alloc_global(sizeof(TimerFrame));
        memset(f, 0, sizeof(*f));
        f->pc = 0;
        f->delay_ns = 5000000;   /* 5ms */
        atomic_store(&f->resume_calls, 0);
        KaulaHandle h = kaula_spawn(timer_resume, f, sizeof(TimerFrame));
        if (kaula_handle_is_null(h)) continue;
        kaula_handle_await(h);
    }

    KaulaMetrics m1;
    kaula_metrics_snapshot(&m1);
    printf("  metrics: spawned=%llu completed=%llu failed=%llu io_submits=%llu timers=%llu\n",
           (unsigned long long)m1.total_spawned,
           (unsigned long long)m1.total_completed,
           (unsigned long long)m1.total_failed,
           (unsigned long long)m1.io_submits,
           (unsigned long long)m1.io_timer_fires);

    ASSERT_TRUE(m1.total_spawned >= N, "spawn 计数应至少为 N");
    ASSERT_TRUE(m1.total_completed >= N, "完成计数应至少为 N");
    ASSERT_TRUE(m1.io_submits >= N, "IO 提交计数应至少为 N");
    ASSERT_TRUE(m1.io_timer_fires >= N, "定时器触发计数应至少为 N");
    ASSERT_EQ(m1.worker_count, 2, "Worker 数应被记录");

    kaula_runtime_shutdown();
}

static void test_metrics_disabled_by_default(void) {
    TEST_CASE("metrics 默认关闭（避免热路径开销）");
    KaulaRuntimeConfig cfg = {0};
    cfg.worker_count = 1;
    cfg.enable_metrics = 0;
    ASSERT_EQ(kaula_runtime_init(&cfg), 0, "初始化应成功");

    /* 关闭时计数器不应增长（热路径完全跳过） */
    KaulaMetrics m0, m1;
    kaula_metrics_snapshot(&m0);

    for (int i = 0; i < 10; i++) {
        TimerFrame* f = (TimerFrame*)kmm_v4_alloc_global(sizeof(TimerFrame));
        memset(f, 0, sizeof(*f));
        f->pc = 0; f->delay_ns = 1000000;
        atomic_store(&f->resume_calls, 0);
        KaulaHandle h = kaula_spawn(timer_resume, f, sizeof(TimerFrame));
        if (kaula_handle_is_null(h)) continue;
        kaula_handle_await(h);
    }

    kaula_metrics_snapshot(&m1);
    ASSERT_EQ(m1.total_spawned, m0.total_spawned, "关闭时 spawn 计数不应增长");

    kaula_runtime_shutdown();
}

/* ========== 配置 ========== */

static void test_config_env_override(void) {
    TEST_CASE("环境变量 KAULA_WORKERS 覆盖 Worker 数（§12.2）");
#if KAULA_PLATFORM_WINDOWS
    SetEnvironmentVariableA("KAULA_WORKERS", "3");
#else
    setenv("KAULA_WORKERS", "3", 1);
#endif

    KaulaRuntimeConfig cfg = {0};
    cfg.worker_count = 0;   /* 让环境变量决定 */
    ASSERT_EQ(kaula_runtime_init(&cfg), 0, "初始化应成功");
    ASSERT_EQ(kaula_conc_worker_count(), 3, "Worker 数应取自环境变量");

    kaula_runtime_shutdown();

#if KAULA_PLATFORM_WINDOWS
    SetEnvironmentVariableA("KAULA_WORKERS", NULL);
#else
    unsetenv("KAULA_WORKERS");
#endif
}

static void test_config_quantum_honored(void) {
    TEST_CASE("budget_quantum 配置真正生效（缺陷 #8）");
    KaulaRuntimeConfig cfg = {0};
    cfg.worker_count = 1;
    cfg.budget_quantum = 64;   /* 非默认值 */
    ASSERT_EQ(kaula_runtime_init(&cfg), 0, "初始化应成功");

    ASSERT_EQ(g_conc_runtime.budget_quantum, 64, "全局量子应为配置值");
    ASSERT_EQ(g_conc_workers[0].budget.quantum, 64, "Worker 预算应为配置值");

    kaula_runtime_shutdown();
}

int main(void) {
    /*
     * 行缓冲：测试被超时杀掉时，已打印的用例名必须已经落到输出里，
     * 否则只能看到"卡住了"而看不到卡在哪一条。
     */
    setvbuf(stdout, NULL, _IONBF, 0);
    setvbuf(stderr, NULL, _IONBF, 0);

    TEST_SECTION("IoDriver 定时器");
    test_timer_suspend_resume();
    test_subms_timer_precision();
    test_many_timers();
    TEST_SECTION("IoDriver socket");
    test_socket_suspend_resume();
    TEST_SECTION("阻塞车道");
    test_block_lane_isolation();
    test_block_lane_direct_submit();
    TEST_SECTION("撤离");
    test_evict_scan();
    test_evict_stalled_worker();
    TEST_SECTION("预算自适应");
    test_budget_adaptor_converges();
    test_budget_quantum_clamped();
    TEST_SECTION("可观测性");
    test_metrics_snapshot();
    test_metrics_disabled_by_default();
    TEST_SECTION("配置");
    test_config_env_override();
    test_config_quantum_honored();
    return test_report("iodriver");
}