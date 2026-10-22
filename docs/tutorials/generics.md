# 泛型

Kaula 支持在函数、结构体、枚举上使用泛型，在编译期展开为具体类型（**单态化**），零运行时开销。

## 编译期单态化

Kaula 泛型采用 **monomorphization（单态化）** 策略：编译器在编译时为每组具体类型参数生成独立的、只处理该类型的 C 代码。

```
identity<i32>  →  专门处理 i32 的一份代码
identity<f64>  →  专门处理 f64 的另一份代码
```

与类型擦除不同，单态化后的代码：
- **无动态分派/反射开销**：等价于手写的专用函数
- **C ABI 友好**：实例化后的结构体可直接与 C 互操作
- **内联潜力大**：每个实例的代码可见，编译器可做针对性优化

为避免实例化深度无限递归，默认最大深度为 32 层。

## 泛型函数

使用 `<T>` 声明类型参数，参数用 `名字: T` 标注：

```kaula
import std.io

fn identity<T>(x: T) T {
    return x
}

fn main() {
    i64 a = identity<i64>(42)
    f64 b = identity<f64>(3.14)
    string c = identity<string>("gen")
    println(a, b, c)
}
```

调用时显式写 `identity<i64>(...)` 指定类型参数。

## 泛型结构体

```kaula
import std.io

struct Pair<T> {
    first: T,
    second: T
}

fn main() {
    Pair<i64> pi
    pi.first = 1
    pi.second = 2
    println(pi.first, pi.second)
}
```

字段逐个赋值即可。编译后会为 `Pair<i64>` 生成对应的 C 结构体。

## 泛型枚举与解构

代数数据类型天然适合泛型：

```kaula
import std.io

enum Box<T> {
    Value(T),
    Empty
}

fn boxValue(Box<i64> b) i64 {
    i64 result = 0
    match b {
        Value(v) => { result = v },
        Empty => { result = -1 },
        _ => { result = -2 }
    }
    return result
}

fn main() {
    Box<i64> full = Box.Value(7)
    Box<i64> empty = Box.Empty
    println(boxValue(full))    # 7
    println(boxValue(empty))   # -1
}
```

构造用限定名 `Box.Value(7)`，解构在 `match` 里把载荷绑定到局部变量。

## 完整示例

参见 [examples/generics.kl](examples/generics.kl) 与 `test/stress_generics.kl`。
