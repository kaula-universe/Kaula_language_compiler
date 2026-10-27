// 事务一致性：Rust 侧必须与 Kaula / Go 侧算出同一个值。
//
// 期望值 13356499072 —— 由 Kaula 与 Go 两侧实测写死。

const P: i64 = 9223372036854775807;

fn parse_and_checksum() -> i64 {
    let mut state: i64 = 2545;
    let mut checksum: i64 = 0;
    for _ in 0..256 {
        state = ((state % 1048576) * 1103515245 + 12345) % P;
        checksum = ((checksum % 1073741824) * 31 + state % 256) % P;
    }
    checksum
}

fn main() {
    let got = parse_and_checksum();
    let want: i64 = 13356499072;
    println!("rs  = {}", got);
    if got != want {
        println!("MISMATCH want {}", want);
        std::process::exit(1);
    }
    println!("rs agrees with kaula and go");
}
