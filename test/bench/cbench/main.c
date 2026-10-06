/*
 * C 侧基准驱动 —— 直调并发内核 API。
 *
 * 三边必须跑同一个函数、同一组常量、同样多的循环，否则测出来的差值里
 * 就混进了「工作量不同」这一项。见 TRANSACTION.md。
 *
 * 这条边与 kaula 边的关系：
 *
 *   kaula 边测的是「用户写 conc fn + spawn + await」经编译器 lowering 之后
 *   的最终形态 —— 编译器生成 Frame 结构体、resume 状态机、coercion shim，
 *   再调 kaula_spawn / kaula_spawn_simple，参数经栈展开到内核 ABI。
 *
 *   本边测的是「同一件事手工写一遍」的下限 —— 没有 lowering，Frame 由人手
 *   写，参数经结构体指针直传，resume 直接返回 KAULA_RESUME_DONE。
 *
 *   两者之差 = 编译器的 lowering 开销（语法的代价）。这是这张表要回答的
 *   核心问题之一：Kaula 的「更现代、更好用」值不值那几个百分点。
 *
 * 计时口径（对齐 kaula_conc_task.c 的内核打点，见 TRANSACTION.md）：
 *
 *   submit_ns   : kaula_task_init 里打点（进队列之前）
 *   complete_ns : kaula_task_complete 里打点（置 DONE 之前）
 *
 *   所以本边必须用 kaula_handle_await_into_timed 拷出内核测得的那两个
 *   时间戳之差，而不是调用方在 spawn 前 / await 后自己掐表。
 *   并发窗口里按「调用方何时取到结果」量到的是排队 + 顺序取回，
 *   与服务时间能差两个数量级（槽位 999 早算完，却要等 0..998 全取完）。
 *   ——修复前实测 p50=3.15s 而整轮只用 26ms，p50 大于总耗时。
 *
 *   注意：这里刻意用 await_into_timed 而不是 await_into + 全局访问器
 *   kaula_conc_last_latency_ns()。两者读的是同一个 complete_ns - submit_ns，
 *   但 timed 形态把值写进调用方缓冲区，省掉一次 TLS 读取。kaula 边走的是
 *   访问器形态（编译器只生成 into，不改 await 的 ABI）。差异属噪声级，
 *   且下面 report 里同时打印访问器口径做交叉校验。
 *
 * 用法：bench_c <curve> <conc> <total>
 *
 *      curve: light | suspend | mixed | degrade
 *
 * 输出：
 *   PHASE load_start / PHASE load_end / PHASE settle（供外部采样器对齐区间）
 *   一行 RESULT，key 集与 kaula/go/tokio 三边完全一致。
 */

#include "kaula/concurrency/kaula_conc.h"

#include <inttypes.h>
#include <stdarg.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <stdlib.h>   /* posix_memalign / malloc / free / exit / strtoll */

/* ========== 事务常量：四边逐位一致（TRANSACTION.md） ========== */

#define STEPS        256
#define SEED_VAL     2545
#define MULT         1103515245LL
#define ADD_CONST    12345LL
#define SHIFT        1048576LL                 /* 2^20 */
#define MOD_P        9223372036854775807LL     /* 2^63 - 1 */
#define MASK30       1073741824LL              /* 2^30 */

/* 交叉校验的期望值：四边 parse_and_checksum 必须都算出它 */
#define EXPECTED_SUM 13356499072LL

#define MAX_SAMPLES  2000
#define DEGRADE_SPIN 20000LL                   /* 超过预算量子 256，验预算抢占 */
#define SUSPEND_US   200LL                     /* suspend 曲线的挂起时长 */
#define SETTLE_US    400000LL                  /* 静置期 400ms，四边同一个数 */

/*
 * 缓存行大小（字节）。
 *
 * 与内核 KAULA_CACHE_LINE 同源；此处独立定义是为了让基准不依赖内核内部
 * 头（基准只 include 公开的 kaula_conc.h）。x86-64 / aarch64 均为 64。
 */
#ifndef KAULA_BENCH_CACHE_LINE
#define KAULA_BENCH_CACHE_LINE 64
#endif

/* ========== 曲线枚举 ========== */

typedef enum {
    CURVE_LIGHT = 0,
    CURVE_SUSPEND,
    CURVE_MIXED,
    CURVE_DEGRADE,
    CURVE_UNKNOWN
} Curve;

