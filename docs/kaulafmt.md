# Kaula 代码格式化工具 (kaulafmt)

`kaulafmt` 是 Kaula 语言的代码格式化工具，用于自动格式化 `.kl` 源文件，使其符合统一的代码风格。

## 使用方法

### 基本用法

```bash
# 将格式化结果输出到标准输出
kaulafmt <input.kl>

# 将格式化结果写回原文件
kaulafmt -w <input.kl>
```

### 命令行参数

| 参数 | 说明 |
|------|------|
| `<input.kl>` | 要格式化的 Kaula 源文件（必须为 `.kl` 扩展名） |
| `-w` | 将格式化结果写回原文件（in-place 编辑） |

### 示例

```bash
# 预览格式化结果
kaulafmt main.kl

# 格式化并写回文件
kaulafmt -w main.kl

# 格式化多个文件
kaulafmt -w src/*.kl
```

## 格式化规则

### 缩进

- 使用 **4 个空格**作为缩进单位
- 不使用 Tab 字符

### 空行

- 顶层语句之间用 **空行**分隔
- 块语句内部的每条语句后换行

### 代码风格

#### 函数定义

```kaula
# 普通函数
fn add(a, b) -> int {
    return a + b
}

# 带类型参数和约束的函数
fn max[T: Ord](a: T, b: T) -> T {
    if a > b {
        return a
    }
    return b
}

# 泛型函数（export 标记泛型）
export fn map[T, R](list: List[T], fn: (T) -> R) -> List[R] {
    // ...
}

# 导出函数
export fn publicApi() {
    // ...
}

# 空函数体格式化为单行
fn empty() {}
```

#### 控制流

```kaula
if condition {
    // ...
} else if other {
    // ...
} else {
    // ...
}

# 单条 else if 直接跟在 } 后面
if x > 0 {
    // ...
} else if x < 0 {
    // ...
} else {
    // ...
}

while condition {
    // ...
}

for i = 0; i < 10; i = i + 1 {
    // ...
}

# for 循环支持省略初始化和更新部分
for ; condition; {
    // ...
}
```

#### 类型定义

```kaula
# 类
class Person {
    name: string
    age: int

    fn getName() -> string {
        return self.name
    }

    constructor(name: string, age: int) {
        // ...
    }
}

# 泛型类（export 标记泛型）
export class Container[T] {
    value: T

    fn get() -> T {
        return self.value
    }
}

# 实现接口的类
class Circle implements Drawable {
    radius: float

    fn draw() {
        // ...
    }

    fn getBounds() -> Rect {
        // ...
    }
}

# 接口
interface Drawable {
    fn draw()
    fn getBounds() -> Rect
}

# 结构体
struct Point {
    x: float
    y: float
}

# 类型别名
type ID = int

# 泛型类型别名
type export Pair[T, U] = (T, U)
```

#### 导入导出

```kaula
import "std/io"
export myFunction
```

#### 变量声明

```kaula
# 局部变量
let x = 10
const PI = 3.14

# NonLocal 变量（全局）
global counter = 0
local temp = 100

# 带值的变量声明
global name = "hello"
let result = a + b
```

#### 函数注解

```kaula
#[prefix]
fn prefixHandler() {
    // ...
}

#[tree]
fn treeBuilder() {
    // ...
}

#[root]
fn rootFn() {
    // ...
}

#[prefix,tree]
fn combinedAnnotation() {
    // ...
}
```

#### 表达式

```kaula
# 二元表达式
result = a + b * c

# 函数调用
print("hello")
add(1, 2)

# 索引访问
item = arr[0]
nested = matrix[i][j]

# 成员访问
name = person.name
value = obj.field.subfield

# 前缀调用
$result = $prefix{expr}

# 类型转换
num = (int)(floatValue)
str = (string)(obj)
```

#### 其他语句

```kaula
# VO (Value Object)
vo {
    someValue
}

# Spend 语句
spend target {
    call index {
        // ...
    }
}

# Task 语句
task function arg

# Call 语句
call target

# Tree 语句
tree rootNode

# Object 语句（带类型）
object TypeName MyObject {
    field1
    field2
    value = someExpression
}

# Object 语句（无类型）
object SimpleObj {
    field
}

# Return 语句
return
return value
return a + b
```

## 工作原理

`kaulafmt` 的处理流程：

1. **词法分析** - 使用 `kaula-compiler/internal/lexer` 将源代码转换为 Token 流
2. **语法分析** - 使用 `kaula-compiler/internal/parser` 解析 Token 流生成 AST
3. **格式化输出** - 遍历 AST，按照格式化规则重新生成源代码

### 核心组件

```go
type Formatter struct {
    indent    int           // 当前缩进级别
    buf       bytes.Buffer  // 输出缓冲区
    indentStr string        // 缩进字符串（4 空格）
}
```

### 处理的语句类型

- `fn` - 函数定义（支持泛型、类型参数、注解）
- `if` / `else` / `else if` - 条件语句
- `while` - 循环语句
- `for` - 循环语句
- `return` - 返回语句
- `vo` - Value Object
- `spend` - Spend 语句
- `task` - Task 语句
- `prefix` - Prefix 语句
- `tree` - Tree 语句
- `object` - 对象定义
- `class` - 类定义
- `interface` - 接口定义
- `struct` - 结构体定义
- `type` - 类型别名
- `import` - 导入语句
- `export` - 导出语句
- `nonlocal` - 非局部变量
- `call` - 调用语句
- `method` - 方法定义（类内部）
- `constructor` - 构造函数（类内部）

### 处理的表达式类型

- 二元表达式 (`a + b`)
- 函数调用 (`func(args)`)
- 标识符 (`variable`)
- 整数/浮点数/字符串/布尔字面量
- 索引访问 (`arr[i]`)
- 成员访问 (`obj.field`)
- 前缀调用 (`$name{...}`)
- 类型转换 (`(Type)(expr)`)

## 构建

`kaulafmt` 使用 Go 编写，作为编译器工具链的一部分构建：

```bash
# 使用构建脚本
python toolkit_build.py

# 或单独构建
cd kaula-compiler/cmd/kaulafmt
go build -o kaulafmt.exe
```

## 错误处理

- 输入文件必须为 `.kl` 扩展名
- 如果解析失败，工具会退出并显示错误信息
- 无法格式化时会返回非零退出码

## 限制

- 不支持格式化表达式内部的复杂嵌套（如链式调用的换行）
- 不支持自定义缩进风格
- 保留注释（注释会在格式化过程中丢失）
- 未知的语句或表达式类型会被注释标记（如 `/* unknown statement: xxx */`）
- 不支持格式化注解的自定义格式
- 不支持保留原始代码中的手动换行
