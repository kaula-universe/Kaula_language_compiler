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
	
	// 各阶段统计
	stageStats = make(map[string]*StageStat)
)

// StageStat 阶段统计信息
type StageStat struct {
	Name          string
	StartTime     time.Time
	EndTime       time.Time
	StartMemory   uint64
	EndMemory     uint64
	PeakMemory    uint64
	AllocCount    int64
	GCCount       uint32
	LastFunc      string
	LastFile      string
	LastLine      int
}

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

// StartStage 开始一个阶段
func StartStage(name string) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	
	stageStats[name] = &StageStat{
		Name:        name,
		StartTime:   time.Now(),
		StartMemory: m.Alloc,
		GCCount:     m.NumGC,
	}
}

// EndStage 结束一个阶段
func EndStage(name string) {
	if stat, ok := stageStats[name]; ok {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		
		stat.EndTime = time.Now()
		stat.EndMemory = m.Alloc
		stat.GCCount = m.NumGC - stat.GCCount
		
		if m.Alloc > stat.PeakMemory {
			stat.PeakMemory = m.Alloc
		}
		
		stat.AllocCount = int64(m.Mallocs - m.Frees)
	}
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
		printGoroutineStack()
	}
	
	return nil
}

// CheckMemory 检查内存使用
func CheckMemory(stage string) error {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	current := m.Alloc
	
	// 更新阶段统计
	if stat, ok := stageStats[stage]; ok {
		if current > stat.PeakMemory {
			stat.PeakMemory = current
		}
	}
	
	if current > atomic.LoadUint64(&memoryLimit) {
		printMemoryHotspots()
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
		printMemoryHotspots()
		printGoroutineStack()
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
	
	// 打印阶段统计
	if stat, ok := stageStats[stage]; ok {
		fmt.Fprintf(os.Stderr, "  Stage duration: %dms\n", stat.EndTime.Sub(stat.StartTime).Milliseconds())
		fmt.Fprintf(os.Stderr, "  Stage memory delta: %dMB -> %dMB (+%dMB)\n", 
			stat.StartMemory/1024/1024, stat.EndMemory/1024/1024, 
			(stat.EndMemory-stat.StartMemory)/1024/1024)
		fmt.Fprintf(os.Stderr, "  Peak memory: %dMB\n", stat.PeakMemory/1024/1024)
		fmt.Fprintf(os.Stderr, "  Allocations: %d\n", stat.AllocCount)
		fmt.Fprintf(os.Stderr, "  GC during stage: %d\n", stat.GCCount)
	}
	
	fmt.Fprintf(os.Stderr, "  Heap objects: %d\n", m.HeapObjects)
	fmt.Fprintf(os.Stderr, "  Heap alloc: %dMB\n", m.HeapAlloc/1024/1024)
	fmt.Fprintf(os.Stderr, "  Stack in use: %dKB\n", m.StackInuse/1024)
	fmt.Fprintf(os.Stderr, "  Next GC: %dMB\n", m.NextGC/1024/1024)
	fmt.Fprintf(os.Stderr, "  Pause total: %dms\n", m.PauseTotalNs/1000000)
	fmt.Fprintf(os.Stderr, "========================\n")
}

// printMemoryHotspots 打印内存热点（最大的对象分配）
func printMemoryHotspots() {
	fmt.Fprintf(os.Stderr, "\n=== Memory Hotspots ===\n")
	
	// 获取堆信息
	buf := make([]byte, 1<<20)
	runtime.Stack(buf, true)
	
	// 分析 goroutine 栈
	goroutines := runtime.NumGoroutine()
	fmt.Fprintf(os.Stderr, "  Active goroutines: %d\n", goroutines)
	
	// 打印内存分配器统计
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	
	fmt.Fprintf(os.Stderr, "  Mallocs: %d\n", m.Mallocs)
	fmt.Fprintf(os.Stderr, "  Frees: %d\n", m.Frees)
	fmt.Fprintf(os.Stderr, "  Lookups: %d\n", m.Lookups)
	fmt.Fprintf(os.Stderr, "  Alloc rate: %.0f MB/s\n", float64(m.TotalAlloc)/1024/1024)
	
	fmt.Fprintf(os.Stderr, "========================\n\n")
}

// printGoroutineStack 打印所有 goroutine 的调用栈
func printGoroutineStack() {
	fmt.Fprintf(os.Stderr, "\n=== Goroutine Stack Trace ===\n")
	buf := make([]byte, 1<<20)
	stackLen := runtime.Stack(buf, true)
	
	if stackLen > 0 {
		fmt.Fprintf(os.Stderr, "%s\n", buf[:stackLen])
	}
	fmt.Fprintf(os.Stderr, "========================\n\n")
}

// PrintCurrentLocation 打印当前代码位置（用于追踪）
func PrintCurrentLocation(stage string) {
	_, file, line, ok := runtime.Caller(1)
	if ok {
		fmt.Fprintf(os.Stderr, "[DEBUG] %s: at %s:%d\n", stage, file, line)
	}
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

// GetMemoryStats 获取内存统计信息
func GetMemoryStats() (avgMemory uint64, maxMemory uint64) {
	if len(stageStats) == 0 {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return m.Alloc / 1024 / 1024, m.Alloc / 1024 / 1024
	}
	
	totalMemory := uint64(0)
	maxMemory = 0
	
	for _, stat := range stageStats {
		// 计算该阶段的平均内存使用（开始和结束的平均值）
		avgStageMemory := (stat.StartMemory + stat.EndMemory) / 2
		totalMemory += avgStageMemory
		
		// 更新最大内存使用
		if stat.PeakMemory > maxMemory {
			maxMemory = stat.PeakMemory
		}
	}
	
	// 计算平均值
	avgMemory = totalMemory / uint64(len(stageStats))
	
	// 转换为 MB
	avgMemory = avgMemory / 1024 / 1024
	maxMemory = maxMemory / 1024 / 1024
	
	return avgMemory, maxMemory
}
