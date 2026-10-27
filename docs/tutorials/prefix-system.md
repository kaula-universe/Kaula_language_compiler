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

## 完整示例

参见 [examples/prefix.kl](examples/prefix.kl)。
