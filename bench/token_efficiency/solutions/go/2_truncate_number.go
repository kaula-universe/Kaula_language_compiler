// BENCH:PREFIX
package main

import (
// BENCH:BEGIN
	"fmt"
	"math"
)

func truncateNumber(number float64) float64 {
	return math.Mod(number, 1.0)
}

// BENCH:END

// BENCH:MAIN:BEGIN
func main() {
	if truncateNumber(3.5) != 0.5 {
		panic("case1")
	}
	fmt.Println("ok")
}
// BENCH:MAIN:END
