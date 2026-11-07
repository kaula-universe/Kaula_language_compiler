// BENCH:PREFIX

// BENCH:BEGIN
fn strlen(s: &str) -> usize {
    s.chars().count()
}

// BENCH:END

// BENCH:MAIN:BEGIN
fn main() {
    assert_eq!(strlen(""), 0);
    assert_eq!(strlen("abc"), 3);
    println!("ok");
}
// BENCH:MAIN:END
