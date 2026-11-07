// BENCH:BEGIN
fn mean_absolute_deviation(numbers: &[f64]) -> f64 {
    let sum: f64 = numbers.iter().sum();
    let mean = sum / numbers.len() as f64;
    let dev: f64 = numbers.iter().map(|v| (v - mean).abs()).sum();
    dev / numbers.len() as f64
}

// BENCH:END

// BENCH:MAIN:BEGIN
fn main() {
    assert!((mean_absolute_deviation(&[1.0, 2.0, 3.0, 4.0]) - 1.0).abs() < 1e-6);
    println!("ok");
}
// BENCH:MAIN:END
