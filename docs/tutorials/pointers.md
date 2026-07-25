# 指针与可空类型

Kaula 提供 C 风格的指针操作，以及安全的可空类型。

## 指针

使用 `*` 声明指针类型，`&` 取地址，`*` 解引用：

```kaula
int x = 42
int* ptr = &x       # ptr 指向 x
*ptr = 100          # 解引用并修改 x 的值
println(x)          # 输出: 100
```

指针类型可以写在变量名前后：

```kaula
int* p1 = &x        # 类型前置写法
int *p2 = &x        # 类型后置写法
```

## 空指针

指针可以赋值为 `null`：

```kaula
int* ptr = null
if (ptr != null) {
    println(*ptr)
}
```

## 可空类型

使用 `?` 后缀声明可空类型，比裸指针更安全：

```kaula
int? maybe = null       # 可空的 int
int? value = 42         # 也可赋正常值

string? name = null     # 可空的字符串
```

可空类型在编译期可被静态检查，减少空引用崩溃。

## 指针与函数参数

指针作为函数参数可实现引用语义（修改调用方的变量）：

```kaula
fn swap(int* a, int* b) {
    int tmp = *a
    *a = *b
    *b = tmp
}

fn main() {
    int x = 1, y = 2
    swap(&x, &y)
    println("x = ", x, ", y = ", y)   # x = 2, y = 1
}
```

## void 指针

`void*` 用于不透明指针：

```kaula
void* data = malloc(64)
```

## 完整示例

参见 [examples/pointers.kl](examples/pointers.kl)。