static Curve parse_curve(const char* s) {
    if (strcmp(s, "light")   == 0) return CURVE_LIGHT;
    if (strcmp(s, "suspend") == 0) return CURVE_SUSPEND;
    if (strcmp(s, "mixed")   == 0) return CURVE_MIXED;
    if (strcmp(s, "degrade") == 0) return CURVE_DEGRADE;
    return CURVE_UNKNOWN;
}

/* ========== 事务本体 ==========
 *
 * 与 Kaula 模板 parse_and_checksum / Go parseAndChecksum 逐字对应。
 *
 * 先 % SHIFT 再乘：state < 2^50。
 * 先 % MASK30 再 *31：中间值 < 2^35。
 * 这两处不是防御性写法 —— C 的有符号溢出是 UB，Kaula 的是检查陷阱，
 * 同一行代码两个目标语义不同，压到区间内才有唯一行为。
 */
static inline int64_t parse_and_checksum(void) {
    int64_t state    = SEED_VAL;
    int64_t checksum = 0;
    for (int k = 0; k < STEPS; k++) {
        state    = ((state % SHIFT) * MULT + ADD_CONST) % MOD_P;
        checksum = ((checksum % MASK30) * 31 + (state % 256)) % MOD_P;
    }
    return checksum;
}

/* ========== Frame：与编译器 lowering 的产出一一对应 ==========
 *
 * 编译器为 conc fn parse_and_checksum() 生成的 Frame 只有 pc + result
 * （无形参、单形态则连 pc 都没有）。这里为对标「编译器的开销」，也
 * 保留 pc 字段并让 resume 走 switch —— 因为 Kaula 侧的 conc fn 在
 * 本基准里都带挂起点（suspend/mixed 至少一条曲线有），编译器必然
 * 生成状态机形态。light/degrade 两条曲线 Kaula 侧是纯计算 conc fn，
 * 会被内联成直接调用（conf_inline.go），C 边这里同样让它们走 spawn
 * 以满足「并发投递」语义 —— 两边都是「一次 spawn 一个任务」，可比。
 */
typedef struct {
    int32_t pc;
    int32_t _pad;
    int64_t result;
    /* ---- degrade 专用参数 ---- */
    int64_t spin_rounds;
    /* ---- mixed 专用参数：这一单是否带挂起 ---- */
    int32_t with_sleep;
    int32_t _pad2;
} BenchFrame;

/* ========== resume 函数：编译器 lowering 的状态机形态 ==========
 *
 * Kaula 侧每个 conc fn 的 resume 长这样：
 *
 *   static KaulaResumeResult K_xxx_resume(void* f_) {
 *       BenchFrame* f = (BenchFrame*)f_;
 *       switch (f->pc) {
 *       case 0:
 *           ...入口段...到第一个挂起点
 *           f->pc = 1;
 *           return SUSPEND(io_request);
 *       case 1:
 *           ...恢复段...
 *           f->result = ...;
 *           return DONE;
 *       }
 *   }
 *
 * 这里手工写出同样的形态。四个曲线各一个 resume。
 */

/* 构造 DONE 结果。
 *
 * 关键：结果不是写进 Frame 就完事 —— 内核在 Worker 收到 DONE 后执行
 * `kaula_task_complete(task, result.value)`，await_into 拷的是这个
 * value 指向的内存。所以必须把 &f->result（或等价的结果槽地址）放进
 * value，否则 await 拷到的是 NULL，表现为「跑得飞快但结果全 0」。
 *
 * 这也正是 kaula_conc_task.h 反复强调「结果以 &f->result 的形式返回，
 * 提前释放就是 UAF」的原因：Frame 必须活到 await 之后。本基准的
 * Frame 在 frames[j] 池里，整个进程存活期有效，天然满足。 */
static KaulaResumeResult make_done(void* result_ptr) {
    KaulaResumeResult r;
    memset(&r, 0, sizeof(r));
    r.status = KAULA_RESUME_DONE;
    r.value  = result_ptr;
    return r;
}

/* light：纯计算，无挂起。pc 恒为 0，一次算完直接 DONE。 */
static KaulaResumeResult light_resume(void* f_) {
    BenchFrame* f = (BenchFrame*)f_;
    f->result = parse_and_checksum();
    return make_done(&f->result);
}

