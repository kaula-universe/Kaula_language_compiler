# std 错误处理与可捕获 Panic

本教程演示 Kaula 标准库中 **std.error**、**std.option**、**std.panic** 三个模块的组合使用，构建完整的错误处理体系：

- **Result/Option**：函数级错误传播，支持 i64/f64/ptr/bool/String 多种成功载荷
- **Error 对象与错误链**：wrap/cause 语义，保留完整因果上下文
- **errno 桥接**：把系统错误码转为 Error，自动归类
- **可捕获 panic**：`panic_protect` 回调模式，unwrap 失败、主动 panic 均可捕获恢复

---

## 1. 基本依赖

```kaula
import std.io
import std.option
import std.error
import std.panic
import std.string   # 用于把 const char* 转为 Kaula string 以便 println
```

---

## 2. Option / Result 基础

### Option（可选值）

```kaula
Option o = std.option.option_some_i64(42)
if std.option.option_is_some(&o) {
    std.io.println("some: ", std.option.option_unwrap_i64(&o))
}
Option n = std.option.option_none()
i64 v = std.option.option_unwrap_or_i64(&n, -1)   # 默认值
```

### Result（成功或错误）

```kaula
# 成功载荷：i64 / f64 / ptr / bool / string
Result ok = std.option.result_ok_i64(7)
Result bok = std.option.result_ok_bool(true)
Result sok = std.option.result_ok_string("hello")
# 错误
Result err = std.option.result_err(7, "boom")

# 判断与取值
if std.option.result_is_ok(&ok) {
    std.io.println(std.option.result_unwrap_ok_i64(&ok))
}
if std.option.result_is_err(&err) {
    std.io.println("code: ", std.option.result_err_code(&err))
    string m = std.string.string_create(std.option.result_err_msg(&err))
    std.io.println("msg: ", m)
}

# 资源回收（string 载荷必须显式销毁，避免 KMM 泄漏）
std.option.result_destroy(&sok)
std.option.result_destroy(&err)
```

### 链式传播

```kaula
fn fallible(n: i64) Result {
    if n == 0 { return std.option.result_err(1, "n is zero") }
    return std.option.result_ok_i64(n * 2)
}
Result r = fallible(4)   # 自动传播
```

---

## 3. Error 对象与错误链

```kaula
Error* inner = std.error.error_create(8, 101, "cannot open file", "app.kl", 10)
Error* mid = std.error.error_wrap(inner, 12, "load config failed")
Error* outer = std.error.error_wrap(mid, 11, "app startup failed")

if std.error.error_has_cause(outer) {
    Error* c1 = std.error.error_cause(outer)   # mid
    # 继续追踪
}
# 打印整条链（stderr）
std.error.error_print_chain(outer)
```

输出示例：
```
Error: Already exists (code: 0)
Message: app startup failed
  caused by:
Error: Cancelled (code: 0)
Message: load config failed
  caused by:
Error: Runtime error (code: 101)
Message: cannot open file
File: app.kl:10
```

---

## 4. errno 桥接

```kaula
Error* e = std.error.error_from_errno(2, "open('nofile.txt')")
# 自动归类：ENOENT→NOT_FOUND, EACCES→PERMISSION_DENIED, ETIMEDOUT→TIMEOUT, ENOMEM→OUT_OF_MEMORY, 其他→SYSTEM_ERROR
std.io.println("type: ", std.string.string_create(std.error.error_type_to_string(std.error.error_get_type(e))))
std.io.println("code: ", std.error.error_get_code(e))
string em = std.string.string_create(std.error.error_get_message(e))
std.io.println("msg: ", em)
std.io.println("strerror: ", std.string.string_create(std.error.error_strerror(2)))
```

---

## 5. 可捕获 Panic（推荐用法：panic_protect）

Kaula **不支持**裸 `try/catch` 语法。标准库提供 **回调包裹** 模式：

```kaula
fn risky_body(ctx: void()) {
    # 这里可以包含任意可能 panic 的代码
    std.panic.panic("deliberate panic!")
    # 或者 unwrap 触发：
    Result bad = std.option.result_err(3, "unwrap me")
    std.io.println(std.option.result_unwrap_ok_i64(&bad))   # 会 panic (code=2)
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
| **panic_protect** | 唯一推荐入口。setjmp 帧在回调执行期间**始终在栈上**，longjmp 回跳合法（Windows x64 也安全） |
| **panic / panic_with_code / panicf** | 抛出 panic。在 `panic_protect` 内会跳转到最近的保护帧；块外会打印消息并 abort |
| **panic_message / panic_last_code** | 仅在捕获分支有效 |
| **unwrap 可捕获** | `option_unwrap_*`、`result_unwrap_ok_*` 失败时抛出可捕获 panic（code=1 或 2） |

### 低级接口（仅 C 宏/手写用）

```kaula
PanicFrame f
if std.panic.panic_try(&f) == 0 {
    risky()
    std.panic.panic_leave(&f)
} else {
    println(std.panic.panic_message())
    std.panic.panic_leave(&f)
}
```
> `panic_try/panic_leave` 的 setjmp 注册在 `panic_try` 自身帧上，**函数返回后不可 longjmp**（Windows 会 failfast），仅适合 C 宏封装。Kaula 侧请统一用 `panic_protect`。

---

## 6. 常见陷阱

1. **const char* 打印**：`error_get_message`、`panic_message` 等返回 `const char*`，直接传给 `println` 会打印指针地址。须先转 `string`：
   ```kaula
   string s = std.string.string_create(std.panic.panic_message())
   std.io.println(s)
   ```

2. **Result string 载荷销毁**：`result_ok_string` 分配的字符串归 Result 所有，必须调用 `result_destroy` 释放。

3. **panic_protect 回调参数**：回调签名 `fn(ctx: void())`，第三个参数传 `null` 或上下文指针。

4. **多层嵌套**：`panic_protect` 支持嵌套，内层未捕获的 panic 会冒泡到外层。

---

## 7. 完整示例

见仓库 `test/error_handling.kl`。