/*
 * Kaula 并发内核（CONC）· 初始化与关闭
 *
 * 文档 §11。
 *
 * 修复记录：
 * - 原 kaula_runtime_init(config) 直接读 config->worker_count，
 *   config 为 NULL 时崩溃。现接受 NULL 并走默认值。
 * - 原实现读了 config 却从不使用 budget_quantum / max_block_threads，
 *   Worker 恒用硬编码 256，车道恒 2 线程。现真正接线。
 * - 原实现无 kaula_runtime_ensure_init()，用户代码必须手动 init，
 *   忘记就静默退化成"同步执行"。现提供 ensure + atexit 兜底。
 */

#include "kaula_conc.h"

#include "kaula_conc_task.h"
#include "kaula_conc_worker.h"
#include "kaula_conc_scheduler.h"
#include "kaula_conc_iodriver.h"
#include "kaula_conc_block_lane.h"
#include "kaula_conc_epoch.h"
#include "kaula_conc_completion.h"
#include "kaula_conc_evict.h"
#include "kaula_conc_metrics.h"

#include "../kmm_scoped_allocator_v4.h"

#include <stdlib.h>
#include <string.h>

static int g_conc_initialized = 0;
static _Atomic(int) g_conc_initing = 0;

/*
 * 关闭闩锁：shutdown 之后永不再初始化。
 *
 * 单看 g_conc_initialized 是不够的 —— shutdown 会把它清回 0，
 * 于是下一次 ensure_init 会把整套运行时重新拉起来，清掉 shutdown 标志。
 * 症状是"关掉之后再 spawn 居然成功了"，且已经 join 过的 Worker 不会被重建，
 * 任务投进没有消费者的队列 —— await 永久阻塞。
 *
 * 这个闩锁是单调的：一旦置位，进程生命周期内不再复位。
 */
static _Atomic(int) g_conc_shut_down = 0;

/* ========== 配置解析 ========== */

static uint32_t env_uint(const char* name, uint32_t fallback) {
    const char* v = kaula_p_get_env(name);
    if (v == NULL || *v == '\0') return fallback;
    char* end = NULL;
    unsigned long parsed = strtoul(v, &end, 10);
    if (end == v) return fallback;
    if (parsed > UINT32_MAX) return fallback;
    return (uint32_t)parsed;
}

/* 仅在非 NULL 的局部副本上填充默认值；绝不 memset(NULL)，也不改写用户 const 配置。 */
static void apply_defaults(KaulaRuntimeConfig* cfg) {
    if (cfg == NULL) {
        return;
    }

    if (cfg->worker_count == 0) {
        cfg->worker_count = env_uint("KAULA_WORKERS", 0);
        if (cfg->worker_count == 0) {
            cfg->worker_count = kaula_conc_cpu_count();
        }
    }
    if (cfg->budget_quantum == 0) {
        uint32_t q = env_uint("KAULA_BUDGET", KAULA_DEFAULT_BUDGET_QUANTUM);
        cfg->budget_quantum = (int32_t)q;
    }
    if (cfg->max_block_threads == 0) {
        cfg->max_block_threads = env_uint("KAULA_BLOCK_THREADS",
                                          KAULA_BLOCK_LANE_MAX_THREADS);
    }
    if (cfg->stall_threshold_ns == 0) {
        cfg->stall_threshold_ns = KAULA_DEFAULT_STALL_NS;
    }
    if (cfg->enable_evict_monitor < 0) {
        cfg->enable_evict_monitor = 1;
    }
    if (cfg->enable_metrics < 0) {
        cfg->enable_metrics = 0;
    }
    if (cfg->inject_queue_capacity == 0) {
        cfg->inject_queue_capacity = KAULA_INJECT_QUEUE_CAPACITY;
    }
    if (cfg->max_tasks == 0) {
        cfg->max_tasks = KAULA_MAX_TASKS;
    }

    /* 钳制到编译期硬上限（I3） */
    if (cfg->worker_count > KAULA_MAX_WORKERS) {
        cfg->worker_count = KAULA_MAX_WORKERS;
    }
    if (cfg->budget_quantum < KAULA_BUDGET_MIN_QUANTUM) {
        cfg->budget_quantum = KAULA_BUDGET_MIN_QUANTUM;
    }
    if (cfg->budget_quantum > KAULA_BUDGET_MAX_QUANTUM) {
        cfg->budget_quantum = KAULA_BUDGET_MAX_QUANTUM;
    }
    if (cfg->max_block_threads > KAULA_BLOCK_LANE_MAX_THREADS) {
        cfg->max_block_threads = KAULA_BLOCK_LANE_MAX_THREADS;
    }
}

