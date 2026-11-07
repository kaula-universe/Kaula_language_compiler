// BENCH:BEGIN
fn truncate_number(number: f64) -> f64 {
    number % 1.0
}

// BENCH:END

// BENCH:MAIN:BEGIN
fn main() {
    assert_eq!(truncate_number(3.5), 0.5);
    println!("ok");
}
// BENCH:MAIN:END
