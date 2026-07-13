# 语义分析

语义分析器对 AST 进行类型检查、符号管理和作用域验证。有两个实现：

- `sema/sema.go` - 主语义分析器（完整功能）
- `semantic/semantic.go` - 简化版（基础功能）

## 主语义分析器 (sema)

### 两遍分析

**第一遍：符号收集**
- 遍历所有顶层声明（函数、变量、类、结构体、接口、类型别名）
- 将符号注册到符号表
- 处理导入模块的标准库函数
- 解析第三方库（pkglib）函数签名

**第二遍：函数体分析**
- 分析每个函数的函数体
- 类型检查表达式和语句
- 验证变量使用（未定义、类型不匹配）
- 检查控制流（return、break、continue）

### 核心功能

#### 类型检查

```go
// 支持的类型
基本类型: int, float, double, bool, char, string, void
指针类型: int*, string*, Type*
泛型类型: Box<int>, Map<string, int>
自定义类型: 用户定义的类、结构体、接口
```

#### 类型推导

```go
// auto 声明的类型推导
auto x = 42        // 推导为 int
auto y = 3.14      // 推导为 float
auto z = "hello"   // 推导为 string
```

#### 类型提升

整数类型按精度提升：
```
u8 < i8 < u16 < i16 < u32 < i32 < u64 < i64
```

#### 导入解析

```go
// 标准库导入
import std.io    // 解析 stdlib.json 中的 std.io 模块
import std.math  // 解析 std.math 模块

// 本地文件导入
import utils     // 解析本地 utils.kl 文件
```

#### 泛型约束

```go
// 泛型函数的类型参数
fn max<T>(T a, T b) T {
    if a > b { return a }
    return b
}

// 泛型类
class Box<T> {
    T value
    constructor(T v) { this.value = v }
}
```

#### SOR 原语验证

```go
// yield - 所有权转移
yield(source)   // 将 source 的所有权转移

// release - 释放所有权
release(source) // 释放 source 的所有权

// extract - 提取所有权
extract(source, index) // 从 source 提取指定元素
```

#### 前缀变量检查

```go
// 检测前缀变量遮蔽
prefix myPrefix {
    $x = 10  // 前缀变量
}

// 检查局部变量是否遮蔽前缀变量
fn example() {
    int x = 20  // 警告：遮蔽前缀变量 $x
}
```

#### 树分析

```go
// 验证树注解
#[prefix,tree] fn layout() { ... }  // 有效
#[root,tree] fn mainTree() { ... }  // 有效
#[tree] fn orphan() { ... }         // 孤儿树，警告
```

### 核心类型

```go
// SemanticAnalyzer 语义分析器
type SemanticAnalyzer struct {
    symbolTable        *symbol.SymbolTable
    errorCollector     *errors.ErrorCollector
    stdlibConfig       *stdlib.StdlibConfig
    prefixSymbolTables map[string]*symbol.SymbolTable  // 前缀符号表
    sorEnabled         bool                            // SOR 模式
    currentPrefix      string                          // 当前前缀
    currentFunction    string                          // 当前函数
}

// 两遍分析
func (sa *SemanticAnalyzer) Analyze(program *ast.Program) {
    // 第一遍：符号收集
    sa.collectSymbols(program)
    // 第二遍：函数体分析
    sa.analyzeFunctionBodies(program)
}
```

### 错误类型

```go
// 语义错误
- 未定义的变量/函数
- 类型不匹配
- 参数数量错误
- 返回类型不匹配
- 导入模块未找到
- 泛型约束违反
- 前缀变量遮蔽
- SOR 所有权违规
```

## 简化版语义分析器 (semantic)

用于简单场景的轻量级分析器：

```go
type Analyzer struct {
    scope          *Scope          // 当前作用域
    errorCollector *errors.ErrorCollector
}

type Scope struct {
    name     string
    parent   *Scope
    symbols  map[string]*Symbol
}

type Symbol struct {
    name string
    typ  string
}
```

支持的类型：`int`, `float`, `bool`, `string`

## API

```go
// 主语义分析器
analyzer := sema.NewSemanticAnalyzerWithConfig("stdlib.json", errorCollector)
analyzer.SetSOREnabled(true)
analyzer.Analyze(program)

if analyzer.HasErrors() {
    // 处理错误
}

// 获取标准库配置
stdlibConfig := analyzer.GetStdlibConfig()

// 简化版
simpleAnalyzer := semantic.NewAnalyzer(errorCollector)
simpleAnalyzer.Analyze(program)
```

## 标准库集成

语义分析器通过 `stdlib.json` 加载标准库函数签名：

```json
{
  "std.io": {
    "header": "std/io/io.h",
    "println": {"args": ["const char*"], "varargs": true},
    "print_int": {"args": ["i64"]}
  }
}
```

分析器验证：
- 函数调用的参数类型
- 返回类型
- 模块依赖关系
- 第三方库函数签名
