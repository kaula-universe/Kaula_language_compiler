# 属性注解与编译期反射

## 属性注解

使用 `#[name]` 语法为声明或表达式添加注解。

### 声明级属性

```kaula
#[packed]                         # 紧凑内存布局（无填充）
struct PacketHeader {
    u8 version,
    u8 flags,
    u16 length
}

#[aligned(16)]                    # 16 字节对齐
struct AlignedBuf {
    data: [64]u8
}

#[section(".isr_vector")]         # 链接到指定段
#[naked]                          # 裸函数（无 prologue/epilogue）
fn vector_handler() void {
    // raw asm body
}

#[inline]                         # 内联提示
fn fast_path() int { return 42 }

#[deprecated]                     # 标记弃用
fn old_api() { }

#[weak]                           # 弱符号
fn fallback() int { return -1 }

#[no_kmm]                         # 禁用 KMM 内存管理（回退到系统 malloc）

#[volatile]                       # volatile 语义
u32* status_reg = 0x40000000
```

### #[no_kmm] 详解

`#[no_kmm]` 属性在特定函数中禁用 KMM V4 内存管理：

- 函数内不插入 `kmm_v4_scope_push()`/`kmm_v4_scope_pop()`
- `std_malloc` 不会被重写为 `kmm_v4_alloc_auto`，保持系统 malloc 调用
- 适用于需要手动管理内存生命周期的场景（如 C 库互操作）

```kaula
#[no_kmm]
fn manual_memory() void* {
    // 使用系统 malloc，不受 KMM 作用域管理
    auto ptr = std.memory.std_malloc(1024)
    // ... 需要手动 std_free ...
    return ptr
}

fn kmm_managed() {
    // 默认 KMM 管理，函数退出自动回收
    auto ptr = std.memory.std_malloc(1024)
    // 无需 free
}
```

### 表达式级属性

```kaula
auto cr3 = #[asm("mov %cr3, %rax")]     # 内联汇编
#[fence()]                                # 内存屏障
```

## 编译期反射

反射内建全部在**编译期**求值，结果直接固化成常量写进生成的 C 代码，运行时零开销。
它们都是**表达式**，可以直接用在赋值、`println`、数组下标等任何表达式位置。

### 类型度量：`sizeof` / `alignof` / `offsetof`

```kaula
struct Packet {
    header: i64
    length: i64
    payload: i64
}

fn main() {
    auto sz = sizeof(Packet)             # 类型大小
    auto al = alignof(Packet)            # 类型对齐
    auto off = offsetof(Packet, length)  # 字段偏移（字节）
    println(sz, al, off)                 # 24 8 8（多参数以空格分隔）
}
```

三者返回 64 位无符号值，分别映射到 C 的 `sizeof` / `_Alignof` / `offsetof`。

### 反射五件套

| 内建 | 参数 | 返回 | 说明 |
|------|------|------|------|
| `type_name(T)` | 类型名 | `string` | 类型的源码名称，如 `"Point"`、`"i32"` |
| `type_kind(T)` | 类型名 | `string` | 类型类别：`int` / `float` / `bool` / `char` / `string` / `void` / `struct`，无法归类时为 `"unknown"` |
| `field_count(T)` | 结构体名 | `u64` | 字段数量；非结构体返回 `0` |
| `field_name(T, i)` | 结构体名 + 索引 | `string` | 第 `i` 个字段名（**从 0 开始**）；越界返回空串 |
| `field_type(T, i)` | 结构体名 + 索引 | `string` | 第 `i` 个字段类型名；越界返回空串 |

```kaula
struct Point {
    x: i64
    y: i64
    tag: string
}

fn main() {
    println(type_name(Point))        # Point
    println(type_kind(Point))        # struct
    println(field_count(Point))      # 3
    println(field_name(Point, 0))    # x
    println(field_type(Point, 2))    # string

    # 内建类型同样适用
    println(type_name(i32))          # i32
    println(type_kind(i32))          # int
    println(type_kind(string))       # string
}
```

需要注意的边界行为：

- `field_*` 的索引从 **0** 开始，和 `spend` 的 `call(i)`（从 1 开始）不是一回事；
- 对未知类型调用 `type_kind` 返回 `"unknown"`，不会报错；
- 越界的 `field_name` / `field_type` 返回空字符串 `""`（运行时是 SSO 空 `String`），不触发 panic。

