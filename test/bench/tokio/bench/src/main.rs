// Tokio 侧基准驱动 —— 工具链自检
//
// 当前状态：只验证「tokio 装上了、能跑、事务算得对」。
// run_bench.py 期望的 RESULT 行（tps/elapsed_ns/p50/p95/p99/max/n/checksum）
// 与四条曲线尚未实现，见 TRANSACTION.md。
//
// 事务定义与期望值 13356499072 见 TRANSACTION.md，与 Kaula / Go 侧逐位一致。

const P: i64 = 9223372036854775807;

// 与 checksum_probe.rs / main.go / bench.kl.tmpl 逐位一致。
// 先 % 1048576 再乘：中间值 < 2^50，普通 i64 即可，无需 wrapping 原语。
fn parse_and_checksum() -> i64 {
    let mut state: i64 = 2545;
    let mut checksum: i64 = 0;
    for _ in 0..256 {
        state = ((state % 1048576) * 1103515245 + 12345) % P;
        checksum = ((checksum % 1073741824) * 31 + state % 256) % P;
    }
    checksum
}

#[tokio::main]
async fn main() {
    // 挂起通路自检：TRANSACTION.md 规定的 suspend 原语
    let t = std::time::Instant::now();
    tokio::time::sleep(std::time::Duration::from_micros(200)).await;
    let slept_ns = t.elapsed().as_nanos();

    let got = parse_and_checksum();
    const WANT: i64 = 13356499072;

    println!("tokio runtime up");
    println!("sleep(200us) actual = {}ns", slept_ns);
    println!("rs  = {}", got);

    if got != WANT {
        println!("MISMATCH want {}", WANT);
        std::process::exit(1);
    }
    println!("rs agrees with kaula and go");
}
