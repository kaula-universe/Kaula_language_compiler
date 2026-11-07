// BENCH:BEGIN
package main

import "fmt"

func strlen(s string) int {
	return len(s)
}

// BENCH:END

// BENCH:MAIN:BEGIN
func main() {
	if strlen("") != 0 || strlen("abc") != 3 {
		panic("case")
	}
	fmt.Println("ok")
}
// BENCH:MAIN:END
