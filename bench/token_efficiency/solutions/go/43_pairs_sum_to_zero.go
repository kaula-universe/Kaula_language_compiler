// BENCH:PREFIX
package main

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

// BENCH:MAIN:BEGIN
func main() {
	if pairsSumToZero([]int{1, 3, 5, 0}) {
		panic("case1")
	}
	if !pairsSumToZero([]int{2, 4, -5, 3, 5, 7}) {
		panic("case2")
	}
	fmt.Println("ok")
}
// BENCH:MAIN:END
