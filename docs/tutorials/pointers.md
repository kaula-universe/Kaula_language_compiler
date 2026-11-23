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
    std.io.println(x)          # 输出: 100
}
```

指针类型可以写在变量名前后：

```kaula
import std.io

fn main() {
    i64 x = 1
    i64* p1 = &x        # 类型前置写法
    i64 *p2 = &x        # 类型后置写法（等价）
    std.io.println(*p1, *p2)
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
        std.io.println(*ptr)
    }
}
```

## 可空类型（编译期 `T?`）

Kaula 的可空性是**语言级编译期机制**：类型加 `?` 后缀（如 `i64?`），
配合流敏感判空分析在编译期拒绝未判空的使用；生成的 C 代码中 `?` 被
完全擦除（与 `T` 是同一个 C 类型），**零运行时开销**。

```kaula
import std.io

fn main() {
    i64 x = 42
    i64? p = &x          # 可空指针
    i64? none = null

    if (p != null) {
        # 判空分支内 p 被标记为「已检查」，可安全解引用
        std.io.println(*p)   # 42
    }
    if (none == null) {
        std.io.println("none is null")
    }
}
```

**流敏感判空跟踪**：`if (v != null)` / `if (v == null)` 的分支会更新
`v` 的「已判空」状态（快照/合并，`if/else` 两路各自成立）。把未判空的
可空变量传给非可空形参会在编译期被拒绝：

```text
error[nullable_passed]: 可空变量 'p' 未判空即作为实参传递,
  但形参类型 'i64*' 不接受 null
  Suggestion: 先判空再传递: if p != null { ... }
```

规则小结：

| 写法 | 结果 |
|------|------|
| `T? v = expr` | 声明可空变量 |
| `v = null` | 赋空值（`null` 兼容所有可空类型） |
| `if (v != null) { ... }` | 分支内 `v` 视为已判空，可解引用/传递 |
| 未判空即传递 | 编译错误 `nullable_passed` |
| 生成的 C | `?` 擦除，零运行时开销 |

> **与 `std.option` 的分工**：`T?` 适合指针/引用的「空或不空」，安全
> 保证在编译期；`std.option.Option` 是运行期装箱的可选值（unwrap 失败
> 抛可捕获 panic），适合把「有值/无值」当作数据传递的场景。用法见
> [std 错误处理](error-and-panic.md)。
>
> 回归语料：`test/nullable_flow.kl`；完整可运行示例见
> [examples/nullable.kl](examples/nullable.kl)。

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
    std.io.println("x = ", x, ", y = ", y)   # x = 2, y = 1
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
    std.io.println(p[0])
}
```

## 完整示例

参见 [examples/pointers.kl](examples/pointers.kl)。
