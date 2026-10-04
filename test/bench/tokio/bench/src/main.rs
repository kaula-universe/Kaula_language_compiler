// Tokio 侧基准驱动 —— 与 Kaula 模板 / Go 侧逐字对齐。
//
// 三边必须跑同一个函数、同一组常量、同样多的循环，否则测出来的差值里
// 就混进了「工作量不同」这一项。见 TRANSACTION.md。
//
// 用法：bench_tokio <curve> <conc> <total>
//
//	curve: light | suspend | mixed | degrade
//
// 计时口径（对齐 kaula_conc_task.c 的内核打点）：
//
//	Kaula 在 kaula_task_init 里打 submit_ns（进队列之前），
//	在 kaula_task_complete 里打 complete_ns（置 DONE 之前）。
//	所以这里 submit 取在 tokio::spawn 之前（含任务装箱 + 入队），
//	complete 取在任务体算完那一刻（含排队等待 + 执行）。
//
//	注意：TRANSACTION.md 里写的还是旧口径「提交→拿到结果」，那已被
//	e55adb9 判定为错误 —— 并发窗口里按「调用方何时取到结果」量到的是
//	排队 + 顺序取回，实测 p50=3.15s 而整轮只用 26ms。以代码为准。
//
// 时钟：std::time::Instant 在 Windows 上走 QueryPerformanceCounter，
// 实测连续读最小非零间隔 100ns，与 Kaula 的 kaula_p_now_ns 同源。
// 不能像 Go 那样用墙钟 —— Go 的 time.Now() 单调分量在 Windows 上读的是
// InterruptTime，粒度 10~40µs，会把 p50 全读成 0。

use std::hint::black_box;
use std::time::{Duration, Instant};

// ---- 事务常量：与 kaula/bench.kl.tmpl、go/bench/main.go 完全一致 ----
const STEPS: i64 = 256;
const SEED: i64 = 2545;
const MULT: i64 = 1103515245;
const ADD: i64 = 12345;
const SHIFT: i64 = 1048576; // 2^20
// 2^63 - 1：先 % 2^20 再乘，中间值 < 2^51，不需要 64 位环绕原语
const P: i64 = 9223372036854775807;
// 滚动散列必须先压到 2^30 再 *31：9.2e18*31 会溢出有符号 64 位
const MASK30: i64 = 1073741824;

// 交叉校验的期望值：三边 parse_and_checksum 必须都算出它
const EXPECTED_SUM: i64 = 13356499072;

const MAX_SAMPLES: usize = 2000;
const DEGRADE_SPIN: i64 = 20000; // 超过预算量子 256，验预算抢占
const SUSPEND_US: u64 = 200;
const SETTLE_MS: u64 = 400;

// Windows 把默认时钟粒度定在 15.625ms。Kaula 内核在 792960d 里已经调了
// timeBeginPeriod(1)，Go 侧也补了；不补齐的话每次 200µs 的 sleep 会被
// 放大一个数量级，suspend/mixed 两条曲线就不是同一把尺子。
#[cfg(windows)]
#[link(name = "winmm")]
unsafe extern "system" {
    fn timeBeginPeriod(u_period: u32) -> i32;
}

fn begin_timer_period() {
    #[cfg(windows)]
    unsafe {
        let _ = timeBeginPeriod(1);
    }
}

