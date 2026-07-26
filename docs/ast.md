# 抽象语法树 (AST)

AST 定义了编译器内部的程序表示。实现位于 `internal/ast/ast.go`。

## 设计

- **接口驱动**：`Node` 接口统一所有节点，`Statement` 和 `Expression` 继承 `Node`
- **位置追踪**：每个节点携带 `Position`（行、列、文件），支持精确错误报告
- **遍历支持**：`Traverse` 方法提供统一的树遍历机制

## 节点层次

```
Node (接口)
├── Statement (接口)
│   ├── Program                    // 程序根节点
│   ├── FunctionStatement          // 函数声明
│   ├── ClassStatement             // 类定义
│   ├── InterfaceStatement         // 接口定义
│   ├── StructStatement            // 结构体定义
│   ├── TypeAliasStatement         // 类型别名
│   ├── VariableDeclaration        // 变量声明
│   ├── IfStatement                // if 语句
│   ├── WhileStatement             // while 循环
│   ├── ForStatement               // (legacy) C 风格 for，已不再由解析器生成
│   ├── ForInStatement             // range-based for: for x in range(...) / arr { body }
│   ├── SwitchStatement            // switch 语句
│   ├── CaseStatement              // case 分支
│   ├── ReturnStatement            // return 语句
│   ├── BreakStatement             // break 语句
│   ├── ContinueStatement          // continue 语句
│   ├── ImportStatement            // import 语句
│   ├── ExportStatement            // export 语句
│   ├── PackageStatement           // package 声明
│   ├── NonLocalStatement          // nonlocal 语句
│   ├── ExpressionStatement        // 表达式语句
│   ├── BlockStatement             // 块语句
│   ├── VOStatement                // VO 语句
│   ├── SpendStatement             // spend 语句
│   ├── CallClause                 // call 子句
│   ├── CallStatement              // call 语句
│   ├── TaskStatement              // task 语句
│   ├── PrefixStatement            // prefix 语句
│   ├── TreeStatement              // tree 语句
│   ├── ObjectStatement            // object 语句
│   ├── YieldStatement             // yield 语句
│   ├── ReleaseStatement           // release 语句
│   ├── ExtractStatement           // extract 语句
│   ├── MethodStatement            // 方法声明
│   ├── ConstructorStatement       // 构造函数声明
│   ├── FieldDeclaration           // 字段声明
│   ├── ImplementsClause           // implements 子句
│   └── GenericInstance            // 泛型实例
│
└── Expression (接口)
    ├── Identifier                 // 标识符
    ├── IntegerLiteral             // 整数字面量
    ├── FloatLiteral               // 浮点字面量
    ├── StringLiteral              // 字符串字面量
    ├── CharLiteral                // 字符字面量
    ├── BooleanLiteral             // 布尔字面量
    ├── BinaryExpression           // 二元表达式
    ├── UnaryExpression            // 一元表达式
    ├── CallExpression             // 函数调用
    ├── IndexExpression            // 索引访问
    ├── MemberAccessExpression     // 成员访问
    ├── MemberExpression           // 成员表达式
    ├── TypeCastExpression         // 类型转换
    ├── PrefixCallExpression       // 前缀调用
    ├── LiteralExpression          // 字面量表达式
    └── ParenExpression            // 括号表达式
```

## 关键节点详解

### Program

```go
type Program struct {
    Statements []Statement  // 顶层语句列表
    Pos        Position
    Source     string       // 完整源码（用于错误上下文）
}
```

提供查找方法：
- `FindFunction(name)` - 查找函数
- `FindPrefix(name)` - 查找前缀
- `FindObject(name)` - 查找对象
- `FindClass(name)` - 查找类
- `FindInterface(name)` - 查找接口
- `FindStruct(name)` - 查找结构体

### FunctionStatement

```go
type FunctionStatement struct {
    Name          string
    TypeParams    []*TypeParameter  // 泛型类型参数
    Params        []string          // 参数名列表
    ParamTypes    []string          // 参数类型列表
    Body          []Statement
    ReturnType    string
    Generic       bool              // 是否是泛型函数
    NoKMM         bool              // 是否禁用 KMM 内存管理
    Inline        bool              // 是否内联
    Annotation    TreeAnnotationType // 函数注解
    SOREnabled    bool              // SOR 启用
    IsPublic      bool              // pub 修饰符
    PrefixName    string            // prefix 名称
    TaskParams    []*TaskParam      // 任务参数
    AsyncParams   []*AsyncParam     // 异步参数
    Pos           Position
}
```

注解类型（`TreeAnnotationType`）：
- `TreeAnnotationNone` - 无注解
- `TreeAnnotationPrefix` - `#[prefix]`
- `TreeAnnotationTree` - `#[tree]`
- `TreeAnnotationPrefixTree` - `#[prefix,tree]`
- `TreeAnnotationRoot` - `#[root]`
- `TreeAnnotationRootTree` - `#[root,tree]`

### ClassStatement

```go
type ClassStatement struct {
    Name        string
    TypeParams  []*TypeParameter       // 泛型参数
    Fields      []*FieldDeclaration    // 字段
    Methods     []*MethodStatement     // 方法
    Constructors []*ConstructorStatement // 构造函数
    Implements  []string               // 实现的接口
    Generic     bool
    Pos         Position
}
```

### VariableDeclaration

```go
type VariableDeclaration struct {
    Type     string      // 类型名
    Name     string      // 变量名
    Value    Expression  // 初始值（可选）
    Nullable bool        // 是否可空
    IsAuto   bool        // 是否使用 auto 推导
    IsPublic bool        // pub 修饰符
    Pos      Position
}
```

### SpendStatement

```go
type SpendStatement struct {
    Target  Expression      // 消费目标
    Calls   []*CallClause   // call 子句列表
    Pos     Position
}

type CallClause struct {
    Index Expression  // 元素索引（1-based）
    Body  []Statement // 处理逻辑
    Pos   Position
}
```

### PrefixCallExpression

```go
type PrefixCallExpression struct {
    Name       string                // 前缀名
    Params     map[string]Expression // 参数映射
    Body       []Statement           // 块体
    Annotation TreeAnnotationType
    Pos        Position
}
```

## 泛型支持

### TypeParameter

```go
type TypeParameter struct {
    Name      string  // 类型参数名
    Constraint string // 类型约束
    Pos       Position
}
```

### TypeArgument

```go
type TypeArgument struct {
    Type string  // 类型实参
    Pos  Position
}
```

### GenericInstance

```go
type GenericInstance struct {
    OriginalName    string       // 原始泛型名
    TypeArguments   []TypeArgument // 类型实参
    InstantiatedName string     // 实例化后的名称
    Pos             Position
}
```

## 遍历机制

```go
// 遍历整个程序
program.Traverse(func(node Node) {
    switch n := node.(type) {
    case *FunctionStatement:
        // 处理函数
    case *ClassStatement:
        // 处理类
    // ...
    }
})
```

`traverseNode` 函数递归遍历所有子节点，覆盖所有 AST 节点类型。
