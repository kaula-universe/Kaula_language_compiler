# 泛型

Kaula 支持在函数、结构体、枚举和类上使用泛型，在编译期展开为具体类型，零运行时开销。

## 泛型函数

使用 `<T>` 声明类型参数：

```kaula
fn identity<T>(T x) T {
    return x
}

fn max<T>(T a, T b) T {
    if a > b {
        return a
    }
    return b
}
```

调用时可显式指定类型参数或由编译器推导：

```kaula
auto x = identity<int>(42)     # 显式
auto y = identity(3.14)        # 推导为 float
auto m = max(10, 20)           # 推导为 int
```

## 泛型结构体

```kaula
struct Pair<T, E> {
    first: T,
    second: E
}

fn main() {
    Pair<int, string> p = { .first = 1, .second = "one" }
    println(p.first)
}
```

## 泛型枚举

代数数据类型天然适合泛型：

```kaula
enum Option<T> {
    Some(T),
    None
}

enum Result<T, E> {
    Ok(T),
    Err(E)
}
```

使用：

```kaula
fn divide(int a, int b) Result<int, string> {
    if b == 0 {
        return Result.Err("division by zero")
    }
    return Result.Ok(a / b)
}
```

## 泛型类

```kaula
class Box<T> {
    value: T

    constructor(T val) {
        self.value = val
    }

    fn get() T {
        return self.value
    }
}
```

## 泛型约束

可为类型参数指定约束：

```kaula
fn print_all<T: ToString>(T item) {
    println(item.to_string())
}
```

## 完整示例

参见 [examples/generics.kl](examples/generics.kl)。
