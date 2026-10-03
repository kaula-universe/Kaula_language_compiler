/*
 * Kaula 并发内核 · Worker 隔离（撤离）实现
 */

#include "kaula_conc_evict.h"
#include "kaula_conc_worker.h"
#include "kaula_conc_scheduler.h"

#include <stdlib.h>
#include <string.h>

typedef struct {
    kaula_p_thread_t thread;
    _Atomic(int)     running;
    _Atomic(uint64_t) total;
    kaula_p_event    wake;
} KaulaEvictMonitor;

static KaulaEvictMonitor g_evict;

/* ========== 扫描 ========== */

void kaula_evict_scan(void) {
    uint64_t now = kaula_p_now_ns();
    uint64_t threshold = g_conc_runtime.stall_threshold_ns;

    uint32_t n = g_conc_runtime.worker_count;
    for (uint32_t i = 0; i < n; i++) {
        KaulaWorker* w = &g_conc_workers[i];

        if (!atomic_load_explicit(&w->running, memory_order_acquire)) continue;

        KaulaTask* task = w->current_task;
        if (task == NULL) continue;

        /* 只有从未被标记过的 Worker 才判定一次，避免重复扩容 */
        if (atomic_load_explicit(&w->evicted, memory_order_acquire)) continue;

        uint64_t start = w->current_task_start_ns;
        if (start == 0) continue;
        if (now - start < threshold) continue;

        /*
         * I5：标记这个 Worker 被隔离，并扩容一个替代者。
         *
         * 关键：不碰 task、不打断 w。
         * 任务状态在帧里，与执行它的线程无关（I2 只要求单一 owner，
         * 而 owner 在这里仍是 w —— 保持不变正是 I5 要的"任务原地不动"）。
         * 新 Worker 从队列取其他任务，系统整体继续推进。
         */
        atomic_store_explicit(&w->evicted, 1, memory_order_release);
        atomic_fetch_add_explicit(&g_evict.total, 1, memory_order_relaxed);

        /*
         * 扩容：在现有 worker_count 之后追加一个 Worker。
         * 上限保护：不做无限扩容（否则死循环任务会把线程数撑爆）。
         */
        uint32_t current = g_conc_runtime.worker_count;
        uint64_t evictions = atomic_load(&g_evict.total);
        if (current < KAULA_MAX_WORKERS && evictions <= KAULA_EVICT_MAX_REPLACEMENTS) {
            /* 两阶段创建（与 runtime_init 一致）：先初始化结构再启动线程，
             * 否则新 worker 半初始化期间就会被其他 worker 的 steal 读到。 */
            if (kaula_worker_prepare(&g_conc_workers[current], current) == 0) {
                if (kaula_worker_launch(&g_conc_workers[current]) != 0) {
                    kaula_p_event_destroy(&g_conc_workers[current].park_event);
                    return;
                }
                g_conc_runtime.worker_count = current + 1;
                /*
                 * 被隔离的旧 Worker 已从池中"淡出"：
                 * 把它的 id 标记后不再参与窃取目标选择。
                 * 实际做法是让 evicted 标志参与调度器的受害者筛选。
                 */
            }
        }
    }
}

/* ========== 监控线程 ========== */

static void evict_loop(void* arg) {
    KaulaEvictMonitor* m = (KaulaEvictMonitor*)arg;

    while (atomic_load_explicit(&m->running, memory_order_acquire)) {
        kaula_evict_scan();
        /* 检查周期取阈值的一半：既能及时发现，又不至于太频繁 */
        uint64_t period = g_conc_runtime.stall_threshold_ns / 2;
        if (period < 1000000ULL) period = 1000000ULL;
        kaula_p_sleep_ns(period);
    }
}

/* ========== 生命周期 ========== */

int kaula_evict_init(void) {
    memset(&g_evict, 0, sizeof(g_evict));
    atomic_store(&g_evict.total, 0);
    atomic_store_explicit(&g_evict.running, 0, memory_order_release);
    return 0;
}

int kaula_evict_start(void) {
    if (!g_conc_runtime.enable_evict_monitor) return 0;

    if (kaula_p_event_create(&g_evict.wake) != 0) return -1;

    atomic_store_explicit(&g_evict.running, 1, memory_order_release);
    if (kaula_p_thread_create(&g_evict.thread, evict_loop, &g_evict) != 0) {
        atomic_store_explicit(&g_evict.running, 0, memory_order_release);
        return -1;
    }
    return 0;
}

void kaula_evict_stop(void) {
    if (!g_evict.running) return;
    atomic_store_explicit(&g_evict.running, 0, memory_order_release);
}

/*
 * 判断依据必须是线程句柄，不是 running 标志。
 *
 * 关闭顺序是 stop() 然后 join()，而 stop() 已经把 running 清成 0 ——
 * 原实现在这里再判一次 running，于是 join 永远直接返回，监控线程的
 * 句柄既没 join 也没 CloseHandle。kaula_evict_init() 随后的 memset
 * 还会把这个句柄抹掉，于是每一轮 init/shutdown 都永久漏一个句柄，
 * 且上一轮的监控线程还活着时下一轮已经开始拆解运行时状态。
 */
void kaula_evict_join(void) {
    if (!kaula_p_thread_is_valid(&g_evict.thread)) return;
    kaula_p_thread_join(g_evict.thread);
    kaula_p_thread_invalidate(&g_evict.thread);
}

void kaula_evict_destroy(void) {
    /* 同block_lane：POSIX 上 kaula_p_event 是 struct，不能与 0 比较/赋值 */
    if (kaula_p_event_is_valid(&g_evict.wake)) {
        kaula_p_event_destroy(&g_evict.wake);
        kaula_p_event_invalidate(&g_evict.wake);
    }
}

uint64_t kaula_evict_total(void) {
    return atomic_load(&g_evict.total);
}