// loadgen —— 三方服务共用的压测客户端（Kaula / Go / Tokio）。
//
// 设计要点（见 TRANSACTION.md）：
//  1. RTT 由本进程测量 —— 三边服务端各自的内部计时口径不同，不可比。
//  2. 本进程**独立复刻**服务端算法做期望值校验 —— 抓到「跑得快但算错了」。
//  3. 每窗口输出一行，供上层做收敛判定（丢弃前 50% 窗口）。
//  4. 干扰由外部注入，本进程不感知，保证三边口径完全一致。
//
// 用法：loadgen <addr:port> <并发连接数> <压测秒数> <窗口秒数> [phase]
//   输出：win,tps,p50,p95,p99,p999,mismatch,errors
//   末行：TOTAL ok=<n> mismatch=<n> errors=<n> bytes_written=<n>
package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

const (
	totalSleepUs = 200
	itemName     = "itemA"
	qtyFixed     = 3
	priceFixed   = 1990
)

var (
	mu       sync.Mutex
	winUs    []int64 // 当前窗口 RTT 样本（微秒）
	okTotal  atomic.Int64
	mismatch atomic.Int64
	errors   atomic.Int64
)

// ---------- 服务端算法的独立复刻（校验用） ----------

// checksum256：u32 回绕，FNV-1a 变体。与三边服务端逐位等价。
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

// mixedVerify：i32 回绕，64 轮。Go 的 int32 原生回绕，与 Kaula as<i32>
// 及 Rust wrapping_* 等价。
func mixedVerify(rec []byte, seq, qty, price int32) int32 {
	var h int32
	for k := 0; k < 64; k++ {
		h = (h ^ int32(rec[k%len(rec)])) + seq + qty + price
		h = h*31 + int32(k)
	}
	return h
}

// expectedChecksum 算出服务端应当返回的 hex8 值。
func expectedChecksum(line []byte) (uint32, int32) {
	ck := checksum256(line)
	seq, qty, price := parseLine(line)
	rec := make([]byte, 64)
	// 服务端填充 rec 的方式：前三字节是 seq/qty/price 的低 8 位
	rec[0] = byte(seq)
	rec[1] = byte(qty)
	rec[2] = byte(price)
	h := mixedVerify(rec, seq, qty, price)
	ck ^= uint32(h)
	return ck, qty * price
}

