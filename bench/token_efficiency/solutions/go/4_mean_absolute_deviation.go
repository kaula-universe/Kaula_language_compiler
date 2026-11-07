// BENCH:PREFIX
package main

import (
// BENCH:BEGIN
	"fmt"
	"math"
)

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

// BENCH:MAIN:BEGIN
func main() {
	if math.Abs(meanAbsoluteDeviation([]float64{1.0, 2.0, 3.0, 4.0})-1.0) > 1e-6 {
		panic("case1")
	}
	fmt.Println("ok")
}
// BENCH:MAIN:END
