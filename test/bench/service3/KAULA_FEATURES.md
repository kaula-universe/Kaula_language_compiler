# Kaula 现代特性清单 × 服务代码精简评估

> 目的：回答「Kaula 支持很多现代特性，说不定可以减少服务代码？你列出来我看看都有啥」，
> 以及后续的「泛型 / ADT / 空安全 / 错误传播 / 完整 OOP 都能用吗」。
>
> 方法：**全部实机验证**。每个条目用 `kaulac` 真编译 + 真运行。
>
> ⚠️ **第二轮修订说明（重要）**
> 本文第一版把若干「我自己写错了语法」的用例当成了编译器缺陷。第二版重做：
> - 逐条对齐 **官方文档 `docs/tutorials/`** 与 **117 份语料库样例**确认正确语法；
> - 每条判据都在**两份编译器**上跑：真基线（commit `9a334e2`，独立 git worktree 编译）
>   与修复后的编译器；
> - 上一版的误判在 **§3.3 更正清单**中逐条列出并给出正确写法。
>
> 结论：**修正后的可用面比第一版报告的要宽**。真正损坏的东西更少，但也确实
> 新发现了一组泛型/ADT 层面的实质缺陷（已修复，见 §3.2）。

---

## 0. 一句话结论

- Kaula 的**现代特性面确实很宽**：67 个 stdlib 模块、属性系统、comptime、闭包、match、
  derive、泛型、ADT、可空类型、`:=` 错误传播、class/interface、`conc fn` 并发原语。
- **正确语法下**，泛型结构体、**泛型 ADT 的构造与解构**、OOP class/interface、`#[derive]`、
  `:=` 错误传播在**修复前的基线上就已经能用**。第一版把这些判为"不可用"是错的。
- **真正损坏的部分**（已在本轮修复）：
  - 3 个**静默错误**的字面量缺陷（编译通过、零警告，只给错答案）——§3.1；
  - 5 个**泛型 / ADT 层面**的缺陷，让 `docs/tutorials/generics.md` 里的官方写法编不过——§3.2。
- **真正不支持、不要依赖**：三元 `?:`、C 风格 `switch`（二者文档均未承诺）、
  可空类型 `T?` 赋非空字面量、`int`/`float` 类型名（文档笔误，应用 `i32`/`f64`）。
- 就"减少服务代码"而言，**最大的一块收益已确认可行**：用 `std.net` + `std.string` +
  `std.format` 可以删掉手写的 `svc_wrap.c`（C 层 socket 封装），
  纯 kl 语言层的并发 TCP echo 服务已实机跑通（见 §4.1）。

---

## 1. 实测可用的特性（可放心用）

### 1.1 类型与数据结构

| 特性 | 语法示例 | 实测结果 |
|---|---|---|
| 结构体 | `struct P { x: i32, y: i32 }` | ✅ |
| 嵌套结构体 | `struct I { v: i32 }` / `struct O { i: I }` | ✅ |
| 结构体作为返回值 | `fn mk() -> P { return P { x = 1 } }` | ✅ |
| 枚举（C 风格） | `enum Color { Red, Green, Blue }` → `Color.Red` | ✅ |
| 多变量声明 | `i64 a = 1, b = 2` | ✅ |
| 前缀指针类型 | `*u8` / `u8*` | ✅ |
| 类型转换 | `as<i64>(x)` / `as<u32>(x)` | ✅ |
| 泛型结构体 | `Box<i32> b = { .value = 42 }` → `42` | ✅（**基线即可用**，见 §3.3） |
| 泛型函数 | `fn identity<T>(T x) T` + `identity<i32>(11)` → `11` | ✅ |
| 泛型 ADT 构造与解构 | `Option<i32> o = Option.Some(42)` + `match o { Some(v) => ... }` → `42` | ✅（基线即可用） |

> 注：结构化「方法」在 Kaula 里**不是** `impl`/`self` 语法，而是 `class` 的成员方法
> （§2 有官方写法）。struct **不支持**方法、构造器、`implements`——这是设计，不是缺陷。

### 1.2 控制流

| 特性 | 语法示例 | 实测结果 |
|---|---|---|
| `match` 表达式 | `i32 r = match k { 1 => 10, _ => 0 }` | ✅ |
| match 变体解构（含泛型 ADT） | `match o { Some(v) => ..., None => ... }` | ✅ |
| `for ... in range(N)` | `for i in range(3)` | ✅ |
| `range(a, b)` / `range(a, b, step)` 负步长 | — | ✅ |
| `while` / `break` / `continue` / `if / else` | — | ✅ |

