# 动态对象（object）

`object` 是 Kaula 的**运行时动态对象**：键是字符串，值是无类型装箱槽。
它不占静态布局，字段在运行时增加、覆盖、删除，适合配置、JSON 风格的松散数据、
以及编译期还不知道有哪些字段的场景。

与 `struct` 的分工：

| 维度 | `struct` | `object` |
|------|----------|----------|
| 字段集合 | 编译期固定 | 运行期可变 |
| 字段类型 | 静态确定 | 无类型（装箱） |
| 内存布局 | 紧凑、可内联 | 哈希表，堆分配 |
| 访问开销 | 偏移量直接寻址 | 字符串查找 |
| 典型用途 | 数据建模、FFI 布局 | 配置、插件参数、动态记录 |

结论：**能用 `struct` 就用 `struct`**，`object` 是布局未知时的逃生口。

## 创建：对象字面量

`object { 字段: 值, 字段: 值 }` 在表达式位置构造一个对象，等价于先 `dynobj_create()`
再逐个 `dynobj_set()`：

```kaula
import std.io
import std.obj

fn main() {
    object point = object { x: 1, y: 2 }
    std.io.println("字段数: ", std.obj.dynobj_size(point))
    std.io.println("有 x 吗: ", std.obj.dynobj_contains(point, "x"))
    std.io.println("有 z 吗: ", std.obj.dynobj_contains(point, "z"))
}
```

- 字段名是**字符串键**，不加引号也算字符串字面量。
- 值可以是任意可装箱表达式：整数、浮点、布尔、字符串，甚至另一个对象。

空对象用 `object()`：

```kaula
import std.io
import std.obj

fn main() {
    object empty = object()
    std.io.println("空对象字段数: ", std.obj.dynobj_size(empty))
}
```

## 读、写与动态增删

字段读用点语法，等价于 `dynobj_get`；字段写用赋值，等价于 `dynobj_set`：

```kaula
import std.io
import std.obj

fn main() {
    object point = object { x: 1, y: 2 }

    // 读：返回装箱值句柄
    object xv = point.x
    std.io.println("读到 x 的句柄: ", xv)

    // 写：覆盖已有字段，字段数不变
    point.x = 10
    std.io.println("覆盖后字段数: ", std.obj.dynobj_size(point))

    // 写一个不存在的键 = 动态增加字段
    point.z = 3
    std.io.println("新增后字段数: ", std.obj.dynobj_size(point))

    // 删除字段
    std.obj.dynobj_delete(point, "z")
    std.io.println("删除后字段数: ", std.obj.dynobj_size(point))
}
```

写字段时**不做静态类型兼容检查**：动态对象字段是无类型的装箱槽，运行期
`dynobj_set` 接受任何可装箱值，因此 `point.x = 10`（左值推断为 `object`、右值是
`i64`）是合法的，不应报 `assign_type_mismatch`。这是 2026-11-07 修正的语义，
回归语料是 `test/object_literal.kl`。

## 内省 API

`import std.obj` 之后可用：

| API | 说明 |
|-----|------|
| `dynobj_size(obj)` | 字段数，返回 `size_t` |
| `dynobj_contains(obj, key)` | 键是否存在，返回布尔 |
| `dynobj_get(obj, key)` | 取装箱值 |
| `dynobj_set(obj, key, value)` | 写字段（不存在则新增） |
| `dynobj_delete(obj, key)` | 删除字段 |
| `dynobj_clear(obj)` | 清空所有字段 |

`dynobj_size` 返回 `size_t`，和整数字面量比较时要显式转换：

```kaula
import std.io
import std.obj

fn main() {
    object point = object { x: 1, y: 2 }
    if std.obj.dynobj_size(point) == as<size_t>(2) {
        std.io.println("恰好两个字段")
    }
}
```

原因见[数值类型名称](numeric-type-names.md)：`size_t` 的宽度由目标 ABI 决定
（LP64 是 8 字节、32 位目标是 4 字节），不会静默与 `i64` 互相赋值。

## 嵌套

对象可以作字段值，形成树形结构：

```kaula
import std.io
import std.obj

fn main() {
    object user = object {
        name: "kaula",
        profile: object { age: 1, lang: "kl" }
    }
    std.io.println("顶层字段数: ", std.obj.dynobj_size(user))
    std.io.println("有 profile 吗: ", std.obj.dynobj_contains(user, "profile"))
}
```

## 与 `object Name { ... }` 语句的区别

- `object { ... }`（本章）是**表达式**，出现在赋值右侧或实参位置，每次求值新建一个对象；
- `object Name { ... }` 是**声明语句**，把 `Name` 登记成全局 `object` 类型符号，
  适合当模块级的共享配置容器。

声明语句的字段名必须唯一（重复报 `duplicate_object_field`），回归语料见
`test/object_stmt.kl`。

## 已知限制

写这篇教程时编译器团队明确登记的限制，使用前请知晓：

1. **`.kl` 层拿不到拆箱 API**。`std.obj` 只暴露 `dynobj_get/set/size/contains/delete/clear`，
   没有 `dynobj_box_*` / `dynobj_unbox_*` 胶水。因此读出的字段是**不透明句柄**
   （打印出来是地址），不能直接参与数值运算，需要 C 侧拆箱函数或在未来补上胶水层。
2. **裸机（freestanding）不支持动态对象**。运行时 `dynobj_*` 依赖托管堆；
   裸机目标下声明 `object` 会被前端直接拒绝，而不是链接期才炸符号。

完整可运行示例见 [`examples/objects.kl`](examples/objects.kl)。
