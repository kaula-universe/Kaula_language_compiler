# 字面量与初始化

本章讲清 Kaula 里几类字面量的写法、类型推导与初始化形式。它们的共同点是：
**写错时往往编译通过、零警告，只是安静地给出错答案**，所以值得逐条记住。

## 切片字面量

`[]T 变量 = [元素, ...]` 按元素类型分配，元素的存储宽度由 `T` 决定：

```kaula
import std.io

fn main() {
    []i32 a = [1, 3, 1990]
    []i64 b = [1, 3, 1990]
    std.io.println("i32 求和: ", a[0] + a[1] + a[2])
    std.io.println("i64 求和: ", b[0] + b[1] + b[2])
}
```

`[]i32` 与 `[]i64` 必须各自按 4/8 字节步长寻址。若元素类型被硬编码成 `int64_t`，
`[]i32` 的下标就会错位读，结果仍然是"一个数"，只是错的。

## 定长数组初始化

定长数组 `T 名[N]` 有两种初始化写法，推荐方括号：

```kaula
import std.io

fn main() {
    // 推荐：方括号，语义就是"逐元素初值"
    i32 arr[5] = [10, 20, 30, 40, 50]
    i32 s = 0
    for v in arr {
        s = s + v
    }
    std.io.println("求和: ", s)
}
```

花括号形式兼容可用，但要清楚它走的是**初始化器**语义，不是结构体字面量：

```kaula
import std.io

fn main() {
    i32 arr[5] = {10, 20, 30, 40, 50}
    std.io.println("arr[4] = ", arr[4])
}
```

两种写法都要求元素个数不超过 `N`，省略的尾部元素为零值。

## 结构体字面量

结构体三种写法都支持，按团队风格选一种即可：

```kaula
import std.io

struct Point {
    x: i64,
    y: i64
}

fn main() {
    Point a = Point { x: 3, y: 4 }      // 冒号形式（文档主推）
    Point b = Point { x = 5, y = 6 }    // 等号形式
    Point c = Point { .x = 7, .y = 8 }  // 带点形式（C 设计器习惯）
    std.io.println(a.x, " ", b.y, " ", c.x)
}
```

三种形式都必须是**全部字段给定**（或显式接受零值缺省），并且**不能丢字段名**：
把 `{ x = 10 }` 当成裸值列表处理会得到全零对象，编译期不报错。

## 整数字面量一律 `i64`

整数字面量不按数值大小收窄，永远是 `i64`；要窄类型必须显式转换：

```kaula
import std.io

fn main() {
    i64 big = 100            // 字面量就是 i64
    i32 small = as<i32>(100) // 显式转换才落到 i32
    std.io.println(big, " ", small)
}
```

同理，`size_t` 这类 ABI 相关宽度类型与整数字面量比较时也要 `as<size_t>(...)`，
细节见[数值类型名称](numeric-type-names.md)。

## 本章对应的回归语料

| 教程示例 | 回归语料 |
|----------|----------|
| 切片字面量、定长数组、结构体字面量 | `test/literal_semantics.kl` |

完整可运行示例见 [`examples/literals.kl`](examples/literals.kl)。
