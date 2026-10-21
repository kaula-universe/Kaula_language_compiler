# 前缀系统

Prefix 是 Kaula 独有的声明式代码复用机制，类似带参数的代码模板，在编译期展开。

## 定义前缀

使用 `prefix` 关键字定义模板，`$` 前缀引用参数：

```kaula
prefix Logger {
    $message
    $level
    auto log = $level + ": " + $message
    println(log)
}

prefix Validate {
    $value
    $min
    if $value < $min {
        println("validation failed")
    }
}
```

## 调用前缀

使用 `@PrefixName(params)` 语法：

```kaula
fn main() {
    int age = -1

    @Logger(message = "server started", level = "INFO")
    @Validate(value = age, min = 0)
}
```

等价于手动展开：

```kaula
fn main() {
    int age = -1
    # 展开 Logger
    auto log = "INFO" + ": " + "server started"
    println(log)
    # 展开 Validate
    if age < 0 {
        println("validation failed")
    }
}
```

## 实用场景

### 资源管理

```kaula
prefix with_lock {
    lock($lock)
    $body
    unlock($lock)
}

@with_lock(lock = my_mutex) {
    critical_section()
}
```

### 日志包装

```kaula
prefix trace {
    println("[TRACE] ", $msg)
}

@trace(msg = "entering function")
```

## 展开规则（精确语义）

前缀模板体里出现的 `$name` 分三类，展开器按下面的顺序判定：

| 形态 | 判定 | 展开行为 |
|------|------|----------|
| 裸 `$name`（独占一条语句） | 参数**声明** + 调用体拼接点 | 不生成代码；把 `@X(...) { body }` 的 body 拼在这里 |
| `$name = 默认值` | 带默认值的参数声明 | 不生成代码；调用方未传该参数时用默认值绑定 |
| 表达式里的 `$name` | 参数**引用** | 替换为调用方实参（或默认值）生成的 C 表达式 |

调用体的落点规则：第一个**未被绑定的裸 `$param`** 才是拼接点；若模板体里没有这样的
占位符，调用体整体追加在模板体之后。整个展开结果包在一个块作用域里，因此同名变量
多次展开不会重定义。

```kaula
prefix Retry {
    $times = 3      // 带默认值：调用方不传就是 3
    $body           // 裸占位符：调用体拼在这里
    println("[RETRY] budget=", $times)
    $body           // 已经拼过一次，此处不再重复
}

@Retry {            // 不传 times → 用默认值 3
    do_work()
}
```

## 结构宏：四个"能看到语法"的能力

前缀不只是"替换文本"，它在 **AST 层**工作。下面四条决定了它能写出真正操作语法的宏。

### 1. 参数绑定的是 AST，不是字符串

`$param` 的载荷是调用点传入的**表达式节点**（`ast.Expression`），不是提前生成的 C
代码。展开时才按需生成，因此同一个宏在不同上下文中可以正确地使用实参的语法结构。

```kaula
#[prefix]
fn Twice(x: i64) {
    i64 r = $x + $x     // 两次引用 $x：实参 `n + 1` 会被完整地放两次
    r
}

@Twice(x = n + 1)       // → int64_t r = (n+1) + (n+1)，括号由 AST 结构保证
```

默认值同样是 AST：`$times = 3` 里的 `3` 是字面量节点，只有调用方确实不传时才求值。

#### `$p = expr` 的两种含义

同一个写法有两种用途，判据是**调用点有没有为 `p` 传实参**：

```kaula
#[prefix]
fn AddTo(target: i64, value: i64) {
    $target = $target + $value      // $target 有实参 → 这是普通赋值
}

#[prefix]
fn WithDefault(x: i64) {
    $limit = 10                     // $limit 没有实参 → 这是默认值声明，不生成代码
    println($x + $limit)
}
```

| 调用点 | `$p = expr` 的含义 | 是否生成代码 |
|--------|--------------------|--------------|
| 传了 `p = …` | 普通赋值（左值 `$p` 被替换为实参 AST） | 生成 |
| 没传，且是首次出现 | 参数默认值声明 | 不生成，`$p` 以默认值内联 |
| 没传，但 `$p` 已在前面出现过 | 普通赋值 | 生成 |

所以 `@AddTo(target = acc, value = 42)` 展开后 `acc` 真的会变：

```kaula
i64 acc = 100
@AddTo(target = acc, value = 42)
println(acc)          // 142
```

`$limit = 10` 则不会在展开结果里留下一条无用赋值，`$limit` 直接以 `10` 内联。

### 2. 自动 alpha 重命名（卫生性）

展开时，模板体内**自己声明的局部变量**会被唯一化为 `__kaula_m{seq}_{name}`，
因此宏里的临时变量不会踩到调用方的同名变量：

```kaula
prefix Swap {
    $a
    $b
    i64 tmp = $a
    $a = $b
    $b = tmp
}

fn main() {
    i64 tmp = 100       // 调用方的 tmp
    i64 x = 1
    i64 y = 2
    @Swap(a = x, b = y) // 展开后宏内的 tmp → __kaula_m1_tmp，调用方的 tmp 保持 100
}
```

不同调用点得到不同序号，模板 AST 本身**从不被改写**（每次展开都是克隆）。

### 3. `tree(...)` 结构约束

在 `#[prefix] fn` 的参数位置可以写结构签名，声明"这个参数必须是某种语法结构"。
展开**之前**校验，不满足时给出带期望/实际结构的错误，而不是让生成出的 C 在
clang 阶段炸掉。

