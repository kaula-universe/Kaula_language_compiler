# 模式匹配 (match)

Kaula 使用 `match` 进行模式匹配，支持枚举、整数、字符串的分支分派。
`match` 是**语句**：在 `match` 之前先声明结果变量，各分支内对其赋值，分支结束后使用。

## 基本分派

```kaula
import std.io

enum Direction { North, South, East, West }

fn dirLabel(Direction d) string {
    string result = ""
    match d {
        North => { result = "north" },
        South => { result = "south" },
        East => { result = "east" },
        West => { result = "west" }
    }
    return result
}

fn main() {
    println(dirLabel(Direction.North))   # north
    println(dirLabel(Direction.West))    # west
}
```

> 注意：把 `match` 写成 `auto x = match(v) { ... }` 表达式形式当前不支持；
> 正确做法是先声明 `string result = ""`，在分支里赋值，再在分支外使用。

## 整数与字符串匹配

`_` 是通配符，匹配所有剩余情况。

```kaula
import std.io

fn main() {
    i64 code = 2
    string r = ""
    match code {
        1 => { r = "one" },
        2 => { r = "two" },
        _ => { r = "many" }
    }
    println(r)   # two

    string cmd = "start"
    string action = ""
    match cmd {
        "start" => { action = "begin" },
        "stop"  => { action = "end" },
        _       => { action = "unknown" }
    }
    println(action)   # begin
}
```

## 匹配带载荷的枚举变体（解构）

`match` 可以解构带数据的枚举变体，把载荷绑定到局部变量。大写开头是变体模式，
小写开头是绑定变量名。

```kaula
import std.io

enum Shape {
    Circle(f64),
    Rect(i64, i64)
}

fn describe(Shape s) {
    match s {
        Circle(r) => { println("circle r = ", r) },
        Rect(w, h) => { println("rect ", w, " x ", h) },
        _ => { println("unknown") }
    }
}

fn main() {
    Shape a = Shape.Circle(3.0)
    Shape b = Shape.Rect(4, 5)
    describe(a)
    describe(b)
}
```

> 构造带载荷的枚举值时必须用限定名 `Shape.Circle(3.0)`；载荷在 `enum` 里只写类型
> 列表（`Circle(f64)`），不写字段名；在 `match` 解构时再把载荷绑定到局部变量
> （`Circle(r) => ...`）。与基本分派一样：在分支块内处理，记得带 `_` 兜底。

## 完整示例

参见 [examples/match.kl](examples/match.kl)。