> `std.io.println` **不做 `%` 格式化替换**，要用 `std.io.print().int(x)`。
> `println` 可直接打印多个参数（`println("x = ", x)`）。

### 1.3 并发（本基准的核心）

| 特性 | 语法示例 | 实测结果 |
|---|---|---|
| `conc fn` 挂起函数 | `conc fn conn_task(fd: i32) -> i32 {...}` | ✅ |
| `spawn` 创建任务 | `spawn conn_task(cfd)` | ✅ |
| `Handle` + `await` | `Handle h = spawn f(); h.await` | ✅ |
| 跨挂起点的 IO（epoll） | `net_recv` 等挂起点 | ✅ |
| 定时器挂起 | `std.time.time_sleep_us(200)` | ✅ |

### 1.4 标准库（实机验证的关键模块）

| 模块 | 关键函数 | 实测结果 | 服务代码价值 |
|---|---|---|---|
| `std.net` | `tcp_server_create` / `socket_accept` / `tcp_receive` / `tcp_send` / `socket_close` | ✅ **真起监听、真 accept、真收发** | ⭐⭐⭐ 可删整个 `svc_wrap.c` |
| `std.string` | `string_create` / `string_create_from_int` / `string_concat` / `string_length` | ✅ 拼接结果正确 | ⭐⭐ 可替手写字符串拼接 |
| `std.format` | `format_int(buf, size, value, base)` | ✅ dec/hex 均可用 | ⭐⭐ 可替 `itoa`/`itohex8` |
| `std.hash` | `crc64_compute(data, len)` | ✅ | ⭐ 可替部分校验逻辑 |
| `std.memory` | `kmm_v4_alloc` / `kmm_v4_free` | ✅ | 已在用 |
| `std.time` | `time_sleep_us` | ✅ | 已在用 |
| `std.option` | `option_some_i64` / `result_ok_i64` / `result_err` / `result_err_code` | ✅ 模块化 Option/Result | ⭐⭐ 错误体系 |
| `std.error` / `std.panic` | `panic_protect` 可捕获 panic | ✅ | ⭐⭐ |

### 1.5 属性系统

| 特性 | 语法 | 实测结果 |
|---|---|---|
| `#[test]` | 标记测试函数，`kaulac unittest` 自动发现 | ✅ |
| `#[derive(Debug, Clone, Eq)]` | 官方逐字段赋值写法 | ✅ **从未损坏**（见 §3.3） |
| `#[packed]` / `#[aligned]` / `#[inline]` / `#[naked]` / `#[no_kmm]` / `#[volatile]` | — | ✅ 声明可用 |
| `#[command]` 命令行解析 | `#[arg(short="v", long="verbose")]` | ✅ |
| comptime 内省 | `sizeof(T)` / `offsetof(T,f)` / `field_count(T)` / `type_name(T)` | ✅ |

### 1.6 闭包

```kaula
auto add = fn(i64 x) -> i64 { return x + 1 }
add(41)   // → 42  ✅ 实测正确
```

---

## 2. 泛型 / ADT / 空安全 / 错误传播 / OOP 实测

文档 `docs/tutorials/generics.md`、`match.md`、`pointers.md`、`error-and-panic.md`、
`class-and-interface.md` 明确承诺了这些特性。逐条实测，**双编译器对照**：