// parse_and_checksum —— 事务本体。
//
// 与模板逐字对应。先 % 1048576 再乘：state < 2^50；先 % 1073741824 再 *31：
// 中间值 < 2^35，两个中间值都远小于 2^63，Rust release 构建里普通 i64
// 算术不会溢出，不需要 wrapping 语义。
//
// 入口值过一遍 black_box：这是无参数的纯函数，LLVM 的函数特化会推断出
// 「返回值是常量」，把 256 次循环整个搬到编译期，运行时只剩一个常量返回。
// 实测不加时 20000 次事务耗时 0 ns —— 等于什么都没测。black_box 只是把
// 初值声明为编译期不可知，返回值仍是同一个 13356499072，语义完全不变。
fn parse_and_checksum() -> i64 {
    let mut state = black_box(SEED);
    let mut checksum = black_box(0);
    for _ in 0..STEPS {
        state = ((state % SHIFT) * MULT + ADD) % P;
        checksum = ((checksum % MASK30) * 31 + state % 256) % P;
    }
    checksum
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Curve {
    Light,
    Suspend,
    Mixed,
    Degrade,
}

impl Curve {
    fn parse(s: &str) -> Option<Self> {
        match s {
            "light" => Some(Self::Light),
            "suspend" => Some(Self::Suspend),
            "mixed" => Some(Self::Mixed),
            "degrade" => Some(Self::Degrade),
            _ => None,
        }
    }
}

// run_curve —— 按曲线跑一个事务。
// withSleep 只对 mixed 有意义，按发单序奇偶定，不用随机源。
async fn run_curve(curve: Curve, with_sleep: bool) -> i64 {
    match curve {
        Curve::Suspend => {
            // 这里量到的 sleep 超额是 Tokio 的固有属性，不是基准口径问题：
            // src/runtime/time/source.rs 的 deadline_to_tick 会把 deadline
            // 向上取整到当前毫秒末尾（"Round up to the end of a ms"），
            // tick_to_duration 也是 Duration::from_millis —— 轮盘粒度 1ms；
            // 再叠 Windows Sleep 自身的 1ms 取整，200µs 实测约 2ms。
            // 同条件实测对照：Go time.Sleep(200µs) p50=623800ns，
            // std::thread::sleep(200µs) p50=983500ns，tokio p50=2001100ns。
            // 三边都已调 timeBeginPeriod(1)，这是可比的运行时差异。
            tokio::time::sleep(Duration::from_micros(SUSPEND_US)).await;
            parse_and_checksum()
        }
        Curve::Mixed => {
            if with_sleep {
                tokio::time::sleep(Duration::from_micros(SUSPEND_US)).await;
            }
            parse_and_checksum()
        }
        Curve::Degrade => {
            // 忙循环而不是睡 —— 睡走定时器，那是 suspend 那条路。
            // 这里要的是「占住 Worker 的计算」，正是预算与停滞判定该管的东西。
            // 同样过 black_box：无副作用的常量循环会被 LLVM 折叠，20000 次
            // 会变成编译期算完的一次，degrade 曲线就测不出 CPU 占用了。
            let mut acc = black_box(1i64);
            for _ in 0..DEGRADE_SPIN {
                acc = ((acc % MASK30) * MULT + ADD) % P;
            }
            acc ^ parse_and_checksum()
        }
        Curve::Light => parse_and_checksum(),
    }
}

// percentile 取排序后样本的分位值，与 Kaula 模板的 (n*pct)/100 下标一致。
// 夹到 n-1：n 很小时 (n*999)/1000 可能落到 n 上。
fn percentile(sorted: &[i64], pct: usize) -> i64 {
    let n = sorted.len();
    if n == 0 {
        return 0;
    }
    let mut idx = n * pct / 100;
    if idx >= n {
        idx = n - 1;
    }
    sorted[idx]
}

fn die(msg: String) -> ! {
    println!("{}", msg);
    std::process::exit(1);
}

#[tokio::main]
async fn main() {
    // 提高系统时钟分辨率到 1ms，与 Kaula 侧 792960d 的 timeBeginPeriod(1) 对齐。
    begin_timer_period();

    let args: Vec<String> = std::env::args().collect();
    if args.len() != 4 {
        die("BENCH_ERROR usage: bench_tokio <curve> <conc> <total>".into());
    }
    let curve_str = &args[1];
    let curve = match Curve::parse(curve_str) {
        Some(c) => c,
        None => die(format!("BENCH_ERROR unknown curve {:?}", curve_str)),
    };
    let conc: usize = match args[2].parse() {
        Ok(v) if v > 0 => v,
        _ => die(format!("BENCH_ERROR bad args: {:?}", &args[1..])),
    };
    let total: usize = match args[3].parse() {
        Ok(v) if v > 0 => v,
        _ => die(format!("BENCH_ERROR bad args: {:?}", &args[1..])),
    };

    // 预热：确认事务本体逐位正确。三边任何一个算错都在这里暴露，
    // 否则「跑得飞快但算错了」不会有任何断言失败。
    if parse_and_checksum() != EXPECTED_SUM {
        die("BENCH_ERROR warmup checksum mismatch".into());
    }
    // 这条曲线的期望值：degrade 会走 acc^parse 的路径，与上面不同。
    let expected = run_curve(curve, false).await;

    // 每窗口复用的缓冲：循环内分配会让分配器的开销混进被测量的时间。
    let mut lat = vec![0i64; MAX_SAMPLES];
    let mut handles: Vec<tokio::task::JoinHandle<(i64, i64)>> = Vec::with_capacity(conc);

    let mut checksum: i64 = 0;
    let mut errors: i64 = 0;
    let mut n: usize = 0;
    let mut issued: usize = 0;
    let mut done: usize = 0;

    println!("PHASE load_start");
    let t0 = Instant::now();

    // 窗口式：先填满 conc 个在途，再统一取回。
    //
    // 不逐个「提交-等-提交」串行：那测的是单任务往返延迟，没有并发在飞，
    // TPS 会低一个数量级。也不按大批量提交后统一等：批边界会形成同步点，
    // 测出来的是「批」的吞吐而不是调度的吞吐。
    while done < total {
        handles.clear();
        let mut filled = 0usize;
        while filled < conc && issued < total {
            // 在 tokio::spawn 之前打点：与 Kaula 的 submit_ns（进队列之前）
            // 同口径，任务装箱与入队成本要计入调度器。
            let sub = Instant::now();
            let with_sleep = issued % 2 == 0; // mixed 按奇偶定，50% 且不需要随机源
            handles.push(tokio::spawn(async move {
                let v = run_curve(curve, with_sleep).await;
                // 任务体算完那一刻打点 = Kaula 的 complete_ns（置 DONE 之前）
                let end = Instant::now();
                // saturating：正常不会 end < sub，但单调钟读数在虚拟机里
                // 偶有回退，panic 会让整轮基准崩掉而不是暴露异常。
                let lat = end.saturating_duration_since(sub).as_nanos() as i64;
                (v, lat)
            }));
            issued += 1;
            filled += 1;
        }

        // 顺序取回：与模板的 in_flight[j].await 对齐。
        // checksum 是模和，与顺序无关；但保持同序便于逐行比对。
        // drain 而非 iter：&JoinHandle 不是 Future，必须按值 await；
        // drain 会保留 Vec 的容量，下个窗口不用重新分配。
        for h in handles.drain(..) {
            match h.await {
                Ok((v, lat_ns)) => {
                    // 累加器必须三边同式且都不溢出。原来的 (checksum + v) % P
                    // 在 degrade 上必溢出：v ≈ 9.8e18，第 10 次累加就越过有符号
                    // 64 位。Go/Rust release 静默回绕，而 Kaula 的乘加是检查
                    // 溢出并陷阱 —— 同一行代码三个目标语义不同，Kaula 直接
                    // KAULA FATAL 退出。改成滚动散列：两边先压到 2^30，
                    // 最大中间值 32*(2^30-1) < 2^35，远离 2^63。
                    // 与事务本体用的是同一种写法。
                    checksum = ((checksum % MASK30) * 31 + (v % MASK30)) % P;
                    if v != expected {
                        errors += 1;
                    }
                    if n < MAX_SAMPLES {
                        lat[n] = lat_ns;
                        n += 1;
                    }
                }
                // 任务 panic 或被取消：记为错误。done 照样 +1，
                // 否则窗口在途数对不上，外层 while 会死循环。
                Err(_) => errors += 1,
            }
            done += 1;
        }
    }
    let elapsed = t0.elapsed().as_nanos() as i64;

    // 静置期：给运行时一点时间回收，之后才测 RSS 回落。
    println!("PHASE load_end");
    tokio::time::sleep(Duration::from_millis(SETTLE_MS)).await;
    println!("PHASE settle");

    // 分位数在计时区之外算：排序本身不该污染被测量的时间。
    let sorted = &mut lat[..n];
    sorted.sort_unstable();

    let tps = if elapsed > 0 {
        total as i64 * 1_000_000_000 / elapsed
    } else {
        0
    };

    // 单行输出：多行调用与自动补分号的脚本会互相干扰。
    println!(
        "RESULT curve={} tps={} elapsed_ns={} p50={} p95={} p99={} p999={} max={} n={} win={} errors={} checksum={}",
        curve_str,
        tps,
        elapsed,
        percentile(sorted, 50),
        percentile(sorted, 95),
        percentile(sorted, 99),
        percentile(sorted, 999),
        percentile(sorted, 100),
        n,
        conc,
        errors,
        checksum
    );
}