/* suspend：一次挂起（IoDriver 定时器）+ 一次计算。
 *
 * 挂起点用 KAULA_IO_SLEEP：提交后 Worker 立刻转做别的事（I4），
 * 与 Kaula 的 std.time.time_sleep_us 被 lowering 改写成定时器请求等价。
 */
static KaulaResumeResult suspend_resume(void* f_) {
    BenchFrame* f = (BenchFrame*)f_;
    switch (f->pc) {
    case 0: {
        KaulaResumeResult r;
        memset(&r, 0, sizeof(r));
        r.status = KAULA_RESUME_SUSPEND;
        r.io_request.type       = KAULA_IO_SLEEP;
        r.io_request.timeout_ns = (uint64_t)SUSPEND_US * 1000ULL;
        f->pc = 1;
        return r;
    }
    case 1:
    default:
        f->result = parse_and_checksum();
        return make_done(&f->result);
    }
}

/* mixed：按 with_sleep 决定是否挂起，再算一次。 */
static KaulaResumeResult mixed_resume(void* f_) {
    BenchFrame* f = (BenchFrame*)f_;
    switch (f->pc) {
    case 0:
        if (f->with_sleep) {
            KaulaResumeResult r;
            memset(&r, 0, sizeof(r));
            r.status = KAULA_RESUME_SUSPEND;
            r.io_request.type       = KAULA_IO_SLEEP;
            r.io_request.timeout_ns = (uint64_t)SUSPEND_US * 1000ULL;
            f->pc = 1;
            return r;
        }
        f->result = parse_and_checksum();
        return make_done(&f->result);
    case 1:
    default:
        f->result = parse_and_checksum();
        return make_done(&f->result);
    }
}

/* degrade：长忙循环（不是 sleep —— 睡走定时器，那是 suspend 那条路），
 * 再算一次，取异或。忙循环正是预算与停滞判定该管的东西。 */
static KaulaResumeResult degrade_resume(void* f_) {
    BenchFrame* f = (BenchFrame*)f_;
    int64_t acc = 1;
    for (int64_t r = 0; r < f->spin_rounds; r++) {
        acc = ((acc % MASK30) * MULT + ADD_CONST) % MOD_P;
    }
    f->result = acc ^ parse_and_checksum();
    return make_done(&f->result);
}

static KaulaResumeFn resume_for(Curve c) {
    switch (c) {
    case CURVE_SUSPEND: return suspend_resume;
    case CURVE_MIXED:   return mixed_resume;
    case CURVE_DEGRADE: return degrade_resume;
    default:            return light_resume;
    }
}

/* ========== 分位数：与 Go percentile / Kaula 下标公式逐字一致 ========== */

static int cmp_i64(const void* a, const void* b) {
    int64_t x = *(const int64_t*)a, y = *(const int64_t*)b;
    return (x > y) - (x < y);
}

static int64_t percentile(const int64_t* sorted, int64_t n, int permille) {
    if (n <= 0) return 0;
    int64_t idx = (n * permille) / 1000;
    if (idx >= n) idx = n - 1;
    return sorted[idx];
}

/* ========== 计时 ========== */

static uint64_t now_ns(void) {
    /* 与 kaula_p_now_ns 同源口径：CLOCK_MONOTONIC 纳秒 */
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (uint64_t)ts.tv_sec * 1000000000ULL + (uint64_t)ts.tv_nsec;
}

static void sleep_us(uint64_t us) {
    struct timespec ts;
    ts.tv_sec  = (time_t)(us / 1000000ULL);
    ts.tv_nsec = (long)((us % 1000000ULL) * 1000ULL);
    nanosleep(&ts, NULL);
}

static void die(const char* fmt, ...) {
    va_list ap;
    va_start(ap, fmt);
    vfprintf(stdout, fmt, ap);
    va_end(ap);
    fputc('\n', stdout);
    fflush(stdout);
    exit(1);
}

/* ========== 主流程 ========== */