/* ========== 初始化 ========== */

int kaula_runtime_init(const KaulaRuntimeConfig* config) {
    /* 幂等：文档要求重复 init 安全 */
    if (g_conc_initialized) return 0;

    /* 防止两个线程同时初始化 */
    int expected = 0;
    if (!atomic_compare_exchange_strong(&g_conc_initing, &expected, 1)) {
        while (atomic_load(&g_conc_initing)) { /* 等另一个线程完成 */ }
        return g_conc_initialized ? 0 : -1;
    }

    /* 关键修复：config 可为 NULL；先拷到局部 cfg，再在局部上填默认值。
     * 绝不就地改写用户 const 配置，也不 memset(NULL)。 */
    KaulaRuntimeConfig cfg;
    if (config != NULL) {
        cfg = *config;
    } else {
        memset(&cfg, 0, sizeof(cfg));
    }
    apply_defaults(&cfg);

    /* 读取 Task 复用池高水位（环境变量可配，init 时读一次） */
    kaula_task_pool_high_water_init();

    /* ---- 全局状态 ---- */
    g_conc_runtime.worker_count = cfg.worker_count;
    g_conc_runtime.budget_quantum = cfg.budget_quantum;
    g_conc_runtime.max_block_threads = cfg.max_block_threads;
    g_conc_runtime.stall_threshold_ns = cfg.stall_threshold_ns;
    g_conc_runtime.enable_evict_monitor = cfg.enable_evict_monitor;
    g_conc_runtime.max_tasks = cfg.max_tasks;
    /* 批量窃取批量大小：环境变量 KAULA_STEAL_BATCH，init 时读一次缓存。
     * 默认 8；0 = 关闭批量、全走单任务 steal（不重编即可 A/B 隔离 k=1/4/8）。 */
    {
        uint32_t batch = 8;
        const char* env = kaula_p_get_env("KAULA_STEAL_BATCH");
        if (env && *env) {
            long v = strtol(env, NULL, 10);
            if (v < 0) v = 0;
            batch = (uint32_t)v;
        }
        g_conc_runtime.steal_batch_max = batch;
    }
    /* busy-wait 低延迟模式：park前轮询次数。
     * 环境变量 KAULA_BUSY_WAIT_SPINS 或 config.busy_wait_spins；
     * 0 = 不忙等（默认park，省CPU）；>0 = 低延迟模式（耗CPU）。 */
    {
        uint32_t spins = cfg.busy_wait_spins;
        const char* env = kaula_p_get_env("KAULA_BUSY_WAIT_SPINS");
        if (env && *env) {
            long v = strtol(env, NULL, 10);
            if (v < 0) v = 0;
            spins = (uint32_t)v;
        }
        g_conc_runtime.busy_wait_spins = spins;
    }
    atomic_store_explicit(&g_conc_runtime.shutdown, 0, memory_order_relaxed);
    atomic_store_explicit(&g_conc_runtime.active_task_count, 0, memory_order_relaxed);
    atomic_store_explicit(&g_conc_runtime.next_task_id, 1, memory_order_relaxed);

    kaula_metrics_enable(cfg.enable_metrics);

    /* ---- KMM ---- */
    kmm_v4_init_pool(0);

    /* ---- 基础结构 ---- */
    kaula_inject_queue_init();
    kaula_completion_init();
    kaula_epoch_init();
    kaula_evict_init();

    /* ---- IoDriver ---- */
    if (kaula_iodriver_init() != 0) goto fail_early;

    /* ---- Block Lane ---- */
    if (kaula_block_lane_init(cfg.max_block_threads) != 0) {
        kaula_iodriver_destroy();
        goto fail_early;
    }

    /* ---- Worker（两阶段创建，初始化竞态修复）---- */
    /* 阶段 1：先初始化全部 Worker 结构，任何线程启动前完成，
     * 否则 worker[0] 的线程一启动就会 steal 读 worker[1..n]
     * 尚未初始化的本地队列（未初始化读 + 可能窃取垃圾指针）。 */
    for (uint32_t i = 0; i < cfg.worker_count; i++) {
        if (kaula_worker_prepare(&g_conc_workers[i], i) != 0) {
            for (uint32_t j = 0; j < i; j++) {
                kaula_p_event_destroy(&g_conc_workers[j].park_event);
            }
            kaula_block_lane_destroy();
            kaula_iodriver_destroy();
            goto fail_early;
        }
    }
    /* 阶段 2：全部结构就绪后统一启动线程 */
    for (uint32_t i = 0; i < cfg.worker_count; i++) {
        if (kaula_worker_launch(&g_conc_workers[i]) != 0) {
            for (uint32_t j = 0; j < cfg.worker_count; j++) {
                kaula_worker_stop(&g_conc_workers[j]);
                kaula_worker_join(&g_conc_workers[j]);
            }
            kaula_block_lane_destroy();
            kaula_iodriver_destroy();
            goto fail_early;
        }
    }

    /*
     * ---- 后台线程 ----
     *
     * 先抬时钟分辨率再起线程：Worker park 超时是 1ms、IoDriver 批等待
     * 也会用到亚 15.6ms 的超时，不抬的话每次挂起往返多付约 20ms 固定税
     * （见 kaula_conc_platform.h 的说明）。放在线程起来之前，
     * 免得第一批任务就吃到未抬分辨率的节拍。
     */
    kaula_p_timer_resolution_raise();

    if (kaula_iodriver_start() != 0) {
        for (uint32_t i = 0; i < cfg.worker_count; i++) {
            kaula_worker_stop(&g_conc_workers[i]);
            kaula_worker_join(&g_conc_workers[i]);
        }
        kaula_block_lane_destroy();
        kaula_iodriver_destroy();
        goto fail_early;
    }

    if (kaula_block_lane_start() != 0) {
        kaula_iodriver_stop();
        kaula_iodriver_join();
        for (uint32_t i = 0; i < cfg.worker_count; i++) {
            kaula_worker_stop(&g_conc_workers[i]);
            kaula_worker_join(&g_conc_workers[i]);
        }
        kaula_block_lane_destroy();
        kaula_iodriver_destroy();
        goto fail_early;
    }

    if (kaula_evict_start() != 0) {
        /* 撤离监控是可选增强，失败不致命 */
        /* 继续运行，靠 L1 预算保障公平性 */
    }

    g_conc_initialized = 1;
    atomic_store(&g_conc_initing, 0);
    return 0;

fail_early:
    atomic_store(&g_conc_initing, 0);
    return -1;
}

