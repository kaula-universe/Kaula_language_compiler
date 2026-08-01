# 核心运行时特性

编译器 Go 层的核心运行时特性实现。位于 `internal/core/` 目录。

## 目录结构

```
core/
├── spendcall.go    # Spend/Call 机制
├── prefix.go       # 前缀系统
├── tree.go         # 树系统
└── task.go         # 任务调度
```

## Spend/Call 机制 (spendcall.go)

顺序消费模式，组件被逐个消费。

### 核心类型

```go
type Spendable struct {
    Components  []interface{}  // 组件列表
    CallCounter int            // 调用计数
    Consumed    bool           // 是否已完全消费
}
```

### API

```go
// 创建可消费对象
spendable := core.NewSpendable()

// 添加组件
spendable.Add(component1)
spendable.Add(component2)

// 消费组件
component := spendable.Call()  // 返回下一个组件

// 检查状态
if spendable.IsConsumed() {
    // 所有组件已消费
}
```

### 自动释放

当所有组件消费完毕时自动释放资源：

```go
func (s *Spendable) Call() interface{} {
    if s.CallCounter >= len(s.Components) {
        s.Consumed = true
        s.release()  // 自动释放
        return nil
    }
    
    component := s.Components[s.CallCounter]
    s.CallCounter++
    return component
}
```

## 前缀系统 (prefix.go)

管理 DSL-like 代码块的前缀系统。

### 核心类型

```go
type PrefixContext struct {
    Name      string                    // 前缀名
    Variables map[string]*PrefixVariable // 变量映射
    Calls     []*PrefixCall            // 调用列表
}

type PrefixVariable struct {
    Name  string      // 变量名（$x）
    Value interface{} // 变量值
    Type  PrefixVarType // 变量类型
}

type PrefixCall struct {
    Name   string                 // 前缀名
    Params map[string]interface{} // 参数
    Body   func()                // 块体
}
```

### API

```go
// 创建前缀管理器
pm := core.NewPrefixManager()

// 进入前缀上下文
pm.EnterContext("layout")

// 设置变量
pm.SetVariable("$x", 10)
pm.SetVariable("$y", "hello")

// 获取变量
value := pm.GetVariable("$x")

// 调用前缀
pm.Call("layout", params, body)

// 退出上下文
pm.ExitContext()
```

### 变量遮蔽

检测局部变量遮蔽前缀变量：

```go
func (pm *PrefixManager) CheckShadowing(localVar string) bool {
    // 检查局部变量是否遮蔽前缀变量
    for _, ctx := range pm.contextStack {
        if _, exists := ctx.Variables["$"+localVar]; exists {
            return true  // 存在遮蔽
        }
    }
    return false
}
```

### 导出/导入

```go
// 导出前缀表到 JSON
pm.ExportToFile("prefix_table.json")

// 从 JSON 导入
pm.ImportFromFile("prefix_table.json")
```

## 树系统 (tree.go)

管理带注解的树结构。

### 核心类型

```go
type Tree struct {
    Root       *TreeNode           // 根节点
    Annotation TreeAnnotation      // 树注解
    Constraints []*TreeConstraint  // 约束
}

type TreeNode struct {
    ID         string
    Type       TreeNodeType
    Annotation TreeAnnotation
    Children   []*TreeNode
    Constraint *TreeConstraint
}

type TreeAnnotation int

const (
    TreeAnnotationNone TreeAnnotation = iota
    TreeAnnotationPrefix
    TreeAnnotationTree
    TreeAnnotationPrefixTree
    TreeAnnotationRoot
    TreeAnnotationRootTree
)
```

### API

```go
// 创建树管理器
tm := core.NewTreeManager()

// 创建树
tree := tm.CreateTree("layout", TreeAnnotationPrefixTree)

// 添加节点
tree.Root.AddChild(&TreeNode{
    ID:   "header",
    Type: TreeNodeTypeStatement,
})

// 验证树
errors := tm.ValidateTree(tree)

// 查找孤儿树
orphans := tm.FindOrphanTrees()
```

### 树验证

```go
func (tm *TreeManager) ValidateTree(tree *Tree) []error {
    var errors []error
    
    // 检查是否有根树
    if tree.Annotation == TreeAnnotationRoot || tree.Annotation == TreeAnnotationRootTree {
        if tm.rootTree != nil {
            errors = append(errors, errors.New("multiple root trees"))
        }
        tm.rootTree = tree
    }
    
    // 检查孤儿树
    if tree.Annotation == TreeAnnotationTree {
        errors = append(errors, errors.New("orphan tree without root"))
    }
    
    return errors
}
```

## 任务调度 (task.go)

三级优先级队列系统。

### 核心类型

```go
type Task struct {
    ID       int
    Priority int        // 0=HIGH, 1=MEDIUM, 2=LOW
    Func     func()
    Arg      interface{}
}

type PriorityQueue struct {
    high   []*Task
    medium []*Task
    low    []*Task
}
```

### API

```go
// 创建优先级队列
pq := core.NewPriorityQueue(100)

// 添加任务
pq.Add(&Task{
    Priority: 0,  // HIGH
    Func:     processHighPriority,
})

// 批量添加
pq.AddBatch(tasks)

// 执行任务
task := pq.Execute()  // 按优先级执行

// 批量执行
pq.ExecuteBatch()
```

### 优先级执行

```go
func (pq *PriorityQueue) Execute() *Task {
    // 1. 执行高优先级任务
    if len(pq.high) > 0 {
        task := pq.high[0]
        pq.high = pq.high[1:]
        task.Func()
        return task
    }
    
    // 2. 执行中优先级任务
    if len(pq.medium) > 0 {
        task := pq.medium[0]
        pq.medium = pq.medium[1:]
        task.Func()
        return task
    }
    
    // 3. 执行低优先级任务
    if len(pq.low) > 0 {
        task := pq.low[0]
        pq.low = pq.low[1:]
        task.Func()
        return task
    }
    
    return nil
}
```

### 环形缓冲区

简单队列使用环形缓冲区实现：

```go
type SimpleQueue struct {
    items  []interface{}
    head   int
    tail   int
    count  int
    capacity int
}

func (q *SimpleQueue) Add(item interface{}) bool {
    if q.count >= q.capacity {
        return false  // 队列满
    }
    
    q.items[q.tail] = item
    q.tail = (q.tail + 1) % q.capacity
    q.count++
    return true
}
```

## 线程安全

所有核心特性都支持线程安全：

```go
// 使用 sync.RWMutex
type Spendable struct {
    mu         sync.RWMutex
    Components []interface{}
    // ...
}

func (sp *Spendable) Add(component interface{}) {
    sp.mu.Lock()
    defer sp.mu.Unlock()
    // ...
}

func (sp *Spendable) Call() interface{} {
    sp.mu.RLock()
    defer sp.mu.RUnlock()
    // ...
}
```

## 使用示例

```kaula
// Spend/Call 系统
spend(component1, component2) {
    call(1) {
        return processFirst()
    }
    call(2) {
        return processSecond()
    }
}

// 前缀系统
prefix layout {
    $x = 10
    $y = "hello"
}

@layout {
    println($x)
    println($y)
}

// 任务调度
task(0, highPriorityFunc, arg)
task(1, mediumPriorityFunc, arg)
task(2, lowPriorityFunc, arg)
```
