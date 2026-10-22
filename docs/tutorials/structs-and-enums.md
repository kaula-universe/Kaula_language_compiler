# 结构体与枚举

## 结构体

使用 `struct` 定义复合数据类型：

```kaula
struct Point {
    x: i64,
    y: i64
}

struct Rectangle {
    tl: Point,
    br: Point
}
```

字段使用 `name: type` 语法，支持逗号或换行分隔。

### 访问结构体字段

通过 `.` 运算符访问：

```kaula
import std.io

struct Point {
    x: i64,
    y: i64
}

struct Rectangle {
    tl: Point,
    br: Point
}

fn area(Rectangle r) i64 {
    i64 w = r.br.x - r.tl.x
    i64 h = r.br.y - r.tl.y
    return w * h
}

fn main() {
    Rectangle rect = Rectangle {
        tl: Point { x: 0, y: 0 },
        br: Point { x: 3, y: 4 }
    }
    println("area = ", area(rect))   # area = 12
}
```

### 结构体字面量

构造时用 `TypeName { field: value, ... }`，嵌套结构体同样嵌套写：

```kaula
import std.io

struct Point {
    x: i64,
    y: i64
}

fn main() {
    Point p = Point { x: 3, y: 4 }
    println(p.x, p.y)
}
```

## 枚举

使用 `enum` 定义枚举类型，支持无变体枚举（类似 C enum）：

```kaula
import std.io

enum Color {
    Red,
    Green,
    Blue
}

fn main() {
    Color c = Color.Red
    println("color = ", c)
}
```

访问枚举值用限定名 `Color.Red`。带载荷的枚举变体与 `match` 解构见 [match.md](match.md)。

## 完整示例

参见 [examples/struct_enum.kl](examples/struct_enum.kl)。

## C 代码生成

Kaula 编译器将 struct 编译为 C 的 `typedef struct`，并自动添加 `K_` 前缀以避免与系统头文件冲突：

```kaula
struct Point {
    x: i64,
    y: i64
}

struct Rectangle {
    tl: Point,
    br: Point
}
```

生成的 C 代码：

```c
typedef struct K_Point {
    int64_t x;
    int64_t y;
} K_Point;

typedef struct K_Rectangle {
    K_Point tl;
    K_Point br;
} K_Rectangle;
```

> `K_` 前缀是编译器内部约定，用户代码中始终使用 Kaula 原名（如 `Rectangle`），编译器在代码生成阶段自动替换。详见[代码生成器文档](../code-generation.md#用户类型命名消歧)。
