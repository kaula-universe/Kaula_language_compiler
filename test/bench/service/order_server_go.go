// Go 版 TCP 订单处理服务——与 kaula_order_server.c 同协议、同业务流水线。
// 每连接一个 goroutine：bufio 读行 → 解析 → checksum256(256轮) → qty*price
// → 200us sleep（模拟下游 IO）→ 编码响应。
package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

const port = 8124
const dbSleep = 200 * time.Microsecond

func checksum256(s string) uint32 {
	h := uint32(0x811C9DC5)
	for round := 0; round < 256; round++ {
		for i := 0; i < len(s); i++ {
			h = (h ^ uint32(s[i])) * 16777619 + uint32(round)
		}
	}
	return h
}

// parseOrder 解析 "O,<seq>,<item>,<qty>,<price>"
func parseOrder(line string) (uint32, int, int, bool) {
	parts := strings.Split(line, ",")
	if len(parts) != 5 || parts[0] != "O" {
		return 0, 0, 0, false
	}
	seq, err1 := strconv.ParseUint(parts[1], 10, 32)
	qty, err2 := strconv.Atoi(parts[3])
	price, err3 := strconv.Atoi(parts[4])
	if err1 != nil || err2 != nil || err3 != nil {
		return 0, 0, 0, false
	}
	return uint32(seq), qty, price, true
}

// BizRecord 模拟业务对象：与 Kaula 侧同语义（每请求分配/填充/校验/释放）
type BizRecord struct {
	OrderID  uint64
	Qty      int32
	Price    int32
	Total    int32
	ItemHash uint32
	TsNS     uint64
	Reserved [6]uint64
	Payload  [32]byte
}

// mixedVerify 二级计算成分（与 Kaula 侧同语义的 64 轮混合哈希）
func mixedVerify(r *BizRecord, salt uint64) uint64 {
	h := salt
	for i := 0; i < 64; i++ {
		h ^= h << 13
		h ^= h >> 7
		h ^= h << 17
		h += uint64(r.Qty)*2654435761 + uint64(r.Price)
		h *= 0x9E3779B97F4A7C15
	}
	return h
}

func handleConn(c net.Conn) {
	defer c.Close()
	r := bufio.NewReaderSize(c, 4096)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		ck := checksum256(line)
		seq, qty, price, ok := parseOrder(line)
		if !ok {
			ck = 0xDEADBEEF
		}
		total := qty * price
		// 复合负载：分配业务对象 → 填充 → 二级校验 → 丢弃（GC 回收）
		rec := &BizRecord{OrderID: uint64(seq), Qty: int32(qty), Price: int32(price),
			Total: int32(total), ItemHash: ck, TsNS: uint64(time.Now().UnixNano())}
		v := mixedVerify(rec, uint64(ck)^uint64(seq))
		ck ^= uint32(v>>32) ^ uint32(v)
		time.Sleep(dbSleep) // 模拟下游 IO
		resp := fmt.Sprintf("R,%d,%08x,%d\n", seq, ck, total)
		if _, err := c.Write([]byte(resp)); err != nil {
			return
		}
	}
}

func main() {
	ls, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		fmt.Fprintln(os.Stderr, "listen:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "[go] order server on port %d\n", port)
	for {
		c, err := ls.Accept()
		if err != nil {
			continue
		}
		go handleConn(c)
	}
}
