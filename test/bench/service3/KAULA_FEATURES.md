# Kaula 现代特性清单 × 服务代码精简评估

> 目的：回答「Kaula 支持很多现代特性，说不定可以减少服务代码？你列出来我看看都有啥」。
>
> 方法：**全部实机验证**。每个条目用 `kaulac` 真编译 + 真运行，不是照抄文档。
> 探测脚本位于 `/tmp/feat2/`（`n*.kl` 网络、`s*.kl` 字符串、`a*.kl` 数组、
> `m*/c*` 控制流与类型、`h*/f*` 哈希/格式化、`d*/t*` 属性）。
>
> ⚠️ 重要提醒：**文档与实测存在偏差**。本文以实测为准，凡实测与文档冲突处均已标注。

---

## 0. 一句话结论

- Kaula 的**现代特性面确实很宽**（67 个 stdlib 模块、属性系统、comptime、闭包、match、derive、泛型、OOP、并发原语）。
- **"文档宣称有" ≠ "现在能用"**：实测发现 **5 个静默错误缺陷**（编译通过但结果错），
  其中 `[]i32` 数组字面量、定长数组初始化、struct 字面量无点赋值最危险——它们不报错，只给错数。
- **这 5 个缺陷已全部修复**（见 §5），并补了单元 + 端到端回归测试。
- 就"减少服务代码"而言，**最大的一块收益已确认可行**：用 `std.net` + `std.string` +
  `std.format` 可以把服务里手写的 `svc_wrap.c`（C 层 socket 封装）**整块删掉**，
  纯 kl 语言层的并发 TCP echo 服务已实机跑通（见 §4.1）。
- 泛型 / ADT / 空安全 / 错误传播 / OOP 的实测结论见 **§2**：OOP 与泛型函数可用，
  但泛型结构体字面量、ADT 构造、`?` 错误传播、三元表达式**当前不可用**。

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

> 注：结构化「方法」在 Kaula 里**不是** `impl`/`self` 语法，而是**自由函数**
> `fn area(Rectangle r) int`——这是设计，不是缺陷。

### 1.2 控制流

| 特性 | 语法示例 | 实测结果 |
|---|---|---|
| `match` 表达式 | `i32 r = match k { 1 => 10, 2 => 20, _ => 0 }` | ✅ 输出 20 正确 |
| `for ... in range(N)` | `for i in range(3)` | ✅ |
| `range(a, b)` | `for i in range(1, 4)` | ✅ |
| `range(a, b, step)` 负步长 | `for k in range(10, 0, -5)` | ✅ |
| `while` / `break` / `continue` | — | ✅ |
| `if / else` | — | ✅ |

> **纠正之前的误判**：早期探测曾认为 `match` 生成非法 C（`return switch (k) {`），
> 实为探测脚本自身写错。修正后 `match` 在表达式位置**完全正常**。
> 唯一注意点：`std.io.println` **不做 `%` 格式化替换**，要用 `print_int`/`print_char`。

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

### 1.5 属性系统

| 特性 | 语法 | 实测结果 |
|---|---|---|
| `#[test]` | 标记测试函数，`kaulac unittest` 自动发现 | ✅ 编译通过 |
| `#[derive(...)]` | `#[derive(Debug, Clone, Eq)]` | ⚠️ 见 §2.4（**有静默缺陷**） |
| `#[packed]` / `#[aligned]` / `#[inline]` / `#[naked]` / `#[no_kmm]` / `#[volatile]` | — | ✅ 声明可用 |
| comptime 内省 | `sizeof(T)` / `offsetof(T,f)` / `field_count(T)` / `type_name(T)` | ✅ |

### 1.6 闭包

```kaula
auto add = fn(i64 x) -> i64 { return x + 1 }
add(41)   // → 42  ✅ 实测正确
```

---

## 2. 泛型 / ADT / 空安全 / 错误传播 / OOP 实测

文档 `docs/tutorials/` 里明确承诺了这些现代特性。逐条实测结论：

| 特性 | 文档 | 实测 | 说明 |
|---|---|---|---|
| 泛型函数 | ✅ | ✅ **可用** | `fn identity<T>(T x) T` + `identity<i32>(42)` → 42 |
| 泛型 class | ✅ | ❌ 生成重复 typedef | `K_Box_i32` redefinition，clang 拒绝 |
| 泛型 struct 字面量 | ✅ | ❌ | `Box<i32> { value = 42 }` 解析成比较表达式 |
| 泛型 enum（ADT） | ✅ | ⚠️ 部分 | `enum Result<T,E>` 声明+`Result.Ok(42)` 构造可编译；带类型参数的变体解构 `match r { Ok(x) => x }` 报 type_mismatch |
| OOP `class` / `interface` | ✅ | ✅ **可用** | `Vec2(3.0,4.0)` + 方法 `scale(2.0)` → x=6.0 正确 |
| 空安全 `Option<T>` | ✅ | ⚠️ 仅模块 API | 语言级 `Option<T>` 类型名不可用；`std.option.*` 模块函数可用（`option_some_i64` 等需传指针 `&o`） |
| 错误传播 `?` | ✅ | ❌ | `may(1)?` 语法错误 |
| 三元 `?:` | — | ❌ | 语法错误 |
| C 风格 `switch` | — | ❌ | 用 `match` 代替 |

**服务代码可用的部分**：泛型函数、OOP class/interface、`match`。
**当前不可用、不要依赖**：泛型结构体/类、ADT 解构、`?`、三元、裸 `Option<T>`。

---

## 3. 已修复的 5 个静默错误缺陷（本轮）

> 全部为**编译通过、零警告、只给错答案**。共同根因都在 parser/AST 层：
> 声明的**字面量形状没被识别，值被静默丢弃**。修复见 §5。

