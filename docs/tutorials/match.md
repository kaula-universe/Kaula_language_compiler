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
    std.io.println(dirLabel(Direction.North))   # north
    std.io.println(dirLabel(Direction.West))    # west
}
```

> `match` 也可作为表达式直接赋给变量。C 的 `switch` 不能作表达式值，编译器
> 内部改写为「声明 + switch 赋值」（`GenerateMatchAssign`）：
>
> ```kaula
> auto word = match(x) {
>     1 => "one",
>     2 => "two",
>     3 => "three",
>     _ => "many"
> }
> ```
>
> 整数、字符串、枚举值均支持该形式，完整可运行示例见 `test/stress_enum_match.kl`。

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
    std.io.println(r)   # two

    string cmd = "start"
    string action = ""
    match cmd {
        "start" => { action = "begin" },
        "stop"  => { action = "end" },
        _       => { action = "unknown" }
    }
    std.io.println(action)   # begin
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
        Circle(r) => { std.io.println("circle r = ", r) },
        Rect(w, h) => { std.io.println("rect ", w, " x ", h) },
        _ => { std.io.println("unknown") }
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