/*
 * 确保运行时已初始化。
 * 首次调用走默认配置；用户后续显式 init 会被忽略（幂等）。
 */
int kaula_runtime_ensure_init(void) {
    if (atomic_load_explicit(&g_conc_shut_down, memory_order_acquire)) {
        return -1;
    }
    if (g_conc_initialized) return 0;
    return kaula_runtime_init(NULL);
}

int kaula_runtime_is_initialized(void) {
    return g_conc_initialized;
}

/* 诊断插桩（临时）：await_into 路径计数（定义在 kaula_conc_task.c） */
extern _Atomic(uint64_t) g_dbg_ai_null;
extern _Atomic(uint64_t) g_dbg_ai_awaitfail;
extern _Atomic(uint64_t) g_dbg_ai_ok;

/* ========== 关闭 ========== */

void kaula_runtime_shutdown(void) {
    if (!g_conc_initialized) return;

    /* 1. 置关闭标记：不再接受新 spawn，且此后不允许再初始化 */
    atomic_store_explicit(&g_conc_runtime.shutdown, 1, memory_order_release);
    atomic_store_explicit(&g_conc_shut_down, 1, memory_order_release);

    /*
     * 还原时钟分辨率。
     *
     * 放在停后台线程之前：驱动线程停止时还要做最后一轮定时器复查，
     * 那一步仍会用到带超时的等待。进程级设置，不能泄漏到用户后续逻辑。
     */
    kaula_p_timer_resolution_restore();

    /* 2. 停撤离监控并等它退出。
     *    join 不能省：监控线程会扫 g_conc_workers[] 并可能扩容 Worker，
     *    不等它退出就往下拆运行时状态，那边正在读的就是刚被拆的内存。 */
    kaula_evict_stop();
    kaula_evict_join();

    /* 3. 唤醒所有 Worker，让它们看到标志并退出 */
    kaula_scheduler_notify_all();
    for (uint32_t i = 0; i < g_conc_runtime.worker_count; i++) {
        kaula_worker_stop(&g_conc_workers[i]);
    }

    /* 4. 等 Worker 退出。它们会把残留 Task 转入注入队列，
     *    因此这里不会丢任务。 */
    for (uint32_t i = 0; i < g_conc_runtime.worker_count; i++) {
        kaula_worker_join(&g_conc_workers[i]);
    }

    /* 4.5 清空各 Worker 的 Task 复用池（P0-2）。
     *    池里的 Task 均已 complete/fail 且经 epoch 判定安全，
     *    现在单线程上下文，直接销毁 event 并释放。 */
    for (uint32_t i = 0; i < g_conc_runtime.worker_count; i++) {
        KaulaWorker* w = &g_conc_workers[i];
        KaulaTask* t = w->task_pool;
        while (t != NULL) {
            KaulaTask* nx = t->next_free;
            if (kaula_p_event_is_valid(&t->done_event)) {
                kaula_p_event_destroy(&t->done_event);
                kaula_p_event_invalidate(&t->done_event);
            }
            kmm_v4_survivor_free(t);
            t = nx;
        }
        w->task_pool = NULL;
    }

    /* 5. 停 IoDriver：残留挂起任务被标 ECANCELED 并放回 READY */
    kaula_iodriver_stop();
    kaula_iodriver_join();

    /* 6. 处理注入队列里没人接的 Task：标失败，唤醒 await 者。
     *    必须在 epoch 回收之前做，否则 await 会拿到悬垂指针。 */
    KaulaTask* orphan;
    uint32_t drained = 0;
    while ((orphan = kaula_inject_queue_pop()) != NULL) {
        if (!kaula_handle_is_null((KaulaHandle){0, orphan, {0}})) {
            orphan->error = KAULA_ERR_SHUTDOWN;
            kaula_task_wake_waiters(orphan);
            /* 不走 kaula_task_complete：那会递减计数并 retire，
             * 这里统一在下方批量 retire。 */
        }
        drained++;
    }

    /* 7. 停阻塞车道 */
    kaula_block_lane_stop();
    kaula_block_lane_join();
    kaula_block_lane_destroy();

    /* 8. 单线程上下文下强制回收所有退休对象 */
    kaula_epoch_shutdown();
    /*
     * 排空全局 Task 复用池（stop 之后才安全：此时无 Worker 在归还）。
     * 池是 static、跨 init/shutdown 存活，不清空会给下次 init 留下悬垂
     * 指针（test_epoch/test_task 等多次 init+shutdown 的用例立刻段错误）。
     */
    kaula_task_global_pool_reset();
    kaula_completion_shutdown();
    kaula_evict_destroy();
    kaula_iodriver_destroy();

    /* 9. KMM */
    kmm_v4_destroy_pool();

    g_conc_runtime.worker_count = 0;
    g_conc_initialized = 0;
    atomic_store(&g_conc_initing, 0);

    /* 诊断插桩（临时）：await_into 三路径计数 */
    fprintf(stdout, "DBG await_into: null=%llu awaitfail=%llu ok=%llu\n",
            (unsigned long long)atomic_load_explicit(&g_dbg_ai_null, memory_order_relaxed),
            (unsigned long long)atomic_load_explicit(&g_dbg_ai_awaitfail, memory_order_relaxed),
            (unsigned long long)atomic_load_explicit(&g_dbg_ai_ok, memory_order_relaxed));
    fflush(stdout);

    (void)drained;
}

/* ========== 查询 ========== */

uint32_t kaula_conc_cpu_count(void) {
    const char* v = kaula_p_get_env("KAULA_CPUS");
    if (v != NULL && *v != '\0') {
        long n = strtol(v, NULL, 10);
        if (n > 0) return (uint32_t)n;
    }

#if KAULA_PLATFORM_WINDOWS
    SYSTEM_INFO si;
    GetSystemInfo(&si);
    return si.dwNumberOfProcessors > 0 ? si.dwNumberOfProcessors : 2;
#else
    long n = sysconf(_SC_NPROCESSORS_ONLN);
    return n > 0 ? (uint32_t)n : 2;
#endif
}

uint32_t kaula_conc_worker_count(void) {
    return g_conc_runtime.worker_count;
}

uint64_t kaula_conc_active_tasks(void) {
    return atomic_load(&g_conc_runtime.active_task_count);
}

int kaula_conc_is_shutdown(void) {
    return atomic_load(&g_conc_runtime.shutdown);
}