| # | 缺陷 | 修复前 | 修复后 | 根因 |
|---|---|---|---|---|
| 1 | `[]i32 arr = [1,3,1990]` | 算出 **4**（应 1994） | **1994** ✅ | `generateArrayLiteral` 硬编码 `(int64_t[]){...}`，绑到 `int32_t*` 步长错位 |
| 2 | `i32 arr[5] = {10,...}` | 数组**全为 0** | **{10,20,30,40,50}** ✅ | `{...}` 被 parser 无条件当 struct literal，裸值被丢弃 |
| 3 | `Point p = Point{ x = 10 }` | 字段**全为 0** | **10, 20** ✅ | `x = 10`（无点）不被识别，字段被丢弃 |
| 4 | `#[derive(Debug)]` | 打印 `x: 0, y: 0` | `x: 10, y: 20` ✅ | 依赖缺陷 3（读的是全零对象） |
| 5 | `for v in arr`（`[]T`） | 生成 `.len` → clang 报错 | **清晰编译期错误** ✅ | `[]T` 的 C 侧是裸指针，运行期不带长度 |

修复的**可观测收益**：
- `i32 arr[5] = {10,20,30,40,50}` + 迭代求和：**0 → 150**。
- `[]i32` 字面量取值：**4 → 1994**。
- struct 字面量成为真正的可用特性（此前等于摆设）。

### 3.1 其它语法差异（不算缺陷，但易踩）

| 项 | 结果 |
|---|---|
| `String` 大写类型名 | ❌ 未定义；要用小写 `string` |
| `Socket*` 类型名 | ❌ 未定义；要用 `*u8` 接收（类型不安全但可行） |
| `format_printf("%08x", ...)` 变参 | ❌ 参数不匹配；要用 `format_int(buf,size,v,base)` |
| `println("%d", x)` | ⚠️ `%d` 不被替换，原样打印；要用 `print_int` |
| 数组类型声明 | 是 `i32 name[5]` 或 `[5]i32 name`，**不是** `int[5] name`（文档有误） |

> 附带发现：`basicTypes`（`compiler/internal/sema/sema.go:1815`）只注册了标量 + `string` + `File`
> + `object` + `Handle`，**没有** `String`/`Socket` 等 stdlib 结构体类型。
> 这是"stdlib 是 C 层 API、未向语言层导出类型"的根因。

---

---

## 4. 面向"减少服务代码"的评估

现有 `kaula_kl/order_server.kl` 共 177 行，其中可被 stdlib 替代的手写部分：

| 现有手写代码 | 行数 | 可否被 stdlib 替代 | 替代方案 |
|---|---|---|---|
| `itoa` 整数转字符串 | 13 行 | ✅ 可 | `std.format.format_int(buf, size, v, 10)` |
| `itohex8` 十六进制 | 15 行 | ✅ 可 | `std.format.format_int(buf, size, v, 16)` |
| `parse_field_at` 扫描解析 | 14 行 | ⚠️ 部分 | `std.string.string_split` 可试，但需要 `string` 层 |
| `checksum256_at` | 9 行 | ❌ 不可 | 基准刻意与 Go/Rust 逐位等价，**必须保留** |
| `svc_wrap.c`（C 层 socket 封装 + `kaula_svc_listen`/`accept`/`send`/`kaula_tls_io_result`/`kaula_svc_at`） | 整个文件 | ✅ 可 | `std.net`（已实机验证跑通 echo） |

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
外部连接发送 `hello-kaula\n`，**原样收到回包**。`std.net` 在纯 kl 语言层完全可用。

### 4.2 减代码的量化预期

- **删掉 `svc_wrap.c` 整块 C 封装**：服务侧的 C 依赖降到 0（除 runtime 外）。
- **删掉 `itoa` + `itohex8`（28 行）**：换成 3～4 行 `format_int` 调用。
- 净收益：**服务代码从 ~177 行降到 ~120 行左右**，且去掉一个"语言层 + C 层"双份维护面。
- 但要注意两点代价：
  1. `std.net` 的 `tcp_receive`/`tcp_send` 是**通用 socket 调用**，需确认是否与
     CONC 的 `IoDriver`（epoll）挂起点语义一致——**能否在 `conc fn` 里真挂起、而非阻塞线程**，
     这一点尚未验证；若会阻塞，抗干扰曲线会变差。
  2. `Socket*` 无类型名，只能用 `*u8` 承接，**类型安全丢失**。

---

## 5. 建议

1. ~~先修 4 个静默错误缺陷~~ —— **已完成**（实际是 5 个，见 §3）。修复后：
   `go test ./...` 全绿；`scripts/run_tests.py` **104/106**（余下 2 个
   `stdlib_async_safety` 为**预存失败**，用修复前的基线编译器复现同样 -11，
   与本次改动无关）；新增单元测试 4 个 + 端到端 `test/literal_semantics.kl`，
   二者在未修复时**全部 FAIL**，守护力已验证。
2. **再做服务精简**：把 `svc_wrap.c` 换成 `std.net`、把 `itoa`/`itohex8` 换成 `format_int`。
   但**前置条件**是先验证 `std.net` 在 `conc fn` 中是否走真正的 epoll 挂起——
   否则精简了代码却毁了抗干扰指标，得不偿失。
3. **不要依赖尚不可用的现代特性**：泛型结构体/类、ADT 解构、`?` 错误传播、三元表达式
   （§2 有完整实测表）。服务代码只用 §1 里已验证可用的那部分。
4. `std.json` / `std.web` / `std.hash` 系列可作为下一步能力储备，本次未做端到端压测验证。

> 本文全部结论来自实测。探测脚本在 `/tmp/feat2/`、`/tmp/feat3/`，需要时可复现。
