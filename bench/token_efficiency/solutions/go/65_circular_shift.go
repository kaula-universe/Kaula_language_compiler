// BENCH:BEGIN
package main

import (
	"fmt"
	"strconv"
)

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

// BENCH:MAIN:BEGIN
func main() {
	if circularShift(100, 2) != "001" || circularShift(97, 8) != "79" {
		panic("case")
	}
	fmt.Println("ok")
}
// BENCH:MAIN:END
