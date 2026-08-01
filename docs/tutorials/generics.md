# 泛型

Kaula 支持在函数、结构体、枚举和类上使用泛型，在编译期展开为具体类型（**单态化**），零运行时开销。

## 编译期单态化

Kaula 泛型采用 **monomorphization（单态化）** 策略：编译器在编译时为每组具体类型参数生成独立的、只处理该类型的 C 代码。

```
Box<int>      →  typedef struct K_Box_int { int64_t value; } K_Box_int
Box<float>    →  typedef struct K_Box_float { float value; }   K_Box_float
Pair<int,i64> →  typedef struct K_Pair_int_i64 { int64_t first; int64_t second; } K_Pair_int_i64
```

与 Java 的类型擦除或 C# 的 JIT 泛型不同，单态化后的代码：
- **无动态分派/反射开销**：等价于手写的专用类型
- **C ABI 友好**：实例化后的结构体可直接与 C 互操作
- **内联潜力大**：每个实例的代码可见，编译器可做针对性优化

为避免实例化深度无限递归，默认最大深度为 32 层。

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

struct Box<T> {
    value: T
}

fn main() {
    Pair<int, string> p = { .first = 1, .second = "one" }
    println(p.first)

    Box<int> b = { .value = 42 }
    println(b.value)
}
```

编译后生成两个独立的 C 结构体：

```c
typedef struct K_Pair_int_string {
    int64_t first;
    String second;
} K_Pair_int_string;

typedef struct K_Box_int {
    int64_t value;
} K_Box_int;
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

    fn set(T val) void {
        self.value = val
    }
}

fn main() {
    Box<int> b = Box<int>(42)
    println(b.get())
    b.set(99)
    println(b.get())
}
```

构造函数与方法同样参与单态化：`Box<int>` 的 `get()` 返回 `int64_t`，`Box<float>` 的 `get()` 返回 `float`。

## 泛型约束

可为类型参数指定约束：

```kaula
fn print_all<T: ToString>(T item) {
    println(item.to_string())
}
```

## 类型参数中的泛型嵌套

泛型类型的类型参数可以引用另一个泛型类型：

```kaula
struct ListNode<T> {
    value: T,
    next: *ListNode<T>
}

Option<Box<int>>     # OK：泛型嵌套实例化
Pair<int, Box<f64>>  # OK：多参数泛型嵌套
```

## 完整示例

参见 [examples/struct_enum.kl](examples/struct_enum.kl) 与 [examples/control.kl](examples/control.kl)。
