# Kaula `#[]` 高级属性扩展

Kaula 支持通过 `#[attribute]` 语法为函数、结构体、字段等添加元数据注解。本文档介绍高级属性扩展：`#[test]`、`#[derive]`、`#[command]`。

## 目录

- [`#[test]` 自动测试](#test-自动测试)
- [`#[derive]` 派生宏](#derive-派生宏)
- [`#[command]` 命令行参数解析](#command-命令行参数解析)
- [`#[prefix]` 前缀模板函数](#prefix-前缀模板函数)
- [属性组合使用](#属性组合使用)

---

## `#[test]` 自动测试

### 概述

`#[test]` 属性标记一个函数为测试函数。使用 `kaulac unittest <file.kl>` 编译时，编译器会自动收集所有测试函数，生成测试运行器（test runner）替换掉 `main`，编译完成后自动执行。

> **注意**：`kaulac test` 与 `kaulac unittest` 是两套不同的东西，不要混淆。
> - `kaulac unittest <file.kl>` — 本节的 `#[test]` runner。
> - `kaulac test [file.kl|dir]` — 语料库**快照回归测试**：以普通模式编译每个含顶层 `fn main()` 的 `.kl`，运行后与 `<file>.kl.stdout` 比对。与 `#[test]` 属性无关。

### 基本用法

```kaula
#[test]
fn test_addition() {
    i64 result = 2 + 3
    assert(result == 5)
}
```

断言说明：`assert(cond)` 为编译器内置，无需 import。测试模式下失败会被
runner 记录（测试计为 FAILED，`kaulac unittest` 返回非零退出码）；普通模式下
失败会打印断言文本并 `exit(1)`。

### 运行测试

```bash
# 运行指定文件中的测试
kaulac unittest my_tests.kl

# 批量运行目录下所有含 #[test] 的 .kl
kaulac unittest test/
kaulac unittest              # 等价于 kaulac unittest .

# 透传编译旗标
kaulac unittest --no-cache test/
```

若目标文件里没有任何 `#[test]` 函数，编译会直接报错退出，而不是退化成
普通 `main` 并以 0 退出把「没跑任何测试」伪装成「全部通过」。

### 批量模式

指定目录（或省略参数）时进入批量模式：递归找出所有含 `#[test]` 的 `.kl`，
逐个编译运行，最后汇总。

- 目录扫描会跳过 `build/`、`cache/`、`pkglib/`、`.git/`、`node_modules/`
- 每个文件以独立子进程运行，互不干扰；任一文件有失败测试即整体退出码非零
- 单文件与批量两条路径共用同一套单文件编译逻辑，行为一致

```
Running 4 test file(s) under test/
...
===== Test Files Summary =====
Files:  4
Passed: 4
Failed: 0
==========================

All test files passed.
```

### 测试输出示例

```
===== Running Tests =====

Running Kaula tests...

test test_addition ... ok
test test_string ... ok
  (字符串基础操作)
test test_ignored ... IGNORED

===== Test Summary =====
Total:   4
Passed:  2
Failed:  0
Ignored: 1
Time:    0.001s
========================

All tests passed.
```

### `#[test("描述")]` 测试描述

给测试附加一行说明，输出时显示在测试名下方：

```kaula
#[test("2 + 3 必须等于 5")]
fn test_addition() {
    assert(2 + 3 == 5)
}
```

描述会被转义后写入生成的 C 源码，引号和反斜杠都是安全的。描述不能跨行
（lexer 扫描 `#[...]` 内容时遇换行即止）。

### `#[ignore]` 忽略测试

使用 `#[ignore]` 属性可以暂时跳过某个测试：

import std.io
```kaula
#[test]
#[ignore]
fn test_not_ready() {
    // 这个测试不会被执行
    std.io.println("this will not run")
}
```

### `#[should_panic]` 预期失败

标记「这个测试本就应该断言失败」。若该测试的断言确实失败则计为通过；
若它反而通过了，则计为 FAILED：

```kaula
#[test]
#[should_panic]
fn test_known_bug() {
    assert(buggy_value() == 3)   // 目前失败 -> 测试通过
}
```

### 注意事项

- 测试函数签名必须是 `fn name()`，无参数无返回值
- 测试函数不需要 `pub` 修饰符，编译器会自动识别（也不会报未使用警告）
- 测试模式下，原有的 `main` 函数会被测试运行器替代
- 测试函数中的 `println` 输出会直接显示在控制台
- 断言失败信息会打到 stderr，并带上 `文件:行号`

---

## `#[derive]` 派生宏

### 概述

`#[derive]` 属性为结构体自动生成常用方法实现，避免重复编写样板代码。目前支持三个 trait：`Debug`、`Clone`、`Eq`。

### 基本用法

```kaula
#[derive(Debug, Clone, Eq)]
struct Point {
    x: i64
    y: i64
}
```

编译器会自动生成以下方法：

| Trait | 生成方法 | 签名 |
|-------|---------|------|
| Debug | `Point_debug` | `fn Point_debug(self: *Point)` |
| Clone | `Point_clone` | `fn Point_clone(self: *Point) -> *Point` |
| Eq | `Point_equals` | `fn Point_equals(a: *Point, b: *Point) -> bool` |

### Debug - 调试打印

自动生成逐字段打印的方法：

```kaula
Point p
p.x = 10
p.y = 20
Point_debug(&p)
// 输出: Point { x: 10, y: 20 }
```

支持的字段类型及打印方式：
- 整数类型（i8/i16/i32/i64/u8/u16/u32/u64）：`%lld`
- 浮点类型（f32/f64）：`%f`
- 布尔类型：`true` / `false`
- 字符串类型：带引号打印
- 其他类型：显示类型名占位

### Clone - 深拷贝

自动生成内存拷贝方法：

```kaula
Point* p2 = Point_clone(&p)
// p2 是 p 的完整副本，修改 p2 不影响 p
```

实现方式：`malloc` 分配新内存 + `memcpy` 拷贝。

### Eq - 相等比较

自动生成逐字段比较方法：

import std.io
```kaula
Point a
a.x = 5
a.y = 5
Point b
b.x = 5
b.y = 5

if Point_equals(&a, &b) == 1 {
    std.io.println("a == b")
}

b.x = 6
if Point_equals(&a, &b) == 0 {
    std.io.println("a != b")
}
```

比较规则：
- 整数/布尔：直接 `!=` 比较
- 浮点：直接 `!=` 比较
- 字符串：使用 `strcmp` 比较
- 指针：空指针检查 + 逐字段比较

### 注意事项

- 派生方法名格式为 `{StructName}_{trait}`，不带 `K_` 前缀
- sema 阶段会自动注册派生方法符号，无需手动声明
- 编译器会自动生成前向声明，避免 implicit declaration 警告
- 结构体字段必须有明确的类型声明

---

## `#[command]` 命令行参数解析

### 概述

`#[command]` 属性为结构体自动生成命令行参数解析函数，支持短选项、长选项、默认值和帮助文本。

### 基本用法

```kaula
#[command]
struct Config {
    #[arg(short="v", long="verbose", help="启用详细输出")]
    verbose: bool

    #[arg(short="o", long="output", default="result.txt", help="输出文件路径")]
    output: string

    #[arg(short="n", long="count", default="10", help="处理数量")]
    count: i64
}
```

编译器会自动生成 `Config_parse(argc, argv)` 函数。

### `#[arg]` 字段属性

| 参数 | 说明 | 示例 |
|------|------|------|
| `short` | 短选项名（不带 `-`） | `short="v"` → `-v` |
| `long` | 长选项名（不带 `--`） | `long="verbose"` → `--verbose` |
| `default` | 默认值 | `default="10"` |
| `help` | 帮助文本 | `help="启用详细输出"` |

### 使用示例

import std.io
```kaula
fn main(i32 argc, char** argv) {
    Config* cfg = Config_parse(argc, argv)
    if cfg == null {
        std.io.println("参数解析失败")
        return
    }

    if cfg.verbose == 1 {
        std.io.println("详细模式已启用")
    }
    std.io.println("输出文件: ")
    std.io.println(cfg.output)
}
```

命令行调用：

```bash
./myapp -v -o output.txt -n 42
# 或
./myapp --verbose --output=output.txt --count=42
```

### 支持的字段类型

| 类型 | 解析方式 | 默认值示例 |
|------|---------|-----------|
| `bool` | 出现即设为 1（true） | `default="true"` |
| 整数类型 | `atoll()` 转换 | `default="10"` |
| 浮点类型 | `atof()` 转换 | `default="3.14"` |
| `string` | `string_wrap()` 包装 | `default="hello"` |

### 注意事项

- 布尔选项不需要后跟值，出现即表示 true
- 非布尔选项必须后跟值，否则使用默认值
- `string` 类型使用 `string_wrap()` 零成本包装，不额外分配内存
- 解析函数返回堆分配的结构体指针，使用后需手动释放

---

## `#[prefix]` 前缀模板函数

### 概述

`#[prefix]` 把一个普通函数声明为**编译期模板**：调用点写 `@Name(参数 = 值)`
时，编译器在当前位置展开函数体，不做真实调用、不产生函数调用开销。

### 基本用法

import std.io
```kaula
#[prefix]
pub fn trace(msg: string) {
    std.io.println("[T] ", $msg)
}

fn main() {
    @trace(msg = "direct")     // 展开为 std.io.println("[T] ", "direct")
}
```

**模板参数必须用 `$` 引用。** 形参 `msg` 在函数体里写成 `$msg`；写成裸 `msg`
时展开器不会替换它，生成的 C 代码会引用一个不存在的标识符（编译期报
`use of undeclared identifier`）。

### 与 `prefix Name { ... }` 的区别

| | `prefix Name { ... }` 语句 | `#[prefix] fn Name(...)` |
|---|---|---|
| 位置 | 任意语句位置（函数体内也可） | 顶层的函数声明 |
| 参数声明 | 模板体内的裸 `$param` / `$param = 默认值` | 形参列表，体内按 `$param` 引用 |
| 跨文件可见 | 需要调用方 `import` 定义文件 | 同左，且遵循 `pub` 修饰符 |
| 主要用途 | 局部代码片段模板 | 跨文件共享的日志/校验/资源包装 |

### 跨文件可见性

`import "helper.kl"` 之后，helper 中的 `#[prefix]` 函数与 `prefix` 语句都能在
导入方展开。跨文件查找由 CLI 侧 `collectLocalPrefixDefs` 收集、经
`SetLocalExternalPrefixes` 注入语义分析与代码生成两侧。

注意这个能力**曾经完全失效**：两侧都只查本文件 AST，跨文件 `@Name(...)` 被静默
丢弃（编译 `Success`、无错误、无代码）。回归用例：`test/prefix_xfile.kl`。

### 注意事项

- 展开结果包在独立块作用域内，同名变量多次展开不会重定义。
- `@Name(...)` 的实参可以是任意表达式，在**调用点**的作用域里求值。
- 前缀不能用于需要跨 TU 链接的场景：它是纯编译期展开，展开后的代码落在调用方 TU。

---

## 属性组合使用

多个属性可以组合使用，发挥更大威力：

import std.io
```kaula
#[derive(Debug, Clone, Eq)]
#[command]
struct AppConfig {
    #[arg(short="v", long="verbose", help="详细模式")]
    verbose: bool

    #[arg(short="o", long="output", default="out.txt", help="输出文件")]
    output: string
}

#[test]
fn test_config_defaults() {
    AppConfig* cfg = AppConfig_parse(1, null)
    if cfg.verbose != 0 {
        std.io.println("FAIL: verbose should default to false")
    }
    AppConfig_debug(cfg)
}
```

---

## 实现细节

### 编译器集成

高级属性扩展的核心实现在 `compiler/internal/codegen/attribute_features.go`：

- `CollectTestFunctions()` - 收集 `#[test]` 函数（含 `#[ignore]`、`#[should_panic]`、`#[test("描述")]`）
- `GenerateTestRunner()` - 生成测试运行器代码
- `cStringLiteral()` - 把描述编码为安全的 C 字符串字面量
- `CollectDeriveTypes()` - 收集 `#[derive]` 结构体
- `GenerateDeriveImpls()` - 生成派生方法实现
- `CollectCommands()` - 收集 `#[command]` 结构体
- `GenerateCommandParser()` - 生成命令行解析器

### 编译流程

1. **Lexer**：扫描 `#[...]` 原文内容（按字节收集，保留 UTF-8）
2. **Parser**：解析 `#[attribute]` 语法，支持连续属性（如 `#[test]#[ignore]`）
3. **Sema**：注册派生方法和命令解析函数符号，跳过测试函数的未使用警告
4. **Codegen**：
   - 收集所有高级属性信息
   - 生成前向声明
   - 在函数代码后插入派生实现和命令解析器
   - 测试模式下用测试运行器替代 main 函数

### 命令入口

| 命令 | 行为 | 与 `#[test]` 的关系 |
|------|------|-------------------|
| `kaulac unittest <file.kl>` | 置 `testMode`，编译后自动运行 `#[test]` runner，失败数决定退出码 | **本属性由它驱动** |
| `kaulac unittest [dir]` | 批量：递归找出含 `#[test]` 的 `.kl` 逐个运行并汇总 | 同上 |
| `kaulac test [file.kl\|dir]` | 快照回归测试器：普通模式编译含顶层 `main` 的 `.kl`，比对 `.kl.stdout` | 无关 |
| `kaulac compile/run/build/check` | 普通编译 | 无关 |

`testMode` 一旦为真，`assert(cond)` 会绑定到 runner 的 `__kaula_test_assert`
（记录失败而非 `exit(1)`），这是 `#[test]` 能真正判定失败的前提。

### 与已有属性的关系

Kaula 已支持的属性（函数/变量/类型级）不受影响：
- 函数：`inline`、`naked`、`section`、`weak`、`deprecated`、`asm`、`no_kmm`、`sor`、`prefix`、`tree`、`root`
- 变量：`volatile`、`aligned`、`section`、`weak`、`deprecated`
- 类型：`packed`、`aligned`、`transparent`

新增属性与已有属性正交，可以自由组合。
