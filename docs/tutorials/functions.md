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

## 嵌套函数与闭包捕获

Kaula 允许在函数体内定义嵌套函数（`fn`），嵌套函数可以**读写**外层函数的局部变量。
对外层变量的引用会自动按引用（指针）传递：嵌套函数被提升为文件级函数，编译器
为其追加隐式捕获参数 `_cap_N`，并在调用点自动传入外层变量的地址。

```kaula
fn counter() {
    int count = 0
    fn increment() int {
        count = count + 1    # 读+写外层局部变量（自动捕获）
        return count
    }
    println("count = ", increment())   # count = 1
    println("count = ", increment())   # count = 2
}
```

### nonlocal 声明

`nonlocal` 用于显式声明嵌套函数绑定外层函数作用域中的同名变量（与 Python 的 `nonlocal` 类似），
便于阅读与静态校验，不影响捕获行为：

```kaula
fn outer() {
    int x = 10
    fn inner() {
        nonlocal int x       # 显式声明绑定外层 x
        x = x + 1
    }
    inner()
}
```

`nonlocal` 的编译期校验规则：

- 同名变量必须先存在**外层函数作用域**中（本作用域内已声明同名变量 → 错误）；
- 仅存在于全局作用域时无需 `nonlocal`（直接读写即可），写成 `nonlocal` 报错；
- 声明的类型必须与外层变量一致（未写类型时自动取外层类型）。

### 支持范围与限制

| 场景 | 支持 |
|---|---|
| 嵌套函数读写外层局部变量 | ✅ 支持（按引用捕获） |
| 深层嵌套（内层捕获外层函数的捕获） | ✅ 支持（指针转发） |
| 在 `if`/`while` 等块内定义嵌套函数 | ✅ 支持 |
| 嵌套函数作为参数传递**给**嵌套函数 | ✅ 支持 |
| lambda 捕获外层局部变量 | ❌ 暂不支持（编译报错），请改用嵌套函数 |
| 带捕获的嵌套函数作为返回值传递（闭包逃逸） | ❌ 不支持（捕获指针仅在原作用域内有效） |

>捕获按引用传递（`&var`），与 Kotlin/C++ 的引用捕获语义一致，所以嵌套函数
>只能在声明它的外层函数作用域内被调用；需要跨作用域携带环境时请使用类/结构体。

## 完整示例

参见 [examples/functions.kl](examples/functions.kl)。