int main(int argc, char** argv) {
    if (argc != 4) {
        die("BENCH_ERROR usage: bench_c <curve> <conc> <total>");
    }
    Curve curve = parse_curve(argv[1]);
    if (curve == CURVE_UNKNOWN) {
        die("BENCH_ERROR unknown curve %s", argv[1]);
    }
    long conc  = strtol(argv[2], NULL, 10);
    long total = strtol(argv[3], NULL, 10);
    if (conc <= 0 || total <= 0) {
        die("BENCH_ERROR bad args");
    }

    /* 预热：确认事务本体逐位正确。四边任何一个算错都在这里暴露，
     * 否则「跑得飞快但算错了」不会有任何断言失败。 */
    if (parse_and_checksum() != EXPECTED_SUM) {
        die("BENCH_ERROR warmup checksum %" PRId64 " want %" PRId64,
            parse_and_checksum(), (int64_t)EXPECTED_SUM);
    }
    (void)curve;

    /* 内核起来：worker 数走默认（CPU 数），与 kaula 边的默认配置一致。
     * enable_metrics = -1 让它按默认（关）。基准不开内核内部指标 ——
     * 那会污染热路径；外部采样器（metrics.py）取的是 OS 视角。 */
    KaulaRuntimeConfig cfg;
    memset(&cfg, 0, sizeof(cfg));
    cfg.worker_count   = 0;      /* 0 = 自动检测 CPU 数 */
    cfg.enable_metrics = -1;     /* < 0 = 默认（关） */
    if (kaula_runtime_init(&cfg) != 0) {
        die("BENCH_ERROR kaula_runtime_init failed");
    }

    /* 这条曲线的期望值：degrade 走 acc^parse 路径，与纯 parse 不同。
     * 与 Go 的 runCurve(curve,false) / Kaula 的 spawn curve_xxx() 一致 ——
     * 都是在计时区之外先跑一次拿 expected。 */
    int64_t expected;
    if (curve == CURVE_DEGRADE) {
        int64_t acc = 1;
        for (int64_t r = 0; r < DEGRADE_SPIN; r++) {
            acc = ((acc % MASK30) * MULT + ADD_CONST) % MOD_P;
        }
        expected = acc ^ parse_and_checksum();
    } else {
        expected = parse_and_checksum();
    }

    /* 缓冲：全部预分配，循环内不做 malloc —— 分配会混进被测量的时间。
     *
     * 只保留真正用到的三块：
     *   lat[]       延迟样本（内核打点的服务时间）
     *   frames[]    Frame 池
     *   in_flight[] 在途句柄窗口
     * 早期版本还分配了 svc/res/submit_ts，但延迟走的是
     * kaula_handle_await_into_timed 的直接出参，不经过它们 —— 去掉。
     *
     * ---- 缓存行隔离（false sharing）----
     *
     * 这三块是「跨核交替读写」的典型地形，相邻槽位共处一条缓存行时
     * 会构成 false sharing：
     *
     *   frames[j]      main 写（填窗时 f->pc=0）→ Worker 读并写 f->result
     *                  → main 读（await 拷出）→ 循环
     *   in_flight[j]   main 写（spawn 返回值）→ 内核读 → main 写（置空）
     *
     * 若两个槽位落在同一条 64B 行（BenchFrame 实际 32B，恰好一行两个），
     * main 写槽位 j 就会失效 Worker 正在读的槽位 j+1，每次换手都要
     * 跨核往返一次缓存行。
     *
     * 做法：每个槽位独占一条（或整数条）缓存行。
     *   - in_flight：KaulaHandle 是 48B，按 64B 对齐后正好一槽一行。
     *   - frames 指针数组：每 8 字节一个元素，用「一槽一行」的步长摆放，
     *     即指针数组按 KAULA_BENCH_CACHE_LINE 间隔分配，跳着写。
     *   - Frame 本体：用 posix_memalign 按行对齐，确保相邻 Frame 不共享行。
     *
     * 只在本文件内做，不改内核（内核的 Task/Worker 布局是另一件事，
     * 已由 Frame 热/冷字段分区的提交处理过）。
     */
    const long CACHE_LINE = KAULA_BENCH_CACHE_LINE;
    long in_flight_stride = (long)((sizeof(KaulaHandle) + CACHE_LINE - 1) / CACHE_LINE);
    if (in_flight_stride < 1) in_flight_stride = 1;

    int64_t* lat = NULL;
    if (posix_memalign((void**)&lat, CACHE_LINE, sizeof(int64_t) * MAX_SAMPLES) != 0) {
        die("BENCH_ERROR alloc failed");
    }

    /* in_flight：conc 个「一行一个」的槽位。
     *
     * 容量必须按「槽位间距」算，不能按 sizeof(KaulaHandle) 算 ——
     * KaulaHandle 是 48B，间距是 64B，按 48 分配会让最后一个槽位
     * 越过缓冲区尾部（48*conc < 64*(conc-1)+48）。 */
    size_t in_flight_slot  = (size_t)in_flight_stride * CACHE_LINE;
    size_t in_flight_bytes = in_flight_slot * (size_t)conc;
    KaulaHandle* in_flight = NULL;
    if (posix_memalign((void**)&in_flight, CACHE_LINE, in_flight_bytes) != 0) {
        die("BENCH_ERROR alloc failed");
    }
    memset(in_flight, 0, in_flight_bytes);
    /* 槽位 j 的首地址：按槽位间距步进，保持「一行一个」的间隔 */
    #define IN_FLIGHT_AT(j) \
        ((KaulaHandle*)((char*)in_flight + (size_t)(j) * in_flight_slot))

    /* frames：指针数组按行对齐 */
    size_t frames_bytes = sizeof(BenchFrame*) * (size_t)conc;
    BenchFrame** frames = NULL;
    if (posix_memalign((void**)&frames, CACHE_LINE, frames_bytes) != 0) {
        die("BENCH_ERROR alloc failed");
    }

    /* Frame 池：每个槽位一个，跨窗口复用 —— 与 Go 的 per-slot 缓冲对等。
     * 每个任务独立 Frame 是必须的：内核的 submit_ns/complete_ns 打在
     * Task 上，但结果写在 Frame，复用同一个 Frame 会让「上一个任务的
     * 结果」在新任务跑完前被读到。这里 conc 个槽位，槽位 j 在 await
     * 完之后才被下一次填窗覆盖，无竞争。
     *
     * 每个 Frame 按缓存行对齐分配：BenchFrame 32B，若不隔离则一行放两个，
     * 于是 main 写槽位 j 会失效 Worker 正在读的 j+1。 */
    for (long j = 0; j < conc; j++) {
        void* p = NULL;
        if (posix_memalign(&p, CACHE_LINE, sizeof(BenchFrame)) != 0 || p == NULL) {
            die("BENCH_ERROR frame alloc failed");
        }
        frames[j] = (BenchFrame*)p;
        memset(frames[j], 0, sizeof(BenchFrame));
        IN_FLIGHT_AT(j)->task = NULL;
        IN_FLIGHT_AT(j)->task_id = 0;
        IN_FLIGHT_AT(j)->reserved[0] = 0;
        IN_FLIGHT_AT(j)->reserved[1] = 0;
    }

    int64_t n = 0, checksum = 0, errors = 0;
    int64_t issued = 0, done = 0;
    int64_t spawn_failed = 0;

    puts("PHASE load_start");
    fflush(stdout);
    uint64_t t0 = now_ns();

    /* 窗口式：先填满 conc 个在途，再逐个 await。
     *
     * 不逐个「提交-等-提交」串行：那测的是单任务往返延迟，没有并发在飞，
     * TPS 会低一个数量级。也不按大批量提交后统一等：批边界会形成同步点，
     * 测出来的是「批」的吞吐而不是调度的吞吐。 */
    while (done < total) {
        /* 1) 填窗口 */
        long filled = 0;
        for (long j = 0; j < conc && issued < total; j++) {
            BenchFrame* f = frames[j];
            f->pc         = 0;
            f->result     = 0;
            f->spin_rounds = (curve == CURVE_DEGRADE) ? DEGRADE_SPIN : 0;
            /* mixed 按 issued 的奇偶定，50% 且不需要随机源（与 Kaula/Go 同） */
            f->with_sleep = (curve == CURVE_MIXED && (issued % 2 == 0)) ? 1 : 0;

            /* 提交。延迟由内核在 kaula_task_init / kaula_task_complete 里
             * 打点，调用方无需自己掐表，所以这里不再记 submit_ts。 */
            *IN_FLIGHT_AT(j) = kaula_spawn(resume_for(curve), f, sizeof(BenchFrame));
            /* 满载背压：内核在达到硬上限时返回带错误码的空句柄
             * （KAULA_ERR_WOULD_BLOCK），而不是丢弃或崩溃（I3：一切有界）。
             * 基准必须处理它 —— 静默忽略会让下面的 await 等一个空句柄。
             * 这里记为 spawn_failed 并计入 errors，不污染延迟样本。 */
            if (kaula_handle_is_null(*IN_FLIGHT_AT(j))) {
                spawn_failed++;
                errors++;
                done++;
                issued++;
                continue;
            }
            issued++;
            filled++;
        }

        /* 2) 逐个取回 */
        for (long j = 0; j < filled; j++) {
            int64_t v   = 0;
            uint64_t lat_ns = 0;
            /* 内核口径的服务时间：complete_ns - submit_ns，与取回顺序无关。
             * 结果拷进本地变量 —— 绝不能用任务结果指针，它在任务 retire
             * 时就悬垂了（await_into 的存在理由，见 kaula_conc_task.h）。 */
            kaula_handle_await_into_timed(*IN_FLIGHT_AT(j), &v, sizeof(v), &lat_ns);
            int err = kaula_handle_error(*IN_FLIGHT_AT(j));
            kaula_handle_release(*IN_FLIGHT_AT(j));
            *IN_FLIGHT_AT(j) = kaula_handle_null();

            if (err != KAULA_OK) errors++;
            /* 累加器必须四边同式且都不溢出。原来的 (checksum + v) % MOD_P
             * 在 degrade 上必溢出：v ≈ 9.8e18，第 10 次累加就越过有符号
             * 64 位。改成滚动散列：两边先压到 2^30，最大中间值
             * 32*(2^30-1) < 2^35，远离 2^63。与事务本体同一种写法。 */
            checksum = ((checksum % MASK30) * 31 + (v % MASK30)) % MOD_P;
            if (v != expected) errors++;

            if (n < MAX_SAMPLES) {
                lat[n] = (int64_t)lat_ns;
                n++;
            }
            done++;
        }
    }

    uint64_t elapsed = now_ns() - t0;

    /* 静置期：给运行时一点时间回收，之后才测 RSS 回落。
     * 与 Go time.Sleep / Kaula time_sleep_us 对齐 —— 400ms 同一个数。 */
    puts("PHASE load_end");
    fflush(stdout);
    sleep_us(SETTLE_US);
    puts("PHASE settle");
    fflush(stdout);

    /* 分位数在计时区之外算：排序本身不该污染被测量的时间 */
    qsort(lat, (size_t)n, sizeof(int64_t), cmp_i64);

    int64_t p50  = percentile(lat, n, 500);
    int64_t p95  = percentile(lat, n, 950);
    int64_t p99  = percentile(lat, n, 990);
    int64_t p999 = percentile(lat, n, 999);
    int64_t mx   = n > 0 ? lat[n - 1] : 0;

    int64_t tps = elapsed > 0 ? (int64_t)total * 1000000000LL / (int64_t)elapsed : 0;

    /* 推迟到输出后再 release —— 让 RESULT 行的所有量都已确定。
     *
     * inlined 字段：C 边恒为 0。
     *   这是 C 边与 kaula 边的关键口径锚点。kaula 边的纯计算曲线
     *   （light/degrade）被编译器判为「无挂起点」而完全内联为直接调用，
     *   内核看不到这个任务，延迟打点为 0，inlined=1。C 边直调内核，
     *   每一单都真的进内核，inlined=0。两边的 inlined 不同正是
     *   「编译器的优化幅度」的量化入口 —— 见 BENCH_C_VS_KAULA_VS_GO.md。 */
    printf("RESULT curve=%s tps=%" PRId64 " elapsed_ns=%" PRId64
           " p50=%" PRId64 " p95=%" PRId64 " p99=%" PRId64
           " p999=%" PRId64 " max=%" PRId64 " n=%" PRId64
           " win=%ld errors=%" PRId64 " checksum=%" PRId64
           " inlined=0 spawn_failed=%" PRId64 "\n",
           argv[1], tps, (int64_t)elapsed, p50, p95, p99, p999, mx,
           n, conc, errors, checksum, spawn_failed);
    fflush(stdout);

    for (long j = 0; j < conc; j++) free(frames[j]);
    free(lat); free(in_flight); free(frames);

    kaula_runtime_shutdown();
    return 0;
}
