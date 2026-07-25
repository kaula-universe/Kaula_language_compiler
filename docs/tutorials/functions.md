# 函数

Kaula 使用 `fn` 关键字定义函数，采用类似 Go 的语法风格。

## 函数定义

**无返回值函数：**

```kaula
fn greet(string name) {
    println("Hello, ", name)
}
```

**有返回值函数：**

```kaula
fn add(int a, int b) int {
    return a + b
}
```

返回类型写在参数列表之后。

## 参数

函数参数使用 `type name` 的格式声明：

```kaula
fn calculate(int x, int y, string label) int {
    println(label)
    return x + y
}
```

## 多返回值

> Kaula 目前暂不支持多返回值，可以通过结构体或指针参数实现。

## 递归

Kaula 支持递归函数调用：

```kaula
fn factorial(int n) int {
    if (n <= 1) {
        return 1
    }
    return n * factorial(n - 1)
}
```

## 完整示例

参见 [examples/functions.kl](examples/functions.kl)。
