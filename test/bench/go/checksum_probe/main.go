// 事务一致性：Go 侧必须与 Kaula 侧（checksum_probe.kl）算出同一个值。
//
// 期望值 13356499072 —— 由 Kaula 侧实测写死。
// 算错了也能跑得飞快、TPS 很好看，不会崩也没有其他断言会失败，
// 所以只能靠把期望值钉死来保证三边比的是同一件事。

package main

import "fmt"

const p = int64(9223372036854775807)

// parseAndChecksum 必须与 TRANSACTION.md 的定义逐位一致。
func parseAndChecksum() int64 {
	var state int64 = 2545
	var checksum int64
	for k := 0; k < 256; k++ {
		state = ((state%1048576)*1103515245 + 12345) % p
		checksum = ((checksum%1073741824)*31 + state%256) % p
	}
	return checksum
}

func main() {
	got := parseAndChecksum()
	const want int64 = 13356499072
	fmt.Printf("go  = %d\n", got)
	if got != want {
		fmt.Printf("MISMATCH want %d\n", want)
		return
	}
	fmt.Println("go agrees with kaula")
}
