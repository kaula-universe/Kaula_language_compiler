//go:build !windows

package main

import "time"

// 非 Windows：POSIX 的 CLOCK_MONOTONIC 本来就是纳秒级，
// time.Since 走的正是它，没有 Windows 上 InterruptTime 那个粒度问题。
//
// 这里返回纳秒而非 tick，ticksToNs 退化为恒等 —— 接口与 Windows 侧一致，
// main.go 不需要写平台分支。

var monoStart = time.Now()

func nowTicks() int64 {
	return time.Since(monoStart).Nanoseconds()
}

func ticksToNs(d int64) int64 { return d }

// POSIX 时钟粒度是纳秒，不需要抬系统定时器。
func beginTimerPeriod() {}
func lowerTimerPeriod() {}
