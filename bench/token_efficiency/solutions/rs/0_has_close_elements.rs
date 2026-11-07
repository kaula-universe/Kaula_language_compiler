// BENCH:BEGIN
fn has_close_elements(numbers: &[f64], threshold: f64) -> bool {
    for i in 0..numbers.len() {
        for j in 0..numbers.len() {
            if i != j && (numbers[i] - numbers[j]).abs() < threshold {
                return true;
            }
        }
    }
    false
}

// BENCH:END

// BENCH:MAIN:BEGIN
fn main() {
    let a = [1.0, 2.0, 3.9, 4.0, 5.0, 2.2];
    assert!(has_close_elements(&a, 0.3));
    assert!(!has_close_elements(&a, 0.05));
    println!("ok");
}
// BENCH:MAIN:END