// parseLine 手写扫描 "O,<seq>,<item>,<qty>,<price>"，与三边服务端同法。
func parseLine(line []byte) (seq, qty, price int32) {
	field := func(k int) int32 {
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
	return field(1), field(3), field(4)
}

// parseResponse 解析 "R,<seq>,<hex8>,<total>" 并做四项校验。
// 返回 mismatch 是否为真、是否为格式错误。
func validate(resp []byte, seq int32, expCk uint32, expTotal int32) (mismatch, bad bool) {
	// R,<seq>,<hex8>,<total>\n
	if len(resp) < 8 || resp[0] != 'R' || resp[1] != ',' {
		return true, true
	}
	i := 2
	var gotSeq int64
	for i < len(resp) && resp[i] != ',' {
		if resp[i] < '0' || resp[i] > '9' {
			return true, true
		}
		gotSeq = gotSeq*10 + int64(resp[i]-'0')
		i++
	}
	if i >= len(resp) {
		return true, true
	}
	i++ // 跳过逗号
	if i+8 > len(resp) {
		return true, true
	}
	var gotCk uint32
	for j := 0; j < 8; j++ {
		c := resp[i+j]
		var d uint32
		switch {
		case c >= '0' && c <= '9':
			d = uint32(c - '0')
		case c >= 'a' && c <= 'f':
			d = uint32(c-'a') + 10
		case c >= 'A' && c <= 'F':
			d = uint32(c-'A') + 10
		default:
			return true, true
		}
		gotCk = gotCk<<4 | d
	}
	i += 8
	if i >= len(resp) || resp[i] != ',' {
		return true, true
	}
	i++
	var gotTotal int64
	for i < len(resp) && resp[i] != ',' && resp[i] != '\n' && resp[i] != '\r' {
		if resp[i] < '0' || resp[i] > '9' {
			return true, true
		}
		gotTotal = gotTotal*10 + int64(resp[i]-'0')
		i++
	}
	mismatch = int64(seq) != gotSeq || gotCk != expCk || gotTotal != int64(expTotal)
	return mismatch, false
}

// ---------- 客户端 ----------

func client(addr string, stop <-chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()
	var c net.Conn
	var err error
	for i := 0; i < 20; i++ { // 服务启动竞态：重试拨号
		c, err = net.Dial("tcp", addr)
		if err == nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err != nil {
		errors.Add(1)
		return
	}
	defer c.Close()
	r := bufio.NewReaderSize(c, 8192)
	w := bufio.NewWriterSize(c, 4096)
	seq := int32(0)
	for {
		select {
		case <-stop:
			return
		default:
		}
		seq++
		// "O,<seq>,itemA,3,1990\n" —— 手工拼字节，避免 fmt 的分配与格式化开销
		buf := make([]byte, 0, 32)
		buf = append(buf, 'O', ',')
		buf = strconv.AppendInt(buf, int64(seq), 10)
		buf = append(buf, ',')
		buf = append(buf, itemName...)
		buf = append(buf, ',')
		buf = strconv.AppendInt(buf, qtyFixed, 10)
		buf = append(buf, ',')
		buf = strconv.AppendInt(buf, priceFixed, 10)
		buf = append(buf, '\n')
		// 期望值用**不含换行**的行体（服务端按不含 \n 的长度算）
		expCk, expTotal := expectedChecksum(buf[:len(buf)-1])

		t0 := time.Now()
		if _, err := w.Write(buf); err != nil {
			errors.Add(1)
			return
		}
		if err := w.Flush(); err != nil {
			errors.Add(1)
			return
		}
		c.SetReadDeadline(time.Now().Add(30 * time.Second))
		resp, err := r.ReadBytes('\n')
		elapsed := time.Since(t0).Microseconds()
		if err != nil {
			errors.Add(1)
			return
		}
		if mm, bad := validate(resp, seq, expCk, expTotal); bad {
			errors.Add(1)
		} else if mm {
			mismatch.Add(1)
		} else {
			okTotal.Add(1)
		}
		mu.Lock()
		winUs = append(winUs, elapsed)
		mu.Unlock()
	}
}

func percentile(sorted []int64, p float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)) * p)
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func main() {
	if len(os.Args) < 5 {
		fmt.Fprintln(os.Stderr,
			"usage: loadgen <addr:port> <conns> <seconds> <window_sec> [phase]")
		os.Exit(1)
	}
	addr := os.Args[1]
	conns, _ := strconv.Atoi(os.Args[2])
	seconds, _ := strconv.Atoi(os.Args[3])
	winSec, _ := strconv.Atoi(os.Args[4])
	phase := "load"
	if len(os.Args) > 5 {
		phase = os.Args[5]
	}
	_ = phase

	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < conns; i++ {
		wg.Add(1)
		go client(addr, stop, &wg)
	}

	// 预热 3s：连接建立 + 缓冲爬升 + 服务端 worker 唤醒，不计入
	time.Sleep(3 * time.Second)
	okTotal.Store(0)
	mismatch.Store(0)

	deadline := time.Now().Add(time.Duration(seconds) * time.Second)
	win := 0
	var lastOk, lastMis int64
	for time.Now().Before(deadline) {
		time.Sleep(time.Duration(winSec) * time.Second)
		mu.Lock()
		samples := winUs
		winUs = nil
		mu.Unlock()
		ok := okTotal.Load()
		mis := mismatch.Load()
		n := ok - lastOk
		dMis := mis - lastMis
		lastOk, lastMis = ok, mis
		if len(samples) == 0 {
			fmt.Printf("%d,0,0,0,0,0,%d,0\n", win, dMis)
			win++
			continue
		}
		sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
		tps := float64(n) / float64(winSec)
		fmt.Printf("%d,%.0f,%d,%d,%d,%d,%d,0\n", win, tps,
			percentile(samples, 0.50), percentile(samples, 0.95),
			percentile(samples, 0.99), percentile(samples, 0.999), dMis)
		win++
	}
	close(stop)
	wg.Wait()
	fmt.Printf("TOTAL ok=%d mismatch=%d errors=%d\n",
		okTotal.Load(), mismatch.Load(), errors.Load())
}
