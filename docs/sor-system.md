# SOR 子所有权释放系统

SOR（Sub-Ownership Release）是 Kaula 的编译期所有权验证系统，类似 Rust 但更轻量。实现位于 `internal/sor/` 目录。

## 设计理念

SOR 通过三个原语管理资源所有权：

1. **yield** - 所有权转移（move）
2. **release** - 释放所有权（share/read-only）
3. **extract** - 提取所有权（extract sub-element）

## 核心概念

### 所有权状态

```go
type OwnershipState int

const (
    Owned          OwnershipState = iota // 拥有所有权
    Released                             // 已释放（只读）
    Extracted                            // 已提取
    Moved                                // 已移动
    Hollow                               // 空洞（部分提取后）
    UnionReleased                        // 联合释放
)
```

### SOR 对象

```go
type SORObject struct {
    ID             string            // 唯一标识
    State          OwnershipState    // 所有权状态
    Type           string            // 类型名
    Children       map[string]*SORObject // 子对象
    ReleaseHolders []string          // 释放持有者
    Scope          int               // 作用域深度
    Line           int               // 声明行号
}
```

## 三大原语

### yield（所有权转移）

```kaula
// 转移所有权
yield(source)
// source 的所有权转移到新位置
// 原位置变为 Moved 状态
```

语义：
- 将对象的所有权从一个位置转移到另一个位置
- 原位置不再拥有该对象
- 新位置获得完整所有权

### release（释放所有权）

```kaula
// 释放所有权（变为只读）
release(source)
// source 变为 Released 状态
// 可以读取，不能修改
```

语义：
- 将对象的所有权释放
- 对象变为只读状态
- 可以被多个持有者共享
- 检测 DAG 循环（防止循环引用）

### extract（提取所有权）

```kaula
// 提取子元素
extract(source, index)
// 从 source 提取指定元素
// source 变为 Hollow 状态
```

语义：
- 从对象中提取子元素
- 原对象变为 Hollow（部分空洞）
- 提取的元素获得独立所有权

## 目录结构

```
sor/
├── types.go          # 核心类型定义
├── analyzer.go       # 符号执行引擎
├── ownership.go      # 所有权跟踪器
├── dag.go            # DAG 循环检测
├── cfg.go            # 控制流图构建
├── escape.go         # 逃逸分析
├── memory.go         # 内存分配决策
├── liveness.go       # 活跃性分析
├── interproc.go      # 过程间分析
├── adapter.go        # AST 到 SOR 适配器
├── points_to.go      # 指向分析
├── scopetree.go      # 作用域树分析
├── sizing.go         # 类型大小估算
├── loopdetect.go     # 循环检测
├── union.go          # 联合域分析
└── *_test.go         # 测试文件
```

## 分析流程

### 1. AST 适配 (adapter.go)

从 AST 提取 SOR 相关语句：

```go
// adapter.go
func ExtractSORStatements(program *ast.Program) []Stmt {
    // 提取 yield/release/extract 语句
    // 提取变量声明
    // 提取控制流
    // 提取函数调用
}
```

### 2. 控制流图 (cfg.go)

构建控制流图：

```go
// cfg.go
type CFG struct {
    Blocks    []*BasicBlock  // 基本块
    Entry     *BasicBlock    // 入口块
    Exit      *BasicBlock    // 出口块
    LoopEdges []*Edge        // 循环边
}

type BasicBlock struct {
    ID    int
    Stmts []Stmt
    Succs []*BasicBlock
    Preds []*BasicBlock
}
```

### 3. 符号执行 (analyzer.go)

模拟执行，跟踪所有权变化：

```go
// analyzer.go
type Analyzer struct {
    ownership *OwnershipTracker
    cfg       *CFG
    errors    []*SORError
}

func (a *Analyzer) Analyze(stmts []Stmt) []*SORError {
    // 符号执行每个语句
    // 跟踪所有权状态变化
    // 检测违规
}
```

### 4. 所有权跟踪 (ownership.go)

维护所有对象的状态：

