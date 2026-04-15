package timeout

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sync/atomic"
	"time"
)

var (
	// 全局内存限制（字节）
	memoryLimit uint64 = 256 * 1024 * 1024 // 256MB
	
	// 全局时间限制（毫秒）
	timeLimit uint64 = 3000 // 3 秒
	
	// 当前内存使用
	currentMemory uint64
	
	// 开始时间
	startTime time.Time
	
	// 是否已超时
	timedOut int32
)

// TimeoutError 超时错误
type TimeoutError struct {
	Stage     string
	ElapsedMs int64
	LimitMs   uint64
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("timeout in %s stage: elapsed %dms, limit %dms", e.Stage, e.ElapsedMs, e.LimitMs)
}

// MemoryError 内存超限错误
type MemoryError struct {
	Stage   string
	Current uint64
	Limit   uint64
}

func (e *MemoryError) Error() string {
	return fmt.Sprintf("memory limit exceeded in %s stage: current %dMB, limit %dMB", e.Stage, e.Current/1024/1024, e.Limit/1024/1024)
}

// Init 初始化超时控制
func Init() {
	startTime = time.Now()
	atomic.StoreInt32(&timedOut, 0)
}

// SetLimits 设置限制
func SetLimits(memoryMB uint64, timeoutSec uint64) {
	memoryLimit = memoryMB * 1024 * 1024
	timeLimit = timeoutSec * 1000
}

// CheckTimeout 检查是否超时
func CheckTimeout(stage string) error {
	elapsed := time.Since(startTime).Milliseconds()
	limit := atomic.LoadUint64(&timeLimit)
	
	if elapsed > int64(limit) {
		atomic.StoreInt32(&timedOut, 1)
		return &TimeoutError{
			Stage:     stage,
			ElapsedMs: elapsed,
			LimitMs:   limit,
		}
	}
	
	// 打印调试信息（如果超过 80% 时间）
	if elapsed > int64(limit)*80/100 {
		fmt.Fprintf(os.Stderr, "⚠️  WARNING: %s stage taking too long (%dms/%dms)\n", stage, elapsed, limit)
		printDebugInfo(stage)
	}
	
	return nil
}

// CheckMemory 检查内存使用
func CheckMemory(stage string) error {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	current := m.Alloc
	
	if current > atomic.LoadUint64(&memoryLimit) {
		return &MemoryError{
			Stage:   stage,
			Current: current,
			Limit:   atomic.LoadUint64(&memoryLimit),
		}
	}
	
	// 打印调试信息（如果使用超过 80% 内存）
	if current > atomic.LoadUint64(&memoryLimit)*80/100 {
		fmt.Fprintf(os.Stderr, "⚠️  WARNING: %s stage using too much memory (%dMB/%dMB)\n", 
			stage, current/1024/1024, atomic.LoadUint64(&memoryLimit)/1024/1024)
		printDebugInfo(stage)
	}
	
	return nil
}

// printDebugInfo 打印调试信息
func printDebugInfo(stage string) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	
	fmt.Fprintf(os.Stderr, "=== Debug Info for %s ===\n", stage)
	fmt.Fprintf(os.Stderr, "  Time elapsed: %dms\n", time.Since(startTime).Milliseconds())
	fmt.Fprintf(os.Stderr, "  Memory allocated: %dMB\n", m.Alloc/1024/1024)
	fmt.Fprintf(os.Stderr, "  Memory total: %dMB\n", m.TotalAlloc/1024/1024)
	fmt.Fprintf(os.Stderr, "  Goroutines: %d\n", runtime.NumGoroutine())
	fmt.Fprintf(os.Stderr, "  GC runs: %d\n", m.NumGC)
	fmt.Fprintf(os.Stderr, "========================\n")
}

// WithTimeout 创建带超时的上下文
func WithTimeout(stage string) (context.Context, context.CancelFunc) {
	limit := atomic.LoadUint64(&timeLimit)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(limit)*time.Millisecond)
	
	// 启动监控协程
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		
		for {
			select {
			case <-ticker.C:
				if err := CheckTimeout(stage); err != nil {
					cancel()
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	
	return ctx, cancel
}

// IsTimedOut 检查是否已超时
func IsTimedOut() bool {
	return atomic.LoadInt32(&timedOut) == 1
}

// Reset 重置超时控制
func Reset() {
	startTime = time.Now()
	atomic.StoreInt32(&timedOut, 0)
}

// GetElapsed 获取已用时间
func GetElapsed() time.Duration {
	return time.Since(startTime)
}
