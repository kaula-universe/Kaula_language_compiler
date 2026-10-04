// Go 侧基准驱动 —— 与 Kaula 模板 / Tokio 逐字对齐。
//
// 三边必须跑同一个函数、同一组常量、同样多的循环，否则测出来的差值里
// 就混进了「工作量不同」这一项。见 TRANSACTION.md。
//
// 用法：bench_go.exe <curve> <conc> <total>
//
//	curve: light | suspend | mixed | degrade
//
// 计时口径（对齐 kaula_conc_task.c 的内核打点）：
//
//	Kaula 在 kaula_task_init 里打 submit_ns（进队列之前），
//	在 kaula_task_complete 里打 complete_ns（置 DONE 之前）。
//	所以这里 submit 取在 `go` 语句之前（含 goroutine 创建 + 入队），
//	complete 取在任务体算完那一刻（含排队等待 + 执行）。
//
//	注意：TRANSACTION.md 里写的还是旧口径「提交→拿到结果」，那已被
//	e55adb9 判定为错误 —— 并发窗口里按「调用方何时取到结果」量到的是
//	排队 + 顺序取回，实测 p50=3.15s 而整轮只用 26ms。以代码为准。
package main

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"
)

const (
	// ---- 事务常量：与 kaula/bench.kl.tmpl、tokio/bench/src/main.rs 完全一致 ----
	steps    = 256
	seedVal  = 2545
	mult     = 1103515245
	addConst = 12345
	shift    = 1048576 // 2^20
	// 2^63 - 1：先 % 2^20 再乘，中间值 < 2^51，不需要 64 位环绕原语
	modP = 9223372036854775807
	// 滚动散列必须先压到 2^30 再 *31：9.2e18*31 会溢出有符号 64 位
	mask30 = 1073741824

	// 交叉校验的期望值：三边 parse_and_checksum 必须都算出它
	expectedSum = 13356499072

	maxSamples  = 2000
	degradeSpin = 20000 // 超过预算量子 256，验预算抢占
	suspendUs   = 200
)

// parseAndChecksum —— 事务本体。
//
// 与模板逐字对应。先 % 1048576 再乘：state < 2^50；先 % 1073741824 再 *31：
// 中间值 < 2^35。Go 的 int64 溢出是静默环绕，但这里压根不会溢出。
func parseAndChecksum() int64 {
	state := int64(seedVal)
	checksum := int64(0)
	for k := 0; k < steps; k++ {
		state = ((state%shift)*mult + addConst) % modP
		checksum = ((checksum%mask30)*31 + (state % 256)) % modP
	}
	return checksum
}

// runCurve —— 按曲线跑一个事务。
// withSleep 只对 mixed 有意义，按发单序奇偶定，不用随机源。
func runCurve(curve string, withSleep bool) int64 {
	switch curve {
	case "suspend":
		time.Sleep(suspendUs * time.Microsecond)
		return parseAndChecksum()
	case "mixed":
		if withSleep {
			time.Sleep(suspendUs * time.Microsecond)
		}
		return parseAndChecksum()
	case "degrade":
		// 忙循环而不是睡 —— 睡走定时器，那是 suspend 那条路。
		// 这里要的是「占住 Worker 的计算」，正是预算与停滞判定该管的东西。
		acc := int64(1)
		for r := 0; r < degradeSpin; r++ {
			acc = ((acc%mask30)*mult + addConst) % modP
		}
		return acc ^ parseAndChecksum()
	default: // light
		return parseAndChecksum()
	}
}

// percentile 取排序后样本的千分位值（permille：500=p50, 990=p99, 999=p99.9,
// 1000=max），与 Kaula 模板的下标公式统一口径。
// 夹到 n-1：n 很小时 (n*permille)/1000 可能落到 n 上。
func percentile(sorted []int64, permille int) int64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	idx := (n * permille) / 1000
	if idx >= n {
		idx = n - 1
	}
	return sorted[idx]
}

func die(format string, a ...any) {
	fmt.Printf(format, a...)
	fmt.Println()
	os.Exit(1)
}

