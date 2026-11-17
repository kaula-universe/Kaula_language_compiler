// BENCH:PREFIX

// BENCH:BEGIN
fn circular_shift(x: i64, shift: usize) -> String {
    let s = x.to_string();
    if shift > s.len() {
        return s.chars().rev().collect();
    }
    let cut = s.len() - shift;
    format!("{}{}", &s[cut..], &s[..cut])
}

// BENCH:END

// BENCH:MAIN:BEGIN
fn main() {
    assert_eq!(circular_shift(100, 2), "001");
    assert_eq!(circular_shift(97, 8), "79");
    println!("ok");
}
// BENCH:MAIN:END
