# 控制流

Kaula 提供了 `if`、`while`、`for` 三种基本控制流结构。

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

## for 循环

类似 C 的 for 语法：

```kaula
for (int j = 0; j < 3; j = j + 1) {
    println("j = ", j)
}
```

## break 和 continue

```kaula
int i = 0
while (true) {
    i = i + 1
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