func main() {
	// 提高系统时钟分辨率到 1ms，与 Kaula 侧 792960d 的 timeBeginPeriod(1) 对齐。
	// 不调的话 Windows 默认 15.625ms 的时钟粒度会把每次 200µs 的 sleep
	// 放大一个数量级，suspend/mixed 两条曲线对 Kaula/Tokio 不公平。
	beginTimerPeriod()
	defer lowerTimerPeriod()

	if len(os.Args) != 4 {
		die("BENCH_ERROR usage: bench_go <curve> <conc> <total>")
	}
	curve := os.Args[1]
	conc, err1 := strconv.Atoi(os.Args[2])
	total, err2 := strconv.Atoi(os.Args[3])
	if err1 != nil || err2 != nil || conc <= 0 || total <= 0 {
		die("BENCH_ERROR bad args: %v", os.Args[1:])
	}
	switch curve {
	case "light", "suspend", "mixed", "degrade":
	default:
		die("BENCH_ERROR unknown curve %q", curve)
	}

	// 预热：确认事务本体逐位正确。三边任何一个算错都在这里暴露，
	// 否则「跑得飞快但算错了」不会有任何断言失败。
	if w := parseAndChecksum(); w != expectedSum {
		die("BENCH_ERROR warmup checksum %d want %d", w, expectedSum)
	}
	// 这条曲线的期望值：degrade 会走 acc^parse 的路径，与上面不同。
	expected := runCurve(curve, false)

	// 每窗口复用的缓冲：循环内 make 会让分配混进被测量的时间。
	lat := make([]int64, maxSamples)
	res := make([]int64, conc)
	latW := make([]int64, conc)
	// 存 nowTicks() 的原始读数，不是 time.Time。
	// time.Now() 在 Windows 上的单调分量是 InterruptTime（粒度 10~40µs），
	// 而事务只有 ~2.4µs，差值会全是 0；nowTicks 走 QPC，粒度 100ns，
	// 与 Kaula 的 kaula_p_now_ns / Rust 的 Instant 同源。
	sub := make([]int64, conc)

	var wg sync.WaitGroup
	var checksum, errors int64
	n, issued, done := 0, 0, 0

	fmt.Println("PHASE load_start")
	t0 := nowTicks()

	// 窗口式：先填满 conc 个在途，再统一取回。
	//
	// 不逐个「提交-等-提交」串行：那测的是单任务往返延迟，没有并发在飞，
	// TPS 会低一个数量级。也不按大批量提交后统一等：批边界会形成同步点，
	// 测出来的是「批」的吞吐而不是调度的吞吐。
	for done < total {
		filled := 0
		for j := 0; j < conc && issued < total; j++ {
			// 在 `go` 之前打点：与 Kaula 的 submit_ns（进队列之前）同口径，
			// goroutine 创建与入队成本要计入调度器。
			sub[j] = nowTicks()
			withSleep := issued%2 == 0 // mixed 按奇偶定，50% 且不需要随机源
			wg.Add(1)
			go func(j int, withSleep bool) {
				defer wg.Done()
				v := runCurve(curve, withSleep)
				// 任务体算完那一刻打点 = Kaula 的 complete_ns（置 DONE 之前）
				end := nowTicks()
				res[j] = v
				latW[j] = ticksToNs(end - sub[j])
			}(j, withSleep)
			issued++
			filled++
		}
		wg.Wait()

		// 顺序取回：与模板的 in_flight[j].await 对齐。
		// 保持同序便于逐行比对（现在是滚动散列，本身已与顺序相关）。
		for j := 0; j < filled; j++ {
			// 累加器必须三边同式且都不溢出。原来的 (checksum + res) % modP
			// 在 degrade 上必溢出：res ≈ 9.8e18，第 10 次累加就越过有符号
			// 64 位。Go/Rust 静默回绕，而 Kaula 的乘加是检查溢出并陷阱 ——
			// 同一行代码三个目标语义不同，Kaula 直接 KAULA FATAL 退出。
			// 改成滚动散列：两边先压到 2^30，最大中间值 32*(2^30-1) < 2^35。
			checksum = ((checksum%mask30)*31 + (res[j]%mask30)) % modP
			if res[j] != expected {
				errors++
			}
			if n < maxSamples {
				lat[n] = latW[j]
				n++
			}
			done++
		}
	}
	elapsed := ticksToNs(nowTicks() - t0)

	// 静置期：给运行时一点时间回收，之后才测 RSS 回落。
	fmt.Println("PHASE load_end")
	time.Sleep(400 * time.Millisecond)
	fmt.Println("PHASE settle")

	// 分位数在计时区之外算：排序本身不该污染被测量的时间。
	sorted := lat[:n]
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	tps := int64(0)
	if elapsed > 0 {
		tps = int64(total) * 1_000_000_000 / elapsed
	}

	// 单行输出：多行调用与自动补分号的脚本会互相干扰。
	fmt.Printf("RESULT curve=%s tps=%d elapsed_ns=%d p50=%d p95=%d p99=%d p999=%d max=%d n=%d win=%d errors=%d checksum=%d\n",
		curve, tps, elapsed,
		percentile(sorted, 500), percentile(sorted, 950),
		percentile(sorted, 990), percentile(sorted, 999),
		percentile(sorted, 1000), n, conc, errors, checksum)
}