```kaula
#[prefix]
fn AddTo(target: i64, tree(i64) value: i64) {
    $target = $target + $value
}

@AddTo(target = acc, value = 42)          // OK
// @AddTo(target = acc, value = "text")   // 报错：结构/类型不匹配
```

可用的结构类型名：

| 写法 | 接受的实参 |
|------|-----------|
| `tree(expression)` / `tree(expr)` | 任意表达式族节点 |
| `tree(statement)` / `tree(stmt)` | 任意语句 |
| `tree(block)` | 语句块 |
| `tree(block<expression, expression>)` | 恰好两条表达式的块（嵌套形状也校验） |
| `tree(literal)` / `tree(call)` / `tree(identifier)` | 对应具体节点类别 |
| `tree(i64)` / `tree(string)` / `tree(Vec<i64>)` | 指定类型名（对字面量做实校验，标识符放行） |
| `tree(generic)` / `tree(any)` | 不限 |

同名 `tree Name { ... }` 声明也可以作为签名来源，其根约束作用到 `$body`。

### 4. 表达式级宏：`@Name(...)` 可以出现在表达式位置

模板体的**最后一条语句**如果是表达式，宏就有值：

```kaula
#[prefix]
fn Lower(c: char) -> i64 {
    i64 base = $c - 'A'
    base + 'a'          // 最后一条表达式 = 宏的值
}

fn main() {
    i64 v = @Lower(c = 'C') + 1     // 可以参与算术、作实参、作返回值
    println(v)                       // 100
}
```

实现上整体包成 GNU 语句表达式 `({ ... })`，宏内局部变量作用域封闭：

```c
int64_t v = ({ int64_t __kaula_m1_base = 67; __kaula_m1_base + 97; }) + 1;
```

**什么时候报错**（不会静默取一个无关的值）：

| 情形 | 结果 |
|------|------|
| 最后一条是变量声明 / 赋值 / `if` / 块 | 报错「没给出返回值」 |
| 最后一条是 void 调用（`println(...)`、`print`、`assert` 等） | 报错，不把 `printf` 的返回值当宏的值 |
| 表达式位置却写了 `{ ... }` 调用体 | 报错（调用体只属于语句位置） |
| 最后一条是 `return expr` / `yield expr` | 取值，不报错 |

## 跨文件使用

前缀定义的可见性与 `pub` 函数一致：`import "helper.kl"` 之后，helper 里定义的
`prefix` 语句与 `#[prefix]` 函数都能在导入方展开。

```kaula
// helper.kl
prefix Trace {
    $msg
    println("[TRACE] ", $msg)
}
```

```kaula
// main.kl
import "helper.kl"

fn main() {
    @Trace(msg = "cross-file works")   // 展开为 println("[TRACE] ", "cross-file works")
}
```

> **历史缺陷（已修复）**：早期版本的语义分析与代码生成只在**本文件 AST** 里查找
> 前缀定义，跨文件调用既不报错也不展开 —— 编译状态是 `Success`，`@Trace(...)`
> 却被整个丢弃，只在运行时表现为"少了几行输出"。现在两侧都会接收被导入文件的
> 前缀定义（`SetLocalExternalPrefixes`，由 CLI 的 `collectLocalPrefixDefs` 收集），
> 回归用例见 `test/prefix_xfile.kl`。

### `#[prefix]` 函数的参数必须带 `$`

`#[prefix]` 函数是"函数形态的前缀模板"，它的形参在模板体里同样要按 `$param`
引用：

```kaula
#[prefix]
pub fn trace(msg: string) {
    println("[T] ", $msg)     // 正确：$msg 会被替换为实参
    // println("[T] ", msg)   // 错误：msg 不是模板参数，生成出未声明的标识符
}
```

这与 `prefix Name { ... }` 语句的约定一致——模板参数一律用 `$` 标记，不加 `$`
的标识符就是普通的 Kaula 标识符，展开器不会替换它。

### 前缀函数体也会被生成为一个 C 函数

`#[prefix] fn` 除了在调用点展开，还会生成一个同名的 C 函数，用于兼容"前缀也能
当普通函数调用"的老写法。那个函数体里没有 `$limit` 这种东西，所以直译的条件很窄：

| 模板体形态 | 生成的 C 函数体 |
|-----------|----------------|
| 只用形参（`println($x)`）、或只有裸 `$p` 占位 | 直译，`$x` → 形参 `x`；只有占位时为空函数体 |
| 自声明了 `$参数`（`$limit = 10`） | 不生成函数体，只保留声明 |

```kaula
#[prefix]
fn WithDefault(x: i64) {
    $limit = 10              // 模板自声明的 $limit，在 C 里没有对应变量
    println($x + $limit)
}

fn main() {
    @WithDefault(x = 1)      // 展开：printf("%d\n", 1 + 10)
}
```

> **历史缺陷（已修复）**：早期实现把函数体原样直译，`$limit = 10` 变成
> `limit = 10;`，整个程序在 clang 阶段因 `use of undeclared identifier` 失败，
> 报错位置跟用户写的前缀毫无关系。现在遇到无法直译的模板体就不生成那个函数——
> 模板本来就不会被当普通函数调用，缺实现不影响任何合法调用。
> 回归用例：`TestPrefixBodyEmitsNoUndeclaredIdent`。

## 完整示例

参见 [examples/prefix.kl](examples/prefix.kl)，跨文件用例见
[test/prefix_xfile.kl](../../test/prefix_xfile.kl)。

