// Go 版订单服务 —— 与 Kaula(kl) / Tokio 版逐位等价（见 TRANSACTION.md）。
//
// 每连接一个 goroutine：bufio 读行 → checksum256(256轮, u32回绕)
//   → 手写扫描解析 → 堆分配 64B 业务对象 → 64轮 i32 混合校验
//   → sleep 200us（模拟下游 IO）→ 编码 "R,<seq>,<hex8>,<total>\n"
package main

import (
	"bufio"
	"net"
	"os"
	"strconv"
	"time"
)

const (
	defaultPort  = 8124
	totalSleepUs = 200
)

func checksum256(b []byte) uint32 {
	h := uint32(0x811C9DC5)
	for round := 0; round < 256; round++ {
		for i := 0; i < len(b); i++ {
			h = (h ^ uint32(b[i])) * 16777619
			h = h + uint32(round)
		}
	}
	return h
}

// parseField 手写扫描第 k 个逗号分隔字段（0 起始），与其余两边同法。
func parseField(line []byte, k int) int32 {
	i, f := 0, 0
	for i < len(line) && f < k {
		if line[i] == ',' {
			f++
		}
		i++
	}
	var v int32
	for i < len(line) && line[i] != ',' {
		v = v*10 + int32(line[i]-'0')
		i++
	}
	return v
}

// bizRecord 模拟业务对象：堆分配，让分配器进入每请求热路径。
type bizRecord struct {
	payload [64]byte
}

func handleConn(c net.Conn) {
	defer c.Close()
	r := bufio.NewReaderSize(c, 8192)
	w := bufio.NewWriterSize(c, 4096)
	resp := make([]byte, 0, 32)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return
		}
		// 去掉行尾 \n（\r 也去，服务端算法按行体长度算）
		n := len(line)
		for n > 0 && (line[n-1] == '\n' || line[n-1] == '\r') {
			n--
		}
		body := line[:n]

		ck := checksum256(body)
		seq := parseField(body, 1)
		qty := parseField(body, 3)
		price := parseField(body, 4)
		total := qty * price

		// S4：堆分配 + 64 轮 i32 混合校验（i32 原生回绕 ≡ Kaula as<i32>）
		rec := &bizRecord{}
		rec.payload[0] = byte(seq)
		rec.payload[1] = byte(qty)
		rec.payload[2] = byte(price)
		var h int32
		for k := 0; k < 64; k++ {
			h = (h ^ int32(rec.payload[k%64])) + seq + qty + price
			h = h*31 + int32(k)
		}
		ck ^= uint32(h)

		// S5：模拟下游 IO
		time.Sleep(totalSleepUs * time.Microsecond)

		// S6：编码 "R,<seq>,<hex8>,<total>\n"
		resp = resp[:0]
		resp = append(resp, 'R', ',')
		resp = strconv.AppendInt(resp, int64(seq), 10)
		resp = append(resp, ',')
		const hexdig = "0123456789abcdef"
		for s := 28; s >= 0; s -= 4 {
			resp = append(resp, hexdig[(ck>>uint(s))&0xF])
		}
		resp = append(resp, ',')
		resp = strconv.AppendInt(resp, int64(total), 10)
		resp = append(resp, '\n')

		if _, err := w.Write(resp); err != nil {
			return
		}
		if err := w.Flush(); err != nil {
			return
		}
	}
}

func main() {
	port := defaultPort
	var workers int
	if v := os.Getenv("SVC_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			port = p
		}
	}
	workers = 4 // 与 Kaula/Tokio 边默认一致（TRANSACTION.md §7）

	// 显式设定 GOMAXPROCS：否则测的是「机器核数」而不是三边同一配置
	if v := os.Getenv("SVC_WORKERS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			workers = n
		}
	}
	runtimeGOMAXPROCS(workers)

	addr := "0.0.0.0:" + strconv.Itoa(port)
	ls, err := net.Listen("tcp", addr)
	if err != nil {
		os.Stderr.WriteString("[go] listen: " + err.Error() + "\n")
		os.Exit(1)
	}
	os.Stderr.WriteString("[go] order server on " + addr +
		" GOMAXPROCS=" + strconv.Itoa(workers) + "\n")
	for {
		c, err := ls.Accept()
		if err != nil {
			continue
		}
		if tc, ok := c.(*net.TCPConn); ok {
			tc.SetNoDelay(true)
		}
		go handleConn(c)
	}
}
