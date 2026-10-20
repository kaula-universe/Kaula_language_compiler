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

## 完整示例

参见 [examples/prefix.kl](examples/prefix.kl)，跨文件用例见
[test/prefix_xfile.kl](../../test/prefix_xfile.kl)。

