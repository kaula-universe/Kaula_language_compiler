# 类与接口

## 类

`class` 定义引用语义的类型，支持字段、方法和构造函数：

```kaula
import std.io
import std.math

class Vec2 {
    x: f64,
    y: f64

    constructor(f64 ax, f64 ay) {
        self.x = ax
        self.y = ay
    }

    fn length() f64 {
        return std.math.sqrt(self.x * self.x + self.y * self.y)
    }

    fn scale(f64 factor) {
        self.x = self.x * factor
        self.y = self.y * factor
    }
}
```

方法通过 `self` 引用实例字段。构造参数名避免与字段同名（否则触发 shadowing 警告）。

### 使用类

```kaula
import std.io
import std.math

class Vec2 {
    x: f64,
    y: f64
    constructor(f64 ax, f64 ay) {
        self.x = ax
        self.y = ay
    }
    fn length() f64 {
        return std.math.sqrt(self.x * self.x + self.y * self.y)
    }
    fn scale(f64 factor) {
        self.x = self.x * factor
        self.y = self.y * factor
    }
}

fn main() {
    Vec2 v = Vec2(3.0, 4.0)
    f64 len = v.length()
    println("length: ", len)    # length: 5

    v.scale(2.0)
    println("scaled: (", v.x, ", ", v.y, ")")
}
```

## 接口

`interface` 定义一组方法签名，类通过 `implements` 实现接口：

```kaula
import std.io

interface Shape {
    fn area() f64
    fn describe() string
}

class Circle implements Shape {
    radius: f64
    tag: string

    constructor(f64 r, string t) {
        self.radius = r
        self.tag = t
    }

    fn area() f64 {
        return 3.14159 * self.radius * self.radius
    }

    fn describe() string {
        return self.tag
    }
}

fn main() {
    Shape s = Circle(2.0, "my-circle")
    println("area = ", s.area())
    println("name = ", s.describe())
}
```

## 结构体 vs 类

| 特性 | struct | class |
|------|--------|-------|
| 语义 | 值类型（复制） | 引用类型（指针） |
| 方法 | 不支持 | 支持 |
| 构造函数 | 不支持 | 支持 |
| 接口 | 不支持 | 支持 `implements` |

## 完整示例

参见 [examples/class_interface.kl](examples/class_interface.kl)。
