# 模式匹配 (match)

Kaula 使用 `match` 表达式进行模式匹配，支持解构枚举变体、通配符和变量绑定。

## match 表达式

`match` 是表达式，可以返回值：

```kaula
auto description = match(x) {
    1 => "one",
    2 => "two",
    _ => "many"
}
println(description)
```

`_` 是通配符，匹配所有剩余情况。

每个分支是一个 `模式 => 表达式`，多个分支用逗号分隔。

## 匹配枚举变体

match 可以解构枚举变体中的数据：

```kaula
enum Option<T> {
    Some(T),
    None
}

fn check(Option<int> opt) {
    match opt {
        Some(val) => {
            println("got: ", val)
        },
        None => {
            println("nothing")
        }
    }
}
```

大写开头为枚举变体模式，小写开头为变量绑定。

## 多值匹配

```kaula
match value {
    "start" => println("begin"),
    "stop"  => println("end"),
    _       => println("unknown")
}
```

## 完整示例

参见 [examples/match.kl](examples/match.kl)。
