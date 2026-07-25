# 类与接口

## 类

`class` 定义引用语义的类型，支持字段、方法和构造函数：

```kaula
class Vec2 {
    x: f64,
    y: f64

    constructor(f64 x, f64 y) {
        self.x = x
        self.y = y
    }

    fn length() f64 {
        return math_sqrt(self.x * self.x + self.y * self.y)
    }

    fn scale(f64 factor) {
        self.x = self.x * factor
        self.y = self.y * factor
    }
}
```

方法通过 `self` 引用实例字段。

### 使用类

```kaula
fn main() {
    Vec2 v = Vec2(3.0, 4.0)
    auto len = v.length()
    println("length: ", len)    # length: 5.0

    v.scale(2.0)
    println("scaled: (", v.x, ", ", v.y, ")")
}
```

## 接口

`interface` 定义一组方法签名，类通过 `implements` 实现接口：

```kaula
interface Drawable {
    fn draw() void
}

interface Printable {
    fn to_string() string
}

class Circle implements Drawable, Printable {
    radius: f64

    constructor(f64 r) {
        self.radius = r
    }

    fn draw() void {
        println("drawing circle")
    }

    fn to_string() string {
        return "Circle(radius=" + string_from_f64(self.radius) + ")"
    }
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