| 特性 | 官方语法（出处） | 真基线 `9a334e2` | 修复后 | 说明 |
|---|---|---|---|---|
| 泛型函数 | `fn identity<T>(T x) T`（generics.md） | ✅ `11` | ✅ `11` | 一直可用 |
| 泛型 struct | `Box<i32> b = { .value = 42 }`（generics.md） | ✅ `42` | ✅ `42` | **一直可用**，第一版误判见 §3.3 |
| 泛型 struct 无点字面量 | `Box<i32> { value = 42 }` | ❌ 全零 | ✅ | 非官方写法；修的是"静默丢弃"，见 §3.1 |
| 泛型 **class** | `class Box<T>` + `Box<i32> b = Box<i32>(42)`（generics.md） | ❌ `K_Box_i32` redefinition | ✅ `42` | **真缺陷**，已修 §3.2 |
| 泛型 class 方法调用 | `b.get()` | ❌ 非法 C 标识符 `Box<i32>_get` | ✅ `Box_i32_get(b)` | 同上 |
| 泛型 class + 外层返回值 | `fn main() -> i32 { Box<i32> b = ...; return 0 }` | ❌ `return 0` 被吞 | ✅ | 同上 |
| 泛型 enum 构造 | `Option<i32> o = Option.Some(42)` | ✅ `42` | ✅ `42` | **一直可用**，第一版误判 |
| 泛型 enum match 解构 | `match o { Some(v) => ... }` | ✅ `42` | ✅ `42` | 同上 |
| 泛型 enum 负载**参与算术** | `out = v + 1` | ❌ `type_mismatch` | ✅ `43` | **真缺陷**，已修 §3.2 |
| `Option<T>` 作返回类型 | `fn may(i32 x) Option<i32>` | ⚠️ 声明可用，负载算术报错 | ✅ `10` | 同一缺陷的下游表现 |
| 泛型 Result + `:=` 传播 | `v := fallible(n)`，`fallible` 返回 `Res<i32,i32>` | ❌ `try_assign_non_enum` | ✅ `13` | **真缺陷**，已修 §3.2 |
| 非泛型 Result + `:=` 传播 | `v := might_fail(true)`（test/try_assign.kl） | ✅ Ok→`9` / Err→`1` | ✅ 同 | **一直可用**，第一版误判 |
| 空安全 `T?` 声明 + `== null` | `i32? maybe = null`（pointers.md） | ✅ 打印 `is null` | ✅ 同 | 可空类型存在且静态可查 |
| 空安全赋非空字面量 | `int? value = 42`（pointers.md） | ❌ `int32_t* v = 42` 类型不符 | ❌ 同 | **仍受限**：可空类型实现为裸指针，不能直接吃标量字面量 |
| OOP `class` 方法 + `self` | `constructor(f64 px, f64 py)` + `fn scale(f64 f)`（class-and-interface.md） | ✅ | ✅ | 一直可用 |
| `interface` + `implements` | `class Circle implements Drawable` | ✅ `drawing circle` | ✅ | 一直可用 |
| `?` **作为错误传播算子** | — | 不存在 | 不存在 | **我的误判**：`?` 是可空类型标记，Kaula 的错误传播是 `:=`（error-and-panic.md:76 明写） |
| 三元 `?:` | — | ❌ 语法错误 | ❌ | 文档未承诺；用 `if/else` 或 `match` |
| C 风格 `switch` | — | ❌ | ❌ | 文档未承诺；用 `match` |

**服务代码可放心依赖**：泛型函数、泛型 struct、泛型 ADT、泛型 class/Result + `:=`、
OOP class/interface、`match`、可空类型声明、`#[derive]`。
**不要依赖**：三元 `?:`、C 风格 `switch`、可空类型吃字面量、文档里的 `int`/`float`/`String`/`Socket` 类型名。

---

## 3. 本轮修复的缺陷

### 3.1 三个「静默错误」字面量缺陷（编译通过、零警告，只给错答案）

| # | 缺陷 | 修复前 | 修复后 | 根因 |
|---|---|---|---|---|
| 1 | `[]i32 arr = [1,3,1990]` 求和 | **4**（应 1994） | `1994` ✅ | `generateArrayLiteral` 硬编码 `(int64_t[]){...}`，绑到 `int32_t*` 步长错位 |
| 2 | `i32 arr[5] = {10,20,30,40,50}` | 数组**全为 0** | `{10,20,30,40,50}` ✅ | `{...}` 被 parser 无条件当 struct literal，裸值丢弃 |
| 3 | `Point p = Point { x = 10, y = 20 }` | 字段**全为 0** | `10, 20` ✅ | `IDENT` + `ASSIGN` 分支缺失，字段被丢弃 |

> **缺陷 2 有官方文档背书**：`docs/tutorials/control-flow.md:76`「迭代数组/切片」一节原样写着
> `int[5] arr = {10, 20, 30, 40, 50}` + `for x in arr`。
> （顺带：该处类型名 `int` 是文档笔误，语言里没有 `int`。）
>
> **缺陷 3 需诚实标注**：无点写法**没有**出现在任何官方文档/样例里（官方只有
> `{ .field = value }` 与逐字段赋值）。但 parser 遇到它时不报错、直接吞掉整个初始化列表，
> 产出全零对象——这才是缺陷。修完后两种写法都合法，不再有"静默丢弃"路径。

另外把 `for v in arr`（`[]T` 切片）原先 clang 深处报的
`member reference base type 'int64_t *'` 改成了清晰的编译期诊断。

### 3.2 五个泛型 / ADT 缺陷

`docs/tutorials/generics.md` 里三个例子（`Option<T>`、`Result<T,E>`、`class Box<T>`）
原先都编不过。根因与修复：

