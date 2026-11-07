// BENCH:PREFIX
package main

import (
// BENCH:BEGIN
	"fmt"
	"math"
)

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

// BENCH:MAIN:BEGIN
func main() {
	if !hasCloseElements([]float64{1.0, 2.0, 3.9, 4.0, 5.0, 2.2}, 0.3) {
		panic("case1")
	}
	if hasCloseElements([]float64{1.0, 2.0, 3.9, 4.0, 5.0, 2.2}, 0.05) {
		panic("case2")
	}
	fmt.Println("ok")
}
// BENCH:MAIN:END