```go
// ownership.go
type OwnershipTracker struct {
    objects    map[string]*SORObject
    dag        *DAG
    scopeStack []int
    errors     []*SORError
}

// 三大操作
func (ot *OwnershipTracker) Yield(sourceID string) error
func (ot *OwnershipTracker) Release(sourceID string) error
func (ot *OwnershipTracker) Extract(sourceID, index string) error
```

### 5. DAG 循环检测 (dag.go)

检测 release 关系中的循环：

```go
// dag.go
type DAG struct {
    nodes map[string]*DAGNode
}

type DAGNode struct {
    ID       string
    Children []*DAGNode
    Color    int  // DFS 颜色标记
}

func (d *DAG) DetectCycle() ([]string, bool) {
    // 三色标记 DFS
    // 返回循环路径
}
```

### 6. 逃逸分析 (escape.go)

分析对象的逃逸级别：

```go
// escape.go
type EscapeLevel int

const (
    EscapeNone     EscapeLevel = iota // 无逃逸
    EscapeArg                         // 作为参数逃逸
    EscapeReturn                      // 作为返回值逃逸
    EscapeCrossScope                  // 跨作用域逃逸
    EscapeGlobal                      // 全局逃逸
    EscapeHeap                        // 堆逃逸
)

func (ea *EscapeAnalyzer) Analyze(obj *SORObject) EscapeLevel {
    // 分析对象逃逸级别
    // 映射到分配策略
}
```

### 7. 内存分配决策 (memory.go)

根据所有权状态决定分配策略：

```go
// memory.go
type AllocKind int

const (
    AllocStack      AllocKind = iota // 栈分配
    AllocBumpPool                     // Bump Pool 分配
    AllocArenaTiny                    // 小 Arena
    AllocArenaSmall                   // 中 Arena
    AllocArenaMedium                  // 大 Arena
)

type DropAction int

const (
    DropNone       DropAction = iota // 无操作
    DropScopeEnd                      // 作用域结束时释放
    DropHollow                        // 变为空洞
)

func (ma *MemoryAnalyzer) Analyze(obj *SORObject) (AllocKind, DropAction) {
    // 根据所有权状态和类型决定分配
}
```

### 8. 活跃性分析 (liveness.go)

确定变量的最后使用点：

```go
// liveness.go
type LivenessAnalyzer struct {
    lastUses map[string]int  // 变量 → 最后使用行号
}

func (la *LivenessAnalyzer) Analyze(stmts []Stmt) map[string]int {
    // 计算每个变量的最后使用点
    // 用于精确的释放点计算
}
```

### 9. 类型大小估算 (sizing.go)

估算类型的编译期大小：

```go
// sizing.go
func EstimateSize(typeName string) int {
    // 基本类型大小
    // 指针大小
    // 结构体大小（含对齐）
    // 数组大小
}
```

## 错误类型

```go
type SORErrorKind int

const (
    ErrUseAfterMove    SORErrorKind = iota // 移动后使用
    ErrUseAfterRelease                      // 释放后使用
    ErrDoubleRelease                        // 重复释放
    ErrCycleDetected                        // 检测到循环
    ErrNullDereference                      // 空指针解引用
    ErrInvalidExtract                       // 无效提取
    ErrOwnershipLeak                        // 所有权泄漏
    ErrScopeViolation                       // 作用域违规
    // ...
)
```

## SOR 错误示例

```
SOR Error: Use after move
  Object 'data' was moved at line 10
  but used again at line 15
  
  10: yield(data)
      ^^^^^^^^^^ moved here
  ...
  15: println(data)
              ^^^^ used here
```

## 内存优化

SOR 分析结果用于优化内存分配：

1. **栈分配**：无逃逸的对象分配在栈上
2. **Bump Pool**：临时对象使用 Bump Pool（KMM V4 per-thread heap）
3. **作用域释放**：作用域结束时批量释放（kmm_v4_scope_pop）

> **注意**：Arena 分级（AllocArenaTiny/Small/Medium）已在 KMM V4 中收敛为 BumpPool，代码生成统一使用 `kmm_v4_alloc_auto`。