| # | 症状 | 根因 | 修复 |
|---|---|---|---|
| A | 泛型 ADT 负载参与算术报 `type_mismatch` | `analyzeMatchExpression` 用 `FindEnum("Option<i32>")` 查声明，而 `FindEnum` 只认基名 `Option`；即便找到，载荷类型还是类型参数名 `T` | 新增 `resolveEnumTarget()`：剥离 `<...>` + 回填类型实参，按 `TypeParams` 做替换 |
| B | `:=` 作用于泛型 Result 报 `try_assign_non_enum` | 同一处查找缺陷（`Line ~1725`） | 改用 `resolveEnumTarget()`，并对 Ok 载荷做实参替换 |
| C | 同一枚举的 `typedef` 被输出几十次 → `redefinition of 'K_Box_i32'` | `InstantiateGenericType` 的 `genericTypeCache` 在生成**之后**才写回，处理字段/方法时的嵌套请求查不到缓存 → 无限重入 | 在生成**之前**登记缓存（环保护） |
| D | 泛型 class 变量按值生成：`K_Box_i32 b = ...` | 实例化名没进 `classTypes`，引用语义判定失效 | 登记 `classTypes[Box_i32]` 与用户书写形态 `classTypes[Box<i32>]` |
| E | 方法调用生成非法标识符 `Box<i32>_get(...)` | 方法前缀直接用了未改写的 `sym.Type` | 方法调用处按 `<...>` 实参改写前缀 |
| F | 泛型 class 之后 `return 0` 被吞成裸 `return` | 构造器生成会把 `currentFunctionReturnType` 复位为 `""`，污染外层函数 | 在 `InstantiateGenericType` 里保存/恢复该状态 |

穷尽性检查也顺带修好：原先对 `Option<i32>` 查不到枚举声明，会对穷尽的 match 误报
「缺少 `_` 通配符分支」。

### 3.3 更正清单：我此前误判的项目

| 第一版的结论 | 实际情况 | 正确写法 |
|---|---|---|
| "`#[derive(Debug)]` 有静默缺陷" | ❌ **从未损坏**。基线就输出 `Point { x: 10, y: 20 }` | `Point p; p.x = 10; p.y = 20; Point_debug(&p)` |
| "泛型结构体字面量不可用" | ❌ 一直可用 | `Box<i32> b = { .value = 42 }`（带点） |
| "ADT 构造 / 解构不可用" | ❌ 一直可用 | `Option<i32> o = Option.Some(42)` + `match o { Some(v) => ... }` |
| "`Option<T>` 不能作返回类型" | ❌ 一直可用 | `fn may(i32 x) Option<i32>` |
| "OOP 泛型 class 不可用（`K_Box_i32` 重定义）" | ⚠️ **这条缺陷是真的**，见 §3.2 C/D/E/F | 已修复 |
| "错误传播 `?` 不可用" | ❌ **语法前提就错了**：`?` 是可空标记，错误传播是 `:=` | `v := fallible(n)`（左侧**不写类型**） |
| "Result<T,string> 的 Err 绑定失败" | ❌ 一直可用 | `Result<i32,string> r = Result.Err("boom")` + `match` |

### 3.4 其它语法差异（不算缺陷，但易踩）

| 项 | 结果 |
|---|---|
| `int` / `float` 类型名 | ❌ 不存在；用 `i32` / `f64`（**多份官方文档写错**） |
| `String` 大写类型名 | ❌ 未定义；用小写 `string` |
| `Socket*` 类型名 | ❌ 未定义；用 `*u8` 承接（丢类型安全） |
| `println("%d", x)` | ⚠️ `%d` 不被替换，原样打印；用 `std.io.print().int(x)` |
| 数组类型声明 | `i32 name[5]` 或 `[5]i32 name`，**不是** `int[5] name` |
| 定长数组初始化 | 官方样例用方括号 `i32 arr[10] = [0, 1, 2, ...]`；花括号形式现已支持 |

---

## 4. 面向"减少服务代码"的评估

现有 `kaula_kl/order_server.kl` 共 177 行，其中可被 stdlib 替代的手写部分：

| 现有手写代码 | 行数 | 可否被 stdlib 替代 | 替代方案 |
|---|---|---|---|
| `itoa` 整数转字符串 | 13 行 | ✅ 可 | `std.format.format_int(buf, size, v, 10)` |
| `itohex8` 十六进制 | 15 行 | ✅ 可 | `std.format.format_int(buf, size, v, 16)` |
| `parse_field_at` 扫描解析 | 14 行 | ⚠️ 部分 | `std.string.string_split` 可试，但需 `string` 层 |
| `checksum256_at` | 9 行 | ❌ 不可 | 基准刻意与 Go/Rust 逐位等价，**必须保留** |
| `svc_wrap.c`（C 层 socket 封装） | 整个文件 | ✅ 可 | `std.net`（已实机验证跑通 echo） |

