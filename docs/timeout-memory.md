# 超时与内存控制

编译器内置资源监控，防止编译过程耗尽系统资源。实现位于 `internal/timeout/timeout.go`。

## 设计

- **实时监控**：跟踪编译时间和内存使用
- **阈值告警**：80% 阈值时发出警告
- **强制终止**：超出限制时自动终止
- **阶段统计**：记录每个编译阶段的资源使用

## 默认限制

```go
const (
    DefaultMemoryLimit = 256  // MB
    DefaultTimeout     = 3    // 秒
)

// kaulac 主程序覆盖
const (
    MemoryLimit = 4096  // MB
    Timeout     = 120   // 秒
)
```

## 核心类型

```go
type StageStat struct {
    Name       string        // 阶段名
    StartTime  time.Time     // 开始时间
    EndTime    time.Time     // 结束时间
    StartMem   uint64        // 开始内存
    EndMem     uint64        // 结束内存
    PeakMem    uint64        // 峰值内存
    GCCount    uint32        // GC 次数
}

type TimeoutError struct {
    Stage   string
    Elapsed time.Duration
    Limit   time.Duration
}

type MemoryError struct {
    Stage   string
    Current uint64
    Limit   uint64
}
```

## API

### 初始化

```go
// 初始化超时控制
timeout.Init()

// 设置限制
timeout.SetLimits(4096, 120)  // 4096MB, 120秒
```

### 阶段跟踪

```go
// 开始阶段
timeout.StartStage("Lexing + Parsing")

// ... 执行编译 ...

// 结束阶段
stat := timeout.EndStage("Lexing + Parsing")
fmt.Printf("Stage %s: %v, Memory: %d MB\n", 
    stat.Name, 
    stat.EndTime.Sub(stat.StartTime),
    stat.EndMem/1024/1024)
```

### 资源检查

```go
// 检查超时
if err := timeout.CheckTimeout("Semantic Analysis"); err != nil {
    log.Fatal(err)
}

// 检查内存
if err := timeout.CheckMemory("Code Generation"); err != nil {
    log.Fatal(err)
}

// 使用 WithTimeout 包装
err := timeout.WithTimeout("Compilation", func() error {
    // 执行编译
    return nil
})
```

### 状态查询

```go
// 检查是否超时
if timeout.IsTimedOut() {
    // 已超时
}

// 获取已用时间
elapsed := timeout.GetElapsed()

// 获取内存统计
stats := timeout.GetMemoryStats()
```

## 内存监控

### 内存统计

```go
func GetMemoryStats() map[string]uint64 {
    var m runtime.MemStats
    runtime.ReadMemStats(&m)
    
    return map[string]uint64{
        "alloc":     m.Alloc,      // 当前分配
        "total":     m.TotalAlloc, // 总分配
        "sys":       m.Sys,        // 系统内存
        "heap_alloc": m.HeapAlloc, // 堆分配
        "heap_sys":  m.HeapSys,    // 堆系统
        "heap_idle": m.HeapIdle,   // 堆空闲
        "heap_inuse": m.HeapInuse, // 堆使用
        "gc_num":    m.NumGC,      // GC 次数
    }
}
```

### 内存检查

```go
func CheckMemory(stage string) error {
    var m runtime.MemStats
    runtime.ReadMemStats(&m)
    
    currentMB := m.Alloc / 1024 / 1024
    
    // 80% 警告
    if currentMB > uint64(memoryLimit*0.8) {
        log.Printf("[Warning] Memory usage at %d MB (limit: %d MB)", 
            currentMB, memoryLimit)
    }
    
    // 超出限制
    if currentMB > uint64(memoryLimit) {
        return &MemoryError{
            Stage:   stage,
            Current: m.Alloc,
            Limit:   uint64(memoryLimit) * 1024 * 1024,
        }
    }
    
    return nil
}
```

## 超时控制

### 超时检查

```go
func CheckTimeout(stage string) error {
    if atomic.LoadInt32(&timedOut) == 1 {
        return nil  // 已超时
    }
    
    elapsed := time.Since(startTime)
    
    // 80% 警告
    if elapsed > time.Duration(timeoutLimit*0.8)*time.Second {
        log.Printf("[Warning] Timeout at %v (limit: %v)", 
            elapsed, timeoutLimit)
    }
    
    // 超出限制
    if elapsed > time.Duration(timeoutLimit)*time.Second {
        atomic.StoreInt32(&timedOut, 1)
        return &TimeoutError{
            Stage:   stage,
            Elapsed: elapsed,
            Limit:   time.Duration(timeoutLimit) * time.Second,
        }
    }
    
    return nil
}
```

### 原子操作

使用原子操作确保线程安全：

```go
var timedOut int32  // 原子标志

func IsTimedOut() bool {
    return atomic.LoadInt32(&timedOut) == 1
}

func SetTimedOut() {
    atomic.StoreInt32(&timedOut, 1)
}
```

## 阶段统计

### 统计示例

```go
// 编译输出
=== Timing Breakdown ===
Stage 1 (Lex + Parse):         6.0025ms
Stage 2 (Semantic):            0.9999ms
Stage 3 (Codegen+Compile):    2.8505352s
---------------------------------
Total End-to-End:              2.9200441s

=== Memory Usage ===
Peak Memory: 45.2 MB
GC Cycles: 3
```

### 统计收集

```go
type StageStats struct {
    Stages []*StageStat
    Total  time.Duration
    Peak   uint64
}

func GetStageStats() *StageStats {
    // 收集所有阶段统计
}
```

## 调试输出

### 内存热点

```go
func DumpMemoryHotspots() {
    // 输出内存使用最多的分配点
    profile := "heap_profile.pprof"
    f, _ := os.Create(profile)
    pprof.WriteHeapProfile(f)
    f.Close()
    fmt.Printf("Memory profile written to %s\n", profile)
}
```

### Goroutine 堆栈

```go
func DumpGoroutineStacks() {
    // 输出所有 goroutine 的堆栈
    buf := make([]byte, 1<<20)
    n := runtime.Stack(buf, true)
    fmt.Printf("=== Goroutine Stacks ===\n%s\n", buf[:n])
}
```

## 使用示例

```go
func main() {
    // 初始化
    timeout.Init()
    timeout.SetLimits(4096, 120)
    
    // Stage 1: Lexing + Parsing
    timeout.StartStage("Lex + Parse")
    program := parse(source)
    stat1 := timeout.EndStage("Lex + Parse")
    
    // Stage 2: Semantic Analysis
    timeout.StartStage("Semantic")
    if err := timeout.CheckTimeout("Semantic"); err != nil {
        log.Fatal(err)
    }
    analyzer.Analyze(program)
    stat2 := timeout.EndStage("Semantic")
    
    // Stage 3: Code Generation
    timeout.StartStage("Codegen")
    if err := timeout.CheckMemory("Codegen"); err != nil {
        log.Fatal(err)
    }
    cCode := codegen.Generate(program)
    stat3 := timeout.EndStage("Codegen")
    
    // 输出统计
    fmt.Printf("Total: %v\n", stat1.Elapsed + stat2.Elapsed + stat3.Elapsed)
}
```

## 最佳实践

1. **设置合理限制**：根据项目大小设置适当的内存和时间限制
2. **监控阶段耗时**：识别编译瓶颈
3. **使用警告阈值**：在 80% 时发出警告，提前发现问题
4. **记录统计信息**：用于性能分析和优化
5. **测试边界情况**：确保大文件编译不会超时
