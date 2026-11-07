// loadgen.go —— TCP 订单服务压测客户端（Kaula/Go 两服务共用）。
//
// 用法：go run loadgen.go <addr:port> <并发连接数> <时长秒> <窗口秒>
// 每窗口输出一行：win, tps, p50, p95, p99, p999（延迟单位 us）
// 恒定并发：每连接串行 发请求→等响应→发下一条，不做额外限速。
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

var windowUs []int64 // 当前窗口延迟样本
var mu sync.Mutex
var done atomic.Int64

func client(addr string, stop chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()
	var c net.Conn
	var err error
	for i := 0; i < 10; i++ { // 服务启动竞态：重试拨号
		c, err = net.Dial("tcp", addr)
		if err == nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "dial: %v\n", err)
		return
	}
	defer c.Close()
	r := bufio.NewReaderSize(c, 4096)
	seq := uint64(0)
	for {
		select {
		case <-stop:
			return
		default:
		}
		seq++
		req := fmt.Sprintf("O,%d,itemA,3,1990\n", seq)
		t0 := time.Now()
		if _, err := c.Write([]byte(req)); err != nil {
			return
		}
		c.SetReadDeadline(time.Now().Add(10 * time.Second)) // 防御：服务端异常时不被永久卡死
		_, err := r.ReadString('\n')
		if err != nil {
			return
		}
		elapsed := time.Since(t0).Microseconds()
		mu.Lock()
		windowUs = append(windowUs, elapsed)
		mu.Unlock()
		done.Add(1)
	}
}

func percentile(sorted []int64, p float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted))*p + 0.5)
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func main() {
	if len(os.Args) != 5 {
		fmt.Fprintln(os.Stderr, "usage: loadgen <addr:port> <conns> <seconds> <window_sec>")
		os.Exit(1)
	}
	addr := os.Args[1]
	conns, _ := strconv.Atoi(os.Args[2])
	seconds, _ := strconv.Atoi(os.Args[3])
	winSec, _ := strconv.Atoi(os.Args[4])

	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < conns; i++ {
		wg.Add(1)
		go client(addr, stop, &wg)
	}

	// 预热 3s（连接建立 + 缓冲区爬升），不计入
	time.Sleep(3 * time.Second)

	deadline := time.Now().Add(time.Duration(seconds) * time.Second)
	win := 0
	for time.Now().Before(deadline) {
		time.Sleep(time.Duration(winSec) * time.Second)
		mu.Lock()
		samples := windowUs
		windowUs = nil
		mu.Unlock()
		n := done.Swap(0)
		if len(samples) == 0 {
			fmt.Printf("%d,0,0,0,0,0\n", win)
			win++
			continue
		}
		sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
		tps := float64(n) / float64(winSec)
		fmt.Printf("%d,%.0f,%d,%d,%d,%d\n", win, tps,
			percentile(samples, 0.50), percentile(samples, 0.95),
			percentile(samples, 0.99), percentile(samples, 0.999))
		win++
	}
	close(stop)
	wg.Wait()
}
