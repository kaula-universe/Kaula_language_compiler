# 指针与可空类型

Kaula 提供 C 风格的指针操作，以及基于 `std.option` 的安全可空类型。

## 指针

使用 `*` 声明指针类型，`&` 取地址，`*` 解引用：

```kaula
import std.io

fn main() {
    i64 x = 42
    i64* ptr = &x       # ptr 指向 x
    *ptr = 100          # 解引用并修改 x 的值
    println(x)          # 输出: 100
}
```

指针类型可以写在变量名前后：

```kaula
import std.io

fn main() {
    i64 x = 1
    i64* p1 = &x        # 类型前置写法
    i64 *p2 = &x        # 类型后置写法（等价）
    println(*p1, *p2)
}
```

## 空指针

指针可以赋值为 `null`，使用前判空：

```kaula
import std.io

fn main() {
    i64 x = 42
    i64* ptr = &x
    if (ptr != null) {
        println(*ptr)
    }
}
```

## 可空类型（std.option）

Kaula 不使用 `int?` 后缀语法表示可空，而是用标准库 `std.option.Option`：

```kaula
import std.io
import std.option

fn main() {
    Option some = std.option.option_some_i64(42)
    Option none = std.option.option_none()

    if (std.option.option_is_some(&some)) {
        i64 v = std.option.option_unwrap_i64(&some)
        println("some = ", v)              # some = 42
    }
    i64 d = std.option.option_unwrap_or_i64(&none, 7)
    println("none fallback = ", d)         # none fallback = 7
}
```

`Option` 在编译期强制处理「有值/无值」两种情况，减少空引用崩溃。
完整 API 见 [error-and-panic.md](error-and-panic.md) 与 `std/option`。

## 指针与函数参数

指针作为函数参数可实现引用语义（修改调用方的变量）：

```kaula
import std.io

fn swap(i64* a, i64* b) {
    i64 tmp = *a
    *a = *b
    *b = tmp
}

fn main() {
    i64 x = 1, y = 2
    swap(&x, &y)
    println("x = ", x, ", y = ", y)   # x = 2, y = 1
}
```

## 不透明指针（void()）

Kaula **禁止裸 `void*`**。不透明数据指针使用透明记法 `void()`（见
[variables-and-types.md](variables-and-types.md#ffi-专用用-adopt-套用-c-原生类型)）：

```kaula
import std.io
import std.memory

fn main() {
    void() buf = std.memory.kmm_v4_alloc(64)
    i64* p = as<i64*>(buf)
    p[0] = 1
    println(p[0])
}
```

## 完整示例

参见 [examples/pointers.kl](examples/pointers.kl)。
