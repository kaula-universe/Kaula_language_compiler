# 控制流

Kaula 提供了 `if`、`while`、`for` 三种基本控制流结构。其中 `for` 为 range 系迭代，不再支持 C 风格的三段式 `for(init; cond; update)`。

## if / else

```kaula
int score = 85

if (score >= 90) {
    println("Grade: A")
} else if (score >= 80) {
    println("Grade: B")
} else {
    println("Grade: C")
}
```

条件表达式可以加括号也可以不加。

## while 循环

```kaula
int i = 0
while (i < 5) {
    println("i = ", i)
    i = i + 1
}
```

## for 循环（range 系）

Kaula 的 `for` 采用 `for <var> in <iterable> { ... }` 形式，`<iterable>` 可以是 `range(...)` 或数组/切片表达式。C 风格的 `for(init; cond; update)` 已被移除，统一使用 range 迭代。

### range(N)

从 `0` 迭代到 `N-1`，步长为 `1`：

```kaula
for j in range(3) {
    println("j = ", j)
}
// 输出: j = 0, j = 1, j = 2
```

### range(start, end)

从 `start` 迭代到 `end-1`，步长为 `1`：

```kaula
for j in range(2, 6) {
    println("j = ", j)
}
// 输出: j = 2, j = 3, j = 4, j = 5
```

### range(start, end, step)

指定步长，`step` 可以为负数（递减迭代）：

```kaula
for j in range(0, 10, 2) {
    println("j = ", j)
}
// 输出: j = 0, j = 2, j = 4, j = 6, j = 8

for j in range(5, 0, -1) {
    println("j = ", j)
}
// 输出: j = 5, j = 4, j = 3, j = 2, j = 1
```

### 迭代数组/切片

```kaula
int[5] arr = {10, 20, 30, 40, 50}
for x in arr {
    println("x = ", x)
}
```

循环变量的类型由编译器从迭代对象自动推断：`range(...)` 产生 `int`，数组/切片产生元素类型。索引变量由编译器内部管理，用户代码不可见，从源头消除越界可能。

## break 和 continue

```kaula
for i in range(100) {
    if (i == 3) {
        continue    # 跳过 3
    }
    if (i > 5) {
        break       # 退出循环
    }
    println("i = ", i)
}
```

## 完整示例

参见 [examples/control.kl](examples/control.kl)。