### 形状查询：`shape_of(expr)`

与反射五件套查**类型**不同，`shape_of` 查的是**表达式实参的语法形状**——这是展开器在 prefix 宏 tree 分派时本来就知道、但此前不对外暴露的事实。它**只能出现在 `comptime(...)` 上下文里**（直接写在普通表达式位置会报 `shape_of_outside_comptime`），编译期静态返回一个字符串：

| 实参形态 | 返回 |
|------|------|
| 整数字面量 | `"int"` |
| 浮点 / 布尔 / 字符串字面量 | `"float"` / `"bool"` / `"string"` |
| 标识符 | `"ident"` |
| 二元 / 一元 / 条件表达式 | `"binary"` / `"unary"` / `"conditional"` |
| 调用 / 下标 / 成员访问 | `"call"` / `"index"` / `"field"` |
| 括号表达式 / 结构体字面量 | `"paren"` / `"struct_literal"` |
| 其他 | `"expression"` |

```kaula
fn main(x: i32, a: i32, b: i32) {
    println(comptime(shape_of(123)))   # int
    println(comptime(shape_of(x)))     # ident
    println(comptime(shape_of(a + b)))# binary
}
```

它是「宏 tree 分派」与「comptime 值域」之间的层间桥：宏作者可以在 comptime 里读到实参形状并据此分叉，而不必让宏体本身变成可执行程序。

### 编译期表达式：`comptime(...)`

`comptime` 是一个**表达式包装**，语义是"在编译期把里面的常量表达式算出来"：

```kaula
fn main() {
    auto n = comptime(1 + 2 * 3)        # 编译期算出 7
    auto s = comptime(type_name(i32))   # 与 type_name(i32) 等价
    println(n, s)                       # 7 i32
}
```

> 注意写法：`comptime` 后面跟括号表达式，不是语句前缀。
> `comptime auto x = ...` **不是合法语法**，会报 `comptime 后应该跟表达式`。

> 多参数 `println(a, b, ...)` 在参数之间自动插入一个空格后输出，
> 例如 `println(7, "i32")` 输出 `7 i32`。它**不做** `%` 格式化替换，
> 需要格式化请用 `std.io.print()`。

## 高级属性扩展

Kaula 还提供了三个高级属性，用于自动化常见开发任务：

### `#[test]` - 自动测试

标记函数为测试函数，使用 `kaulac unittest` 命令自动发现并运行：

```kaula
#[test]
fn test_add() {
    i64 result = 2 + 3
    assert(result == 5)
}

#[test]
#[ignore]
fn test_not_ready() {
    // 暂时跳过的测试
}
```

运行：
```bash
kaulac unittest my_tests.kl
```

输出包含每个测试的通过/失败/忽略状态及汇总统计，失败时退出码非零。

> `kaulac test` 是语料库快照回归测试器，与 `#[test]` 无关，详见
> [属性扩展](../attributes.md#test-自动测试)。

### `#[derive]` - 派生宏

为结构体自动生成 Debug、Clone、Eq 方法：

```kaula
#[derive(Debug, Clone, Eq)]
struct Point {
    x: i64
    y: i64
}

fn main() {
    Point p
    p.x = 10
    p.y = 20

    Point_debug(&p)           // 输出: Point { x: 10, y: 20 }
    Point* p2 = Point_clone(&p)  // 深拷贝
    if Point_equals(&p, p2) == 1 {
        println("p == p2")
    }
}
```

### `#[command]` - 命令行参数解析

为结构体自动生成命令行参数解析函数：

```kaula
#[command]
struct Config {
    #[arg(short="v", long="verbose", help="启用详细输出")]
    verbose: bool

    #[arg(short="o", long="output", default="out.txt", help="输出文件")]
    output: string
}

fn main(i32 argc, char** argv) {
    Config* cfg = Config_parse(argc, argv)
    if cfg.verbose == 1 {
        println("详细模式")
    }
}
```

详细用法参见 [`#[]` 高级属性扩展](../attributes.md)。

## 完整示例

参见 [examples/attributes.kl](examples/attributes.kl)。
