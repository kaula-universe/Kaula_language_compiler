//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

// Windows 平台支撑：时钟分辨率 + 系统定时器粒度。
//
// 两件事都必须与 Kaula 对齐，否则三个运行时不在同一把尺子上量。
// 不引入 golang.org/x/sys：基准只想依赖标准库，拉外部模块会带来网络依赖
// 与版本漂移；syscall.NewLazyDLL 是标准库且足够。

var (
	k32        = syscall.NewLazyDLL("kernel32.dll")
	procQPC    = k32.NewProc("QueryPerformanceCounter")
	procQPF    = k32.NewProc("QueryPerformanceFrequency")
	winmm      = syscall.NewLazyDLL("winmm.dll")
	procBeginP = winmm.NewProc("timeBeginPeriod")
	procEndP   = winmm.NewProc("timeEndPeriod")
)

// qpcFreq：QueryPerformanceFrequency，本机实测 10_000_000（名义 100ns/tick）。
var qpcFreq int64

func init() {
	var f int64
	_, _, _ = procQPF.Call(uintptr(unsafe.Pointer(&f)))
	if f > 0 {
		qpcFreq = f
	}
}

// nowTicks 返回单调时钟的原始读数。
//
// 必须走 QueryPerformanceCounter，不能用 time.Now()：
//
//	Go 在 Windows 上的 time.Now() 单调分量读的是 KUSER_SHARED_DATA.InterruptTime
//	（0x7ffe0008，见 src/runtime/time_windows_amd64.s 的 time·now），
//	由定时器中断更新，实测连续读 200 万次里 1999922 次返回同一个值 ——
//	粒度约 10~40µs。而单次事务只有 ~2.4µs，落在粒度以内，
//	导致 p50 全部读成 0（实测 5000 次里 4956 次为 0）。
//
//	Kaula 的 kaula_p_now_ns()（kaula_conc_platform.h:121）与 Rust 的
//	std::time::Instant 在 Windows 上同样走 QPC，实测粒度均为 100ns。
//	三边不同源，延迟分布就不可比。
//
// 返回原始 tick 而非纳秒：热路径上免掉两次 64 位除法，
// 只在最后聚合时 ticksToNs 换算一次。
func nowTicks() int64 {
	var c int64
	_, _, _ = procQPC.Call(uintptr(unsafe.Pointer(&c)))
	return c
}

// ticksToNs 把 nowTicks 的差值换算成纳秒。
// 与 kaula_p_now_ns 同构：整秒 + 余数两步，避免 ticks * 1e9 溢出
// （QPC 计数运行时约 10^13，乘 10^9 到 10^22，远超 int64 上限）。
func ticksToNs(d int64) int64 {
	if qpcFreq == 0 {
		return d
	}
	// 差值可能为负（理论上不该），但整除向下取整会让 -1 tick 变 0，
	// 这里不做修正 —— 负延迟本身就是需要暴露的异常。
	sec := d / qpcFreq
	rem := d % qpcFreq
	return sec*1_000_000_000 + rem*1_000_000_000/qpcFreq
}

// beginTimerPeriod 把系统定时器粒度提到 1ms，与 Kaula 内核 792960d 对齐。
// Windows 默认 15.625ms，不提的话每次 200µs 的 time.Sleep 会被放大一个
// 数量级，suspend/mixed 两条曲线对 Kaula/Tokio 就不公平了。
func beginTimerPeriod() {
	_, _, _ = procBeginP.Call(1)
}

func lowerTimerPeriod() {
	_, _, _ = procEndP.Call(1)
}