## SOR 与 KMM V4 集成

### 内存分配决策收敛

KMM V4 统一了分配路径，SOR 的分配决策在代码生成阶段全部映射到 BumpPool：

```go
// memory.go - SOR 分配决策
type AllocKind int

const (
    AllocStack      AllocKind = iota // 栈分配
    AllocBumpPool                     // Bump Pool 分配（KMM V4 唯一运行时路径）
    AllocArenaTiny                    // 已废弃，收敛到 BumpPool
    AllocArenaSmall                   // 已废弃，收敛到 BumpPool
    AllocArenaMedium                  // 已废弃，收敛到 BumpPool
)
```

代码生成器将所有 `AllocBumpPool`/`AllocArenaTiny`/`AllocArenaSmall`/`AllocArenaMedium` 统一生成为 `kmm_v4_alloc_auto` 调用：

```go
// sor_codegen.go - 分配代码生成
func (scg *SORCodegen) generateAlloc(kind AllocKind, size int) string {
    switch kind {
    case AllocStack:
        return generateStackAlloc(size)  // 栈上分配
    default:
        // 所有堆分配统一走 KMM V4 BumpPool
        return fmt.Sprintf("kmm_v4_alloc_auto(%d)", size)
    }
}
```

### 作用域释放点计算

SOR 的活跃性分析精确计算每个变量的最后使用点，指导代码生成器在正确位置插入 `kmm_v4_scope_pop`：

```kaula
#[sor]
fn process() {
    auto buf = std.memory.std_malloc(1024)
    
    yield buf -> owner         // 所有权转移
    
    extract owner[0] -> first  // 提取子结构
    
    println(first)             // 最后使用点
    
    release owner -> [a, b]    // 释放所有权
    // ↑ SOR 分析在此插入 kmm_v4_scope_pop()
}
```

### 池容量自动计算

SOR 分析根据对象大小总和自动计算 KMM 池容量：

```go
// SOR 分析结果传递给代码生成
sorResult := sor.AnalyzeFull(program)

// 根据 SOR 分析的对象大小总和计算池容量
poolSize := sorResult.TotalObjectSize * 2  // 2x 余量
codegen.EmitDefine("KMM_V4_POOL_SIZE", poolSize)
```

### KMM V4 per-thread heap 与 SOR 的协作

SOR 的作用域模型与 KMM V4 的 per-thread heap 完美契合：

| SOR 概念 | KMM V4 实现 | 说明 |
|----------|-------------|------|
| 作用域进入 | `kmm_v4_scope_push()` | 保存 thread heap offset |
| 作用域退出 | `kmm_v4_scope_pop()` | 恢复 offset，批量回收 |
| 对象分配 | `kmm_v4_alloc_auto(size)` | Bump pointer 推进 |
| 对象释放 | no-op | 作用域退出时统一回收 |
| 所有权转移 | 编译期检查 | 运行时零开销 |
| 子结构提取 | 编译期检查 | 运行时零开销 |

**关键设计**：scope_push/pop 只操作 per-thread heap offset，不影响全局 offset。多线程环境下，一个线程的 scope 回退不会影响其他线程的分配。

## API

```go
// 创建 SOR 分析器
analyzer := sor.NewAnalyzer()

// 分析 AST
errors := analyzer.Analyze(program)

// 获取内存分析结果
memResult := analyzer.GetMemoryResult()

// 获取池容量
poolSize := memResult.PoolCapacity
```

## 与代码生成集成

SOR 分析结果传递给代码生成器：

```go
// main.go
if config.SOR {
    sorResult := sor.AnalyzeFull(program)
    codegen.SetSORResult(sorResult)
    
    // 生成 KMM 池大小定义
    // #define KMM_V4_POOL_SIZE <poolSize>
}
```

## 性能影响

- **编译时间**：增加 ~10-20%
- **运行时性能**：减少内存分配，提高缓存命中率
- **安全性**：编译期检测内存错误
