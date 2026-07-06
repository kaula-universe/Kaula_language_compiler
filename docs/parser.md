# 语法分析器 (Parser)

语法分析器将 Token 序列转换为 AST（抽象语法树）。实现位于 `internal/parser/parser.go`。

## 设计

- **迭代式递归下降**：使用显式栈替代递归，避免深层嵌套导致的栈溢出
- **两 Token 预读**：维护 `curTok` 和 `peekTok`，支持向前查看一个 Token
- **错误恢复**：遇到语法错误时跳过当前 Token 继续解析，收集所有错误
- **任务栈**：使用 `ParseTask` 栈管理解析任务状态

## 语法规则

### 变量声明

```
类型 变量名 [= 表达式]
Type name [= expr]

// 示例
int x = 10
float y
string name = "hello"
Box<int> box
int* ptr
*int ptr2
auto z = 42          // 类型推导
```

支持的指针语法：
- `Type name*` - 后缀指针
- `Type* name` - 中缀指针
- `*Type name` - 前缀指针

### 函数声明

```
#[注解] fn 函数名[<泛型参数>](参数列表) 返回类型 { 函数体 }

// 示例
fn main() { ... }
fn add(int a, int b) int { return a + b }
fn identity<T>(T x) T { return x }
#[prefix] fn layout() { ... }
#[no_kmm] fn unsafe_fn() { ... }
#[inline] fn fast_fn() { ... }
#[sor] fn owned_fn() { ... }
pub fn exported() { ... }
```

函数参数支持：
- 类型注解：`int x`, `string name`
- 指针类型：`int* ptr`, `*int ptr`
- 任务参数：`task(1)` - 高优先级
- 异步参数：`async(value)` - 异步值

### 类定义

```
class 类名[<泛型参数>] implements 接口列表 {
    字段声明
    方法定义
    构造函数
}

// 示例
class Point {
    float x
    float y
    
    Point(float x, float y) {
        this.x = x
        this.y = y
    }
    
    float distance() {
        return math_sqrt(this.x * this.x + this.y * this.y)
    }
}
```

### 结构体定义

```
struct 结构体名[<泛型参数>] {
    字段声明
}

// 示例
struct Vec3 {
    float x
    float y
    float z
}
```

### 接口定义

```
interface 接口名 {
    返回类型 方法名(参数列表)
}

// 示例
interface Drawable {
    void draw()
    float getArea()
}
```

### 类型别名

```
type 名称 = 底层类型
type 名称[<泛型参数>] = 底层类型
type 名称 func(参数类型...) 返回类型

// 示例
type i64 = long
type Callback func(int) void
type Pair<T> struct { T first; T second }
```

### 控制流

```
// if 语句（支持括号和无括号两种形式）
if (条件) { ... }
if 条件 { ... }
if 条件 { ... } else { ... }
if 条件 { ... } else if 条件 { ... }

// while 循环
while (条件) { ... }
while 条件 { ... }

// for 循环
for (初始化; 条件; 更新) { ... }

// switch 语句
switch (表达式) {
    case 值1: 语句...
    case 值2: 语句...
    default: 语句...
}

// break 和 continue
break
continue

// return
return 表达式
```

### 模块系统

```
// 导入标准库模块
import std.io
import std.math
import std.container

// 导入本地文件
import utils

// 导出声明
export fn myFunction()
export class MyClass
export object myObject
export var myVar

// 包声明
package mypackage

// 公开修饰符
pub fn publicFunction() { ... }
pub struct PublicStruct { ... }
```

### VO 系统

```
// VO 语句
vo create(100)
vo_data_load(vo, 1, data)
vo_code_load(vo, -1, fn)
vo_associate(vo, 1, -1)
result = vo_access(vo, 1)
```

### Spend/Call 系统

```
// spend/call 语句
spend(component1) {
    call(1) {
        return 1
    }
    call(2) {
        return 2
    }
}
```

### 前缀系统

```
// 前缀定义
prefix myPrefix {
    $x = 10
    $y = "hello"
}

// 前缀调用
@myPrefix {
    println($x)
}
```

### 树系统

```
// 树定义
tree(myRoot) {
    // 树节点
}

#[prefix,tree] fn layout() { ... }
#[root,tree] fn mainTree() { ... }
```

### 表达式

```
// 二元表达式
a + b, a - b, a * b, a / b, a % b
a == b, a != b, a < b, a > b, a <= b, a >= b
a && b, a || b
a & b, a | b, a ^ b
a << b, a >> b

// 一元表达式
-a, !a, &a

// 函数调用
funcName(arg1, arg2)
funcName<T>(arg1, arg2)  // 泛型调用

// 成员访问
obj.field

// 索引访问
array[index]

// 类型转换
(i64)(result)
(f64)(value)

// 前缀调用表达式
@prefixName(param1=value1) { body }
```

## 核心类型

```go
// Parser 语法分析器
type Parser struct {
    lexer          *lexer.Lexer
    curTok         lexer.Token       // 当前 Token
    peekTok        lexer.Token       // 预读 Token
    errorCollector *errors.ErrorCollector
    file           string
    taskStack      []ParseTask       // 解析任务栈
    skipMainCheck  bool              // 跳过 main 函数检查
}

// ParseTask 解析任务
type ParseTask struct {
    TaskType   ParseTaskType
    Precedence int
    Result     interface{}
}
```

## API

```go
// 创建语法分析器
parser := NewParser(lexerInstance)

// 解析整个程序
program := parser.parseProgram()

// 错误处理
if parser.errorCollector.HasErrors() {
    parser.errorCollector.ReportErrors()
}
```

## 错误处理

语法分析器使用两 Token 预读进行错误预测：

```go
// 检测常见错误
if p.curTok.Type == lexer.TOKEN_COLON {
    p.error("unexpected ':' - Kaula uses braces {} for function bodies, not colons")
}
```

错误恢复策略：
- 跳过当前 Token，尝试下一个
- 记录错误位置和上下文
- 收集所有错误后统一报告