**新增可用手段（本轮修好后才成立）**：错误处理可以用
`enum Result<T,E>` + `:=` 传播改写，`parse_field_at` 不再需要手写错误码层层返回——
这是比替换 stdlib 函数更大的一块结构简化。

### 4.1 已验证的替代：纯 kl 语言层并发 TCP 服务

```kaula
import std.io, std.net, std.string, std.memory

conc fn client_task(c: *u8) -> i32 {
    u8* buf = std.memory.kmm_v4_alloc(4096)
    for rnd in range(3) {
        i64 n = std.net.tcp_receive(c, buf, 4096)
        if (n <= 0) { break }
        std.net.tcp_send(c, buf, as<u64>(n))
    }
    std.net.socket_close(c)
    return 0
}

fn main() -> i32 {
    string host = std.string.string_create("0.0.0.0")
    *u8 s = std.net.tcp_server_create(host, 8201)
    if (s == null) { return 1 }
    while (true) {
        *u8 c = std.net.socket_accept(s)
        if (c != null) { spawn client_task(c) }
    }
    return 0
}
```

**实机验证结果**：进程真在 `0.0.0.0:8201` 监听（`ss -ltnp` 可见），
外部连接发送 `hello-kaula\n`，**原样收到回包**。

### 4.2 减代码的量化预期

- **删掉 `svc_wrap.c` 整块 C 封装**：服务侧的 C 依赖降到 0（除 runtime 外）。
- **删掉 `itoa` + `itohex8`（28 行）**：换成 3～4 行 `format_int` 调用。
- **错误处理链路**：`Result<T,E>` + `:=` 可替掉逐层判空返回。
- 净收益：**服务代码从 ~177 行降到 ~110 行左右**，且去掉一个"语言层 + C 层"双份维护面。
- 代价与前置条件：
  1. `std.net` 的 `tcp_receive`/`tcp_send` 是**通用 socket 调用**，需确认是否与 CONC 的
     `IoDriver`（epoll）挂起点语义一致——**能否在 `conc fn` 里真挂起、而非阻塞线程**，
     尚未验证；若会阻塞，抗干扰曲线会变差，简化反而得不偿失。
  2. `Socket*` 无类型名，只能用 `*u8` 承接，**类型安全丢失**。

---

## 5. 建议

1. ~~修缺陷~~ —— **已完成**。见 §3.1（3 个静默错误）+ §3.2（6 处泛型/ADT 修复）。
2. **再做服务精简**：把 `svc_wrap.c` 换成 `std.net`、`itoa`/`itohex8` 换成 `format_int`、
   错误返回改用 `Result<T,E>` + `:=`。**前置条件**：先验证 `std.net` 在 `conc fn` 中
   是否走真正的 epoll 挂起。
3. **已可靠用的现代特性**（双编译器核过）：泛型函数 / 泛型 struct / 泛型 ADT /
   泛型 class / `:=` 错误传播 / OOP class+interface / 可空类型 / `#[derive]` / `match`。
4. **不要依赖**：三元 `?:`、C 风格 `switch`、可空类型吃标量字面量，以及文档里出现的
   `int`/`float`/`String`/`Socket` 类型名（这些都行不通）。
5. **文档修 bug**（可选）：`docs/tutorials/` 多处把 `int` 写成类型名，`control-flow.md`
   的 `int[5] arr = {...}` 应为 `i32 arr[5] = [...]`。

---

## 6. 验证方法与结果

**方法**：每条判据在两份编译器上各跑一次
（`build/bin/kaulac_true_base` = commit `9a334e2` 的独立 git worktree；
`build/bin/kaulac` = 修复后）。每次都 `rm -rf cache` 并删除旧产物，
避免陈旧二进制污染结论（第一轮曾栽在这上面）。

| 项 | 结果 |
|---|---|
| `go build ./...` + `go vet ./...` | ✅ 干净 |
| `go test ./...` | ✅ 全绿（14 个含测试包） |
| `scripts/run_tests.py` | **104/106**（余 2 个 `stdlib_async_safety` 为**预存失败**，真基线同样 exit -11） |
| 官方 `docs/tutorials/examples/*.kl` | ✅ 10/10 编译通过 |
| 回归单测守护力 | 8 个测试在**真基线上全部 FAIL**，修复后全部 PASS |
| Kaula 服务端到端 | 4 条请求与基线响应**逐字节一致**（如 `O,7,SKU-0001,105,57` → `R,7,e9da68a0,5985`） |

> 探测用例与对比脚本在 `/tmp/feat4/`（`run2.sh` 做双编译器对照），需要时可复现。
