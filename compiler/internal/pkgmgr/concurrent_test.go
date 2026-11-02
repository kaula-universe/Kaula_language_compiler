package pkgmgr

import (
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
)

// TestFetchAllConcurrent_Parallelism 验证并发执行且结果与输入同序。
// 用 atomic 计数 + 释放屏障强制前 K 个任务同时进入临界区，
// 确保真的并发而非被 GOMAXPROCS 串行调度掩盖：到达的任务阻塞在 gate 上，
// 只有第 K 个到达者关闭 gate 后所有等待者才一起放行，因此 gate 打开瞬间
// 必然同时有 K 个任务在临界区中（running >= K），并发度断言即成立。
func TestFetchAllConcurrent_Parallelism(t *testing.T) {
	const barrier = 4 // 要求同时 ≥4 个任务在临界区
	names := []string{"a", "b", "c", "d", "e"}
	var running, maxRunning, arrived atomic.Int32

	// 屏障：前 4 个任务同时达到后才放行，制造真实并发窗口
	gate := make(chan struct{})

	slots := fetchAllConcurrent(names, func(name string) (*FetchResult, error) {
		cur := running.Add(1)
		defer running.Add(-1)
		for {
			old := maxRunning.Load()
			if cur <= old || maxRunning.CompareAndSwap(old, cur) {
				break
			}
		}
		// 第 K 个到达者负责关 gate，其余到达者阻塞等待
		if arrived.Add(1) == barrier {
			close(gate)
		}
		<-gate
		return &FetchResult{Name: name, Version: "1.0.0"}, nil
	})

	if err := firstError(slots); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 结果必须与输入同序
	for i, s := range slots {
		if s.name != names[i] || s.fr == nil || s.fr.Name != names[i] {
			t.Fatalf("slot %d = %+v, want %s", i, s, names[i])
		}
	}
	// gate 打开瞬间至少 barrier 个任务同时在临界区
	if m := maxRunning.Load(); m < barrier {
		t.Fatalf("expected concurrency >= %d, got max=%d", barrier, m)
	}
}

// TestFetchAllConcurrent_ErrorPropagation 验证错误传播（不 panic）。
func TestFetchAllConcurrent_ErrorPropagation(t *testing.T) {
	names := []string{"ok1", "bad", "ok2"}
	slots := fetchAllConcurrent(names, func(name string) (*FetchResult, error) {
		if name == "bad" {
			return nil, errors.New("boom")
		}
		return &FetchResult{Name: name}, nil
	})
	if err := firstError(slots); err == nil {
		t.Fatalf("expected error to propagate")
	} else if err.Error() != "boom" {
		t.Fatalf("wrong error: %v", err)
	}
	// 其他任务结果仍应记录
	if slots[0].fr == nil || slots[2].fr == nil {
		t.Fatalf("successful tasks should have results %+v", slots)
	}
}

// TestFetchAllConcurrent_Empty 空输入不报错。
func TestFetchAllConcurrent_Empty(t *testing.T) {
	slots := fetchAllConcurrent(nil, func(name string) (*FetchResult, error) {
		return nil, fmt.Errorf("should not be called")
	})
	if len(slots) != 0 {
		t.Fatalf("expected empty slots, got %d", len(slots))
	}
}
