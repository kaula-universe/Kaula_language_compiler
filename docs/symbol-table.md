# 符号表 (Symbol Table)

符号表管理编译器中的标识符信息，支持嵌套作用域和泛型。实现位于 `internal/symbol/symbol.go`。

## 设计

- **层次化作用域**：符号表形成链表，支持词法作用域
- **泛型支持**：支持泛型类型参数和实例化
- **类型缓存**：泛型实例化结果缓存，避免重复计算
- **线程安全**：通过锁机制支持并发访问

## 核心类型

### Symbol

```go
type Symbol struct {
    Name         string              // 符号名
    Type         string              // 类型名
    Nullable     bool                // 是否可空
    Scope        string              // 所在作用域
    Line         int                 // 声明行号
    Column       int                 // 声明列号
    IsGeneric    bool                // 是否是泛型
    GenericInst  *GenericInstanceInfo // 泛型实例信息
}
```

### GenericInstanceInfo

```go
type GenericInstanceInfo struct {
    OriginalName  string       // 原始泛型名
    TypeArguments []string     // 类型实参
    Constraints   []string     // 类型约束
}
```

### SymbolTable

```go
type SymbolTable struct {
    parent     *SymbolTable              // 父作用域
    symbols    map[string]*Symbol        // 符号映射
    scopeName  string                    // 作用域名
    scopeDepth int                       // 作用域深度
    typeCache  map[string]*Symbol        // 泛型类型缓存
}
```

## 作用域层次

```
全局作用域
├── 函数作用域 (add)
│   ├── 参数作用域
│   └── 块作用域
├── 函数作用域 (main)
│   ├── 块作用域
│   └── 嵌套块作用域
└── 类作用域 (Point)
    ├── 字段作用域
    └── 方法作用域
```

## API

### 创建符号表

```go
// 创建全局符号表
globalTable := NewSymbolTable(nil, "global")

// 创建嵌套作用域
functionScope := NewSymbolTable(globalTable, "function:add")
blockScope := NewSymbolTable(functionScope, "block:if")
```

### 添加符号

```go
// 添加简单符号
table.AddSymbol("x", "int", false)

// 添加可空符号
table.AddSymbol("ptr", "int*", true)

// 添加泛型符号
table.AddGenericSymbol("Box", "T", []string{"any"})

// 添加带位置的符号
table.AddSymbolWithPosition("y", "float", false, 10, 5)
```

### 查找符号

```go
// 查找符号（沿作用域链向上查找）
symbol := table.GetSymbol("x")

// 查找本地符号（仅当前作用域）
symbol := table.GetLocalSymbol("x")

// 检查符号是否存在
exists := table.HasSymbol("x")
```

### 泛型操作

```go
// 实例化泛型类型
table.InstantiateGeneric("Box", []string{"int"}, "Box<int>")

// 检查是否是泛型类型
isGeneric := table.IsGenericType("Box")

// 获取类型参数
params := table.GetTypeParams("Box")
```

### 作用域管理

```go
// 获取当前作用域深度
depth := table.GetScopeDepth()

// 获取当前作用域名
name := table.GetScopeName()

// 获取所有符号
allSymbols := table.GetAllSymbols()

// 获取当前作用域的符号
localSymbols := table.GetSymbolsInScope()
```

## 泛型支持

### 泛型声明

```kaula
// 泛型函数
fn max<T>(T a, T b) T {
    if a > b { return a }
    return b
}

// 泛型类
class Box<T> {
    T value
}
```

### 泛型实例化

```go
// 实例化泛型函数
table.InstantiateGeneric("max", []string{"int"}, "max<int>")

// 实例化泛型类
table.InstantiateGeneric("Box", []string{"string"}, "Box<string>")
```

### 泛型缓存

```go
// 类型缓存避免重复实例化
typeCache: map[string]*Symbol{
    "Box<int>":    {Name: "Box<int>", Type: "Box<int>"},
    "Box<string>": {Name: "Box<string>", Type: "Box<string>"},
}
```

## 作用域链查找

```go
func (st *SymbolTable) GetSymbol(name string) *Symbol {
    // 1. 查找当前作用域
    if sym, ok := st.symbols[name]; ok {
        return sym
    }
    
    // 2. 向上查找父作用域
    if st.parent != nil {
        return st.parent.GetSymbol(name)
    }
    
    // 3. 未找到
    return nil
}
```

## 使用示例

```go
// 创建符号表
global := NewSymbolTable(nil, "global")

// 添加全局变量
global.AddSymbol("MAX_SIZE", "int", false)

// 进入函数作用域
fnScope := NewSymbolTable(global, "function:main")

// 添加参数
fnScope.AddSymbol("argc", "int", false)
fnScope.AddSymbol("argv", "string*", true)

// 进入块作用域
blockScope := NewSymbolTable(fnScope, "block:if")

// 添加局部变量
blockScope.AddSymbol("temp", "int", false)

// 查找符号（沿链向上）
sym := blockScope.GetSymbol("argc")  // 找到函数参数
sym = blockScope.GetSymbol("MAX_SIZE")  // 找到全局变量
```

## 泛型示例

```go
// 创建符号表
table := NewSymbolTable(nil, "global")

// 添加泛型类型参数
table.AddGenericSymbol("T", "", []string{"any"})

// 实例化泛型
table.InstantiateGeneric("Box", []string{"int"}, "Box<int>")

// 查找实例化后的符号
sym := table.GetSymbol("Box<int>")
// sym.IsGeneric = true
// sym.GenericInst = {OriginalName: "Box", TypeArguments: ["int"]}
```

## 注意事项

1. **作用域嵌套**：确保正确管理作用域进入/退出
2. **符号遮蔽**：内层作用域可以遮蔽外层符号
3. **泛型缓存**：避免重复实例化相同泛型
4. **线程安全**：并发访问时使用适当的锁机制
