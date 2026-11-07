// BENCH:BEGIN
fn pairs_sum_to_zero(l: &[i64]) -> bool {
    for i in 0..l.len() {
        for j in (i + 1)..l.len() {
            if l[i] + l[j] == 0 {
                return true;
            }
        }
    }
    false
}

// BENCH:END

// BENCH:MAIN:BEGIN
fn main() {
    assert!(!pairs_sum_to_zero(&[1, 3, 5, 0]));
    assert!(pairs_sum_to_zero(&[2, 4, -5, 3, 5, 7]));
    println!("ok");
}
// BENCH:MAIN:END
