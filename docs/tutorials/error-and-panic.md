# std 错误处理与可捕获 Panic

本教程演示 Kaula 标准库中 **std.error**、**std.option**、**std.panic** 三个模块的组合使用，构建完整的错误处理体系：

- **Result/Option**：函数级错误传播，支持 i64/f64/ptr/bool/String 多种成功载荷
- **Error 对象与错误链**：wrap/cause 语义，保留完整因果上下文
- **errno 桥接**：把系统错误码转为 Error，自动归类
- **可捕获 panic**：`panic_protect` 回调模式，unwrap 失败、主动 panic 均可捕获恢复

---

## 1. Option / Result 基础

### Option（可选值）

```kaula
import std.io
import std.option

fn main() {
    Option o = std.option.option_some_i64(42)
    if std.option.option_is_some(&o) {
        std.io.println("some: ", std.option.option_unwrap_i64(&o))
    }
    Option n = std.option.option_none()
    i64 v = std.option.option_unwrap_or_i64(&n, -1)   # 默认值
    std.io.println("unwrap_or fallback: ", v)
}
```

### Result（成功或错误）

```kaula
import std.io
import std.option

fn main() {
    Result ok = std.option.result_ok_i64(7)
    if std.option.result_is_ok(&ok) {
        std.io.println("ok value: ", std.option.result_unwrap_ok_i64(&ok))
    }

    Result sok = std.option.result_ok_string("hello")
    if std.option.result_is_ok(&sok) {
        string s = std.option.result_unwrap_ok_string(&sok)
        std.io.println("ok string: ", s)
        std.option.result_destroy(&sok)
    }

    Result err = std.option.result_err(7, "boom")
    if std.option.result_is_err(&err) {
        std.io.println("err code: ", std.option.result_err_code(&err))
        string m = std.option.result_err_msg(&err)
        std.io.println("err msg: ", m)
        std.option.result_destroy(&err)
    }
}
```

> string 载荷的 Result 用 `result_destroy` 回收，避免 KMM 泄漏。

### 链式传播

```kaula
import std.io
import std.option

fn fallible(n: i64) Result {
    if n == 0 { return std.option.result_err(1, "n is zero") }
    return std.option.result_ok_i64(n * 2)
}

fn main() {
    Result r = fallible(4)
    if std.option.result_is_ok(&r) {
        std.io.println("prop ok: ", std.option.result_unwrap_ok_i64(&r))
    }
}
```

---

## 2. `:=` 错误传播赋值（enum Result 专用）

> 类似 Rust 的 `?` 运算符。

### 语法

写成 `name := expr`（冒号加等号）：右侧是一个返回 Result 风格 enum 的表达式，
`:=` 自动判断 Ok/Err 并解包。

- 右侧 `expr` 必须返回一个 **enum 类型**，且该 enum 必须包含 `Ok(T)` 和 `Err(E)` 两个变体
- 若右侧为 `Err` → **自动 return 当前函数**，携带该 Err 值
- 若右侧为 `Ok` → 解包 `Ok_val` 赋值给 `name`，类型自动推断为 `T`

### 前提条件

1. 当前函数的返回类型必须与右侧 enum 类型**相同**
2. enum 必须有 `Ok` 变体且 `Ok` 必须携带一个字段
3. `main` 函数返回 `i32`，**不能**使用 `:=`

### 示例

```kaula
import std.io

enum IntResult {
    Ok(i32),
    Err(i32),
}

fn safe_div(a: i32, b: i32) -> IntResult {
    if b == 0 {
        return IntResult.Err(-1)
    }
    return IntResult.Ok(a / b)
}

fn compute(a: i32, b: i32, c: i32) -> IntResult {
    # 若 safe_div 返回 Err，自动 return；否则解包赋值
    x := safe_div(a, b)
    y := safe_div(x, c)
    return IntResult.Ok(y)
}

fn main() {
    std.io.println("demo: := 传播见 compute；正常路径返回 Ok")
}
```

### 注意事项

- `:=` 左侧变量类型由 `Ok` 变体字段类型推断
- 右侧表达式如果不是 enum 或没有 `Ok` 变体，sema 会报错

---

## 3. Error 对象与错误链

```kaula
import std.io
import std.error
import std.string

fn main() {
    Error* inner = std.error.error_create(8, 101, "cannot open file", "app.kl", 10)
    Error* mid = std.error.error_wrap(inner, 12, "load config failed")
    Error* outer = std.error.error_wrap(mid, 11, "app startup failed")

    if std.error.error_has_cause(outer) {
        Error* c1 = std.error.error_cause(outer)   # mid
        string msg = std.string.string_create(std.error.error_get_message(c1))
        std.io.println("inner cause: ", msg)
    }
    # 打印整条链（stderr）
    std.error.error_print_chain(outer)
}
```

---

## 4. errno 桥接

```kaula
import std.io
import std.error
import std.string

fn main() {
    Error* e = std.error.error_from_errno(2, "open('nofile.txt')")
    # 自动归类：ENOENT→NOT_FOUND, EACCES→PERMISSION_DENIED, ETIMEDOUT→TIMEOUT...
    string ty = std.string.string_create(std.error.error_type_to_string(std.error.error_get_type(e)))
    std.io.println("type: ", ty)
    std.io.println("code: ", std.error.error_get_code(e))
    string em = std.string.string_create(std.error.error_get_message(e))
    std.io.println("msg: ", em)
}
```

---

## 5. 可捕获 Panic（推荐用法：panic_protect）

Kaula **不支持**裸 `try/catch` 语法。标准库提供 **回调包裹** 模式：

```kaula
import std.io
import std.option
import std.panic
import std.string

fn risky_body(ctx: void()) {
    # 这里可以包含任意可能 panic 的代码
    std.panic.panic("deliberate panic!")
}

fn main() {
    PanicFrame f
    if std.panic.panic_protect(&f, &risky_body, null) == 1 {
        # 捕获到 panic
        string pm = std.string.string_create(std.panic.panic_message())
        std.io.println("caught: ", pm)
        std.io.println("code: ", std.panic.panic_last_code())
    }
}
```

### 关键点

| 要点 | 说明 |
|------|------|
| **panic_protect** | 唯一推荐入口。setjmp 帧在回调执行期间**始终在栈上**，longjmp 回跳合法 |
| **panic / panic_with_code** | 抛出 panic。在 `panic_protect` 内会跳转到最近的保护帧；块外会打印消息并 abort |
| **panic_message / panic_last_code** | 仅在捕获分支有效 |
| **unwrap 可捕获** | `option_unwrap_*`、`result_unwrap_ok_*` 失败时抛出可捕获 panic |

---

## 6. 常见陷阱

1. **const char* 打印**：`error_get_message`、`panic_message` 等返回 `const char*`，须先 `std.string.string_create(...)` 转成 string 再打印。

2. **Result string 载荷销毁**：`result_ok_string` 分配的字符串归 Result 所有，必须调用 `result_destroy` 释放。

3. **panic_protect 回调参数**：回调签名 `fn(ctx: void())`，第三个参数传 `null` 或上下文指针。

---

## 7. 完整示例

见仓库 `test/error_handling.kl` 与 `test/stress_panic.kl`。
