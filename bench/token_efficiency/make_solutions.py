#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""生成 bench/token_efficiency/solutions/ 下六语言参考实现（含官方用例自检）。

任务集：HumanEval/0,2,4,23,43,65（MultiPL-E 同任务多语言基准的任务定义）。
每个文件用 BENCH:BEGIN / BENCH:END 标出"实现区"，token 统计只算这一区，
测试驱动（main）在标记之外，不计入。

用法：python3 make_solutions.py
"""
import os
from pathlib import Path

HERE = Path(__file__).resolve().parent
SOL = HERE / "solutions"

TASKS = ["0_has_close_elements", "2_truncate_number", "4_mean_absolute_deviation",
         "23_strlen", "43_pairs_sum_to_zero", "65_circular_shift"]

FILES = {}

# ---------------------------------------------------------------- HumanEval/0
FILES["py/0_has_close_elements.py"] = '''import math
# BENCH:BEGIN
def has_close_elements(numbers, threshold):
    for i, a in enumerate(numbers):
        for j, b in enumerate(numbers):
            if i != j and abs(a - b) < threshold:
                return True
    return False
# BENCH:END

# BENCH:BEGIN
# (imports/types counted above)

if __name__ == "__main__":
    assert has_close_elements([1.0, 2.0, 3.9, 4.0, 5.0, 2.2], 0.3) == True
    assert has_close_elements([1.0, 2.0, 3.9, 4.0, 5.0, 2.2], 0.05) == False
    assert has_close_elements([1.0, 2.0, 5.9, 4.0, 5.0], 0.8) == False
    assert has_close_elements([1.1, 2.2, 3.1, 4.1, 5.1], 1.0) == True
    print("ok")
'''

FILES["go/0_has_close_elements.go"] = '''package main

import (
	"fmt"
	"math"
)

// BENCH:BEGIN
func hasCloseElements(numbers []float64, threshold float64) bool {
	for i := 0; i < len(numbers); i++ {
		for j := 0; j < len(numbers); j++ {
			if i != j && math.Abs(numbers[i]-numbers[j]) < threshold {
				return true
			}
		}
	}
	return false
}
// BENCH:END

func main() {
	if !hasCloseElements([]float64{1.0, 2.0, 3.9, 4.0, 5.0, 2.2}, 0.3) {
		panic("case1")
	}
	if hasCloseElements([]float64{1.0, 2.0, 3.9, 4.0, 5.0, 2.2}, 0.05) {
		panic("case2")
	}
	fmt.Println("ok")
}
'''

FILES["cpp/0_has_close_elements.cpp"] = '''#include <cmath>
#include <cstdio>
#include <vector>

// BENCH:BEGIN
bool has_close_elements(const std::vector<double>& numbers, double threshold) {
    for (size_t i = 0; i < numbers.size(); i++) {
        for (size_t j = 0; j < numbers.size(); j++) {
            if (i != j && std::fabs(numbers[i] - numbers[j]) < threshold) {
                return true;
            }
        }
    }
    return false;
}
// BENCH:END

int main() {
    std::vector<double> a{1.0, 2.0, 3.9, 4.0, 5.0, 2.2};
    if (!has_close_elements(a, 0.3)) return 1;
    if (has_close_elements(a, 0.05)) return 2;
    std::printf("ok\\n");
    return 0;
}
'''

FILES["c/0_has_close_elements.c"] = '''#include <math.h>
#include <stdbool.h>
#include <stdio.h>

// BENCH:BEGIN
bool has_close_elements(const double* numbers, int n, double threshold) {
    for (int i = 0; i < n; i++) {
        for (int j = 0; j < n; j++) {
            if (i != j && fabs(numbers[i] - numbers[j]) < threshold) {
                return true;
            }
        }
    }
    return false;
}
// BENCH:END

int main(void) {
    double a[6] = {1.0, 2.0, 3.9, 4.0, 5.0, 2.2};
    if (!has_close_elements(a, 6, 0.3)) return 1;
    if (has_close_elements(a, 6, 0.05)) return 2;
    printf("ok\\n");
    return 0;
}
'''

FILES["rs/0_has_close_elements.rs"] = '''// BENCH:BEGIN
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

fn main() {
    let a = [1.0, 2.0, 3.9, 4.0, 5.0, 2.2];
    assert!(has_close_elements(&a, 0.3));
    assert!(!has_close_elements(&a, 0.05));
    println!("ok");
}
'''

FILES["kl/0_has_close_elements.kl"] = '''import std.io
import std.math

// BENCH:BEGIN
fn has_close_elements(f64* numbers, i64 n, f64 threshold) bool {
    for i in range(n) {
        for j in range(n) {
            if i != j && std.math.abs(numbers[i] - numbers[j]) < threshold {
                return true
            }
        }
    }
    return false
}
// BENCH:END

fn main() i32 {
    []f64 a = [1.0, 2.0, 3.9, 4.0, 5.0, 2.2]
    if !has_close_elements(a, 6, 0.3) { return 1 }
    if has_close_elements(a, 6, 0.05) { return 2 }
    std.io.println("ok")
    return 0
}
'''

# ---------------------------------------------------------------- HumanEval/2
FILES["py/2_truncate_number.py"] = '''# BENCH:BEGIN
def truncate_number(number):
    return number % 1.0
# BENCH:END

if __name__ == "__main__":
    assert truncate_number(3.5) == 0.5
    assert abs(truncate_number(3.25) - 0.25) < 1e-9
    print("ok")
'''

FILES["go/2_truncate_number.go"] = '''package main

import (
	"fmt"
	"math"
)

// BENCH:BEGIN
func truncateNumber(number float64) float64 {
	return math.Mod(number, 1.0)
}
// BENCH:END

func main() {
	if truncateNumber(3.5) != 0.5 {
		panic("case1")
	}
	fmt.Println("ok")
}
'''

FILES["cpp/2_truncate_number.cpp"] = '''#include <cmath>
#include <cstdio>

// BENCH:BEGIN
double truncate_number(double number) {
    return std::fmod(number, 1.0);
}
// BENCH:END

int main() {
    if (truncate_number(3.5) != 0.5) return 1;
    std::printf("ok\\n");
    return 0;
}
'''

FILES["c/2_truncate_number.c"] = '''#include <math.h>
#include <stdio.h>

// BENCH:BEGIN
double truncate_number(double number) {
    return fmod(number, 1.0);
}
// BENCH:END

int main(void) {
    if (truncate_number(3.5) != 0.5) return 1;
    printf("ok\\n");
    return 0;
}
'''

FILES["rs/2_truncate_number.rs"] = '''// BENCH:BEGIN
fn truncate_number(number: f64) -> f64 {
    number % 1.0
}
// BENCH:END

fn main() {
    assert_eq!(truncate_number(3.5), 0.5);
    println!("ok");
}
'''

FILES["kl/2_truncate_number.kl"] = '''import std.io
import std.math

// BENCH:BEGIN
fn truncate_number(f64 number) f64 {
    return std.math.fmod(number, 1.0)
}
// BENCH:END

fn main() i32 {
    if truncate_number(3.5) != 0.5 { return 1 }
    std.io.println("ok")
    return 0
}
'''

# ---------------------------------------------------------------- HumanEval/4
FILES["py/4_mean_absolute_deviation.py"] = '''# BENCH:BEGIN
def mean_absolute_deviation(numbers):
    mean = sum(numbers) / len(numbers)
    return sum(abs(x - mean) for x in numbers) / len(numbers)
# BENCH:END

if __name__ == "__main__":
    assert abs(mean_absolute_deviation([1.0, 2.0, 3.0]) - 2.0 / 3.0) < 1e-6
    assert abs(mean_absolute_deviation([1.0, 2.0, 3.0, 4.0]) - 1.0) < 1e-6
    print("ok")
'''

FILES["go/4_mean_absolute_deviation.go"] = '''package main

import (
	"fmt"
	"math"
)

// BENCH:BEGIN
func meanAbsoluteDeviation(numbers []float64) float64 {
	var sum float64
	for _, v := range numbers {
		sum += v
	}
	mean := sum / float64(len(numbers))
	var dev float64
	for _, v := range numbers {
		dev += math.Abs(v - mean)
	}
	return dev / float64(len(numbers))
}
// BENCH:END

func main() {
	if math.Abs(meanAbsoluteDeviation([]float64{1.0, 2.0, 3.0, 4.0})-1.0) > 1e-6 {
		panic("case1")
	}
	fmt.Println("ok")
}
'''

FILES["cpp/4_mean_absolute_deviation.cpp"] = '''#include <cmath>
#include <cstdio>
#include <numeric>
#include <vector>

// BENCH:BEGIN
double mean_absolute_deviation(const std::vector<double>& numbers) {
    double sum = std::accumulate(numbers.begin(), numbers.end(), 0.0);
    double mean = sum / static_cast<double>(numbers.size());
    double dev = 0.0;
    for (double v : numbers) {
        dev += std::fabs(v - mean);
    }
    return dev / static_cast<double>(numbers.size());
}
// BENCH:END

int main() {
    std::vector<double> a{1.0, 2.0, 3.0, 4.0};
    if (std::fabs(mean_absolute_deviation(a) - 1.0) > 1e-6) return 1;
    std::printf("ok\\n");
    return 0;
}
'''

FILES["c/4_mean_absolute_deviation.c"] = '''#include <math.h>
#include <stdio.h>

// BENCH:BEGIN
double mean_absolute_deviation(const double* numbers, int n) {
    double sum = 0.0;
    for (int i = 0; i < n; i++) {
        sum += numbers[i];
    }
    double mean = sum / (double)n;
    double dev = 0.0;
    for (int i = 0; i < n; i++) {
        dev += fabs(numbers[i] - mean);
    }
    return dev / (double)n;
}
// BENCH:END

int main(void) {
    double a[4] = {1.0, 2.0, 3.0, 4.0};
    if (fabs(mean_absolute_deviation(a, 4) - 1.0) > 1e-6) return 1;
    printf("ok\\n");
    return 0;
}
'''

FILES["rs/4_mean_absolute_deviation.rs"] = '''// BENCH:BEGIN
fn mean_absolute_deviation(numbers: &[f64]) -> f64 {
    let sum: f64 = numbers.iter().sum();
    let mean = sum / numbers.len() as f64;
    let dev: f64 = numbers.iter().map(|v| (v - mean).abs()).sum();
    dev / numbers.len() as f64
}
// BENCH:END

fn main() {
    assert!((mean_absolute_deviation(&[1.0, 2.0, 3.0, 4.0]) - 1.0).abs() < 1e-6);
    println!("ok");
}
'''

FILES["kl/4_mean_absolute_deviation.kl"] = '''import std.io
import std.math

// BENCH:BEGIN
fn mean_absolute_deviation(f64* numbers, i64 n) f64 {
    f64 sum = 0.0
    for i in range(n) {
        sum = sum + numbers[i]
    }
    f64 mean = sum / as<f64>(n)
    f64 dev = 0.0
    for i in range(n) {
        dev = dev + std.math.abs(numbers[i] - mean)
    }
    return dev / as<f64>(n)
}
// BENCH:END

fn main() i32 {
    []f64 a = [1.0, 2.0, 3.0, 4.0]
    if std.math.abs(mean_absolute_deviation(a, 4) - 1.0) > 1e-6 { return 1 }
    std.io.println("ok")
    return 0
}
'''

# ---------------------------------------------------------------- HumanEval/23
FILES["py/23_strlen.py"] = '''# BENCH:BEGIN
def strlen(string):
    return len(string)
# BENCH:END

if __name__ == "__main__":
    assert strlen("") == 0
    assert strlen("abc") == 3
    print("ok")
'''

FILES["go/23_strlen.go"] = '''package main

import "fmt"

// BENCH:BEGIN
func strlen(s string) int {
	return len(s)
}
// BENCH:END

func main() {
	if strlen("") != 0 || strlen("abc") != 3 {
		panic("case")
	}
	fmt.Println("ok")
}
'''

FILES["cpp/23_strlen.cpp"] = '''#include <cstdio>
#include <string>

// BENCH:BEGIN
int strlen(const std::string& s) {
    return static_cast<int>(s.size());
}
// BENCH:END

int main() {
    std::string a = "abc";
    if (strlen(std::string()) != 0 || strlen(a) != 3) return 1;
    std::printf("ok\\n");
    return 0;
}
'''

FILES["c/23_strlen.c"] = '''#include <stdio.h>
#include <string.h>

// BENCH:BEGIN
int strlen_of(const char* s) {
    return (int)strlen(s);
}
// BENCH:END

int main(void) {
    if (strlen_of("") != 0 || strlen_of("abc") != 3) return 1;
    printf("ok\\n");
    return 0;
}
'''

FILES["rs/23_strlen.rs"] = '''// BENCH:BEGIN
fn strlen(s: &str) -> usize {
    s.chars().count()
}
// BENCH:END

fn main() {
    assert_eq!(strlen(""), 0);
    assert_eq!(strlen("abc"), 3);
    println!("ok");
}
'''

FILES["kl/23_strlen.kl"] = '''import std.io
import std.string

// BENCH:BEGIN
fn kl_strlen(String s) i64 {
    return as<i64>(std.string.length(s))
}
// BENCH:END

fn main() i32 {
    String empty = std.string.create("")
    String abc = std.string.create("abc")
    if kl_strlen(empty) != 0 { return 1 }
    if kl_strlen(abc) != 3 { return 2 }
    std.io.println("ok")
    return 0
}
'''

# ---------------------------------------------------------------- HumanEval/43
FILES["py/43_pairs_sum_to_zero.py"] = '''# BENCH:BEGIN
def pairs_sum_to_zero(l):
    for i, a in enumerate(l):
        for b in l[i + 1:]:
            if a + b == 0:
                return True
    return False
# BENCH:END

if __name__ == "__main__":
    assert pairs_sum_to_zero([1, 3, 5, 0]) == False
    assert pairs_sum_to_zero([2, 4, -5, 3, 5, 7]) == True
    assert pairs_sum_to_zero([1]) == False
    print("ok")
'''

FILES["go/43_pairs_sum_to_zero.go"] = '''package main

import "fmt"

// BENCH:BEGIN
func pairsSumToZero(l []int) bool {
	for i := 0; i < len(l); i++ {
		for j := i + 1; j < len(l); j++ {
			if l[i]+l[j] == 0 {
				return true
			}
		}
	}
	return false
}
// BENCH:END

func main() {
	if pairsSumToZero([]int{1, 3, 5, 0}) {
		panic("case1")
	}
	if !pairsSumToZero([]int{2, 4, -5, 3, 5, 7}) {
		panic("case2")
	}
	fmt.Println("ok")
}
'''

FILES["cpp/43_pairs_sum_to_zero.cpp"] = '''#include <cstdio>
#include <vector>

// BENCH:BEGIN
bool pairs_sum_to_zero(const std::vector<int>& l) {
    for (size_t i = 0; i < l.size(); i++) {
        for (size_t j = i + 1; j < l.size(); j++) {
            if (l[i] + l[j] == 0) {
                return true;
            }
        }
    }
    return false;
}
// BENCH:END

int main() {
    if (pairs_sum_to_zero({1, 3, 5, 0})) return 1;
    if (!pairs_sum_to_zero({2, 4, -5, 3, 5, 7})) return 2;
    std::printf("ok\\n");
    return 0;
}
'''

FILES["c/43_pairs_sum_to_zero.c"] = '''#include <stdbool.h>
#include <stdio.h>

// BENCH:BEGIN
bool pairs_sum_to_zero(const long* l, int n) {
    for (int i = 0; i < n; i++) {
        for (int j = i + 1; j < n; j++) {
            if (l[i] + l[j] == 0) {
                return true;
            }
        }
    }
    return false;
}
// BENCH:END

int main(void) {
    long a[6] = {2, 4, -5, 3, 5, 7};
    long b[4] = {1, 3, 5, 0};
    if (pairs_sum_to_zero(b, 4)) return 1;
    if (!pairs_sum_to_zero(a, 6)) return 2;
    printf("ok\\n");
    return 0;
}
'''

FILES["rs/43_pairs_sum_to_zero.rs"] = '''// BENCH:BEGIN
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

fn main() {
    assert!(!pairs_sum_to_zero(&[1, 3, 5, 0]));
    assert!(pairs_sum_to_zero(&[2, 4, -5, 3, 5, 7]));
    println!("ok");
}
'''

FILES["kl/43_pairs_sum_to_zero.kl"] = '''import std.io

// BENCH:BEGIN
fn pairs_sum_to_zero(i64* l, i64 n) bool {
    for i in range(n) {
        for j in range(i + 1, n) {
            if l[i] + l[j] == 0 {
                return true
            }
        }
    }
    return false
}
// BENCH:END

fn main() i32 {
    []i64 a = [2, 4, -5, 3, 5, 7]
    []i64 b = [1, 3, 5, 0]
    if pairs_sum_to_zero(b, 4) { return 1 }
    if !pairs_sum_to_zero(a, 6) { return 2 }
    std.io.println("ok")
    return 0
}
'''

# ---------------------------------------------------------------- HumanEval/65
FILES["py/65_circular_shift.py"] = '''# BENCH:BEGIN
def circular_shift(x, shift):
    s = str(x)
    if shift > len(s):
        return s[::-1]
    return s[len(s) - shift:] + s[:len(s) - shift]
# BENCH:END

if __name__ == "__main__":
    assert circular_shift(100, 2) == "001"
    assert circular_shift(12, 2) == "12"
    assert circular_shift(97, 8) == "79"
    assert circular_shift(11, 101) == "11"
    print("ok")
'''

FILES["go/65_circular_shift.go"] = '''package main

import (
	"fmt"
	"strconv"
)

// BENCH:BEGIN
func circularShift(x int, shift int) string {
	s := strconv.Itoa(x)
	if shift > len(s) {
		r := []rune(s)
		for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
			r[i], r[j] = r[j], r[i]
		}
		return string(r)
	}
	return s[len(s)-shift:] + s[:len(s)-shift]
}
// BENCH:END

func main() {
	if circularShift(100, 2) != "001" || circularShift(97, 8) != "79" {
		panic("case")
	}
	fmt.Println("ok")
}
'''

FILES["cpp/65_circular_shift.cpp"] = '''#include <algorithm>
#include <cstdio>
#include <string>

// BENCH:BEGIN
std::string circular_shift(int x, int shift) {
    std::string s = std::to_string(x);
    if (shift > static_cast<int>(s.size())) {
        std::reverse(s.begin(), s.end());
        return s;
    }
    return s.substr(s.size() - shift) + s.substr(0, s.size() - shift);
}
// BENCH:END

int main() {
    if (circular_shift(100, 2) != "001") return 1;
    if (circular_shift(97, 8) != "79") return 2;
    std::printf("ok\\n");
    return 0;
}
'''

FILES["c/65_circular_shift.c"] = '''#include <stdio.h>
#include <stdlib.h>
#include <string.h>

// BENCH:BEGIN
char* circular_shift(long x, long shift) {
    char buf[32];
    snprintf(buf, sizeof(buf), "%ld", x);
    int n = (int)strlen(buf);
    char* out = (char*)malloc((size_t)n + 1);
    if (shift > n) {
        for (int i = 0; i < n; i++) {
            out[i] = buf[n - 1 - i];
        }
    } else {
        for (int i = 0; i < n; i++) {
            out[i] = buf[(n - (int)shift + i) % n];
        }
    }
    out[n] = '\\0';
    return out;
}
// BENCH:END

int main(void) {
    char* a = circular_shift(100, 2);
    if (strcmp(a, "001") != 0) return 1;
    free(a);
    char* b = circular_shift(97, 8);
    if (strcmp(b, "79") != 0) return 2;
    free(b);
    printf("ok\\n");
    return 0;
}
'''

FILES["rs/65_circular_shift.rs"] = '''// BENCH:BEGIN
fn circular_shift(x: i64, shift: usize) -> String {
    let s = x.to_string();
    if shift > s.len() {
        return s.chars().rev().collect();
    }
    let cut = s.len() - shift;
    format!("{}{}", &s[cut..], &s[..cut])
}
// BENCH:END

fn main() {
    assert_eq!(circular_shift(100, 2), "001");
    assert_eq!(circular_shift(97, 8), "79");
    println!("ok");
}
'''

FILES["kl/65_circular_shift.kl"] = '''import std.io
import std.string

// BENCH:BEGIN
fn reverse_chars(String s, i64 n) String {
    String out = std.string.create("")
    for i in range(n) {
        String c = std.string.substring(s, as<size_t>(n - 1 - i), as<size_t>(n - i))
        out = std.string.concat(out, c)
    }
    return out
}

fn circular_shift(i64 x, i64 shift) String {
    String s = std.string.create_from_int(x)
    i64 n = as<i64>(std.string.length(s))
    if shift > n {
        return reverse_chars(s, n)
    }
    String head = std.string.substring(s, as<size_t>(n - shift), as<size_t>(n))
    String tail = std.string.substring(s, as<size_t>(0), as<size_t>(n - shift))
    return std.string.concat(head, tail)
}
// BENCH:END

fn main() i32 {
    String a = circular_shift(100, 2)
    String b = circular_shift(97, 8)
    String c = circular_shift(11, 101)
    if std.string.compare(a, std.string.create("001")) != 0 { return 1 }
    if std.string.compare(b, std.string.create("79")) != 0 { return 2 }
    if std.string.compare(c, std.string.create("11")) != 0 { return 3 }
    std.io.println("ok")
    return 0
}
'''


def main():
    for rel, content in FILES.items():
        p = SOL / rel
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text(content, encoding="utf-8")
    print(f"wrote {len(FILES)} files to {SOL}")


if __name__ == "__main__":
    main()
